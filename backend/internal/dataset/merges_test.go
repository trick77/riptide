package dataset_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/metrics"
)

// A housekeeping batch opened across a period's edge is one batch in both
// periods: its siblings are the team's openings of all history, not the
// window's PRs. Three repos opened the title within the hour around T0;
// two merged before, one after.
func TestDatasetHousekeepingAcrossThePeriodEdge(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const web, ops = "kasse/kasse-web", "kasse/kasse-contract"
	merged := func(repo string, id int64, opened, at time.Time) {
		s.pr(prEvent{event: "pr:opened", repo: repo, id: id, author: "dev-anna", branch: "chore/lint", target: "master",
			title: "Rotate lint config", at: opened})
		s.pr(prEvent{event: "pr:merged", repo: repo, id: id, author: "dev-anna", branch: "chore/lint", target: "master",
			title: "Rotate lint config", mergeCommit: fmt.Sprintf("%040x", id), at: at})
	}
	merged(web, 1, t0.Add(-20*time.Minute), t0.Add(-15*time.Minute))
	merged(ops, 2, t0.Add(-10*time.Minute), t0.Add(-5*time.Minute))
	merged(kasseAPI, 3, t0.Add(10*time.Minute), t0.Add(20*time.Minute))
	for _, w := range []struct {
		name     string
		from, to time.Time
		want     string
	}{
		{"previous", days(-7), t0, "[kasse/kasse-contract#2 kasse/kasse-web#1]"},
		{"current", t0, days(7), "[kasse/kasse-api#3]"},
	} {
		d := load(t, st, w.from, w.to)
		var got []string
		for _, p := range d.PRs {
			if p.Housekeeping {
				got = append(got, fmt.Sprintf("%s#%d", p.Repo, p.PRID))
			}
		}
		if fmt.Sprint(got) != w.want {
			t.Errorf("%s period: housekeeping = %v, want %v", w.name, got, w.want)
		}
	}
}

// An App going live on the Monday after a lost Bitbucket hole ended on
// Friday is no infrastructure: its GitOps commit may lie in the hole, and
// Argo CD synced it days later. One live long after the hole stays one.
func TestDatasetNewAppAfterAnOutageIsNoInfra(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	busy(s, kasseAPI, "kasse-api/master", -20, 8)
	s.push(kasseOps, "master", days(-20), commit{sha: sha("b"), authored: days(-20)})
	s.deploy(team, "kasse-prod", "prod", sha("b"), "Succeeded", days(-20), "reg/kasse-api:1.0")
	// Bitbucket quiet from Tuesday 09:36 to Friday 09:36 of the second week
	// while a push it lost is built.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 5, author: "dev-anna", branch: "feature/a", at: days(8.4)})
	s.run(run{pipeline: "kasse-api/master", runID: "lost", status: "SUCCESS", commit: strings.Repeat("5e", 20), started: days(9.5)})
	s.deploy(team, "kasse-prod", "prod", sha("b"), "Succeeded", days(10))
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 6, author: "dev-anna", branch: "feature/b", at: days(11.4)})
	// Monday after: a new App goes live, its revision unknown to Bitbucket.
	s.deploy(team, "neu-prod", "prod", strings.Repeat("6e", 20), "Succeeded", days(14.4), "reg/neu:1")
	// Three weeks later, another.
	s.deploy(team, "spaet-prod", "prod", strings.Repeat("6f", 20), "Succeeded", days(25), "reg/spaet:1")
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 7, author: "dev-anna", branch: "feature/c", at: days(25.5)})
	d := load(t, st, t0, days(28))
	infra := map[string]bool{}
	for _, dep := range d.Deploys {
		infra[dep.App] = dep.Infra
	}
	if fmt.Sprint(infra) != "map[kasse-prod:false neu-prod:false spaet-prod:true]" {
		t.Errorf("infra = %v", infra)
	}
}

// A PR is an umbrella only when a person's PR went into its source branch,
// a bot told as every PR's opener is: by riptide's automation detection at
// read too, not only the ingest flag. And a person's PR merged with its
// merge commit in the second a bot's PR was marked merged without one is
// no umbrella: a bot's PR never makes a person's uncounted.
func TestDatasetUmbrellaNeedsAPerson(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const web = "kasse/kasse-web"
	// Renovate-Bot is a configured author, its rows not flagged at ingest.
	s.pr(prEvent{event: "pr:opened", repo: web, id: 1, author: "Renovate-Bot", branch: "deps/x", target: "release/1",
		at: days(1)})
	s.pr(prEvent{event: "pr:merged", repo: web, id: 1, author: "Renovate-Bot", branch: "deps/x", target: "release/1",
		mergeCommit: sha("1"), at: days(1.5)})
	s.pr(prEvent{event: "pr:opened", repo: web, id: 2, author: "dev-anna", branch: "release/1", target: "master", at: days(2)})
	s.pr(prEvent{event: "pr:merged", repo: web, id: 2, author: "dev-anna", branch: "release/1", target: "master",
		mergeCommit: sha("2"), at: days(2.5)})
	at := days(3)
	s.pr(prEvent{event: "pr:opened", repo: web, id: 30, author: "dev-anna", branch: "feature/a", target: "master", at: at.Add(-time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: web, id: 30, author: "dev-anna", branch: "feature/a", target: "master",
		mergeCommit: sha("3"), at: at})
	s.pr(prEvent{event: "pr:opened", repo: web, id: 31, author: "renovate-bot", automation: "renovate", branch: "renovate/y",
		target: "master", at: at.Add(-time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: web, id: 31, author: "renovate-bot", automation: "renovate", branch: "renovate/y",
		target: "master", at: at.Add(300 * time.Millisecond)})
	d := load(t, st, t0, days(7))
	for _, p := range d.PRs {
		if p.Umbrella {
			t.Errorf("#%d an umbrella", p.PRID)
		}
	}
}

// A branch merged into an umbrella by its own PR, and marked merged into
// master by a second PR of the same branch when the umbrella went in, is
// one piece of work: the second is not counted again.
func TestDatasetTwiceFromOneBranch(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const web = "kasse/kasse-web"
	pr := func(id int64, branch, target, commit string, at time.Time) {
		s.pr(prEvent{event: "pr:opened", repo: web, id: id, author: "dev-anna", branch: branch, target: target, at: at.Add(-time.Hour)})
		s.pr(prEvent{event: "pr:merged", repo: web, id: id, author: "dev-anna", branch: branch, target: target, mergeCommit: commit, at: at})
	}
	pr(1, "feature/x", "release/1", sha("1"), days(2))
	at := days(3)
	pr(2, "release/1", "master", sha("2"), at)
	pr(3, "feature/x", "master", "", at.Add(400*time.Millisecond))
	d := load(t, st, t0, days(7))
	got := map[int64]string{}
	for _, p := range d.PRs {
		got[p.PRID] = fmt.Sprintf("umbrella=%v implicit=%v twice=%v", p.Umbrella, p.ImplicitMerge, p.Twice)
	}
	want := map[int64]string{1: "umbrella=false implicit=false twice=false", 2: "umbrella=true implicit=false twice=false",
		3: "umbrella=false implicit=true twice=true"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("prs = %v, want %v", got, want)
	}
	m := metrics.Compute(d, &dataset.Dataset{Meta: d.Meta}, nil, metrics.Params{Period: metrics.ClosedPeriod(1, days(7), time.UTC)})
	if m.Throughput.Merged != 1 || m.Throughput.Twice != 1 || m.Throughput.Umbrella != 1 {
		t.Errorf("merged %d, twice %d, umbrella %d", m.Throughput.Merged, m.Throughput.Twice, m.Throughput.Umbrella)
	}
}
