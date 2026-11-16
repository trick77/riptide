package metrics

import (
	"fmt"
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

// leadData is n human commits to prod, each taking days.
func leadData(count int, days float64, mod ...func(*dataset.Dataset)) *dataset.Dataset {
	return data(append([]func(*dataset.Dataset){func(ds *dataset.Dataset) {
		for i := range count {
			ds.LeadTimes = append(ds.LeadTimes, leadTimeRow(func(l *dataset.LeadTime) {
				l.CommitSHA, l.LeadTimeS = fmt.Sprint(i), int64(days*86400)
			}))
		}
	}}, mod...)...)
}

func TestCompareWithThePreviousPeriod(t *testing.T) {
	params := Params{Period: p4(), Previous: PreviousPeriod(4, now, zurich)}
	m := Compute(leadData(12, 2), leadData(12, 3), params)
	c := m.Comparison
	if !c.From.Equal(params.Previous.From) || !c.To.Equal(params.Previous.To) {
		t.Errorf("period = %v..%v", c.From, c.To)
	}
	if c.LeadTime.Verdict != ChangeBetter || c.LeadTime.Prev == nil || *c.LeadTime.Prev != 3*86400 {
		t.Errorf("lead time = %+v", c.LeadTime)
	}
	// No PRs, runs or reviews in either period: nothing to tell.
	if c.ReviewTime.Verdict != ChangeNone || *c.ReviewTime.Reason != ReasonLowSample {
		t.Errorf("review = %+v", c.ReviewTime)
	}
	if len(c.Build) != 0 {
		t.Errorf("build = %+v", c.Build)
	}

	// Argo CD started inside the previous period: it reads low, no verdict.
	late := func(ds *dataset.Dataset) { ds.Meta.DataStart.ArgoCD = agoP(30) }
	c = Compute(leadData(12, 2, late), leadData(12, 3, late), params).Comparison
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
	c = Compute(data(releases(0)), earlier(6), params).Comparison
	if c.DeployFreq.Reason == nil || *c.DeployFreq.Reason != ReasonIncomplete {
		t.Errorf("first deploys = %+v", c.DeployFreq)
	}
	// One new App among the releases still compares.
	if c := Compute(data(releases(0)), earlier(1), params).Comparison; c.DeployFreq.Verdict != ChangeBetter {
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
	c = Compute(builds(nil, 0), builds(nil, month), params).Comparison
	if len(c.Build) != 1 || c.Build[0].Kind != KindMain || c.Build[0].Success.Verdict != ChangeSame ||
		c.Build[0].Compared != 1 || c.Build[0].Slower != 0 {
		t.Errorf("no build type = %+v", c.Build)
	}
	// The repo against its own previous period, end to end: twice as slow
	// is one slower repo, and a finding.
	slower := builds(nil, 0)
	for i := range slower.Runs {
		slower.Runs[i].DurationS = n(1200)
	}
	sm := Compute(slower, builds(nil, month), params)
	if c := sm.Comparison.Build; len(c) != 1 || c[0].Slower != 1 || c[0].Compared != 1 {
		t.Errorf("slower = %+v", c)
	}
	if len(sm.Findings) == 0 || sm.Findings[0].Kind != FindingSlow {
		t.Errorf("findings = %+v", sm.Findings)
	}
	c = Compute(builds(agoP(30), 0), builds(agoP(30), month), params).Comparison
	if len(c.Build) != 1 || c.Build[0].Success.Reason == nil || *c.Build[0].Success.Reason != ReasonIncomplete {
		t.Errorf("build type inside = %+v", c.Build)
	}
}

func TestFindings(t *testing.T) {
	share := func(x float64) *float64 { return &x }
	chain := func(hot float64) Chain {
		return Chain{Stages: []Stage{
			{Key: StageReview, Share: share(1 - hot)},
			{Key: StageToProd, Share: share(hot), Hot: true},
		}}
	}
	factor := func(x float64) *float64 { return &x }
	b := Build{Hotspots: Hotspots{Kinds: []KindHotspots{
		{Kind: KindFeature, Repos: []RepoHotspot{
			{Repo: "kasse/e2e", Flaky: true, WastedSeconds: 100},
			{Repo: "kasse/api", Slow: true, Factor: factor(2.6)},
		}},
		{Kind: KindMain, Repos: []RepoHotspot{
			{Repo: "kasse/web", Flaky: true, WastedSeconds: 900},
			// flagged twice: once is enough
			{Repo: "kasse/e2e", Flaky: true, WastedSeconds: 50},
		}},
	}}}
	reviewed := Review{Approved: 4}
	kinds := func(fs []Finding) string {
		out := ""
		for _, f := range fs {
			out += f.Kind + ":" + f.Tone
			if f.Repo != nil {
				out += ":" + *f.Repo
			}
			out += " "
		}
		return out
	}

	// The waiting stage first, the worst flaky repo next, capped at three,
	// then the question to talk about.
	same := Change{Verdict: ChangeSame}
	got := kinds(findings(chain(0.86), b, reviewed, OpenPRs{Long: 3, Jammed: 1}, same))
	want := "stage:attention flaky:attention:kasse/web flaky:attention:kasse/e2e first_touch:talk "
	if got != want {
		t.Errorf("findings = %q, want %q", got, want)
	}
	// Under half the time, or on fewer than 10 PRs: a hint, after the repos.
	got = kinds(findings(chain(0.4), Build{}, reviewed, OpenPRs{}, same))
	if got != "stage:hint first_touch:talk " {
		t.Errorf("hint = %q", got)
	}
	few := chain(0.9)
	few.Stages[1].LowSample = true
	if got = kinds(findings(few, Build{}, reviewed, OpenPRs{}, same)); got != "stage:hint first_touch:talk " {
		t.Errorf("few PRs = %q", got)
	}
	// Nothing at all: no findings, no question without approvals.
	if fs := findings(Chain{}, Build{}, Review{}, OpenPRs{}, Change{Verdict: ChangeNone}); len(fs) != 0 {
		t.Errorf("empty = %+v", fs)
	}
}

// Jammed repos: attention when more than at the previous period's end,
// after the repos; else a hint after the stage's. Long-open PRs in no
// jammed repo: nothing.
func TestFindingsOpenPRs(t *testing.T) {
	hint := Chain{Stages: []Stage{{Key: StageReview, Share: ptr(0.4), Hot: true}}}
	kinds := func(fs []Finding) string {
		out := ""
		for _, f := range fs {
			out += f.Kind + ":" + f.Tone + " "
		}
		return out
	}
	flaky := Build{Hotspots: Hotspots{Kinds: []KindHotspots{{Kind: KindMain, Repos: []RepoHotspot{
		{Repo: "kasse/web", Flaky: true, WastedSeconds: 900},
	}}}}}
	if got := kinds(findings(hint, flaky, Review{}, OpenPRs{Long: 3, Jammed: 1}, Change{Verdict: ChangeWorse})); got != "flaky:attention open_prs:attention stage:hint " {
		t.Errorf("more than before = %q", got)
	}
	for _, v := range []string{ChangeSame, ChangeBetter, ChangeNone} {
		if got := kinds(findings(hint, Build{}, Review{}, OpenPRs{Long: 3, Jammed: 1}, Change{Verdict: v})); got != "stage:hint open_prs:hint " {
			t.Errorf("%s = %q", v, got)
		}
	}
	// None long open, or no repo jammed: nothing, whatever the comparison
	// says.
	if got := kinds(findings(Chain{}, Build{}, Review{}, OpenPRs{Open: 4}, Change{Verdict: ChangeWorse})); got != "" {
		t.Errorf("none long open = %q", got)
	}
	if got := kinds(findings(Chain{}, Build{}, Review{}, OpenPRs{Open: 9, Long: 5}, Change{Verdict: ChangeWorse})); got != "" {
		t.Errorf("none jammed = %q", got)
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
	if !k.Repos[0].Flaky || !k.Repos[1].Slow || k.Repos[2].Flaky || k.Repos[2].Slow {
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
