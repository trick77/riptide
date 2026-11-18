package metrics

import "time"

// Heatmap: a kind's failure rate per repo and week. A repo failing more shows
// as a red row, a shared outage as a red column, without reading a number.

// HeatMinRuns are the runs with a verdict a repo needs in a week for a
// class: below it one red run would paint the cell at 100 %. A starting
// value, not measured.
const HeatMinRuns = 3

// HeatSteps are the failure rates where classes 2, 3 and 4 start; class 1
// is any failure below the first. Starting values, not measured. Sent as a
// copy: a response must not alias it.
var HeatSteps = []float64{0.1, 0.2, 0.4}

// HeatCell is one repo's runs of a kind in one week, counted as the
// hotspots count them: with a verdict, outliers included.
type HeatCell struct {
	Runs   int `json:"runs"`
	Failed int `json:"failed"`
	// Rate is Failed over Runs; null without a run.
	Rate *float64 `json:"rate"`
	// Step is the class: null below HeatMinRuns runs, 0 without a failure,
	// 1..4 at the HeatSteps cut points.
	Step *int `json:"step"`
	// NoData: no run in a week before the run data starts (Build's
	// GapWeeks), unknown rather than none. A week with runs is never.
	NoData bool `json:"no_data"`
}

// HeatRow is one repo's weeks, a cell per week of the period.
type HeatRow struct {
	Repo string `json:"repo"`
	// Other: the kind's repos past the hotspots' cut, together, so every
	// column holds all of the kind's runs; Repo is empty.
	Other bool       `json:"other"`
	Weeks []HeatCell `json:"weeks"`
}

// HeatClass is one class of the heatmap, for the legend: a failure rate
// from From (inclusive) to To (exclusive), null where open. Class 0 is no
// failure at all, class 1 any failure under the first cut point.
type HeatClass struct {
	Step int      `json:"step"`
	From *float64 `json:"from"`
	To   *float64 `json:"to"`
}

// heatClasses are the classes HeatSteps makes, class 0 first.
func heatClasses() []HeatClass {
	out := []HeatClass{{Step: 0}}
	for s := 1; s <= len(HeatSteps)+1; s++ {
		c := HeatClass{Step: s}
		if s >= 2 {
			c.From = ptr(HeatSteps[s-2])
		}
		if s <= len(HeatSteps) {
			c.To = ptr(HeatSteps[s-1])
		}
		out = append(out, c)
	}
	return out
}

// heatStep classes a week of runs with failed of them red.
func heatStep(runs, failed int) *int {
	if runs < HeatMinRuns {
		return nil
	}
	if failed == 0 {
		return ptr(0)
	}
	rate, step := float64(failed)/float64(runs), 1
	for _, cut := range HeatSteps {
		if rate+eps >= cut {
			step++
		}
	}
	return ptr(step)
}

// heatmap is a row per repo of rows, in their order, then one for the
// kind's other repos when it has any; a cell per week of p, the first gap
// weeks before the run data. Weeks as BuildStats.Weekly buckets them, by a
// run's end, else its start, so a column holds the runs the kind's weekly
// rate is over; a run loaded for the period but ending outside it counts
// in no week.
func heatmap(p Period, kr kindRuns, rows []RepoHotspot, gap int) []HeatRow {
	cells := map[string][]HeatCell{}
	out := make([]HeatRow, len(rows))
	for i, h := range rows {
		out[i] = HeatRow{Repo: h.Repo, Weeks: make([]HeatCell, len(p.Weeks))}
		cells[h.Repo] = out[i].Weeks
	}
	other := HeatRow{Other: true, Weeks: make([]HeatCell, len(p.Weeks))}
	hasOther := false
	runs := make([]int, len(kr.runs))
	for i := range runs {
		runs[i] = i
	}
	for w, week := range bucket(p, runs, func(i int) *time.Time { return runAt(kr.runs[i]) }) {
		for _, i := range week {
			v := kr.verdicts[i]
			if v == VerdictNone {
				continue
			}
			row, ok := cells[kr.repos[i]]
			if !ok {
				row, hasOther = other.Weeks, true
			}
			row[w].Runs++
			if v == VerdictFailure {
				row[w].Failed++
			}
		}
	}
	if hasOther {
		out = append(out, other)
	}
	for _, row := range out {
		for w := range row.Weeks {
			c := &row.Weeks[w]
			c.Rate, c.Step = ratio(c.Failed, c.Runs), heatStep(c.Runs, c.Failed)
			c.NoData = w < gap && c.Runs == 0
		}
	}
	return out
}
