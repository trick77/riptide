package dataset_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/trick77/riptide/internal/store"
	"github.com/trick77/riptide/internal/testdb"
)

// The seed: one team (checkout) with two products, kasse and portal, and a
// second team (platform) whose rows must never show up. Invented names only.
//
// Timeline, T0 = window start, window 28 days:
//
//	T0-20d    PR #1 opened, merged next day (before the window)
//	T0-12d    master push A (the previous release head)
//	T0-10d    kasse-prod deploys images 1.0 (before the window: its first deploy)
//	T0-3d     PR #5 opened as a draft; ready at T0-2d, review at T0-1d
//	T0+19h    PR #7's first commit; PR opened T0+1d, approved +3h (first touch), merged +5h
//	T0+1d+5h  master push: F1, a renovate commit, the merge commit M1
//	T0+1d+6h  master push: R1, the release tooling commit
//	T0+2d-1h  GitOps release commit G1 bumps kasse-api A..R1; GitOps PR #3
//	T0+2d     kasse-intg deploys G1 (its first deploy); PR #5 approved, merged +1h
//	T0+3d     kasse-prod deploys G1: new images, a release
//	T0+4d     kasse-prod deploys a config change: same images, a sync
//	T0+5d     kasse-prod deploy fails; renovate PR #9 opened
//	T0+6d     portal-prod's first deploy; PR #2 opened by a declared service account, declined
//	T0+7d     atlas-prod deploys a revision Bitbucket never saw (no project)
var t0 = time.Date(2030, 3, 4, 0, 0, 0, 0, time.UTC)

const (
	team      = "checkout"
	otherTeam = "platform"
	kasseAPI  = "kasse/kasse-api"
	kasseOps  = "kasse/kasse-gitops"
	portalWeb = "portal/portal-web"
	portalOps = "portal/portal-gitops"
	ledger    = "ledger/ledger-core"
)

func hours(n float64) time.Time { return t0.Add(time.Duration(n * float64(time.Hour))) }
func days(n float64) time.Time  { return hours(24 * n) }

func sha(c string) string { return strings.Repeat(c, 40) }

type seeder struct {
	t    *testing.T
	pool *pgxpool.Pool
	n    int
}

// migrated opens a store on a fresh schema, migrated, and a pool on it.
func migrated(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	dsn := testdb.DSN(t)
	s, err := store.Open(context.Background(), dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, testdb.Pool(t, dsn)
}

func (s *seeder) id() string {
	s.n++
	return fmt.Sprintf("seed-%d", s.n)
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s *seeder) exec(sql string, args ...any) {
	s.t.Helper()
	if _, err := s.pool.Exec(context.Background(), sql, args...); err != nil {
		s.t.Fatalf("seed: %v\n%s", err, sql)
	}
}

// pr inserts a Bitbucket PR event. author is the PR author on lifecycle
// events and the actor on reviewer events, as the parser stores them.
type prEvent struct {
	teamName, event, repo string
	id                    int64
	author, branch        string
	automation            string
	draft                 bool
	mergeCommit           string
	title                 string
	jira                  []string
	at                    time.Time
}

func (s *seeder) pr(e prEvent) {
	s.t.Helper()
	if e.teamName == "" {
		e.teamName = team
	}
	if e.jira == nil {
		e.jira = []string{}
	}
	// Every kasse-api PR event carries the same source-branch head: a SHA
	// many Bitbucket rows share, which must not multiply the run built from
	// it.
	var head *string
	if e.repo == kasseAPI {
		h := sha("1")
		head = &h
	}
	pull := map[string]any{"id": e.id, "draft": e.draft, "fromRef": map[string]any{"displayId": e.branch, "latestCommit": head}}
	if e.title != "" {
		pull["title"] = e.title
	}
	if e.mergeCommit != "" {
		pull["properties"] = map[string]any{"mergeCommit": map[string]any{"id": e.mergeCommit}}
	}
	payload := jsonString(map[string]any{"eventKey": e.event, "pullRequest": pull})
	var automation, changeType *string
	if e.automation != "" {
		automation = &e.automation
	}
	if e.event == "pr:opened" || e.event == "pr:merged" {
		ct := "other"
		if b, _, ok := strings.Cut(e.branch, "/"); ok && (b == "feature" || b == "bugfix") {
			ct = b
		}
		changeType = &ct
	}
	s.exec(`INSERT INTO bitbucket_events (delivery_id, event_type, repo_full_name, pr_id, commit_sha, author,
		branch_name, change_type, jira_keys, automation_source, occurred_at, team, payload)
		VALUES ($1, $2, $3, $4, $5, $6, lower($7), $8, $9, $10, $11, $12, $13::jsonb)`,
		s.id(), e.event, e.repo, e.id, head, e.author, e.branch, changeType, e.jira, automation, e.at, e.teamName, payload)
}

type commit struct {
	sha, author, kind, message string
	authored                   time.Time
	merge                      bool
}

// push inserts a repo:refs_changed whose tip is the last commit.
func (s *seeder) push(repo, branch string, at time.Time, commits ...commit) {
	s.t.Helper()
	cs := make([]any, len(commits))
	for i, c := range commits {
		parents := []any{map[string]any{"id": sha("0")}}
		if c.merge {
			parents = append(parents, map[string]any{"id": sha("0")})
		}
		author, kind := c.author, c.kind
		if author == "" {
			author = "dev-anna"
		}
		if kind == "" {
			kind = "NORMAL"
		}
		ms := c.authored.UnixMilli()
		cs[i] = map[string]any{
			"id": c.sha, "message": c.message, "authorTimestamp": ms, "committerTimestamp": ms,
			"author": map[string]any{"name": author, "displayName": author, "type": kind}, "parents": parents,
		}
	}
	payload := jsonString(map[string]any{"eventKey": "repo:refs_changed", "commits": cs})
	s.exec(`INSERT INTO bitbucket_events (delivery_id, event_type, repo_full_name, branch_name, commit_sha, author,
		occurred_at, team, jira_keys, payload)
		VALUES ($1, 'repo:refs_changed', $2, $3, $4, 'dev-anna', $5, $6, '{}', $7::jsonb)`,
		s.id(), repo, branch, commits[len(commits)-1].sha, at, team, payload)
}

func (s *seeder) deploy(teamName, app, env, rev, phase string, at time.Time, images ...string) {
	s.t.Helper()
	if images == nil {
		images = []string{}
	}
	payload := jsonString(map[string]any{"app_name": app, "images": images})
	s.exec(`INSERT INTO argocd_events (delivery_id, app_name, revision, operation_phase, environment,
		started_at, finished_at, occurred_at, team, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8, $9::jsonb)`,
		s.id(), app, rev, phase, env, at.Add(-45*time.Second), at, teamName, payload)
}

type run struct {
	teamName, pipeline, runID, phase, status, commit, buildType, actor string
	started                                                            time.Time
	images                                                             []string
}

func (s *seeder) run(r run) {
	s.t.Helper()
	if r.teamName == "" {
		r.teamName = team
	}
	if r.phase == "" {
		r.phase = "COMPLETED"
	}
	if r.images == nil {
		r.images = []string{}
	}
	var buildType, actor, kind, status *string
	if r.buildType != "" {
		buildType = &r.buildType
	}
	if r.actor != "" {
		k := "service"
		actor, kind = &r.actor, &k
	}
	if r.status != "" {
		status = &r.status
	}
	finished := r.started.Add(10 * time.Minute)
	s.exec(`INSERT INTO pipeline_events (delivery_id, source, pipeline_name, run_id, phase, status, commit_sha,
		image_refs, actor_handle, actor_account_kind, build_type, started_at, finished_at, occurred_at, team, payload)
		VALUES ($1, 'jenkins', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12, $13, '{}')`,
		s.id(), r.pipeline, r.runID, r.phase, status, r.commit, r.images, actor, kind, buildType,
		r.started, finished, r.teamName)
}

func (s *seeder) rollup(teamName, prKey, outcome string, closed time.Time) {
	s.t.Helper()
	s.exec(`INSERT INTO noergler_events (delivery_id, event_type, pr_key, repo, outcome, reviewer_handle,
		reviewer_account_kind, lines_added, lines_removed, files_changed, total_runs, findings_count, cost_usd,
		occurred_at, team, payload)
		VALUES ($1, 'pr_completed', $2::varchar, split_part($2::text, '#', 1), $3, 'noergler', 'bot', 10, 2, 3, 2, 4, 0.123456,
		        $4, $5, '{}')`,
		s.id(), prKey, outcome, closed, teamName)
}

// seed writes the timeline above.
func seed(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	s := &seeder{t: t, pool: pool}

	// PR #1: opened and merged before the window.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 1, author: "dev-anna", branch: "feature/kas-1", at: days(-20)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 1, author: "dev-anna", branch: "feature/kas-1", at: days(-19)})

	// PR #5: opened as a draft before the window, merged in it.
	s.push(kasseAPI, "feature/kas-5-refund", days(-4), commit{sha: sha("6"), authored: days(-4).Add(-2 * time.Hour)})
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 5, author: "dev-anna", branch: "feature/KAS-5-refund", draft: true,
		jira: []string{"KAS-5"}, at: days(-3)})
	s.pr(prEvent{event: "pr:comment:added", repo: kasseAPI, id: 5, author: "rita", at: days(-3).Add(2 * time.Hour)}) // in draft: not pickup
	s.pr(prEvent{event: "pr:ready_for_review", repo: kasseAPI, id: 5, author: "dev-anna", at: days(-2)})
	s.pr(prEvent{event: "pr:reviewer:needs_work", repo: kasseAPI, id: 5, author: "rita", at: days(-1)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 5, author: "rita", at: days(2)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 5, author: "dev-anna", branch: "feature/KAS-5-refund",
		mergeCommit: sha("5"), jira: []string{"KAS-5", "KAS-6"}, at: days(2).Add(time.Hour)})

	// PR #7: the full chain, approval as the reviewer's first touch.
	s.push(kasseAPI, "feature/kas-7-pay", hours(20), commit{sha: sha("1"), authored: hours(19)})
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 7, author: "dev-anna", branch: "feature/KAS-7-pay",
		title: "Pay with card", jira: []string{"KAS-7"}, at: days(1)})
	s.pr(prEvent{event: "pr:comment:added", repo: kasseAPI, id: 7, author: "noergler", automation: "noergler",
		at: days(1).Add(time.Hour)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 7, author: "rita", at: days(1).Add(3 * time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 7, author: "dev-anna", branch: "feature/KAS-7-pay",
		mergeCommit: sha("2"), title: "Pay by card", jira: []string{"KAS-7"}, at: days(1).Add(5 * time.Hour)})

	// Master history of kasse-api and the release that ships it.
	s.push(kasseAPI, "master", days(-12), commit{sha: sha("a"), authored: days(-12), message: "previous release head"})
	s.push(kasseAPI, "master", days(1).Add(5*time.Hour),
		commit{sha: sha("1"), authored: hours(19)},
		commit{sha: sha("3"), author: "renovate-bot", authored: days(1).Add(4 * time.Hour), message: "update lib"},
		// bot-shaped, in no config: the ingest heuristic still catches it
		commit{sha: sha("f"), author: "deploy-bot", authored: days(1).Add(4*time.Hour + 30*time.Minute), message: "bump chart"},
		// an account the config names only since after ingest
		commit{sha: strings.Repeat("c1", 20), author: "ci-helper", authored: days(1).Add(4*time.Hour + 40*time.Minute), message: "sync docs"},
		commit{sha: sha("2"), authored: days(1).Add(5 * time.Hour), merge: true, message: "Pull request #7: pay"})
	s.push(kasseAPI, "master", days(1).Add(6*time.Hour),
		commit{sha: sha("4"), author: "ci-release", kind: "SERVICE", authored: days(1).Add(6 * time.Hour),
			message: "[maven-release-plugin] prepare release kasse-api-1.1"})
	s.push(kasseAPI, "master", days(2).Add(time.Hour),
		commit{sha: sha("5"), authored: days(2).Add(time.Hour), merge: true, message: "Pull request #5: refund"})

	release := "kasse PROD 1.1\n\n[kasse-api 1.0 -> 1.1](https://git.example.com/projects/kasse/repos/kasse-api/compare/diff?targetBranch=" +
		sha("a") + "&sourceBranch=" + sha("4") + ")"
	s.push(kasseOps, "master", days(-11), commit{sha: sha("b"), authored: days(-11), message: "kasse 1.0"})
	s.push(kasseOps, "master", days(2).Add(-time.Hour), commit{sha: sha("c"), authored: days(2).Add(-time.Hour), message: release})
	s.push(kasseOps, "master", days(4).Add(-time.Hour), commit{sha: sha("d"), authored: days(4).Add(-time.Hour), message: "kasse config"})
	s.push(kasseOps, "master", days(5).Add(-time.Hour), commit{sha: sha("e"), authored: days(5).Add(-time.Hour), message: "kasse 1.2"})
	s.push(portalOps, "master", days(6).Add(-time.Hour), commit{sha: sha("7"), authored: days(6).Add(-time.Hour), message: "portal 2.0"})

	// PR #3: a stage promotion in the GitOps repo.
	s.pr(prEvent{event: "pr:opened", repo: kasseOps, id: 3, author: "dev-anna", branch: "kasse_PROD_1.1", at: days(2).Add(-2 * time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: kasseOps, id: 3, author: "dev-anna", branch: "kasse_PROD_1.1", at: days(2).Add(-time.Hour)})
	// PR #9: a bot's, still open.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 9, author: "renovate-bot", automation: "renovate",
		branch: "renovate/lib-2", at: days(5)})
	// PR #11: by an account ingest took for a person; the config names it
	// now. Still open.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 11, author: "ci-helper", branch: "chore/docs", at: days(4)})
	// PR #2: opened by an account a sender declared a service, declined.
	s.pr(prEvent{event: "pr:opened", repo: portalWeb, id: 2, author: "ci-runner", branch: "feature/portal-2", at: days(6)})
	s.pr(prEvent{event: "pr:declined", repo: portalWeb, id: 2, author: "ci-runner", branch: "feature/portal-2", at: days(6).Add(time.Hour)})
	// The other team's PR, in the window.
	s.pr(prEvent{teamName: otherTeam, event: "pr:opened", repo: ledger, id: 4, author: "dev-otto", branch: "feature/l-4", at: days(3)})

	// Deploys.
	s.deploy(team, "kasse-prod", "prod", sha("b"), "Succeeded", days(-10), "reg/kasse-api:1.0")
	s.deploy(team, "kasse-intg", "intg", sha("c"), "Succeeded", days(2), "reg/kasse-api:1.1")
	s.deploy(team, "kasse-prod", "prod", sha("c"), "Succeeded", days(3), "reg/kasse-api:1.1")
	s.deploy(team, "kasse-prod", "prod", sha("d"), "Succeeded", days(4), "reg/kasse-api:1.1")
	s.deploy(team, "kasse-prod", "prod", sha("e"), "Failed", days(5), "reg/kasse-api:1.2")
	s.deploy(team, "portal-prod", "prod", sha("7"), "Succeeded", days(6), "reg/portal-web:2.0")
	s.deploy(team, "atlas-prod", "prod", sha("8"), "Succeeded", days(7), "reg/atlas:1")
	s.deploy(team, "kasse-prod", "prod", sha("d"), "Running", days(8), "reg/kasse-api:1.1")
	s.deploy(team, "kasse-prod", "prod", sha("c"), "Succeeded", days(30), "reg/kasse-api:1.1") // after the window
	s.deploy(otherTeam, "ledger-prod", "prod", sha("9"), "Succeeded", days(3), "reg/ledger:1")

	// CI runs.
	s.run(run{pipeline: "kasse-api/master", runID: "10", status: "SUCCESS", commit: sha("4"), buildType: "release",
		started: days(1).Add(6 * time.Hour), images: []string{"reg/kasse-api:1.1"}})
	s.run(run{pipeline: "kasse-api/feature%2Fkas-7-pay", runID: "3", status: "SUCCESS", commit: sha("1"), buildType: "build", started: hours(20)})
	s.run(run{pipeline: "kasse-api/PR-7", runID: "1", status: "FAILURE", commit: sha("1"), started: days(1).Add(time.Hour)})
	s.run(run{pipeline: "kasse-api/release%2F1.1", runID: "2", status: "SUCCESS", commit: sha("4"), buildType: "hotfix", started: days(3)})
	s.run(run{pipeline: "nightly", runID: "1", status: "SUCCESS", commit: sha("9"), started: days(4)})
	s.run(run{pipeline: "kasse-api/master", runID: "11", phase: "STARTED", commit: sha("5"), actor: "ci-runner", started: days(9)})
	s.run(run{pipeline: "kasse-api/master", runID: "9", status: "SUCCESS", commit: sha("a"), started: days(-5)})
	s.run(run{teamName: otherTeam, pipeline: "ledger-core/master", runID: "1", status: "SUCCESS", commit: sha("9"), started: days(3)})

	// Noergler rollups.
	s.rollup(team, kasseAPI+"#7", "merged", days(1).Add(5*time.Hour))
	s.rollup(team, kasseAPI+"#1", "merged", days(-19))
	s.rollup(otherTeam, ledger+"#4", "declined", days(3))
}
