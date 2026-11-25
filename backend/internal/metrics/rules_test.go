package metrics

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// The assessment's rules of version 2: no question without a move, quiet
// decided here, symmetric thresholds with count gates, the bots' builds
// and infrastructure Apps out. Synthetic names only.

// quietData is a period of a team that merges merged PRs, every one traced
// to production, its sources covering both periods: nothing to say about
// it, unless mod adds something.
func quietData(p Period, merged int, mod ...func(*dataset.Dataset)) *dataset.Dataset {
	return data(func(ds *dataset.Dataset) {
		covered(ds, p)
		for i := range merged {
			at := p.To.Add(-time.Duration(2*24+i*20) * time.Hour).UTC()
			ds.PRs = append(ds.PRs, pr(func(x *dataset.PR) {
				x.PRID, x.MergedAt, x.OpenedAt, x.ReadyAt = int64(i+1), &at, ptr(at.Add(-time.Hour)), ptr(at.Add(-time.Hour))
				x.FirstCommitAt, x.FirstReviewAt, x.ApprovedAt = ptr(at.Add(-2*time.Hour)), ptr(at.Add(-30*time.Minute)), ptr(at.Add(-30*time.Minute))
				x.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: at.Add(time.Hour)}}
			}))
		}
		for _, m := range mod {
			m(ds)
		}
	})
}

func assessQuiet(cur, prev func(*dataset.Dataset)) Assessment {
	c, p := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	return Assess(quietData(c, 12, cur), quietData(p, 12, prev), Params{Period: c, Previous: p}, History{})
}

func noop(*dataset.Dataset) {}

func TestQuietReasons(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	silence := func(src string, unseen int, from time.Time) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) {
			ds.Meta.Silences = []dataset.Silence{{Source: src, From: from, To: from.Add(80 * time.Hour), UnseenRuns: unseen}}
		}
	}
	jam := func(ds *dataset.Dataset) {
		for i := range OpenJamMin {
			at := cur.To.Add(-time.Duration(9+i) * 24 * time.Hour).UTC()
			ds.OpenPRs = append(ds.OpenPRs, openPR(func(o *dataset.OpenPR) {
				o.PRID, o.OpenedAt, o.ReadyAt, o.ReadySince, o.LastEventAt = int64(50+i), at, ptr(at), ptr(at), cur.To.Add(-24*time.Hour).UTC()
			}))
		}
	}
	// A forgotten PR is a number of the open PRs, no finding.
	forgotten := func(ds *dataset.Dataset) {
		at := cur.To.Add(-60 * 24 * time.Hour).UTC()
		ds.OpenPRs = append(ds.OpenPRs, openPR(func(o *dataset.OpenPR) {
			o.PRID, o.OpenedAt, o.ReadyAt, o.ReadySince, o.LastEventAt = 90, at, ptr(at), ptr(at), at
		}))
	}
	bitbucketFrom := func(at time.Time) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) { ds.Meta.DataStart.Bitbucket = ptr(at) }
	}
	pipelineFrom := func(at time.Time) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) { ds.Meta.DataStart.Pipeline = ptr(at) }
	}
	untraced := func(ds *dataset.Dataset) {
		for i := range ds.PRs {
			ds.PRs[i].Environments = []dataset.EnvironmentDeploy{}
		}
	}
	noArgo := func(ds *dataset.Dataset) { ds.Meta.DataStart.ArgoCD = nil }
	noNoergler := func(ds *dataset.Dataset) { ds.Meta.DataStart.Noergler = nil }
	mid := cur.From.Add(10 * 24 * time.Hour).UTC()
	for _, c := range []struct {
		name      string
		cur, prev func(*dataset.Dataset)
		want      []string
	}{
		{"quiet", noop, noop, []string{}},
		{"a forgotten PR", forgotten, noop, []string{}},
		{"a team without noergler", noNoergler, noNoergler, []string{}},
		{"bitbucket lost events now", silence(dataset.SourceBitbucket, 3, mid), noop, []string{QuietSourceGap}},
		{"bitbucket lost events before", noop, silence(dataset.SourceBitbucket, 3, prev.From.Add(5*24*time.Hour)),
			[]string{QuietSourceGap}},
		{"bitbucket silent, nothing unseen", silence(dataset.SourceBitbucket, 0, mid), noop, []string{}},
		{"noergler silent mid-period", silence(dataset.SourceNoergler, 0, mid), noop, []string{}},
		{"argocd silent mid-period", silence(dataset.SourceArgoCD, 0, mid), noop, []string{}},
		{"bitbucket starts in the previous period", bitbucketFrom(prev.From.Add(24 * time.Hour)),
			bitbucketFrom(prev.From.Add(24 * time.Hour)), []string{QuietNoBaseline}},
		{"pipeline starts in the period", pipelineFrom(mid), pipelineFrom(mid), []string{QuietNoBaseline, QuietSourceGap}},
		{"no PR traced", untraced, noop, []string{QuietNotMeasurable}},
		{"no argo cd", noArgo, noArgo, []string{QuietNotMeasurable}},
		{"a jam", jam, noop, []string{QuietFindings}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := assessQuiet(c.cur, c.prev)
			if !slices.Equal(a.QuietReasons, c.want) || a.Quiet != (len(c.want) == 0) {
				t.Errorf("quiet %v reasons %v, want %v; signals %+v", a.Quiet, a.QuietReasons, c.want, a.Signals)
			}
		})
	}
	// Every source new since the previous period began: no number compares.
	since := func(ds *dataset.Dataset) {
		at := ptr(prev.From.Add(48 * time.Hour))
		ds.Meta.DataStart = dataset.SourceTimes{Bitbucket: at, Pipeline: at, ArgoCD: at, Noergler: at}
	}
	a := assessQuiet(since, since)
	if !slices.Equal(a.QuietReasons, []string{QuietNoBaseline}) || len(a.Limits) != 4 {
		t.Errorf("all sources new: reasons %v, limits %+v", a.QuietReasons, a.Limits)
	}
	for _, f := range a.Team {
		if f.Verdict != ChangeNone {
			t.Errorf("%s compared without a previous period: %+v", f.ID, f)
		}
	}
	// The silence names its source and span, for the page to word.
	a = assessQuiet(silence(dataset.SourceBitbucket, 3, mid), noop)
	if len(a.Limits) != 1 || a.Limits[0].Kind != LimitSilence || a.Limits[0].Source != SourceBitbucket ||
		!a.Limits[0].From.Equal(mid) || a.Limits[0].To == nil {
		t.Errorf("limits = %+v", a.Limits)
	}
	expectNear(t, "traced", a.Coverage.ToProd.Rate, 1)
}

// Lost Bitbucket events make its counts read low: no worsening (or gain)
// is raised from them, in either period. Without unseen runs the same
// silence is a team that did nothing, and its numbers compare.
func TestLostEventsCompareNothing(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	lost := func(unseen int) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) {
			from := cur.From.Add(3 * 24 * time.Hour).UTC()
			ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: from, To: from.Add(80 * time.Hour), UnseenRuns: unseen}}
		}
	}
	for _, c := range []struct {
		unseen int
		want   bool
	}{{0, true}, {2, false}} {
		a := Assess(quietData(cur, 20, lost(c.unseen)), quietData(prev, 30), Params{Period: cur, Previous: prev}, History{})
		if got := signal(a, "change:merged_per_week") != nil; got != c.want {
			t.Errorf("%d unseen runs: merged PRs card %v, want %v", c.unseen, got, c.want)
		}
	}
}

// No question without a move: approvals alone ask nothing. A notable move
// of an undirected number is the question, and stands out.
func TestQuestionOnlyOnMove(t *testing.T) {
	a := assessQuiet(noop, noop)
	if a.Question != nil {
		t.Fatalf("question without a move: %+v", a.Question)
	}
	// Half the approvals came after a comment before, none now: 6 of 12.
	half := func(ds *dataset.Dataset) {
		for i := range ds.PRs {
			ds.PRs[i].ApprovedOnFirstTouch = i%2 == 0
		}
	}
	a = assessQuiet(noop, half)
	if a.Question == nil || a.Question.ID != "question:first_touch" {
		t.Fatalf("question = %+v", a.Question)
	}
	if a.Quiet || !slices.Equal(a.QuietReasons, []string{QuietFindings}) {
		t.Errorf("a question left the period quiet: %v", a.QuietReasons)
	}
	// One approval of 12 more without a comment is 8 points and one case:
	// no news, no question.
	one := func(ds *dataset.Dataset) { ds.PRs[0].ApprovedOnFirstTouch = false }
	if a = assessQuiet(noop, one); a.Question != nil {
		t.Errorf("a question on one case: %+v", a.Question)
	}
}

func TestCaseGate(t *testing.T) {
	notable := Change{Verdict: ChangeWorse, Directed: true, Magnitude: ptr(MagnitudeNotable), Score: ptr(-0.3)}
	for _, c := range []struct {
		cur, prev   float64
		nCur, nPrev int
		want        string
	}{
		{11.0 / 12, 1, 12, 12, MagnitudeMinor},   // one failure of 12: 8 points, one case
		{10.0 / 12, 1, 12, 12, MagnitudeNotable}, // two
		{0.90, 0.95, 40, 20, MagnitudeNotable},   // 5 points on 20 cases and more
		{0.90, 0.96, 40, 15, MagnitudeMinor},     // 6 points of the smaller 15: under one case
	} {
		got := *caseGate(notable, ptr(c.cur), ptr(c.prev), c.nCur, c.nPrev).Magnitude
		if got != c.want {
			t.Errorf("%v of %d against %v of %d = %s, want %s", c.cur, c.nCur, c.prev, c.nPrev, got, c.want)
		}
	}
	minor := Change{Verdict: ChangeWorse, Directed: true, Magnitude: ptr(MagnitudeMinor)}
	if got := caseGate(minor, ptr(0.5), ptr(1.0), 12, 12); *got.Magnitude != MagnitudeMinor {
		t.Errorf("a minor move made notable: %s", *got.Magnitude)
	}
}

// Deploy frequency on few releases is news only by a step of two or more:
// 1 to 2 releases doubles the rate and is no card; 1 to 3 is; on ten
// releases and more the rate decides.
func TestReleaseGate(t *testing.T) {
	notable := Change{Verdict: ChangeBetter, Directed: true, Magnitude: ptr(MagnitudeNotable), Score: ptr(1.0)}
	for _, c := range []struct {
		cur, prev int
		want      string
	}{{2, 1, MagnitudeMinor}, {3, 1, MagnitudeNotable}, {13, 10, MagnitudeNotable}, {0, 1, MagnitudeMinor}} {
		if got := *releaseGate(notable, c.cur, c.prev).Magnitude; got != c.want {
			t.Errorf("%d against %d releases = %s, want %s", c.cur, c.prev, got, c.want)
		}
	}
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	releases := func(p Period, k int, infra bool) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) {
			for i := range k {
				ds.Deploys = append(ds.Deploys, deploy(func(d *dataset.Deploy) {
					d.Revision, d.At, d.Infra = fmt.Sprintf("r%d", i), p.To.Add(-time.Duration(1+i)*24*time.Hour).UTC(), infra
				}))
			}
		}
	}
	a := Assess(quietData(cur, 12, releases(cur, 2, false)), quietData(prev, 12, releases(prev, 1, false)),
		Params{Period: cur, Previous: prev}, History{})
	if s := signal(a, "change:deploy_frequency"); s != nil {
		t.Errorf("1 to 2 releases raised %+v", s)
	}
	for _, f := range a.Team {
		if f.ID == "team.deploy_frequency" && (f.Verdict != ChangeBetter || *f.Magnitude != MagnitudeMinor) {
			t.Errorf("1 to 2 releases = %+v, want better, minor", f)
		}
	}
	a = Assess(quietData(cur, 12, releases(cur, 3, false)), quietData(prev, 12, releases(prev, 1, false)),
		Params{Period: cur, Previous: prev}, History{})
	if s := signal(a, "change:deploy_frequency"); s == nil || s.Tone != ToneGood {
		t.Errorf("1 to 3 releases = %+v", s)
	}
	// Infrastructure Apps' deploys are in no judged number: twelve of them
	// against none move nothing.
	a = Assess(quietData(cur, 12, releases(cur, 12, true)), quietData(prev, 12), Params{Period: cur, Previous: prev}, History{})
	if s := signal(a, "change:deploy_frequency"); s != nil {
		t.Errorf("infrastructure deploys raised %+v", s)
	}
}

// The dependency bots' builds raise nothing: a bot's branch failing, or a
// bot PR's PR-<n> build, is no quality problem of the team's. The same
// failures on a person's branch are.
func TestBotBuildsRaiseNothing(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	runs := func(p Period, branch, kind string, failures int) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) {
			for i := range HotspotMinRuns + 2 {
				at := p.To.Add(-time.Duration(2+i) * 24 * time.Hour).UTC()
				status := "SUCCESS"
				if i < failures {
					status = "FAILURE"
				}
				ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
					r.Branch, r.BranchKind, r.PipelineName, r.RunID = branch, kind, "kasse-api/"+branch, fmt.Sprint(branch, i)
					r.StartedAt, r.FinishedAt, r.DurationS, r.Status = &at, ptr(at.Add(10*time.Minute)), n(600), s(status)
					r.BuildType = nil
				}))
			}
		}
	}
	botPR := func(ds *dataset.Dataset) {
		ds.OpenPRs = append(ds.OpenPRs, openPR(func(o *dataset.OpenPR) {
			o.PRID, o.Bot, o.LastEventAt = 7, true, cur.To.Add(-24*time.Hour).UTC()
		}))
	}
	for _, c := range []struct {
		name, branch, kind string
		extra              func(*dataset.Dataset)
		want               bool
	}{
		{"a person's branch", "feature/kas-1-pay", dataset.BranchFeature, noop, true},
		{"renovate", "renovate/lib-1.x", dataset.BranchOther, noop, false},
		{"a bot PR's build", "PR-7", dataset.BranchPR, botPR, false},
	} {
		a := Assess(quietData(cur, 12, runs(cur, c.branch, c.kind, 8), c.extra), quietData(prev, 12, runs(prev, c.branch, c.kind, 0)),
			Params{Period: cur, Previous: prev}, History{})
		got := slices.ContainsFunc(a.Signals, func(s Signal) bool { return s.Kind == SignalFailingMore })
		if got != c.want {
			t.Errorf("%s: failing more %v, want %v (%+v)", c.name, got, c.want, a.Signals)
		}
	}
}

// Agent waits: a hint, and only from two minutes on.
func TestAgentWaitFloor(t *testing.T) {
	for _, c := range []struct {
		wait, build float64
		want        bool
	}{{100, 150, false}, {160, 300, true}, {100, 300, false}} {
		a := &assessor{gaps: map[string]bool{}}
		a.m.Build.Kinds = []BuildStats{{Kind: KindMain, Waiting: Spread{N: 12, P50: ptr(c.wait)}, Duration: Spread{N: 12, P50: ptr(c.build)}}}
		a.buildSignals()
		s := signal(Assessment{Signals: a.out}, "agent_wait:main")
		if (s != nil) != c.want || s != nil && s.Tone != ToneHint {
			t.Errorf("wait %v of %v = %+v, want %v as a hint", c.wait, c.build, s, c.want)
		}
	}
}
