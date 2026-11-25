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
			ds.Meta.Silences = []dataset.Silence{{Source: src, From: from, To: from.Add(80 * time.Hour), UnseenRuns: unseen,
				Lost: src == dataset.SourceBitbucket && unseen > 0}}
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
		// Every number compared here is Bitbucket's: with its events lost,
		// none is, and nothing else was.
		{"bitbucket lost events now", silence(dataset.SourceBitbucket, 3, mid), noop, []string{QuietNoBaseline, QuietSourceGap}},
		{"bitbucket lost events before", noop, silence(dataset.SourceBitbucket, 3, prev.From.Add(5*24*time.Hour)),
			[]string{QuietNoBaseline, QuietSourceGap}},
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
	if len(a.Limits) != 1 || a.Limits[0].Kind != LimitSilence || a.Limits[0].Source != dataset.SourceBitbucket ||
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
			ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: from, To: from.Add(80 * time.Hour),
				UnseenRuns: unseen, Lost: unseen > 0}}
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

// The bots' builds (dataset.Run.Bot, flagged at load from the config's
// branch prefixes and the bot PRs' PR-<n>) raise nothing and count in no
// build number of the page either: a bot's failed build is no quality
// problem of the team's. Their count and CI time stand apart. The same
// failures on a person's branch are a finding.
func TestBotBuildsRaiseNothing(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	runs := func(p Period, branch, kind string, bot bool, failures int) func(*dataset.Dataset) {
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
					r.BuildType, r.Bot = nil, bot
				}))
			}
		}
	}
	for _, c := range []struct {
		name, branch, kind string
		bot, want          bool
	}{
		{"a person's branch", "feature/kas-1-pay", dataset.BranchFeature, false, true},
		{"a bot's branch", "renovate/lib-1.x", dataset.BranchOther, true, false},
		{"a bot PR's build", "PR-7", dataset.BranchPR, true, false},
	} {
		cd, pd := quietData(cur, 12, runs(cur, c.branch, c.kind, c.bot, 8)), quietData(prev, 12, runs(prev, c.branch, c.kind, c.bot, 0))
		a := Assess(cd, pd, Params{Period: cur, Previous: prev}, History{})
		got := slices.ContainsFunc(a.Signals, func(s Signal) bool { return s.Kind == SignalFailingMore })
		if got != c.want {
			t.Errorf("%s: failing more %v, want %v (%+v)", c.name, got, c.want, a.Signals)
		}
		b := Compute(cd, pd, nil, Params{Period: cur, Previous: prev}).Build
		if counted := len(b.Kinds) > 0; counted == c.bot || (c.bot && (b.BotRuns.Runs != HotspotMinRuns+2 || b.BotRuns.CISeconds != float64(600*(HotspotMinRuns+2)))) {
			t.Errorf("%s: kinds %+v, bot runs %+v", c.name, b.Kinds, b.BotRuns)
		}
	}
}

// A period compared on numbers too thin to tell is no baseline: three
// merged PRs, no release and no jammed repo in either period judge only
// counts of nothing against nothing. Not quiet: nothing was compared.
func TestQuietNeedsASampledBaseline(t *testing.T) {
	c, p := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	a := Assess(quietData(c, 3), quietData(p, 3), Params{Period: c, Previous: p}, History{})
	if a.Quiet || !slices.Contains(a.QuietReasons, QuietNoBaseline) {
		t.Errorf("quiet %v reasons %v on three PRs a period", a.Quiet, a.QuietReasons)
	}
	// Twelve compare: the same period with enough PRs is quiet.
	if a = assessQuiet(noop, noop); !a.Quiet {
		t.Errorf("twelve PRs a period: reasons %v", a.QuietReasons)
	}
}

// Bitbucket starting hours before the period's end makes no weekly rate:
// three PRs over six hours are no 84 a week.
func TestMergedPerWeekOnAShortSpan(t *testing.T) {
	c, p := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	late := func(ds *dataset.Dataset) { ds.Meta.DataStart.Bitbucket = ptr(c.To.Add(-6 * time.Hour).UTC()) }
	m := Compute(quietData(c, 3, late), quietData(p, 0), nil, Params{Period: c, Previous: p})
	if m.Throughput.MergedPerWeek != nil || m.Throughput.Merged != 3 || m.Throughput.Since == nil {
		t.Errorf("merged %d, per week %v, since %v", m.Throughput.Merged, m.Throughput.MergedPerWeek, m.Throughput.Since)
	}
	// A week and more of data keeps its rate.
	week := func(ds *dataset.Dataset) { ds.Meta.DataStart.Bitbucket = ptr(c.To.Add(-8 * 24 * time.Hour).UTC()) }
	if m = Compute(quietData(c, 3, week), quietData(p, 0), nil, Params{Period: c, Previous: p}); m.Throughput.MergedPerWeek == nil {
		t.Error("eight days of data without a rate")
	}
}

// Why a period is not measurable, said by the backend: a missing release is
// no tracing gap, and a period whose merged PRs are all libraries' had
// nothing to trace.
func TestNotMeasurableSubReasons(t *testing.T) {
	cur := ClosedPeriod(4, now, zurich)
	untraced := func(ds *dataset.Dataset) {
		for i := range ds.PRs {
			ds.PRs[i].Environments = []dataset.EnvironmentDeploy{}
		}
	}
	deployed := func(ds *dataset.Dataset) {
		ds.Deploys = append(ds.Deploys, deploy(func(d *dataset.Deploy) { d.At = cur.To.Add(-48 * time.Hour).UTC() }))
	}
	// Re-syncs of what ran already: nothing new went to production.
	resynced := func(ds *dataset.Dataset) {
		ds.Deploys = append(ds.Deploys, deploy(func(d *dataset.Deploy) {
			d.At, d.Kind, d.NewImages = cur.To.Add(-48*time.Hour).UTC(), s("sync"), []string{}
		}))
	}
	libraries := func(ds *dataset.Dataset) {
		for i := range ds.PRs {
			ds.PRs[i].Library = true
		}
	}
	for _, c := range []struct {
		name string
		mod  []func(*dataset.Dataset)
		want string
	}{
		{"measured", nil, ""},
		{"no bitbucket", []func(*dataset.Dataset){func(ds *dataset.Dataset) { ds.Meta.DataStart.Bitbucket = nil }}, NotMeasurableBitbucket},
		{"no argo cd", []func(*dataset.Dataset){func(ds *dataset.Dataset) { ds.Meta.DataStart.ArgoCD = nil }}, NotMeasurableArgoCD},
		{"no merged PR", []func(*dataset.Dataset){func(ds *dataset.Dataset) { ds.PRs = nil }}, NotMeasurableNoMerged},
		{"nothing went to production", []func(*dataset.Dataset){untraced}, NotMeasurableNoRelease},
		{"only re-syncs went to production", []func(*dataset.Dataset){untraced, resynced}, NotMeasurableNoRelease},
		{"deploys, none traced", []func(*dataset.Dataset){untraced, deployed}, NotMeasurableToProd},
		{"libraries only", []func(*dataset.Dataset){untraced, libraries}, ""},
	} {
		a := assessQuiet(func(ds *dataset.Dataset) {
			for _, m := range c.mod {
				m(ds)
			}
		}, noop)
		got := ""
		if a.NotMeasurable != nil {
			got = *a.NotMeasurable
		}
		if got != c.want || slices.Contains(a.QuietReasons, QuietNotMeasurable) != (c.want != "") {
			t.Errorf("%s: not measurable %q (reasons %v), want %q", c.name, got, a.QuietReasons, c.want)
		}
		// Libraries only is its own state, no limit: said, not as a hole.
		if a.LibrariesOnly != (c.name == "libraries only") || (a.LibrariesOnly && (a.Library != 12 || a.Limited)) {
			t.Errorf("%s: libraries only %v, library %d, limited %v", c.name, a.LibrariesOnly, a.Library, a.Limited)
		}
	}
}

// A lost Bitbucket silence still running at the read has no end to name;
// silent SilentDays at the period's end too, it is that gate's limit, said
// once.
func TestOngoingSilenceLimit(t *testing.T) {
	cur := ClosedPeriod(4, now, zurich)
	from := cur.To.Add(-3 * 24 * time.Hour).UTC()
	ongoing := func(from time.Time) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) {
			ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: from, To: now.UTC(), UnseenRuns: 4,
				Lost: true, Ongoing: true}}
			ds.Meta.DataEnd.Bitbucket = ptr(from)
		}
	}
	a := assessQuiet(ongoing(from), noop)
	if len(a.Limits) != 1 || a.Limits[0].Kind != LimitSilence || a.Limits[0].To != nil || !a.Limited {
		t.Errorf("limits = %+v", a.Limits)
	}
	long := cur.To.Add(-9 * 24 * time.Hour).UTC()
	a = assessQuiet(ongoing(long), noop)
	if len(a.Limits) != 1 || a.Limits[0].Kind != LimitSilentEnd {
		t.Errorf("limits past the gate = %+v", a.Limits)
	}
}

// In a product view a build reaches its product through Bitbucket's
// commits: with its events lost, no repo's builds compare, so no hotspot is
// flagged and no repo is judged on them, as the kinds' numbers are blanked.
// The team view keeps them.
func TestHotspotsWithLostEventsInAProduct(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	runs := func(p Period, failures int) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) {
			for i := range HotspotMinRuns + 2 {
				at := p.To.Add(-time.Duration(2+i) * 24 * time.Hour).UTC()
				status := "SUCCESS"
				if i < failures {
					status = "FAILURE"
				}
				ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
					r.Branch, r.BranchKind, r.PipelineName, r.RunID = "feature/kas-1", dataset.BranchFeature, "kasse-api/feature%2Fkas-1", fmt.Sprint(i)
					r.StartedAt, r.FinishedAt, r.DurationS, r.Status, r.BuildType = &at, ptr(at.Add(10*time.Minute)), n(600), s(status), nil
				}))
			}
		}
	}
	lost := func(ds *dataset.Dataset) {
		from := cur.From.Add(3 * 24 * time.Hour).UTC()
		ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: from, To: from.Add(80 * time.Hour),
			UnseenRuns: 2, Lost: true}}
	}
	cd, pd := quietData(cur, 12, runs(cur, 8), lost), quietData(prev, 12, runs(prev, 0))
	for _, c := range []struct {
		product string
		want    bool
	}{{"", true}, {"kasse", false}} {
		a := Assess(cd, pd, Params{Period: cur, Previous: prev, Product: c.product}, History{})
		got := slices.ContainsFunc(a.Signals, func(s Signal) bool { return s.Kind == SignalFailingMore })
		if got != c.want {
			t.Errorf("product %q: failing more %v, want %v", c.product, got, c.want)
		}
		m := Compute(cd, pd, nil, Params{Period: cur, Previous: prev, Product: c.product})
		flagged := false
		for _, k := range m.Build.Hotspots.Kinds {
			for _, h := range k.Repos {
				flagged = flagged || h.FailingMore || h.Compared
			}
		}
		if flagged != c.want {
			t.Errorf("product %q: page hotspots flagged %v, want %v", c.product, flagged, c.want)
		}
	}
}

// A revision an infrastructure App and a product's App both deployed is
// the product's release, counted once: never infrastructure's too.
func TestInfraRevisionCountsOnce(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	both := func(ds *dataset.Dataset) {
		at := cur.To.Add(-48 * time.Hour).UTC()
		ds.Deploys = append(ds.Deploys,
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "shared", at }),
			deploy(func(d *dataset.Deploy) {
				d.App, d.Project, d.Infra, d.Revision, d.At = "logs-prod", nil, true, "shared", at
			}),
			deploy(func(d *dataset.Deploy) {
				d.App, d.Project, d.Infra, d.Revision, d.At = "logs-prod", nil, true, "own", at
			}))
	}
	f := Compute(quietData(cur, 12, both), quietData(prev, 12), nil, Params{Period: cur, Previous: prev}).Delivery.Frequency
	if f.Releases != 1 || f.Infra != 1 {
		t.Errorf("releases %d, infra %d", f.Releases, f.Infra)
	}
}

// The page's flags the UI words without deciding: nothing compared (the
// header claims no comparison, the assessment says so), and a product
// without deploys of its own beside deploys without a product.
func TestPageFlags(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	since := func(ds *dataset.Dataset) {
		at := ptr(prev.From.Add(5 * 24 * time.Hour))
		ds.Meta.DataStart = dataset.SourceTimes{Bitbucket: at, Pipeline: at, ArgoCD: at, Noergler: at}
	}
	m := Compute(quietData(cur, 12, since), quietData(prev, 12, since), nil, Params{Period: cur, Previous: prev})
	a := Assess(quietData(cur, 12, since), quietData(prev, 12, since), Params{Period: cur, Previous: prev}, History{})
	if m.Comparison.Any || !a.NothingCompared || !a.Limited {
		t.Errorf("all sources new: any %v, nothing compared %v, limited %v", m.Comparison.Any, a.NothingCompared, a.Limited)
	}
	m = Compute(quietData(cur, 12), quietData(prev, 12), nil, Params{Period: cur, Previous: prev})
	if a = assessQuiet(noop, noop); !m.Comparison.Any || a.NothingCompared || a.Limited {
		t.Errorf("covered: any %v, nothing compared %v, limited %v", m.Comparison.Any, a.NothingCompared, a.Limited)
	}
	unattributed := func(ds *dataset.Dataset) {
		ds.Deploys = append(ds.Deploys, deploy(func(d *dataset.Deploy) {
			d.App, d.Project, d.Revision, d.At = "neu-prod", nil, "u1", cur.To.Add(-48*time.Hour).UTC()
		}))
	}
	for _, c := range []struct {
		product string
		want    bool
	}{{"kasse", true}, {"", false}} {
		f := Compute(quietData(cur, 12, unattributed), quietData(prev, 12), nil,
			Params{Period: cur, Previous: prev, Product: c.product}).Delivery.Frequency
		if f.MaybeUnattributed != c.want || f.Unattributed != 1 {
			t.Errorf("product %q: maybe unattributed %v, unattributed %d", c.product, f.MaybeUnattributed, f.Unattributed)
		}
	}
}

// Lost Bitbucket events: in a product view the deploy and build numbers,
// which reach a product through Bitbucket's commits, compare nothing on
// the page either; in the team view they do.
func TestLostEventsOnThePage(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	lost := func(ds *dataset.Dataset) {
		from := cur.From.Add(3 * 24 * time.Hour).UTC()
		ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: from, To: from.Add(80 * time.Hour),
			UnseenRuns: 2, Lost: true}}
	}
	releases := func(p Period, k int) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) {
			for i := range k {
				ds.Deploys = append(ds.Deploys, deploy(func(d *dataset.Deploy) {
					d.Revision, d.At = fmt.Sprintf("%s-%d", p.From.Format("0102"), i), p.To.Add(-time.Duration(1+i)*12*time.Hour).UTC()
				}))
			}
		}
	}
	cd, pd := quietData(cur, 12, lost, releases(cur, 12)), quietData(prev, 12, releases(prev, 12))
	for _, c := range []struct {
		product string
		want    string
	}{{"", ChangeSame}, {"kasse", ChangeNone}} {
		m := Compute(cd, pd, nil, Params{Period: cur, Previous: prev, Product: c.product})
		if m.Comparison.DeployFreq.Verdict != c.want || m.Comparison.MergedPerWeek.Verdict != ChangeNone {
			t.Errorf("product %q: deploy frequency %+v, merged per week %+v", c.product, m.Comparison.DeployFreq,
				m.Comparison.MergedPerWeek)
		}
	}
}

// A repo is judged on what can raise a finding on it, never on its open
// PRs: many open behind a freeze read as fine beside a jam. While
// Bitbucket's events are lost, its merged PRs judge nothing. A pipeline that
// names no repository is no repo.
func TestJudgeableRepos(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	open := func(ds *dataset.Dataset) {
		for i := range OpenJamMin + 2 {
			at := cur.To.Add(-time.Duration(2+i) * 24 * time.Hour).UTC()
			ds.OpenPRs = append(ds.OpenPRs, openPR(func(o *dataset.OpenPR) {
				o.Repo, o.Project, o.PRID, o.OpenedAt, o.ReadyAt, o.ReadySince, o.LastEventAt =
					"portal/portal-web", "portal", int64(70+i), at, ptr(at), ptr(at), at
			}))
		}
	}
	scanner := func(ds *dataset.Dataset) {
		at := cur.To.Add(-48 * time.Hour).UTC()
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.Repo, r.Project, r.PipelineName, r.Branch, r.StartedAt, r.FinishedAt = nil, nil, "scanner", "", &at, ptr(at.Add(time.Minute))
		}))
	}
	a := Assess(quietData(cur, 12, open, scanner), quietData(prev, 12), Params{Period: cur, Previous: prev}, History{})
	// kasse-api: twelve merged PRs in both periods; portal-web: open PRs only.
	if a.Active != 2 || a.Calm != 1 || a.Unjudged != 1 {
		t.Errorf("active %d, calm %d, unjudged %d", a.Active, a.Calm, a.Unjudged)
	}
	lost := func(ds *dataset.Dataset) {
		from := cur.From.Add(3 * 24 * time.Hour).UTC()
		ds.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: from, To: from.Add(80 * time.Hour),
			UnseenRuns: 2, Lost: true}}
	}
	if a = Assess(quietData(cur, 12, lost), quietData(prev, 12), Params{Period: cur, Previous: prev}, History{}); a.Calm != 0 || a.Unjudged != 1 {
		t.Errorf("lost: calm %d, unjudged %d", a.Calm, a.Unjudged)
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
