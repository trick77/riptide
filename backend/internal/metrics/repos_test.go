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

// repoData is a team of eight repos over six projects, every group:
// post-api's project last released 40 days back while worked on (none),
// bau-web's 20 days back (rare); kasse released 10 days back, shipping
// kasse-ui and leaving kasse-api unchanged, whose PR merged 20 days back
// still waits (regular, not on production); lager-api ships weekly but slow
// to production, lager-lib is a library, portal-web is no App's known,
// tools-cli nobody works on. The releases are the ones the traces list.
func repoData() *dataset.Dataset {
	return stateData(func(d *dataset.Dataset) {
		d.Apps = append(d.Apps, dataset.App{Name: "kasse-canary", Project: s("kasse")},
			dataset.App{Name: "lager-prod", Project: s("lager")}, dataset.App{Name: "tools-prod", Project: s("tools")},
			dataset.App{Name: "post-prod", Project: s("post")}, dataset.App{Name: "bau-prod", Project: s("bau")})
		lager := []dataset.TraceRelease{}
		for w := range 8 {
			lager = append(lager, traceRelease("lager-prod", 3+7*float64(w)))
			d.Releases = append(d.Releases, release("lager-prod", 3+7*float64(w), false))
		}
		d.Releases = append(d.Releases, release("kasse-prod", 40, false), release("kasse-prod", 20, false),
			release("kasse-canary", 19.9, false), release("kasse-prod", 10, false), release("kasse-prod", -1, false),
			release("tools-prod", 50, false), release("post-prod", 40, false), release("bau-prod", 20, false))
		d.Traces = map[string]dataset.RepoTrace{
			"kasse/kasse-api": {DeployedSince: endAgoP(150), Releases: []dataset.TraceRelease{traceRelease("kasse-prod", 40),
				// After the period's end: not one of it.
				traceRelease("kasse-prod", -1),
				// Shipped nothing of the repo.
				{App: "kasse-prod", Environment: "prod", At: endAgo(10), Known: endAgo(300), Unchanged: true}}},
			// One revision on two Apps: one release; another one 10 days back.
			"kasse/kasse-ui": {DeployedSince: endAgoP(150), Releases: []dataset.TraceRelease{
				traceRelease("kasse-prod", 20), traceRelease("kasse-canary", 19.9), traceRelease("kasse-prod", 10)}},
			"lager/lager-api": {DeployedSince: endAgoP(150), Releases: lager},
			"tools/tools-cli": {DeployedSince: endAgoP(150), Releases: []dataset.TraceRelease{traceRelease("tools-prod", 50)}},
			"post/post-api":   {DeployedSince: endAgoP(150), Releases: []dataset.TraceRelease{traceRelease("post-prod", 40)}},
			"bau/bau-web":     {DeployedSince: endAgoP(150), Releases: []dataset.TraceRelease{traceRelease("bau-prod", 20)}},
		}
		d.Deploys = []dataset.Deploy{
			deploy(func(x *dataset.Deploy) { x.App, x.Revision, x.At = "kasse-prod", "r-ui", endAgo(20) }),
			deploy(func(x *dataset.Deploy) { x.App, x.Revision, x.At = "kasse-canary", "r-ui", endAgo(19.9) }),
			deploy(func(x *dataset.Deploy) { x.App, x.Revision, x.At = "kasse-prod", "r-ui2", endAgo(10) }),
		}
		d.PRs = []dataset.PR{
			merged(1, 20),
			merged(2, 6, onRepo("kasse/kasse-ui")),
			merged(10, 4, onRepo("lager/lager-lib"), func(p *dataset.PR) { p.Library = true }),
			merged(11, 4, onRepo("portal/portal-web")),
			merged(12, 5, onRepo("post/post-api")),
			merged(13, 5, onRepo("bau/bau-web")),
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
	want := []string{"post/post-api:none", "bau/bau-web:rare", "portal/portal-web:unjudged",
		"lager/lager-api:regular", "kasse/kasse-api:regular", "kasse/kasse-ui:regular", "lager/lager-lib:library",
		"tools/tools-cli:idle"}
	if !slices.Equal(order, want) {
		t.Fatalf("rows = %v, want %v", order, want)
	}
	post := rowOf(t, v, "post/post-api")
	if post.DaysSince == nil || *post.DaysSince != 40 || post.Releases != 0 || len(post.Flags) != 1 ||
		post.Flags[0] != (RepoFlag{Key: StateNoRelease, Tone: DotBreach, Value: 40}) ||
		*post.ReleasesForm != ReleasesFormNone {
		t.Errorf("post-api = %+v", post)
	}
	if bau := rowOf(t, v, "bau/bau-web"); bau.Flags[0].Key != StateReleaseDue || *bau.DaysSince != 20 {
		t.Errorf("bau-web = %+v", bau)
	}
	// Its project released 10 days back, leaving it unchanged: no missed
	// release, its merged work waits on production. Its own release 40 days
	// back is the previous period's.
	api := rowOf(t, v, "kasse/kasse-api")
	if api.DaysSince == nil || *api.DaysSince != 10 || api.Releases != 0 || api.ReleasesPrev == nil ||
		len(api.Flags) != 1 || api.Flags[0] != (RepoFlag{Key: StateNotOnProd, Tone: DotBreach, Value: 20}) {
		t.Errorf("kasse-api = %+v", api)
	}
	ui := rowOf(t, v, "kasse/kasse-ui")
	// One revision on two Apps is one release, in one week.
	if ui.Releases != 2 || len(ui.Flags) != 0 || *ui.DaysSince != 10 {
		t.Errorf("kasse-ui = %+v", ui)
	}
	weekly := 0
	for _, w := range ui.ReleaseWeeks {
		weekly += *w
	}
	if weekly != 2 {
		t.Errorf("kasse-ui release weeks = %v, add up to %d", ui.ReleaseWeeks, weekly)
	}
	lager := rowOf(t, v, "lager/lager-api")
	if lager.Releases != 4 || lager.ToProd == nil || *lager.ToProd != 16 || lager.ToProdN != 6 ||
		!slices.ContainsFunc(lager.Flags, func(f RepoFlag) bool { return f.Key == StateSlowToProd && f.Tone == DotBreach }) ||
		lager.ReleasesForm == nil || lager.ReleasesBase != 4 {
		t.Errorf("lager-api = %+v", lager)
	}
	if n := len(lager.ReleaseWeeks); n != 8 || *lager.ReleaseWeeks[7] != 1 {
		t.Errorf("lager-api release weeks = %v", lager.ReleaseWeeks)
	}
	if w := rowOf(t, v, "portal/portal-web"); w.Reason == nil || *w.Reason != RepoReasonUntraced || w.DaysSince != nil ||
		w.ReleasesForm != nil {
		t.Errorf("portal-web = %+v", w)
	}
	if l := rowOf(t, v, "lager/lager-lib"); l.DaysSince != nil || l.ReleasesForm != nil {
		t.Errorf("a library has no release to be late with: %+v", l)
	}
	if v.Open != len(v.Rows) || v.HintDays != 14 || v.BreachDays != 28 {
		t.Errorf("open %d of %d, thresholds %d/%d", v.Open, len(v.Rows), v.HintDays, v.BreachDays)
	}
	if len(v.Groups) != 6 || v.Groups[0].Key != RepoGroupNone || len(v.Groups[0].Hidden) != 0 {
		t.Errorf("groups = %+v", v.Groups)
	}

	rel := spreadOf(t, v, SpreadReleases, nil)
	// post-api released nothing in the period: set apart, last.
	last := rel.Dots[len(rel.Dots)-1]
	if !rel.Shown || rel.Apart != 1 || !last.Apart || last.Repo != "post/post-api" || last.X != nil ||
		last.Verdict != DotBreach {
		t.Errorf("releases strip = %+v", rel)
	}
	prod := spreadOf(t, v, SpreadToProd, nil)
	if prod.Mark == nil || prod.Mark.Value != 14 || prod.Dots[0].Verdict != DotBreach || prod.Dots[0].Repo != "lager/lager-api" {
		t.Errorf("to_prod strip = %+v", prod)
	}
}

// A release of the period's rolled out first to one App and later to
// another counts once, in the week of its first deploy, as the team's
// frequency counts it.
func TestRepoViewRevisionOnce(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Apps = append(d.Apps, dataset.App{Name: "kasse-canary", Project: s("kasse")})
		d.Traces = map[string]dataset.RepoTrace{"kasse/kasse-api": {DeployedSince: endAgoP(150),
			Releases: []dataset.TraceRelease{traceRelease("kasse-canary", 9), traceRelease("kasse-prod", 2)}}}
		d.Releases = []dataset.AppRelease{release("kasse-canary", 9, false), release("kasse-prod", 2, false)}
		d.Deploys = []dataset.Deploy{
			deploy(func(x *dataset.Deploy) { x.App, x.Revision, x.At = "kasse-canary", "r1", endAgo(9) }),
			deploy(func(x *dataset.Deploy) { x.App, x.Revision, x.At = "kasse-prod", "r1", endAgo(2) }),
		}
		d.PRs = []dataset.PR{merged(1, 12)}
	})
	m := Compute(ds, &dataset.Dataset{Meta: ds.Meta}, nil, Params{Period: stCur, Previous: stPrev})
	r := rowOf(t, m.Repos, "kasse/kasse-api")
	weekly := 0
	for _, w := range r.ReleaseWeeks {
		weekly += *w
	}
	if r.Releases != 1 || weekly != 1 || m.Delivery.Frequency.Releases != 1 {
		t.Errorf("releases %d, weekly %v, team %d", r.Releases, r.ReleaseWeeks, m.Delivery.Frequency.Releases)
	}
	// One release over the period's four weeks: written as a count.
	if *r.ReleasesForm != ReleasesFormInWeeks || r.ReleasesBase != 4 {
		t.Errorf("form %v over %d weeks", str(r.ReleasesForm), r.ReleasesBase)
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
	// At least 5 days, as the release state reads it.
	if r.Releases != 0 || r.DaysSince == nil || *r.DaysSince != 5 || r.Group != RepoGroupRegular || !r.SinceLowerBound {
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

// cardNames are the units a card of key names, sorted; none without the
// card.
func cardNames(a Assessment, key string) []string {
	out := []string{}
	if sig := stateSignal(a, key); sig != nil {
		for _, u := range sig.State.Units {
			out = append(out, u.Name)
		}
	}
	slices.Sort(out)
	return out
}

// rowsFlagged are the rows' units flagged key, sorted: a repo, or its
// project for a per-project state.
func rowsFlagged(v RepoView, key string, project bool) []string {
	out := []string{}
	for _, r := range v.Rows {
		if !slices.ContainsFunc(r.Flags, func(f RepoFlag) bool { return f.Key == key }) {
			continue
		}
		name := r.Repo
		if project {
			name = r.Project
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// The ranking never contradicts the assessment: a row is flagged for a
// state exactly where the state's card names it, or its project.
func expectFlagsMatch(t *testing.T, v RepoView, a Assessment) {
	t.Helper()
	for _, c := range []struct {
		key     string
		project bool
	}{{StateNoRelease, true}, {StateReleaseDue, true}, {StateNotOnProd, false}, {StateSlowToProd, false}} {
		if got, want := rowsFlagged(v, c.key, c.project), cardNames(a, c.key); !slices.Equal(got, want) {
			t.Errorf("%s: rows flag %v, the card names %v", c.key, got, want)
		}
	}
}

// The strips show what the states judge: the same value for the same repo.
func TestRepoViewMatchesStates(t *testing.T) {
	ds := repoData()
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
	if u, r := unit(StateNoRelease, "post"), rowOf(t, v, "post/post-api"); *r.DaysSince != u.Value ||
		r.SinceLowerBound != u.LowerBound {
		t.Errorf("days since: row %v, state %v", *r.DaysSince, u.Value)
	}
	if u, r := unit(StateNotOnProd, "kasse/kasse-api"), rowOf(t, v, "kasse/kasse-api"); r.Flags[0].Value != u.Value {
		t.Errorf("not on prod: row %+v, state %v", r.Flags, u.Value)
	}
	expectFlagsMatch(t, v, a)
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

// The assessment's gates hold for the rows: no release judged where Argo CD
// never reports the production stage or went silent, nobody's work where
// Bitbucket did; the card raises nothing there, so no row does.
func TestRepoViewGates(t *testing.T) {
	worked := func(mod func(*dataset.Dataset)) *dataset.Dataset {
		return stateData(func(d *dataset.Dataset) {
			d.Traces = map[string]dataset.RepoTrace{"kasse/kasse-api": {DeployedSince: endAgoP(150),
				Releases: []dataset.TraceRelease{traceRelease("kasse-prod", 30)}}}
			d.Releases = []dataset.AppRelease{release("kasse-prod", 30, false)}
			d.PRs = []dataset.PR{merged(1, 25)}
			for i := range 5 {
				ready := endAgo(15 + float64(i))
				d.PRs = append(d.PRs, merged(int64(10+i), 12, func(p *dataset.PR) {
					p.ReadyAt, p.FirstReviewAt = ptr(ready), ptr(afterWorkingDays(ready, 3, zurich))
				}))
			}
			mod(d)
		})
	}
	for _, c := range []struct {
		name, reason string
		mod          func(*dataset.Dataset)
	}{
		{"argo only reports intg", RepoReasonNoProdStage, func(d *dataset.Dataset) {
			d.Releases = []dataset.AppRelease{{App: "kasse-prod", Environment: "intg", At: endAgo(30)}}
			d.Traces["kasse/kasse-api"] = dataset.RepoTrace{DeployedSince: endAgoP(150)}
		}},
		{"argo silent", RepoReasonArgoSilent, func(d *dataset.Dataset) { d.Meta.DataEnd.ArgoCD = endAgoP(20) }},
		{"bitbucket silent", RepoReasonBitbucketSilent, func(d *dataset.Dataset) { d.Meta.DataEnd.Bitbucket = endAgoP(10) }},
	} {
		ds := worked(c.mod)
		v := repoViewOf(ds, &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps})
		a := assessStates(t, ds, nil, History{})
		r := rowOf(t, v, "kasse/kasse-api")
		if r.Group != RepoGroupUnjudged || r.Reason == nil || *r.Reason != c.reason || r.DaysSince != nil {
			t.Errorf("%s: row = %+v", c.name, r)
		}
		// A silent source flags nothing and judges no dot; the others still
		// do, as their cards.
		bitbucketKey := func(f RepoFlag) bool { return f.Key == StateFirstReviewWait }
		argoKey := func(f RepoFlag) bool { return !bitbucketKey(f) }
		if (c.reason == RepoReasonArgoSilent && slices.ContainsFunc(r.Flags, argoKey)) ||
			(c.reason == RepoReasonBitbucketSilent && slices.ContainsFunc(r.Flags, bitbucketKey)) {
			t.Errorf("%s: flags = %+v", c.name, r.Flags)
		}
		if c.reason != RepoReasonBitbucketSilent && r.ReleasesForm != nil {
			t.Errorf("%s: releases written %v", c.name, *r.ReleasesForm)
		}
		if p := spreadOf(t, v, SpreadPickup, nil); c.reason == RepoReasonBitbucketSilent &&
			(len(p.Dots) != 1 || p.Dots[0].Verdict != DotNeutral) {
			t.Errorf("%s: pickup strip %+v", c.name, p)
		}
		expectFlagsMatch(t, v, a)
	}
}

// A project none of whose Apps ever released to production is no project
// rolled out: the release rule does not judge it, its waiting work is
// StateNotOnProd's.
func TestRepoViewNeverReleased(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Apps = append(d.Apps, dataset.App{Name: "lager-prod", Project: s("lager")})
		d.Releases = []dataset.AppRelease{{App: "kasse-prod", Environment: "intg", At: endAgo(30)},
			release("lager-prod", 3, false)}
		d.PRs = []dataset.PR{merged(1, 25)}
	})
	v := repoViewOf(ds, &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps})
	r := rowOf(t, v, "kasse/kasse-api")
	if r.Group != RepoGroupUnjudged || *r.Reason != RepoReasonNeverReleased || r.DaysSince != nil ||
		len(r.Flags) != 1 || r.Flags[0].Key != StateNotOnProd || *r.ReleasesForm != ReleasesFormNone {
		t.Errorf("row = %+v", r)
	}
	expectFlagsMatch(t, v, assessStates(t, ds, nil, History{}))
}

// A repo of a project past the breach whose merges all lie in the previous
// period merged nothing now: no dot set apart on the wait to production.
func TestRepoViewNoneWithoutMerges(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.Releases = []dataset.AppRelease{release("kasse-prod", 40, false)}
	})
	prev := &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps, PRs: []dataset.PR{merged(1, 35)}}
	v := repoViewOf(ds, prev)
	if r := rowOf(t, v, "kasse/kasse-api"); r.Group != RepoGroupNone || r.Merged != 0 {
		t.Fatalf("row = %+v", r)
	}
	if s := spreadOf(t, v, SpreadToProd, nil); len(s.Dots) != 0 || s.Apart != 0 {
		t.Errorf("to_prod strip = %+v", s)
	}
}

// Bitbucket's lost events in either period make its counts read low there:
// no repo's previous value, as the team tile has none.
func TestRepoViewPrevLost(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.PRs = []dataset.PR{merged(1, 5), merged(2, 6), merged(3, 7)}
	})
	prev := &dataset.Dataset{Meta: ds.Meta, Apps: ds.Apps, PRs: []dataset.PR{merged(4, 35), merged(5, 36), merged(6, 37)}}
	for i := range prev.PRs {
		prev.PRs[i].LinesAdded, prev.PRs[i].LinesRemoved = n(10), n(5)
		ds.PRs[i].LinesAdded, ds.PRs[i].LinesRemoved = n(10), n(5)
	}
	if r := rowOf(t, repoViewOf(ds, prev), "kasse/kasse-api"); r.MergedPrev == nil || r.BatchPrev == nil {
		t.Fatalf("whole: %+v", r)
	}
	prev.Meta.Silences = []dataset.Silence{{Source: dataset.SourceBitbucket, From: endAgo(40), To: endAgo(38), Lost: true}}
	m := Compute(ds, prev, nil, Params{Period: stCur, Previous: stPrev})
	r := rowOf(t, m.Repos, "kasse/kasse-api")
	if r.MergedPrev != nil || r.BatchPrev != nil || m.Comparison.MergedPerWeek.Prev != nil {
		t.Errorf("lost: row %+v, team %+v", r, m.Comparison.MergedPerWeek)
	}
	// The page says why, with the span.
	if c := m.Comparison.MergedPerWeek; *c.Reason != ReasonLost || c.From == nil || !c.From.Equal(endAgo(40)) ||
		c.To == nil || !c.To.Equal(endAgo(38)) {
		t.Errorf("reason = %+v", c)
	}
}

// Weeks after a source fell silent for good are gaps, never zeros: in the
// trends and in a row's weeks alike.
func TestTrendsSilentSource(t *testing.T) {
	ds := repoData()
	ds.Meta.DataEnd.ArgoCD = endAgoP(20)
	m := Compute(ds, repoPrev(ds), nil, Params{Period: stCur, Previous: stPrev})
	g := m.Trends.Releases.Gaps
	if len(g) != 8 || g[7] != GapSilent || g[6] != GapSilent || g[4] != "" || m.Trends.Releases.Values[7] != nil {
		t.Errorf("release gaps = %q", g)
	}
	if r := rowOf(t, m.Repos, "lager/lager-api"); r.ReleaseWeeks[7] != nil || r.ReleaseWeeks[4] == nil {
		t.Errorf("lager-api weeks = %v", r.ReleaseWeeks)
	}
	if g := m.Trends.Merged.Gaps; g[7] != "" {
		t.Errorf("merged gaps = %q (Bitbucket reports)", g)
	}
}

// A build judged on its minutes rounded is drawn there: 902 s is 15.0
// minutes, on the 15-minute mark, not past it.
func TestRepoViewBuildMinutes(t *testing.T) {
	ds := stateData(func(d *dataset.Dataset) {
		d.PRs = []dataset.PR{merged(1, 5)}
		for i := range 6 {
			d.Runs = append(d.Runs, run(func(r *dataset.Run) {
				r.PipelineName, r.Branch, r.BranchKind, r.BuildType = "kasse-api/feature%2Fx", "feature/x", dataset.BranchFeature, nil
				r.RunID, r.DurationS = string(rune('a'+i)), n(902)
				r.StartedAt, r.FinishedAt, r.OccurredAt = endAgoP(3), endAgoP(2.9), endAgo(2.9)
			}))
		}
	})
	s := spreadOf(t, repoViewOf(ds, &dataset.Dataset{Meta: ds.Meta}), SpreadBuildDuration, ptr(KindFeature))
	if len(s.Dots) != 1 || *s.Dots[0].Value != 900 || s.Dots[0].Verdict != DotWithin || *s.Dots[0].X != s.Mark.X {
		t.Errorf("strip = %+v", s)
	}
}

func TestStripScale(t *testing.T) {
	// Close values stay close: 902 s against a 900 s mark, 3.0 against 3.1.
	sc := stripScale([]float64{902}, ptr(900.0), UnitSeconds, false)
	a, _ := sc.position(902, false)
	m, _ := sc.position(900, false)
	if math.Abs(a-m) > 0.05 || m <= 0 || m >= 1 {
		t.Errorf("902 at %v, mark at %v on %+v", a, m, sc)
	}
	sc = stripScale([]float64{3.0, 3.1}, nil, UnitPerWeek, true)
	a, _ = sc.position(3.0, true)
	b, _ := sc.position(3.1, true)
	if math.Abs(a-b) > 0.1 {
		t.Errorf("3.0 at %v, 3.1 at %v", a, b)
	}
	// One value: centred.
	sc = stripScale([]float64{4}, nil, UnitPerWeek, true)
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
		[]spreadDot{d("a", ptr(0.0), false), d("b", ptr(0.0), false), d("c", ptr(0.0), false)}, nil, nil, 0)
	if s.Shown || s.Same == nil || *s.Same != 0 || s.Judged != 3 {
		t.Errorf("one value = %+v", s)
	}
	s = newSpread(SpreadReleases, nil, UnitPerWeek, true,
		[]spreadDot{d("a", ptr(0.5), false), d("b", nil, true), d("c", nil, true)}, nil, nil, 1)
	if s.Shown || s.Same != nil || s.Apart != 2 {
		t.Errorf("one placed = %+v", s)
	}
}

func TestSpreadNote(t *testing.T) {
	d := func(v float64, n int) spreadDot {
		return spreadDot{repo: "r", value: ptr(v), n: n, verdict: DotNeutral}
	}
	// One repo carries 20 of the team's 24 releases.
	note := spreadNote([]spreadDot{d(5, 20), d(0.5, 2), d(0.5, 2)}, ptr(0.5), ptr(6.0), 24)
	if note == nil || note.Kind != NoteDominant || !near(*note.Share, 20.0/24) {
		t.Errorf("dominant = %+v", note)
	}
	// Of the team's own count, not the strip's sum: 20 of 50 dominates
	// nothing, though the three repos on the strip add up to 24.
	if note := spreadNote([]spreadDot{d(5, 20), d(0.5, 2), d(0.5, 2)}, ptr(0.5), ptr(6.0), 50); note == nil ||
		note.Kind != NoteFar {
		t.Errorf("of the team's 50 = %+v", note)
	}
	note = spreadNote([]spreadDot{d(1, 1), d(1.2, 1), d(5, 1), d(0.2, 1)}, ptr(1.1), nil, 0)
	if note == nil || note.Kind != NoteFar || note.Near != 2 || note.Above != 1 || note.Below != 1 || note.Of != 4 {
		t.Errorf("far = %+v", note)
	}
	// Close together, but the team adds them up to far more.
	note = spreadNote([]spreadDot{d(1, 4), d(1, 4), d(1, 4)}, ptr(1.0), ptr(3.0), 12)
	if note == nil || note.Kind != NotePooled || !note.Additive {
		t.Errorf("pooled = %+v", note)
	}
	if note := spreadNote([]spreadDot{d(1, 1), d(9, 1)}, ptr(5.0), ptr(5.0), 0); note != nil {
		t.Errorf("two repos say nothing: %+v", note)
	}
	if note := spreadNote([]spreadDot{d(1, 1), d(1.1, 1), d(1.2, 1)}, ptr(1.1), ptr(1.0), 0); note != nil {
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
