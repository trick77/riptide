package metrics

import (
	"fmt"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// weekRuns are count main runs of repo finished at, the first red of them
// with status, each lasting d seconds.
func weekRuns(repo string, at time.Time, count, red int, status string, d int64) []dataset.Run {
	var out []dataset.Run
	for i := range count {
		out = append(out, run(func(r *dataset.Run) {
			r.Repo, r.PipelineName, r.RunID = s(repo), repo+"/master", fmt.Sprint(at.Unix(), i)
			r.StartedAt, r.FinishedAt, r.DurationS = ptr(at.Add(-time.Minute)), ptr(at), n(d)
			if i < red {
				r.Status = s(status)
			}
		}))
	}
	return out
}

func heatRowOf(k KindHotspots, repo string) HeatRow {
	for _, r := range k.Heatmap {
		if r.Repo == repo {
			return r
		}
	}
	return HeatRow{Repo: "missing"}
}

func expectCell(t *testing.T, what string, c HeatCell, runs, failed int, step *int) {
	t.Helper()
	if c.Runs != runs || c.Failed != failed || (c.Step == nil) != (step == nil) || (step != nil && *c.Step != *step) {
		t.Errorf("%s = %d/%d step %v, want %d/%d step %v", what, c.Failed, c.Runs, showInt(c.Step), failed, runs, showInt(step))
	}
	if runs == 0 {
		expectNil(t, what+" rate", c.Rate)
	} else {
		expectNear(t, what+" rate", c.Rate, float64(failed)/float64(runs))
	}
}

func showInt(v *int) any {
	if v == nil {
		return "null"
	}
	return *v
}

func TestHeatmap(t *testing.T) {
	p := p4()
	mid := func(w int) time.Time { return p.Weeks[w].Start.Add(50 * time.Hour) }
	ds := data(func(ds *dataset.Dataset) {
		// kasse/a: the cut points exactly, and 2 runs too few for a class
		ds.Runs = append(ds.Runs, weekRuns("kasse/a", mid(3), 9, 1, "FAILURE", 100)...)
		// a run of an unseen commit, named by its pipeline: kasse/a's row
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.Repo, r.PipelineName, r.RunID = nil, "a/master", "nil-repo"
			r.StartedAt, r.FinishedAt, r.DurationS = ptr(mid(3).Add(-time.Minute)), ptr(mid(3)), n(100)
		}))
		ds.Runs = append(ds.Runs, weekRuns("kasse/a", mid(2), 5, 1, "UNSTABLE", 100)...)
		ds.Runs = append(ds.Runs, weekRuns("kasse/a", mid(1), 5, 2, "Failed", 100)...)
		ds.Runs = append(ds.Runs, weekRuns("kasse/a", mid(0), 2, 2, "FAILURE", 100)...)
		// kasse/b: under 10 %, a stuck red run counted as a run; 3 green;
		// aborted runs are no runs; a week without any
		ds.Runs = append(ds.Runs, weekRuns("kasse/b", mid(3), 10, 0, "", 100)...)
		ds.Runs = append(ds.Runs, weekRuns("kasse/b", mid(3).Add(time.Hour), 1, 1, "FAILURE", 100000)...)
		ds.Runs = append(ds.Runs, weekRuns("kasse/b", mid(2), 3, 0, "", 100)...)
		ds.Runs = append(ds.Runs, weekRuns("kasse/b", mid(1), 5, 3, "ABORTED", 100)...)
		// kasse/c: Monday 00:30 in Zurich, still Sunday in UTC
		ds.Runs = append(ds.Runs, weekRuns("kasse/c", p.Weeks[3].Start.Add(30*time.Minute), 1, 0, "", 100)...)
	})
	b := buildAlone(ds, p, filter{})
	h := b.Hotspots
	if h.HeatMinRuns != HeatMinRuns || len(h.HeatSteps) != 3 {
		t.Errorf("heat constants = %d %v", h.HeatMinRuns, h.HeatSteps)
	}
	if b.Kinds[0].OutlierRuns != 1 {
		t.Fatalf("outliers = %d, want the stuck run", b.Kinds[0].OutlierRuns)
	}
	k := kindOf(h, KindMain)
	if len(k.Heatmap) != len(k.Repos) {
		t.Fatalf("rows = %d, repos %d", len(k.Heatmap), len(k.Repos))
	}
	for i, r := range k.Heatmap {
		if r.Repo != k.Repos[i].Repo || len(r.Weeks) != len(p.Weeks) {
			t.Errorf("row %d = %s with %d weeks, want %s", i, r.Repo, len(r.Weeks), k.Repos[i].Repo)
		}
	}
	a, bb, c := heatRowOf(k, "kasse/a"), heatRowOf(k, "kasse/b"), heatRowOf(k, "kasse/c")
	expectCell(t, "a KW 46", a.Weeks[3], 10, 1, ptr(2))
	expectCell(t, "a KW 45", a.Weeks[2], 5, 1, ptr(3))
	expectCell(t, "a KW 44", a.Weeks[1], 5, 2, ptr(4))
	expectCell(t, "a KW 43", a.Weeks[0], 2, 2, nil)
	expectCell(t, "b KW 46", bb.Weeks[3], 11, 1, ptr(1))
	expectCell(t, "b KW 45", bb.Weeks[2], 3, 0, ptr(0))
	expectCell(t, "b KW 44", bb.Weeks[1], 2, 0, nil)
	expectCell(t, "b KW 43", bb.Weeks[0], 0, 0, nil)
	expectCell(t, "c KW 46", c.Weeks[3], 1, 0, nil)
	expectCell(t, "c KW 45", c.Weeks[2], 0, 0, nil)
	// The cells count what the hotspot row counts.
	for _, r := range k.Repos {
		sum := 0
		for _, w := range heatRowOf(k, r.Repo).Weeks {
			sum += w.Runs
		}
		if sum != r.Runs {
			t.Errorf("%s: %d runs in the weeks, %d in the row", r.Repo, sum, r.Runs)
		}
	}
}

// The runs are loaded by when their event arrived, the weeks go by a run's
// end, else its start, as the kind's weekly rate has them: every column
// reconciles with it, and a run ended before the period is in no week.
func TestHeatmapWeeksAsWeekly(t *testing.T) {
	p := p4()
	monday := p.Weeks[2].Start
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = append(ds.Runs, weekRuns("kasse/a", p.Weeks[1].Start.Add(50*time.Hour), 4, 1, "FAILURE", 100)...)
		// started on Sunday, ended on Monday: the Monday's week
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.RunID, r.Status = "straddle", s("FAILURE")
			r.StartedAt, r.FinishedAt, r.DurationS = ptr(monday.Add(-20*time.Minute)), ptr(monday.Add(10*time.Minute)), n(1800)
		}))
		ds.Runs = append(ds.Runs, weekRuns("kasse/kasse-api", monday.Add(time.Hour), 3, 0, "", 100)...)
		// still running when the period's data was read: its start
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.RunID, r.FinishedAt = "open", nil
			r.StartedAt = ptr(p.Weeks[3].Start.Add(time.Hour))
		}))
		// its event arrived in the period, it ended before: no week
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.RunID, r.Status = "early", s("FAILURE")
			r.StartedAt, r.FinishedAt = ptr(p.From.Add(-2*time.Hour)), ptr(p.From.Add(-time.Hour))
		}))
	})
	b := buildAlone(ds, p, filter{})
	k := kindOf(b.Hotspots, KindMain)
	api := heatRowOf(k, "kasse/kasse-api")
	expectCell(t, "straddle week", api.Weeks[2], 4, 1, ptr(3))
	expectCell(t, "week before", api.Weeks[1], 0, 0, nil)
	expectCell(t, "open run", api.Weeks[3], 1, 0, nil)
	for w := range p.Weeks {
		runs, failed := 0, 0
		for _, r := range k.Heatmap {
			runs, failed = runs+r.Weeks[w].Runs, failed+r.Weeks[w].Failed
		}
		weekly := b.Kinds[0].Weekly[w]
		if runs == 0 {
			expectNil(t, fmt.Sprint("weekly ", w), weekly)
			continue
		}
		expectNear(t, fmt.Sprint("weekly ", w), weekly, 1-float64(failed)/float64(runs))
	}
	// The previous period, computed only to compare with, draws nothing.
	prev := build(ds, p, filter{}, &baseline{repos: newRepoNamer(ds.Runs), previous: true})
	if h := kindOf(prev.Hotspots, KindMain).Heatmap; h == nil || len(h) != 0 {
		t.Errorf("previous period heatmap = %v", h)
	}
}

// Past the hotspots' cut, the kind's other repos share one row: an outage
// hitting only them still paints its column, and every column holds all
// of the kind's runs.
func TestHeatmapOtherRepos(t *testing.T) {
	p := p4()
	mid := p.Weeks[2].Start.Add(50 * time.Hour)
	ds := data(func(ds *dataset.Dataset) {
		for i := range 10 {
			// the two wasting the least CI time fall past the top 8, all
			// red in week 2
			count, red, d := 20-i, 1, int64(1000)
			if i >= 8 {
				count, red, d = 3, 3, 100
			}
			ds.Runs = append(ds.Runs, weekRuns(fmt.Sprintf("kasse/r%d", i), mid, count, red, "FAILURE", d)...)
		}
		ds.Meta.BuildDataFrom = ptr(p.Weeks[2].Start)
	})
	b := buildAlone(ds, p, filter{})
	k := kindOf(b.Hotspots, KindMain)
	if len(k.Repos) != 8 || len(k.Heatmap) != 9 {
		t.Fatalf("repos %d, rows %d", len(k.Repos), len(k.Heatmap))
	}
	other := k.Heatmap[8]
	if !other.Other || other.Repo != "" {
		t.Errorf("last row = %+v", other)
	}
	expectCell(t, "other repos", other.Weeks[2], 6, 6, ptr(4))
	// Weeks before the run data are unknown, not empty; a week with runs
	// never is.
	if b.GapWeeks != 2 || !other.Weeks[0].NoData || !other.Weeks[1].NoData || other.Weeks[2].NoData || other.Weeks[3].NoData {
		t.Errorf("gap %d, no data %+v", b.GapWeeks, other.Weeks)
	}
	runs, failed := 0, 0
	for _, r := range k.Heatmap {
		runs, failed = runs+r.Weeks[2].Runs, failed+r.Weeks[2].Failed
	}
	expectNear(t, "week 2 against weekly", b.Kinds[0].Weekly[2], 1-float64(failed)/float64(runs))

	// The legend's classes, from the same cut points; the response gets a
	// copy of them.
	c := b.Hotspots.HeatClasses
	if len(c) != 5 || c[0].From != nil || c[0].To != nil || c[1].From != nil || *c[1].To != 0.1 ||
		*c[2].From != 0.1 || *c[2].To != 0.2 || *c[4].From != 0.4 || c[4].To != nil {
		t.Errorf("classes = %+v", c)
	}
	b.Hotspots.HeatSteps[0] = 1
	if HeatSteps[0] != 0.1 {
		t.Error("the response aliases HeatSteps")
	}
}

func TestHeatStep(t *testing.T) {
	for _, c := range []struct {
		runs, failed int
		want         *int
	}{
		{0, 0, nil}, {2, 1, nil}, {3, 0, ptr(0)}, {30, 2, ptr(1)}, {10, 1, ptr(2)}, {20, 3, ptr(2)},
		{5, 1, ptr(3)}, {40, 15, ptr(3)}, {5, 2, ptr(4)}, {3, 3, ptr(4)},
	} {
		got := heatStep(c.runs, c.failed)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%d of %d: step %v, want %v", c.failed, c.runs, showInt(got), showInt(c.want))
		}
	}
}
