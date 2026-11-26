package metrics

import (
	"slices"
	"strconv"
	"testing"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
)

// assessBoth assesses ds against prev (both read: the states look into the
// previous period's rows too).
func assessBoth(t *testing.T, ds, prev *dataset.Dataset, th *config.Thresholds) Assessment {
	t.Helper()
	return Assess(ds, prev, Params{Period: stCur, Previous: stPrev, Thresholds: th}, History{})
}

// A repo only infrastructure Apps deploy is no team's delivery: its merged
// work lying before production fires nothing, nor is it untraceable.
func TestNotOnProdSkipsInfraRepos(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("monitoring-prod", 30, false), release("kasse-prod", 2, false)}
		d.Traces["ops/log-shipper"] = dataset.RepoTrace{DeployedSince: endAgoP(100),
			Releases: []dataset.TraceRelease{{Environment: "prod", At: endAgo(30)}}}
		d.PRs = []dataset.PR{merged(1, 20, func(p *dataset.PR) { p.Repo, p.Project = "ops/log-shipper", "ops" })}
	})
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateNotOnProd); x != nil {
		t.Errorf("an infrastructure repo judged: %+v", x.State.Units)
	}
	// The same repo released by a product's App is the team's.
	ds.Releases[0].App = "kasse-prod"
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateNotOnProd); x == nil || x.State.Units[0].Name != "ops/log-shipper" {
		t.Errorf("a product's repo = %+v, want it judged", x)
	}
}

// Declined after long work, whenever opened: a PR opened in the previous
// period and declined in this one counts; one a later PR from the same
// branch took on does not.
func TestLateDeclineAcrossWindows(t *testing.T) {
	ds := stateData()
	prev := &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps}
	for i := range 3 {
		prev.PRs = append(prev.PRs, pr(func(p *dataset.PR) {
			p.PRID, p.State, p.MergedAt, p.TrunkAt = int64(50+i), "declined", nil, nil
			p.OpenedAt, p.ReadyAt, p.ClosedAt = endAgoP(40), endAgoP(40), endAgoP(3)
			p.SourceRef = "feature/KAS-" + strconv.Itoa(50+i)
		}))
	}
	ds.PRs = []dataset.PR{merged(1, 3)}
	if x := stateSignal(assessBoth(t, ds, prev, nil), StateLateDecline); x == nil || x.State.Units[0].Value != 3 {
		t.Errorf("late decline = %+v, want the 3 PRs opened in the previous period", x)
	}
	// A new PR from the same branch carries one on: superseded.
	ds.PRs = append(ds.PRs, merged(2, 1, func(p *dataset.PR) {
		p.SourceRef, p.OpenedAt = "feature/KAS-50", endAgoP(2)
	}))
	if x := stateSignal(assessBoth(t, ds, prev, nil), StateLateDecline); x != nil {
		t.Errorf("a superseded PR counted: %+v", x.State.Units)
	}
}

// A repo whose feature and trunk builds are both long is one unit, named
// by its worse kind: never "2 von 2 Repos" for one repo.
func TestBuildStatesOneUnitPerRepo(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		for i := range 6 {
			d.Runs = append(d.Runs, mainRun(i, false, 10-float64(i), func(r *dataset.Run) { r.DurationS = n(1800) }),
				mainRun(100+i, false, 10-float64(i), func(r *dataset.Run) {
					r.PipelineName, r.Branch, r.BranchKind = "kasse-api/feature%2FKAS-1", "feature/KAS-1", dataset.BranchFeature
					r.DurationS = n(2400)
				}))
		}
	})
	x := stateSignal(assessStates(t, ds, nil, History{}), StateLongBuilds)
	if x == nil || x.State.Count != 1 || x.State.Judged != 1 || len(x.State.Units) != 1 ||
		!slices.Equal(x.State.Units[0].Names, []string{KindFeature}) || x.State.Units[0].Value != 40 {
		t.Errorf("long builds = %+v, want one unit, the feature builds' 40 minutes", x)
	}
}

// An App whose production deploy failed before the period, nothing since:
// still failing, since its first failure.
func TestDeployFailingBeforePeriod(t *testing.T) {
	ds := stateData()
	prev := &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps,
		Deploys: []dataset.Deploy{prodDeploy("kasse-prod", "Succeeded", 45), prodDeploy("kasse-prod", "Failed", 40)}}
	x := stateSignal(assessBoth(t, ds, prev, nil), StateDeployFailing)
	if x == nil || x.State.Units[0].Since == nil || !x.State.Units[0].Since.Equal(endAgo(40)) {
		t.Errorf("failing = %+v, want kasse-prod failing since 40 days back", x)
	}
}

// A repo whose trunk is develop (where its PRs go): its develop builds are
// its trunk's.
func TestTrunkFailingOnDevelop(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		dev := func(r *dataset.Run) {
			r.PipelineName, r.Branch, r.BranchKind = "kasse-api/develop", "develop", dataset.BranchKind("develop")
		}
		d.Runs = []dataset.Run{mainRun(1, false, 9, dev), mainRun(2, true, 7, dev), mainRun(3, true, 6, dev),
			mainRun(4, true, 5, dev), mainRun(5, false, 4, dev)}
		for i := range 3 {
			d.PRs = append(d.PRs, merged(int64(i+1), 3, func(p *dataset.PR) { p.TrunkBranch = s("develop") }))
		}
	})
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateTrunkFailing); x == nil || *x.State.Units[0].Count != 3 {
		t.Errorf("develop = %+v, want kasse-api's trunk failing 3 times", x)
	}
}

// A 26-week view reads its previous 26 weeks too; no state looks further
// back than StateLookbackWeeks: a PR merged 40 weeks ago and never shipped
// is no unit's oldest, the same in every view.
func TestStateLookbackBound(t *testing.T) {
	cur, prevP := ClosedPeriod(26, now, zurich), ClosedPrevious(26, now, zurich)
	ds := stateData(func(d *dataset.Dataset) {
		d.Meta.From, d.Meta.To = cur.From, cur.To
		d.Releases = []dataset.AppRelease{release("kasse-prod", 2, false)}
		d.PRs = []dataset.PR{merged(1, 20)}
	})
	prev := &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps, PRs: []dataset.PR{merged(2, 280)}}
	a := Assess(ds, prev, Params{Period: cur, Previous: prevP}, History{})
	if x := stateSignal(a, StateNotOnProd); x == nil || x.State.Units[0].Value != 20 || *x.State.Units[0].Count != 1 {
		t.Errorf("26-week view = %+v, want kasse-api's 20 days, the 40-week-old PR out of the look-back", x)
	}
}

// A covering state's worsening folds when the units of all covering red
// states name every driver; a hint never covers.
func TestFoldChangesUnion(t *testing.T) {
	card := func(key, tone string, units ...string) Signal {
		c := &StateCard{Key: key}
		for _, u := range units {
			c.Units = append(c.Units, StateUnit{Name: u})
		}
		return Signal{ID: SignalState + ":" + key, Kind: SignalState, Tone: tone, State: c}
	}
	change := Signal{ID: "change:review_time", Kind: SignalChange, Tone: ToneAttention, Metric: ptr("review_time"),
		Drivers: []Driver{{Repo: "kasse/kasse-api"}, {Repo: "lager/lager-api"}}}
	a := &assessor{out: []Signal{
		card(StateFirstReviewWait, ToneAttention, "kasse/kasse-api"),
		card(StateApprovedWaiting, ToneAttention, "lager/lager-api"), change,
		card(StateBranchFailing, ToneHint, "kasse/kasse-api:feature/x"),
		{ID: "change:build_success:feature", Kind: SignalChange, Tone: ToneAttention, Metric: ptr("build_success:feature")},
	}}
	a.foldChanges()
	ids := sigIDs(a.out)
	if slices.Contains(ids, "change:review_time") {
		t.Errorf("review change kept, its drivers both named: %v", ids)
	}
	if !slices.Contains(ids, "change:build_success:feature") {
		t.Errorf("a hint folded a red change: %v", ids)
	}
}

// A PR merged into a branch not yet on the trunk is the team's workflow,
// no source failing to report: no "nicht messbar" card, no data-honesty
// reason keeping the period from quiet. A PR in a lost silence still is.
func TestUntracedLeavesOffTrunkOut(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 2, false)}
		d.PRs = []dataset.PR{merged(1, 5), merged(2, 5, func(p *dataset.PR) { p.TrunkAt, p.OffTrunk = nil, true })}
	})
	a := assessStates(t, ds, nil, History{})
	if x := stateSignal(a, StateUntraced); x != nil {
		t.Errorf("an off-trunk PR raised untraced: %+v", x.State.Units)
	}
	if slices.Contains(a.QuietReasons, QuietUnjudged) {
		t.Errorf("quiet reasons %v, want no unjudged for an off-trunk PR", a.QuietReasons)
	}
	// Beside a PR tracing cannot place, it is counted neither in the unit
	// nor as its reason.
	ds.PRs = append(ds.PRs, merged(3, 5, func(p *dataset.PR) { p.TrunkAt = nil }))
	x := stateSignal(assessStates(t, ds, nil, History{}), StateUntraced)
	if x == nil || *x.State.Units[0].Count != 1 || *x.State.Units[0].Reason != TraceNoMergeCommit {
		t.Errorf("untraced = %+v, want 1 PR without a merge commit", x)
	}
}

// A project's red state flags its repos: none of them counts as calm.
func TestCalmLineProjectState(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 40, false)}
		// kasse-api: 5 PRs on production 2 days after merging, the typical
		// wait judged clear (a judged repo), merged after the last release.
		for i := range 5 {
			d.PRs = append(d.PRs, merged(int64(i+1), 10, shippedOn(8)))
		}
	})
	a := assessStates(t, ds, nil, History{})
	if x := stateSignal(a, StateNoRelease); x == nil {
		t.Fatalf("no breach: %v", sigIDs(a.Signals))
	}
	if a.Calm != 0 {
		t.Errorf("calm = %d, want kasse-api flagged by its project's breach", a.Calm)
	}
}
