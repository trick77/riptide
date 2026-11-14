package metrics

import (
	"slices"
	"time"
)

// Windows are the period lengths the page offers, in weeks.
var Windows = []int{4, 12, 26}

// ValidWindow reports whether weeks is one of Windows.
func ValidWindow(weeks int) bool { return slices.Contains(Windows, weeks) }

// Week is one calendar week, Monday 00:00 to Monday 00:00 in the viewer's
// zone, with its ISO 8601 number.
type Week struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	KW    int       `json:"kw"`
}

// Period is the window the page shows: whole calendar weeks in the
// viewer's zone, the current one running to now. Charts bucket per week.
type Period struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Weeks []Week    `json:"weeks"`
	// Span is the length in weeks, the current one counted as far as it has
	// run: the divisor of a per-week rate.
	Span float64 `json:"-"`
}

// weekStart is the Monday 00:00 of t's week in loc.
func weekStart(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	d := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, loc)
	back := (int(d.Weekday()) + 6) % 7
	return d.AddDate(0, 0, -back)
}

// NewPeriod is the period of the given number of weeks ending now. Calendar
// arithmetic, so a week across a DST switch is 167 or 169 hours.
func NewPeriod(weeks int, now time.Time, loc *time.Location) Period {
	first := weekStart(now, loc).AddDate(0, 0, -7*(weeks-1))
	list := make([]Week, weeks)
	for i := range list {
		start := first.AddDate(0, 0, 7*i)
		_, kw := start.ISOWeek()
		list[i] = Week{Start: start.UTC(), End: start.AddDate(0, 0, 7).UTC(), KW: kw}
	}
	return Period{
		From:  first.UTC(),
		To:    now.UTC(),
		Weeks: list,
		Span:  float64(now.Sub(first)) / float64(7*24*time.Hour),
	}
}

// WeekIndex is the index of the week t falls in, or -1 outside the period.
func (p Period) WeekIndex(t time.Time) int {
	if t.Before(p.From) || !t.Before(p.To) {
		return -1
	}
	for i, w := range p.Weeks {
		if t.Before(w.End) {
			return i
		}
	}
	return -1
}

// In reports whether t is set and inside the period.
func (p Period) In(t *time.Time) bool { return t != nil && p.WeekIndex(*t) >= 0 }

// bucket sorts rows into the period's weeks in one pass; rows outside it,
// or without an instant, are dropped.
func bucket[T any](p Period, rows []T, at func(T) *time.Time) [][]T {
	out := make([][]T, len(p.Weeks))
	for _, r := range rows {
		t := at(r)
		if t == nil {
			continue
		}
		if i := p.WeekIndex(*t); i >= 0 {
			out[i] = append(out[i], r)
		}
	}
	return out
}
