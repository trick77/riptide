package metrics

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
)

// The closed period the state tests judge: four weeks ending Monday
// 2026-11-09 00:00 in Zurich (now is Thursday of that week).
var (
	stCur  = ClosedPeriod(4, now, zurich)
	stPrev = ClosedPrevious(4, now, zurich)
	stEnd  = stCur.To
)

// before is the instant days before the period's end.
func endAgo(days float64) time.Time {
	return stEnd.Add(-time.Duration(days * float64(24*time.Hour))).UTC()
}

func endAgoP(days float64) *time.Time { return ptr(endAgo(days)) }

// stateData is a team with one product, kasse: its App kasse-prod deploys
// kasse/kasse-api, which tracing knows; every source reporting for long.
func stateData(mod ...func(*dataset.Dataset)) *dataset.Dataset {
	ds := data(func(d *dataset.Dataset) {
		d.Meta.From, d.Meta.To = stCur.From, stCur.To
		d.Meta.DataStart = dataset.SourceTimes{Bitbucket: endAgoP(200), Pipeline: endAgoP(200), ArgoCD: endAgoP(200),
			Noergler: endAgoP(200)}
		d.Meta.DataEnd = dataset.SourceTimes{Bitbucket: endAgoP(0.1), Pipeline: endAgoP(0.1), ArgoCD: endAgoP(0.1),
			Noergler: endAgoP(0.1)}
		d.Meta.BuildDataFrom, d.Meta.BuildTypeFrom = endAgoP(200), endAgoP(200)
		d.Apps = []dataset.App{{Name: "kasse-prod", Project: s("kasse")}, {Name: "monitoring-prod", Infra: true}}
		d.Traces = map[string]dataset.RepoTrace{"kasse/kasse-api": {DeployedSince: endAgoP(150)}}
	})
	for _, m := range mod {
		m(ds)
	}
	return ds
}

// assessStates assesses ds against an empty previous period, with th
// (nil: the starting values) and h.
func assessStates(t *testing.T, ds *dataset.Dataset, th *config.Thresholds, h History) Assessment {
	t.Helper()
	p := Params{Period: stCur, Previous: stPrev, Thresholds: th}
	return Assess(ds, &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps}, p, h)
}

func stateSignal(a Assessment, key string) *Signal {
	for i := range a.Signals {
		if a.Signals[i].ID == SignalState+":"+key {
			return &a.Signals[i]
		}
	}
	return nil
}

func release(app string, days float64, first bool) dataset.AppRelease {
	return dataset.AppRelease{App: app, Environment: "prod", At: endAgo(days), First: first}
}

// merged is a person's PR of kasse-api merged days before the period's end.
func merged(id int64, days float64, mod ...func(*dataset.PR)) dataset.PR {
	return pr(append([]func(*dataset.PR){func(p *dataset.PR) {
		p.PRID, p.MergedAt, p.TrunkAt = id, endAgoP(days), endAgoP(days)
		p.OpenedAt, p.ReadyAt, p.ApprovedAt, p.FirstReviewAt = endAgoP(days+1), endAgoP(days+1), endAgoP(days+0.5), endAgoP(days+0.5)
		p.FirstCommitAt = endAgoP(days + 2)
	}}, mod...)...)
}

func TestWorkingDays(t *testing.T) {
	// Thursday 12:00 to Tuesday 12:00 in Zurich: Thu, Fri, Mon, Tue halves.
	from := time.Date(2026, 11, 19, 12, 0, 0, 0, zurich)
	to := time.Date(2026, 11, 24, 12, 0, 0, 0, zurich)
	if got := workingDays(from, to, zurich); !near(got, 3) {
		t.Errorf("working days = %v, want 3", got)
	}
	if got := workingDays(to, from, zurich); got != 0 {
		t.Errorf("backwards = %v, want 0", got)
	}
	// A weekend alone is no working time.
	sat := time.Date(2026, 11, 21, 0, 0, 0, 0, zurich)
	if got := workingDays(sat, sat.AddDate(0, 0, 2), zurich); got != 0 {
		t.Errorf("weekend = %v, want 0", got)
	}
}

func TestStatePeriod(t *testing.T) {
	p := StatePeriod(stCur, stPrev)
	if p == nil || !p.To.Equal(stPrev.From) || len(p.Weeks) != StateLookbackWeeks-8 {
		t.Fatalf("look-back = %+v, want %d weeks up to the previous period", p, StateLookbackWeeks-8)
	}
	if !p.From.Equal(stEnd.In(zurich).AddDate(0, 0, -7*StateLookbackWeeks).UTC()) {
		t.Errorf("from %v, want %d weeks before the end", p.From, StateLookbackWeeks)
	}
	if got := StatePeriod(ClosedPeriod(26, now, zurich), ClosedPrevious(26, now, zurich)); got != nil {
		t.Errorf("a 26-week view looks back through its previous period already, got %+v", got)
	}
}

func TestNoReleaseBreachAndHint(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Apps = append(d.Apps, dataset.App{Name: "lager-prod", Project: s("lager")},
			dataset.App{Name: "portal-prod", Project: s("portal")}, dataset.App{Name: "tools-prod", Project: s("tools")})
		d.Releases = []dataset.AppRelease{
			release("kasse-prod", 60, false), release("kasse-prod", 40, false),
			// A release after the period's end is not one of it.
			release("kasse-prod", -1, false),
			release("lager-prod", 19, false),
			release("portal-prod", 5, false),
			// Nobody works on tools: idle, no finding.
			release("tools-prod", 90, false),
			// Infrastructure never judged.
			release("monitoring-prod", 80, false),
		}
		d.PRs = []dataset.PR{
			merged(1, 3),
			merged(2, 3, func(p *dataset.PR) { p.Repo, p.Project = "lager/lager-api", "lager" }),
			merged(3, 2, func(p *dataset.PR) { p.Repo, p.Project = "portal/portal-web", "portal" }),
		}
	})
	a := assessStates(t, ds, nil, History{})
	b := stateSignal(a, StateNoRelease)
	if b == nil {
		t.Fatalf("no breach card; signals %v", sigIDs(a.Signals))
	}
	c := b.State
	if b.Tone != ToneAttention || c.Count != 1 || c.Judged != 3 || c.Idle != 1 || c.Units[0].Name != "kasse" ||
		c.Units[0].Value != 40 || c.Threshold.Value != 28 || c.Threshold.Key != "release_breach_days" {
		t.Errorf("breach = %s %+v, want kasse 40 days of 3 judged, tools idle, against 28", b.Tone, *c)
	}
	if !slices.Equal(c.Units[0].Names, []string{"kasse-prod"}) || c.Units[0].LowerBound {
		t.Errorf("kasse's Apps %v lower %v, want kasse-prod, a known release", c.Units[0].Names, c.Units[0].LowerBound)
	}
	d := stateSignal(a, StateReleaseDue)
	if d == nil || d.Tone != ToneHint || d.State.Count != 1 || d.State.Units[0].Name != "lager" || d.State.Units[0].Value != 19 {
		t.Errorf("hint = %+v, want lager after 19 days", d)
	}
	if d != nil && d.Weight >= b.Weight {
		t.Errorf("the rule broken ranks first: hint weight %d, breach %d", d.Weight, b.Weight)
	}
}

func TestNoReleaseGuards(t *testing.T) {
	// The App's first deploy right after Argo CD started: no known release,
	// "at least since then". A library-only project has no App: never judged.
	ds := stateData(func(d *dataset.Dataset) {
		d.Meta.DataStart.ArgoCD = endAgoP(40)
		d.Releases = []dataset.AppRelease{release("kasse-prod", 35, true)}
		d.PRs = []dataset.PR{merged(1, 3), merged(2, 3, func(p *dataset.PR) {
			p.Repo, p.Project, p.Library = "libs/lib-core", "libs", true
		})}
	})
	a := assessStates(t, ds, nil, History{})
	b := stateSignal(a, StateNoRelease)
	if b == nil || !b.State.Units[0].LowerBound || !b.State.SinceLowerBound || b.State.Judged != 1 {
		t.Fatalf("breach = %+v, want kasse at least since its first deploy, the library project unjudged", b)
	}
	// Argo CD silent at the end: nothing judged, no card.
	ds.Meta.DataEnd.ArgoCD = endAgoP(10)
	if b := stateSignal(assessStates(t, ds, nil, History{}), StateNoRelease); b != nil {
		t.Errorf("a gated Argo CD judges no release: %+v", b)
	}
}

func TestNotOnProd(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 2, false)}
		d.Traces["portal/portal-web"] = dataset.RepoTrace{DeployedSince: endAgoP(100)}
		d.PRs = []dataset.PR{
			// Lying 20 days, and a younger one: kasse-api fires.
			merged(1, 20), merged(2, 5),
			// Shipped: not lying.
			merged(3, 30, func(p *dataset.PR) {
				p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: endAgo(25)}}
			}),
			// A library's PR is never rolled out.
			merged(4, 40, func(p *dataset.PR) { p.Repo, p.Project, p.Library = "libs/lib-core", "libs", true }),
			// A repo no deploy shows an App deploying: not this state.
			merged(5, 40, func(p *dataset.PR) { p.Repo, p.Project = "lager/lager-api", "lager" }),
			// Merged into a branch that has not gone on: not on the trunk.
			merged(6, 30, func(p *dataset.PR) {
				p.Repo, p.Project, p.TrunkAt, p.OffTrunk = "portal/portal-web", "portal", nil, true
			}),
		}
	})
	a := assessStates(t, ds, nil, History{})
	x := stateSignal(a, StateNotOnProd)
	if x == nil {
		t.Fatalf("no card; signals %v", sigIDs(a.Signals))
	}
	c := x.State
	if c.Count != 1 || c.Units[0].Name != "kasse/kasse-api" || c.Units[0].Value != 20 || *c.Units[0].Count != 2 {
		t.Errorf("lying = %+v, want kasse-api, 2 PRs, the oldest 20 days", *c)
	}
	if slices.ContainsFunc(c.Units, func(u StateUnit) bool { return u.Name != "kasse/kasse-api" }) {
		t.Errorf("only kasse-api may be named: %+v", c.Units)
	}
}

func TestSlowToProd(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		for i := range 5 {
			d.PRs = append(d.PRs, merged(int64(i+1), 25, func(p *dataset.PR) {
				p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: endAgo(25 - 16 - float64(i))}}
			}))
		}
	})
	x := stateSignal(assessStates(t, ds, nil, History{}), StateSlowToProd)
	if x == nil || x.State.Units[0].Value != 18 || *x.State.Units[0].Count != 5 {
		t.Errorf("slow = %+v, want kasse-api typically 18 days on 5 PRs", x)
	}
	// Four PRs: too few to judge.
	ds.PRs = ds.PRs[:4]
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateSlowToProd); x != nil {
		t.Errorf("four PRs judge nothing: %+v", x.State)
	}
}

func approval(repo string, id int64, approved float64, mergedDays *float64) dataset.Approval {
	a := dataset.Approval{Repo: repo, Project: projectOf(repo), PRID: id, ApprovedAt: endAgoP(approved)}
	if mergedDays != nil {
		a.MergedAt = endAgoP(*mergedDays)
	}
	return a
}

func TestApprovedWaiting(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.PRs = []dataset.PR{merged(1, 3)}
		d.Approvals = []dataset.Approval{
			// Approved Monday and Tuesday of the last week, still waiting:
			// 4 and 3 working days.
			approval("kasse/kasse-api", 10, 7, nil), approval("kasse/kasse-api", 11, 6, nil),
			// Approved on Friday: half a working day, no finding.
			approval("kasse/kasse-api", 12, 2.5, nil),
			// Merged before the end: not waiting.
			approval("kasse/kasse-api", 13, 9, ptr(1.0)),
			// A bot's PR never counts.
			func() dataset.Approval { a := approval("kasse/kasse-api", 14, 8, nil); a.Bot = true; return a }(),
			// One in portal: one is no habit.
			approval("portal/portal-web", 20, 8, nil),
		}
	})
	a := assessStates(t, ds, nil, History{})
	x := stateSignal(a, StateApprovedWaiting)
	if x == nil {
		t.Fatalf("no card; signals %v", sigIDs(a.Signals))
	}
	if x.Tone != ToneHint || x.State.Count != 1 || x.State.Units[0].Name != "kasse/kasse-api" || *x.State.Units[0].Count != 2 ||
		x.State.Units[0].Value != 5 {
		t.Errorf("approved waiting = %s %+v, want a hint, kasse-api with 2 PRs, the oldest 5 working days", x.Tone, x.State.Units)
	}
	// Waiting two sprints: red.
	ds.Approvals[0].ApprovedAt = endAgoP(30)
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateApprovedWaiting); x == nil || x.Tone != ToneAttention {
		t.Errorf("held two sprints = %+v, want red", x)
	}
	// A lost Bitbucket silence after the approvals: their merge may be lost.
	ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: endAgo(3), To: endAgo(1), Lost: true}}
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateApprovedWaiting); x != nil {
		t.Errorf("unknown after a lost silence: %+v", x.State.Units)
	}
}

// pauseData is kasse merging 8 PRs a week for 12 weeks before the period's
// last week, which has none while approved PRs wait in it.
func pauseData(waiting int) *dataset.Dataset {
	return stateData(func(d *dataset.Dataset) {
		id := int64(0)
		for w := 1; w <= 12; w++ {
			for i := range 8 {
				id++
				d.PRs = append(d.PRs, merged(id, float64(7*w)+0.5+float64(i)*0.3))
			}
		}
		for i := range waiting {
			// Approved Tuesday of the last week, merged Friday after the end.
			d.Approvals = append(d.Approvals, approval("kasse/kasse-api", int64(100+i), 6, ptr(-4.0)))
		}
	})
}

func TestMergePause(t *testing.T) {
	a := assessStates(t, pauseData(3), nil, History{})
	x := stateSignal(a, StateMergePause)
	if x == nil {
		t.Fatalf("no merge pause; signals %v", sigIDs(a.Signals))
	}
	u := x.State.Units[0]
	if u.Name != "kasse" || u.Value != 3 || *u.Count != 0 || *u.Typical != 8 || u.Week == nil || u.Week.KW != 45 ||
		x.Weight != tierNow {
		t.Errorf("pause = %+v week %+v weight %d, want kasse KW 45, 0 merges of typically 8, 3 waiting, now", u, u.Week,
			x.Weight)
	}
	// Two approved PRs in a quiet week: a holiday, no pause.
	if x := stateSignal(assessStates(t, pauseData(2), nil, History{}), StateMergePause); x != nil {
		t.Errorf("two waiting PRs make no pause: %+v", x.State.Units)
	}
	// A week of normal merges is none whatever waits.
	ds := pauseData(3)
	for i := range 8 {
		ds.PRs = append(ds.PRs, merged(int64(500+i), 1+float64(i)*0.5))
	}
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateMergePause); x != nil {
		t.Errorf("a week of 8 merges is no pause: %+v", x.State.Units)
	}
	// Bitbucket lost the whole week: not judged.
	ds = pauseData(3)
	ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: endAgo(7.5), To: endAgo(-0.5), Lost: true}}
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateMergePause); x != nil {
		t.Errorf("a lost week judges nothing: %+v", x.State.Units)
	}
}

func botRun(i int, failed bool, mod ...func(*dataset.Run)) dataset.Run {
	st := "SUCCESS"
	if failed {
		st = "FAILURE"
	}
	return run(append([]func(*dataset.Run){func(r *dataset.Run) {
		r.RunID, r.Status, r.Bot = strconv.Itoa(i), s(st), true
		r.PipelineName, r.Branch, r.BranchKind = "kasse-api/renovate%2Flib-2.x", "renovate/lib-2.x", dataset.BranchOther
		r.StartedAt, r.FinishedAt = endAgoP(10-float64(i)*0.1), endAgoP(10-float64(i)*0.1-0.01)
		r.OccurredAt = *r.FinishedAt
	}}, mod...)...)
}

func TestBotLoop(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		for i := range 12 {
			d.Runs = append(d.Runs, botRun(i, true))
		}
		// An aborted build neither breaks nor extends the loop.
		d.Runs = append(d.Runs, botRun(20, false, func(r *dataset.Run) { r.Status = s("ABORTED") }))
		// Another bot branch fails 9 times, then passes.
		for i := range 10 {
			d.Runs = append(d.Runs, botRun(30+i, i < 9, func(r *dataset.Run) {
				r.PipelineName, r.Branch = "kasse-api/renovate%2Fother-1.x", "renovate/other-1.x"
			}))
		}
		// A person's branch failing on and on is no bot loop.
		for i := range 15 {
			d.Runs = append(d.Runs, run(func(r *dataset.Run) {
				r.RunID, r.Status = strconv.Itoa(100+i), s("FAILURE")
				r.PipelineName, r.Branch, r.BranchKind = "kasse-api/feature%2FKAS-1", "feature/KAS-1", dataset.BranchFeature
				r.StartedAt, r.FinishedAt = endAgoP(5-float64(i)*0.1), endAgoP(5-float64(i)*0.1-0.01)
			}))
		}
	})
	a := assessStates(t, ds, nil, History{})
	x := stateSignal(a, StateBotLoop)
	if x == nil {
		t.Fatalf("no loop; signals %v", sigIDs(a.Signals))
	}
	if x.Tone != ToneHint || x.State.Count != 1 || x.State.Judged != 2 || x.State.Units[0].Value != 12 ||
		x.State.Units[0].Name != "kasse/kasse-api:renovate/lib-2.x" {
		t.Errorf("loop = %s %+v, want a hint on one of two bot branches, 12 in a row", x.Tone, *x.State)
	}
	if *x.State.Units[0].CISeconds != 12*600 {
		t.Errorf("wasted CI time = %v, want the loop's 12 builds", *x.State.Units[0].CISeconds)
	}
	for _, s := range a.Signals {
		if s.Tone == ToneAttention {
			t.Errorf("a bot's failures raise no red card: %s", s.ID)
		}
	}
	th := config.DefaultThresholds()
	th.BotLoopFailures = 5
	if x := stateSignal(assessStates(t, ds, &th, History{}), StateBotLoop); x == nil || x.State.Count != 2 {
		t.Errorf("at 5 in a row both branches loop: %+v", x)
	}
}

func TestHonestyStates(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{{App: "kasse-prod", Environment: "intg", At: endAgo(3)}}
		d.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: endAgo(12), To: endAgo(9), Lost: true,
			UnseenRuns: 2}}
		d.Traces["kasse/kasse-api"] = dataset.RepoTrace{DeployedSince: endAgoP(150), Releases: []dataset.TraceRelease{
			{Environment: "prod", At: endAgo(8), Silence: true}}}
		d.PRs = []dataset.PR{merged(1, 10)}
		// An App whose GitOps commit no Bitbucket event names.
		d.Deploys = []dataset.Deploy{deploy(func(x *dataset.Deploy) { x.App, x.At, x.Project = "lager-prod", endAgo(2), nil })}
	})
	a := assessStates(t, ds, nil, History{})
	u := stateSignal(a, StateUntraced)
	if u == nil || u.Tone != ToneLimit || u.State.Units[0].Reason == nil || *u.State.Units[0].Reason != TraceInSilence {
		t.Errorf("untraced = %+v, want kasse-api's PR in the silence", u)
	}
	if x := stateSignal(a, StateUnattributed); x == nil || x.State.Units[0].Value != 1 {
		t.Errorf("unattributed = %+v, want the release without a project", x)
	}
	if x := stateSignal(a, StateNoProdStage); x == nil || !slices.Equal(x.State.Units[0].Names, []string{"intg"}) {
		t.Errorf("no prod stage = %+v, want intg reported, never prod", x)
	}
	if !slices.Contains(a.QuietReasons, QuietUnjudged) {
		t.Errorf("quiet reasons %v, want unjudged for the data-honesty states", a.QuietReasons)
	}
}

func TestStateHysteresisAndResolution(t *testing.T) {
	// kasse-api's oldest lying PR at 12 days: under the threshold, above 80 %
	// of it. Named last time: held.
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 2, false)}
		d.PRs = []dataset.PR{merged(1, 12)}
	})
	id := SignalState + ":" + StateNotOnProd
	h := History{Runs: 1, Latest: map[string]string{id: ToneAttention}, Open: map[string]Problem{id: {Tone: ToneAttention}},
		StateUnits: map[string][]string{id: {"kasse/kasse-api"}}, Seen: map[string]int{id: 1}}
	a := assessStates(t, ds, nil, h)
	x := stateSignal(a, StateNotOnProd)
	if x == nil || !x.State.Units[0].Held || x.Seen != 1 {
		t.Fatalf("held = %+v, want kasse-api held, seen once", x)
	}
	// Not named last time: no card.
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateNotOnProd); x != nil {
		t.Errorf("12 days without history fire nothing: %+v", x.State.Units)
	}
	// Under 80 % of it: resolved, with evidence.
	ds.PRs = []dataset.PR{merged(1, 8)}
	a = assessStates(t, ds, nil, h)
	if stateSignal(a, StateNotOnProd) != nil || !slices.ContainsFunc(a.Signals, func(s Signal) bool {
		return s.ID == SignalResolved+":"+id && s.Tone == ToneGood
	}) {
		t.Errorf("signals %v, want the state resolved", sigIDs(a.Signals))
	}
	// No longer judgeable (no merged PR of a deployable repo): not resolved,
	// "nicht mehr messbar".
	ds.PRs = nil
	a = assessStates(t, ds, nil, h)
	if !slices.ContainsFunc(a.Signals, func(s Signal) bool {
		return s.ID == SignalState+":"+StateUnmeasurable+":"+StateNotOnProd && s.Tone == ToneLimit && s.State.Was != nil
	}) || slices.ContainsFunc(a.Signals, func(s Signal) bool { return s.Kind == SignalResolved }) {
		t.Errorf("signals %v, want not_on_prod unmeasurable, never resolved", sigIDs(a.Signals))
	}
}

func TestStateCardWorstThree(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		for i, days := range []float64{30, 45, 20, 60, 33} {
			repo := "kasse/kasse-" + strconv.Itoa(i)
			d.Traces[repo] = dataset.RepoTrace{DeployedSince: endAgoP(150)}
			d.PRs = append(d.PRs, merged(int64(i+1), days, func(p *dataset.PR) { p.Repo = repo }))
		}
	})
	x := stateSignal(assessStates(t, ds, nil, History{}), StateNotOnProd)
	if x == nil {
		t.Fatal("no card")
	}
	c := x.State
	var worst []float64
	for _, u := range c.Worst {
		worst = append(worst, u.Value)
	}
	if c.Count != 5 || c.More != 2 || !slices.Equal(worst, []float64{60, 45, 33}) || len(c.Units) != 5 ||
		!c.Since.Equal(endAgo(60)) {
		t.Errorf("card = %+v, want 5 repos, the worst 60, 45, 33, 2 more, since the oldest", *c)
	}
}

// A notable worsening a red state covers is a fact on its card, one finding
// said once; a review worsening keeps the repos no unit names; a hint
// covers nothing, so the red change stays.
func TestFoldChanges(t *testing.T) {
	card := func(key, tone string, units ...string) Signal {
		c := &StateCard{Key: key}
		for _, u := range units {
			c.Units = append(c.Units, StateUnit{Name: u})
		}
		return Signal{ID: SignalState + ":" + key, Kind: SignalState, Tone: tone, State: c,
			Facts: []Fact{{ID: SignalState + ":" + key + ".count"}}}
	}
	change := func(metric string, drivers ...string) Signal {
		s := Signal{ID: "change:" + metric, Kind: SignalChange, Tone: ToneAttention, Metric: ptr(metric),
			Facts: []Fact{{ID: "change:" + metric + ".value"}}}
		for _, d := range drivers {
			s.Drivers = append(s.Drivers, Driver{Repo: d})
		}
		return s
	}
	a := &assessor{out: []Signal{
		card(StateNoRelease, ToneAttention, "kasse"), change("deploy_frequency"),
		card(StateFirstReviewWait, ToneAttention, "kasse/kasse-api"), change("review_time", "kasse/kasse-api", "lager/lager-api"),
		card(StateBranchFailing, ToneHint, "kasse/kasse-api:feature/x"), change("build_success:feature"),
	}}
	a.foldChanges()
	got := map[string]Signal{}
	for _, s := range a.out {
		got[s.ID] = s
	}
	if _, ok := got["change:deploy_frequency"]; ok || len(got["state:no_release"].Facts) != 2 {
		t.Errorf("deploy frequency not folded: %v", sigIDs(a.out))
	}
	if r, ok := got["change:review_time"]; !ok || len(r.Drivers) != 1 || r.Drivers[0].Repo != "lager/lager-api" {
		t.Errorf("review change = %+v, want lager-api left as its driver", r)
	}
	if _, ok := got["change:build_success:feature"]; !ok {
		t.Errorf("a hint folded a red change away: %v", sigIDs(a.out))
	}
}

func sigIDs(ss []Signal) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}
