package dataset_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/metrics"
)

// Tracing a merged PR to the deploys that shipped it (trace.go): its own
// small product, shop, with an API, a UI and a GitOps repo. Invented names.

const (
	shopAPI = "shop/shop-api"
	shopUI  = "shop/shop-ui"
	shopOps = "shop/shop-gitops"
)

// pushFrom is push with the payload's changes[]: the branch moved from
// from (the tip before it) to the last commit.
func (s *seeder) pushFrom(repo, branch, from string, at time.Time, commits ...commit) {
	s.t.Helper()
	cs := make([]any, len(commits))
	for i, c := range commits {
		ms := c.authored.UnixMilli()
		cs[i] = map[string]any{"id": c.sha, "message": c.message, "authorTimestamp": ms, "committerTimestamp": ms,
			"author":  map[string]any{"name": "dev-anna", "displayName": "dev-anna", "type": "NORMAL"},
			"parents": []any{map[string]any{"id": sha("0")}}}
	}
	tip := commits[len(commits)-1].sha
	payload := jsonString(map[string]any{"eventKey": "repo:refs_changed", "commits": cs,
		"changes": []any{map[string]any{"ref": map[string]any{"displayId": branch, "type": "BRANCH"},
			"fromHash": from, "toHash": tip, "type": "UPDATE"}}})
	s.exec(`INSERT INTO bitbucket_events (delivery_id, event_type, repo_full_name, branch_name, commit_sha, author,
		occurred_at, team, jira_keys, payload, created_at)
		VALUES ($1, 'repo:refs_changed', $2, $3, $4, 'dev-anna', $5, $6, '{}', $7::jsonb, $5)`,
		s.id(), repo, branch, tip, at, team, payload)
}

// merged is a person's PR into master, merged at at, its merge commit mc
// pushed to the trunk in the same second.
func (s *seeder) merged(repo string, id int64, mc string, at time.Time) {
	s.t.Helper()
	branch := fmt.Sprintf("feature/shop-%d", id)
	s.pr(prEvent{event: "pr:opened", repo: repo, id: id, author: "dev-anna", branch: branch, target: "master",
		at: at.Add(-time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: repo, id: id, author: "dev-anna", branch: branch, target: "master",
		mergeCommit: mc, at: at})
	s.push(repo, "master", at, commit{sha: mc, authored: at, merge: true, message: fmt.Sprintf("Pull request #%d", id)})
}

// release pushes the GitOps release commit rev, its message the links.
func (s *seeder) release(rev string, at time.Time, links ...string) {
	s.t.Helper()
	s.push(shopOps, "master", at, commit{sha: rev, authored: at, message: "shop PROD\n\n" + strings.Join(links, "\n")})
}

// compare is a release commit's compare link of repo's slug from old to new.
func compare(repo, old, new string) string {
	project, slug, _ := strings.Cut(repo, "/")
	return "[" + slug + "](https://git.example.com/projects/" + strings.ToUpper(project) + "/repos/" + slug +
		"/compare/diff?targetBranch=" + old + "&sourceBranch=" + new + ")"
}

// prodOf is each PR's first production deploy (relative to T0), "-" for
// none, and whether it was placed through a lower bound ("~").
func prodOf(d *dataset.Dataset, repo string) map[int64]string {
	out := map[int64]string{}
	for _, p := range d.PRs {
		if p.Repo != repo || p.State != "merged" {
			continue
		}
		out[p.PRID] = "-"
		for _, e := range p.Environments {
			if e.Environment == "prod" {
				out[p.PRID] = e.FirstDeployedAt.Sub(t0).String() + "/" + e.Route
				if e.LowerBound {
					out[p.PRID] += "~"
				}
			}
		}
	}
	return out
}

func expectProd(t *testing.T, d *dataset.Dataset, repo string, want map[int64]string) {
	t.Helper()
	got := prodOf(d, repo)
	for id, w := range want {
		if got[id] != w {
			t.Errorf("%s #%d on prod %q, want %q", repo, id, got[id], w)
		}
	}
}

// The compare links' routes: both ends on the trunk (exact); an old end
// the repo's first push names as the tip before it (exact); an old end no
// payload names, which predates the data (a lower bound: everything since
// the repo's first trunk push). A PR merged after the new end is not in
// the release, whenever it deploys.
func TestTraceCompareLinks(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	// shop-api's data starts with a push whose tip before it riptide never
	// saw; shop-ui's first push names it.
	s.push(shopAPI, "master", days(1), commit{sha: sha("a"), authored: days(1)})
	s.pushFrom(shopUI, "master", strings.Repeat("e0", 20), days(1), commit{sha: sha("b"), authored: days(1)})
	s.merged(shopAPI, 1, strings.Repeat("a1", 20), days(2))
	s.merged(shopAPI, 2, strings.Repeat("a2", 20), days(4))
	s.merged(shopUI, 11, strings.Repeat("b1", 20), days(2))
	s.merged(shopUI, 12, strings.Repeat("b2", 20), days(3))
	s.release(strings.Repeat("c1", 20), days(4).Add(time.Hour),
		compare(shopAPI, strings.Repeat("0e", 20), strings.Repeat("a2", 20)),
		compare(shopUI, strings.Repeat("e0", 20), strings.Repeat("b1", 20)))
	s.deploy(team, "shop-prod", "prod", strings.Repeat("c1", 20), "Succeeded", days(5), "reg/shop-api:1.0", "reg/shop-ui:1.0")
	// Merged after the release's new end, before the next deploy: not in
	// it. The next release links both ends sighted.
	s.merged(shopAPI, 3, strings.Repeat("a3", 20), days(5).Add(time.Hour))
	s.merged(shopUI, 13, strings.Repeat("b3", 20), days(6))
	s.release(strings.Repeat("c2", 20), days(7),
		compare(shopAPI, strings.Repeat("a2", 20), strings.Repeat("a3", 20)),
		compare(shopUI, strings.Repeat("b1", 20), strings.Repeat("b2", 20)))
	s.deploy(team, "shop-prod", "prod", strings.Repeat("c2", 20), "Succeeded", days(8), "reg/shop-api:1.1", "reg/shop-ui:1.1")

	d := load(t, st, t0, days(28))
	expectProd(t, d, shopAPI, map[int64]string{1: "120h0m0s/compare~", 2: "120h0m0s/compare~", 3: "192h0m0s/compare"})
	// #12 merged before #11's release but after its new end: the next one.
	// #13 is in neither: the second link ends at #12's merge.
	expectProd(t, d, shopUI, map[int64]string{11: "120h0m0s/compare", 12: "192h0m0s/compare", 13: "-"})
	for _, dep := range d.Deploys {
		if !dep.Traced {
			t.Errorf("deploy %s at %v not traced", dep.App, dep.At)
		}
	}
}

// The image route, for a release commit without links: the run that built
// each deployed reference, its commit placed on the trunk; the newest head
// of the image repository's tags before (exact), else everything since the
// repo's first trunk push for an App's first deploy (a lower bound). A PR
// merged after the head but before the deploy is not in it; a run's own
// commit pushed after its start does not take in what landed meanwhile; a
// head pushed after the deploy is no head.
func TestTraceImages(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	api := func(n int) string { return strings.Repeat(fmt.Sprintf("a%d", n), 20) }
	ui := func(n int) string { return strings.Repeat(fmt.Sprintf("b%d", n), 20) }
	img := func(repo, tag string) string { return "reg/" + strings.Split(repo, "/")[1] + ":" + tag }
	build := func(repo, id, commit string, started time.Time, tags ...string) {
		var refs []string
		for _, tag := range tags {
			refs = append(refs, img(repo, tag))
		}
		s.run(run{pipeline: strings.Split(repo, "/")[1] + "/master", runID: id, status: "SUCCESS", commit: commit,
			buildType: "release", started: started, images: refs})
	}
	s.push(shopAPI, "master", days(1), commit{sha: api(0), authored: days(1)})
	s.push(shopUI, "master", days(1), commit{sha: ui(0), authored: days(1)})

	// The App's first deploy: everything since the data began.
	s.merged(shopAPI, 1, api(1), days(2))
	build(shopAPI, "1", api(1), days(2).Add(10*time.Minute), "1.0")
	// The UI job pushes its own release commit three minutes into the run;
	// #12, merged in between, is not in the image.
	s.merged(shopUI, 11, ui(1), days(2))
	build(shopUI, "1", ui(9), days(2).Add(time.Hour), "1.0")
	s.merged(shopUI, 12, ui(2), days(2).Add(time.Hour+time.Minute))
	s.push(shopUI, "master", days(2).Add(time.Hour+3*time.Minute), commit{sha: ui(9), authored: days(2).Add(time.Hour)})
	s.release(sha("1"), days(3).Add(-time.Hour))
	s.deploy(team, "shop-prod", "prod", sha("1"), "Succeeded", days(3), img(shopAPI, "1.0"), img(shopUI, "1.0"))

	// A new tag: from the head of the one before (exact). #3 merged after
	// the build's head, before the deploy: not in it.
	s.merged(shopAPI, 2, api(2), days(4))
	build(shopAPI, "2", api(2), days(4).Add(10*time.Minute), "1.1")
	s.merged(shopAPI, 3, api(3), days(4).Add(time.Hour))
	s.release(sha("2"), days(5).Add(-time.Hour))
	s.deploy(team, "shop-prod", "prod", sha("2"), "Succeeded", days(5), img(shopAPI, "1.1"), img(shopUI, "1.0"))

	// Two tags of one image repository side by side: from the newest head
	// of the set before to the newest of this one.
	s.merged(shopAPI, 4, api(4), days(5).Add(30*time.Minute))
	build(shopAPI, "3", api(4), days(5).Add(time.Hour), "1.2")
	s.release(sha("3"), days(6).Add(-time.Hour))
	s.deploy(team, "shop-prod", "prod", sha("3"), "Succeeded", days(6), img(shopAPI, "1.1"), img(shopAPI, "1.2"),
		img(shopUI, "1.0"))

	// A head pushed only after the deploy built nothing it ran.
	s.merged(shopAPI, 5, api(5), days(6).Add(30*time.Minute))
	build(shopAPI, "4", api(6), days(6).Add(time.Hour), "1.3")
	s.release(sha("4"), days(7).Add(-time.Hour))
	s.deploy(team, "shop-prod", "prod", sha("4"), "Succeeded", days(7), img(shopAPI, "1.3"), img(shopUI, "1.0"))
	s.push(shopAPI, "master", days(8), commit{sha: api(6), authored: days(8)})

	d := load(t, st, t0, days(28))
	expectProd(t, d, shopAPI, map[int64]string{1: "72h0m0s/image~", 2: "120h0m0s/image", 3: "144h0m0s/image",
		4: "144h0m0s/image", 5: "-"})
	expectProd(t, d, shopUI, map[int64]string{11: "72h0m0s/image~", 12: "-"})
	rt := d.Traces[shopAPI]
	var unresolved []string
	for _, r := range rt.Releases {
		if !r.Resolved(days(6).Add(30*time.Minute), "master") {
			unresolved = append(unresolved, r.At.Sub(t0).String())
		}
	}
	if !slices.Equal(unresolved, []string{"168h0m0s"}) {
		t.Errorf("releases unresolved for #5 = %v", unresolved)
	}
	// shop-ui's image stayed the same: those releases shipped nothing of it.
	for _, r := range d.Traces[shopUI].Releases[1:] {
		if !r.Unchanged {
			t.Errorf("shop-ui release at %v changed it", r.At.Sub(t0))
		}
	}
}

// Compare links naming other repos only prove a release shipped nothing of
// a repo while its own image stayed the same; its image changed, with a
// head tracing cannot place, the release may have shipped its PRs: never
// "not yet".
func TestTraceLinksNamingOthersOnly(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.push(shopAPI, "master", days(1), commit{sha: sha("a"), authored: days(1)})
	s.push(shopUI, "master", days(1), commit{sha: sha("b"), authored: days(1)})
	s.run(run{pipeline: "shop-api/master", runID: "1", status: "SUCCESS", commit: sha("a"), buildType: "release",
		started: days(1).Add(time.Minute), images: []string{"reg/shop-api:1.0"}})
	s.release(sha("1"), days(2).Add(-time.Hour), compare(shopUI, strings.Repeat("e0", 20), sha("b")))
	s.deploy(team, "shop-prod", "prod", sha("1"), "Succeeded", days(2), "reg/shop-api:1.0", "reg/shop-ui:1.0")
	s.merged(shopAPI, 1, strings.Repeat("a1", 20), days(3))
	// The next release's links name shop-ui only; shop-api's tag moves, its
	// run's commit never reached the trunk Bitbucket reported.
	s.merged(shopUI, 11, strings.Repeat("b1", 20), days(3))
	s.run(run{pipeline: "shop-api/master", runID: "2", status: "SUCCESS", commit: strings.Repeat("a9", 20),
		buildType: "release", started: days(3).Add(time.Hour), images: []string{"reg/shop-api:1.1"}})
	s.release(sha("2"), days(4).Add(-time.Hour), compare(shopUI, sha("b"), strings.Repeat("b1", 20)))
	s.deploy(team, "shop-prod", "prod", sha("2"), "Succeeded", days(4), "reg/shop-api:1.1", "reg/shop-ui:1.1")
	// The release after: shop-ui only, shop-api's image the same.
	s.merged(shopUI, 12, strings.Repeat("b2", 20), days(5))
	s.release(sha("3"), days(6).Add(-time.Hour), compare(shopUI, strings.Repeat("b1", 20), strings.Repeat("b2", 20)))
	s.deploy(team, "shop-prod", "prod", sha("3"), "Succeeded", days(6), "reg/shop-api:1.1", "reg/shop-ui:1.2")
	d := load(t, st, t0, days(28))
	expectProd(t, d, shopAPI, map[int64]string{1: "-"})
	expectProd(t, d, shopUI, map[int64]string{11: "96h0m0s/compare", 12: "144h0m0s/compare"})
	var resolved []bool
	for _, r := range d.Traces[shopAPI].Releases {
		if r.At.After(days(3)) {
			resolved = append(resolved, r.Resolved(days(3), "master"))
		}
	}
	if !slices.Equal(resolved, []bool{false, true}) {
		t.Errorf("shop-api's releases after #1 resolved = %v, want [false true]", resolved)
	}
}

// A run's head caps a compare link's new end: the release tool's commit
// the link names was pushed after the build started, and a PR merged in
// between is not in the image.
func TestTraceLinkCappedByTheBuild(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.push(shopAPI, "master", days(1), commit{sha: sha("a"), authored: days(1)})
	s.merged(shopAPI, 1, strings.Repeat("a1", 20), days(2))
	s.run(run{pipeline: "shop-api/master", runID: "1", status: "SUCCESS", commit: strings.Repeat("a1", 20),
		buildType: "release", started: days(2).Add(time.Minute), images: []string{"reg/shop-api:1.0"}})
	s.merged(shopAPI, 2, strings.Repeat("a2", 20), days(2).Add(2*time.Minute))
	s.push(shopAPI, "master", days(2).Add(3*time.Minute), commit{sha: strings.Repeat("a9", 20), authored: days(2)})
	s.release(sha("1"), days(3).Add(-time.Hour), compare(shopAPI, sha("a"), strings.Repeat("a9", 20)))
	s.deploy(team, "shop-prod", "prod", sha("1"), "Succeeded", days(3), "reg/shop-api:1.0")
	d := load(t, st, t0, days(28))
	expectProd(t, d, shopAPI, map[int64]string{1: "72h0m0s/compare", 2: "-"})
}

// A PR merged into another branch reaches the trunk with the PR that merges
// that branch; one Bitbucket marked merged when an umbrella's merge brought
// it in, with the umbrella.
func TestTraceThroughUmbrellas(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.push(shopAPI, "master", days(1), commit{sha: sha("a"), authored: days(1)})
	// #2 into the umbrella branch, merged on with #3 two days later.
	s.pr(prEvent{event: "pr:opened", repo: shopAPI, id: 2, author: "dev-anna", branch: "feature/shop-2",
		target: "feature/umbau", at: days(1)})
	s.pr(prEvent{event: "pr:merged", repo: shopAPI, id: 2, author: "dev-anna", branch: "feature/shop-2",
		target: "feature/umbau", mergeCommit: strings.Repeat("a2", 20), at: days(2)})
	// The trunk is master: most PRs go there.
	s.merged(shopAPI, 1, strings.Repeat("a1", 20), days(1).Add(time.Hour))
	s.merged(shopAPI, 5, strings.Repeat("a5", 20), days(1).Add(2*time.Hour))
	s.pr(prEvent{event: "pr:opened", repo: shopAPI, id: 3, author: "dev-otto", branch: "feature/umbau", target: "master",
		at: days(3)})
	s.pr(prEvent{event: "pr:merged", repo: shopAPI, id: 3, author: "dev-otto", branch: "feature/umbau", target: "master",
		mergeCommit: strings.Repeat("a3", 20), at: days(4)})
	s.push(shopAPI, "master", days(4), commit{sha: strings.Repeat("a3", 20), authored: days(4), merge: true})
	// #4, marked merged in #3's second without a merge commit of its own.
	s.pr(prEvent{event: "pr:opened", repo: shopAPI, id: 4, author: "dev-anna", branch: "feature/shop-4", target: "master",
		at: days(2)})
	s.pr(prEvent{event: "pr:merged", repo: shopAPI, id: 4, author: "dev-anna", branch: "feature/shop-4", target: "master",
		at: days(4)})
	// #6 into a branch nothing went on from: not on the trunk.
	s.pr(prEvent{event: "pr:merged", repo: shopAPI, id: 6, author: "dev-anna", branch: "feature/shop-6",
		target: "feature/liegt", mergeCommit: strings.Repeat("a6", 20), at: days(2)})
	s.release(sha("1"), days(5).Add(-time.Hour), compare(shopAPI, strings.Repeat("a1", 20), strings.Repeat("a3", 20)))
	s.deploy(team, "shop-prod", "prod", sha("1"), "Succeeded", days(5), "reg/shop-api:1.0")
	d := load(t, st, t0, days(28))
	expectProd(t, d, shopAPI, map[int64]string{1: "-", 2: "120h0m0s/compare", 3: "120h0m0s/compare",
		4: "120h0m0s/compare", 5: "120h0m0s/compare", 6: "-"})
	for _, p := range d.PRs {
		if p.PRID == 6 && (p.TrunkAt != nil || !p.OffTrunk) {
			t.Errorf("#6 trunk %v off trunk %v", p.TrunkAt, p.OffTrunk)
		}
	}
}

// A closed period is frozen at its end: a deploy after it places none of
// its PRs on production, in no number; the facts, read to now, show it.
// Library and bot PRs are in no base.
func TestTraceClosedPeriodFrozen(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	// Argo CD reports from the start: a failed sync, nothing deployed.
	s.deploy(team, "shop-prod", "prod", sha("0"), "Failed", days(0.5))
	s.push(shopAPI, "master", days(1), commit{sha: sha("a"), authored: days(1)})
	s.merged(shopAPI, 1, strings.Repeat("a1", 20), days(2))
	s.merged(shopAPI, 2, strings.Repeat("a2", 20), days(3))
	s.release(sha("1"), days(4).Add(-time.Hour), compare(shopAPI, sha("a"), strings.Repeat("a1", 20)))
	s.deploy(team, "shop-prod", "prod", sha("1"), "Succeeded", days(4), "reg/shop-api:1.0")
	// A bot's PR, shipped with #1's release: never in the base.
	s.pr(prEvent{event: "pr:merged", repo: shopAPI, id: 9, author: "renovate-bot", automation: "renovate",
		branch: "renovate/lib", target: "master", mergeCommit: strings.Repeat("a9", 20), at: days(1).Add(time.Hour)})
	s.push(shopAPI, "master", days(1).Add(time.Hour), commit{sha: strings.Repeat("a9", 20), authored: days(1)})
	period := metrics.NewPeriod(1, days(5), time.UTC)
	compute := func() metrics.Metrics {
		d := load(t, st, period.From, period.To)
		return metrics.Compute(d, &dataset.Dataset{Meta: d.Meta}, nil, metrics.Params{Period: period})
	}
	before := compute()
	c := before.Coverage
	if c.Merged != 2 || c.OnProd != 1 || c.NotYet != 1 || c.NotPlaceable != 0 || !eqTime(c.OldestNotYet, ts(days(3))) {
		t.Fatalf("coverage = %+v", c.TraceCounts)
	}
	if l := before.Delivery.LeadTime; l.Human.N != 1 || before.Throughput.Coverage.ToProd.Count != 1 {
		t.Errorf("lead time %+v, to prod %+v", l, before.Throughput.Coverage.ToProd)
	}
	// #2 ships after the period's end.
	s.release(sha("2"), days(6), compare(shopAPI, strings.Repeat("a1", 20), strings.Repeat("a2", 20)))
	s.deploy(team, "shop-prod", "prod", sha("2"), "Succeeded", days(6).Add(time.Hour), "reg/shop-api:1.1")
	after := compute()
	if a, b := jsonString([]any{after.Coverage, after.Delivery.LeadTime, after.LeadChain, after.Throughput.Coverage}),
		jsonString([]any{before.Coverage, before.Delivery.LeadTime, before.LeadChain, before.Throughput.Coverage}); a != b {
		t.Errorf("closed period moved:\n%s\n%s", b, a)
	}
	facts := metrics.ComputeFacts(load(t, st, period.From, days(28)), metrics.Params{Period: period}, "")
	for _, row := range facts.PRs.Rows {
		if row.PRID == 2 && (row.ProdState == nil || *row.ProdState != metrics.ProdOnProd || row.ProdAt == nil) {
			t.Errorf("facts #2 = %v at %v", str(row.ProdState), row.ProdAt)
		}
	}
}

// Every release of a repo's Apps tells whether it is resolved for the repo;
// one in the reach of a lost Bitbucket silence says so.
func TestTraceReleasesInASilence(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.push(shopAPI, "master", days(1), commit{sha: sha("a"), authored: days(1)})
	s.merged(shopAPI, 1, strings.Repeat("a1", 20), days(2))
	s.release(sha("1"), days(3).Add(-time.Hour), compare(shopAPI, sha("a"), strings.Repeat("a1", 20)))
	s.deploy(team, "shop-prod", "prod", sha("1"), "Succeeded", days(3), "reg/shop-api:1.0")
	s.merged(shopAPI, 2, strings.Repeat("a2", 20), days(4))
	s.run(run{pipeline: "shop-api/master", runID: "1", status: "SUCCESS", commit: strings.Repeat("a1", 20), started: days(2)})
	// Bitbucket stores nothing from day 4 to day 10 while CI builds
	// commits it never saw: lost. The release on day 9 went out in it.
	for i := range 8 {
		at := days(7).Add(time.Duration(i) * 8 * time.Hour)
		s.run(run{pipeline: "shop-api/master", runID: fmt.Sprint(100 + i), status: "SUCCESS",
			commit: strings.Repeat(fmt.Sprintf("%02x", i), 20), started: at})
	}
	s.push(shopAPI, "master", days(4).Add(time.Hour), commit{sha: sha("b"), authored: days(4)})
	s.push(shopAPI, "master", days(10).Add(time.Hour), commit{sha: sha("c"), authored: days(10)})
	s.deploy(team, "shop-prod", "prod", sha("9"), "Succeeded", days(9), "reg/shop-api:1.1")
	d := load(t, st, t0, days(28))
	expectProd(t, d, shopAPI, map[int64]string{1: "72h0m0s/compare", 2: "-"})
	rt := d.Traces[shopAPI]
	if len(rt.Releases) != 2 || rt.Releases[0].Silence || !rt.Releases[1].Silence || rt.Releases[1].Resolved(days(4), "") {
		t.Errorf("releases = %+v", rt.Releases)
	}
}
