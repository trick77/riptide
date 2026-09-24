package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/trick77/riptide/internal/store"
	"github.com/trick77/riptide/internal/testdb"
)

// dbHarness runs the handlers over the real store, so the assertions read the
// columns Postgres actually holds.
func dbHarness(t *testing.T) (*harness, *pgxpool.Pool) {
	t.Helper()
	dsn := testdb.DSN(t)
	ctx := context.Background()
	st, err := store.Open(ctx, dsn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return newHarness(t, st), testdb.Pool(t, dsn)
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestE2EBitbucket(t *testing.T) {
	h, pool := dbHarness(t)
	ctx := context.Background()
	expectStatus(t, h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", fixture(t, "bitbucket_pr_merged.json"),
		map[string]string{"X-Request-Id": "uuid-1"}), http.StatusAccepted)
	var team, repo, changeType string
	var keys []string
	var automated bool
	var source, lines *string
	var linesAdded *int64
	if err := pool.QueryRow(ctx, `SELECT team, repo_full_name, change_type, jira_keys, is_automated,
		automation_source, lines_added, payload->>'eventKey' FROM bitbucket_events`).
		Scan(&team, &repo, &changeType, &keys, &automated, &source, &linesAdded, &lines); err != nil {
		t.Fatal(err)
	}
	if team != "checkout" || repo != "acme/payments-api" || changeType != "feature" || automated || source != nil || linesAdded != nil {
		t.Errorf("row = %s %s %s %v %v %v", team, repo, changeType, automated, source, linesAdded)
	}
	if len(keys) < 2 {
		t.Errorf("jira keys = %v", keys)
	}

	expectStatus(t, h.bitbucket(t, "checkout", checkoutBitbucket, "pr:opened", fixture(t, "bitbucket_renovate_pr.json"),
		map[string]string{"X-Request-Id": "uuid-r"}), http.StatusAccepted)
	if n := count(t, pool, `SELECT count(*) FROM bitbucket_events WHERE delivery_id = 'uuid-r' AND is_automated AND automation_source = 'renovate'`); n != 1 {
		t.Error("renovate PR not automated")
	}

	// An unknown repo is recorded; the team comes from the path.
	ghost := fixture(t, "bitbucket_pr_merged.json")
	ghost["repository"] = map[string]any{"slug": "Repo", "project": map[string]any{"key": "GHOST"}}
	expectStatus(t, h.bitbucket(t, "platform", platformBitbucket, "pr:merged", ghost, map[string]string{"X-Request-Id": "ghost-1"}), http.StatusAccepted)
	if n := count(t, pool, `SELECT count(*) FROM bitbucket_events WHERE repo_full_name = 'ghost/repo' AND team = 'platform'`); n != 1 {
		t.Error("unknown repo not recorded lowercased under the caller's team")
	}

	// Redelivery dedupes.
	expectStatus(t, h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", fixture(t, "bitbucket_pr_merged.json"),
		map[string]string{"X-Request-Id": "uuid-1"}), http.StatusAccepted)
	if n := count(t, pool, `SELECT count(*) FROM bitbucket_events`); n != 3 {
		t.Errorf("rows = %d", n)
	}
	if last := h.processed(t)[3]; last["outcome"] != "deduped" {
		t.Errorf("outcome = %v", last["outcome"])
	}

	// A push stores branch and commit; the raw body is the payload.
	expectStatus(t, h.bitbucket(t, "checkout", checkoutBitbucket, "repo:refs_changed", fixture(t, "bitbucket_refs_changed.json"),
		map[string]string{"X-Request-Id": "push-1"}), http.StatusAccepted)
	if n := count(t, pool, `SELECT count(*) FROM bitbucket_events WHERE delivery_id = 'push-1' AND branch_name = 'master'
		AND commit_sha = 'feedfacefeedfacefeedfacefeedfacefeedface' AND author = 'alice' AND NOT is_revert
		AND payload->'changes'->0->>'type' IS NOT NULL`); n != 1 {
		t.Error("push row wrong")
	}
}

func TestE2EPipeline(t *testing.T) {
	h, pool := dbHarness(t)
	ctx := context.Background()
	body := fixture(t, "pipeline_jenkins_completed.json")
	body["commit_sha"] = "ABC1234567890ABC1234567890ABC1234567890A"
	body["image_ref"] = "registry.example.com/acme/payments-api:2.0.41"
	body["actor_handle"] = "ci-service"
	expectStatus(t, h.bearer(t, "/webhooks/pipeline", checkoutJenkins, body), http.StatusAccepted)
	var team, commit, image, handle, kind, status string
	var dur int
	var extra *string
	if err := pool.QueryRow(ctx, `SELECT team, commit_sha, image_ref, actor_handle, actor_account_kind, status,
		duration_seconds, payload->>'commit_sha' FROM pipeline_events`).
		Scan(&team, &commit, &image, &handle, &kind, &status, &dur, &extra); err != nil {
		t.Fatal(err)
	}
	if team != "checkout" || commit != "abc1234567890abc1234567890abc1234567890a" || image != "registry.example.com/acme/payments-api:2.0.41" ||
		handle != "ci-service" || kind != "service" || status != "SUCCESS" || dur != 210 {
		t.Errorf("row = %s %s %s %s %s %s %d", team, commit, image, handle, kind, status, dur)
	}
	// The payload is the raw body: the column is lowercased, the payload is not.
	if *extra != "ABC1234567890ABC1234567890ABC1234567890A" {
		t.Errorf("payload commit = %s", *extra)
	}

	naive := fixture(t, "pipeline_tekton_completed.json")
	naive["started_at"] = "2026-04-28T10:05:00"
	naive["finished_at"] = "2026-04-28T10:06:00"
	naive["image_ref"] = ""
	expectStatus(t, h.bearer(t, "/webhooks/pipeline", checkoutJenkins, naive), http.StatusAccepted)
	if n := count(t, pool, `SELECT count(*) FROM pipeline_events WHERE source = 'tekton' AND duration_seconds = 60
		AND started_at = '2026-04-28T10:05:00Z' AND image_ref IS NULL AND actor_account_kind IS NULL`); n != 1 {
		t.Error("tekton row wrong")
	}
}

func TestE2EArgoCD(t *testing.T) {
	h, pool := dbHarness(t)
	body := fixture(t, "argocd_synced.json")
	body["revision"] = "ABC1234567890ABC1234567890ABC1234567890A"
	body["images"] = []any{"registry.example.com/acme/payments-api:2.0.41", "registry.example.com/acme/sidecar:1.2"}
	expectStatus(t, h.bearer(t, "/webhooks/argocd", checkoutArgoCD, body), http.StatusAccepted)
	if n := count(t, pool, `SELECT count(*) FROM argocd_events WHERE team = 'checkout' AND app_name = 'payments-api-prod'
		AND operation_phase = 'Succeeded' AND duration_seconds = 45 AND destination_namespace = 'payments-prod'
		AND environment = 'prod' AND revision = 'abc1234567890abc1234567890abc1234567890a'
		AND payload->'images' = '["registry.example.com/acme/payments-api:2.0.41", "registry.example.com/acme/sidecar:1.2"]'::jsonb`); n != 1 {
		t.Error("argocd row wrong")
	}
	noNS := fixture(t, "argocd_synced.json")
	delete(noNS, "destination_namespace")
	noNS["images"] = []any{}
	noNS["operation_phase"] = "Running"
	expectStatus(t, h.bearer(t, "/webhooks/argocd", checkoutArgoCD, noNS), http.StatusAccepted)
	if n := count(t, pool, `SELECT count(*) FROM argocd_events WHERE destination_namespace IS NULL AND environment IS NULL
		AND payload->'images' = '[]'::jsonb`); n != 1 {
		t.Error("argocd row without namespace wrong")
	}
}

func TestE2ENoergler(t *testing.T) {
	h, pool := dbHarness(t)
	merged := fixture(t, "noergler_pr_completed_merged.json")
	merged["reviewer_handle"] = "Rop"
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, merged), http.StatusAccepted)
	again := fixture(t, "noergler_pr_completed_merged.json")
	again["total_runs"] = 99
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, again), http.StatusAccepted)
	if n := count(t, pool, `SELECT count(*) FROM noergler_events WHERE event_type = 'pr_completed' AND outcome = 'merged'
		AND pr_key = 'proj/payments-api#42' AND repo = 'acme/payments-api' AND total_runs = 3
		AND reviewer_handle = 'Rop' AND reviewer_account_kind = 'bot' AND cost_usd = 0.3821
		AND models_used = '{gpt-4o-2024-08-06}' AND merge_commit_sha = 'def4567890abc1234567890abc1234567890abcd'
		AND occurred_at = '2026-04-29T18:42:00Z' AND team = 'checkout'`); n != 1 {
		t.Error("rollup row wrong or the redelivery overwrote it")
	}
	for _, f := range []string{"noergler_pr_completed_declined.json", "noergler_pr_completed_deleted.json"} {
		expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, fixture(t, f)), http.StatusAccepted)
	}
	if n := count(t, pool, `SELECT count(*) FROM noergler_events WHERE outcome IN ('declined', 'deleted') AND merge_commit_sha IS NULL`); n != 2 {
		t.Errorf("declined/deleted rows = %d", n)
	}
	unpriced := fixture(t, "noergler_pr_completed_merged.json")
	delete(unpriced, "total_cost_usd")
	unpriced["pr_key"] = "PROJ/other#1"
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, unpriced), http.StatusAccepted)
	if n := count(t, pool, `SELECT count(*) FROM noergler_events WHERE pr_key = 'proj/other#1' AND cost_usd IS NULL`); n != 1 {
		t.Error("unpriced rollup wrong")
	}

	fb := fixture(t, "noergler_feedback.json")
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, fb), http.StatusAccepted)
	fb["verdict"] = "acknowledged"
	delete(fb, "commit_sha")
	expectStatus(t, h.bearer(t, "/webhooks/noergler", checkoutNoergler, fb), http.StatusAccepted)
	if n := count(t, pool, `SELECT count(*) FROM noergler_events WHERE event_type = 'feedback' AND actor = 'alice@example.com'
		AND finding_id = 'finding-2026-04-29-0001' AND models_used IS NULL`); n != 2 {
		t.Errorf("feedback rows = %d", n)
	}
}

func TestE2EUnstorablePayload(t *testing.T) {
	h, pool := dbHarness(t)
	body := strings.Replace(string(jsonBytes(t, fixture(t, "pipeline_jenkins_completed.json"))), `"SUCCESS"`, `"SUCC\u0000ESS"`, 1)
	expectStatus(t, h.bearer(t, "/webhooks/pipeline", checkoutJenkins, body), http.StatusUnprocessableEntity)
	bb := strings.Replace(string(jsonBytes(t, fixture(t, "bitbucket_pr_merged.json"))), `"MERGED"`, `"MER\u0000GED"`, 1)
	w := h.bitbucket(t, "checkout", checkoutBitbucket, "pr:merged", bb, nil)
	expectBody(t, w, `{"status":"ignored","reason":"payload not storable as JSONB"}`)
	if n := count(t, pool, `SELECT (SELECT count(*) FROM pipeline_events) + (SELECT count(*) FROM bitbucket_events)`); n != 0 {
		t.Errorf("rows = %d", n)
	}
}
