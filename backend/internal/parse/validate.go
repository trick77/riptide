package parse

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// FieldError is one validation failure, in FastAPI's 422 shape:
// {"type": ..., "loc": ["body", field], "msg": ...}.
type FieldError struct {
	Type string   `json:"type"`
	Loc  []string `json:"loc"`
	Msg  string   `json:"msg"`
}

// ValidationError is a rejected body. Every failing field is reported, not
// just the first, so a sender fixes its template in one round.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, fe := range e.Errors {
		parts[i] = strings.Join(fe.Loc, ".") + ": " + fe.Msg
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// jsonbSafe reports whether Postgres JSONB accepts every string escape in
// raw, which must already be valid JSON. JSON allows two escapes JSONB
// refuses: \u0000, and a UTF-16 surrogate that is not half of a pair. Go's
// decoder accepts both (it substitutes U+FFFD), so without this check the
// body would parse and then fail at insert, as a 500 on every retry.
func jsonbSafe(raw []byte) bool {
	inString := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inString {
			inString = c == '"'
			continue
		}
		switch c {
		case '"':
			inString = false
		case '\\':
			i++
			if i >= len(raw) || raw[i] != 'u' {
				continue
			}
			v, ok := hex4(raw, i+1)
			if !ok {
				return false
			}
			i += 4
			switch {
			case v == 0:
				return false
			case v >= 0xDC00 && v <= 0xDFFF:
				return false // a low surrogate with no high one before it
			case v >= 0xD800 && v <= 0xDBFF:
				if i+2 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
					return false
				}
				low, ok := hex4(raw, i+3)
				if !ok || low < 0xDC00 || low > 0xDFFF {
					return false
				}
				i += 6
			}
		}
	}
	return true
}

func hex4(raw []byte, at int) (int, bool) {
	if at+4 > len(raw) {
		return 0, false
	}
	v, err := strconv.ParseUint(string(raw[at:at+4]), 16, 32)
	return int(v), err == nil
}

// object is a decoded JSON body plus the errors found reading it.
type object struct {
	fields map[string]json.RawMessage
	errs   []FieldError
}

// decodeObject reads a body that must be one JSON object.
func decodeObject(raw []byte) (*object, error) {
	invalid := func(msg string) error {
		return &ValidationError{Errors: []FieldError{{Type: "json_invalid", Loc: []string{"body"}, Msg: msg}}}
	}
	notObject := &ValidationError{Errors: []FieldError{{Type: "model_attributes_type", Loc: []string{"body"}, Msg: "Input should be a valid dictionary or object"}}}
	if !utf8.Valid(raw) {
		return nil, invalid("JSON decode error")
	}
	// json.Unmarshal checks the whole input, trailing bytes included.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return nil, notObject
		}
		return nil, invalid("JSON decode error")
	}
	if fields == nil {
		return nil, notObject
	}
	if !jsonbSafe(raw) {
		return nil, invalid("JSON contains \\u0000 or an unpaired surrogate escape, which Postgres JSONB cannot store")
	}
	return &object{fields: fields}, nil
}

func (o *object) fail(field, typ, msg string) {
	o.errs = append(o.errs, FieldError{Type: typ, Loc: []string{"body", field}, Msg: msg})
}

func (o *object) hasError(field string) bool {
	for _, fe := range o.errs {
		if len(fe.Loc) > 1 && fe.Loc[1] == field {
			return true
		}
	}
	return false
}

func (o *object) err() error {
	if len(o.errs) == 0 {
		return nil
	}
	return &ValidationError{Errors: o.errs}
}

// forbidExtra rejects keys outside allowed: for contracts where a typo must
// not land silently in the payload column.
func (o *object) forbidExtra(allowed ...string) {
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[a] = true
	}
	var extra []string
	for k := range o.fields {
		if !ok[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		o.fail(k, "extra_forbidden", "Extra inputs are not permitted")
	}
}

// present reports whether the key exists with a non-null value.
func (o *object) present(field string) (json.RawMessage, bool) {
	raw, ok := o.fields[field]
	if !ok || isJSONNull(raw) {
		return nil, false
	}
	return raw, true
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (o *object) rawString(field string) (string, bool, bool) {
	raw, ok := o.present(field)
	if !ok {
		return "", false, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		o.fail(field, "string_type", "Input should be a valid string")
		return "", false, false
	}
	return s, true, true
}

// requiredString reads a mandatory string of at least minLen characters.
func (o *object) requiredString(field string, minLen int) string {
	s, ok, typed := o.rawString(field)
	if !typed {
		return ""
	}
	if !ok {
		o.missing(field)
		return ""
	}
	if n := len([]rune(s)); n < minLen {
		o.fail(field, "string_too_short", fmt.Sprintf("String should have at least %d character%s", minLen, plural(minLen)))
		return ""
	}
	return s
}

// optionalString reads an optional string. Absent, null, "" and
// whitespace-only all mean absent: a templated-but-unset parameter arrives
// empty far more often than missing, and rejecting it would drop the event.
// A present value must have at least minLen characters.
func (o *object) optionalString(field string, minLen int) *string {
	s, ok, typed := o.rawString(field)
	if !typed || !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	if n := len([]rune(s)); n < minLen {
		o.fail(field, "string_too_short", fmt.Sprintf("String should have at least %d character%s", minLen, plural(minLen)))
		return nil
	}
	return &s
}

// optionalTrimmed is optionalString with surrounding whitespace removed.
func (o *object) optionalTrimmed(field string) *string {
	s := o.optionalString(field, 0)
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

// literal reads a string that must be one of choices; def applies when the
// field is absent or empty.
func (o *object) literal(field, def string, choices ...string) string {
	s, ok, typed := o.rawString(field)
	if !typed {
		return ""
	}
	if !ok || (strings.TrimSpace(s) == "" && def != "") {
		if def == "" {
			o.missing(field)
		}
		return def
	}
	for _, c := range choices {
		if s == c {
			return s
		}
	}
	quoted := make([]string, len(choices))
	for i, c := range choices {
		quoted[i] = "'" + c + "'"
	}
	o.fail(field, "literal_error", "Input should be "+joinOr(quoted))
	return ""
}

// maxSpan is the longest started..finished span the generated
// duration_seconds column (an int4 of seconds) can hold.
const maxSpan = time.Duration(math.MaxInt32) * time.Second

// checkSpan rejects a start/finish pair whose duration would overflow
// duration_seconds, e.g. a sender's zero time.Time as started_at: Postgres
// would refuse the row on every retry.
func (o *object) checkSpan(field string, started, finished *time.Time) {
	if started == nil || finished == nil || o.hasError("started_at") {
		return
	}
	span := finished.Sub(*started)
	if span > maxSpan || span < -maxSpan {
		o.fail(field, "value_error", "Value error, finished_at is too far from started_at: the duration must fit 2147483647 seconds")
	}
}

func (o *object) missing(field string) {
	o.fail(field, "missing", "Field required")
}

// requiredTime reads a mandatory timestamp.
func (o *object) requiredTime(field string) time.Time {
	t := o.optionalTime(field, true)
	if t == nil {
		return time.Time{}
	}
	return *t
}

// optionalTime reads a timestamp: an ISO 8601 string (see Time) or Unix
// epoch seconds (milliseconds above 2e10), normalised to UTC. "" is absent.
func (o *object) optionalTime(field string, required bool) *time.Time {
	raw, ok := o.present(field)
	if !ok {
		if required {
			o.missing(field)
		}
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if strings.TrimSpace(s) == "" {
			if required {
				o.missing(field)
			}
			return nil
		}
		t, err := Time(strings.TrimSpace(s))
		if err != nil {
			o.fail(field, "datetime_from_date_parsing", "Input should be a valid datetime")
			return nil
		}
		return &t
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		f, err := n.Float64()
		if err == nil && !math.IsInf(f, 0) && math.Abs(f) < 1e14 {
			if math.Abs(f) > 2e10 {
				f /= 1000
			}
			sec, frac := math.Modf(f)
			t := time.Unix(int64(sec), int64(math.Round(frac*1e6))*1000).UTC()
			return &t
		}
	}
	o.fail(field, "datetime_type", "Input should be a valid datetime")
	return nil
}

// integer reads a mandatory integer >= minimum. A JSON number with no
// fractional part or a numeric string is accepted; the value must fit a
// BIGINT column.
func (o *object) integer(field string, minimum int64) int64 {
	raw, ok := o.present(field)
	if !ok {
		o.missing(field)
		return 0
	}
	var text string
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		text = n.String()
	} else if err := json.Unmarshal(raw, &text); err != nil {
		o.fail(field, "int_type", "Input should be a valid integer")
		return 0
	}
	v, ok := parseInt(strings.TrimSpace(text))
	if !ok {
		o.fail(field, "int_parsing", "Input should be a valid integer")
		return 0
	}
	if v < minimum {
		o.fail(field, "greater_than_equal", fmt.Sprintf("Input should be greater than or equal to %d", minimum))
		return 0
	}
	return v
}

// parseInt accepts "12", "12.0", "1.2e1"; rejects fractions and anything
// outside int64.
func parseInt(s string) (int64, bool) {
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v, true
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || !r.IsInt() || !r.Num().IsInt64() {
		return 0, false
	}
	return r.Num().Int64(), true
}

// costLimit is the smallest value NUMERIC(12,6) cannot hold once rounded to
// six places: 999999.9999995 rounds up to 1000000.
var costLimit = big.NewRat(9999999999995, 10000000)

// optionalDecimal reads an optional non-negative decimal as its exact text,
// so no float rounding happens before Postgres stores it. It must fit
// NUMERIC(12,6); digits beyond the sixth decimal are rounded by the column.
func (o *object) optionalDecimal(field string) *string {
	raw, ok := o.present(field)
	if !ok {
		return nil
	}
	var text string
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		text = n.String()
	} else if err := json.Unmarshal(raw, &text); err != nil {
		o.fail(field, "decimal_type", "Decimal input should be an integer, float, string or Decimal object")
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	r, ok := new(big.Rat).SetString(text)
	if !ok {
		o.fail(field, "decimal_parsing", "Input should be a valid decimal")
		return nil
	}
	if r.Sign() < 0 {
		o.fail(field, "greater_than_equal", "Input should be greater than or equal to 0")
		return nil
	}
	if r.Cmp(costLimit) >= 0 {
		o.fail(field, "less_than_equal", "Input should be less than 1000000")
		return nil
	}
	s := r.FloatString(6)
	return &s
}

// stringList reads a list of strings; required lists must be present.
func (o *object) stringList(field string, required bool) ([]string, bool) {
	raw, ok := o.present(field)
	if !ok {
		if required {
			o.missing(field)
		}
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		o.fail(field, "list_type", "Input should be a valid list")
		return nil, false
	}
	out := make([]string, 0, len(items))
	good := true
	for i, it := range items {
		var s string
		if err := json.Unmarshal(it, &s); err != nil {
			o.errs = append(o.errs, FieldError{Type: "string_type", Loc: []string{"body", field, strconv.Itoa(i)}, Msg: "Input should be a valid string"})
			good = false
			continue
		}
		out = append(out, s)
	}
	return out, good
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func joinOr(items []string) string {
	if len(items) <= 1 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}
