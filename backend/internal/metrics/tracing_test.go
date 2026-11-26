package metrics

import (
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// Where a merged PR stands on production at a cutoff: the first rule that
// applies. Never "not yet" without proof: a release of its repo's Apps
// tracing could not resolve may have shipped it.
func TestProdPlacement(t *testing.T) {
	cutoff := p4().To
	merged := ago(5)
	release := func(at time.Time, mod ...func(*dataset.TraceRelease)) dataset.TraceRelease {
		r := dataset.TraceRelease{Environment: "prod", At: at}
		for _, m := range mod {
			m(&r)
		}
		return r
	}
	stretch := func(from, to time.Time, lower bool) func(*dataset.TraceRelease) {
		return func(r *dataset.TraceRelease) {
			r.Stretches = append(r.Stretches, dataset.Stretch{Environment: "prod", At: r.At, Repo: "kasse/kasse-api",
				Branch: "master", From: from, To: to, LowerBound: lower})
		}
	}
	onBranch := func(branch string, from, to time.Time) func(*dataset.TraceRelease) {
		return func(r *dataset.TraceRelease) {
			r.Stretches = append(r.Stretches, dataset.Stretch{Environment: "prod", At: r.At, Repo: "kasse/kasse-api",
				Branch: branch, From: from, To: to})
		}
	}
	knownAt := func(at time.Time) func(*dataset.TraceRelease) {
		return func(r *dataset.TraceRelease) { r.Known = at }
	}
	traced := func(rels ...dataset.TraceRelease) map[string]dataset.RepoTrace {
		return map[string]dataset.RepoTrace{"kasse/kasse-api": {DeployedSince: agoP(50), Releases: rels}}
	}
	develop := func(p *dataset.PR) { p.TrunkBranch = ptr("develop") }
	for _, c := range []struct {
		name          string
		pr            func(*dataset.PR)
		traces        map[string]dataset.RepoTrace
		state, reason string
	}{
		{"shipped before the cutoff", func(p *dataset.PR) {
			p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: ago(1), Route: dataset.RouteImage}}
		}, traced(), ProdOnProd, ""},
		{"shipped after it: not yet then", func(p *dataset.PR) {
			p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: cutoff}}
		}, traced(), ProdNotYet, ""},
		{"only on intg", func(p *dataset.PR) {
			p.Environments = []dataset.EnvironmentDeploy{{Environment: "intg", FirstDeployedAt: ago(1)}}
		}, traced(), ProdNotYet, ""},
		{"no merge commit", func(p *dataset.PR) { p.TrunkAt, p.MergeCommit = nil, nil }, traced(), ProdNotPlaceable,
			TraceNoMergeCommit},
		// Off the trunk with no umbrella merged: waiting there, or gone on by a
		// push or a hotfix deploy; no telling, never "not yet".
		{"merged into a branch that went nowhere yet", func(p *dataset.PR) { p.TrunkAt, p.OffTrunk = nil, true }, traced(),
			ProdNotPlaceable, TraceOffTrunk},
		// The same at the cutoff when its umbrella merges only after it: a
		// closed period does not move.
		{"off the trunk, its umbrella merged after the cutoff", func(p *dataset.PR) {
			p.TrunkAt, p.OffTrunk = ptr(cutoff.Add(time.Hour)), true
		}, traced(), ProdNotPlaceable, TraceOffTrunk},
		{"off the trunk, its umbrella merged before the cutoff, no release since", func(p *dataset.PR) { p.OffTrunk = true },
			traced(), ProdNotYet, ""},
		{"its umbrella merged after the cutoff", func(p *dataset.PR) { p.TrunkAt = ptr(cutoff.Add(time.Hour)) }, traced(),
			ProdNotYet, ""},
		{"no App known to deploy the repo", func(*dataset.PR) {}, map[string]dataset.RepoTrace{}, ProdNotPlaceable,
			TraceRepoUntraced},
		{"an App known only after the cutoff", func(*dataset.PR) {},
			map[string]dataset.RepoTrace{"kasse/kasse-api": {DeployedSince: ptr(cutoff.Add(time.Hour))}}, ProdNotPlaceable,
			TraceRepoUntraced},
		{"a release since, unresolved", func(*dataset.PR) {}, traced(release(ago(2))), ProdNotPlaceable,
			TraceReleaseUnresolved},
		{"a release since, unresolved, in a silence", func(*dataset.PR) {},
			traced(release(ago(2), func(r *dataset.TraceRelease) { r.Silence = true })), ProdNotPlaceable, TraceInSilence},
		{"one in a silence, one not", func(*dataset.PR) {},
			traced(release(ago(3), func(r *dataset.TraceRelease) { r.Silence = true }), release(ago(2))), ProdNotPlaceable,
			TraceReleaseUnresolved},
		{"a release before the merge is no evidence", func(*dataset.PR) {}, traced(release(ago(6))), ProdNotYet, ""},
		{"a release after the cutoff is no evidence", func(*dataset.PR) {}, traced(release(cutoff.Add(time.Hour))),
			ProdNotYet, ""},
		{"a resolved release that ended before the merge", func(*dataset.PR) {},
			traced(release(ago(2), stretch(ago(9), ago(6), true))), ProdNotYet, ""},
		{"an exact stretch that ended before it", func(*dataset.PR) {},
			traced(release(ago(2), stretch(ago(9), ago(6), false))), ProdNotYet, ""},
		// Starting at a known old end after the PR reached the trunk: the
		// App ran it already, with a deploy riptide did not see.
		{"an exact stretch starting after it: shipped unseen", func(*dataset.PR) {},
			traced(release(ago(2), stretch(ago(4), ago(3), false))), ProdNotPlaceable, TraceInSilence},
		{"an exact stretch starting at it: shipped unseen", func(*dataset.PR) {},
			traced(release(ago(2), stretch(merged, ago(3), false))), ProdNotPlaceable, TraceInSilence},
		{"an exact stretch of another branch proves nothing", func(*dataset.PR) {},
			traced(release(ago(2), onBranch("release/1", ago(4), ago(3)), stretch(ago(9), ago(6), false))), ProdNotYet, ""},
		// A release whose App was known to deploy the repo only after the
		// cutoff is no evidence for the period (closed periods stay put).
		{"a release of an App known only after the cutoff", func(*dataset.PR) {},
			traced(release(ago(2), knownAt(cutoff))), ProdNotYet, ""},
		{"a release of an App known before the cutoff", func(*dataset.PR) {},
			traced(release(ago(2), knownAt(ago(60)))), ProdNotPlaceable, TraceReleaseUnresolved},
		// Into develop, a trunk: master's releases hold it once a PR merged
		// develop into master, and tell nothing before.
		{"into develop, a master release, not onward yet", develop,
			traced(release(ago(2), onBranch("master", ago(9), ago(3)))), ProdNotPlaceable, TraceReleaseUnresolved},
		{"into develop, onward on master after the release", func(p *dataset.PR) {
			develop(p)
			p.OnwardAt, p.OnwardBranch = agoP(1), ptr("master")
		}, traced(release(ago(2), onBranch("master", ago(9), ago(3)))), ProdNotYet, ""},
		{"into develop, onward only after the cutoff", func(p *dataset.PR) {
			develop(p)
			p.OnwardAt, p.OnwardBranch = ptr(cutoff.Add(time.Hour)), ptr("master")
		}, traced(release(ago(2), onBranch("master", ago(9), ago(3)))), ProdNotPlaceable, TraceReleaseUnresolved},
		{"a lower bound starting after the merge may have it", func(*dataset.PR) {},
			traced(release(ago(2), stretch(ago(4), ago(3), true))), ProdNotPlaceable, TraceReleaseUnresolved},
		{"a release that shipped nothing of the repo", func(*dataset.PR) {},
			traced(release(ago(2), func(r *dataset.TraceRelease) { r.Unchanged = true })), ProdNotYet, ""},
		{"a release on intg is none", func(*dataset.PR) {},
			traced(release(ago(2), func(r *dataset.TraceRelease) { r.Environment = "intg" })), ProdNotYet, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			ds := data(func(ds *dataset.Dataset) { ds.Traces = c.traces })
			p := pr(func(p *dataset.PR) { p.MergedAt, p.TrunkAt = &merged, &merged }, c.pr)
			got := prodPlacement(ds, p, cutoff)
			if got.state != c.state || got.reason != c.reason {
				t.Errorf("placement = %+v, want %s %s", got, c.state, c.reason)
			}
		})
	}
	// Argo CD not reporting yet when it reached the trunk: no telling.
	ds := data(func(ds *dataset.Dataset) { ds.Traces, ds.Meta.DataStart.ArgoCD = traced(), agoP(4) })
	if got := prodPlacement(ds, pr(func(p *dataset.PR) { p.TrunkAt = &merged }), cutoff); got.reason != TraceInSilence {
		t.Errorf("argo late = %+v", got)
	}
}

// Coverage adds up, in all and per repo: merged = on prod + not yet + not
// placeable. A library's PR is outside the base, a bot's in no number.
func TestCoverage(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.Traces = map[string]dataset.RepoTrace{"kasse/kasse-api": {DeployedSince: agoP(50)}}
		ds.PRs = []dataset.PR{
			shipped(3600, func(p *dataset.PR) { p.PRID = 1; p.Environments[0].LowerBound = true }),
			pr(func(p *dataset.PR) { p.PRID = 2 }),
			pr(func(p *dataset.PR) { p.PRID, p.MergedAt, p.TrunkAt = 3, agoP(3), agoP(3) }),
			pr(func(p *dataset.PR) { p.PRID, p.TrunkAt, p.MergeCommit = 4, nil, nil }),
			pr(func(p *dataset.PR) { p.Repo, p.PRID, p.Library = "kasse/kasse-lib", 5, true }),
			shipped(60, func(p *dataset.PR) { p.PRID, p.Bot = 6, true }),
			pr(func(p *dataset.PR) { p.Repo, p.Project, p.PRID = "portal/portal-web", "portal", 7 }),
		}
	})
	m := Compute(ds, noPrevious(ds), nil, all())
	c := m.Coverage
	if c.Merged != 5 || c.OnProd != 1 || c.LowerBound != 1 || c.NotYet != 2 || c.NotPlaceable != 2 || c.Library != 1 ||
		c.Reasons.NoMergeCommit != 1 || c.Reasons.RepoUntraced != 1 || !eqTimePtr(c.OldestNotYet, agoP(3)) {
		t.Fatalf("coverage = %+v", c.TraceCounts)
	}
	if to := m.Throughput.Coverage.ToProd; to.Count != c.OnProd || to.N != c.Merged {
		t.Errorf("to prod %+v, coverage %d of %d", to, c.OnProd, c.Merged)
	}
	if l := m.Delivery.LeadTime; l.Human.N != 1 || l.LowerBound != 1 {
		t.Errorf("lead time = %+v", l)
	}
	// Most not yet first; the library repo, nothing to place, last.
	var order []string
	for _, r := range c.Repos {
		order = append(order, r.Repo)
		if r.Merged != r.OnProd+r.NotYet+r.NotPlaceable {
			t.Errorf("%s does not add up: %+v", r.Repo, r.TraceCounts)
		}
	}
	if len(order) != 3 || order[0] != "kasse/kasse-api" || order[1] != "portal/portal-web" || order[2] != "kasse/kasse-lib" {
		t.Errorf("repos = %v", order)
	}
	// A product view: its repos only.
	if p := Compute(ds, noPrevious(ds), nil, product("portal")).Coverage; p.Merged != 1 || p.NotPlaceable != 1 || len(p.Repos) != 1 {
		t.Errorf("portal coverage = %+v", p)
	}
}

func eqTimePtr(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
