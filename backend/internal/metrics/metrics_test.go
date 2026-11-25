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

// calendarOf is the frequency with its days, as Compute fills them.
func calendarOf(ds *dataset.Dataset, p Period) DeployFrequency {
	d := deployFrequency(ds, p, filter{}, "prod")
	d.fillCalendar(p, ds.Meta.DataStart.ArgoCD)
	return d
}

func TestDeployDays(t *testing.T) {
	p := p4()
	d := calendarOf(deployData(), p)
	if len(d.Days) != 28 || d.Days[0].Date != "2026-10-19" || d.Days[27].Date != "2026-11-15" {
		t.Fatalf("days = %+v", d.Days)
	}
	// The days add up to the weeks.
	for w, week := range d.Weeks {
		sum := 0
		for _, day := range d.Days[7*w : 7*w+7] {
			sum += day.Releases
		}
		if sum != week.Release {
			t.Errorf("KW %d: days %d, week %d", p.Weeks[w].KW, sum, week.Release)
		}
	}
	// r4 on Monday 2.11., r1 on Wednesday 11.11.; a sync and a first deploy
	// are no release.
	if d.Days[14].Releases != 1 || d.Days[23].Releases != 1 {
		t.Errorf("days 14/23 = %+v %+v", d.Days[14], d.Days[23])
	}
	// Today has data, tomorrow none yet; Argo CD reported all along.
	if d.Days[24].NoData || !d.Days[25].NoData || d.Days[0].NoData || d.NoDataDays != 3 {
		t.Errorf("no data = %v %v %v, %d days", d.Days[24].NoData, d.Days[25].NoData, d.Days[0].NoData, d.NoDataDays)
	}
	// Per weekday over the whole days with data only: Friday to Sunday of
	// the running week are still to come, today is not over.
	expectWeekday(t, d, 0, 1, 4, 0.25)
	expectWeekday(t, d, 2, 1, 4, 0.25)
	expectWeekday(t, d, 3, 0, 3, 0)
	expectWeekday(t, d, 4, 0, 3, 0)
	// A release this morning shows on today's cell, not in Thursday's rate.
	today := calendarOf(data(func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{deploy(func(d *dataset.Deploy) { d.At = ago(0.1) })}
	}), p)
	if today.Days[24].Releases != 1 || today.Days[24].Step != 1 {
		t.Errorf("today = %+v", today.Days[24])
	}
	expectWeekday(t, today, 3, 0, 3, 0)

	// Around the switch back from summer time, Sunday 25.10. has 25 hours:
	// 23:30 is still Sunday, 00:30 Monday. Five releases on Monday shade
	// as four or more.
	at := func(v string) time.Time { t, _ := time.Parse(time.RFC3339, v); return t }
	ds := data(func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "sun", at("2026-10-25T23:30:00+01:00") }),
		}
		for i, r := range []string{"m1", "m2", "m3", "m4", "m5"} {
			ds.Deploys = append(ds.Deploys, deploy(func(d *dataset.Deploy) {
				d.Revision, d.At = r, at("2026-10-26T00:30:00+01:00").Add(time.Duration(i)*time.Hour)
			}))
		}
		ds.Meta.DataStart.ArgoCD = ptr(at("2026-10-21T18:00:00+02:00"))
	})
	d = calendarOf(ds, p)
	sun, mon := d.Days[6], d.Days[7]
	if sun != (DeployDay{Date: "2026-10-25", Releases: 1, Step: 1}) || mon != (DeployDay{Date: "2026-10-26", Releases: 5, Step: 4}) {
		t.Errorf("sunday %+v, monday %+v", sun, mon)
	}
	// Argo CD started on Wednesday evening: that day counts, Tuesday not.
	if !d.Days[1].NoData || d.Days[2].NoData || d.NoDataDays != 5 {
		t.Errorf("before Argo CD = %v %v, %d days", d.Days[1].NoData, d.Days[2].NoData, d.NoDataDays)
	}
	// The Mondays and Tuesdays before Argo CD are no days without a release,
	// and its first Wednesday, known only from 18:00, is no whole day.
	expectWeekday(t, d, 0, 5, 3, 5.0/3)
	expectWeekday(t, d, 1, 0, 3, 0)
	expectWeekday(t, d, 2, 0, 3, 0)
	expectWeekday(t, d, 6, 1, 3, 1.0/3)
	// Argo CD never reported: no day has data, no weekday a rate.
	silent := calendarOf(data(func(ds *dataset.Dataset) { ds.Meta.DataStart.ArgoCD = nil }), p)
	if !silent.Days[0].NoData || !silent.Days[24].NoData || silent.NoDataDays != 28 {
		t.Error("days with data without Argo CD")
	}
	for i, wd := range silent.Weekdays {
		if wd.Days != 0 || wd.PerDay != nil {
			t.Errorf("silent weekday %d = %+v", i, wd)
		}
	}

	// Days cut in another zone than the weeks (a period without its zone,
	// days in UTC): Sunday 23:30 local is past the UTC week's last day. The
	// release stays counted, no day takes it, nothing panics.
	utc := p
	utc.loc = nil
	d = calendarOf(ds, utc)
	placed := 0
	for _, day := range d.Days {
		placed += day.Releases
	}
	if d.Releases != 6 || placed != 5 || d.Days[6].Releases != 0 {
		t.Errorf("releases %d, placed %d, sunday %+v", d.Releases, placed, d.Days[6])
	}
}

func TestCalendarCurrentPeriodOnly(t *testing.T) {
	ds := deployData()
	m := Compute(ds, ds, nil, Params{Period: p4(), Previous: PreviousPeriod(4, now, zurich)})
	if len(m.Delivery.Frequency.Days) != 28 {
		t.Errorf("days = %d", len(m.Delivery.Frequency.Days))
	}
	pm := compute(ds, PreviousPeriod(4, now, zurich), filter{}, &baseline{repos: newRepoNamer(nil)})
	if pm.Delivery.Frequency.Days != nil {
		t.Error("days built for the previous period")
	}
}

func expectWeekday(t *testing.T, d DeployFrequency, i, releases, days int, perDay float64) {
	t.Helper()
	wd := d.Weekdays[i]
	if wd.Releases != releases || wd.Days != days {
		t.Errorf("weekday %d = %d releases on %d days, want %d on %d", i, wd.Releases, wd.Days, releases, days)
	}
	expectNear(t, "per day", wd.PerDay, perDay)
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
	// The bot's PR 2 never counts.
	if got := ids(mergedPRs(ds, p, filter{})); !slices.Equal(got, []int64{1, 6, 7}) {
		t.Errorf("merged = %v", got)
	}
	if got := ids(mergedPRs(ds, p, filter{product: "portal"})); !slices.Equal(got, []int64{6}) {
		t.Errorf("portal = %v", got)
	}

	// Cycle time needs the opening.
	c := cycleTimes(mergedPRs(ds, p, filter{}))
	if len(c) != 2 || !near(c[0], 0.2*86400) || !near(c[1], 86400) {
		t.Errorf("cycle = %v", c)
	}
	if got := histogram([]float64{60, 900, 3600, 5 * 3600, 2 * 86400, 9 * 86400, 10}); !slices.Equal(got, []int{2, 1, 1, 1, 1, 1}) {
		t.Errorf("histogram = %v", got)
	}

	// Change types per merge week.
	mix := changeMix(mergedPRs(ds, p, filter{}), p)
	if mix[3] != (MixWeek{Feature: 1}) || mix[2] != (MixWeek{Bugfix: 1, Other: 1}) {
		t.Errorf("mix = %+v", mix)
	}

	r := reviewOf(ds, p, filter{})
	if r.Merged != 3 || r.Untracked != 0 || r.Cycle.N != 2 {
		t.Errorf("review = %+v", r)
	}
	expectNear(t, "untracked rate", r.UntrackedRate, 0)
	if r := reviewOf(data(), p, filter{}); r.UntrackedRate != nil || r.FirstTouchRate != nil {
		t.Errorf("empty review rates = %+v", r)
	}
}

// Throughput and batch size count the people's merged PRs only: a bot's PR
// and a stage promotion are in neither, however large. A PR without a
// rollup, or with one that counted no line, is merged but has no size.
func TestThroughput(t *testing.T) {
	p := p4()
	size := func(id int64, days float64, added, removed, files int64) func(*dataset.PR) {
		return func(pr *dataset.PR) {
			pr.PRID, pr.MergedAt = id, agoP(days)
			pr.LinesAdded, pr.LinesRemoved, pr.FilesChanged = n(added), n(removed), n(files)
		}
	}
	ds := data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{
			pr(size(1, 1.8, 10, 2, 3)),
			pr(size(2, 1.8, 4000, 1000, 90), func(pr *dataset.PR) { pr.Bot = true }),
			pr(size(3, 1.8, 4000, 1000, 90), func(pr *dataset.PR) { pr.GitOps = true }),
			pr(size(6, 9, 100, 50, 7)),
			pr(func(pr *dataset.PR) { pr.PRID, pr.MergedAt = 7, agoP(9) }),
			pr(size(8, 9, 0, 0, 0)),
			pr(size(9, 9, 30, 0, 1)),
		}
	})
	th := throughput(mergedPRs(ds, p, filter{}), p)
	if th.Merged != 5 || th.BatchSize.N != 3 || th.BatchFiles.N != 3 {
		t.Fatalf("throughput = %+v", th)
	}
	// The test period's last week still runs: per week of what has run.
	expectNear(t, "merged per week", th.MergedPerWeek, 5/p.Span)
	expectNear(t, "batch p50", th.BatchSize.P50, 30)
	expectNear(t, "batch p90", th.BatchSize.P90, 126)
	expectNear(t, "files p50", th.BatchFiles.P50, 3)
	if w := th.Weeks[3]; w.Merged != 1 || w.Sized != 1 {
		t.Errorf("last week = %+v", w)
	}
	expectNear(t, "last week p50", th.Weeks[3].BatchP50, 12)
	if w := th.Weeks[2]; w.Merged != 4 || w.Sized != 2 {
		t.Errorf("week before = %+v", w)
	}
	expectNear(t, "week before p50", th.Weeks[2].BatchP50, 90)
	if w := th.Weeks[0]; w.Merged != 0 || w.BatchP50 != nil {
		t.Errorf("empty week = %+v", w)
	}

	// Smaller batches are the better direction, more merged PRs too.
	ok := func(v float64) side { return side{ptr(v), true} }
	if c := batchSizeMetric.judge(ok(80), ok(200), false); c.Verdict != ChangeBetter {
		t.Errorf("smaller batch = %+v", c)
	}
	if c := batchSizeMetric.judge(ok(200), ok(80), false); c.Verdict != ChangeWorse {
		t.Errorf("larger batch = %+v", c)
	}
	if c := mergedPerWeekMetric.judge(ok(12), ok(8), false); c.Verdict != ChangeBetter {
		t.Errorf("more merged = %+v", c)
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
			// a bot's PR, a stage promotion and a draft never made ready
			// drop out, approvals without a comment included
			pr(func(p *dataset.PR) { p.PRID, p.Bot = 3, true }),
			pr(func(p *dataset.PR) { p.PRID, p.GitOps = 6, true }),
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
	expectNear(t, "to prod share", stage(c, StageToProd).Share, 20*h/total)
	if !stage(c, StageCoding).Hot || stage(c, StageToProd).Hot {
		t.Errorf("hot = %+v", c.Stages)
	}
	if !near(c.BeforeProd, total-20*h) || !stage(c, StageToPre).LowSample {
		t.Errorf("before prod = %v", c.BeforeProd)
	}
}

func TestStageBoundsAreLogSpaced(t *testing.T) {
	b := StageBounds()
	if len(b) != 23 || b[0] != 300 || b[len(b)-1] != 28*86400 {
		t.Fatalf("bounds = %v", b)
	}
	for i := 1; i < len(b); i++ {
		if r := b[i] / b[i-1]; r < 1.49 || r > 1.52 {
			t.Errorf("step %d: ratio %v", i, r)
		}
	}
	// A copy: changing it changes no one else's.
	b[0] = 1
	if StageBounds()[0] != 300 {
		t.Error("StageBounds shares its array")
	}
}

func TestStageHistogram(t *testing.T) {
	b := StageBounds()
	last := len(b) - 2
	// Out-of-range waits clamp to the ends; an edge opens its bucket.
	h := stageHistogram([]float64{0, 299, 300, b[1] - 1, b[1], b[5] + 1, b[len(b)-1] - 1, b[len(b)-1], 1e9})
	want := make([]int, len(b)-1)
	want[0], want[1], want[5], want[last] = 4, 1, 1, 3
	if !slices.Equal(h, want) {
		t.Errorf("histogram = %v, want %v", h, want)
	}
	if h := stageHistogram(nil); len(h) != len(b)-1 || slices.Max(h) != 0 {
		t.Errorf("empty = %v", h)
	}
}

// A stage carries its histogram over the same PRs as its spread; a
// low-sample stage its waits one by one instead.
func TestChainStageDistributions(t *testing.T) {
	const h = 3600.0
	var prs []dataset.PR
	for i := range 12 {
		prs = append(prs, pr(func(p *dataset.PR) {
			p.PRID = int64(i + 1)
			// coding: a day and i hours; merge: 10 min after approval
			p.FirstCommitAt, p.ReadyAt = agoP(10+float64(i)/24), agoP(9)
			p.ApprovedAt, p.MergedAt = agoP(8), agoP(8-10.0/1440)
			if i < 3 {
				p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: ago(8 - 10.0/1440 - float64(i+1)/24)}}
			}
		}))
	}
	c := leadChain(chainOf(data(func(ds *dataset.Dataset) { ds.PRs = prs }), p4(), filter{}))
	if !slices.Equal(c.BucketBoundsS, StageBounds()) {
		t.Errorf("bounds = %v", c.BucketBoundsS)
	}
	for _, s := range c.Stages {
		if s.LowSample {
			if s.Histogram != nil {
				t.Errorf("%s: low sample with histogram %v", s.Key, s.Histogram)
			}
			continue
		}
		sum := 0
		for _, n := range s.Histogram {
			sum += n
		}
		if len(s.Histogram) != len(StageBounds())-1 || sum != s.N {
			t.Errorf("%s: histogram %v, n %d", s.Key, s.Histogram, s.N)
		}
	}
	coding := stage(c, StageCoding)
	if coding.LowSample || coding.DurationsS != nil {
		t.Errorf("coding = %+v", coding)
	}
	merge := stage(c, StageMerge)
	if merge.Histogram[bucketOf(600)] != 12 {
		t.Errorf("merge = %v", merge.Histogram)
	}
	prod := stage(c, StageToProd)
	if !prod.LowSample || !slices.Equal(prod.DurationsS, []float64{h, 2 * h, 3 * h}) {
		t.Errorf("to prod = %+v", prod)
	}
	// No PR reached the stage: no waits, an empty list rather than null.
	empty := leadChain(chainOf(data(func(ds *dataset.Dataset) { ds.PRs = prs[3:] }), p4(), filter{}))
	if s := stage(empty, StageToProd); s.DurationsS == nil || len(s.DurationsS) != 0 || !s.LowSample {
		t.Errorf("empty to prod = %+v", s)
	}
}

func bucketOf(s float64) int {
	return slices.Index(stageHistogram([]float64{s}), 1)
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
	if stage(empty, StageCoding).Share != nil || stage(empty, StageCoding).Hot {
		t.Errorf("empty = %+v", empty)
	}
	// All medians zero: equal shares, never NaN.
	zero := data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{pr(func(p *dataset.PR) {
			p.FirstCommitAt, p.ReadyAt, p.ApprovedAt, p.MergedAt = agoP(2), agoP(2), agoP(2), agoP(2)
			p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: ago(2)}}
		})}
	})
	zc := leadChain(chainOf(zero, p4(), filter{}))
	expectNear(t, "zero share", stage(zc, StageCoding).Share, 1.0/4)
	// The same without production: no shares at all, equal ones neither.
	zero.PRs[0].Environments = nil
	zc = leadChain(chainOf(zero, p4(), filter{}))
	if zc.ReachesProd || stage(zc, StageCoding).Share != nil || stage(zc, StageCoding).Hot {
		t.Errorf("zero without prod = %+v", zc.Stages)
	}
}

// No PR reached production (a product whose prod deploys predate its CI
// reports): the medians add up to the time to merge, so no stage has a
// share of the time to production and none is hot.
func TestChainThatDoesNotReachProduction(t *testing.T) {
	var prs []dataset.PR
	for i := range 12 {
		prs = append(prs, pr(func(p *dataset.PR) {
			p.PRID = int64(i + 1)
			p.FirstCommitAt, p.ReadyAt, p.ApprovedAt, p.MergedAt = agoP(10), agoP(5), agoP(4.9), agoP(4.8)
			p.Environments = []dataset.EnvironmentDeploy{{Environment: "intg", FirstDeployedAt: ago(4.7)}}
		}))
	}
	c := leadChain(chainOf(data(func(ds *dataset.Dataset) { ds.PRs = prs }), p4(), filter{}))
	if c.ReachesProd {
		t.Fatal("reaches prod without a production wait")
	}
	for _, s := range c.Stages {
		if s.Share != nil || s.Hot {
			t.Errorf("%s: share %v, hot %v", s.Key, s.Share, s.Hot)
		}
	}
	// So no slow stage to name and no wait share for the assessment.
	if hotStage(c) != nil {
		t.Error("hot stage without production")
	}
	if ws, _ := waitShare(c); ws != nil {
		t.Errorf("wait share = %v", *ws)
	}
	// The medians stay: only the shares need the whole way.
	expectNear(t, "coding p50", stage(c, StageCoding).P50, 5*86400)
	if c.BeforeProd <= 0 {
		t.Errorf("before prod = %v", c.BeforeProd)
	}

	// One PR in production: shares again, the hot stage among them.
	prs[0].Environments = append(prs[0].Environments, dataset.EnvironmentDeploy{Environment: "prod", FirstDeployedAt: ago(1)})
	c = leadChain(chainOf(data(func(ds *dataset.Dataset) { ds.PRs = prs }), p4(), filter{}))
	if !c.ReachesProd || stage(c, StageCoding).Share == nil || !stage(c, StageCoding).Hot {
		t.Errorf("chain = %+v", c.Stages)
	}
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
				r.BuildType, r.Branch, r.BranchKind, r.DurationS = s("HOTFIX"), "release/1.1", dataset.BranchOther, n(200)
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
	b := buildAlone(buildData(), p4(), filter{})
	type row struct {
		kind            string
		runs, succeeded int
	}
	var got []row
	for _, k := range b.Kinds {
		got = append(got, row{k.Kind, k.Runs, k.Succeeded})
	}
	want := []row{{KindFeature, 2, 1}, {KindMain, 2, 1}, {KindRelease, 2, 2}, {KindOther, 1, 1}, {KindUnknown, 1, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("kinds = %+v", got)
	}
	// A GitOps repo's run, a stage promotion's check, counts nowhere.
	promo := buildData()
	promo.GitOpsRepos = []string{"kasse/kasse-gitops"}
	promo.Runs = append(promo.Runs,
		run(func(r *dataset.Run) {
			r.GitOps, r.Repo, r.PipelineName = true, s("kasse/kasse-gitops"), "kasse-gitops/master"
		}),
		// its commit unseen by Bitbucket: no flag, known by its name
		run(func(r *dataset.Run) { r.Repo, r.Project, r.PipelineName = nil, s("kasse"), "kasse-gitops/master" }))
	pb := buildAlone(promo, p4(), filter{})
	if !reflect.DeepEqual(pb.Kinds, b.Kinds) {
		t.Errorf("gitops run counted: %+v", pb.Kinds)
	}
	for kind, repos := range pb.repos {
		if repos["kasse/kasse-gitops"] != nil {
			t.Errorf("gitops repo active in %s", kind)
		}
	}
	// the previous period: no prefixes, no hint, nobody shows them
	prev := build(buildData(), p4(), filter{}, &baseline{repos: newRepoNamer(nil), previous: true})
	if p := prev.Kinds[3].Prefixes; p == nil || len(p) != 0 || prev.FeatureHint != nil {
		t.Errorf("previous period prefixes = %#v, hint %+v", p, prev.FeatureHint)
	}
	for _, k := range b.Kinds {
		if k.Kind != KindOther && len(k.Prefixes) != 0 {
			t.Errorf("%s prefixes = %+v", k.Kind, k.Prefixes)
		}
	}
	if p := b.Kinds[3].Prefixes; len(p) != 1 || p[0] != (BranchPrefix{"master", 1}) {
		t.Errorf("other prefixes = %+v", p)
	}
	if b.FeatureHint != nil {
		t.Errorf("hint with feature runs = %+v", b.FeatureHint)
	}
	main := b.Kinds[1]
	// The green run's 600 s alone: the red one's 900 s is no build time.
	expectNear(t, "main p50", main.Duration.P50, 600)
	if main.CISeconds != 1500 || main.WastedSeconds != 900 || main.Failed != 1 {
		t.Errorf("main = %+v", main)
	}
	expectNear(t, "main week 4", main.Weekly[3], 0.5)
	expectNil(t, "main week 1", main.Weekly[0])
	if got := buildAlone(buildData(), p4(), filter{product: "portal"}); len(got.Kinds) != 0 {
		t.Errorf("portal = %+v", got.Kinds)
	}
	// Run data from ten days back: the first two weeks are a gap.
	if !b.StartsInside || b.GapWeeks != 2 {
		t.Errorf("starts inside %v, gap %d", b.StartsInside, b.GapWeeks)
	}
	none := buildAlone(data(func(ds *dataset.Dataset) { ds.Meta.BuildDataFrom = nil }), p4(), filter{})
	if none.StartsInside || none.GapWeeks != 4 || len(none.Kinds) != 0 {
		t.Errorf("no data = %+v", none)
	}
}

// otherRuns are runs on the given branches, none a feature build.
func otherRuns(status string, branches ...string) []dataset.Run {
	var rs []dataset.Run
	for _, b := range branches {
		rs = append(rs, run(func(r *dataset.Run) {
			r.Branch, r.BranchKind, r.BuildType, r.Status = b, dataset.BranchOther, nil, s(status)
		}))
	}
	return rs
}

func hinted(rs ...[]dataset.Run) Build {
	ds := data(func(ds *dataset.Dataset) { ds.Runs = slices.Concat(rs...) })
	return buildAlone(ds, p4(), filter{})
}

func TestBranchPrefixes(t *testing.T) {
	b := hinted(
		otherRuns("SUCCESS", "Release/1.1", "release/1.2", "release/2.0", "KAS-12-login", "kas-7-cart",
			"nightly", "nightly", "", "zzz", "Renovate/lib", "jan-topic", "/KAS-9-x"),
		// aborted: no run, no prefix
		otherRuns("ABORTED", "aaa", "aaa", "aaa"),
	)
	// a leading '/' is no scheme: /KAS-9-x is kas-…
	want := []BranchPrefix{{"kas-…", 3}, {"release/", 3}, {"nightly", 2}, {"", 1}, {"jan-…", 1}}
	if got := b.Kinds[0].Prefixes; !reflect.DeepEqual(got, want) {
		t.Errorf("prefixes = %+v", got)
	}
	// kas-… (3), jan-… and zzz look like work in progress
	if b.FeatureHint == nil || b.FeatureHint.Runs != 5 {
		t.Errorf("hint = %+v", b.FeatureHint)
	}
}

func TestFeatureHint(t *testing.T) {
	// Only release branches, nightlies, bots, docs, Tekton and aborted runs:
	// no sign of unrecognised feature builds.
	none := hinted(
		otherRuns("SUCCESS", "release/1.1", "release-1.2", "nightly", "nightly-full", "renovate/x",
			"dependabot/y", "snyk-fix-1", "", "develop", "docs/readme", "doc/api"),
		otherRuns("ABORTED", "kas-1-x"),
	)
	if none.FeatureHint != nil {
		t.Errorf("hint = %+v", none.FeatureHint)
	}
	// A DOC Jira key is a feature scheme, not docs: it still hints.
	if b := hinted(otherRuns("SUCCESS", "DOC-123-login", "doc-7-x")); b.FeatureHint == nil || b.FeatureHint.Runs != 2 {
		t.Errorf("hint with DOC keys = %+v", b.FeatureHint)
	}
	// recognised feature builds: nothing to hint at
	feat := otherRuns("SUCCESS", "feat/x")
	feat[0].BranchKind = dataset.BranchFeature
	if b := hinted(feat, otherRuns("FAILURE", "kas-1-x")); b.FeatureHint != nil {
		t.Errorf("hint with feature runs = %+v", b.FeatureHint)
	}
	// feature runs that all aborted are no recognised feature build
	aborted := otherRuns("ABORTED", "PR-3", "PR-4")
	for i := range aborted {
		aborted[i].BranchKind = dataset.BranchPR
	}
	if b := hinted(aborted, otherRuns("SUCCESS", "kas-1-x")); b.FeatureHint == nil || b.FeatureHint.Runs != 1 {
		t.Errorf("hint with aborted feature runs = %+v", b.FeatureHint)
	}
	if b := hinted(otherRuns("FAILURE", "kas-1-x")); b.FeatureHint == nil || b.FeatureHint.Runs != 1 {
		t.Errorf("hint = %+v", b.FeatureHint)
	}
	// no runs at all
	if b := hinted(); b.FeatureHint != nil || len(b.Kinds) != 0 {
		t.Errorf("empty = %+v", b)
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
	// A release branch is told by its build_type, its branch is other.
	for bt, want := range map[string]string{"": KindOther, "build": KindOther, "release": KindRelease, "hotfix": KindRelease} {
		got := RunKind(run(func(r *dataset.Run) {
			r.Branch = dataset.Branch("kasse-api/release%2F1.2")
			r.BranchKind = dataset.BranchKind(r.Branch)
			r.BuildType = nil
			if bt != "" {
				r.BuildType = s(bt)
			}
		}), agoP(30))
		if got != want {
			t.Errorf("release/1.2, build_type %q = %s, want %s", bt, got, want)
		}
	}
}

func TestRepoDurations(t *testing.T) {
	h := buildAlone(buildData(), p4(), filter{}).Hotspots
	byKind := map[string]KindHotspots{}
	for _, k := range h.Kinds {
		byKind[k.Kind] = k
	}
	main := byKind[KindMain].Repos
	// Both runs count as runs, the green one's 600 s alone as duration.
	if len(main) != 1 || main[0].Repo != "kasse/kasse-api" || main[0].Runs != 2 || *main[0].Duration.P50 != 600 {
		t.Errorf("main = %+v", main)
	}
	if other := byKind[KindOther].Repos; other[0].Repo != "nightly" {
		t.Errorf("other = %+v", other)
	}
}

// A UI repo whose green builds take 25 minutes and whose red ones stop
// after 2: its build takes 25 minutes, not the 13 a mixed median says.
func TestDurationsGreenOnly(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		for i, d := range []int64{1500, 1500, 1500, 120, 120, 120} {
			status := "SUCCESS"
			if d == 120 {
				status = "FAILURE"
			}
			ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
				r.RunID, r.Repo, r.DurationS, r.Status = string(rune('a'+i)), s("kasse/kasse-ui"), n(d), s(status)
			}))
		}
	})
	b := buildAlone(ds, p4(), filter{})
	expectNear(t, "kind p50", b.Kinds[0].Duration.P50, 1500)
	k := b.Hotspots.Kinds[0]
	expectNear(t, "team p50", k.Duration.P50, 1500)
	if r := repoOf(k, "kasse/kasse-ui"); r.Runs != 6 || r.Duration.N != 3 || *r.Duration.P50 != 1500 {
		t.Errorf("repo = %+v", r)
	}
	if len(k.Slowest) != 1 || *k.Slowest[0].Duration.P50 != 1500 {
		t.Errorf("slowest = %+v", k.Slowest)
	}
}

func TestCosts(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Reviews = []dataset.Review{
			reviewRow(func(r *dataset.Review) { r.CostUSD = s("1.250000") }),
			reviewRow(func(r *dataset.Review) {
				r.CostUSD, r.LinesAdded, r.LinesRemoved = s("0.750000"), n(300), n(200)
			}),
			reviewRow(func(r *dataset.Review) { r.Outcome, r.CostUSD = s("declined"), s("0.400000") }),
			reviewRow(func(r *dataset.Review) { r.Outcome, r.CostUSD = s("deleted"), nil }),
			reviewRow(func(r *dataset.Review) {
				r.Repo, r.Project, r.CostUSD = s("portal/portal-web"), s("portal"), s("3.000000")
			}),
			// Stage promotions: in the total and the rows, on their own line,
			// out of merged, wasted and per kloc whatever their outcome.
			reviewRow(func(r *dataset.Review) {
				r.Repo, r.GitOps, r.CostUSD, r.LinesAdded, r.LinesRemoved = s("kasse/kasse-gitops"), true, s("0.200000"), n(1), n(1)
			}),
			reviewRow(func(r *dataset.Review) {
				r.Repo, r.GitOps, r.Outcome, r.CostUSD = s("kasse/kasse-gitops"), true, s("declined"), s("0.100000")
			}),
		}
	})
	c := costs(ds, filter{})
	if !near(c.Total, 5.7) || !near(c.Merged, 5) || c.MergedReviews != 3 || !near(c.Wasted, 0.4) || c.ReviewedRepos != 3 {
		t.Errorf("costs = %+v", c)
	}
	if !near(c.Promotion, 0.3) || c.PromotionReviews != 2 {
		t.Errorf("promotion = %v over %d", c.Promotion, c.PromotionReviews)
	}
	expectNear(t, "per kloc", c.PerKloc, 5.0/1500*1000)
	expectNear(t, "wasted share", c.WastedShare, 0.4/5.4)
	// Only promotions: nothing to take a share of.
	onlyPromotion := data(func(ds *dataset.Dataset) {
		ds.Reviews = []dataset.Review{reviewRow(func(r *dataset.Review) { r.GitOps = true })}
	})
	if c := costs(onlyPromotion, filter{}); c.WastedShare != nil || c.PromotionReviews != 1 || !near(c.Total, 0.5) {
		t.Errorf("only promotion = %+v", c)
	}
	if len(c.Rows) != 2 || c.Rows[0].Label != "portal" || c.Rows[1].Label != "kasse" || !near(c.Rows[1].Value, 2.7) {
		t.Errorf("rows = %+v", c.Rows)
	}
	k := costs(ds, filter{product: "kasse"})
	if len(k.Rows) != 2 || k.Rows[0].Label != "kasse/kasse-api" || !near(k.Rows[0].Value, 2.4) ||
		k.Rows[1].Label != "kasse/kasse-gitops" || !near(k.Rows[1].Value, 0.3) {
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
	m := Compute(ds, noPrevious(ds), nil, Params{Period: p4(), Product: "portal"})
	if m.Product == nil || *m.Product != "portal" || m.Review.Merged != 1 || len(m.Period.Weeks) != 4 ||
		len(m.Review.CycleHistogram.UpperBoundsS) != len(m.Review.CycleHistogram.Human)-1 {
		t.Errorf("portal = %+v", m)
	}
	// A product without data is all of them.
	if m := Compute(ds, noPrevious(ds), nil, Params{Period: p4(), Product: "nope"}); m.Product != nil || m.Review.Merged != 3 {
		t.Errorf("unknown product = %+v", m.Review)
	}
}
