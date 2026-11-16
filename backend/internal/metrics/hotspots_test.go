package metrics

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// runs is count main runs of one repo, each lasting d seconds, the first
// failures of them with status.
func runs(repo string, count int, d int64, failures int, status string) []dataset.Run {
	var out []dataset.Run
	for i := range count {
		out = append(out, run(func(r *dataset.Run) {
			r.Repo, r.PipelineName, r.RunID, r.DurationS = s(repo), repo+"/master", fmt.Sprint(i), n(d)
			if i < failures {
				r.Status = s(status)
			}
		}))
	}
	return out
}

// baselineOf is what Compute hands build: prev's runs, named as both
// periods' are.
func baselineOf(ds, prev *dataset.Dataset, incomplete bool) *baseline {
	names := newRepoNamer(slices.Concat(ds.Runs, prev.Runs))
	pb := build(prev, p4(), filter{}, &baseline{repos: names})
	return &baseline{repos: names, kinds: pb.repos, incomplete: incomplete}
}

// against is the build section of ds with prev as the previous period.
func against(ds, prev *dataset.Dataset) Build {
	return build(ds, p4(), filter{}, baselineOf(ds, prev, false))
}

// flagged counts a kind's slow and flaky repos.
func flagged(k KindHotspots) (slower, flakier int) {
	for _, h := range k.Repos {
		if h.Slow {
			slower++
		}
		if h.Flaky {
			flakier++
		}
	}
	return slower, flakier
}

func kindOf(h Hotspots, kind string) KindHotspots {
	for _, k := range h.Kinds {
		if k.Kind == kind {
			return k
		}
	}
	return KindHotspots{Kind: "missing"}
}

func repoOf(k KindHotspots, repo string) RepoHotspot {
	for _, h := range k.Repos {
		if h.Repo == repo {
			return h
		}
	}
	return RepoHotspot{Repo: "missing"}
}

// The chart's bubbles: the rows' CI time is at most the kind's, never all
// of it, and the repos it cannot draw are counted.
func TestHotspotsChart(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		for i := range 10 {
			ds.Runs = append(ds.Runs, runs(fmt.Sprintf("kasse/r%d", i), 10, 100, 0, "")...)
		}
		// only aborted runs: CI time, no row
		ds.Runs = append(ds.Runs, runs("kasse/aborted", 3, 100, 3, "ABORTED")...)
		ds.Runs = append(ds.Runs, runs("kasse/partial", 10, 100, 0, "")...)
	})
	prev := data(func(ds *dataset.Dataset) {
		ds.Runs = append(ds.Runs, runs("kasse/r0", 10, 100, 0, "")...)
		// no durations then: a failure rate to compare, no median
		for _, r := range runs("kasse/partial", 10, 100, 0, "") {
			r.DurationS = nil
			ds.Runs = append(ds.Runs, r)
		}
	})
	b := against(ds, prev)
	k := kindOf(b.Hotspots, KindMain)
	var ci, kind float64
	for _, h := range k.Repos {
		ci += h.CISeconds
	}
	for _, s := range b.Kinds {
		if s.Kind == KindMain {
			kind = s.CISeconds
		}
	}
	// 11 repos with a row, cut to 8; the aborted one has none.
	if len(k.Repos) != hotspotTop || kind != 11*1000+300 || ci > kind || ci != 8*1000 || k.Unplotted != 4 {
		t.Errorf("rows %d, ci %v of %v, unplotted %d", len(k.Repos), ci, kind, k.Unplotted)
	}
	r0, partial := repoOf(k, "kasse/r0"), repoOf(k, "kasse/partial")
	if !r0.Compared || !r0.PrevComplete {
		t.Errorf("r0 = %+v", r0)
	}
	if !partial.Compared || partial.PrevComplete || partial.FailureChange.Prev == nil {
		t.Errorf("partial = %+v", partial)
	}
}

func TestHotspots(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		// slower and flakier: 10 runs of 1000 s, half of them failed
		ds.Runs = append(ds.Runs, runs("kasse/slow", 10, 1000, 5, "FAILURE")...)
		// the bulk: 20 quick green runs, 2 aborted ones that count for nothing
		ds.Runs = append(ds.Runs, runs("kasse/quick", 22, 100, 2, "ABORTED")...)
		// two runs stuck for long, both broken: too few to flag
		ds.Runs = append(ds.Runs, runs("kasse/stuck", 2, 5000, 2, "UNSTABLE")...)
	})
	prev := data(func(ds *dataset.Dataset) {
		ds.Runs = append(ds.Runs, runs("kasse/slow", 10, 400, 0, "")...)
		ds.Runs = append(ds.Runs, runs("kasse/quick", 20, 100, 0, "")...)
		ds.Runs = append(ds.Runs, runs("kasse/stuck", 2, 100, 0, "")...)
	})
	b := against(ds, prev)
	h := b.Hotspots
	if h.MinRuns != HotspotMinRuns || h.SlowFactor != SlowFactor || h.FlakyFactor != FlakyFactor ||
		h.FlakyMinPoints != FlakyMinPoints || h.FlakyMinFailures != FlakyMinFailures ||
		h.OutlierFactor != OutlierFactor || h.OutlierMinS != OutlierMinS {
		t.Errorf("thresholds = %+v", h)
	}
	k := kindOf(h, KindMain)
	expectNear(t, "team p50", k.Duration.P50, 100)
	// 7 broken of 32 counted runs: ABORTED is no run.
	expectNear(t, "team failure rate", k.FailureRate, 7.0/32)

	// Each repo against its own previous period: the slow one took 400 s.
	slow, quick, stuck := repoOf(k, "kasse/slow"), repoOf(k, "kasse/quick"), repoOf(k, "kasse/stuck")
	expectNear(t, "slow factor", slow.Factor, 2.5)
	if !slow.Slow || slow.DurationChange.Verdict != ChangeWorse || quick.Slow || quick.DurationChange.Verdict != ChangeSame {
		t.Errorf("slow flags = %+v / %+v", slow, quick)
	}
	// From 0 % to half its runs; the stuck repo's failures count but not
	// their time, and two runs flag nothing.
	if !slow.Flaky || slow.WastedSeconds != 5000 || slow.Runs != 10 || slow.Failures != 5 || quick.Flaky {
		t.Errorf("flaky = %+v / %+v", slow, quick)
	}
	expectNear(t, "previous failure rate", slow.FailureChange.Prev, 0)
	// The previous position in the units of the current one: seconds, 0..1.
	expectNear(t, "previous median", slow.DurationChange.Prev, 400)
	// CI time as the kind counts it: aborted runs in, outliers out.
	if slow.CISeconds != 10000 || quick.CISeconds != 2200 || stuck.CISeconds != 0 {
		t.Errorf("ci seconds = %v / %v / %v", slow.CISeconds, quick.CISeconds, stuck.CISeconds)
	}
	// The stuck repo's every run an outlier: no median to draw it at.
	if !slow.Compared || !slow.PrevComplete || stuck.Compared || stuck.PrevComplete || k.Unplotted != 1 ||
		!slow.Plotted || stuck.Plotted {
		t.Errorf("compared = %+v / %+v, unplotted %d", slow, stuck, k.Unplotted)
	}
	if stuck.Flaky || stuck.Slow || stuck.WastedSeconds != 0 || stuck.Failures != 2 ||
		stuck.FailureChange.Reason == nil || *stuck.FailureChange.Reason != ReasonLowSample {
		t.Errorf("stuck = %+v", stuck)
	}
	// The flagged rows first, the flaky one by wasted time.
	if slower, flakier := flagged(k); k.Repos[0].Repo != "kasse/slow" || slower != 1 || flakier != 1 ||
		b.compared[KindMain] != 2 {
		t.Errorf("repos = %+v, compared %d", k.Repos, b.compared[KindMain])
	}

	// The team's median is 100 s: 6× that is under the hour, the hour is
	// the threshold. The stuck runs are 50 times the team's median.
	expectNear(t, "outlier threshold", k.OutlierThresholdS, OutlierMinS)
	if len(h.Outliers) != 2 || h.Outliers[0].Repo != "kasse/stuck" || h.Outliers[0].Kind != KindMain {
		t.Fatalf("outliers = %+v", h.Outliers)
	}
	expectNear(t, "outlier factor", h.Outliers[0].Factor, 50)
}

// A kind mixes toolchains: a UI repo always slower and redder than a
// Gradle one is no hotspot, a Gradle repo that doubled is.
func TestHotspotsAgainstOwnPeriod(t *testing.T) {
	period := func(gradle int64) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			ds.Runs = runs("kasse/web", 20, 900, 4, "FAILURE")
			ds.Runs = append(ds.Runs, runs("kasse/api", 20, gradle, 0, "")...)
		})
	}
	b := against(period(120), period(60))
	k := kindOf(b.Hotspots, KindMain)
	web, api := repoOf(k, "kasse/web"), repoOf(k, "kasse/api")
	if web.Slow || web.Flaky || !api.Slow || api.Flaky {
		t.Errorf("web = %+v, api = %+v", web, api)
	}
	expectNear(t, "api factor", api.Factor, 2)
	if slower, flakier := flagged(k); slower != 1 || flakier != 0 || b.compared[KindMain] != 2 {
		t.Errorf("counts %d %d %d", slower, flakier, b.compared[KindMain])
	}
	// A new repo has nothing to compare with; an incomplete previous period
	// compares nothing.
	fresh := data(func(ds *dataset.Dataset) { ds.Runs = runs("kasse/new", 20, 100, 0, "") })
	nb := against(fresh, period(60))
	if r := repoOf(kindOf(nb.Hotspots, KindMain), "kasse/new"); r.DurationChange.Reason == nil ||
		*r.DurationChange.Reason != ReasonNoValue || r.Factor != nil || nb.compared[KindMain] != 0 {
		t.Errorf("new repo = %+v", r)
	}
	ik := kindOf(build(period(120), p4(), filter{}, baselineOf(period(120), period(60), true)).Hotspots, KindMain)
	if r := repoOf(ik, "kasse/api"); r.Slow || r.DurationChange.Reason == nil || *r.DurationChange.Reason != ReasonIncomplete {
		t.Errorf("incomplete = %+v", r)
	}
}

// One red run after a clean period is no flaky repo, however few the runs:
// the floors of FlakyMinPoints and FlakyMinFailures.
func TestFlakyNeedsPoints(t *testing.T) {
	flaky := func(count, red int) bool {
		prev := data(func(ds *dataset.Dataset) { ds.Runs = runs("kasse/a", count, 100, 0, "") })
		cur := data(func(ds *dataset.Dataset) { ds.Runs = runs("kasse/a", count, 100, red, "FAILURE") })
		return repoOf(kindOf(against(cur, prev).Hotspots, KindMain), "kasse/a").Flaky
	}
	for _, c := range []struct {
		count, red int
		want       bool
	}{{40, 1, false}, {40, 2, true}, {10, 1, false}, {19, 1, false}, {10, 2, true}} {
		if got := flaky(c.count, c.red); got != c.want {
			t.Errorf("%d red of %d: flaky %v", c.red, c.count, got)
		}
	}
}

// A repo slower than the rest but with too few runs for a median of its
// own keeps its time: under the hour no run is an outlier. Nor does a
// median of 0 turn outliers off.
func TestShortRunsAreNoOutliers(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/lib", 40, 120, 0, "")
		ds.Runs = append(ds.Runs, runs("kasse/mono", 4, 900, 0, "")...)
	})
	b := build(ds, p4(), filter{}, nil)
	if k := b.Kinds[0]; k.OutlierRuns != 0 || k.CISeconds != 40*120+4*900 {
		t.Errorf("kind = %+v", k)
	}
	cached := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/lib", 20, 0, 0, "")
		ds.Runs = append(ds.Runs, runs("kasse/stuck", 1, 100000, 0, "")...)
	})
	cb := build(cached, p4(), filter{}, nil)
	if k := cb.Kinds[0]; k.OutlierRuns != 1 || k.CISeconds != 0 {
		t.Errorf("kind at median 0 = %+v", k)
	}
	expectNear(t, "threshold at median 0", kindOf(cb.Hotspots, KindMain).OutlierThresholdS, OutlierMinS)
	if o := cb.Hotspots.Outliers; len(o) != 1 || o[0].Factor != nil {
		t.Errorf("outliers at median 0 = %+v", o)
	}
}

// A Jenkins outage stalls a run of many repos at once. A quarter of the
// runs stuck pulls the p90 up to the stuck runs themselves; the median
// holds, and they leave every duration and CI time, shown on their own.
func TestStuckRunsLeaveBuildTime(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/a", 30, 100, 3, "FAILURE")
		for i := range 10 {
			ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
				repo := fmt.Sprintf("kasse/b%d", i)
				r.Repo, r.PipelineName, r.RunID, r.DurationS = s(repo), repo+"/master", "1", n(100000)
				switch i {
				case 0:
					r.Status = s("FAILURE")
				case 1:
					r.Status = s("ABORTED")
				}
			}))
		}
		// Not stuck: a run 5 times the median.
		ds.Runs = append(ds.Runs, runs("kasse/c", 1, 500, 0, "")...)
	})
	b := build(ds, p4(), filter{}, nil)
	k := b.Kinds[0]
	// Each stuck repo has a single run, too few for a median of its own:
	// measured against the team's 100 s, the aborted one too.
	if k.OutlierRuns != 10 || k.OutlierSeconds != 1000000 || k.CISeconds != 30*100+500 || k.WastedSeconds != 300 {
		t.Errorf("kind = %+v", k)
	}
	// Runs and the success rate keep them: 41 runs, 1 aborted.
	if k.Runs != 40 || k.Failed != 4 || k.NoVerdict != 1 {
		t.Errorf("counts = %+v", k)
	}
	expectNear(t, "p90 without outliers", k.Duration.P90, 100)
	h := kindOf(b.Hotspots, KindMain)
	expectNear(t, "hotspot p90", h.Duration.P90, 100)
	timed := 0
	for _, r := range h.Repos {
		if r.Duration.N > 0 {
			timed++
		}
	}
	if timed != 2 {
		t.Errorf("outliers as durations: %+v", h.Repos)
	}
	if len(b.Hotspots.Outliers) != 10 {
		t.Fatalf("outliers = %+v", b.Hotspots.Outliers)
	}
	expectNear(t, "outlier factor", b.Hotspots.Outliers[0].Factor, 1000)
}

// A repo always slower than the rest is no outlier: with runs enough for a
// median of its own, a run must pass that one's threshold too. Its stuck
// run is, and does not clear the slow flag: it counts as one of the runs.
func TestSlowRepoIsNoOutlier(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/lib", 40, 100, 0, "")
		ds.Runs = append(ds.Runs, runs("kasse/mono", 9, 5000, 0, "")...)
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.Repo, r.PipelineName, r.RunID, r.DurationS = s("kasse/mono"), "kasse-mono/master", "stuck", n(50000)
		}))
	})
	prev := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/lib", 40, 100, 0, "")
		ds.Runs = append(ds.Runs, runs("kasse/mono", 10, 2000, 0, "")...)
	})
	b := against(ds, prev)
	k := b.Kinds[0]
	if k.OutlierRuns != 1 || k.OutlierSeconds != 50000 || k.CISeconds != 40*100+9*5000 {
		t.Errorf("kind = %+v", k)
	}
	o := b.Hotspots.Outliers
	if len(o) != 1 || o[0].RunID != "stuck" {
		t.Fatalf("outliers = %+v", o)
	}
	expectNear(t, "factor over the team's median", o[0].Factor, 500)
	mono := repoOf(kindOf(b.Hotspots, KindMain), "kasse/mono")
	if mono.Runs != 10 || mono.Duration.N != 9 || !mono.Slow {
		t.Errorf("mono = %+v", mono)
	}
}

// A repo of a few runs, hours long each, keeps its time: three runs give
// it a median of its own.
func TestFewLongRunsAreNoOutliers(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/lib", 40, 100, 0, "")
		ds.Runs = append(ds.Runs, runs("kasse/nightly", 4, 7200, 0, "")...)
	})
	if k := build(ds, p4(), filter{}, nil).Kinds[0]; k.OutlierRuns != 0 || k.CISeconds != 40*100+4*7200 {
		t.Errorf("kind = %+v", k)
	}
}

func TestHotspotsNeedEnoughRuns(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = append(runs("kasse/a", 5, 100, 0, ""), runs("kasse/b", 1, 99999, 1, "FAILURE")...)
	})
	prev := data(func(ds *dataset.Dataset) {
		ds.Runs = append(runs("kasse/a", 5, 10, 0, ""), runs("kasse/b", 1, 10, 0, "")...)
	})
	b := against(ds, prev)
	k := kindOf(b.Hotspots, KindMain)
	// Flags need HotspotMinRuns; the outlier cut does not: a kind of a few
	// runs, one stuck for a day, still loses that day.
	expectNear(t, "outlier threshold", k.OutlierThresholdS, OutlierMinS)
	if b.Kinds[0].OutlierRuns != 1 {
		t.Errorf("kind = %+v", b.Kinds[0])
	}
	a, bb := repoOf(k, "kasse/a"), repoOf(k, "kasse/b")
	if a.Slow || bb.Flaky || a.Factor != nil || a.DurationChange.Reason == nil || *a.DurationChange.Reason != ReasonLowSample {
		t.Errorf("flagged on few runs: %+v / %+v", a, bb)
	}
	// A previous median of 0 gives no factor.
	zero := data(func(ds *dataset.Dataset) { ds.Runs = runs("kasse/a", 12, 0, 0, "") })
	twelve := data(func(ds *dataset.Dataset) { ds.Runs = runs("kasse/a", 12, 100, 0, "") })
	if r := repoOf(kindOf(against(twelve, zero).Hotspots, KindMain), "kasse/a"); r.Factor != nil || r.Slow {
		t.Errorf("factor over a zero median = %+v", r)
	}
}

// Kinds keep their own baseline, unknown included: a team that never sent
// a build_type has nothing else.
func TestHotspotsPerKind(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Meta.BuildTypeFrom = nil
		for _, r := range runs("kasse/a", 12, 100, 0, "") {
			r.BuildType = nil
			ds.Runs = append(ds.Runs, r)
		}
		for _, r := range runs("kasse/b", 12, 100, 0, "") {
			r.BuildType, r.DurationS = s("release"), n(10000)
			ds.Runs = append(ds.Runs, r)
		}
	})
	h := build(ds, p4(), filter{}, nil).Hotspots
	if kindOf(h, KindUnknown).Kind == "missing" || kindOf(h, KindRelease).Kind == "missing" {
		t.Fatalf("kinds = %+v", h.Kinds)
	}
	// Each kind against its own: a long release is no outlier.
	if len(h.Outliers) != 0 {
		t.Errorf("mixed kinds: %+v", h)
	}
}

// Flagged repos stay on the list past the top, however many: ten repos
// slower than before, an unflagged one with more wasted time below them.
func TestFlaggedReposStayListed(t *testing.T) {
	period := func(d int64) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			ds.Runs = runs("kasse/bulk", 200, 100, 20, "FAILURE")
			for i := range 10 {
				ds.Runs = append(ds.Runs, runs(fmt.Sprintf("kasse/slow-%d", i), 12, d, 0, "")...)
			}
		})
	}
	k := kindOf(against(period(300), period(100)).Hotspots, KindMain)
	if slower, _ := flagged(k); len(k.Repos) != 10 || slower != 10 {
		t.Errorf("repos = %+v", k.Repos)
	}
	for _, r := range k.Repos {
		if !r.Slow {
			t.Errorf("unflagged past the top: %+v", r)
		}
	}
}

// The kind table counts as the hotspots do: an aborted run is neither a
// failure nor a run, its time only CI time.
func TestBuildsLeaveAbortedOut(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/a", 4, 100, 1, "FAILURE")
		ds.Runs = append(ds.Runs, runs("kasse/b", 2, 3000, 2, "ABORTED")...)
		ds.Runs = append(ds.Runs, runs("kasse/c", 1, 100, 1, "NOT_BUILT")...)
	})
	b := build(ds, p4(), filter{}, nil)
	if len(b.Kinds) != 1 {
		t.Fatalf("kinds = %+v", b.Kinds)
	}
	// NOT_BUILT says nothing about the build either: runs = succeeded +
	// failed. Under an hour nothing is an outlier: the long aborted runs
	// stay CI time.
	k := b.Kinds[0]
	if k.Runs != 4 || k.Succeeded != 3 || k.Failed != 1 || k.NoVerdict != 3 || k.WastedSeconds != 100 || k.CISeconds != 6500 ||
		k.OutlierRuns != 0 {
		t.Errorf("kind = %+v", k)
	}
	expectNear(t, "success rate", k.SuccessRate, 3.0/4)
	expectNear(t, "p50 without aborted", k.Duration.P50, 100)
	expectNear(t, "week", k.Weekly[3], 3.0/4)
	h := kindOf(b.Hotspots, KindMain)
	expectNear(t, "hotspot team p50", h.Duration.P50, 100)
	if r := repoOf(h, "kasse/b"); r.Repo != "missing" {
		t.Errorf("aborted runs as a repo: %+v", r)
	}
	// Tekton's words: Succeeded and Completed pass, Failed fails, None says
	// nothing.
	tekton := data(func(ds *dataset.Dataset) {
		for i, st := range []string{"Succeeded", "Completed", "Failed", "None"} {
			ds.Runs = append(ds.Runs, run(func(r *dataset.Run) { r.Source, r.RunID, r.Status = "tekton", fmt.Sprint(i), s(st) }))
		}
	})
	tk := build(tekton, p4(), filter{}, nil).Kinds[0]
	if tk.Runs != 3 || tk.Succeeded != 2 || tk.Failed != 1 || tk.NoVerdict != 1 {
		t.Errorf("tekton = %+v", tk)
	}
	// Only aborted runs: the kind shows, without a rate.
	only := build(data(func(ds *dataset.Dataset) { ds.Runs = runs("kasse/b", 2, 100, 2, "ABORTED") }), p4(), filter{}, nil)
	if len(only.Kinds) != 1 || only.Kinds[0].Runs != 0 || only.Kinds[0].SuccessRate != nil {
		t.Errorf("only aborted = %+v", only.Kinds)
	}
}

// A kind's CI time splits by outcome without overlap: a stuck run counts
// as an outlier only, whether it failed, was aborted or succeeded. The
// parts sum to the kind's total, the links to the flow's.
func TestCIFlowSplitsWithoutOverlap(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/a", 30, 100, 3, "FAILURE")
		ds.Runs = append(ds.Runs, runs("kasse/c", 2, 200, 2, "ABORTED")...)
		for i, st := range []string{"FAILURE", "ABORTED", "SUCCESS"} {
			ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
				repo := fmt.Sprintf("kasse/b%d", i)
				r.Repo, r.PipelineName, r.RunID, r.DurationS, r.Status = s(repo), repo+"/master", "1", n(100000), s(st)
			}))
		}
		for _, r := range runs("kasse/f", 10, 50, 5, "FAILURE") {
			r.BranchKind, r.Branch = dataset.BranchFeature, "feature/x"
			ds.Runs = append(ds.Runs, r)
		}
		// Clock skew, a finish before the start: no time in any part.
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) { r.RunID, r.DurationS, r.Status = "skew", n(-100), s("FAILURE") }))
		// A kind with only such a run: listed, but no node and no link.
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.PipelineName, r.Branch, r.BranchKind, r.DurationS = "kasse-api/release%2F1", "release/1", dataset.BranchRelease, n(-300)
		}))
	})
	b := build(ds, p4(), filter{}, nil)
	if len(b.Kinds) != 3 || b.Kinds[2].Kind != KindOther || b.Kinds[2].total != 0 {
		t.Fatalf("kinds = %+v", b.Kinds)
	}
	main, feature := b.Kinds[0], b.Kinds[1]
	if main.succeeded != 2700 || main.WastedSeconds != 300 || main.noVerdict != 400 ||
		main.OutlierSeconds != 300000 || main.CISeconds != 3400 || main.total != 303400 {
		t.Errorf("main = %+v", main)
	}
	if feature.succeeded != 250 || feature.WastedSeconds != 250 || feature.noVerdict != 0 ||
		feature.OutlierSeconds != 0 || feature.total != 500 {
		t.Errorf("feature = %+v", feature)
	}
	for _, k := range b.Kinds {
		if k.succeeded+k.WastedSeconds+k.noVerdict != k.CISeconds || k.CISeconds+k.OutlierSeconds != k.total {
			t.Errorf("%s: parts do not sum to the total: %+v", k.Kind, k)
		}
	}
	// The skewed run is no duration anywhere: still a failed run, but in no
	// median, spread or outlier cut.
	if main.Failed != 5 || main.Duration.N != 30 || kindOf(b.Hotspots, KindMain).Duration.N != 30 {
		t.Errorf("skewed run as a duration: %+v", main)
	}
	if b.Kinds[2].Duration.N != 0 || b.Kinds[2].Runs != 1 {
		t.Errorf("other = %+v", b.Kinds[2])
	}

	f := b.CIFlow
	if f.TotalSeconds != 303900 {
		t.Errorf("flow total = %v", f.TotalSeconds)
	}
	want := []FlowOutcomeShare{
		{FlowSucceeded, 2950, 2950.0 / 303900}, {FlowFailed, 550, 550.0 / 303900},
		{FlowNoVerdict, 400, 400.0 / 303900}, {FlowOutlier, 300000, 300000.0 / 303900},
	}
	if !slices.Equal(f.Outcomes, want) {
		t.Errorf("outcomes = %+v", f.Outcomes)
	}
	if !slices.Equal(f.Kinds, []FlowKind{{KindMain, 303400, 303400.0 / 303900}, {KindFeature, 500, 500.0 / 303900}}) {
		t.Errorf("kind nodes = %+v", f.Kinds)
	}
	// Each side of the flow reconciles with the total.
	var kindSum, outcomeSum float64
	for _, k := range f.Kinds {
		kindSum += k.Seconds
	}
	for _, o := range f.Outcomes {
		outcomeSum += o.Seconds
	}
	if kindSum != f.TotalSeconds || outcomeSum != f.TotalSeconds {
		t.Errorf("nodes sum to %v and %v, not %v", kindSum, outcomeSum, f.TotalSeconds)
	}
	// Only the outcomes a kind has: feature neither aborted nor stuck.
	var got []string
	var sum float64
	ofKind := map[string]float64{}
	for _, l := range f.Links {
		got = append(got, l.Kind+">"+l.Outcome)
		sum += l.Seconds
		ofKind[l.Kind] += l.ShareOfKind
	}
	if !slices.Equal(got, []string{"main>succeeded", "main>failed", "main>no_verdict", "main>outlier",
		"feature>succeeded", "feature>failed"}) {
		t.Errorf("links = %v", got)
	}
	if sum != f.TotalSeconds {
		t.Errorf("links sum to %v, not %v", sum, f.TotalSeconds)
	}
	for kind, share := range ofKind {
		expectNear(t, kind+" shares", &share, 1)
	}
	if l := f.Links[5]; l.ShareOfKind != 0.5 || l.Share != 250.0/303900 {
		t.Errorf("feature failed = %+v", l)
	}

	// No runs, or none with a duration: an empty flow, never a division by 0.
	empty := build(data(func(ds *dataset.Dataset) {
		ds.Runs = []dataset.Run{run(func(r *dataset.Run) { r.DurationS = nil })}
	}), p4(), filter{}, nil).CIFlow
	if empty.TotalSeconds != 0 || len(empty.Outcomes) != 0 || len(empty.Links) != 0 {
		t.Errorf("empty flow = %+v", empty)
	}
}

// The wait for an agent and the build split only the runs that report when
// they got one: a run without it, or with one outside the run, has neither,
// never a wait of 0. Duration stays over all of them; outliers carry their
// wait.
func TestBuildSplitsAgentWait(t *testing.T) {
	start := ago(1)
	at := func(s float64) *time.Time { return ptr(start.Add(time.Duration(s * float64(time.Second)))) }
	timed := func(id string, total, agent float64, status string) dataset.Run {
		return run(func(r *dataset.Run) {
			r.RunID, r.Status, r.StartedAt, r.FinishedAt, r.DurationS = id, s(status), at(0), at(total), n(int64(total))
			if agent >= -1 {
				r.AgentStartedAt = at(agent)
			}
		})
	}
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = []dataset.Run{
			timed("1", 600, 60, "SUCCESS"),
			timed("2", 600, 120, "FAILURE"),
			timed("3", 600, -2, "SUCCESS"),      // no agent time reported
			timed("4", 600, -1, "SUCCESS"),      // agent before the start: skew
			timed("5", 600, 700, "SUCCESS"),     // agent after the end: skew
			timed("6", 600, 30, "ABORTED"),      // no verdict, no build time
			timed("7", 20000, 19000, "SUCCESS"), // stuck: an outlier
			timed("8", 20000, -2, "SUCCESS"),    // stuck, no agent time
		}
	})
	b := build(ds, p4(), filter{}, nil)
	k := b.Kinds[0]
	if k.Duration.N != 5 || k.Waiting.N != 2 || k.Building.N != 2 {
		t.Fatalf("n: duration %d, waiting %d, building %d", k.Duration.N, k.Waiting.N, k.Building.N)
	}
	expectNear(t, "wait p50", k.Waiting.P50, 90)
	expectNear(t, "build p50", k.Building.P50, 510)
	expectNear(t, "duration p50", k.Duration.P50, 600)
	if len(b.Hotspots.Outliers) != 2 {
		t.Fatalf("outliers = %+v", b.Hotspots.Outliers)
	}
	for _, o := range b.Hotspots.Outliers {
		switch o.RunID {
		case "7":
			expectNear(t, "outlier wait", o.WaitS, 19000)
		case "8":
			if o.WaitS != nil {
				t.Errorf("outlier without agent time has wait %v", *o.WaitS)
			}
		}
	}
	// No run reports one: both empty, nulls rather than 0.
	none := build(data(func(ds *dataset.Dataset) { ds.Runs = runs("kasse/a", 3, 100, 0, "") }), p4(), filter{}, nil)
	if w := none.Kinds[0].Waiting; w.N != 0 || w.P50 != nil {
		t.Errorf("waiting without agent times = %+v", w)
	}
}

// A repo failing most of its runs can still get flaky: when it starts
// failing them all.
func TestFlakyAboveHalf(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = append(runs("kasse/a", 20, 100, 12, "FAILURE"), runs("kasse/b", 20, 100, 20, "FAILURE")...)
	})
	prev := data(func(ds *dataset.Dataset) {
		ds.Runs = append(runs("kasse/a", 20, 100, 12, "FAILURE"), runs("kasse/b", 20, 100, 12, "FAILURE")...)
	})
	k := kindOf(against(ds, prev).Hotspots, KindMain)
	expectNear(t, "team rate", k.FailureRate, 0.8)
	if a, b := repoOf(k, "kasse/a"), repoOf(k, "kasse/b"); a.Flaky || !b.Flaky || k.Repos[0].Repo != "kasse/b" {
		t.Errorf("repos = %+v", k.Repos)
	}
	// The cap comes last: from 96 % red, all red still flags.
	for _, c := range []struct{ prev, want float64 }{
		{0.1, 0.2}, {0, FlakyMinPoints}, {0.6, 0.8}, {0.03, 0.08}, {0.96, 0.98},
	} {
		if got := flakyThreshold(c.prev); !near(got, c.want) {
			t.Errorf("threshold at %v = %v, want %v", c.prev, got, c.want)
		}
	}
}

// Runs of a commit Bitbucket never saw count with their repository, found
// by its name; a name in two projects stays apart.
func TestRepoOfUnseenCommits(t *testing.T) {
	unseen := func(pipeline string, n int) []dataset.Run {
		var out []dataset.Run
		for _, r := range runs("x", n, 100, 0, "") {
			r.Repo, r.PipelineName = nil, pipeline
			out = append(out, r)
		}
		return out
	}
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/kasse-api", 6, 100, 0, "")
		ds.Runs = append(ds.Runs, unseen("kasse-api/master", 6)...)
		ds.Runs = append(ds.Runs, runs("kasse/shared", 1, 100, 0, "")...)
		ds.Runs = append(ds.Runs, runs("portal/shared", 1, 100, 0, "")...)
		ds.Runs = append(ds.Runs, unseen("shared/master", 1)...)
		ds.Runs = append(ds.Runs, unseen("nightly", 1)...)
	})
	k := kindOf(build(ds, p4(), filter{}, nil).Hotspots, KindMain)
	runsOf := map[string]int{}
	for _, d := range k.Repos {
		runsOf[d.Repo] = d.Runs
	}
	if runsOf["kasse/kasse-api"] != 12 || runsOf["kasse-api"] != 0 || runsOf["shared"] != 1 || runsOf["nightly"] != 1 {
		t.Errorf("repos = %v", runsOf)
	}
	// The previous period's unseen runs find their repository by the names
	// of both periods: a commit seen only now still names last month's run.
	prev := data(func(ds *dataset.Dataset) { ds.Runs = unseen("kasse-api/master", 12) })
	pk := kindOf(against(ds, prev).Hotspots, KindMain)
	if r := repoOf(pk, "kasse/kasse-api"); r.DurationChange.Prev == nil {
		t.Errorf("unseen previous runs = %+v", r)
	}
}

func repoNames(rows []RepoHotspot) []string {
	var out []string
	for _, h := range rows {
		out = append(out, h.Repo)
	}
	return out
}

// A repo steady at 25 minutes is never slow against itself, and without a
// failure ranks below every repo that wasted CI time: the slowest list
// still names it, by median, without a threshold.
func TestSlowestRepos(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		for i := range 9 {
			ds.Runs = append(ds.Runs, runs(fmt.Sprintf("kasse/red-%d", i), 12, 100, 1, "FAILURE")...)
		}
		ds.Runs = append(ds.Runs, runs("kasse/steady", 12, 1500, 0, "")...)
	})
	k := kindOf(build(ds, p4(), filter{}, nil).Hotspots, KindMain)
	if r := repoOf(k, "kasse/steady"); r.Repo != "missing" {
		t.Errorf("steady repo not cut from the hotspots: %+v", repoNames(k.Repos))
	}
	if got := repoNames(k.Slowest); !slices.Equal(got,
		[]string{"kasse/steady", "kasse/red-0", "kasse/red-1", "kasse/red-2", "kasse/red-3"}) {
		t.Errorf("slowest = %v", got)
	}
	expectNear(t, "steady p50", k.Slowest[0].Duration.P50, 1500)

	// One stuck run is no slow repo: the median leaves it aside. Two runs
	// say nothing about a repo, however long.
	mixed := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/burst", 5, 100, 0, "")
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.Repo, r.PipelineName, r.RunID, r.DurationS = s("kasse/burst"), "kasse/burst/master", "stuck", n(50000)
		}))
		ds.Runs = append(ds.Runs, runs("kasse/mid", 5, 300, 0, "")...)
		ds.Runs = append(ds.Runs, runs("kasse/few", 2, 3000, 0, "")...)
		// three runs, one stuck: a median over two
		ds.Runs = append(ds.Runs, runs("kasse/two-left", 2, 3000, 0, "")...)
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.Repo, r.PipelineName, r.RunID, r.DurationS = s("kasse/two-left"), "kasse/two-left/master", "stuck", n(50000)
		}))
	})
	mk := kindOf(build(mixed, p4(), filter{}, nil).Hotspots, KindMain)
	if got := repoNames(mk.Slowest); !slices.Equal(got, []string{"kasse/mid", "kasse/burst"}) {
		t.Errorf("slowest with an outlier = %v", got)
	}
	if b := mk.Slowest[1]; b.Runs != 6 || b.Duration.N != 5 {
		t.Errorf("burst = %+v", b)
	}

	// Each kind its own list: long releases never push a main build out.
	kinds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/a", 12, 100, 0, "")
		for _, r := range runs("kasse/b", 12, 3000, 0, "") {
			r.BuildType = s("release")
			ds.Runs = append(ds.Runs, r)
		}
	})
	h := build(kinds, p4(), filter{}, nil).Hotspots
	if m, r := repoNames(kindOf(h, KindMain).Slowest), repoNames(kindOf(h, KindRelease).Slowest); !slices.Equal(m,
		[]string{"kasse/a"}) || !slices.Equal(r, []string{"kasse/b"}) {
		t.Errorf("slowest main %v, release %v", m, r)
	}
}

// Each repo splits its runs at the agent start as the kind does; a repo
// whose CI never reports it has neither, nulls rather than 0.
func TestRepoSplitsAgentWait(t *testing.T) {
	start := ago(1)
	at := func(s float64) *time.Time { return ptr(start.Add(time.Duration(s * float64(time.Second)))) }
	ds := data(func(ds *dataset.Dataset) {
		for i, agent := range []float64{60, 120, 90} {
			ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
				r.Repo, r.PipelineName, r.RunID = s("kasse/agent"), "kasse/agent/master", fmt.Sprint(i)
				r.StartedAt, r.FinishedAt, r.AgentStartedAt, r.DurationS = at(0), at(600), at(agent), n(600)
			}))
		}
		ds.Runs = append(ds.Runs, runs("kasse/plain", 3, 600, 0, "")...)
	})
	k := kindOf(build(ds, p4(), filter{}, nil).Hotspots, KindMain)
	a, p := repoOf(k, "kasse/agent"), repoOf(k, "kasse/plain")
	if a.Waiting.N != 3 || a.Building.N != 3 {
		t.Fatalf("agent = %+v", a)
	}
	expectNear(t, "repo wait p50", a.Waiting.P50, 90)
	expectNear(t, "repo build p50", a.Building.P50, 510)
	if p.Waiting.N != 0 || p.Waiting.P50 != nil || p.Building.P50 != nil {
		t.Errorf("plain = %+v", p)
	}
	// The slowest list carries the same rows.
	i := slices.IndexFunc(k.Slowest, func(h RepoHotspot) bool { return h.Repo == "kasse/agent" })
	if i < 0 || k.Slowest[i].Waiting.N != 3 || k.Slowest[i].Building.N != 3 {
		t.Errorf("slowest = %+v", k.Slowest)
	}
}

// Outliers across kinds, longest first, ten at most.
func TestOutliersCapped(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/a", 200, 100, 0, "")
		for i := range 12 {
			ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
				r.Repo, r.PipelineName = s("kasse/a"), "kasse-a/master"
				r.RunID, r.DurationS = fmt.Sprint(1000+i), n(int64(10000+i))
			}))
		}
	})
	o := build(ds, p4(), filter{}, nil).Hotspots.Outliers
	if len(o) != outlierTop || o[0].DurationS != 10011 || o[9].DurationS != 10002 {
		t.Errorf("outliers = %+v", o)
	}
}
