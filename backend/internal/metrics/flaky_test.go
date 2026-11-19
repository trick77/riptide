package metrics

import (
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// flakyRun is a run of kasse-api's master pipeline: id, commit, status,
// started days ago.
func flakyRun(id, sha, status string, days float64, mod ...func(*dataset.Run)) dataset.Run {
	return run(append([]func(*dataset.Run){func(r *dataset.Run) {
		r.RunID, r.CommitSHA, r.Status = id, s(sha), s(status)
		r.StartedAt, r.FinishedAt = agoP(days), ptr(ago(days).Add(10*time.Minute))
	}}, mod...)...)
}

// flakyData: red then green on one commit is flaky, whatever lies between
// without a verdict; a new commit, another pipeline, the same run ID sent
// twice, a green before the red and a run without a commit are not.
func flakyData() *dataset.Dataset {
	return data(func(ds *dataset.Dataset) {
		ds.Runs = []dataset.Run{
			flakyRun("1", "a", "FAILURE", 3), flakyRun("2", "a", "SUCCESS", 2.9),
			flakyRun("3", "b", "FAILURE", 3), flakyRun("4", "b", "UNSTABLE", 2.8), flakyRun("5", "b", "SUCCESS", 2.7),
			flakyRun("6", "c", "FAILURE", 3), flakyRun("7", "c", "ABORTED", 2.9), flakyRun("8", "c", "SUCCESS", 2.8),
			flakyRun("9", "d", "FAILURE", 3), flakyRun("10", "e", "SUCCESS", 2.9),
			flakyRun("11", "f", "FAILURE", 3), flakyRun("11", "f", "SUCCESS", 2.9),
			flakyRun("12", "g", "FAILURE", 3), flakyRun("13", "g", "SUCCESS", 2.9, func(r *dataset.Run) {
				r.PipelineName = "kasse-api-nightly/master"
			}),
			flakyRun("14", "h", "SUCCESS", 3), flakyRun("15", "h", "FAILURE", 2.9),
			flakyRun("16", "", "FAILURE", 3, func(r *dataset.Run) { r.CommitSHA = nil }),
			// stuck for ten hours, then rerun: flaky, its time the outlier's
			flakyRun("17", "i", "FAILURE", 3, func(r *dataset.Run) { r.DurationS = n(36000) }),
			flakyRun("18", "i", "SUCCESS", 2),
		}
	})
}

func TestFlakyRuns(t *testing.T) {
	ds := flakyData()
	b := buildAlone(ds, p4(), filter{})
	if len(b.Kinds) != 1 {
		t.Fatalf("kinds = %+v", b.Kinds)
	}
	k := b.Kinds[0]
	// 1, 3, 4, 6, 17 of 10 failed; 18 runs with a verdict, 17 with a commit.
	if k.FlakyRuns != 5 || k.Failed != 10 || k.Unjudged != 1 || k.FlakySeconds != 2400 || k.WastedSeconds != 5400 {
		t.Errorf("kind = %+v", k)
	}
	expectNear(t, "flaky rate", k.FlakyRate, 5.0/17)
	expectNear(t, "flaky of failed", k.FlakyOfFailed, 0.5)

	h := repoOf(kindOf(b.Hotspots, KindMain), "kasse/kasse-api")
	if h.FlakyRuns != 5 || h.FlakySeconds != 2400 {
		t.Errorf("repo = %+v", h)
	}

	// The flow splits the red time: flaky, the rest failed, the outlier
	// apart; still one total.
	got := map[string]float64{}
	sum := 0.0
	for _, o := range b.CIFlow.Outcomes {
		got[o.Outcome] = o.Seconds
		sum += o.Seconds
	}
	if got[FlowFlaky] != 2400 || got[FlowFailed] != 3000 || got[FlowOutlier] != 36000 || sum != b.CIFlow.TotalSeconds {
		t.Errorf("outcomes = %+v, total %v", b.CIFlow.Outcomes, b.CIFlow.TotalSeconds)
	}
	if i := slices.IndexFunc(b.CIFlow.Outcomes, func(o FlowOutcomeShare) bool { return o.Outcome == FlowFlaky }); i != 1 {
		t.Errorf("flaky at %d", i)
	}
}

// The facts mark the runs the build section counts, and find them by the
// mark in either language.
func TestFlakyFacts(t *testing.T) {
	f := ComputeFacts(flakyData(), all(), "")
	var marked []string
	for _, r := range f.Runs.Rows {
		if r.Flaky {
			marked = append(marked, r.RunID)
		}
	}
	slices.Sort(marked)
	if !slices.Equal(marked, []string{"1", "17", "3", "4", "6"}) {
		t.Errorf("marked = %v", marked)
	}
	if rows := ComputeFacts(flakyData(), all(), "flaky").Runs.Rows; len(rows) != 5 {
		t.Errorf("search = %d rows", len(rows))
	}
}

// A red run before the pipelines reported a build type (unknown) pairs
// with its green rerun after (main), and counts in its own kind; a main
// build never with a release. A green run without a start that ended
// before the red one did came first.
func TestFlakyAcrossKindsAndOrder(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = []dataset.Run{
			flakyRun("1", "a", "FAILURE", 12, func(r *dataset.Run) { r.BuildType = nil }),
			flakyRun("2", "a", "SUCCESS", 9),
			flakyRun("3", "b", "FAILURE", 3),
			flakyRun("4", "b", "SUCCESS", 3, func(r *dataset.Run) {
				r.StartedAt, r.FinishedAt = nil, ptr(ago(3).Add(5*time.Minute))
			}),
			// A release skips the tests the main build failed: no pair.
			flakyRun("5", "c", "FAILURE", 3),
			flakyRun("6", "c", "SUCCESS", 2, func(r *dataset.Run) { r.BuildType = s("release") }),
		}
	})
	b := buildAlone(ds, p4(), filter{})
	got := map[string][2]int{}
	for _, k := range b.Kinds {
		got[k.Kind] = [2]int{k.Failed, k.FlakyRuns}
	}
	if got[KindUnknown] != [2]int{1, 1} || got[KindMain] != [2]int{2, 0} {
		t.Errorf("kinds = %v", got)
	}
}

// A PR build of a merge the CI made itself has a new commit every build:
// its head on the same base pairs the rerun. The same head on a moved
// target is other code; a head without its base is never paired.
func TestFlakyOnHeadSHA(t *testing.T) {
	merge := func(head, base string) func(*dataset.Run) {
		return func(r *dataset.Run) {
			r.HeadSHA = s(head)
			if base != "" {
				r.BaseSHA = s(base)
			}
		}
	}
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = []dataset.Run{
			flakyRun("1", "merge1", "FAILURE", 3, merge("h", "t1")),
			flakyRun("2", "merge2", "SUCCESS", 2.9, merge("h", "t1")),
			// master fixed in between: green on another base, not flaky
			flakyRun("3", "merge3", "FAILURE", 2.8, merge("i", "t1")),
			flakyRun("4", "merge4", "SUCCESS", 2.7, merge("i", "t2")),
			// no base sent: no pair
			flakyRun("5", "merge5", "FAILURE", 2.6, merge("j", "")),
			flakyRun("6", "merge6", "SUCCESS", 2.5, merge("j", "")),
		}
	})
	k := buildAlone(ds, p4(), filter{}).Kinds[0]
	if k.Failed != 3 || k.FlakyRuns != 1 || k.Unjudged != 0 {
		t.Errorf("kind = %+v", k)
	}
	// The row shows the head, the code a developer knows, where it pairs on
	// it; without a base the merge commit it pairs on. A search finds the
	// base too.
	for _, r := range ComputeFacts(ds, all(), "").Runs.Rows {
		want := map[string]string{"1": "h", "5": "merge5"}[r.RunID]
		if want != "" && (r.ShownSHA == nil || *r.ShownSHA != want) {
			t.Errorf("run %s: shown sha = %v, want %s", r.RunID, r.ShownSHA, want)
		}
	}
	if rows := ComputeFacts(ds, all(), "t2").Runs.Rows; len(rows) != 1 || rows[0].RunID != "4" {
		t.Errorf("base search = %+v", rows)
	}
}

// The flaky rate compares over the runs with a commit, not every run with
// a verdict: 4 of 20 is too few.
func TestFlakyComparedOnJudgedRuns(t *testing.T) {
	params := Params{Period: p4(), Previous: PreviousPeriod(4, now, zurich)}
	month := 28 * 24 * time.Hour
	period := func(shift time.Duration) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			ds.Meta.BuildDataFrom, ds.Meta.BuildTypeFrom = agoP(60), nil
			for i := range 20 {
				status := "SUCCESS"
				if i == 0 {
					status = "FAILURE"
				}
				r := flakyRun(string(rune('a'+i)), "a", status, 3-0.01*float64(i))
				if i >= 4 {
					r.CommitSHA = nil
				}
				r.StartedAt, r.FinishedAt = ptr(r.StartedAt.Add(-shift)), ptr(r.FinishedAt.Add(-shift))
				ds.Runs = append(ds.Runs, r)
			}
		})
	}
	c := Compute(period(0), period(month), nil, params).Comparison
	if len(c.Build) != 1 || c.Build[0].Flaky.Reason == nil || *c.Build[0].Flaky.Reason != ReasonLowSample {
		t.Errorf("flaky = %+v", c.Build)
	}
}

// A kind compares its flaky rate with its previous period's: lower is
// better, in points.
func TestFlakyCompared(t *testing.T) {
	params := Params{Period: p4(), Previous: PreviousPeriod(4, now, zurich)}
	month := 28 * 24 * time.Hour
	period := func(shift time.Duration, red int) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			ds.Meta.BuildDataFrom, ds.Meta.BuildTypeFrom = agoP(60), nil
			for i := range 20 {
				status := "SUCCESS"
				if i < red {
					status = "FAILURE"
				}
				// Each commit built twice, red first where red.
				for j, st := range []string{status, "SUCCESS"} {
					r := flakyRun(string(rune('a'+i))+string(rune('0'+j)), string(rune('a'+i)), st, 3-0.1*float64(j))
					r.StartedAt, r.FinishedAt = ptr(r.StartedAt.Add(-shift)), ptr(r.FinishedAt.Add(-shift))
					ds.Runs = append(ds.Runs, r)
				}
			}
		})
	}
	c := Compute(period(0, 10), period(month, 0), nil, params).Comparison
	if len(c.Build) != 1 || c.Build[0].Flaky.Verdict != ChangeWorse {
		t.Errorf("flaky = %+v", c.Build)
	}
	expectNear(t, "flaky before", c.Build[0].Flaky.Prev, 0)
}
