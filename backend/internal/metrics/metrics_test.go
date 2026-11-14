package metrics

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

func TestPercentile(t *testing.T) {
	// Postgres: SELECT percentile_cont(p) WITHIN GROUP (ORDER BY x) FROM
	// unnest('{1,2,3,4,10}'::float8[]) x;  0.5 -> 3, 0.9 -> 7.6, 0.25 -> 2
	v := []float64{10, 1, 4, 2, 3}
	expectNear(t, "p50", Percentile(v, 0.5), 3)
	expectNear(t, "p90", Percentile(v, 0.9), 7.6)
	expectNear(t, "p25", Percentile(v, 0.25), 2)
	expectNear(t, "median of two", Median([]float64{1, 2}), 1.5)
	expectNear(t, "one value", Percentile([]float64{5}, 0.9), 5)
	expectNil(t, "no values", Percentile(nil, 0.5))
	if !slices.Equal(v, []float64{10, 1, 4, 2, 3}) {
		t.Errorf("input sorted: %v", v)
	}
	a, b := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	expectNear(t, "between", between(&a, &b), 3600)
	expectNil(t, "between nil", between(nil, &b))
	expectNil(t, "negative wait", wait(&b, &a))
	expectNil(t, "ratio over 0", ratio(1, 0))
}

func TestPeriod(t *testing.T) {
	p := p4()
	var kws []int
	for _, w := range p.Weeks {
		kws = append(kws, w.KW)
	}
	if !slices.Equal(kws, []int{43, 44, 45, 46}) {
		t.Errorf("weeks = %v", kws)
	}
	// Summer time still on the first Monday.
	if want := time.Date(2026, 10, 19, 0, 0, 0, 0, zurich); !p.From.Equal(want) || !p.To.Equal(now) {
		t.Errorf("period = %v to %v", p.From, p.To)
	}
	// 3.5 weeks, plus the hour the switch back from summer time added.
	if !near(p.Span, 3.5+1.0/(7*24)) {
		t.Errorf("span = %v", p.Span)
	}
	if got := weekStart(now, zurich); !got.Equal(time.Date(2026, 11, 9, 0, 0, 0, 0, zurich)) {
		t.Errorf("week start = %v", got)
	}
	for _, c := range []struct {
		at   string
		want int
	}{
		{"2026-10-18T21:59:59Z", -1}, // Sunday 23:59 local
		{"2026-10-18T22:00:00Z", 0},
		{"2026-11-09T00:30:00+01:00", 3},
		{"2026-11-12T12:00:01+01:00", -1},
		{"2026-11-01T00:00:00Z", 1},
	} {
		at, _ := time.Parse(time.RFC3339, c.at)
		if got := p.WeekIndex(at); got != c.want {
			t.Errorf("week of %s = %d, want %d", c.at, got, c.want)
		}
	}
	if p.In(nil) {
		t.Error("nil in period")
	}
	// ISO 8601: 2027-01-01 is in week 53 of 2026.
	if _, w := NewPeriod(4, time.Date(2027, 1, 1, 12, 0, 0, 0, zurich), zurich).Weeks[3].Start.In(zurich).ISOWeek(); w != 53 {
		t.Errorf("KW of 2027-01-01 = %d", w)
	}
	if NewPeriod(4, time.Date(2027, 1, 1, 12, 0, 0, 0, zurich), zurich).Weeks[3].KW != 53 {
		t.Error("KW 53 lost")
	}
	if !ValidWindow(12) || ValidWindow(5) {
		t.Error("windows")
	}
}

func deployData() *dataset.Dataset {
	return data(func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{
			// one release reconciling two Apps: one revision
			deploy(func(d *dataset.Deploy) { d.App, d.Revision = "kasse-intranet", "r1" }),
			deploy(func(d *dataset.Deploy) { d.App, d.Revision = "kasse-extranet", "r1" }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.Kind, d.NewImages, d.At = "r2", s("sync"), nil, ago(2) }),
			deploy(func(d *dataset.Deploy) {
				d.App, d.Project, d.Revision, d.Kind, d.NewImages, d.At = "portal-prod", s("portal"), "r3", s("first_seen"), nil, ago(9)
			}),
			// a revision that is a release for one App, a sync for another
			deploy(func(d *dataset.Deploy) { d.App, d.Revision, d.Kind, d.At = "a", "r4", s("sync"), ago(10) }),
			deploy(func(d *dataset.Deploy) { d.App, d.Revision, d.At = "b", "r4", ago(10) }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.Phase, d.Kind, d.At = "r5", "Failed", nil, ago(3) }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.Environment = "r6", s("intg") }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "r7", ago(60) }), // before the window
		}
	})
}

func TestDeployFrequency(t *testing.T) {
	ds := deployData()
	p := p4()
	d := deployFrequency(ds, p, filter{}, "prod")
	if d.Releases != 2 || d.Syncs != 4 || d.FirstSeen != 1 || d.Resyncs != 1 {
		t.Errorf("releases/syncs/first = %d/%d/%d/%d", d.Releases, d.Syncs, d.FirstSeen, d.Resyncs)
	}
	if !near(d.PerWeek, 2/p.Span) {
		t.Errorf("per week = %v", d.PerWeek)
	}
	if d.Succeeded != 6 || d.Failed != 1 || d.Finished != 7 {
		t.Errorf("succeeded/failed = %d/%d", d.Succeeded, d.Failed)
	}
	expectNear(t, "success rate", d.SuccessRate, 6.0/7)
	want := []DeployWeek{{}, {}, {Release: 1, FirstSeen: 1}, {Release: 1, Sync: 1}}
	if !reflect.DeepEqual(d.Weeks, want) {
		t.Errorf("weeks = %+v", d.Weeks)
	}
	if d.Since != nil || !d.ArgoReporting {
		t.Errorf("since %v, reporting %v", d.Since, d.ArgoReporting)
	}

	if got := deployFrequency(ds, p, filter{product: "portal"}, "prod").Syncs; got != 1 {
		t.Errorf("portal syncs = %d", got)
	}
	if got := deployFrequency(ds, p, filter{}, "intg").Releases; got != 1 {
		t.Errorf("intg releases = %d", got)
	}
	empty := deployFrequency(data(), NewPeriod(4, now, zurich), filter{}, "prod")
	if empty.PerWeek != 0 || empty.SuccessRate != nil {
		t.Errorf("empty = %+v", empty)
	}
	// Argo CD never reported, or only after the period: not reporting yet.
	silent := data(func(ds *dataset.Dataset) { ds.Meta.DataStart.ArgoCD = nil })
	if deployFrequency(silent, p, filter{}, "prod").ArgoReporting {
		t.Error("reporting without data")
	}
}

func TestEnvironments(t *testing.T) {
	ds := deployData()
	if got := environments(ds, filter{}); !slices.Equal(got, []string{"intg", "prod"}) {
		t.Errorf("all = %v", got)
	}
	// the product's own deploys only
	if got := environments(ds, filter{product: "portal"}); !slices.Equal(got, []string{"prod"}) {
		t.Errorf("portal = %v", got)
	}
	test := data(func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{deploy(func(d *dataset.Deploy) { d.Environment = s("test") })}
	})
	if got := environments(test, filter{}); !slices.Equal(got, []string{"test"}) {
		t.Errorf("test = %v", got)
	}
}

func TestDeployWeeksAndDataStart(t *testing.T) {
	// Sunday evening and Monday morning around the KW 45/46 boundary.
	at := func(v string) time.Time { t, _ := time.Parse(time.RFC3339, v); return t }
	ds := data(func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{
			deploy(func(d *dataset.Deploy) { d.App, d.At = "a", at("2026-11-08T20:00:00+01:00") }),
			deploy(func(d *dataset.Deploy) { d.App, d.At, d.Kind = "b", at("2026-11-09T08:00:00+01:00"), s("sync") }),
		}
	})
	d := deployFrequency(ds, p4(), filter{}, "prod")
	if d.Releases != 1 || d.Weeks[2] != (DeployWeek{Release: 1}) || d.Weeks[3] != (DeployWeek{}) {
		t.Errorf("weeks = %+v", d.Weeks)
	}

	// Rated only over the weeks Argo CD reported.
	one := data(func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{deploy()}
		ds.Meta.DataStart.ArgoCD = agoP(7)
	})
	d = deployFrequency(one, p4(), filter{}, "prod")
	if d.Since == nil || !d.Since.Equal(ago(7)) || !near(d.PerWeek, 1) || d.LowSample {
		t.Errorf("since %v, per week %v, low sample %v", d.Since, d.PerWeek, d.LowSample)
	}
	// Under a week of data: one release reads as several a week, flagged.
	one.Meta.DataStart.ArgoCD = agoP(2)
	d = deployFrequency(one, p4(), filter{}, "prod")
	if !near(d.PerWeek, 3.5) || !d.LowSample {
		t.Errorf("two days: per week %v, low sample %v", d.PerWeek, d.LowSample)
	}
}

func TestLeadTime(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.LeadTimes = []dataset.LeadTime{
			leadTimeRow(func(l *dataset.LeadTime) { l.LeadTimeS = 100 }),
			leadTimeRow(func(l *dataset.LeadTime) { l.LeadTimeS = 300 }),
			leadTimeRow(func(l *dataset.LeadTime) { l.LeadTimeS, l.IsMerge = 50, true }),
			leadTimeRow(func(l *dataset.LeadTime) { l.LeadTimeS, l.ServiceAccount = 60, true }),
			leadTimeRow(func(l *dataset.LeadTime) { l.LeadTimeS, l.Bot = 70, true }),
			leadTimeRow(func(l *dataset.LeadTime) { l.LeadTimeS, l.Environment = 900, "intg" }),
			leadTimeRow(func(l *dataset.LeadTime) { l.LeadTimeS, l.Project = 5, "portal" }),
		}
	})
	l := leadTime(ds, filter{}, "prod")
	if l.Human.N != 3 || l.Bot.N != 1 || !l.LowSample {
		t.Errorf("lead time = %+v", l)
	}
	expectNear(t, "p50", l.Human.P50, 100)
	expectNear(t, "p90", l.Human.P90, 260)
	if got := leadTime(ds, filter{product: "kasse"}, "prod").Human.N; got != 2 {
		t.Errorf("kasse n = %d", got)
	}
	if leadTime(data(), filter{}, "prod").LowSample {
		t.Error("no commits flagged as a low sample")
	}
}

func prData() *dataset.Dataset {
	return data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{
			pr(),
			pr(func(p *dataset.PR) { p.PRID, p.Bot, p.ChangeType = 2, true, s("other") }),
			pr(func(p *dataset.PR) { p.PRID, p.GitOps, p.Repo = 3, true, "kasse/kasse-gitops" }),
			pr(func(p *dataset.PR) { p.PRID, p.State, p.MergedAt = 4, "open", nil }),
			pr(func(p *dataset.PR) { p.PRID, p.MergedAt = 5, agoP(40) }), // merged before the window
			pr(func(p *dataset.PR) {
				p.PRID, p.Project, p.Repo, p.ChangeType, p.OpenedAt, p.MergedAt = 6, "portal", "portal/portal-web", s("bugfix"), agoP(10), agoP(9)
			}),
			pr(func(p *dataset.PR) { p.PRID, p.OpenedAt, p.ChangeType, p.MergedAt = 7, nil, nil, agoP(9) }),
		}
	})
}

func ids(prs []dataset.PR) []int64 {
	var out []int64
	for _, p := range prs {
		out = append(out, p.PRID)
	}
	return out
}

func TestPRs(t *testing.T) {
	ds, p := prData(), p4()
	if got := ids(mergedPRs(ds, p, filter{}, false)); !slices.Equal(got, []int64{1, 6, 7}) {
		t.Errorf("merged = %v", got)
	}
	if got := ids(mergedPRs(ds, p, filter{}, true)); !slices.Equal(got, []int64{1, 2, 6, 7}) {
		t.Errorf("with bots = %v", got)
	}
	if got := ids(mergedPRs(ds, p, filter{product: "portal"}, true)); !slices.Equal(got, []int64{6}) {
		t.Errorf("portal = %v", got)
	}

	// Cycle time needs the opening.
	c := cycleTimes(mergedPRs(ds, p, filter{}, false))
	if len(c) != 2 || !near(c[0], 0.2*86400) || !near(c[1], 86400) {
		t.Errorf("cycle = %v", c)
	}
	if got := histogram([]float64{60, 900, 3600, 5 * 3600, 2 * 86400, 9 * 86400, 10}); !slices.Equal(got, []int{2, 1, 1, 1, 1, 1}) {
		t.Errorf("histogram = %v", got)
	}

	// Change types per merge week.
	mix := changeMix(mergedPRs(ds, p, filter{}, true), p)
	if mix[3] != (MixWeek{Feature: 1, Other: 1}) || mix[2] != (MixWeek{Bugfix: 1, Other: 1}) {
		t.Errorf("mix = %+v", mix)
	}

	r := reviewOf(ds, p, filter{bots: true})
	if r.Merged != 4 || r.Untracked != 0 || r.Cycle.N != 2 || !slices.Equal(r.CycleHistogram.Bot, []int{0, 0, 0, 1, 0, 0}) {
		t.Errorf("review = %+v", r)
	}
	expectNear(t, "untracked rate", r.UntrackedRate, 0)
	if r := reviewOf(data(), p, filter{}); r.UntrackedRate != nil || r.FirstTouchRate != nil {
		t.Errorf("empty review rates = %+v", r)
	}
}

func stage(c Chain, key string) Stage {
	for _, s := range c.Stages {
		if s.Key == key {
			return s
		}
	}
	return Stage{Key: "missing"}
}

func TestChain(t *testing.T) {
	const h = 3600.0
	ds := data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{
			pr(func(p *dataset.PR) {
				p.FirstCommitAt, p.ReadyAt, p.ApprovedAt, p.MergedAt = agoP(3), agoP(2), agoP(2-1.0/24), agoP(2-2.0/24)
				p.Environments = []dataset.EnvironmentDeploy{
					{Environment: "intg", FirstDeployedAt: ago(2 - 4.0/24)},
					{Environment: "prod", FirstDeployedAt: ago(1)},
				}
			}),
			// approved after a comment, never deployed
			pr(func(p *dataset.PR) {
				p.PRID, p.ApprovedOnFirstTouch, p.ApprovedAt, p.ReadyAt, p.MergedAt = 2, false, agoP(1.5), agoP(2), agoP(1.4)
			}),
			// a bot's PR and a draft never made ready drop out
			pr(func(p *dataset.PR) { p.PRID, p.Bot = 3, true }),
			pr(func(p *dataset.PR) { p.PRID, p.ReadyAt = 4, nil }),
			// a commit after ready (rebased): no coding stage, never negative
			pr(func(p *dataset.PR) {
				p.PRID, p.FirstCommitAt, p.ReadyAt, p.ApprovedAt, p.MergedAt = 5, agoP(1.95), agoP(2), nil, agoP(1.5)
			}),
		}
	})
	raw := chainOf(ds, p4(), filter{})
	c := leadChain(raw)
	if c.PreEnvironment == nil || *c.PreEnvironment != "intg" {
		t.Fatalf("pre = %v", c.PreEnvironment)
	}
	if n := stage(c, StageCoding).N; n != 2 {
		t.Errorf("coding n = %d", n)
	}
	expectNear(t, "coding p50", stage(c, StageCoding).P50, 86400)
	expectNear(t, "review p50", stage(c, StageReview).P50, (1+12)/2.0*h)
	if stage(c, StageReview).N != 2 || stage(c, StageMerge).N != 2 || stage(c, StageToPre).N != 1 {
		t.Errorf("stages = %+v", c.Stages)
	}
	expectNear(t, "to pre p50", stage(c, StageToPre).P50, 2*h)
	expectNear(t, "to prod p50", stage(c, StageToProd).P50, 20*h)
	if raw.approved != 2 || raw.approvedOnFirstTouch != 1 {
		t.Errorf("approved = %d/%d", raw.approved, raw.approvedOnFirstTouch)
	}
	// Shares of the summed medians; the largest median is hot.
	var total float64
	for _, s := range c.Stages {
		total += *s.P50
	}
	expectNear(t, "sum", c.SumP50, total)
	expectNear(t, "to prod share", stage(c, StageToProd).Share, 20*h/total)
	if !stage(c, StageCoding).Hot || stage(c, StageToProd).Hot {
		t.Errorf("hot = %+v", c.Stages)
	}
	if !near(c.BeforeProd, total-20*h) || !stage(c, StageToPre).LowSample {
		t.Errorf("before prod = %v", c.BeforeProd)
	}
}

func TestChainWithoutAStageBeforeProduction(t *testing.T) {
	c := leadChain(chainOf(data(func(ds *dataset.Dataset) { ds.PRs = []dataset.PR{pr()} }), p4(), filter{}))
	if c.PreEnvironment != nil || stage(c, StageToPre).Key != "missing" || len(c.Stages) != 4 {
		t.Errorf("chain = %+v", c)
	}
	// Straight to production: the deploy time still counts, from the merge.
	c = leadChain(chainOf(data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{pr(func(p *dataset.PR) {
			p.MergedAt = agoP(2)
			p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: ago(1)}}
		})}
	}), p4(), filter{}))
	if s := stage(c, StageToProd); s.N != 1 {
		t.Errorf("to prod = %+v", s)
	}
	expectNear(t, "to prod", stage(c, StageToProd).P50, 86400)

	// No PR at all: nothing to sum, no shares.
	empty := leadChain(chainOf(data(), p4(), filter{}))
	if empty.SumP50 != nil || stage(empty, StageCoding).Share != nil || stage(empty, StageCoding).Hot {
		t.Errorf("empty = %+v", empty)
	}
	// All medians zero: equal shares, never NaN.
	zero := data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{pr(func(p *dataset.PR) {
			p.FirstCommitAt, p.ReadyAt, p.ApprovedAt, p.MergedAt = agoP(2), agoP(2), agoP(2), agoP(2)
		})}
	})
	zc := leadChain(chainOf(zero, p4(), filter{}))
	expectNear(t, "zero share", stage(zc, StageCoding).Share, 1.0/3)
}

func TestChainHotfixPastTheStageBefore(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{
			// the regular PR sets intg as the stage before production
			pr(func(p *dataset.PR) {
				p.MergedAt = agoP(3)
				p.Environments = []dataset.EnvironmentDeploy{
					{Environment: "intg", FirstDeployedAt: ago(2.9)}, {Environment: "prod", FirstDeployedAt: ago(2)},
				}
			}),
			// the hotfix: prod an hour after the merge, intg days later
			pr(func(p *dataset.PR) {
				p.PRID, p.MergedAt = 2, agoP(5)
				p.Environments = []dataset.EnvironmentDeploy{
					{Environment: "prod", FirstDeployedAt: ago(5 - 1.0/24)}, {Environment: "intg", FirstDeployedAt: ago(2)},
				}
			}),
		}
	})
	c := leadChain(chainOf(ds, p4(), filter{}))
	if stage(c, StageToPre).N != 1 || stage(c, StageToProd).N != 2 {
		t.Errorf("stages = %+v", c.Stages)
	}
	expectNear(t, "to prod", stage(c, StageToProd).P50, (0.9*86400+3600)/2)
}

func TestDeliveryPicksTheStageBeforeProduction(t *testing.T) {
	// No PR reached a stage before production: the most deployed one.
	ds := deployData()
	d := delivery(ds, p4(), filter{}, chainOf(ds, p4(), filter{}))
	if d.PreEnvironment == nil || *d.PreEnvironment != "intg" || d.LeadTimePre == nil || d.LeadTimePre.Environment != "intg" {
		t.Errorf("delivery = %+v", d)
	}
	d = delivery(data(), p4(), filter{}, chainOf(data(), p4(), filter{}))
	if d.PreEnvironment != nil || d.LeadTimePre != nil {
		t.Errorf("empty delivery = %+v", d)
	}
}

func buildData() *dataset.Dataset {
	return data(func(ds *dataset.Dataset) {
		ds.Runs = []dataset.Run{
			run(func(r *dataset.Run) { r.BuildType, r.DurationS = s("release"), n(300) }),
			run(func(r *dataset.Run) {
				r.BuildType, r.Branch, r.BranchKind, r.DurationS = s("HOTFIX"), "release/1.1", dataset.BranchRelease, n(200)
			}),
			run(func(r *dataset.Run) { r.Status, r.DurationS = s("FAILURE"), n(900) }),
			run(func(r *dataset.Run) { r.DurationS = n(600) }),
			run(func(r *dataset.Run) {
				r.BranchKind, r.Status, r.DurationS, r.PipelineName = dataset.BranchFeature, s("UNSTABLE"), n(100), "kasse-api/feature%2Fx"
			}),
			run(func(r *dataset.Run) { r.BranchKind, r.DurationS = dataset.BranchPR, n(120) }),
			run(func(r *dataset.Run) {
				r.BranchKind, r.BuildType, r.Repo, r.Project, r.PipelineName = dataset.BranchOther, nil, nil, nil, "nightly"
			}),
			// before the pipelines sent a build_type: a master run is unknown
			run(func(r *dataset.Run) { r.BuildType, r.FinishedAt, r.StartedAt = nil, agoP(20), agoP(20) }),
			run(func(r *dataset.Run) { r.Status = nil }), // no status: not counted
		}
	})
}

func TestBuilds(t *testing.T) {
	b := build(buildData(), p4(), filter{})
	type row struct {
		kind            string
		runs, succeeded int
	}
	var got []row
	for _, k := range b.Kinds {
		got = append(got, row{k.Kind, k.Runs, k.Succeeded})
	}
	want := []row{{KindRelease, 2, 2}, {KindMain, 2, 1}, {KindFeature, 2, 1}, {KindOther, 1, 1}, {KindUnknown, 1, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kinds = %+v", got)
	}
	main := b.Kinds[1]
	expectNear(t, "main p50", main.Duration.P50, 750)
	if main.CISeconds != 1500 || main.WastedSeconds != 900 || main.Failed != 1 {
		t.Errorf("main = %+v", main)
	}
	expectNear(t, "main week 4", main.Weekly[3], 0.5)
	expectNil(t, "main week 1", main.Weekly[0])
	if got := build(buildData(), p4(), filter{product: "portal"}); len(got.Kinds) != 0 {
		t.Errorf("portal = %+v", got.Kinds)
	}
	// Run data from ten days back: the first two weeks are a gap.
	if !b.StartsInside || b.GapWeeks != 2 {
		t.Errorf("starts inside %v, gap %d", b.StartsInside, b.GapWeeks)
	}
	none := build(data(func(ds *dataset.Dataset) { ds.Meta.BuildDataFrom = nil }), p4(), filter{})
	if none.StartsInside || none.GapWeeks != 4 || len(none.Kinds) != 0 {
		t.Errorf("no data = %+v", none)
	}
}

func TestRunKind(t *testing.T) {
	if got := RunKind(run(func(r *dataset.Run) { r.BuildType = nil }), nil); got != KindUnknown {
		t.Errorf("no build_type ever = %s", got)
	}
	if got := RunKind(run(func(r *dataset.Run) { r.BuildType = nil }), agoP(30)); got != KindMain {
		t.Errorf("after build_types = %s", got)
	}
	if got := RunKind(run(func(r *dataset.Run) { r.BuildType, r.BranchKind = nil, dataset.BranchFeature }), nil); got != KindFeature {
		t.Errorf("feature = %s", got)
	}
	if got := RunKind(run(func(r *dataset.Run) { r.BuildType, r.FinishedAt, r.StartedAt = nil, nil, nil }), agoP(30)); got != KindMain {
		t.Errorf("no instant = %s", got)
	}
}

func TestRepoDurations(t *testing.T) {
	h := build(buildData(), p4(), filter{}).Hotspots
	byKind := map[string]KindHotspots{}
	for _, k := range h.Kinds {
		byKind[k.Kind] = k
	}
	main := byKind[KindMain].Durations
	if len(main) != 1 || main[0].Repo != "kasse/kasse-api" || main[0].Runs != 2 || *main[0].Duration.P50 != 750 {
		t.Errorf("main = %+v", main)
	}
	if other := byKind[KindOther].Durations; other[0].Repo != "nightly" {
		t.Errorf("other = %+v", other)
	}
}

func TestCosts(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Reviews = []dataset.Review{
			reviewRow(func(r *dataset.Review) { r.CostUSD = s("1.250000") }),
			reviewRow(func(r *dataset.Review) {
				r.PRKey, r.CostUSD, r.LinesAdded, r.LinesRemoved = "kasse/kasse-api#2", s("0.750000"), n(300), n(200)
			}),
			reviewRow(func(r *dataset.Review) {
				r.PRKey, r.Outcome, r.CostUSD = "kasse/kasse-api#3", s("declined"), s("0.400000")
			}),
			reviewRow(func(r *dataset.Review) { r.PRKey, r.Outcome, r.CostUSD = "kasse/kasse-api#4", s("deleted"), nil }),
			reviewRow(func(r *dataset.Review) {
				r.PRKey, r.Repo, r.Project, r.CostUSD = "portal/portal-web#1", s("portal/portal-web"), s("portal"), s("3.000000")
			}),
		}
	})
	c := costs(ds, filter{})
	if !near(c.Total, 5.4) || !near(c.Merged, 5) || c.MergedReviews != 3 || !near(c.Wasted, 0.4) || c.ReviewedRepos != 2 {
		t.Errorf("costs = %+v", c)
	}
	expectNear(t, "per kloc", c.PerKloc, 5.0/1500*1000)
	if len(c.Rows) != 2 || c.Rows[0].Label != "portal" || c.Rows[1].Label != "kasse" {
		t.Errorf("rows = %+v", c.Rows)
	}
	k := costs(ds, filter{product: "kasse"})
	if len(k.Rows) != 1 || k.Rows[0].Label != "kasse/kasse-api" || !near(k.Rows[0].Value, 2.4) {
		t.Errorf("kasse rows = %+v", k.Rows)
	}
	if costs(data(), filter{}).PerKloc != nil {
		t.Error("per kloc without lines")
	}
	// Exact decimals: a sum of tenths that a float would round off.
	tenths := data(func(ds *dataset.Dataset) {
		for range 10 {
			ds.Reviews = append(ds.Reviews, reviewRow(func(r *dataset.Review) { r.CostUSD = s("0.1") }))
		}
		ds.Reviews = append(ds.Reviews, reviewRow(func(r *dataset.Review) { r.CostUSD = s("bogus") }))
	})
	if got := costs(tenths, filter{}).Total; got != 1 {
		t.Errorf("ten tenths = %v", got)
	}
}

func TestCompute(t *testing.T) {
	ds := prData()
	m := Compute(ds, Params{Period: p4(), Product: "portal", Bots: true})
	if m.Product == nil || *m.Product != "portal" || !m.Bots || m.Review.Merged != 1 || len(m.Period.Weeks) != 4 ||
		len(m.Review.CycleHistogram.UpperBoundsS) != len(m.Review.CycleHistogram.Human)-1 {
		t.Errorf("portal = %+v", m)
	}
	// A product without data is all of them.
	if m := Compute(ds, Params{Period: p4(), Product: "nope"}); m.Product != nil || m.Review.Merged != 3 {
		t.Errorf("unknown product = %+v", m.Review)
	}
}
