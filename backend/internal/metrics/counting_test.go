package metrics

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// An App's first deploy the dataset counts as a release (FirstRelease) is
// one everywhere a release counts: the total, its week, the calendar and
// the running week; within the grace it stays a first deploy, counted
// apart.
func TestFirstDeployCountsAsRelease(t *testing.T) {
	p := p4()
	first := func(rev string, days float64, counted bool) dataset.Deploy {
		return deploy(func(d *dataset.Deploy) {
			d.App, d.Revision, d.At, d.Kind, d.NewImages, d.FirstRelease = "kasse-shop-"+rev, rev, ago(days), s("first_seen"), []string{}, counted
		})
	}
	ds := data(func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "r1", ago(1) }),
			first("r2", 2, true),
			first("r3", 9, false),
			// One revision: a release for one App, a first release for
			// another, counts as a release, not as a first one.
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "r4", ago(3) }),
			first("r4", 3, true),
		}
	})
	d := calendarOf(ds, p)
	if d.Releases != 3 || d.FirstReleases != 1 || d.FirstSeen != 1 || d.Syncs != 4 || d.Resyncs != 0 {
		t.Errorf("releases/first releases/first seen/syncs/resyncs = %d/%d/%d/%d/%d",
			d.Releases, d.FirstReleases, d.FirstSeen, d.Syncs, d.Resyncs)
	}
	if w := d.Weeks[3]; w.Release != 3 || w.FirstRelease != 1 {
		t.Errorf("last week = %+v", w)
	}
	if w := d.Weeks[2]; w.FirstSeen != 1 || w.Release != 0 {
		t.Errorf("week before = %+v", w)
	}
	days := 0
	for _, day := range d.Days {
		days += day.Releases
	}
	if days != 3 {
		t.Errorf("calendar releases = %d", days)
	}

	// The running week counts an App going live.
	r := RunningPeriod(now, zurich)
	running := window(r.From, now, func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{first("r9", 0.5, true), first("r8", 0.4, false)}
	})
	if got := runningWeek(running, r, filter{}).Releases; got != 1 {
		t.Errorf("running week releases = %d, want 1", got)
	}

	// The facts row: a release, marked as the App's first deploy, found by
	// both words.
	f := ComputeFacts(ds, all(), "Erstdeploy")
	var rows []string
	for _, x := range f.Deploys.Rows {
		rows = append(rows, fmt.Sprintf("%s:%s:%v", x.Revision, x.Class, x.FirstDeploy))
	}
	if want := "[r2:release:true r4:release:true r3:first_seen:true]"; fmt.Sprint(rows) != want {
		t.Errorf("first deploys = %v, want %s", rows, want)
	}
}

// A sync Argo CD reports as Error failed, as one reported Failed.
func TestDeployErrorIsFailed(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "r1", ago(1) }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.At, d.Phase, d.Kind = "r2", ago(2), "Failed", nil }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.At, d.Phase, d.Kind = "r3", ago(3), "Error", nil }),
		}
	})
	d := deployFrequency(ds, p4(), filter{}, "prod")
	if d.Succeeded != 1 || d.Failed != 2 || d.Finished != 3 {
		t.Errorf("succeeded/failed/finished = %d/%d/%d", d.Succeeded, d.Failed, d.Finished)
	}
	expectNear(t, "success rate", d.SuccessRate, 1.0/3)
	var classes []string
	for _, x := range ComputeFacts(ds, all(), "").Deploys.Rows {
		classes = append(classes, x.Class+":"+x.Phase)
	}
	if fmt.Sprint(classes) != "[release:Succeeded failed:Failed failed:Error]" {
		t.Errorf("classes = %v", classes)
	}
}

// An infrastructure App's deploys are in no number of the frequency but its
// own count; a deploy without a product is in the team's numbers, in no
// product's, and counted team-wide for every view.
func TestDeploysWithoutProduct(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "r1", ago(1) }),
			// infrastructure: a release, a sync and a failure, none counted
			deploy(func(d *dataset.Deploy) {
				d.App, d.Project, d.Revision, d.At, d.Infra = "monitoring", nil, "i1", ago(2), true
			}),
			deploy(func(d *dataset.Deploy) {
				d.App, d.Project, d.Revision, d.At, d.Kind, d.Infra = "monitoring", nil, "i2", ago(3), s("sync"), true
			}),
			deploy(func(d *dataset.Deploy) {
				d.App, d.Project, d.Revision, d.At, d.Phase, d.Kind, d.Infra = "dep-bot", nil, "i3", ago(4), "Failed", nil, true
			}),
			// a release whose GitOps commit Bitbucket's events missed
			deploy(func(d *dataset.Deploy) { d.App, d.Project, d.Revision, d.At = "portal-prod", nil, "u1", ago(5) }),
			// one revision, an infrastructure App's and another's: the other's
			deploy(func(d *dataset.Deploy) {
				d.App, d.Project, d.Revision, d.At, d.Infra = "monitoring", nil, "u2", ago(6), true
			}),
			deploy(func(d *dataset.Deploy) { d.App, d.Project, d.Revision, d.At = "kasse-batch", nil, "u2", ago(6) }),
		}
	})
	p := p4()
	team := deployFrequency(ds, p, filter{}, "prod")
	if team.Releases != 3 || team.Syncs != 3 || team.Failed != 0 || team.Succeeded != 3 {
		t.Errorf("team releases/syncs/failed/succeeded = %d/%d/%d/%d", team.Releases, team.Syncs, team.Failed, team.Succeeded)
	}
	if team.Unattributed != 2 || team.Infra != 2 || !slices.Equal(team.InfraApps, []string{"monitoring"}) {
		t.Errorf("unattributed %d, infra %d %v", team.Unattributed, team.Infra, team.InfraApps)
	}
	kasse := deployFrequency(ds, p, filter{product: "kasse"}, "prod")
	if kasse.Releases != 1 || kasse.Unattributed != 2 || kasse.Infra != 2 {
		t.Errorf("kasse releases %d, unattributed %d, infra %d", kasse.Releases, kasse.Unattributed, kasse.Infra)
	}
	portal := deployFrequency(ds, p, filter{product: "portal"}, "prod")
	if portal.Releases != 0 || portal.Unattributed != 2 {
		t.Errorf("portal releases %d, unattributed %d", portal.Releases, portal.Unattributed)
	}
	if empty := deployFrequency(data(), p, filter{}, "prod"); empty.InfraApps == nil || empty.Unattributed != 0 {
		t.Errorf("empty = %+v", empty)
	}
	infra := map[string]bool{}
	for _, x := range ComputeFacts(ds, all(), "").Deploys.Rows {
		infra[x.Revision] = infra[x.Revision] || x.Infra
	}
	if !infra["i1"] || !infra["i3"] || infra["r1"] || infra["u1"] {
		t.Errorf("infra rows = %v", infra)
	}
}

// Merged work: a PR off the trunk, an implicit merge and a housekeeping PR
// are in no PR number, counted apart per view; the share of the merged PRs
// each number rests on is explicit; the weekly rate runs from Bitbucket's
// start.
func TestMergedWorkCounts(t *testing.T) {
	p := p4()
	mk := func(id int64, mod ...func(*dataset.PR)) dataset.PR {
		return pr(append([]func(*dataset.PR){func(x *dataset.PR) { x.PRID = id }}, mod...)...)
	}
	ds := data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{
			mk(1, func(x *dataset.PR) {
				x.LinesAdded, x.LinesRemoved = n(10), n(2)
				x.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: ago(1)}}
			}),
			mk(2, func(x *dataset.PR) { x.OpenedAt = nil }),
			mk(3, func(x *dataset.PR) { x.OffTrunk = true }),
			mk(4, func(x *dataset.PR) { x.ImplicitMerge, x.MergeCommit = true, nil }),
			mk(5, func(x *dataset.PR) { x.Housekeeping, x.Repo, x.Project = true, "portal/portal-web", "portal" }),
			// off the trunk and implicit: counted once, by the first reason
			mk(6, func(x *dataset.PR) { x.OffTrunk, x.ImplicitMerge = true, true }),
			// a bot's or a promotion is no person's merged work anyway
			mk(7, func(x *dataset.PR) { x.Housekeeping, x.Bot = true, true }),
		}
	})
	if got := len(mergedPRs(ds, p, filter{})); got != 2 {
		t.Errorf("merged = %d, want 2", got)
	}
	m := Compute(ds, noPrevious(ds), nil, all())
	th := m.Throughput
	if th.Merged != 2 || m.Review.Merged != 2 || th.OffTrunk != 2 || th.ImplicitMerges != 1 || th.Housekeeping != 1 {
		t.Errorf("merged %d (review %d), off trunk %d, implicit %d, housekeeping %d",
			th.Merged, m.Review.Merged, th.OffTrunk, th.ImplicitMerges, th.Housekeeping)
	}
	c := th.Coverage
	if c.ToProd != (Share{Count: 1, N: 2, Rate: c.ToProd.Rate}) || c.Sized.Count != 1 || c.Opened.Count != 1 || c.Opened.N != 2 {
		t.Errorf("coverage = %+v", c)
	}
	expectNear(t, "to prod rate", c.ToProd.Rate, 0.5)
	// The kasse view leaves the portal's housekeeping PR out of its counts.
	if k := Compute(ds, noPrevious(ds), nil, product("kasse")).Throughput; k.Housekeeping != 0 || k.OffTrunk != 2 {
		t.Errorf("kasse housekeeping %d, off trunk %d", k.Housekeeping, k.OffTrunk)
	}
	if th.Since != nil {
		t.Errorf("since = %v", th.Since)
	}
	expectNear(t, "merged per week", th.MergedPerWeek, 2/p.Span)
	// Bitbucket started a week before now: the rate is over that week.
	late := data(func(x *dataset.Dataset) { x.PRs, x.Meta.DataStart.Bitbucket = ds.PRs, agoP(7) })
	lt := Compute(late, noPrevious(late), nil, all()).Throughput
	if lt.Since == nil || !lt.Since.Equal(ago(7)) {
		t.Errorf("since = %v", lt.Since)
	}
	expectNear(t, "merged per week since", lt.MergedPerWeek, 2)

	// The facts say why.
	why := map[int64]string{}
	for _, r := range ComputeFacts(ds, all(), "").PRs.Rows {
		if r.NotCounted != nil {
			why[r.PRID] = *r.NotCounted
		}
	}
	want := map[int64]string{3: NotCountedOffTrunk, 4: NotCountedImplicitMerge, 5: NotCountedHousekeeping, 6: NotCountedOffTrunk}
	if fmt.Sprint(why) != fmt.Sprint(want) {
		t.Errorf("not counted = %v, want %v", why, want)
	}
}

// A run without a repository is counted team-wide whatever the view; a
// GitOps run is no build of the team's.
func TestBuildUnattributed(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.GitOpsRepos = []string{"kasse/kasse-gitops"}
		ds.Runs = []dataset.Run{
			run(),
			run(func(r *dataset.Run) { r.RunID, r.Repo, r.Project, r.PipelineName = "2", nil, nil, "nightly" }),
			run(func(r *dataset.Run) { r.RunID, r.Repo, r.Project, r.PipelineName = "3", nil, nil, "shared-lib/master" }),
			// no status: no finished build
			run(func(r *dataset.Run) { r.RunID, r.Repo, r.Project, r.Status = "4", nil, nil, nil }),
			// named after a GitOps repo
			run(func(r *dataset.Run) {
				r.RunID, r.Repo, r.Project, r.PipelineName = "5", nil, nil, "kasse-gitops/master"
			}),
		}
	})
	p := p4()
	if got := buildAlone(ds, p, filter{}).Unattributed; got != 2 {
		t.Errorf("team unattributed = %d", got)
	}
	if got := buildAlone(ds, p, filter{product: "portal"}).Unattributed; got != 2 {
		t.Errorf("portal unattributed = %d", got)
	}
}

// An open PR whose state is unknown is in no open number, no jam, no wait:
// counted apart. Its facts row says so, with no age and no wait.
func TestOpenUnknown(t *testing.T) {
	id := int64(200)
	long := func(mod ...func(*dataset.OpenPR)) dataset.OpenPR {
		id++
		return openPR(append([]func(*dataset.OpenPR){func(o *dataset.OpenPR) {
			o.PRID, o.OpenedAt, o.ReadySince, o.LastEventAt = id, ago(20), agoP(20), ago(15)
		}}, mod...)...)
	}
	unknown := func(o *dataset.OpenPR) { o.Unknown = true }
	ds := data(func(ds *dataset.Dataset) {
		// Four long-open PRs would jam the repo; one is unknown.
		ds.OpenPRs = []dataset.OpenPR{long(), long(), long(), long(unknown),
			long(unknown, func(o *dataset.OpenPR) { o.Repo, o.Project = "portal/portal-web", "portal" })}
	})
	o := openSection(ds, filter{})
	if o.Open != 3 || o.Long != 3 || o.Jammed != 0 || o.Unknown != 2 || o.Inactive != 0 || o.Waiting.FirstReview != 3 {
		t.Errorf("open %d, long %d, jammed %d, unknown %d, inactive %d, waiting %+v",
			o.Open, o.Long, o.Jammed, o.Unknown, o.Inactive, o.Waiting)
	}
	if len(o.Repos) != 1 || o.Repos[0].Open != 3 {
		t.Errorf("repos = %+v", o.Repos)
	}
	if k := openSection(ds, filter{product: "kasse"}); k.Unknown != 1 {
		t.Errorf("kasse unknown = %d", k.Unknown)
	}
	states := map[string]int{}
	for _, r := range ComputeFacts(ds, all(), "").PRs.Rows {
		states[r.State]++
		if r.State == PRStateUnknown && (r.AgeS != nil || r.Waiting != nil) {
			t.Errorf("unknown row = %+v", r)
		}
		if r.State == PRStateUnknown && (r.IdleS == nil || *r.IdleS != (15*24*time.Hour).Seconds()) {
			t.Errorf("unknown row idle = %v", r.IdleS)
		}
	}
	if states["open"] != 3 || states[PRStateUnknown] != 2 {
		t.Errorf("states = %v", states)
	}
}
