package dataset_test

import (
	"context"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
)

// The ingest flags every event on a bot's branch (renovate/…), whoever
// authored it. Who opened the PR decides on read: a person who opens PRs
// from ordinary branches too owns a PR from a bot's branch, its reviews and
// its builds over its life; an account that only ever works on bots'
// branches, the bot's own PR a person pushed to, a declared account and a
// service account stay bots.
func TestDatasetPersonsPRFromBotBranch(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	cfg, err := config.Parse([]byte(`{"automation": {
		"renovate": {"authors": ["Renovate-Bot"], "branch_prefixes": ["renovate/"]},
		"helper": {"authors": ["ci-helper"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	opened := func(id int64, author, branch, automation string, at time.Time) {
		s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: id, author: author, prAuthor: author, branch: branch,
			automation: automation, state: "OPEN", at: at})
	}
	merged := func(id int64, author, branch, automation string, at time.Time) {
		s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: id, author: author, prAuthor: author, branch: branch,
			automation: automation, state: "MERGED", at: at})
	}
	build := func(pipeline, id string, at time.Time) {
		s.run(run{pipeline: pipeline, runID: id, status: "FAILURE", commit: sha("1"), started: at})
	}

	// History: dev-maya opens PRs from ordinary branches; svc-deploy too,
	// but a sender declares it a service; svc-system too, but the git host
	// marks it a service account.
	opened(40, "dev-maya", "feature/kas-40", "", days(-30))
	opened(70, "svc-deploy", "feature/kas-70", "", days(-30))
	s.run(run{pipeline: "nightly", runID: "90", status: "SUCCESS", commit: sha("9"), actor: "svc-deploy", started: days(-1)})
	opened(72, "svc-system", "feature/kas-72", "", days(-30))
	opened(73, "svc-system", "feature/kas-73", "service-account", days(-29))

	// renovate/lib-5: the bot's PR #39, then dev-maya's #41 from the same
	// branch, reviewed by a person and commented by the git host's system
	// user (a SERVICE account the branch's flag hid at ingest).
	opened(39, "renovate-bot", "renovate/lib-5", "renovate", days(0))
	merged(39, "renovate-bot", "renovate/lib-5", "renovate", days(1))
	s.push(kasseAPI, "renovate/lib-5", days(0), commit{sha: sha("51"), authored: days(0), author: "renovate-bot"})
	s.push(kasseAPI, "renovate/lib-5", days(2), commit{sha: sha("52"), authored: days(2), author: "dev-maya"})
	opened(41, "dev-maya", "renovate/lib-5", "renovate", days(3))
	s.pr(prEvent{event: "pr:comment:added", repo: kasseAPI, id: 41, author: "bb-system", prAuthor: "dev-maya",
		automation: "renovate", at: days(3).Add(time.Hour)})
	approvedAt := days(3).Add(3 * time.Hour)
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 41, author: "dev-ben", prAuthor: "dev-maya",
		automation: "renovate", reviewers: map[string]string{"dev-ben": "APPROVED"}, at: approvedAt})
	merged(41, "dev-maya", "renovate/lib-5", "renovate", days(4))
	build("kasse-api/renovate%2Flib-5", "1", days(0.5)) // the bot's #39's
	build("kasse-api/renovate%2Flib-5", "2", days(2))   // dev-maya's, before her PR
	build("kasse-api/PR-41", "1", days(3.5))
	build("kasse-api/renovate%2Flib-5", "3", days(6)) // after #41 closed: the bot's again

	// renovate/lib-6: dev-maya's #42, approved, still open.
	opened(42, "dev-maya", "renovate/lib-6", "renovate", days(5))
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 42, author: "dev-ben", prAuthor: "dev-maya",
		automation: "renovate", reviewers: map[string]string{"dev-ben": "APPROVED"}, state: "OPEN", at: days(6)})
	build("kasse-api/renovate%2Flib-6", "1", days(5.5))
	// renovate/lib-11: dev-maya's #43, open, touched by the system user only,
	// which the PR also lists as approving.
	opened(43, "dev-maya", "renovate/lib-11", "renovate", days(5))
	s.pr(prEvent{event: "pr:comment:added", repo: kasseAPI, id: 43, author: "bb-system", prAuthor: "dev-maya",
		automation: "renovate", state: "OPEN", at: days(5).Add(time.Hour)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 43, author: "bb-system", prAuthor: "dev-maya",
		automation: "renovate", state: "OPEN", reviewers: map[string]string{"bb-system": "APPROVED"},
		types: map[string]string{"bb-system": "SERVICE"}, at: days(5).Add(2 * time.Hour)})
	s.exec(`UPDATE bitbucket_events SET payload = jsonb_set(payload, '{actor}', '{"name": "bb-system", "type": "SERVICE"}')
		WHERE author = 'bb-system'`)

	// An undeclared bot whose first opening from renovate/… predates the
	// prefix in the config (not flagged then): no person.
	opened(55, "deps-old", "renovate/old-1", "", days(-30))
	opened(56, "deps-old", "renovate/old-2", "renovate", days(2))
	merged(56, "deps-old", "renovate/old-2", "renovate", days(2.5))
	build("kasse-api/renovate%2Fold-2", "1", days(2.2))

	// renovate/lib-14: dev-maya's #44, its close never seen; the bot's #45
	// from the same branch later. Her PR's builds end at the bot's opening.
	opened(44, "dev-maya", "renovate/lib-14", "renovate", days(1))
	opened(45, "renovate-bot", "renovate/lib-14", "renovate", days(3))
	build("kasse-api/renovate%2Flib-14", "1", days(2))
	build("kasse-api/renovate%2Flib-14", "2", days(4))

	// renovate/lib-15: the bot's #46 still open when dev-maya opens #47 from
	// it, closed after #47 merged. Builds before #47 are the bot's #46's.
	opened(46, "renovate-bot", "renovate/lib-15", "renovate", days(0))
	opened(47, "dev-maya", "renovate/lib-15", "renovate", days(1))
	merged(47, "dev-maya", "renovate/lib-15", "renovate", days(2))
	s.pr(prEvent{event: "pr:declined", repo: kasseAPI, id: 46, author: "renovate-bot", prAuthor: "renovate-bot",
		branch: "renovate/lib-15", automation: "renovate", state: "DECLINED", at: days(3)})
	build("kasse-api/renovate%2Flib-15", "1", days(0.5))
	build("kasse-api/renovate%2Flib-15", "2", days(1.5))

	// renovate/lib-16: dev-maya's #48, opened before ingestion (no
	// pr:opened stored), pushed to and approved since, still open.
	s.pr(prEvent{event: "pr:from_ref_updated", repo: kasseAPI, id: 48, author: "dev-maya", prAuthor: "dev-maya",
		branch: "renovate/lib-16", automation: "renovate", state: "OPEN", created: days(-40), at: days(5)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 48, author: "dev-ben", prAuthor: "dev-maya",
		branch: "renovate/lib-16", automation: "renovate", reviewers: map[string]string{"dev-ben": "APPROVED"},
		state: "OPEN", at: days(6)})
	build("kasse-api/renovate%2Flib-16", "1", days(5.5))
	build("kasse-api/PR-48", "1", days(6.5))

	// An account in no config, of no bot shape, that only ever opens PRs
	// from bots' branches: the bot nobody declared.
	opened(50, "deps-kasse", "renovate/lib-7", "renovate", days(2))
	merged(50, "deps-kasse", "renovate/lib-7", "renovate", days(2.5))
	build("kasse-api/renovate%2Flib-7", "1", days(2.2))
	build("kasse-api/PR-50", "1", days(2.3))

	// The bot's PR, dev-maya pushed to it and commented.
	opened(60, "renovate-bot", "renovate/lib-8", "renovate", days(2))
	s.push(kasseAPI, "renovate/lib-8", days(2.5), commit{sha: sha("81"), authored: days(2.5), author: "dev-maya"})
	s.pr(prEvent{event: "pr:comment:added", repo: kasseAPI, id: 60, author: "dev-maya", prAuthor: "renovate-bot",
		automation: "renovate", at: days(2.6)})
	merged(60, "renovate-bot", "renovate/lib-8", "renovate", days(3))
	build("kasse-api/PR-60", "1", days(2.7))

	// The declared account's and the service account's PRs from bots'
	// branches.
	opened(71, "svc-deploy", "renovate/lib-10", "renovate", days(2))
	merged(71, "svc-deploy", "renovate/lib-10", "renovate", days(2.5))
	build("kasse-api/renovate%2Flib-10", "1", days(2.2))
	opened(74, "svc-system", "renovate/lib-9", "renovate", days(2))
	merged(74, "svc-system", "renovate/lib-9", "renovate", days(2.5))
	build("kasse-api/renovate%2Flib-9", "1", days(2.2))

	// The ingest flags every push to a bot's branch.
	s.exec(`UPDATE bitbucket_events SET automation_source = 'renovate'
		WHERE event_type = 'repo:refs_changed' AND branch_name LIKE 'renovate/%'`)

	ctx := context.Background()
	sh, err := st.Shared(ctx, dataset.SharedParams{Team: team, From: t0, ClassEnds: []time.Time{days(10)}, Now: days(28),
		States: true, ApprovalsFrom: t0, Automated: cfg.AutomatedAtRead, BotPrefixes: cfg.BotPrefixes()})
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.Dataset(ctx, dataset.Params{Team: team, From: t0, To: days(10), ProductionStage: "prod",
		Automated: cfg.AutomatedAtRead, BotPrefixes: cfg.BotPrefixes(), Now: days(28), Shared: sh, CountsOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	prs := map[int64]dataset.PR{}
	for _, pr := range d.PRs {
		prs[pr.PRID] = pr
	}
	if pr := prs[41]; pr.Bot || !eqTime(pr.FirstReviewAt, &approvedAt) || !eqTime(pr.ApprovedAt, &approvedAt) {
		t.Errorf("#41 bot %v, first review %v, approved %v: want dev-maya's, reviewed and approved by dev-ben",
			pr.Bot, pr.FirstReviewAt, pr.ApprovedAt)
	}
	for _, id := range []int64{39, 50, 56, 60, 71, 74} {
		if pr, ok := prs[id]; !ok || !pr.Bot {
			t.Errorf("#%d = %+v, want a bot's", id, pr)
		}
	}

	open := map[int64]dataset.OpenPR{}
	for _, o := range d.OpenPRs {
		open[o.PRID] = o
	}
	if o, ok := open[42]; !ok || o.Bot || !o.Touched || !o.Approved {
		t.Errorf("open #42 = %+v, want dev-maya's, touched and approved", o)
	}
	if o, ok := open[43]; !ok || o.Bot || o.Touched || o.Approved {
		t.Errorf("open #43 = %+v, want dev-maya's, touched and approved by no person", o)
	}
	if o, ok := open[48]; !ok || o.Bot || !o.Touched || !o.Approved {
		t.Errorf("open #48 = %+v, want dev-maya's, touched and approved", o)
	}

	approvals := map[int64]dataset.Approval{}
	for _, a := range d.Approvals {
		approvals[a.PRID] = a
	}
	if a, ok := approvals[41]; !ok || a.Bot || !eqTime(a.ApprovedAt, &approvedAt) || len(a.Spans) != 1 {
		t.Errorf("approval #41 = %+v, want dev-maya's, approved by dev-ben", a)
	}
	if a, ok := approvals[42]; !ok || a.Bot || a.ApprovedAt == nil {
		t.Errorf("approval #42 = %+v, want dev-maya's, approved", a)
	}
	if a, ok := approvals[43]; !ok || a.Bot || a.ApprovedAt != nil || len(a.Spans) != 0 {
		t.Errorf("approval #43 = %+v, want dev-maya's, approved by no person", a)
	}
	if a, ok := approvals[48]; !ok || a.Bot || !eqTime(a.ApprovedAt, ts(days(6))) {
		t.Errorf("approval #48 = %+v, want dev-maya's, approved on day 6", a)
	}

	bots := map[string]bool{}
	for _, r := range d.Runs {
		bots[r.Branch+"#"+r.RunID] = r.Bot
	}
	want := map[string]bool{
		"renovate/lib-5#1": true, "renovate/lib-5#2": false, "PR-41#1": false, "renovate/lib-5#3": true,
		"renovate/lib-6#1": false,
		"renovate/lib-7#1": true, "PR-50#1": true,
		"PR-60#1":          true,
		"renovate/lib-9#1": true, "renovate/lib-10#1": true,
		"renovate/old-2#1":  true,
		"renovate/lib-14#1": false, "renovate/lib-14#2": true,
		"renovate/lib-15#1": true, "renovate/lib-15#2": false,
		"renovate/lib-16#1": false, "PR-48#1": false,
	}
	for run, bot := range want {
		if got, ok := bots[run]; !ok || got != bot {
			t.Errorf("run %s bot = %v (seen %v), want %v", run, got, ok, bot)
		}
	}
}

// A person's PR from a bot's branch starts its coding time at the oldest
// commit of a push by no bot, else at its opening: the bot's commits on the
// branch before are none of the person's. An ordinary PR keeps the oldest
// commit pushed to its branch, whoever pushed it.
func TestDatasetPersonsPRFromBotBranchCodingTime(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	cfg, err := config.Parse([]byte(`{"automation": {
		"renovate": {"authors": ["Renovate-Bot"], "branch_prefixes": ["renovate/"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	pr := func(event string, id int64, branch, automation string, at time.Time) {
		s.pr(prEvent{event: event, repo: kasseAPI, id: id, author: "dev-maya", prAuthor: "dev-maya", branch: branch,
			automation: automation, at: at})
	}
	// push stores a push by pusher; the seeder's pusher is dev-anna.
	push := func(branch, pusher, id string, at, authored time.Time) {
		s.push(kasseAPI, branch, at, commit{sha: sha(id), authored: authored, author: pusher})
		s.exec(`UPDATE bitbucket_events SET author = $1 WHERE commit_sha = $2`, pusher, sha(id))
	}
	pr("pr:opened", 40, "feature/kas-40", "", days(-30))

	// renovate/lib-12: the bot's commit, the system user's, then dev-maya's.
	push("renovate/lib-12", "renovate-bot", "c1", days(-10), days(-10))
	push("renovate/lib-12", "bb-system", "c2", days(0), days(0))
	s.exec(`UPDATE bitbucket_events SET payload = jsonb_set(payload, '{actor}', '{"name": "bb-system", "type": "SERVICE"}')
		WHERE author = 'bb-system'`)
	push("renovate/lib-12", "dev-maya", "c3", days(1), days(0.5))
	pr("pr:opened", 80, "renovate/lib-12", "renovate", days(2))
	pr("pr:merged", 80, "renovate/lib-12", "renovate", days(3))
	// renovate/lib-13: the bot's commit only.
	push("renovate/lib-13", "renovate-bot", "c4", days(-10), days(-10))
	pr("pr:opened", 81, "renovate/lib-13", "renovate", days(2))
	pr("pr:merged", 81, "renovate/lib-13", "renovate", days(3))
	// An ordinary branch the bot pushed to first.
	push("feature/kas-82", "renovate-bot", "c5", days(-10), days(-10))
	pr("pr:opened", 82, "feature/kas-82", "", days(2))
	pr("pr:merged", 82, "feature/kas-82", "", days(3))
	s.exec(`UPDATE bitbucket_events SET automation_source = 'renovate'
		WHERE event_type = 'repo:refs_changed' AND branch_name LIKE 'renovate/%'`)

	d, err := st.Dataset(context.Background(), dataset.Params{Team: team, From: t0, To: days(10), ProductionStage: "prod",
		Automated: cfg.AutomatedAtRead, BotPrefixes: cfg.BotPrefixes(), Now: days(28), CountsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]time.Time{80: days(0.5), 81: days(2), 82: days(-10)}
	for _, p := range d.PRs {
		if w, ok := want[p.PRID]; ok && (p.Bot || !eqTime(p.FirstCommitAt, &w)) {
			t.Errorf("#%d bot %v, first commit %v, want %v", p.PRID, p.Bot, p.FirstCommitAt, w)
		}
		delete(want, p.PRID)
	}
	if len(want) > 0 {
		t.Errorf("PRs missing: %v", want)
	}
}
