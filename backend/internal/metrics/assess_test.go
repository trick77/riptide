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
		{1, 0, count, MagnitudeNotable}, // damped: 3/2
		{3, 2, count, MagnitudeNotable}, // damped: 5/4, exactly NotableChange
		{11, 10, count, MagnitudeSame},  // 10 %: inside the tolerance
		{13, 11, count, MagnitudeMinor}, // damped: 15/13, moved, no news
		{20, 10, count, MagnitudeNotable},
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
		for i, d := range []float64{9, 10, 12} {
			out = append(out, openPR(func(o *dataset.OpenPR) {
				o.PRID, o.OpenedAt, o.ReadyAt, o.LastEventAt = int64(30+i), before(end, d), ptr(before(end, d)), before(end, 1)
			}))
		}
		return out
	}
	runs := func(end time.Time, repo string, secs int64) []dataset.Run {
		var out []dataset.Run
		for i := range 4 {
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
	if jam == nil || jam.Tone != ToneAttention || jam.Waiting == nil || jam.Waiting.FirstReview != 3 {
		t.Fatalf("jam = %+v, want attention (jammed before too), 3 waiting on a first review; have %v", jam, ids)
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
