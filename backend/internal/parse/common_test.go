package parse

import (
	"reflect"
	"testing"
	"time"
)

func TestChangeType(t *testing.T) {
	for _, c := range []struct{ branch, want string }{
		{"feature/ABC-1-do-thing", "feature"},
		{"feat/short", "feature"},
		{"bugfix/x", "bugfix"},
		{"fix/x", "bugfix"},
		{"hotfix/page", "hotfix"},
		{"chore/deps", "chore"},
		{"refactor/x", "refactor"},
		{"docs/readme", "docs"},
		{"wip/x", "other"},
		{"HOTFIX", "hotfix"},
		{"", ""},
	} {
		if got := ChangeType(c.branch); got != c.want {
			t.Errorf("ChangeType(%q) = %q, want %q", c.branch, got, c.want)
		}
	}
}

func TestJiraKeys(t *testing.T) {
	for _, c := range []struct {
		name  string
		texts []string
		want  []string
	}{
		{"single key in title", []string{"ABC-123: do thing"}, []string{"ABC-123"}},
		{"dedupe across sources", []string{"ABC-1 fix", "ABC-1 also fix", "feature/ABC-2"}, []string{"ABC-1", "ABC-2"}},
		{"several in one string", []string{"ABC-1 and ABC-2 in PROJ-99"}, []string{"ABC-1", "ABC-2", "PROJ-99"}},
		{"lowercase is not a key", []string{"abc-1 do thing"}, []string{}},
		{"empty sources skipped", []string{"", "", "ABC-9"}, []string{"ABC-9"}},
		{"no keys", []string{"just some text"}, []string{}},
		{"needs a word boundary before", []string{"XABC-1", "xABC-1"}, []string{"XABC-1"}},
		{"needs a word boundary after", []string{"ABC-1x ABC-2_ ABC-3é"}, []string{}},
		{"punctuation is a boundary", []string{"(ABC-1), [ABC-2]"}, []string{"ABC-1", "ABC-2"}},
		{"single letter project is not a key", []string{"A-1"}, []string{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := JiraKeys(c.texts...); !reflect.DeepEqual(got, c.want) {
				t.Errorf("JiraKeys(%q) = %q, want %q", c.texts, got, c.want)
			}
		})
	}
}

func TestIsRevert(t *testing.T) {
	for _, c := range []struct {
		title string
		want  bool
	}{
		{`Revert "fix bug"`, true},
		{`revert "thing"`, true},
		{"Normal commit", false},
		{"", false},
		{`Reverts "x"`, false},
	} {
		if got := IsRevert(c.title); got != c.want {
			t.Errorf("IsRevert(%q) = %v, want %v", c.title, got, c.want)
		}
	}
}

func TestEnvironment(t *testing.T) {
	for _, c := range []struct{ ns, want string }{
		{"payments-prod", "prod"},
		{"checkout-intg", "intg"},
		{"billing-syst", "syst"},
		{"orders-dev", "dev"},
		{"noergler-entw", "entw"},
		{"payments-qa", "qa"},
		{"Payments-PROD", "prod"},
		{"multi-part-name-prod", "prod"},
		{"  payments-prod  ", "prod"},
		{"", ""},
		{"  ", ""},
		{"noseparator", ""},
		{"trailing-", ""},
		{"-leading", ""},
	} {
		if got := Environment(c.ns); got != c.want {
			t.Errorf("Environment(%q) = %q, want %q", c.ns, got, c.want)
		}
	}
}

func TestParseTime(t *testing.T) {
	utc := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return v.UTC()
	}
	for _, c := range []struct {
		in   string
		want time.Time
	}{
		{"2026-04-28T10:15:00Z", utc("2026-04-28T10:15:00Z")},
		{"2026-04-28T10:15:00.123456Z", utc("2026-04-28T10:15:00.123456Z")},
		{"2026-04-28T10:15:00+02:00", utc("2026-04-28T08:15:00Z")},
		// Bitbucket Data Center: offset without a colon.
		{"2026-04-28T20:15:00+1000", utc("2026-04-28T10:15:00Z")},
		{"2026-04-28T10:15:00-0130", utc("2026-04-28T11:45:00Z")},
		{"2026-04-28T10:15:00+02", utc("2026-04-28T08:15:00Z")},
		// No offset: UTC.
		{"2026-04-28T10:15:00", utc("2026-04-28T10:15:00Z")},
		{"2026-04-28 10:15", utc("2026-04-28T10:15:00Z")},
		{"2026-04-28", utc("2026-04-28T00:00:00Z")},
		{"2026-04-28T10:15:00.5+00:00", utc("2026-04-28T10:15:00.5Z")},
	} {
		got, err := Time(c.in)
		if err != nil {
			t.Errorf("Time(%q): %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) || got.Location() != time.UTC {
			t.Errorf("Time(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	for _, bad := range []string{
		"", "yesterday", "2026-13-01T00:00:00Z", "2026-04-28T25:00:00Z", "2026-04-28X10:15",
		"2026-04-28T10", "2026-04-28T10:15:00+1", "2026-04-28T10:15:00.Z", "2026-04-28T10:15:00+10:00:00",
		"2026-04-28T10:15:61Z", "2026-04-28T10:15:00.1234567890Z", "2026-04-28T10:15:00+2400",
	} {
		if _, err := Time(bad); err == nil {
			t.Errorf("Time(%q) accepted", bad)
		}
	}
}

func TestISOFormatMatchesPython(t *testing.T) {
	ts := time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)
	if got := isoFormat(ts); got != "2026-04-01T08:00:00+00:00" {
		t.Errorf("isoFormat = %q", got)
	}
	ts = ts.Add(123456789 * time.Nanosecond)
	if got := isoFormat(ts); got != "2026-04-01T08:00:00.123456+00:00" {
		t.Errorf("isoFormat = %q", got)
	}
}

func TestJSONBSafe(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want bool
	}{
		{`{"a": "plain"}`, true},
		{`{"a": "é \n \" \\"}`, true},
		{`{"a": "😀"}`, true},                       // a surrogate pair
		{`{"a": "quoting \\u0000 as text"}`, true}, // escaped backslash, then text
		{`{"a": "\\A"}`, true},                     // escaped backslash, then a real escape
		{`{"u0000": "\u0000"}`, false},             // NUL
		{`{"a": "x\u0000"}`, false},
		{`{"a": "\ud800"}`, false},  // lone high surrogate
		{`{"a": "\ud800A"}`, false}, // high surrogate, no low
		{`{"a": "\udc00"}`, false},  // lone low surrogate
		{`{"a": "\ud800\\"}`, false},
		{`{"a": "\u12"}`, false},     // truncated (invalid JSON anyway)
		{`{"key \u0000": 1}`, false}, // keys count too
	} {
		if got := jsonbSafe([]byte(c.raw)); got != c.want {
			t.Errorf("jsonbSafe(%s) = %v, want %v", c.raw, got, c.want)
		}
	}
}
