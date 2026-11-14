package metrics

import (
	"fmt"
	"testing"

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

func kindOf(h Hotspots, kind string) KindHotspots {
	for _, k := range h.Kinds {
		if k.Kind == kind {
			return k
		}
	}
	return KindHotspots{Kind: "missing"}
}

func TestHotspots(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		// slow and flaky: 10 runs of 1000 s, half of them failed
		ds.Runs = append(ds.Runs, runs("kasse/slow", 10, 1000, 5, "FAILURE")...)
		// the bulk: 20 quick green runs, 2 aborted ones that count for nothing
		ds.Runs = append(ds.Runs, runs("kasse/quick", 22, 100, 2, "ABORTED")...)
		// two runs stuck for long, both broken: too few to flag
		ds.Runs = append(ds.Runs, runs("kasse/stuck", 2, 5000, 2, "UNSTABLE")...)
	})
	h := build(ds, p4(), filter{}).Hotspots
	if h.MinRuns != HotspotMinRuns || h.SlowFactor != SlowFactor || h.FlakyFactor != FlakyFactor || h.OutlierFactor != OutlierFactor {
		t.Errorf("thresholds = %+v", h)
	}
	k := kindOf(h, KindMain)
	expectNear(t, "team p50", k.Duration.P50, 100)
	// 7 broken of 32 counted runs: ABORTED is no run.
	expectNear(t, "team failure rate", k.FailureRate, 7.0/32)

	if len(k.Durations) != 3 {
		t.Fatalf("durations = %+v", k.Durations)
	}
	stuck, slow, quick := k.Durations[0], k.Durations[1], k.Durations[2]
	if stuck.Repo != "kasse/stuck" || slow.Repo != "kasse/slow" || quick.Repo != "kasse/quick" {
		t.Errorf("order = %+v", k.Durations)
	}
	expectNear(t, "slow factor", slow.Factor, 10)
	if !slow.Slow || stuck.Slow || quick.Slow {
		t.Errorf("slow flags = %v %v %v", stuck.Slow, slow.Slow, quick.Slow)
	}

	// By wasted CI time; the quick repo broke nothing.
	if len(k.Failing) != 2 || k.Failing[0].Repo != "kasse/stuck" || k.Failing[1].Repo != "kasse/slow" {
		t.Fatalf("failing = %+v", k.Failing)
	}
	stuckF, slowF := k.Failing[0], k.Failing[1]
	if stuckF.WastedSeconds != 10000 || slowF.WastedSeconds != 5000 || slowF.Runs != 10 || slowF.Failures != 5 {
		t.Errorf("failing = %+v", k.Failing)
	}
	expectNear(t, "slow repo rate", slowF.FailureRate, 0.5)
	if !slowF.Flaky || stuckF.Flaky {
		t.Errorf("flaky flags = %v %v", slowF.Flaky, stuckF.Flaky)
	}

	// p90 of the team is 1000 s: the stuck runs are 5 times past it.
	expectNear(t, "outlier threshold", k.OutlierThresholdS, 3000)
	if len(h.Outliers) != 2 || h.Outliers[0].Repo != "kasse/stuck" || h.Outliers[0].Factor != 5 || h.Outliers[0].Kind != KindMain {
		t.Errorf("outliers = %+v", h.Outliers)
	}
}

func TestHotspotsNeedEnoughRuns(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = append(runs("kasse/a", 5, 100, 0, ""), runs("kasse/b", 1, 99999, 1, "FAILURE")...)
	})
	k := kindOf(build(ds, p4(), filter{}).Hotspots, KindMain)
	expectNil(t, "outlier threshold", k.OutlierThresholdS)
	if k.Durations[0].Slow || k.Failing[0].Flaky {
		t.Errorf("flagged on one run: %+v", k)
	}
	// A team median of 0 gives no factor.
	zero := data(func(ds *dataset.Dataset) { ds.Runs = runs("kasse/a", 3, 0, 0, "") })
	if f := kindOf(build(zero, p4(), filter{}).Hotspots, KindMain).Durations[0].Factor; f != nil {
		t.Errorf("factor over a zero median = %v", *f)
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
	h := build(ds, p4(), filter{}).Hotspots
	if kindOf(h, KindUnknown).Kind == "missing" || kindOf(h, KindRelease).Kind == "missing" {
		t.Fatalf("kinds = %+v", h.Kinds)
	}
	// Each kind against its own: a long release is no outlier.
	if len(h.Outliers) != 0 || kindOf(h, KindRelease).Durations[0].Slow {
		t.Errorf("mixed kinds: %+v", h)
	}
}

// A flagged repo stays on the list however many unflagged ones rank above
// it: eight short-lived repos with long runs, then a slow one with enough.
func TestFlaggedReposStayListed(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/bulk", 200, 100, 20, "FAILURE")
		for i := range 9 {
			ds.Runs = append(ds.Runs, runs(fmt.Sprintf("kasse/few-%d", i), 2, 5000, 2, "FAILURE")...)
		}
		ds.Runs = append(ds.Runs, runs("kasse/slow", 12, 300, 0, "")...)
		ds.Runs = append(ds.Runs, runs("kasse/flaky", 12, 10, 6, "FAILURE")...)
	})
	k := kindOf(build(ds, p4(), filter{}).Hotspots, KindMain)
	last := func(n int) int { return n - 1 }
	if len(k.Durations) != hotspotTop+1 || k.Durations[last(len(k.Durations))].Repo != "kasse/slow" ||
		!k.Durations[last(len(k.Durations))].Slow {
		t.Errorf("durations = %+v", k.Durations)
	}
	if len(k.Failing) != hotspotTop+1 || k.Failing[last(len(k.Failing))].Repo != "kasse/flaky" ||
		!k.Failing[last(len(k.Failing))].Flaky {
		t.Errorf("failing = %+v", k.Failing)
	}
}

// The kind table counts as the hotspots do: an aborted run is neither a
// failure nor a run, its time only CI time.
func TestBuildsLeaveAbortedOut(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/a", 4, 100, 1, "FAILURE")
		ds.Runs = append(ds.Runs, runs("kasse/b", 2, 5000, 2, "ABORTED")...)
		ds.Runs = append(ds.Runs, runs("kasse/c", 1, 100, 1, "NOT_BUILT")...)
	})
	b := build(ds, p4(), filter{})
	if len(b.Kinds) != 1 {
		t.Fatalf("kinds = %+v", b.Kinds)
	}
	// NOT_BUILT says nothing about the build either: runs = succeeded +
	// failed.
	k := b.Kinds[0]
	if k.Runs != 4 || k.Succeeded != 3 || k.Failed != 1 || k.NoVerdict != 3 || k.WastedSeconds != 100 || k.CISeconds != 10500 {
		t.Errorf("kind = %+v", k)
	}
	expectNear(t, "success rate", k.SuccessRate, 3.0/4)
	expectNear(t, "p50 without aborted", k.Duration.P50, 100)
	expectNear(t, "week", k.Weekly[3], 3.0/4)
	h := kindOf(b.Hotspots, KindMain)
	expectNear(t, "hotspot team p50", h.Duration.P50, 100)
	for _, d := range h.Durations {
		if d.Repo == "kasse/b" {
			t.Errorf("aborted runs as durations: %+v", d)
		}
	}
	// Tekton's words: Succeeded and Completed pass, Failed fails, None says
	// nothing.
	tekton := data(func(ds *dataset.Dataset) {
		for i, st := range []string{"Succeeded", "Completed", "Failed", "None"} {
			ds.Runs = append(ds.Runs, run(func(r *dataset.Run) { r.Source, r.RunID, r.Status = "tekton", fmt.Sprint(i), s(st) }))
		}
	})
	tk := build(tekton, p4(), filter{}).Kinds[0]
	if tk.Runs != 3 || tk.Succeeded != 2 || tk.Failed != 1 || tk.NoVerdict != 1 {
		t.Errorf("tekton = %+v", tk)
	}
	// Only aborted runs: the kind shows, without a rate.
	only := build(data(func(ds *dataset.Dataset) { ds.Runs = runs("kasse/b", 2, 100, 2, "ABORTED") }), p4(), filter{})
	if len(only.Kinds) != 1 || only.Kinds[0].Runs != 0 || only.Kinds[0].SuccessRate != nil {
		t.Errorf("only aborted = %+v", only.Kinds)
	}
}

// A team failing most of its runs still has a flaky repo: the one that
// always fails.
func TestFlakyAboveHalf(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = append(runs("kasse/a", 20, 100, 12, "FAILURE"), runs("kasse/b", 20, 100, 20, "FAILURE")...)
	})
	k := kindOf(build(ds, p4(), filter{}).Hotspots, KindMain)
	expectNear(t, "team rate", k.FailureRate, 0.8)
	if len(k.Failing) != 2 || k.Failing[0].Repo != "kasse/b" || !k.Failing[0].Flaky || k.Failing[1].Flaky {
		t.Errorf("failing = %+v", k.Failing)
	}
	if got := flakyThreshold(0.1); !near(got, 0.2) {
		t.Errorf("threshold at 10 %% = %v", got)
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
	k := kindOf(build(ds, p4(), filter{}).Hotspots, KindMain)
	runsOf := map[string]int{}
	for _, d := range k.Durations {
		runsOf[d.Repo] = d.Runs
	}
	if runsOf["kasse/kasse-api"] != 12 || runsOf["kasse-api"] != 0 || runsOf["shared"] != 1 || runsOf["nightly"] != 1 {
		t.Errorf("repos = %v", runsOf)
	}
}

// Outliers across kinds, longest first, ten at most.
func TestOutliersCapped(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/a", 200, 100, 0, "")
		for i := range 12 {
			ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
				r.RunID, r.DurationS = fmt.Sprint(1000+i), n(int64(10000+i))
			}))
		}
	})
	o := build(ds, p4(), filter{}).Hotspots.Outliers
	if len(o) != outlierTop || o[0].DurationS != 10011 || o[9].DurationS != 10002 {
		t.Errorf("outliers = %+v", o)
	}
}
