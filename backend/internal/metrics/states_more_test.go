package metrics

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

func prodDeploy(app, phase string, days float64) dataset.Deploy {
	return deploy(func(d *dataset.Deploy) { d.App, d.Phase, d.At = app, phase, endAgo(days) })
}

func TestDeployFailing(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Deploys = []dataset.Deploy{
			// kasse-prod: failed since Wednesday, nothing since.
			prodDeploy("kasse-prod", "Succeeded", 10), prodDeploy("kasse-prod", "Failed", 5), prodDeploy("kasse-prod", "Error", 4),
			// Infrastructure never judged.
			prodDeploy("monitoring-prod", "Failed", 9),
		}
		d.Deploys[3].Infra = true
		// lager-prod: three runs of failures, each followed by a success.
		for i := range 3 {
			at := 20 - float64(i)*5
			d.Deploys = append(d.Deploys, prodDeploy("lager-prod", "Failed", at), prodDeploy("lager-prod", "Failed", at-0.01),
				prodDeploy("lager-prod", "Succeeded", at-0.5))
		}
	})
	a := assessStates(t, ds, nil, History{})
	x := stateSignal(a, StateDeployFailing)
	if x == nil || x.State.Count != 1 || x.State.Units[0].Name != "kasse-prod" || x.State.Units[0].Value != 3 ||
		*x.State.Units[0].Count != 2 || x.Weight != tierNow {
		t.Errorf("failing = %+v, want kasse-prod on a failure 3 working days, 2 attempts, held up now", x)
	}
	y := stateSignal(a, StateDeployFailures)
	if y == nil || y.Tone != ToneHint || y.State.Units[0].Name != "lager-prod" || y.State.Units[0].Value != 3 {
		t.Errorf("repeated = %+v, want lager-prod 3 runs of failures, a hint", y)
	}
}

func TestFirstReviewWait(t *testing.T) {
	waiting := func(id int64, days float64, mod ...func(*dataset.OpenPR)) dataset.OpenPR {
		return openPR(append([]func(*dataset.OpenPR){func(o *dataset.OpenPR) {
			o.PRID, o.OpenedAt, o.ReadySince, o.LastEventAt = id, endAgo(days), endAgoP(days), endAgo(days)
			o.ReadyAt = o.ReadySince
		}}, mod...)...)
	}
	ds := stateData(func(d *dataset.Dataset) {
		d.OpenPRs = []dataset.OpenPR{
			waiting(1, 7), waiting(2, 6), waiting(3, 5),
			// Touched, a draft, a bot's: none waits for a first review.
			waiting(4, 9, func(o *dataset.OpenPR) { o.Touched = true }),
			waiting(5, 9, func(o *dataset.OpenPR) { o.Draft = true }),
			waiting(6, 9, func(o *dataset.OpenPR) { o.Bot = true }),
		}
	})
	x := stateSignal(assessStates(t, ds, nil, History{}), StateFirstReviewWait)
	if x == nil || x.State.Units[0].Name != "kasse/kasse-api" || *x.State.Units[0].Count != 3 || x.State.Units[0].Value != 5 {
		t.Fatalf("first review = %+v, want kasse-api with 3 PRs, the oldest 5 working days", x)
	}
	// Since the third waiting PR had waited past 2 working days (the youngest,
	// ready 5 days before the end), found in the PR table by the repo; a
	// hint while that is under two sprints.
	u := x.State.Units[0]
	if u.Since == nil || !u.Since.Equal(afterWorkingDays(endAgo(5), 2, zurich)) || u.Measure == nil ||
		*u.Measure != MeasureOldest || u.Query == nil || *u.Query != "kasse/kasse-api" || x.Tone != ToneHint {
		t.Errorf("first review unit = %+v, tone %s", u, x.Tone)
	}
	// One PR ready for 30 days and two for a week: three waiting began a few
	// days ago, a hint, not red.
	young := stateData(func(d *dataset.Dataset) {
		active := func(o *dataset.OpenPR) { o.LastEventAt = endAgo(1) }
		d.OpenPRs = []dataset.OpenPR{waiting(1, 30, active), waiting(2, 6), waiting(3, 5)}
	})
	if x := stateSignal(assessStates(t, young, nil, History{}), StateFirstReviewWait); x == nil || x.Tone != ToneHint {
		t.Errorf("three waiting for days = %+v, want a hint", x)
	}
	// Three waiting for over a month, still active: it lasted, red.
	long := stateData(func(d *dataset.Dataset) {
		active := func(o *dataset.OpenPR) { o.LastEventAt = endAgo(1) }
		d.OpenPRs = []dataset.OpenPR{waiting(1, 40, active), waiting(2, 36, active), waiting(3, 33, active)}
	})
	if x := stateSignal(assessStates(t, long, nil, History{}), StateFirstReviewWait); x == nil || x.Tone != ToneAttention {
		t.Errorf("waiting two sprints = %+v, want red", x)
	}
	// One or two waiting and too few pickups: not judged, never calm.
	few := stateData(func(d *dataset.Dataset) { d.OpenPRs = []dataset.OpenPR{waiting(1, 7)} })
	a := assessStates(t, few, nil, History{})
	if x := stateSignal(a, StateFirstReviewWait); x != nil || a.Calm != 0 {
		t.Errorf("one waiting = %+v, calm %d, want no card and no calm repo on no sample", x, a.Calm)
	}
	// Two waiting are no finding.
	ds.OpenPRs = ds.OpenPRs[1:]
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateFirstReviewWait); x != nil {
		t.Errorf("two waiting PRs = %+v", x.State.Units)
	}
}

func mainRun(i int, failed bool, days float64, mod ...func(*dataset.Run)) dataset.Run {
	st := "SUCCESS"
	if failed {
		st = "FAILURE"
	}
	return run(append([]func(*dataset.Run){func(r *dataset.Run) {
		r.RunID, r.Status = strconv.Itoa(i), s(st)
		r.StartedAt, r.FinishedAt, r.OccurredAt = endAgoP(days), endAgoP(days-0.01), endAgo(days-0.01)
		r.CommitSHA = s(strconv.Itoa(i))
	}}, mod...)...)
}

func TestTrunkFailing(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		// Red from Monday to Thursday of the period's last week, 3 failures.
		d.Runs = []dataset.Run{mainRun(1, false, 9), mainRun(2, true, 7), mainRun(3, true, 6), mainRun(4, true, 5),
			mainRun(5, false, 4)}
		// lager-api: one red build, green at the next try: flaky, no episode.
		d.Runs = append(d.Runs, mainRun(10, true, 6, func(r *dataset.Run) { r.Repo, r.PipelineName = s("lager/lager-api"), "lager-api/master" }),
			mainRun(11, false, 5.9, func(r *dataset.Run) { r.Repo, r.PipelineName = s("lager/lager-api"), "lager-api/master" }))
	})
	x := stateSignal(assessStates(t, ds, nil, History{}), StateTrunkFailing)
	if x == nil || x.State.Count != 1 || x.State.Units[0].Name != "kasse/kasse-api" || *x.State.Units[0].Count != 3 ||
		x.State.Units[0].Value < 2.9 {
		t.Errorf("trunk = %+v, want kasse-api red 3 working days with 3 failures", x)
	}
}

func TestBranchFailing(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		for i := range 6 {
			d.Runs = append(d.Runs, mainRun(i, true, 8-float64(i)*0.5, func(r *dataset.Run) {
				r.PipelineName, r.Branch, r.BranchKind = "kasse-api/feature%2FKAS-7", "feature/KAS-7", dataset.BranchFeature
			}))
			// A bot's branch failing alike is no person's: StateBotLoop's.
			d.Runs = append(d.Runs, botRun(100+i, true))
		}
	})
	a := assessStates(t, ds, nil, History{})
	x := stateSignal(a, StateBranchFailing)
	if x == nil || x.State.Count != 1 || x.State.Units[0].Name != "kasse/kasse-api:feature/KAS-7" || x.State.Units[0].Value != 6 {
		t.Fatalf("branch = %+v, want the person's feature branch, 6 in a row", x)
	}
	// The runs table finds its builds by the pipeline, not by "<repo>:<branch>".
	if q := x.State.Units[0].Query; q == nil || *q != "kasse-api/feature%2FKAS-7" {
		t.Errorf("branch query = %v", q)
	}
}

func TestBuildStates(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		// kasse-api feature builds: 6 green of 20 minutes.
		for i := range 6 {
			d.Runs = append(d.Runs, mainRun(i, false, 10-float64(i), func(r *dataset.Run) {
				r.PipelineName, r.Branch, r.BranchKind = "kasse-api/feature%2Fx", "feature/x", dataset.BranchFeature
				r.DurationS = n(1200)
			}))
		}
		// Release builds: 2 of 5 failed.
		for i := range 5 {
			d.Runs = append(d.Runs, mainRun(50+i, i < 2, 9-float64(i), func(r *dataset.Run) { r.BuildType = s("release") }))
		}
	})
	a := assessStates(t, ds, nil, History{})
	if x := stateSignal(a, StateLongBuilds); x == nil || x.State.Units[0].Value != 20 || x.State.Units[0].Names[0] != KindFeature {
		t.Errorf("long builds = %+v, want kasse-api feature builds of 20 minutes", x)
	}
	if x := stateSignal(a, StateReleaseFailing); x == nil || *x.State.Units[0].Count != 2 || *x.State.Units[0].Of != 5 {
		t.Errorf("release failing = %+v, want 2 of 5", x)
	}
}

func TestFlakyBuilds(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		// 12 feature builds: 3 red ones each passed by a rerun on its commit.
		for i := range 3 {
			sha := "f" + strconv.Itoa(i)
			fb := func(r *dataset.Run) {
				r.PipelineName, r.Branch, r.BranchKind = "kasse-api/feature%2Fx", "feature/x", dataset.BranchFeature
				r.CommitSHA = s(sha)
			}
			d.Runs = append(d.Runs, mainRun(i*2, true, 10-float64(i), fb), mainRun(i*2+1, false, 9.9-float64(i), fb))
		}
		for i := range 6 {
			d.Runs = append(d.Runs, mainRun(20+i, false, 5-float64(i)*0.5, func(r *dataset.Run) {
				r.PipelineName, r.Branch, r.BranchKind = "kasse-api/feature%2Fx", "feature/x", dataset.BranchFeature
			}))
		}
	})
	x := stateSignal(assessStates(t, ds, nil, History{}), StateFlakyBuilds)
	if x == nil || x.State.Units[0].Value != 3 || *x.State.Units[0].Of != 3 {
		t.Errorf("flaky = %+v, want 3 of 3 failed builds flaky", x)
	}
}

func TestBuildsSilent(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		// 12 builds three to four weeks before the end, none since.
		for i := range 12 {
			d.Runs = append(d.Runs, mainRun(i, false, 20+float64(i)*0.5))
		}
		d.PRs = []dataset.PR{merged(1, 3), merged(2, 4), merged(3, 5)}
	})
	x := stateSignal(assessStates(t, ds, nil, History{}), StateBuildsSilent)
	if x == nil || x.Tone != ToneLimit || x.State.Units[0].Value != 3 {
		t.Errorf("silent = %+v, want kasse-api, 3 merges, no build: a limit", x)
	}
	// A build in the span: it reports.
	ds.Runs = append(ds.Runs, mainRun(99, false, 2))
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateBuildsSilent); x != nil {
		t.Errorf("a repo reporting = %+v", x.State.Units)
	}
}

func TestReviewStates(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		for i := range 12 {
			lines := int64(100)
			if i < 6 {
				lines = 900
			}
			d.PRs = append(d.PRs, merged(int64(i+1), 5, func(p *dataset.PR) {
				p.LinesAdded, p.LinesRemoved = n(lines), n(0)
				switch {
				case i < 3:
					// Approved five minutes after being ready, the first touch.
					p.ApprovedAt = ptr(p.ReadyAt.Add(5 * 60e9))
					p.FirstReviewAt, p.ApprovedOnFirstTouch = p.ApprovedAt, true
				case i >= 6:
					p.ApprovedAt = nil
				}
			}))
		}
		for i := range 3 {
			d.PRs = append(d.PRs, pr(func(p *dataset.PR) {
				p.PRID, p.State, p.MergedAt, p.TrunkAt = int64(50+i), "declined", nil, nil
				p.ReadyAt, p.ClosedAt = endAgoP(25), endAgoP(2)
			}))
		}
	})
	a := assessStates(t, ds, nil, History{})
	if x := stateSignal(a, StateBigPRs); x == nil || x.State.Units[0].Value != 0.5 {
		t.Errorf("big = %+v, want half of 12 sized PRs", x)
	}
	if x := stateSignal(a, StateFastApproval); x == nil || x.State.Units[0].Value != 3 {
		t.Errorf("fast = %+v, want 3 big PRs approved in minutes", x)
	}
	if x := stateSignal(a, StateUnapproved); x == nil || *x.State.Units[0].Count != 6 || *x.State.Units[0].Of != 12 {
		t.Errorf("unapproved = %+v, want 6 of 12", x)
	}
	if x := stateSignal(a, StateLateDecline); x == nil || x.State.Units[0].Value != 3 {
		t.Errorf("late decline = %+v, want 3", x)
	}
}

func branchLife(name string, first, last float64, mod ...func(*dataset.BranchLife)) dataset.BranchLife {
	b := dataset.BranchLife{Repo: "kasse/kasse-api", Project: "kasse", Branch: name, FirstAt: endAgo(first), LastAt: endAgo(last),
		Pushes: 3, PushTimes: []time.Time{endAgo(first), endAgo((first + last) / 2), endAgo(last)}}
	for _, m := range mod {
		m(&b)
	}
	return b
}

func TestBranchStates(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Branches = []dataset.BranchLife{
			// Worked on for 20 days, pushed yesterday, no PR.
			branchLife("feature/kas-9-big", 20, 1),
			// Silent for 20 to 40 days: three of them.
			branchLife("feature/kas-1", 50, 20), branchLife("feature/kas-2", 50, 30), branchLife("kas-3-old", 50, 40),
			// Long-lived by design, a bot's, one with a PR: none.
			branchLife("develop", 100, 1, func(b *dataset.BranchLife) { b.TargetAt = endAgoP(99) }),
			branchLife("release/1.x", 100, 1),
			branchLife("renovate/lib-1.x", 100, 1, func(b *dataset.BranchLife) { b.Bot = true }),
			branchLife("feature/kas-5", 30, 1, func(b *dataset.BranchLife) { b.PRAt = endAgoP(29) }),
			// Merged onto the trunk without a PR: done, neither in work nor
			// lying silent.
			branchLife("feature/kas-6", 30, 1, func(b *dataset.BranchLife) { b.MergedAt = endAgoP(0.5) }),
			branchLife("feature/kas-7", 50, 25, func(b *dataset.BranchLife) { b.MergedAt = endAgoP(24) }),
			// Pushed again only after the period's end: silent at it (its
			// later pushes are no part of a closed period).
			branchLife("feature/kas-8", 50, -2, func(b *dataset.BranchLife) {
				b.PushTimes = []time.Time{endAgo(50), endAgo(20), endAgo(-2)}
			}),
		}
		for i := range 5 {
			d.PRs = append(d.PRs, merged(int64(i+1), 3, func(p *dataset.PR) { p.FirstCommitAt = endAgoP(14) }))
		}
	})
	a := assessStates(t, ds, nil, History{})
	if x := stateSignal(a, StateBranchOpen); x == nil || *x.State.Units[0].Count != 1 || x.State.Units[0].Names[0] != "feature/kas-9-big" {
		t.Errorf("open = %+v, want the one branch worked on past a sprint", x)
	}
	if x := stateSignal(a, StateBranchSilent); x == nil || *x.State.Units[0].Count != 4 ||
		!slices.Contains(x.State.Units[0].Names, "feature/kas-8") {
		t.Errorf("silent = %+v, want 4 branches silent, the one pushed after the end among them", x)
	}
	// Worked on past a sprint since its first push and 14 days on: a hint
	// while that is under two sprints; the state holds since then.
	if x := stateSignal(a, StateBranchOpen); x == nil || x.Tone != ToneHint || x.State.Since == nil ||
		!x.State.Since.Equal(endAgo(20).AddDate(0, 0, 14)) {
		t.Errorf("open = %+v, want a hint since 14 days after the first push", x)
	}
	// A branch without a PR is a row of no facts table: no link.
	if x := stateSignal(a, StateBranchOpen); x != nil && (x.State.Facts != "" || x.State.Units[0].Query != nil) {
		t.Errorf("open links to %q %v", x.State.Facts, x.State.Units[0].Query)
	}
	if x := stateSignal(a, StateBranchLife); x == nil || x.State.Units[0].Value < 5 {
		t.Errorf("life = %+v, want branches living past 5 working days", x)
	}
}
