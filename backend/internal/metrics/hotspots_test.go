package metrics

import (
	"fmt"
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
	if h.MinRuns != HotspotMinRuns || h.SlowFactor != SlowFactor || h.FlakyFactor != FlakyFactor || h.OutlierFactor != OutlierFactor ||
		h.OutlierMinS != OutlierMinS {
		t.Errorf("thresholds = %+v", h)
	}
	k := kindOf(h, KindMain)
	expectNear(t, "team p50", k.Duration.P50, 100)
	// 7 broken of 32 counted runs: ABORTED is no run.
	expectNear(t, "team failure rate", k.FailureRate, 7.0/32)

	// The stuck repo's runs are outliers: no build time of it is left.
	if len(k.Durations) != 2 {
		t.Fatalf("durations = %+v", k.Durations)
	}
	slow, quick := k.Durations[0], k.Durations[1]
	if slow.Repo != "kasse/slow" || quick.Repo != "kasse/quick" {
		t.Errorf("order = %+v", k.Durations)
	}
	expectNear(t, "slow factor", slow.Factor, 10)
	if !slow.Slow || quick.Slow {
		t.Errorf("slow flags = %v %v", slow.Slow, quick.Slow)
	}

	// By wasted CI time; the quick repo broke nothing, the stuck one's
	// failures count but not their time.
	if len(k.Failing) != 2 || k.Failing[0].Repo != "kasse/slow" || k.Failing[1].Repo != "kasse/stuck" {
		t.Fatalf("failing = %+v", k.Failing)
	}
	slowF, stuckF := k.Failing[0], k.Failing[1]
	if stuckF.WastedSeconds != 0 || stuckF.Failures != 2 || slowF.WastedSeconds != 5000 || slowF.Runs != 10 || slowF.Failures != 5 {
		t.Errorf("failing = %+v", k.Failing)
	}
	expectNear(t, "slow repo rate", slowF.FailureRate, 0.5)
	if !slowF.Flaky || stuckF.Flaky {
		t.Errorf("flaky flags = %v %v", slowF.Flaky, stuckF.Flaky)
	}

	// The team's median is 100 s: 6× that is under the hour, the hour is
	// the threshold. The stuck runs are 50 times the team's median.
	expectNear(t, "outlier threshold", k.OutlierThresholdS, OutlierMinS)
	if len(h.Outliers) != 2 || h.Outliers[0].Repo != "kasse/stuck" || h.Outliers[0].Kind != KindMain {
		t.Fatalf("outliers = %+v", h.Outliers)
	}
	expectNear(t, "outlier factor", h.Outliers[0].Factor, 50)
}

// A repo slower than the rest but with too few runs for a median of its
// own keeps its time: under the hour no run is an outlier. Nor does a
// median of 0 turn outliers off.
func TestShortRunsAreNoOutliers(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/lib", 40, 120, 0, "")
		ds.Runs = append(ds.Runs, runs("kasse/mono", 4, 900, 0, "")...)
	})
	b := build(ds, p4(), filter{})
	if k := b.Kinds[0]; k.OutlierRuns != 0 || k.CISeconds != 40*120+4*900 {
		t.Errorf("kind = %+v", k)
	}
	cached := data(func(ds *dataset.Dataset) {
		ds.Runs = runs("kasse/lib", 20, 0, 0, "")
		ds.Runs = append(ds.Runs, runs("kasse/stuck", 1, 100000, 0, "")...)
	})
	cb := build(cached, p4(), filter{})
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
	b := build(ds, p4(), filter{})
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
	if len(h.Durations) != 2 {
		t.Errorf("outliers as durations: %+v", h.Durations)
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
	b := build(ds, p4(), filter{})
	k := b.Kinds[0]
	if k.OutlierRuns != 1 || k.OutlierSeconds != 50000 || k.CISeconds != 40*100+9*5000 {
		t.Errorf("kind = %+v", k)
	}
	o := b.Hotspots.Outliers
	if len(o) != 1 || o[0].RunID != "stuck" {
		t.Fatalf("outliers = %+v", o)
	}
	expectNear(t, "factor over the team's median", o[0].Factor, 500)
	mono := kindOf(b.Hotspots, KindMain).Durations[0]
	if mono.Repo != "kasse/mono" || mono.Runs != 10 || mono.Duration.N != 9 || !mono.Slow {
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
	if k := build(ds, p4(), filter{}).Kinds[0]; k.OutlierRuns != 0 || k.CISeconds != 40*100+4*7200 {
		t.Errorf("kind = %+v", k)
	}
}

func TestHotspotsNeedEnoughRuns(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = append(runs("kasse/a", 5, 100, 0, ""), runs("kasse/b", 1, 99999, 1, "FAILURE")...)
	})
	b := build(ds, p4(), filter{})
	k := kindOf(b.Hotspots, KindMain)
	// Flags need HotspotMinRuns; the outlier cut does not: a kind of a few
	// runs, one stuck for a day, still loses that day.
	expectNear(t, "outlier threshold", k.OutlierThresholdS, OutlierMinS)
	if b.Kinds[0].OutlierRuns != 1 {
		t.Errorf("kind = %+v", b.Kinds[0])
	}
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
			ds.Runs = append(ds.Runs, runs(fmt.Sprintf("kasse/few-%d", i), 2, 500, 2, "FAILURE")...)
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
		ds.Runs = append(ds.Runs, runs("kasse/b", 2, 3000, 2, "ABORTED")...)
		ds.Runs = append(ds.Runs, runs("kasse/c", 1, 100, 1, "NOT_BUILT")...)
	})
	b := build(ds, p4(), filter{})
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
	b := build(ds, p4(), filter{})
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
	none := build(data(func(ds *dataset.Dataset) { ds.Runs = runs("kasse/a", 3, 100, 0, "") }), p4(), filter{})
	if w := none.Kinds[0].Waiting; w.N != 0 || w.P50 != nil {
		t.Errorf("waiting without agent times = %+v", w)
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
				r.Repo, r.PipelineName = s("kasse/a"), "kasse-a/master"
				r.RunID, r.DurationS = fmt.Sprint(1000+i), n(int64(10000+i))
			}))
		}
	})
	o := build(ds, p4(), filter{}).Hotspots.Outliers
	if len(o) != outlierTop || o[0].DurationS != 10011 || o[9].DurationS != 10002 {
		t.Errorf("outliers = %+v", o)
	}
}
