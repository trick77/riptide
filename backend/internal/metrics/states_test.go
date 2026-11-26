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
	// The deploy table knows no project: its last release's App finds it.
	if q := c.Units[0].Query; q == nil || *q != "kasse-prod" || c.Facts != "deploys" {
		t.Errorf("kasse's facts %q query %v, want the deploys of kasse-prod", c.Facts, q)
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

// approval is a PR of repo a person approved days before the period's
// end, still approved, merged mergedDays before it (nil: open); its last
// event the approval.
func approval(repo string, id int64, approved float64, mergedDays *float64) dataset.Approval {
	a := dataset.Approval{Repo: repo, Project: projectOf(repo), PRID: id, ApprovedAt: endAgoP(approved),
		Spans: []dataset.ApprovalSpan{{From: endAgo(approved)}}, Activity: []time.Time{endAgo(approved)}}
	if mergedDays != nil {
		a.MergedAt = endAgoP(*mergedDays)
		a.Activity = append(a.Activity, endAgo(*mergedDays))
	}
	return a
}

// withdrawn ends a's approval days before the period's end (unapproved,
// needs work, reset by a push).
func withdrawn(a dataset.Approval, days float64) dataset.Approval {
	a.Spans = []dataset.ApprovalSpan{{From: a.Spans[0].From, To: endAgoP(days)}}
	a.Activity = append(a.Activity, endAgo(days))
	return a
}

// needsWork has another reviewer set needs work on a days before the
// period's end, still standing: a's approval stands too.
func needsWork(a dataset.Approval, days float64) dataset.Approval {
	a.NeedsWork = []dataset.ApprovalSpan{{From: endAgo(days)}}
	a.Activity = append(slices.Clone(a.Activity), endAgo(days))
	slices.SortFunc(a.Activity, func(x, y time.Time) int { return x.Compare(y) })
	return a
}

// touched adds an event days before the period's end to a.
func touched(a dataset.Approval, days float64) dataset.Approval {
	a.Activity = append(slices.Clone(a.Activity), endAgo(days))
	slices.SortFunc(a.Activity, func(x, y time.Time) int { return x.Compare(y) })
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
	// Since the second of them had waited past a working day.
	if s := x.State.Units[0].Since; s == nil || !s.Equal(afterWorkingDays(endAgo(6), 1, zurich)) {
		t.Errorf("since %v, want the second PR's crossing", s)
	}
	// One approval forgotten for 40 days (touched since) and one of 4: two
	// waiting began days ago, a hint (the onset, not the oldest case).
	old := slices.Clone(ds.Approvals)
	ds.Approvals[0] = touched(approval("kasse/kasse-api", 10, 40, nil), 3)
	ds.Approvals[1] = approval("kasse/kasse-api", 11, 4, nil)
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateApprovedWaiting); x == nil || x.Tone != ToneHint {
		t.Errorf("one old approval and one young = %+v, want a hint", x)
	}
	// Both waiting for over a month, still touched: held two sprints, red.
	ds.Approvals[1] = touched(approval("kasse/kasse-api", 11, 35, nil), 2)
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateApprovedWaiting); x == nil || x.Tone != ToneAttention {
		t.Errorf("held two sprints = %+v, want red", x)
	}
	// Approved 40 days back and untouched since: still open, still approved,
	// still waiting (the forgotten PR is what the state is for), and red.
	ds.Approvals[0] = approval("kasse/kasse-api", 10, 40, nil)
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateApprovedWaiting); x == nil || x.Tone != ToneAttention ||
		*x.State.Units[0].Count != 2 {
		t.Errorf("an untouched approved PR = %+v, want it still waiting, red", x)
	}
	ds.Approvals = old
	// Another reviewer's needs work: the approval stands, but the PR is not
	// ready to merge. Not waiting, not done: no card at all.
	ds.Approvals[1] = needsWork(ds.Approvals[1], 2)
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateApprovedWaiting); x != nil {
		t.Errorf("a PR held by needs work waits for its merge: %+v", x.State.Units)
	}
	ds.Approvals = old
	// An approval withdrawn (needs work, unapproved, reset by a push) waits
	// no more.
	ds.Approvals[1] = withdrawn(ds.Approvals[1], 5)
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateApprovedWaiting); x != nil {
		t.Errorf("a withdrawn approval waits: %+v", x.State.Units)
	}
	ds.Approvals = old
	// A lost Bitbucket silence after the approvals: their merge may be lost.
	ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: endAgo(3), To: endAgo(1), Lost: true}}
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateApprovedWaiting); x != nil {
		t.Errorf("unknown after a lost silence: %+v", x.State.Units)
	}
	// So is one running since before the approval.
	ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: endAgo(9), To: endAgo(2), Lost: true}}
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateApprovedWaiting); x != nil {
		t.Errorf("unknown in a silence running at the approval: %+v", x.State.Units)
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
	// A project's PRs are its repos': "kasse/" finds them.
	if u.Query == nil || *u.Query != "kasse/" {
		t.Errorf("pause query = %v", u.Query)
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
	// Approvals withdrawn on Wednesday (needs work by the approver, reset by
	// a push) no longer stand at the week's end.
	ds = pauseData(3)
	for i := range ds.Approvals {
		ds.Approvals[i] = withdrawn(ds.Approvals[i], 4.5)
	}
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateMergePause); x != nil {
		t.Errorf("withdrawn approvals waited: %+v", x.State.Units)
	}
	// Another reviewer's needs work standing at the week's end: approved,
	// but not ready to merge.
	ds = pauseData(3)
	ds.Approvals[0] = needsWork(ds.Approvals[0], 3)
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateMergePause); x != nil {
		t.Errorf("a PR held by needs work waited: %+v", x.State.Units)
	}
	// An approval given up long ago (untouched for OpenInactiveDays by the
	// week's end) waits for nothing.
	ds = pauseData(2)
	ds.Approvals = append(ds.Approvals, approval("kasse/kasse-api", 199, 45, nil))
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateMergePause); x != nil {
		t.Errorf("an abandoned approval made a pause: %+v", x.State.Units)
	}
	// The card counts the approved PRs open at the week's end.
	if x := stateSignal(assessStates(t, pauseData(4), nil, History{}), StateMergePause); x == nil ||
		x.State.Units[0].Value != 4 || x.State.Threshold.Within != nil {
		t.Errorf("pause of 4 = %+v, want 4 approved PRs open and no span threshold", x)
	}
	// A silence lost since the approvals: their merges may be in it, so
	// they wait for nothing, though no silence began after them.
	ds = pauseData(3)
	ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: endAgo(6.5), To: endAgo(5.5), Lost: true}}
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateMergePause); x != nil {
		t.Errorf("approvals in a lost silence waited: %+v", x.State.Units)
	}
}

// A lost Bitbucket silence takes its working time out of the week and its
// expectation: the unit states the typical week scaled to what is left and
// says so (Lost); too little left to expect, the week is not judged.
func TestMergePauseLostSilence(t *testing.T) {
	week := func(lostTo float64) *dataset.Dataset {
		ds := pauseData(0)
		// Approved Friday noon, after the silence; merged after the end.
		for i := range 3 {
			ds.Approvals = append(ds.Approvals, approval("kasse/kasse-api", int64(400+i), 2.5, ptr(-4.0)))
		}
		ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: endAgo(7), To: endAgo(lostTo),
			Lost: true, UnseenRuns: 2}}
		return ds
	}
	// Monday and Tuesday lost: three of five working days left, typically
	// 8 × 3/5.
	x := stateSignal(assessStates(t, week(5), nil, History{}), StateMergePause)
	if x == nil {
		t.Fatal("no pause after a two-day silence")
	}
	if u := x.State.Units[0]; !u.Lost || u.Typical == nil || *u.Typical != 4.8 || *u.Count != 0 || u.Value != 3 {
		t.Errorf("pause = %+v typical %v, want 0 merges of typically 4.8 (scaled), lost, 3 waiting", u, u.Typical)
	}
	// Without a silence: the unscaled typical week, nothing lost.
	ds := week(5)
	ds.Meta.Silences = nil
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateMergePause); x == nil || x.State.Units[0].Lost ||
		*x.State.Units[0].Typical != 8 {
		t.Errorf("pause without a silence = %+v, want typically 8, nothing lost", x)
	}
	// Monday to Thursday lost: 8 × 1/5 left, under merge_pause_min_typical:
	// merges riptide never heard of could make the pause. Not judged.
	if x := stateSignal(assessStates(t, week(3), nil, History{}), StateMergePause); x != nil {
		t.Errorf("a week mostly lost judged a pause: %+v", x.State.Units)
	}
}

// A merge pause reads the week as of its end, whenever it is read: PRs
// approved Friday afternoon and merged Monday were open and approved at the
// week's end, however short their wait; merged on Sunday, they were not.
func TestMergePauseFrozenAtWeekEnd(t *testing.T) {
	ds := pauseData(0)
	for i := range 3 {
		// Friday 15:00 of the last week; merged the Monday after at 16:00.
		ds.Approvals = append(ds.Approvals, approval("kasse/kasse-api", int64(200+i), 2.375, ptr(-0.6667)))
	}
	ds.Meta.GeneratedAt = endAgo(-10)
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateMergePause); x == nil || x.State.Units[0].Value != 3 {
		t.Errorf("Friday approvals open at the week's end = %+v, want a pause with 3", x)
	}
	// Merged on Sunday: closed by the week's end.
	ds = pauseData(0)
	for i := range 3 {
		ds.Approvals = append(ds.Approvals, approval("kasse/kasse-api", int64(300+i), 2.375, ptr(0.5)))
	}
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateMergePause); x != nil {
		t.Errorf("PRs merged before the week's end made a pause: %+v", x.State.Units)
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
	// Against no agreement of the team's: no threshold, no threshold fact a
	// model could call the team's.
	for _, key := range []string{StateUntraced, StateUnattributed, StateNoProdStage} {
		x := stateSignal(a, key)
		if x == nil {
			continue
		}
		if x.State.Threshold != nil || slices.ContainsFunc(x.Facts, func(f Fact) bool { return f.Label == "state_threshold" }) {
			t.Errorf("%s threshold %+v facts %+v, want none", key, x.State.Threshold, x.Facts)
		}
	}
}

// shippedOn is a PR on production days before the period's end.
func shippedOn(days float64) func(*dataset.PR) {
	return func(p *dataset.PR) {
		p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: endAgo(days)}}
	}
}

func hasSignal(a Assessment, id, tone string) bool {
	return slices.ContainsFunc(a.Signals, func(s Signal) bool { return s.ID == id && s.Tone == tone })
}

// Hysteresis, "gelöst" and "nicht mehr messbar" come from the data judged
// again as of a week before, never from a stored analysis: they work
// without one.
func TestStateHysteresisAndResolution(t *testing.T) {
	id := SignalState + ":" + StateNotOnProd
	resolved := SignalResolved + ":" + id
	// A week before, #1 had lain 18 days (fired); it shipped 2 days ago.
	// Now the oldest, #2, has lain 12 days: under 14, above 80 % of it.
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 2, false)}
		d.PRs = []dataset.PR{merged(1, 25, shippedOn(2)), merged(2, 12)}
	})
	a := assessStates(t, ds, nil, History{})
	x := stateSignal(a, StateNotOnProd)
	if x == nil || !x.State.Units[0].Held || x.State.Units[0].Value != 12 {
		t.Fatalf("held = %+v, want kasse-api held at 12 days", x)
	}
	// Not named a week before: no card.
	ds.PRs = []dataset.PR{merged(2, 12)}
	if x := stateSignal(assessStates(t, ds, nil, History{}), StateNotOnProd); x != nil {
		t.Errorf("12 days, nothing a week before, fire nothing: %+v", x.State.Units)
	}
	// Under 80 % of it now: "gelöst", with evidence, once.
	ds.PRs = []dataset.PR{merged(1, 25, shippedOn(2)), merged(2, 8)}
	a = assessStates(t, ds, nil, History{})
	if stateSignal(a, StateNotOnProd) != nil || !hasSignal(a, resolved, ToneGood) {
		t.Errorf("signals %v, want the state resolved", sigIDs(a.Signals))
	}
	// The oldest PR now one tracing cannot place: "nicht mehr messbar",
	// never "gelöst".
	ds.PRs = []dataset.PR{merged(1, 25, shippedOn(2)), merged(3, 20, func(p *dataset.PR) { p.TrunkAt = nil })}
	a = assessStates(t, ds, nil, History{})
	if !hasSignal(a, SignalState+":"+StateUnmeasurable+":"+StateNotOnProd, ToneLimit) ||
		slices.ContainsFunc(a.Signals, func(s Signal) bool { return s.Kind == SignalResolved }) {
		t.Errorf("signals %v, want not_on_prod unmeasurable, never resolved", sigIDs(a.Signals))
	}
	// A raised threshold resolves nothing: the week before is judged against
	// it too.
	th := config.DefaultThresholds()
	th.NotOnProdDays = 21
	ds.PRs = []dataset.PR{merged(1, 16)}
	if a := assessStates(t, ds, &th, History{}); stateSignal(a, StateNotOnProd) != nil || hasSignal(a, resolved, ToneGood) {
		t.Errorf("signals %v, want neither the card nor a resolution after the threshold moved", sigIDs(a.Signals))
	}
}

// A held unit stays held while its number stays in the band, week after
// week: fired two weeks back (#1 at 16 days), in the band a week back (#2
// at 13) and now (#3 at 12). Only held a week back, the card was a hint
// then: below the band now, it resolves silently.
func TestStateHeldAcrossWeeks(t *testing.T) {
	id := SignalState + ":" + StateNotOnProd
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 2, false)}
		d.PRs = []dataset.PR{merged(1, 30, shippedOn(10)), merged(2, 20, shippedOn(3)), merged(3, 12)}
	})
	x := stateSignal(assessStates(t, ds, nil, History{}), StateNotOnProd)
	if x == nil || !x.State.Units[0].Held || x.State.Units[0].Value != 12 {
		t.Errorf("card = %+v, want kasse-api still held at 12 days", x)
	}
	ds.PRs[2] = merged(3, 8)
	a := assessStates(t, ds, nil, History{})
	if stateSignal(a, StateNotOnProd) != nil || hasSignal(a, SignalResolved+":"+id, ToneGood) {
		t.Errorf("signals %v, want neither the card nor gelöst", sigIDs(a.Signals))
	}
}

// A card on which no unit is past the threshold, every unit only held
// within the hysteresis, breaks no agreement now: a hint whatever the
// state's tone, never red, and it takes no red team number's worsening
// onto it. One unit past the threshold beside it keeps the card red.
func TestHeldOnlyCardIsAHint(t *testing.T) {
	// A week before, #1 had lain 18 days (fired); it shipped 2 days ago.
	// Now the oldest, #2, has lain 12 days: under 14, above 80 % of it.
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 2, false)}
		d.PRs = []dataset.PR{merged(1, 25, shippedOn(2)), merged(2, 12)}
	})
	a := assessStates(t, ds, nil, History{})
	x := stateSignal(a, StateNotOnProd)
	if x == nil || x.Tone != ToneHint || !x.State.Units[0].Held {
		t.Fatalf("held only = %+v, want a hint", x)
	}
	if slices.ContainsFunc(a.Signals, func(s Signal) bool { return s.Tone == ToneAttention }) {
		t.Errorf("signals %v, want nothing red", sigIDs(a.Signals))
	}
	// A red worsening of the lead time stays a card of its own: a hint
	// covers nothing.
	as := &assessor{out: []Signal{*x, {ID: "change:lead_time", Kind: SignalChange, Tone: ToneAttention,
		Metric: ptr("lead_time")}}}
	as.foldChanges()
	if len(as.out) != 2 {
		t.Errorf("a held-only card folded the lead time: %v", sigIDs(as.out))
	}
	// Another repo past the threshold now: red, the held unit beside it.
	ds.Traces["kasse/kasse-web"] = dataset.RepoTrace{DeployedSince: endAgoP(150)}
	ds.PRs = append(ds.PRs, merged(3, 20, func(p *dataset.PR) { p.Repo = "kasse/kasse-web" }))
	x = stateSignal(assessStates(t, ds, nil, History{}), StateNotOnProd)
	if x == nil || x.Tone != ToneAttention || x.State.Count != 2 ||
		!slices.ContainsFunc(x.State.Units, func(u StateUnit) bool { return u.Held }) {
		t.Errorf("one past, one held = %+v, want red with both", x)
	}
}

// A state that was only a hint a week back resolves silently: a release
// due a week back, released since.
func TestHintResolvesSilently(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 24, false), release("kasse-prod", 2, false)}
		d.PRs = []dataset.PR{merged(1, 10, shippedOn(2)), merged(2, 3)}
	})
	a := assessStates(t, ds, nil, History{})
	if stateSignal(a, StateReleaseDue) != nil || slices.ContainsFunc(a.Signals, func(s Signal) bool { return s.Kind == SignalResolved }) {
		t.Errorf("signals %v, want no card and no gelöst", sigIDs(a.Signals))
	}
	// Past the breach a week back (red), released since: "gelöst".
	ds.Releases[0] = release("kasse-prod", 38, false)
	if a := assessStates(t, ds, nil, History{}); !hasSignal(a, SignalResolved+":"+SignalState+":"+StateNoRelease, ToneGood) {
		t.Errorf("signals %v, want no_release resolved", sigIDs(a.Signals))
	}
}

// A unit gone or fallen idle since the week before is no news: neither
// "gelöst" nor "nicht mehr messbar", and nothing keeps the period from
// being quiet.
func TestStateGoneIsNoNews(t *testing.T) {
	// A bot branch looped in the period a week before; its builds all lie
	// before this period: gone.
	ds := stateData(func(d *dataset.Dataset) {
		for i := range 12 {
			d.Runs = append(d.Runs, botRun(i, true, func(r *dataset.Run) {
				r.StartedAt, r.FinishedAt = endAgoP(33-float64(i)*0.1), endAgoP(33-float64(i)*0.1-0.01)
				r.OccurredAt = *r.FinishedAt
			}))
		}
	})
	a := assessStates(t, ds, nil, History{})
	if slices.ContainsFunc(a.Signals, func(s Signal) bool {
		return s.Kind == SignalResolved || (s.State != nil && s.State.Key == StateUnmeasurable)
	}) || slices.Contains(a.QuietReasons, QuietUnjudged) {
		t.Errorf("signals %v reasons %v, want nothing of a gone bot branch", sigIDs(a.Signals), a.QuietReasons)
	}
}

// Per unit, not per card: approved PRs waiting in kasse-api a week before,
// now of unknown state after a lost Bitbucket silence: "nicht mehr
// messbar", though no unit fires and kasse-api reads clear otherwise.
func TestStateUnjudgedUnitIsUnmeasurable(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.PRs = []dataset.PR{merged(1, 3)}
		d.Approvals = []dataset.Approval{
			touched(approval("kasse/kasse-api", 10, 14, nil), 6), touched(approval("kasse/kasse-api", 11, 13, nil), 6),
		}
		d.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: endAgo(5), To: endAgo(4), Lost: true}}
	})
	a := assessStates(t, ds, nil, History{})
	if !hasSignal(a, SignalState+":"+StateUnmeasurable+":"+StateApprovedWaiting, ToneLimit) ||
		hasSignal(a, SignalResolved+":"+SignalState+":"+StateApprovedWaiting, ToneGood) {
		t.Errorf("signals %v, want approved_waiting unmeasurable, not resolved", sigIDs(a.Signals))
	}
}

// A project past the breach escalated out of the hint: the hint a week
// before is not "gelöst" beside the red card.
func TestReleaseEscalationIsNoResolution(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 31, false)}
		d.PRs = []dataset.PR{merged(1, 3)}
	})
	a := assessStates(t, ds, nil, History{})
	if !hasSignal(a, SignalState+":"+StateNoRelease, ToneAttention) ||
		hasSignal(a, SignalResolved+":"+SignalState+":"+StateReleaseDue, ToneGood) {
		t.Errorf("signals %v, want the breach and no resolved hint", sigIDs(a.Signals))
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
