package metrics

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

func TestMagnitude(t *testing.T) {
	for _, c := range []struct {
		cur, prev float64
		s         scale
		want      string
	}{
		{100, 95, relative, MagnitudeSame},  // inside the tolerance
		{100, 85, relative, MagnitudeMinor}, // moved, not news
		{100, 75, relative, MagnitudeNotable},
		{75, 100, relative, MagnitudeNotable},
		{3, 0, relative, MagnitudeNotable},
		{0, 0, relative, MagnitudeSame},
		{0.90, 0.88, points, MagnitudeSame},
		{0.90, 0.86, points, MagnitudeMinor},
		{0.90, 0.85, points, MagnitudeNotable},
		{1, 0, count, MagnitudeMinor},     // damped 3/2, but one more is no news
		{2, 1, count, MagnitudeMinor},     // damped 4/3, one more again
		{3, 1, count, MagnitudeNotable},   // damped 5/3, two more
		{4, 2, count, MagnitudeNotable},   // damped 6/4
		{11, 10, count, MagnitudeSame},    // 10 %: inside the tolerance
		{13, 11, count, MagnitudeMinor},   // damped: 15/13, moved, no news
		{13, 10, count, MagnitudeNotable}, // damped: 15/12, exactly NotableChange
		{20, 10, count, MagnitudeNotable},
		{0, 1, count, MagnitudeMinor}, // one fewer is no news either
		{1, 2, count, MagnitudeMinor},
		{0, 2, count, MagnitudeNotable},
	} {
		if got := magnitude(c.cur, c.prev, c.s); got != c.want {
			t.Errorf("magnitude(%v, %v, %v) = %s, want %s", c.cur, c.prev, c.s, got, c.want)
		}
	}
}

func TestClosedPeriod(t *testing.T) {
	p := ClosedPeriod(4, now, zurich)
	if want := time.Date(2026, 11, 9, 0, 0, 0, 0, zurich); !p.To.Equal(want) {
		t.Errorf("to = %v, want %v (the running week left out)", p.To, want)
	}
	if want := time.Date(2026, 10, 12, 0, 0, 0, 0, zurich); !p.From.Equal(want) {
		t.Errorf("from = %v, want %v", p.From, want)
	}
	// 4 weeks and the hour the DST switch on 25.10. adds.
	if len(p.Weeks) != 4 || !near(p.Span, 4+1.0/168) {
		t.Errorf("weeks %d span %v, want 4 whole weeks", len(p.Weeks), p.Span)
	}
	pp := ClosedPrevious(4, now, zurich)
	if !pp.To.Equal(p.From) || len(pp.Weeks) != 4 {
		t.Errorf("previous %v..%v, want 4 weeks up to %v", pp.From, pp.To, p.From)
	}
}

// assessFixture is a team with a jam that was already there, a build slow
// in both periods, a review time that doubled through one repo, and Argo
// CD silent for ten days.
func assessFixture(t *testing.T, h History) Assessment {
	t.Helper()
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	before := func(end time.Time, days float64) time.Time {
		return end.Add(-time.Duration(days * float64(24*time.Hour))).UTC()
	}
	jam := func(end time.Time) []dataset.OpenPR {
		var out []dataset.OpenPR
		for i, d := range []float64{9, 10, 12, 14} {
			out = append(out, openPR(func(o *dataset.OpenPR) {
				o.PRID, o.OpenedAt, o.ReadyAt, o.LastEventAt = int64(30+i), before(end, d), ptr(before(end, d)), before(end, 1)
			}))
		}
		return out
	}
	runs := func(end time.Time, repo string, secs int64) []dataset.Run {
		// Enough runs to judge a repo: a resolution needs a judged number.
		var out []dataset.Run
		for i := range HotspotMinRuns {
			at := before(end, float64(3+i))
			out = append(out, run(func(r *dataset.Run) {
				r.Repo, r.PipelineName, r.RunID = s(repo), repo+"/master", fmt.Sprintf("%s-%d", repo, i)
				r.StartedAt, r.FinishedAt, r.DurationS = &at, ptr(at.Add(time.Duration(secs)*time.Second)), n(secs)
			}))
		}
		return out
	}
	prs := func(end time.Time, repo string, review time.Duration, k int) []dataset.PR {
		var out []dataset.PR
		for i := range k {
			ready := before(end, float64(2+i%10))
			out = append(out, pr(func(p *dataset.PR) {
				p.Repo, p.PRID = repo, int64(100+i)
				p.OpenedAt, p.ReadyAt, p.FirstCommitAt = &ready, &ready, ptr(ready.Add(-time.Hour))
				p.ApprovedAt, p.FirstReviewAt = ptr(ready.Add(review)), ptr(ready.Add(review))
				p.MergedAt = ptr(ready.Add(review + time.Hour))
			}))
		}
		return out
	}
	mk := func(p Period, review time.Duration) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			ds.Meta.From, ds.Meta.To = p.From, p.To
			ds.Meta.DataStart = dataset.SourceTimes{Bitbucket: agoP(200), Pipeline: agoP(200), ArgoCD: agoP(200), Noergler: agoP(200)}
			ds.Meta.BuildDataFrom, ds.Meta.BuildTypeFrom = agoP(200), agoP(200)
			ds.Meta.DataEnd.ArgoCD = ptr(before(cur.To, 10))
			ds.OpenPRs = jam(p.To)
			ds.Runs = slices.Concat(runs(p.To, "kasse/kasse-web", 1500), runs(p.To, "kasse/kasse-api", 300),
				runs(p.To, "kasse/kasse-cli", 200))
			ds.PRs = slices.Concat(prs(p.To, "kasse/kasse-api", review, 12), prs(p.To, "portal/portal-web", 2*time.Hour, 12))
		})
	}
	return Assess(mk(cur, 20*time.Hour), mk(prev, 2*time.Hour), Params{Period: cur, Previous: prev}, h)
}

func signal(a Assessment, id string) *Signal {
	for i := range a.Signals {
		if a.Signals[i].ID == id {
			return &a.Signals[i]
		}
	}
	return nil
}

func TestAssessSignals(t *testing.T) {
	a := assessFixture(t, History{})
	ids := make([]string, len(a.Signals))
	for i, s := range a.Signals {
		ids[i] = s.ID
	}
	if g := signal(a, "data_gap:argocd"); g == nil || g.Tone != ToneGate || a.Signals[0].ID != g.ID {
		t.Fatalf("argocd gap not first: %v", ids)
	}
	if !slices.Equal(a.Gaps, []string{SourceArgoCD}) {
		t.Errorf("gaps = %v", a.Gaps)
	}
	for _, sg := range a.Signals {
		if sg.Source == SourceArgoCD && sg.Kind != SignalDataGap {
			t.Errorf("%s survived the argocd gap", sg.ID)
		}
	}
	jam := signal(a, "pr_jam:kasse/kasse-api")
	if jam == nil || jam.Tone != ToneAttention || jam.Waiting == nil || jam.Waiting.FirstReview != 4 {
		t.Fatalf("jam = %+v, want attention (jammed before too), 4 waiting on a first review; have %v", jam, ids)
	}
	slow := signal(a, "steady_slow:main:kasse/kasse-web")
	if signal(a, "steady_slow:main:kasse/kasse-api") != nil {
		t.Errorf("kasse-api at the kind's median is no steady slow build")
	}
	if slow == nil {
		t.Fatalf("no steady_slow for kasse-web: %v", ids)
	}
	expectNear(t, "slow p50", slow.Facts[0].Value, 1500)
	rev := signal(a, "change:review_time")
	if rev == nil || rev.Tone != ToneAttention || len(rev.Drivers) != 1 || rev.Drivers[0].Repo != "kasse/kasse-api" {
		t.Fatalf("review change = %+v, want worse, driven by kasse-api", rev)
	}
	expectNear(t, "driver share", &rev.Drivers[0].Share, 1)
	// The jam, the slow build and the driver: kasse-api and kasse-web lead.
	if len(a.Focus) < 2 || a.Focus[0].Repo != "kasse/kasse-api" {
		t.Fatalf("focus = %+v", a.Focus)
	}
	if !slices.ContainsFunc(a.Focus, func(f FocusRepo) bool { return f.Repo == "kasse/kasse-web" }) {
		t.Errorf("kasse-web not in the focus: %+v", a.Focus)
	}
	// kasse-cli built, portal-web merged: active, no signal.
	if a.Calm < 2 || a.Active < 4 {
		t.Errorf("active %d calm %d", a.Active, a.Calm)
	}
}

// A jam card turns attention for more jammed repos only where that is
// news: one more is not, two more are.
func TestJamToneFromNotable(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	jammed := func(end time.Time, repos ...string) []dataset.OpenPR {
		var out []dataset.OpenPR
		for _, repo := range repos {
			for i := range OpenJamMin {
				at := end.Add(-time.Duration(9+i) * 24 * time.Hour).UTC()
				out = append(out, openPR(func(o *dataset.OpenPR) {
					o.Repo, o.PRID, o.OpenedAt, o.ReadyAt = repo, int64(len(out)+1), at, ptr(at)
					o.LastEventAt = end.Add(-24 * time.Hour).UTC()
				}))
			}
		}
		return out
	}
	mk := func(p Period, open []dataset.OpenPR) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			ds.Meta.From, ds.Meta.To = p.From, p.To
			ds.Meta.DataStart = dataset.SourceTimes{Bitbucket: agoP(200), Pipeline: agoP(200), ArgoCD: agoP(200), Noergler: agoP(200)}
			ds.OpenPRs = open
		})
	}
	for _, c := range []struct {
		name  string
		repos []string
		want  string
	}{
		{"one more", []string{"kasse/kasse-api"}, ToneHint},
		{"two more", []string{"kasse/kasse-api", "portal/portal-web"}, ToneAttention},
	} {
		a := Assess(mk(cur, jammed(cur.To, c.repos...)), mk(prev, nil), Params{Period: cur, Previous: prev}, History{})
		for _, repo := range c.repos {
			if s := signal(a, "pr_jam:"+repo); s == nil || s.Tone != c.want {
				t.Errorf("%s: pr_jam:%s = %+v, want %s", c.name, repo, s, c.want)
			}
		}
	}
}

// covered is dataset meta with every source heard of long before both
// periods.
func covered(ds *dataset.Dataset, p Period) {
	ds.Meta.From, ds.Meta.To = p.From, p.To
	ds.Meta.DataStart = dataset.SourceTimes{Bitbucket: agoP(200), Pipeline: agoP(200), ArgoCD: agoP(200), Noergler: agoP(200)}
	ds.Meta.BuildDataFrom, ds.Meta.BuildTypeFrom = agoP(200), agoP(200)
}

// The assessment reads every repo, not the page's top rows: a repo that
// stopped failing more ranks below nine wasting more, and is resolved.
func TestResolvedPastTheCut(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	runs := func(end time.Time, repo string, failures int) []dataset.Run {
		var out []dataset.Run
		for i := range HotspotMinRuns + 2 {
			at := end.Add(-time.Duration(2+i) * 24 * time.Hour).UTC()
			status := "SUCCESS"
			if i < failures {
				status = "FAILURE"
			}
			out = append(out, run(func(r *dataset.Run) {
				r.Repo, r.PipelineName, r.RunID = s(repo), repo+"/master", fmt.Sprintf("%s-%d", repo, i)
				r.StartedAt, r.FinishedAt, r.DurationS, r.Status = &at, ptr(at.Add(10*time.Minute)), n(600), s(status)
			}))
		}
		return out
	}
	mk := func(p Period) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			covered(ds, p)
			ds.Runs = runs(p.To, "kasse/kasse-api", 0)
			for i := range 9 {
				ds.Runs = append(ds.Runs, runs(p.To, fmt.Sprintf("kasse/r%d", i), 2)...)
			}
		})
	}
	h := History{Runs: 1, Seen: map[string]int{"failing_more:main:kasse/kasse-api": 1},
		Latest: map[string]string{"failing_more:main:kasse/kasse-api": ToneAttention}}
	a := Assess(mk(cur), mk(prev), Params{Period: cur, Previous: prev}, h)
	if s := signal(a, "resolved:failing_more:main:kasse/kasse-api"); s == nil || len(s.Facts) != 1 {
		t.Errorf("resolution past the cut = %+v", s)
	}

	// Too few runs to judge, or still worse: not resolved, whatever the
	// flag says. Three runs, two red.
	few := func(p Period, failures int) *dataset.Dataset {
		ds := mk(p)
		ds.Runs = slices.DeleteFunc(ds.Runs, func(r dataset.Run) bool { return *r.Repo == "kasse/kasse-api" })
		ds.Runs = append(ds.Runs, runs(p.To, "kasse/kasse-api", failures)[:3]...)
		return ds
	}
	a = Assess(few(cur, 2), mk(prev), Params{Period: cur, Previous: prev}, h)
	if s := signal(a, "resolved:failing_more:main:kasse/kasse-api"); s != nil {
		t.Errorf("resolved on a low sample: %+v", s)
	}
	// A steady slow build with too few timed runs to rank is not resolved.
	h = History{Runs: 1, Seen: map[string]int{"steady_slow:main:kasse/kasse-api": 1},
		Latest: map[string]string{"steady_slow:main:kasse/kasse-api": ToneAttention}}
	a = Assess(few(cur, 0), mk(prev), Params{Period: cur, Previous: prev}, h)
	if s := signal(a, "resolved:steady_slow:main:kasse/kasse-api"); s != nil {
		t.Errorf("steady slow resolved without a rank: %+v", s)
	}
}

// Every repo with an open PR is active, past the open section's top rows.
func TestActivePastTheOpenCut(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	mk := func(p Period, open []dataset.OpenPR) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			covered(ds, p)
			ds.OpenPRs = open
		})
	}
	var open []dataset.OpenPR
	for i := range 10 {
		at := cur.To.Add(-48 * time.Hour).UTC()
		open = append(open, openPR(func(o *dataset.OpenPR) {
			o.Repo, o.PRID, o.OpenedAt, o.ReadyAt, o.LastEventAt = fmt.Sprintf("kasse/r%d", i), int64(i+1), at, ptr(at), at
		}))
	}
	a := Assess(mk(cur, open), mk(prev, nil), Params{Period: cur, Previous: prev}, History{})
	if a.Active != 10 {
		t.Errorf("active = %d, want 10", a.Active)
	}
}

// Merged PRs per week count every merged PR, also one opened before
// ingestion: its unknown opening keeps it out of the cycle time only.
func TestMergedPerWeekCountsEveryMerge(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	mk := func(p Period) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			covered(ds, p)
			for i := range 12 {
				at := p.To.Add(-time.Duration(2+i) * 24 * time.Hour).UTC()
				ds.PRs = append(ds.PRs, pr(func(x *dataset.PR) {
					x.PRID, x.MergedAt, x.OpenedAt = int64(i+1), &at, ptr(at.Add(-time.Hour))
					x.FirstCommitAt, x.ReadyAt = ptr(at.Add(-2*time.Hour)), ptr(at.Add(-time.Hour))
					x.FirstReviewAt, x.ApprovedAt = ptr(at.Add(-30*time.Minute)), ptr(at.Add(-30*time.Minute))
					if i < 4 {
						x.OpenedAt = nil
					}
				}))
			}
			for i := range 3 {
				at := p.To.Add(-48 * time.Hour).UTC()
				ds.OpenPRs = append(ds.OpenPRs, openPR(func(o *dataset.OpenPR) {
					o.PRID, o.OpenedAt, o.ReadyAt, o.LastEventAt = int64(50+i), at, ptr(at), at
				}))
			}
		})
	}
	a := Assess(mk(cur), mk(prev), Params{Period: cur, Previous: prev}, History{})
	team := func(id string) Fact {
		for _, f := range a.Team {
			if f.ID == id {
				return f
			}
		}
		t.Fatalf("no %s in %+v", id, a.Team)
		return Fact{}
	}
	merged := team("team.merged_per_week")
	expectNear(t, "merged per week", merged.Value, 12/cur.Span)
	if merged.Prev == nil {
		t.Errorf("merged per week not compared: %+v", merged)
	}
	wip := team("team.wip_weeks")
	expectNear(t, "wip weeks", wip.Value, 3/(12/cur.Span))
	if wip.Prev == nil {
		t.Errorf("wip weeks not compared: %+v", wip)
	}
}

// A repo without an open PR at the previous end had none open long: a jam
// new since then compares against 0.
func TestNewJamAgainstZero(t *testing.T) {
	cur, prev := ClosedPeriod(4, now, zurich), ClosedPrevious(4, now, zurich)
	var open []dataset.OpenPR
	for i := range OpenJamMin {
		at := cur.To.Add(-time.Duration(9+i) * 24 * time.Hour).UTC()
		open = append(open, openPR(func(o *dataset.OpenPR) {
			o.PRID, o.OpenedAt, o.ReadyAt, o.LastEventAt = int64(i+1), at, ptr(at), cur.To.Add(-24*time.Hour).UTC()
		}))
	}
	mk := func(p Period, open []dataset.OpenPR) *dataset.Dataset {
		return data(func(ds *dataset.Dataset) {
			covered(ds, p)
			ds.OpenPRs = open
		})
	}
	a := Assess(mk(cur, open), mk(prev, nil), Params{Period: cur, Previous: prev}, History{})
	jam := signal(a, "pr_jam:kasse/kasse-api")
	if jam == nil {
		t.Fatalf("no jam: %+v", a.Signals)
	}
	if l := jam.Facts[0]; l.Prev == nil || *l.Prev != 0 || l.Verdict != ChangeWorse {
		t.Errorf("open_long = %+v, want against 0, worse", l)
	}
	// Bitbucket heard of only within the previous period: no comparison.
	late := func(ds *dataset.Dataset) { ds.Meta.DataStart.Bitbucket = ptr(prev.From.Add(24 * time.Hour)) }
	cd, pd := mk(cur, open), mk(prev, nil)
	late(cd)
	late(pd)
	a = Assess(cd, pd, Params{Period: cur, Previous: prev}, History{})
	jam = signal(a, "pr_jam:kasse/kasse-api")
	if jam == nil {
		t.Fatalf("no jam, previous period uncovered: %+v", a.Signals)
	}
	if l := jam.Facts[0]; l.Prev != nil || l.Verdict != ChangeNone {
		t.Errorf("open_long, previous period uncovered = %+v", l)
	}
}

func TestAssessHistory(t *testing.T) {
	a := assessFixture(t, History{
		Seen: map[string]int{"pr_jam:kasse/kasse-api": 2},
		Latest: map[string]string{
			"pr_jam:kasse/kasse-api":            ToneAttention,
			"failing_more:main:kasse/kasse-cli": ToneAttention,
			"change:within_day":                 ToneGood, // a gone improvement is no news
			"change:open_jammed":                ToneAttention,
			"steady_slow:main:kasse/kasse-x":    ToneHint, // no runs now: nothing to show
			// slower last time, steady slow now: still slow, not resolved
			"slower_build:main:kasse/kasse-web": ToneAttention,
		},
	})
	if s := signal(a, "pr_jam:kasse/kasse-api"); s == nil || s.Seen != 2 {
		t.Errorf("jam seen = %+v", s)
	}
	if signal(a, "resolved:failing_more:main:kasse/kasse-cli") == nil {
		t.Errorf("failing more kasse-cli not resolved")
	}
	if signal(a, "resolved:change:within_day") != nil {
		t.Errorf("a gone improvement resolved")
	}
	if signal(a, "resolved:pr_jam:kasse/kasse-api") != nil {
		t.Errorf("a jam still there resolved")
	}
	if signal(a, "resolved:slower_build:main:kasse/kasse-web") != nil {
		t.Errorf("a build still slow resolved")
	}
	if signal(a, "resolved:steady_slow:main:kasse/kasse-x") != nil {
		t.Errorf("resolved without a number")
	}
}

// Every better team number is evidence of what runs, a small gain too; a
// worse one waits for notable, the larger gain ranks first, and the better
// half of a throughput / instability pair is no gain.
func TestChangeSignalsGood(t *testing.T) {
	ch := func(verdict, mag string, score float64) Change {
		return Change{Verdict: verdict, Directed: true, Magnitude: ptr(mag), Score: ptr(score), Prev: ptr(1.0)}
	}
	team := func(metric string, c Change) teamChange {
		return teamChange{metric, metric, UnitSeconds, SourceBitbucket, ptr(2.0), c}
	}
	a := &assessor{gaps: map[string]bool{}, team: []teamChange{
		team("lead_time", ch(ChangeBetter, MagnitudeMinor, 0.2)),
		team("within_day", ch(ChangeBetter, MagnitudeNotable, 0.6)),
		team("wip_weeks", ch(ChangeWorse, MagnitudeMinor, -0.2)),
		team("merged_per_week", ch(ChangeBetter, MagnitudeSame, 0)),
		{"deploy_frequency", "deploy_per_week", UnitPerWeek, SourceArgoCD, ptr(2.0), ch(ChangeBetter, MagnitudeMinor, 0.3)},
	}}
	a.m.Comparison.DeployFreq = ch(ChangeBetter, MagnitudeMinor, 0.3)
	a.m.Comparison.DeploySuccess = ch(ChangeWorse, MagnitudeMinor, -0.1)
	a.changeSignals()
	rank(a.out)
	ids := []string{}
	for _, s := range a.out {
		ids = append(ids, s.ID+"/"+s.Tone)
	}
	want := []string{"throughput_instability/hint", "change:within_day/good", "change:lead_time/good"}
	if !slices.Equal(ids, want) {
		t.Errorf("signals = %v, want %v", ids, want)
	}
}

// A card's weeks come only where the page counts them anyway: releases
// per week and a kind's success rate, both periods; none for the rest.
func TestSeries(t *testing.T) {
	a := &assessor{}
	w0 := time.Date(2026, 11, 2, 0, 0, 0, 0, zurich)
	week := func(days int) Week { return Week{Start: w0.AddDate(0, 0, days), End: w0.AddDate(0, 0, days+7)} }
	a.p.Previous.Weeks = []Week{week(0)}
	a.p.Period.Weeks = []Week{week(7), week(14)}
	// Argo CD reported from the period's first Wednesday on: the previous
	// week is no week of zero releases, the one it started in counts.
	a.m.Meta.DataStart.ArgoCD = ptr(w0.AddDate(0, 0, 9))
	a.m.Delivery.Frequency.Weeks = []DeployWeek{{Release: 2}, {Release: 3, Sync: 4}}
	a.pm.Delivery.Frequency.Weeks = []DeployWeek{{Release: 0}}
	a.m.Build.Kinds = []BuildStats{{Kind: "feature", Weekly: []*float64{ptr(0.9), nil}}}
	vals := func(xs []*float64) []any {
		out := []any{}
		for _, x := range xs {
			if x == nil {
				out = append(out, nil)
			} else {
				out = append(out, *x)
			}
		}
		return out
	}
	d := a.series("deploy_frequency")
	if d == nil || fmt.Sprint(vals(d.Prev)) != "[<nil>]" || fmt.Sprint(vals(d.Cur)) != "[2 3]" {
		t.Errorf("deploys = %+v", d)
	}
	b := a.series("build_success:feature")
	if b == nil || len(b.Prev) != 0 || b.Prev == nil || fmt.Sprint(vals(b.Cur)) != "[0.9 <nil>]" {
		t.Errorf("builds = %+v", b)
	}
	if a.series("review_time") != nil {
		t.Errorf("a series for a number no week counts")
	}
}

// A worsening the latest assessment raised that is a gain now is resolved,
// told once: no good change beside the resolution.
func TestResolvedGainToldOnce(t *testing.T) {
	c := Change{Verdict: ChangeBetter, Directed: true, Magnitude: ptr(MagnitudeMinor), Score: ptr(0.2), Prev: ptr(3.0)}
	a := &assessor{gaps: map[string]bool{},
		h:    History{Latest: map[string]string{"change:lead_time": ToneAttention}},
		team: []teamChange{{"lead_time", "lead_time_p50", UnitSeconds, SourceArgoCD, ptr(2.0), c}}}
	a.changeSignals()
	a.resolvedSignals()
	if len(a.out) != 1 || a.out[0].ID != "resolved:change:lead_time" {
		t.Errorf("signals = %+v, want the resolution alone", a.out)
	}
}

// A change that flips between gain and worsening keeps its ID but starts a
// streak of its own: no "seen before" carried over from the other tone.
func TestSeenPerTone(t *testing.T) {
	a := assessFixture(t, History{
		Seen:   map[string]int{"change:review_time": 2, "pr_jam:kasse/kasse-api": 2},
		Latest: map[string]string{"change:review_time": ToneGood, "pr_jam:kasse/kasse-api": ToneAttention},
	})
	if s := signal(a, "change:review_time"); s == nil || s.Seen != 0 {
		t.Errorf("worsening after gains seen = %+v", s)
	}
	if s := signal(a, "pr_jam:kasse/kasse-api"); s == nil || s.Seen != 2 {
		t.Errorf("jam seen = %+v", s)
	}
}

// A worsening still worse, only no longer notably, is not done.
func TestStillWorseNotResolved(t *testing.T) {
	c := Change{Verdict: ChangeWorse, Directed: true, Magnitude: ptr(MagnitudeMinor), Score: ptr(-0.2), Prev: ptr(3.0)}
	a := &assessor{gaps: map[string]bool{},
		h:    History{Latest: map[string]string{"change:lead_time": ToneAttention}},
		team: []teamChange{{"lead_time", "lead_time_p50", UnitSeconds, SourceArgoCD, ptr(3.5), c}}}
	a.changeSignals()
	a.resolvedSignals()
	if len(a.out) != 0 {
		t.Errorf("signals = %+v, want none", a.out)
	}
}

// The better half of a trade-off resolves nothing: the earlier worsening
// of deploy success is not done while the trade-off says it is no gain.
func TestTradeoffResolvesNothing(t *testing.T) {
	better := Change{Verdict: ChangeBetter, Directed: true, Magnitude: ptr(MagnitudeMinor), Score: ptr(0.2), Prev: ptr(0.8)}
	worse := Change{Verdict: ChangeWorse, Directed: true, Magnitude: ptr(MagnitudeMinor), Score: ptr(-0.2), Prev: ptr(4.0)}
	a := &assessor{gaps: map[string]bool{},
		h:    History{Latest: map[string]string{"change:deploy_success": ToneAttention}},
		team: []teamChange{{"deploy_success", "deploy_success", UnitShare, SourceArgoCD, ptr(0.9), better}}}
	a.m.Comparison.DeployFreq, a.m.Comparison.DeploySuccess = worse, better
	a.changeSignals()
	a.resolvedSignals()
	if len(a.out) != 1 || a.out[0].ID != "throughput_instability" {
		t.Errorf("signals = %+v, want the trade-off alone", a.out)
	}
}
