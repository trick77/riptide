package metrics

import (
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

func TestPreviousPeriod(t *testing.T) {
	cur, prev := p4(), PreviousPeriod(4, now, zurich)
	if !prev.To.Equal(cur.From) || len(prev.Weeks) != 4 || !prev.Weeks[3].End.Equal(cur.From) {
		t.Fatalf("previous = %+v, current from %v", prev, cur.From)
	}
	if prev.Weeks[0].KW != cur.Weeks[0].KW-4 || !near(prev.Span, 4) {
		t.Errorf("previous weeks = %+v, span %v", prev.Weeks, prev.Span)
	}
}

// judge is metric{d, s}.judge, for tables of directions and scales.
func judge(cur, prev side, d direction, s scale, incomplete bool) Change {
	return metric{d, s}.judge(cur, prev, incomplete)
}

func TestJudge(t *testing.T) {
	v := func(x float64) side { return side{ptr(x), true} }
	cases := []struct {
		name       string
		cur, prev  side
		d          direction
		s          scale
		incomplete bool
		verdict    string
		reason     string
	}{
		{"10 % slower is the same", v(110), v(100), down, relative, false, ChangeSame, ""},
		{"11 % slower is worse", v(111), v(100), down, relative, false, ChangeWorse, ""},
		{"faster is better", v(80), v(100), down, relative, false, ChangeBetter, ""},
		{"more often is better", v(3), v(2), up, relative, false, ChangeBetter, ""},
		{"3 points is the same", v(0.94), v(0.97), up, points, false, ChangeSame, ""},
		{"97 % to 88 % is worse", v(0.88), v(0.97), up, points, false, ChangeWorse, ""},
		{"undirected", v(0.5), v(0.2), undirected, points, false, ChangeNone, ReasonUndirected},
		{"incomplete first", side{nil, false}, v(1), up, relative, true, ChangeNone, ReasonIncomplete},
		{"low sample", v(1), side{ptr(2.0), false}, up, relative, false, ChangeNone, ReasonLowSample},
		{"no value", v(1), side{nil, true}, up, relative, false, ChangeNone, ReasonNoValue},
		{"from 0 up", v(1), v(0), up, relative, false, ChangeBetter, ""},
		{"from 0 down", v(1), v(0), down, relative, false, ChangeWorse, ""},
		{"0 and 0", v(0), v(0), up, relative, false, ChangeSame, ""},
		{"a count judges relative", v(2), v(1), down, count, false, ChangeWorse, ""},
		{"a count from 0", v(1), v(0), down, count, false, ChangeWorse, ""},
		{"a count within 10 %", v(21), v(20), down, count, false, ChangeSame, ""},
	}
	for _, c := range cases {
		got := judge(c.cur, c.prev, c.d, c.s, c.incomplete)
		reason := ""
		if got.Reason != nil {
			reason = *got.Reason
		}
		if got.Verdict != c.verdict || reason != c.reason {
			t.Errorf("%s: %+v (reason %q)", c.name, got, reason)
		}
		// A value it does not compare with is not shown either.
		if (reason == ReasonIncomplete || reason == ReasonLowSample) && got.Prev != nil {
			t.Errorf("%s: prev %v shown", c.name, *got.Prev)
		}
	}
}

func TestScore(t *testing.T) {
	v := func(x float64) side { return side{ptr(x), true} }
	cases := []struct {
		name      string
		cur, prev side
		d         direction
		s         scale
		want      float64
	}{
		{"longer is negative", v(41), v(24), down, relative, -math.Log2(41.0 / 24)},
		{"more often is positive", v(3), v(2), up, relative, math.Log2(1.5)},
		{"a doubling is the end", v(2), v(1), up, relative, 1},
		{"past a halving clamps", v(10), v(100), down, relative, 1},
		{"past a doubling clamps", v(500), v(100), down, relative, -1},
		{"within the tolerance nothing", v(110), v(100), down, relative, 0},
		{"just past the tolerance", v(111), v(100), down, relative, -math.Log2(1.11)},
		{"from 0 up", v(1), v(0), up, relative, 1},
		{"from 0 down", v(1), v(0), down, relative, -1},
		{"to 0 down", v(0), v(4), down, relative, 1},
		{"0 and 0", v(0), v(0), down, relative, 0},
		{"a share in points", v(0.88), v(0.97), up, points, -0.36},
		{"failures from 0 % within the tolerance", v(0.03), v(0), down, points, 0},
		{"success from 100 % within the tolerance", v(0.97), v(1), up, points, 0},
		{"failures from 0 % past it", v(0.04), v(0), down, points, -0.16},
		{"a share clamps", v(0.2), v(0.9), up, points, -1},
		{"a share the same", v(0.5), v(0.5), down, points, 0},
		// One jammed repo more is no doubling.
		{"a count from 0 by one is damped", v(1), v(0), down, count, -math.Log2(1.5)},
		{"a count from 1 by one is damped", v(2), v(1), down, count, -math.Log2(4.0 / 3)},
		{"a large count barely", v(20), v(10), down, count, -math.Log2(22.0 / 12)},
		{"a count halving to 0", v(0), v(2), down, count, 1},
		{"a count the same", v(3), v(3), down, count, 0},
		{"a count within 10 %", v(21), v(20), down, count, 0},
	}
	for _, c := range cases {
		got := judge(c.cur, c.prev, c.d, c.s, false)
		if got.Score == nil || !near(*got.Score, c.want) {
			t.Errorf("%s: score %v, want %v", c.name, got.Score, c.want)
			continue
		}
		if math.Signbit(*got.Score) && *got.Score == 0 {
			t.Errorf("%s: -0", c.name)
		}
		// The score never contradicts the verdict: same is 0, the sign the
		// verdict's.
		if got.Verdict == ChangeWorse && *got.Score >= 0 || got.Verdict == ChangeBetter && *got.Score <= 0 ||
			(got.Verdict == ChangeSame) != (*got.Score == 0) {
			t.Errorf("%s: score %v, verdict %s", c.name, *got.Score, got.Verdict)
		}
	}
	// No verdict, no score; undirected included.
	for _, c := range []Change{
		judge(v(0.5), v(0.2), undirected, points, false),
		judge(v(1), v(2), up, relative, true),
		judge(v(1), side{ptr(2.0), false}, up, relative, false),
		judge(v(1), side{nil, true}, up, relative, false),
	} {
		if c.Score != nil {
			t.Errorf("%+v: score %v", c, *c.Score)
		}
	}
}

func TestDirected(t *testing.T) {
	v := func(x float64) side { return side{ptr(x), true} }
	few := side{ptr(1.0), false}
	for _, c := range []struct {
		name string
		got  Change
		want bool
	}{
		{"undirected", judge(v(0.5), v(0.2), undirected, points, false), false},
		{"undirected, low sample", judge(few, v(0.2), undirected, points, false), false},
		{"undirected, incomplete", judge(v(1), v(2), undirected, relative, true), false},
		{"up", judge(v(3), v(2), up, relative, false), true},
		{"down, low sample", judge(few, v(2), down, relative, false), true},
		{"down, no value", judge(v(1), side{nil, true}, down, relative, false), true},
		// The no-value fallbacks read the metric's own direction.
		{"lead time fallback", leadTimeMetric.none(ReasonNoValue), true},
		{"repo duration fallback", repoDurationMetric.none(ReasonNoValue), true},
		{"repo failure fallback", repoFailureMetric.none(ReasonNoValue), true},
		{"open PRs fallback", openPRsMetric.none(ReasonNoValue), false},
	} {
		if c.got.Directed != c.want {
			t.Errorf("%s: %+v", c.name, c.got)
		}
	}
	params := Params{Period: p4(), Previous: PreviousPeriod(4, now, zurich)}
	cmp := Compute(leadData(12, 2), leadData(12, 3), nil, params).Comparison
	for name, c := range map[string]Change{
		"first touch": cmp.FirstTouch, "untracked": cmp.Untracked, "open PRs": cmp.OpenPRs,
		"cost per KLOC": cmp.CostPerKloc, "wasted share": cmp.WastedShare,
	} {
		if c.Directed {
			t.Errorf("%s directed: %+v", name, c)
		}
	}
	for name, c := range map[string]Change{
		"deploy frequency": cmp.DeployFreq, "deploy success": cmp.DeploySuccess, "lead time": cmp.LeadTime,
		"review": cmp.ReviewTime, "within a day": cmp.WithinDay, "jammed": cmp.OpenJammed,
	} {
		if !c.Directed {
			t.Errorf("%s undirected: %+v", name, c)
		}
	}

	// Argo CD silent for the team: nothing measured, not too few.
	silent := func(ds *dataset.Dataset) { ds.Meta.DataStart.ArgoCD = nil }
	cmp = Compute(leadData(12, 2, silent), leadData(12, 3, silent), nil, params).Comparison
	for name, c := range map[string]Change{"frequency": cmp.DeployFreq, "success": cmp.DeploySuccess} {
		if c.Reason == nil || *c.Reason != ReasonNotReporting || !c.Directed || c.Score != nil {
			t.Errorf("silent argo %s: %+v", name, c)
		}
	}
}

// leadData is n human commits to prod, each taking days.
func leadData(count int, days float64, mod ...func(*dataset.Dataset)) *dataset.Dataset {
	return data(append([]func(*dataset.Dataset){func(ds *dataset.Dataset) {
		for range count {
			ds.LeadTimes = append(ds.LeadTimes, leadTimeRow(func(l *dataset.LeadTime) {
				l.LeadTimeS = int64(days * 86400)
			}))
		}
	}}, mod...)...)
}

func TestCompareWithThePreviousPeriod(t *testing.T) {
	params := Params{Period: p4(), Previous: PreviousPeriod(4, now, zurich)}
	m := Compute(leadData(12, 2), leadData(12, 3), nil, params)
	c := m.Comparison
	if !c.From.Equal(params.Previous.From) || !c.To.Equal(params.Previous.To) {
		t.Errorf("period = %v..%v", c.From, c.To)
	}
	if c.LeadTime.Verdict != ChangeBetter || c.LeadTime.Prev == nil || *c.LeadTime.Prev != 3*86400 {
		t.Errorf("lead time = %+v", c.LeadTime)
	}
	expectNear(t, "lead time score", c.LeadTime.Score, math.Log2(1.5))
	// No PRs, runs or reviews in either period: nothing to tell.
	if c.ReviewTime.Verdict != ChangeNone || *c.ReviewTime.Reason != ReasonLowSample || c.ReviewTime.Score != nil {
		t.Errorf("review = %+v", c.ReviewTime)
	}
	if len(c.Build) != 0 {
		t.Errorf("build = %+v", c.Build)
	}

	// Argo CD started inside the previous period: it reads low, no verdict.
	late := func(ds *dataset.Dataset) { ds.Meta.DataStart.ArgoCD = agoP(30) }
	c = Compute(leadData(12, 2, late), leadData(12, 3, late), nil, params).Comparison
	if c.LeadTime.Reason == nil || *c.LeadTime.Reason != ReasonIncomplete || c.DeployFreq.Verdict != ChangeNone {
		t.Errorf("late argo = %+v / %+v", c.LeadTime, c.DeployFreq)
	}

	// First deploys in the previous period are no releases: its rate reads
	// low, so the frequency gets no verdict.
	releases := func(first int) func(*dataset.Dataset) {
		return func(ds *dataset.Dataset) {
			for i := range 10 {
				ds.Deploys = append(ds.Deploys, deploy(func(d *dataset.Deploy) {
					d.Revision, d.At = fmt.Sprint("r", i), ago(1+float64(i))
					if i < first {
						d.Kind = s("first_seen")
					}
				}))
			}
		}
	}
	earlier := func(first int) *dataset.Dataset {
		ds := data(releases(first))
		for i := range ds.Deploys {
			ds.Deploys[i].At = ds.Deploys[i].At.Add(-28 * 24 * time.Hour)
		}
		return ds
	}
	c = Compute(data(releases(0)), earlier(6), nil, params).Comparison
	if c.DeployFreq.Reason == nil || *c.DeployFreq.Reason != ReasonIncomplete {
		t.Errorf("first deploys = %+v", c.DeployFreq)
	}
	// One new App among the releases still compares.
	if c := Compute(data(releases(0)), earlier(1), nil, params).Comparison; c.DeployFreq.Verdict != ChangeBetter {
		t.Errorf("one new app = %+v", c.DeployFreq)
	}
	if c.DeploySuccess.Verdict != ChangeSame {
		t.Errorf("deploy success = %+v", c.DeploySuccess)
	}

	// Builds: a team that never reports a build type compares its master
	// runs alike in both periods; one whose first build type falls into the
	// previous period does not compare.
	builds := func(typeFrom *time.Time, shift time.Duration) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			ds.Meta.BuildDataFrom, ds.Meta.BuildTypeFrom = agoP(60), typeFrom
			for _, r := range runs("kasse/kasse-api", 12, 600, 0, "") {
				r.StartedAt = ptr(r.StartedAt.Add(-shift))
				r.FinishedAt = ptr(r.FinishedAt.Add(-shift))
				ds.Runs = append(ds.Runs, r)
			}
		})
	}
	month := 28 * 24 * time.Hour
	c = Compute(builds(nil, 0), builds(nil, month), nil, params).Comparison
	if len(c.Build) != 1 || c.Build[0].Kind != KindMain || c.Build[0].Success.Verdict != ChangeSame ||
		c.Build[0].Compared != 1 || c.Build[0].Slower != 0 {
		t.Errorf("no build type = %+v", c.Build)
	}
	// The repo against its own previous period, end to end: twice as slow
	// is one slower repo, and a signal.
	slower := builds(nil, 0)
	for i := range slower.Runs {
		slower.Runs[i].DurationS = n(1200)
	}
	sm := Compute(slower, builds(nil, month), nil, params)
	if c := sm.Comparison.Build; len(c) != 1 || c[0].Slower != 1 || c[0].Compared != 1 {
		t.Errorf("slower = %+v", c)
	}
	sa := Assess(slower, builds(nil, month), params, History{})
	if !slices.ContainsFunc(sa.Signals, func(s Signal) bool { return s.Kind == SignalSlower && s.Tone == ToneAttention }) {
		t.Errorf("signals = %+v", sa.Signals)
	}
	c = Compute(builds(agoP(30), 0), builds(agoP(30), month), nil, params).Comparison
	if len(c.Build) != 1 || c.Build[0].Success.Reason == nil || *c.Build[0].Success.Reason != ReasonIncomplete {
		t.Errorf("build type inside = %+v", c.Build)
	}
}

// A worsening whose number is now unknown (no PR reached production, so no
// lead time) is not resolved: nothing says it got better.
func TestUnknownNumberResolvesNothing(t *testing.T) {
	same := Change{Verdict: ChangeSame, Directed: true, Magnitude: ptr(MagnitudeSame), Score: ptr(0.0), Prev: ptr(100.0)}
	resolved := func(v *float64, c Change) bool {
		a := &assessor{
			team: []teamChange{{metric: "lead_time", label: "lead_time_p50", unit: UnitSeconds, source: SourceArgoCD, value: v, c: c}},
			h:    History{Open: map[string]Problem{"change:lead_time": {Tone: ToneAttention, Before: ptr(100.0)}}},
			gaps: map[string]bool{},
		}
		a.resolvedSignals()
		return len(a.out) == 1 && a.out[0].ID == "resolved:change:lead_time"
	}
	if resolved(nil, leadTimeMetric.none(ReasonNoValue)) {
		t.Error("an unknown lead time resolved the worsening")
	}
	if resolved(ptr(100.0), leadTimeMetric.none(ReasonLowSample)) {
		t.Error("a lead time on too few commits resolved the worsening")
	}
	if !resolved(ptr(100.0), same) {
		t.Error("a known lead time back where it was no longer resolves")
	}
}

// No stage raises a signal: some stage always takes longest, and its share
// is a ratio of medians over different PRs. The chain keeps the numbers.
func TestNoStageSignals(t *testing.T) {
	chain := Chain{Stages: []Stage{{Key: StageReview, Share: ptr(0.1)}, {Key: StageToProd, Share: ptr(0.9), Hot: true}}}
	prev := Chain{Stages: []Stage{{Key: StageCoding, Share: ptr(0.9), Hot: true}, {Key: StageToProd, Share: ptr(0.1)}}}
	a := &assessor{m: Metrics{LeadChain: chain}, pm: Metrics{LeadChain: prev}, ds: data(), prev: data()}
	a.run()
	if len(a.out) != 0 {
		t.Errorf("signals = %+v", a.out)
	}
}

func TestSharesOverThePeriod(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{
			// opened 2 days before merging: over a day
			pr(func(p *dataset.PR) { p.PRID, p.OpenedAt, p.MergedAt = 1, agoP(4), agoP(2) }),
			// exactly a day: the histogram's next bucket, so not within
			pr(func(p *dataset.PR) { p.PRID, p.OpenedAt, p.MergedAt = 5, agoP(3), agoP(2) }),
			pr(func(p *dataset.PR) { p.PRID, p.ChangeType = 2, s("bugfix") }),
			pr(func(p *dataset.PR) { p.PRID, p.ChangeType = 3, nil }),
			pr(func(p *dataset.PR) { p.PRID = 4 }),
		}
	})
	r := reviewOf(ds, p4(), filter{})
	if r.WithinDay.Count != 3 || r.WithinDay.N != 5 {
		t.Errorf("within a day = %+v", r.WithinDay)
	}
	expectNear(t, "within a day", r.WithinDay.Rate, 0.6)
	if r.MixTotal != (MixWeek{Feature: 3, Bugfix: 1, Other: 1}) {
		t.Errorf("mix = %+v", r.MixTotal)
	}
	expectNear(t, "feature share", r.MixShare.Feature, 0.6)
	expectNear(t, "hotfix share", r.MixShare.Hotfix, 0)
	if e := reviewOf(data(), p4(), filter{}); e.MixShare.Feature != nil || e.WithinDay.Rate != nil {
		t.Errorf("empty = %+v", e)
	}

	c := costs(data(func(ds *dataset.Dataset) {
		ds.Reviews = []dataset.Review{reviewRow(), reviewRow(func(r *dataset.Review) { r.Outcome = s("declined") })}
	}), filter{})
	expectNear(t, "wasted share", c.WastedShare, 0.5)
	expectNil(t, "no spend", costs(data(), filter{}).WastedShare)
}

func TestReleaseMedianOverWholeWeeks(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Meta.DataStart.ArgoCD = agoP(20) // inside KW 43: that week is left out
		for i, at := range []float64{1, 9, 10, 15, 16, 17} {
			ds.Deploys = append(ds.Deploys, deploy(func(d *dataset.Deploy) { d.Revision, d.At = fmt.Sprint("r", i), ago(at) }))
		}
	})
	f := deployFrequency(ds, p4(), filter{}, "prod")
	// KW 44: 3, KW 45: 2; KW 46 is still running.
	expectNear(t, "median week", f.MedianWeek, 2.5)
	expectNil(t, "no argo", deployFrequency(data(func(ds *dataset.Dataset) { ds.Meta.DataStart.ArgoCD = nil }), p4(),
		filter{}, "prod").MedianWeek)
}

func TestRepoHotspotsAndOutlierGroups(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Runs = append(ds.Runs, runs("kasse/slow", 10, 1000, 0, "")...)
		ds.Runs = append(ds.Runs, runs("kasse/flaky", 10, 100, 8, "FAILURE")...)
		ds.Runs = append(ds.Runs, runs("kasse/quick", 30, 100, 1, "FAILURE")...)
		// stuck together, two repos: the agents
		for i, repo := range []string{"kasse/quick", "kasse/flaky", "kasse/quick"} {
			ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
				r.Repo, r.PipelineName, r.RunID = s(repo), repo+"/master", fmt.Sprint("x", i)
				r.DurationS, r.StartedAt = n(20000), ptr(ago(3).Add(time.Duration(i)*10*time.Minute))
			}))
		}
		// stuck alone, days later: no group
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.Repo, r.PipelineName, r.RunID, r.DurationS, r.StartedAt = s("kasse/slow"), "kasse/slow/master", "y", n(30000), agoP(1)
		}))
	})
	prev := data(func(ds *dataset.Dataset) {
		ds.Runs = append(ds.Runs, runs("kasse/slow", 10, 400, 0, "")...)
		ds.Runs = append(ds.Runs, runs("kasse/flaky", 10, 100, 0, "")...)
		ds.Runs = append(ds.Runs, runs("kasse/quick", 30, 100, 1, "FAILURE")...)
	})
	b := against(ds, prev)
	k := kindOf(b.Hotspots, KindMain)
	order := repoNames(k.Repos)
	if fmt.Sprint(order) != "[kasse/flaky kasse/slow kasse/quick]" {
		t.Errorf("repos = %v", order)
	}
	if !k.Repos[0].FailingMore || !k.Repos[1].Slow || k.Repos[2].FailingMore || k.Repos[2].Slow {
		t.Errorf("flags = %+v", k.Repos)
	}
	g := b.Hotspots.OutlierGroups
	if len(g) != 1 || g[0].Runs != 3 || !g[0].Shared || fmt.Sprint(g[0].Repos) != "[kasse/flaky kasse/quick]" ||
		g[0].To.Sub(g[0].From) != 20*time.Minute {
		t.Errorf("groups = %+v", g)
	}
	if b.Hotspots.OutlierCount != 4 {
		t.Errorf("outlier count = %d", b.Hotspots.OutlierCount)
	}
	// An outage of an hour, a run stuck every 25 min: one group, chained.
	at := func(min int) *time.Time { return ptr(ago(2).Add(time.Duration(min) * time.Minute)) }
	if og := outlierGroups([]Outlier{
		{Repo: "a", StartedAt: at(0)}, {Repo: "b", StartedAt: at(25)}, {Repo: "a", StartedAt: at(50)},
		{Repo: "c", StartedAt: at(75)}, {Repo: "d", StartedAt: at(200)},
	}); len(og) != 1 || og[0].Runs != 4 || len(og[0].Repos) != 3 {
		t.Errorf("chained = %+v", og)
	}
	// One repo stuck twice is no shared outage.
	if og := outlierGroups([]Outlier{
		{Repo: "a", StartedAt: agoP(2)}, {Repo: "a", StartedAt: ptr(ago(2).Add(5 * time.Minute))},
	}); len(og) != 1 || og[0].Shared {
		t.Errorf("one repo = %+v", og)
	}
}
