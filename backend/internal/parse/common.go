// Package parse turns webhook bodies into typed drafts ready for insert. Pure
// functions only: no HTTP, no database, no config. Routers do auth, dispatch,
// config-derived fields and persistence.
package parse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var changeTypes = map[string]string{
	"feature":  "feature",
	"feat":     "feature",
	"bugfix":   "bugfix",
	"fix":      "bugfix",
	"hotfix":   "hotfix",
	"chore":    "chore",
	"refactor": "refactor",
	"docs":     "docs",
}

// ChangeType maps a branch name to its change type by the prefix before the
// first `/`: "" for no branch, "other" for an unknown prefix.
func ChangeType(branch string) string {
	if branch == "" {
		return ""
	}
	prefix, _, _ := strings.Cut(branch, "/")
	if ct, ok := changeTypes[strings.ToLower(prefix)]; ok {
		return ct
	}
	return "other"
}

var jiraKeyRE = regexp.MustCompile(`[A-Z][A-Z0-9]+-[0-9]+`)

// isWord is Python's \w for str patterns: letters, numbers, underscore.
func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

// JiraKeys returns the distinct Jira keys in first-seen order across texts.
// A key must stand on word boundaries: `XABC-1` and `ABC-1x` are not keys.
func JiraKeys(texts ...string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range texts {
		for _, loc := range jiraKeyRE.FindAllStringIndex(s, -1) {
			if loc[0] > 0 {
				if r, _ := utf8.DecodeLastRuneInString(s[:loc[0]]); isWord(r) {
					continue
				}
			}
			if loc[1] < len(s) {
				if r, _ := utf8.DecodeRuneInString(s[loc[1]:]); isWord(r) {
					continue
				}
			}
			key := s[loc[0]:loc[1]]
			if !seen[key] {
				seen[key] = true
				out = append(out, key)
			}
		}
	}
	return out
}

// IsRevert reports a Git-generated revert title: `Revert "…`, any case.
func IsRevert(title string) bool {
	const prefix = `revert "`
	return len(title) >= len(prefix) && strings.EqualFold(title[:len(prefix)], prefix)
}

// Environment is the lowercased stage suffix of an Argo CD destination
// namespace (`payments-prod` -> `prod`), or "" when the namespace has no
// `-` or an empty side. Unknown suffixes pass through; the config decides
// which one is production.
func Environment(namespace string) string {
	namespace = strings.TrimSpace(namespace)
	i := strings.LastIndex(namespace, "-")
	if i <= 0 || i == len(namespace)-1 {
		return ""
	}
	return strings.ToLower(namespace[i+1:])
}

// Time parses an ISO 8601 timestamp and normalises it to UTC; a value
// without an offset is taken as UTC. Accepted: a date, optionally followed by
// `T`, `t` or a space and HH:MM[:SS[.fraction]], optionally followed by `Z`,
// ±HH, ±HHMM or ±HH:MM. Bitbucket Data Center writes offsets without the
// colon (`+1000`), which RFC 3339 parsing rejects.
func Time(s string) (time.Time, error) {
	bad := fmt.Errorf("invalid ISO 8601 timestamp %q", s)
	if len(s) < 10 {
		return time.Time{}, bad
	}
	date, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return time.Time{}, bad
	}
	rest := s[10:]
	if rest == "" {
		return date.UTC(), nil
	}
	if rest[0] != 'T' && rest[0] != 't' && rest[0] != ' ' {
		return time.Time{}, bad
	}
	rest = rest[1:]

	// Split the clock from the offset.
	clock, zone := rest, ""
	if i := strings.IndexAny(rest, "Zz+-"); i >= 0 {
		clock, zone = rest[:i], rest[i:]
	}
	var hh, mm, ss, nanos int
	parts := strings.Split(clock, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return time.Time{}, bad
	}
	if hh, err = twoDigits(parts[0]); err != nil || hh > 23 {
		return time.Time{}, bad
	}
	if mm, err = twoDigits(parts[1]); err != nil || mm > 59 {
		return time.Time{}, bad
	}
	if len(parts) == 3 {
		sec, frac, hasFrac := strings.Cut(parts[2], ".")
		if !hasFrac {
			sec, frac, hasFrac = strings.Cut(parts[2], ",")
		}
		if ss, err = twoDigits(sec); err != nil || ss > 59 {
			return time.Time{}, bad
		}
		if hasFrac {
			if frac == "" || len(frac) > 9 || strings.Trim(frac, "0123456789") != "" {
				return time.Time{}, bad
			}
			n, _ := strconv.Atoi(frac + strings.Repeat("0", 9-len(frac)))
			nanos = n
		}
	}

	loc := time.UTC
	switch zone {
	case "", "Z", "z":
	default:
		sign := 1
		if zone[0] == '-' {
			sign = -1
		}
		z := strings.ReplaceAll(zone[1:], ":", "")
		if len(z) != 2 && len(z) != 4 {
			return time.Time{}, bad
		}
		zh, err := twoDigits(z[:2])
		if err != nil || zh > 23 {
			return time.Time{}, bad
		}
		zm := 0
		if len(z) == 4 {
			if zm, err = twoDigits(z[2:]); err != nil || zm > 59 {
				return time.Time{}, bad
			}
		}
		loc = time.FixedZone("", sign*(zh*3600+zm*60))
	}
	t := time.Date(date.Year(), date.Month(), date.Day(), hh, mm, ss, nanos, loc)
	return t.UTC(), nil
}

func twoDigits(s string) (int, error) {
	if len(s) != 2 || s[0] < '0' || s[0] > '9' || s[1] < '0' || s[1] > '9' {
		return 0, fmt.Errorf("not two digits: %q", s)
	}
	return int(s[0]-'0')*10 + int(s[1]-'0'), nil
}

// isoFormat renders t the way Python's datetime.isoformat() does for an
// aware UTC value: microseconds only when non-zero, offset as +00:00. The
// Argo CD dedup key embeds it.
func isoFormat(t time.Time) string {
	t = t.UTC().Truncate(time.Microsecond)
	s := t.Format("2006-01-02T15:04:05")
	if us := t.Nanosecond() / 1000; us != 0 {
		s += fmt.Sprintf(".%06d", us)
	}
	return s + "+00:00"
}

// ptr returns a pointer to s, or nil for "": the nullable column form.
func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
