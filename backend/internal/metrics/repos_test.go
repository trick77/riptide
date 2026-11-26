package metrics

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// traceRelease is a release of app to production days before the period's
// end that tracing knew of long before, its stretch of the repo unknown.
func traceRelease(app string, days float64) dataset.TraceRelease {
	return dataset.TraceRelease{App: app, Environment: "prod", At: endAgo(days), Known: endAgo(300)}
}

// onRepo moves a PR to repo, its project the repo's.
func onRepo(repo string) func(*dataset.PR) {
	return func(p *dataset.PR) { p.Repo, p.Project = repo, projectOf(repo) }
}

// onProd ships a PR merged days before the period's end to production wait
// days later.
func onProd(days, wait float64) func(*dataset.PR) {
	return func(p *dataset.PR) {
		p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: endAgo(days - wait),
			Route: dataset.RouteCompare}}
	}
}

func rowOf(t *testing.T, v RepoView, repo string) RepoRow {
	t.Helper()
	i := slices.IndexFunc(v.Rows, func(r RepoRow) bool { return r.Repo == repo })
	if i < 0 {
		t.Fatalf("no row for %s in %+v", repo, v.Rows)
	}
	return v.Rows[i]
}

func spreadOf(t *testing.T, v RepoView, key string, kind *string) RepoSpread {
	t.Helper()
	for _, s := range v.Spreads {
		if s.Key == key && (kind == nil) == (s.Kind == nil) && (kind == nil || *kind == *s.Kind) {
			return s
		}
	}
	t.Fatalf("no spread %s", key)
	return RepoSpread{}
}

// repoData is a team of six repos, one per group: kasse-api has released
// nothing for 40 days while worked on, kasse-ui 20 days back, lager-api
// weekly but slow to production, lager-lib a library, portal-web is no
// App's known, tools-cli nobody works on.
func repoData() *dataset.Dataset {
	return stateData(func(d *dataset.Dataset) {
		d.Apps = append(d.Apps, dataset.App{Name: "kasse-canary", Project: s("kasse")},
			dataset.App{Name: "lager-prod", Project: s("lager")}, dataset.App{Name: "tools-prod", Project: s("tools")})
		lager := []dataset.TraceRelease{}
		for w := range 8 {
			lager = append(lager, traceRelease("lager-prod", 3+7*float64(w)))
		}
		d.Traces = map[string]dataset.RepoTrace{
			"kasse/kasse-api": {DeployedSince: endAgoP(150), Releases: []dataset.TraceRelease{traceRelease("kasse-prod", 40),
				// After the period's end: not one of it.
				traceRelease("kasse-prod", -1),
				// Shipped nothing of the repo.
				{App: "kasse-prod", Environment: "prod", At: endAgo(10), Known: endAgo(300), Unchanged: true}}},
			// One revision on two Apps: one release.
			"kasse/kasse-ui": {DeployedSince: endAgoP(150), Releases: []dataset.TraceRelease{
				traceRelease("kasse-prod", 20), traceRelease("kasse-canary", 19.9)}},
			"lager/lager-api": {DeployedSince: endAgoP(150), Releases: lager},
			"tools/tools-cli": {DeployedSince: endAgoP(150), Releases: []dataset.TraceRelease{traceRelease("tools-prod", 50)}},
		}
		d.Deploys = []dataset.Deploy{
			deploy(func(x *dataset.Deploy) { x.App, x.Revision, x.At = "kasse-prod", "r-ui", endAgo(20) }),
			deploy(func(x *dataset.Deploy) { x.App, x.Revision, x.At = "kasse-canary", "r-ui", endAgo(19.9) }),
		}
		d.PRs = []dataset.PR{
			merged(1, 5),
			merged(2, 6, onRepo("kasse/kasse-ui")),
			merged(10, 4, onRepo("lager/lager-lib"), func(p *dataset.PR) { p.Library = true }),
			merged(11, 4, onRepo("portal/portal-web")),
		}
		// Six of lager-api's PRs, each 16 days to production.
		for i := range 6 {
			days := 20 + float64(i)
			d.PRs = append(d.PRs, merged(int64(20+i), days, onRepo("lager/lager-api"), onProd(days, 16)))
		}
	})
}

// repoPrev is the previous period: tools-cli's last work, before its last
// release and more than release_breach_days back.
func repoPrev(ds *dataset.Dataset) *dataset.Dataset {
	return &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps, Traces: map[string]dataset.RepoTrace{},
		PRs: []dataset.PR{merged(30, 55, onRepo("tools/tools-cli"))}}
}

func repoViewOf(ds, prev *dataset.Dataset) RepoView {
	return Compute(ds, prev, nil, Params{Period: stCur, Previous: stPrev}).Repos
}

func TestRepoViewGroupsAndRanking(t *testing.T) {
	ds := repoData()
	v := repoViewOf(ds, repoPrev(ds))
	var order []string
	for _, r := range v.Rows {
		order = append(order, r.Repo+":"+r.Group)
	}
	want := []string{"kasse/kasse-api:none", "kasse/kasse-ui:rare", "portal/portal-web:unjudged",
		"lager/lager-api:regular", "lager/lager-lib:library", "tools/tools-cli:idle"}
	if !slices.Equal(order, want) {
		t.Fatalf("rows = %v, want %v", order, want)
	}
	api := rowOf(t, v, "kasse/kasse-api")
	// The release after the end and the one that left it unchanged count
	// for nothing.
	if api.DaysSince == nil || *api.DaysSince != 40 || api.Releases != 0 || len(api.Flags) != 1 ||
		api.Flags[0] != (RepoFlag{Key: StateNoRelease, Tone: DotBreach, Value: 40}) {
		t.Errorf("kasse-api = %+v", api)
	}
	ui := rowOf(t, v, "kasse/kasse-ui")
	// The newer of the two deploys is its last release.
	if ui.Releases != 1 || ui.Flags[0].Key != StateReleaseDue || *ui.DaysSince != 19 {
		t.Errorf("kasse-ui = %+v (one revision on two Apps is one release)", ui)
	}
	lager := rowOf(t, v, "lager/lager-api")
	if lager.Releases != 4 || lager.ToProd == nil || *lager.ToProd != 16 || lager.ToProdN != 6 ||
		!slices.ContainsFunc(lager.Flags, func(f RepoFlag) bool { return f.Key == StateSlowToProd && f.Tone == DotBreach }) {
		t.Errorf("lager-api = %+v", lager)
	}
	if n := len(lager.ReleaseWeeks); n != 8 || *lager.ReleaseWeeks[7] != 1 {
		t.Errorf("lager-api release weeks = %v", lager.ReleaseWeeks)
	}
	if w := rowOf(t, v, "portal/portal-web"); w.Reason == nil || *w.Reason != RepoReasonUntraced || w.DaysSince != nil {
		t.Errorf("portal-web = %+v", w)
	}
	if l := rowOf(t, v, "lager/lager-lib"); l.DaysSince != nil {
		t.Errorf("a library has no release to be late with: %+v", l)
	}
	if v.Open != len(v.Rows) || v.HintDays != 14 || v.BreachDays != 28 {
		t.Errorf("open %d of %d, thresholds %d/%d", v.Open, len(v.Rows), v.HintDays, v.BreachDays)
	}
	if len(v.Groups) != 6 || v.Groups[0].Key != RepoGroupNone || len(v.Groups[0].Hidden) != 0 {
		t.Errorf("groups = %+v", v.Groups)
	}

	rel := spreadOf(t, v, SpreadReleases, nil)
	// kasse-api released nothing in the period: set apart, last.
	last := rel.Dots[len(rel.Dots)-1]
	if !rel.Shown || rel.Apart != 1 || !last.Apart || last.Repo != "kasse/kasse-api" || last.X != nil ||
		last.Verdict != DotBreach {
		t.Errorf("releases strip = %+v", rel)
	}
	prod := spreadOf(t, v, SpreadToProd, nil)
	if prod.Mark == nil || prod.Mark.Value != 14 || prod.Dots[0].Verdict != DotBreach || prod.Dots[0].Repo != "lager/lager-api" {
		t.Errorf("to_prod strip = %+v", prod)
	}
}

// An App's first deploy soon after Argo CD's first event is no known
// release, as the team's frequency counts it: no release of the repo's,
// though the last one was then or before.
func TestRepoViewFirstDeployInGrace(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Meta.DataStart.ArgoCD = endAgoP(10)
		d.Traces = map[string]dataset.RepoTrace{"kasse/kasse-api": {DeployedSince: endAgoP(5),
			Releases: []dataset.TraceRelease{traceRelease("kasse-prod", 5)}}}
		d.Releases = []dataset.AppRelease{release("kasse-prod", 5, true)}
		d.PRs = []dataset.PR{merged(1, 3)}
	})
	v := repoViewOf(ds, &dataset.Dataset{Meta: ds.Meta})
	r := rowOf(t, v, "kasse/kasse-api")
	if r.Releases != 0 || r.DaysSince == nil || *r.DaysSince != 5 || r.Group != RepoGroupRegular {
		t.Errorf("row = %+v", r)
	}
	// Rolling out regularly: placed at none in the period, never set apart
	// as one that released nothing.
	if s := spreadOf(t, v, SpreadReleases, nil); len(s.Dots) != 1 || s.Dots[0].Apart || s.Dots[0].Verdict != DotWithin {
		t.Errorf("releases strip = %+v", s)
	}
}

func TestRepoViewOpensEightOfMany(t *testing.T) {
	mk := func(n int) RepoView {
		ds := stateData(func(d *dataset.Dataset) {
			d.PRs = nil
			for i := range n {
				d.PRs = append(d.PRs, merged(int64(i+1), 3, onRepo("kasse/r"+string(rune('a'+i)))))
			}
		})
		return repoViewOf(ds, &dataset.Dataset{Meta: ds.Meta})
	}
	if v := mk(10); v.Open != 10 {
		t.Errorf("two behind the button: all shown, open %d", v.Open)
	}
	v := mk(12)
	if v.Open != RepoRankOpen || len(v.Groups) != 1 || len(v.Groups[0].Hidden) != 4 || v.Groups[0].Count != 12 {
		t.Errorf("open %d, groups %+v", v.Open, v.Groups)
	}
	// Past five hidden repos the summary line counts the rest.
	if v := mk(16); len(v.Groups[0].Hidden) != RepoHiddenNamed || v.Groups[0].More != 3 || v.Groups[0].HiddenCount != 8 {
		t.Errorf("groups %+v", v.Groups)
	}
}

// The strips show what the states judge: the same value for the same repo.
func TestRepoViewMatchesStates(t *testing.T) {
	ds := repoData()
	ds.Releases = []dataset.AppRelease{release("kasse-prod", 40, false), release("lager-prod", 3, false)}
	// Five pickups of three working days in kasse-ui.
	for i := range 5 {
		ready := endAgo(15 + float64(i))
		ds.PRs = append(ds.PRs, merged(int64(40+i), 5, onRepo("kasse/kasse-ui"), func(p *dataset.PR) {
			p.ReadyAt, p.FirstReviewAt = ptr(ready), ptr(afterWorkingDays(ready, 3, zurich))
		}))
	}
	// Feature builds of 20 minutes in lager-api.
	for i := range 6 {
		ds.Runs = append(ds.Runs, run(func(r *dataset.Run) {
			r.PipelineName, r.Branch, r.BranchKind, r.BuildType = "lager-api/feature%2Fx", "feature/x", dataset.BranchFeature, nil
			r.Repo, r.Project, r.RunID, r.DurationS = s("lager/lager-api"), s("lager"), string(rune('a'+i)), n(1200)
			r.StartedAt, r.FinishedAt, r.OccurredAt = endAgoP(3), endAgoP(2.9), endAgo(2.9)
		}))
	}
	prev := repoPrev(ds)
	v := repoViewOf(ds, prev)
	a := Assess(ds, prev, Params{Period: stCur, Previous: stPrev}, History{})
	unit := func(key, name string) StateUnit {
		t.Helper()
		sig := stateSignal(a, key)
		if sig == nil {
			t.Fatalf("no %s card", key)
		}
		for _, u := range sig.State.Units {
			if u.Name == name {
				return u
			}
		}
		t.Fatalf("%s names no %s", key, name)
		return StateUnit{}
	}
	if u, r := unit(StateSlowToProd, "lager/lager-api"), rowOf(t, v, "lager/lager-api"); *r.ToProd != u.Value {
		t.Errorf("to prod: row %v, state %v", *r.ToProd, u.Value)
	}
	if u, r := unit(StateFirstReviewWait, "kasse/kasse-ui"), rowOf(t, v, "kasse/kasse-ui"); r.Pickup == nil || *r.Pickup != u.Value {
		t.Errorf("pickup: row %v, state %v", show(r.Pickup), u.Value)
	}
	if u, r := unit(StateNoRelease, "kasse"), rowOf(t, v, "kasse/kasse-api"); *r.DaysSince != u.Value {
		t.Errorf("days since: row %v, state %v", *r.DaysSince, u.Value)
	}
	dur := spreadOf(t, v, SpreadBuildDuration, ptr(KindFeature))
	i := slices.IndexFunc(dur.Dots, func(d RepoDot) bool { return d.Repo == "lager/lager-api" })
	if u := unit(StateLongBuilds, "lager/lager-api"); i < 0 || round1(*dur.Dots[i].Value/60) != u.Value ||
		dur.Dots[i].Verdict != DotHint || dur.Mark == nil || dur.Mark.Value != 900 {
		t.Errorf("build duration strip %+v, state %+v", dur, u)
	}
	pick := spreadOf(t, v, SpreadPickup, nil)
	if pick.Thin == 0 || pick.Dots[len(pick.Dots)-1].Verdict != DotHint ||
		pick.Dots[len(pick.Dots)-1].Repo != "kasse/kasse-ui" {
		t.Errorf("pickup strip = %+v (kasse-api's one PR is too few)", pick)
	}
}

func TestStripScale(t *testing.T) {
	// One value: centred.
	sc := stripScale([]float64{4}, nil, UnitPerWeek, true)
	if x, _ := sc.position(4, true); sc.Log || x != 0.5 {
		t.Errorf("one value at %v on %+v", x, sc)
	}
	// Spread over a factor of 40: log, the larger left when higher is better.
	sc = stripScale([]float64{0.25, 1, 10}, nil, UnitPerWeek, true)
	big, _ := sc.position(10, true)
	small, _ := sc.position(0.25, true)
	if !sc.Log || big >= small || len(sc.Ticks) == 0 || len(sc.Ticks) > maxTicks {
		t.Errorf("log scale %+v: 10 at %v, 0.25 at %v", sc, big, small)
	}
	// A zero on a log scale sits at the end, clamped.
	if x, clamped := sc.position(0, false); x != 0 || !clamped {
		t.Errorf("zero at %v clamped %v", x, clamped)
	}
	// A narrow spread stays linear and takes in the threshold.
	sc = stripScale([]float64{6, 9}, ptr(14.0), UnitDays, false)
	if sc.Log || sc.Hi < 14 {
		t.Errorf("linear with mark: %+v", sc)
	}
	for _, tk := range sc.Ticks {
		if tk.Value < sc.Lo || tk.Value > sc.Hi || math.Signbit(tk.Value) {
			t.Errorf("tick %v outside %v..%v", tk.Value, sc.Lo, sc.Hi)
		}
	}
	// A share runs to 100 %.
	sc = stripScale([]float64{0.72, 1}, nil, UnitShare, true)
	if sc.Lo != 0.7 || sc.Hi != 1 {
		t.Errorf("share scale %+v", sc)
	}
	// Nothing but zeros: a scale all the same, no -0 tick.
	sc = stripScale([]float64{0, 0}, nil, UnitWorkingDays, false)
	if len(sc.Ticks) == 0 || math.Signbit(sc.Ticks[0].Value) {
		t.Errorf("zeros %+v", sc)
	}
}

// Every placed repo at one value: no strip, the value for a line; one repo
// placed beside repos set apart: neither.
func TestSpreadOneValue(t *testing.T) {
	d := func(repo string, v *float64, apart bool) spreadDot {
		return spreadDot{repo: repo, value: v, n: 1, verdict: DotNeutral, apart: apart}
	}
	s := newSpread(SpreadPickup, nil, UnitWorkingDays, false,
		[]spreadDot{d("a", ptr(0.0), false), d("b", ptr(0.0), false), d("c", ptr(0.0), false)}, nil, nil, false)
	if s.Shown || s.Same == nil || *s.Same != 0 || s.Judged != 3 {
		t.Errorf("one value = %+v", s)
	}
	s = newSpread(SpreadReleases, nil, UnitPerWeek, true,
		[]spreadDot{d("a", ptr(0.5), false), d("b", nil, true), d("c", nil, true)}, nil, nil, true)
	if s.Shown || s.Same != nil || s.Apart != 2 {
		t.Errorf("one placed = %+v", s)
	}
}

func TestSpreadNote(t *testing.T) {
	d := func(v float64, n int) spreadDot {
		return spreadDot{repo: "r", value: ptr(v), n: n, verdict: DotNeutral}
	}
	// One repo carries 20 of 24 releases.
	note := spreadNote([]spreadDot{d(5, 20), d(0.5, 2), d(0.5, 2)}, ptr(0.5), ptr(6.0), true)
	if note == nil || note.Kind != NoteDominant || !near(*note.Share, 20.0/24) {
		t.Errorf("dominant = %+v", note)
	}
	note = spreadNote([]spreadDot{d(1, 1), d(1.2, 1), d(5, 1), d(0.2, 1)}, ptr(1.1), nil, false)
	if note == nil || note.Kind != NoteFar || note.Near != 2 || note.Above != 1 || note.Below != 1 || note.Of != 4 {
		t.Errorf("far = %+v", note)
	}
	// Close together, but the team adds them up to far more.
	note = spreadNote([]spreadDot{d(1, 4), d(1, 4), d(1, 4)}, ptr(1.0), ptr(3.0), true)
	if note == nil || note.Kind != NotePooled || !note.Additive {
		t.Errorf("pooled = %+v", note)
	}
	if note := spreadNote([]spreadDot{d(1, 1), d(9, 1)}, ptr(5.0), ptr(5.0), false); note != nil {
		t.Errorf("two repos say nothing: %+v", note)
	}
	if note := spreadNote([]spreadDot{d(1, 1), d(1.1, 1), d(1.2, 1)}, ptr(1.1), ptr(1.0), false); note != nil {
		t.Errorf("nothing to say: %+v", note)
	}
}

func TestTrends(t *testing.T) {
	// Bitbucket from Tuesday of the previous period's second week; a lost
	// silence over the period's second week's working days, another on a
	// Sunday only.
	bitbucket := stPrev.Weeks[1].Start.Add(30 * time.Hour)
	ds := stateData(func(d *dataset.Dataset) {
		d.Meta.DataStart.Bitbucket = &bitbucket
		d.Meta.DataStart.Noergler = &bitbucket
		d.Meta.Lost = []dataset.Silence{
			{Source: dataset.SourceBitbucket, From: stCur.Weeks[1].Start.Add(26 * time.Hour),
				To: stCur.Weeks[1].Start.Add(80 * time.Hour), Lost: true},
			{Source: dataset.SourceBitbucket, From: stCur.Weeks[2].End.Add(-10 * time.Hour),
				To: stCur.Weeks[2].End.Add(-time.Hour), Lost: true},
		}
		d.PRs = []dataset.PR{merged(1, 2, func(p *dataset.PR) { p.LinesAdded, p.LinesRemoved = n(30), n(10) }),
			merged(2, 3)}
	})
	running := RunningPeriod(stEnd.Add(50*time.Hour), zurich)
	run := &dataset.Dataset{Meta: ds.Meta, PRs: []dataset.PR{pr(func(p *dataset.PR) {
		p.MergedAt = ptr(stEnd.Add(30 * time.Hour))
		p.LinesAdded, p.LinesRemoved = n(70), n(30)
	})}}
	m := Compute(ds, &dataset.Dataset{Meta: ds.Meta}, run, Params{Period: stCur, Previous: stPrev, Running: running})
	tr := m.Trends
	if len(tr.Weeks) != 9 || tr.Split != 4 || !tr.Weeks[8].Running || tr.Weeks[7].Running {
		t.Fatalf("weeks = %+v split %d", tr.Weeks, tr.Split)
	}
	want := []string{GapNoData, GapNoData, "", "", "", GapLost, "", ""}
	if !slices.Equal(tr.Merged.Gaps[:8], want) {
		t.Errorf("merged gaps = %q, want %q", tr.Merged.Gaps, want)
	}
	if tr.Merged.Values[0] != nil || tr.Merged.Values[5] != nil || *tr.Merged.Values[7] != 2 || *tr.Merged.Values[8] != 1 {
		t.Errorf("merged = %v", tr.Merged.Values)
	}
	// The last closed week has one sized PR; the weeks before none to size.
	if *tr.Batch.Values[7] != 40 || tr.Batch.N[7] != 1 || tr.Batch.Gaps[6] != GapNone || *tr.Batch.Values[8] != 100 {
		t.Errorf("batch = %v gaps %q", tr.Batch.Values, tr.Batch.Gaps)
	}
	if tr.Releases.Gaps[0] != "" || *tr.Releases.Values[0] != 0 {
		t.Errorf("releases = %v gaps %q", tr.Releases.Values, tr.Releases.Gaps)
	}
	// Bitbucket saw the previous period in part: no previous value per repo,
	// as the comparison holds; Argo CD saw it whole.
	if r := m.Repos.Rows[0]; r.MergedPrev != nil || r.BatchPrev != nil || r.MergedPerWeek == nil || r.ReleasesPrev == nil {
		t.Errorf("row = %+v", r)
	}
}
