package metrics

import (
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// ---- Trends ----------------------------------------------------------------

// Why a trend's week has no value (TrendSeries.Gaps).
const (
	// GapNoData: the series' source had not started reporting by the week's
	// start: an empty week there is no news, a part of one reads low.
	GapNoData = "no_data"
	// GapLost: a lost Bitbucket silence (dataset.Silence.Lost) overlaps the
	// week: its merges read low, a count there would be a false dip.
	GapLost = "lost"
	// GapNone: the week has data but no case to take a typical value of (no
	// PR with a size): no line through it, no zero either.
	GapNone = "none"
)

// Trends are three of the page's numbers week by week over the previous
// period, the period and the week running now, so a line shows where a
// number went instead of two totals: the people's merged PRs (as
// Throughput.Weeks counts them), their typical size, and the releases to
// production (as DeployFrequency.Weeks counts them). A week a source could
// not have seen in full is a gap, never a zero.
type Trends struct {
	// Weeks are the previous period's weeks, then the period's, then the
	// running week's when the page reads one (TrendWeek.Running).
	Weeks []TrendWeek `json:"weeks"`
	// Split is the index of the period's first week in Weeks.
	Split    int         `json:"split"`
	Merged   TrendSeries `json:"merged"`
	Batch    TrendSeries `json:"batch"`
	Releases TrendSeries `json:"releases"`
}

// TrendWeek is a week of the trends.
type TrendWeek struct {
	Week
	// Running: the week running now, counted up to now: no whole week, so
	// the page marks it apart.
	Running bool `json:"running"`
}

// TrendSeries is one number per trend week, each parallel to Trends.Weeks.
type TrendSeries struct {
	// Values are the week's number; null where Gaps names why.
	Values []*float64 `json:"values"`
	// N is each week's base: merged PRs, PRs with a size, releases.
	N []int `json:"n"`
	// Gaps are why a week has no value (GapNoData, GapLost, GapNone), ""
	// where it has one.
	Gaps []string `json:"gaps"`
}

func (s *TrendSeries) add(v *float64, n int, gap string) {
	if gap != "" {
		v = nil
	}
	s.Values, s.N, s.Gaps = append(s.Values, v), append(s.N, n), append(s.Gaps, gap)
}

// weekGap is why a source's week [from, to) has no value: the source's data
// starting after the week's start, or, for Bitbucket's numbers, a lost
// silence (lost) overlapping its working time in loc. A silence reaching
// into a Sunday took no merges of the week.
func weekGap(from, to time.Time, start *time.Time, lost []dataset.Silence, loc *time.Location) string {
	if start == nil || start.After(from) {
		return GapNoData
	}
	for _, s := range lost {
		if s.To.After(from) && s.From.Before(to) && workingDays(laterTime(s.From, from), earlierTime(s.To, to), loc) > 0 {
			return GapLost
		}
	}
	return ""
}

func laterTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlierTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// lostOf are the lost silences the datasets' metas name (dataset.Meta.Lost).
func lostOf(dss ...*dataset.Dataset) []dataset.Silence {
	var out []dataset.Silence
	for _, ds := range dss {
		if ds != nil {
			out = append(out, ds.Meta.Lost...)
		}
	}
	return out
}

// trends are m's and pm's weekly numbers side by side, then the running
// week's from running (ds over p.Running, nil without one) as RunningWeek
// counts it; the period's data start and lost silences decide the gaps.
func trends(m, pm Metrics, running *dataset.Dataset, p Params, f filter) Trends {
	start := m.Meta.DataStart
	lost := lostOf(&dataset.Dataset{Meta: m.Meta}, &dataset.Dataset{Meta: pm.Meta}, running)
	out := Trends{Weeks: []TrendWeek{}, Split: len(pm.Period.Weeks),
		Merged:   TrendSeries{Values: []*float64{}, N: []int{}, Gaps: []string{}},
		Batch:    TrendSeries{Values: []*float64{}, N: []int{}, Gaps: []string{}},
		Releases: TrendSeries{Values: []*float64{}, N: []int{}, Gaps: []string{}},
	}
	type week struct {
		w             TrendWeek
		to            time.Time
		merged, sized int
		batch         *float64
		releases      int
	}
	var weeks []week
	for _, x := range []Metrics{pm, m} {
		for i, w := range x.Period.Weeks {
			tw := week{w: TrendWeek{Week: w}, to: w.End}
			if i < len(x.Throughput.Weeks) {
				t := x.Throughput.Weeks[i]
				tw.merged, tw.sized, tw.batch = t.Merged, t.Sized, t.BatchP50
			}
			if i < len(x.Delivery.Frequency.Weeks) {
				tw.releases = x.Delivery.Frequency.Weeks[i].Release
			}
			weeks = append(weeks, tw)
		}
	}
	if running != nil && len(p.Running.Weeks) == 1 {
		var sizes []float64
		humans := mergedPRs(running, p.Running, f)
		for _, pr := range humans {
			if l, _, ok := prSize(pr); ok {
				sizes = append(sizes, l)
			}
		}
		weeks = append(weeks, week{w: TrendWeek{Week: p.Running.Weeks[0], Running: true}, to: p.Running.To,
			merged: m.RunningWeek.Merged, sized: len(sizes), batch: Median(sizes), releases: m.RunningWeek.Releases})
	}
	loc := p.Period.location()
	for _, w := range weeks {
		out.Weeks = append(out.Weeks, w.w)
		merged := weekGap(w.w.Start, w.to, start.Bitbucket, lost, loc)
		out.Merged.add(f64(w.merged), w.merged, merged)
		batch := merged
		if batch == "" {
			batch = weekGap(w.w.Start, w.to, start.Noergler, nil, loc)
		}
		if batch == "" && w.batch == nil {
			batch = GapNone
		}
		out.Batch.add(w.batch, w.sized, batch)
		out.Releases.add(f64(w.releases), w.releases, weekGap(w.w.Start, w.to, start.ArgoCD, nil, loc))
	}
	return out
}
