package store

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/trick77/ship/kit/migrate"

	"github.com/trick77/riptide/internal/parse"
	"github.com/trick77/riptide/internal/testdb"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// testStore opens a migrated store on a fresh schema, plus a raw pool on the
// same schema for assertions.
func testStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	dsn := testdb.DSN(t)
	ctx := context.Background()
	s, err := Open(ctx, dsn, quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s, testdb.Pool(t, dsn)
}

func TestMigrateIsIdempotentAndSchemaCurrent(t *testing.T) {
	dsn := testdb.DSN(t)
	ctx := context.Background()
	s, err := Open(ctx, dsn, quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SchemaCurrent(ctx); err == nil || !strings.Contains(err.Error(), "riptide migrate") {
		t.Fatalf("unmigrated schema reported current: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if err := s.SchemaCurrent(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != len(files) {
		t.Fatalf("schema_migrations rows = %d, want %d, %v", n, len(files), err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if err := s.SchemaCurrent(ctx); err == nil || !strings.Contains(err.Error(), "behind") {
		t.Fatalf("behind schema reported current: %v", err)
	}
	// Readiness is the database only: serve refuses a behind schema at boot.
	checks := s.ReadyChecks()
	if len(checks) != 1 || checks[0].Name != "database" || checks[0].Run(ctx) != nil {
		t.Fatalf("ready checks: %+v", checks)
	}
}

// A database the hand-rolled migrator left: kit/migrate keeps its
// schema_migrations table, so the switch applies nothing.
func TestMigrateAdoptsTheExistingBookkeeping(t *testing.T) {
	dsn := testdb.DSN(t)
	ctx := context.Background()
	pool := testdb.Pool(t, dsn)
	if _, err := pool.Exec(ctx, `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		body, err := migrationFS.ReadFile("migrations/" + f.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			t.Fatalf("%s: %v", f.Name(), err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, f.Name()); err != nil {
			t.Fatal(err)
		}
	}
	var logs strings.Builder
	s, err := Open(ctx, dsn, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "migration_applied") {
		t.Errorf("an applied migration ran again: %s", logs.String())
	}
	if err := s.SchemaCurrent(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateLogsWhatItApplies(t *testing.T) {
	var logs strings.Builder
	s, err := Open(context.Background(), testdb.DSN(t), slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(logs.String(), "msg=migration_applied migration="); n != len(files) {
		t.Errorf("migration_applied lines = %d, want %d: %s", n, len(files), logs.String())
	}
}

// kit's per-file line goes out under riptide's name as it commits; every
// other record passes through as kit wrote it.
func TestMigrationLogRenamesKitsLine(t *testing.T) {
	var logs strings.Builder
	log := slog.New(migrationLog{slog.NewTextHandler(&logs, nil)}).With("run", 1).WithGroup("g")
	log.Info("Migration applied", "file", "0001_init.sql", "took", "1ms")
	log.Info("Adopted goose migrations", "file", "kept")
	got := logs.String()
	if !strings.Contains(got, "msg=migration_applied run=1 g.migration=0001_init.sql g.took=1ms") {
		t.Errorf("renamed line: %s", got)
	}
	if !strings.Contains(got, `msg="Adopted goose migrations" run=1 g.file=kept`) {
		t.Errorf("passthrough line: %s", got)
	}
}

// A wait past LockTimeout names the advisory key to look for in pg_locks.
func TestMigrateLockTimeoutNamesTheKey(t *testing.T) {
	dsn := testdb.DSN(t)
	ctx := context.Background()
	// Advisory locks are database-wide: hold it on a connection of its own,
	// as another replica's migrate would.
	holder, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Close(ctx) }()
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrateLockID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = holder.Exec(ctx, `SELECT pg_advisory_unlock($1)`, migrateLockID) }()
	s, err := Open(ctx, dsn, quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	prev := migrateLockTimeout
	migrateLockTimeout = 100 * time.Millisecond
	defer func() { migrateLockTimeout = prev }()
	err = s.Migrate(ctx)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) ||
		!strings.HasPrefix(err.Error(), "migration lock key 0x72697074 (objid 1919512692 in pg_locks): timed out after 100ms") ||
		strings.Count(err.Error(), "timed out") != 1 {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenRejectsABadDSN(t *testing.T) {
	if _, err := Open(context.Background(), "postgres://%zz", quiet()); err == nil {
		t.Fatal("bad DSN accepted")
	}
}

func TestCountersAreBigint(t *testing.T) {
	_, pool := testStore(t)
	rows, err := pool.Query(context.Background(), `
		SELECT table_name || '.' || column_name, data_type FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND column_name IN ('pr_id','lines_added','lines_removed','files_changed','total_runs',
		                      'prompt_tokens','completion_tokens','elapsed_ms','findings_count')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var col, typ string
		if err := rows.Scan(&col, &typ); err != nil {
			t.Fatal(err)
		}
		n++
		if typ != "bigint" {
			t.Errorf("%s is %s", col, typ)
		}
	}
	// 12 on the tables, 8 passed through noergler_pr_rollups.
	if n != 20 {
		t.Errorf("found %d counter columns, want 20", n)
	}
}

func ptr[T any](v T) *T { return &v }

// A declined PR reopened and merged emits again, and each rollup is
// cumulative. Summed over noergler_events, declined-then-merged counted the
// declined spend twice; the view keeps only the newest row per PR.
func TestNoerglerPRRollupsKeepTheNewestPerPR(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	rollup := func(key, outcome, cost string, at time.Time) {
		t.Helper()
		lines := int64(100)
		if outcome == "deleted" {
			lines = 0
		}
		d := &parse.NoerglerDraft{
			DeliveryID: "pr_completed#" + key + "#" + outcome, EventType: "pr_completed", PRKey: key,
			Outcome: ptr(outcome), CostUSD: ptr(cost), LinesAdded: ptr(lines), OccurredAt: at, Payload: []byte(`{}`),
		}
		if _, err := s.InsertNoergler(ctx, d, "checkout"); err != nil {
			t.Fatal(err)
		}
	}
	rollup("a#1", "declined", "1.000000", t0)
	rollup("a#1", "merged", "1.500000", t0.Add(time.Hour))
	// c#3's older, costlier rollup arrives last: newest is by emit time,
	// not arrival order, cost or outcome name.
	rollup("c#3", "merged", "0.500000", t0.Add(time.Hour))
	rollup("c#3", "declined", "0.900000", t0)
	rollup("b#2", "merged", "2.000000", t0)
	// Same emit time: the later arrival wins.
	rollup("d#4", "declined", "0.100000", t0)
	rollup("d#4", "merged", "0.200000", t0)
	// Deleted after declined carries 0 lines: the declined row keeps its size.
	rollup("e#5", "declined", "0.300000", t0)
	rollup("e#5", "deleted", "0.300000", t0.Add(time.Hour))

	got := map[string]string{}
	rs, err := pool.Query(ctx, `SELECT pr_key, outcome || ' ' || cost_usd::text || ' ' || lines_added
		FROM noergler_pr_rollups`)
	if err != nil {
		t.Fatal(err)
	}
	for rs.Next() {
		var k, v string
		if err := rs.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		got[k] = v
	}
	if err := rs.Err(); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"a#1": "merged 1.500000 100", "b#2": "merged 2.000000 100", "c#3": "merged 0.500000 100",
		"d#4": "merged 0.200000 100", "e#5": "declined 0.300000 100",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rollups = %v, want %v", got, want)
	}
}

func TestInsertsDedupeOnDeliveryID(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	now := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)

	bb := &parse.BitbucketDraft{
		DeliveryID: "bb-1", EventType: "pr:merged", RepoFullName: ptr("acme/payments-api"), PRID: ptr(int64(42)),
		CommitSHA: ptr("abc1234"), Author: ptr("alice"), BranchName: ptr("feature/x"), ChangeType: ptr("feature"),
		JiraKeys: []string{"ABC-1"}, OccurredAt: now, Payload: []byte(`{"a":1}`),
	}
	pl := &parse.PipelineDraft{
		DeliveryID: "jenkins#p#1#COMPLETED", Source: "jenkins", PipelineName: "p", RunID: "1", Phase: "COMPLETED",
		CommitSHA: "abc1234", ImageRefs: []string{"reg/acme/p:1.0"}, StartedAt: now, FinishedAt: ptr(now.Add(210 * time.Second)),
		OccurredAt: now, Payload: []byte(`{}`),
	}
	ar := &parse.ArgoCDDraft{
		DeliveryID: "app#rev", AppName: "app", Revision: "abc1234", StartedAt: ptr(now), FinishedAt: ptr(now.Add(45 * time.Second)),
		OccurredAt: now, Payload: []byte(`{"images":[]}`),
	}
	no := &parse.NoerglerDraft{
		DeliveryID: "pr_completed#k#merged", EventType: "pr_completed", PRKey: "k", OccurredAt: now,
		ModelsUsed: []string{"m"}, CostUSD: ptr("1.234567"), PromptTokens: ptr(int64(5_000_000_000)), Payload: []byte(`{}`),
	}
	for name, insert := range map[string]func() (bool, error){
		"bitbucket": func() (bool, error) { return s.InsertBitbucket(ctx, bb, "renovate", "checkout") },
		"pipeline":  func() (bool, error) { return s.InsertPipeline(ctx, pl, "checkout") },
		"argocd":    func() (bool, error) { return s.InsertArgoCD(ctx, ar, "checkout") },
		"noergler":  func() (bool, error) { return s.InsertNoergler(ctx, no, "checkout") },
	} {
		first, err := insert()
		if err != nil || !first {
			t.Fatalf("%s first insert = %v, %v", name, first, err)
		}
		second, err := insert()
		if err != nil || second {
			t.Fatalf("%s second insert = %v, %v", name, second, err)
		}
	}

	var automated bool
	var source *string
	if err := pool.QueryRow(ctx, `SELECT is_automated, automation_source FROM bitbucket_events`).Scan(&automated, &source); err != nil {
		t.Fatal(err)
	}
	if !automated || *source != "renovate" {
		t.Errorf("automation = %v %v", automated, source)
	}
	var dur int
	if err := pool.QueryRow(ctx, `SELECT duration_seconds FROM pipeline_events`).Scan(&dur); err != nil || dur != 210 {
		t.Errorf("pipeline duration = %d, %v", dur, err)
	}
	if err := pool.QueryRow(ctx, `SELECT duration_seconds FROM argocd_events`).Scan(&dur); err != nil || dur != 45 {
		t.Errorf("argocd duration = %d, %v", dur, err)
	}
	var cost string
	var tokens int64
	if err := pool.QueryRow(ctx, `SELECT cost_usd::text, prompt_tokens FROM noergler_events`).Scan(&cost, &tokens); err != nil {
		t.Fatal(err)
	}
	if cost != "1.234567" || tokens != 5_000_000_000 {
		t.Errorf("cost/tokens = %s/%d", cost, tokens)
	}

	// A human author: no automation source, is_automated false.
	bb2 := *bb
	bb2.DeliveryID = "bb-2"
	if _, err := s.InsertBitbucket(ctx, &bb2, "", "checkout"); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT is_automated FROM bitbucket_events WHERE delivery_id = 'bb-2'`).Scan(&automated); err != nil || automated {
		t.Errorf("human row automated = %v, %v", automated, err)
	}
}

// 0003 folds image_ref and an image_refs a pre-0003 collector kept only in the
// payload into the new column, without touching modified_at, and leaves
// image_ref in place for old pods mid-rollout and for a rollback.
func TestMigrationImageRefsBackfill(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t, testdb.DSN(t))
	apply := func(name string) {
		t.Helper()
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	apply("0001_initial.sql")
	apply("0002_noergler_pr_rollups.sql")
	if _, err := pool.Exec(ctx, `INSERT INTO pipeline_events
		(delivery_id, source, pipeline_name, run_id, phase, occurred_at, modified_at, image_ref, payload) VALUES
		('scalar', 'jenkins', 'p', '1', 'COMPLETED', now(), '2000-01-01', ' reg/a:1 ', '{}'),
		('both',   'jenkins', 'p', '2', 'COMPLETED', now(), '2000-01-01', 'reg/a:1',
		 '{"image_refs": ["reg/b:1", " reg/a:1", "", 3, "reg/c:1", "reg/b:1"]}'),
		('none',   'jenkins', 'p', '3', 'COMPLETED', now(), '2000-01-01', NULL, '{"image_refs": "reg/x:1"}')`); err != nil {
		t.Fatal(err)
	}
	apply("0003_pipeline_image_refs.sql")

	for id, want := range map[string]string{"scalar": "reg/a:1", "both": "reg/a:1,reg/b:1,reg/c:1", "none": ""} {
		var refs []string
		var untouched bool
		if err := pool.QueryRow(ctx, `SELECT image_refs, modified_at = '2000-01-01' FROM pipeline_events WHERE delivery_id = $1`, id).
			Scan(&refs, &untouched); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(refs, ","); got != want || !untouched {
			t.Errorf("%s: image_refs = %q (want %q), modified_at untouched = %v", id, got, want, untouched)
		}
	}
	// Old pods mid-rollout (or after a rollback) still write image_ref, or keep
	// a sent image_refs only in the payload: the insert trigger fills the
	// column for them. A row that already carries image_refs is left alone.
	if _, err := pool.Exec(ctx, `INSERT INTO pipeline_events (delivery_id, source, pipeline_name, run_id, phase, occurred_at, image_ref, payload) VALUES
		('old-scalar',  'jenkins', 'p', '4', 'COMPLETED', now(), 'reg/d:1', '{}'),
		('old-payload', 'jenkins', 'p', '5', 'COMPLETED', now(), NULL, '{"image_refs": ["reg/e:1", "reg/f:1"]}')`); err != nil {
		t.Fatalf("old-pod insert: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pipeline_events (delivery_id, source, pipeline_name, run_id, phase, occurred_at, image_refs, payload)
		VALUES ('new-pod', 'jenkins', 'p', '6', 'COMPLETED', now(), '{reg/g:1}', '{"image_refs": ["reg/other:1"]}')`); err != nil {
		t.Fatalf("new-pod insert: %v", err)
	}
	for id, want := range map[string]string{"old-scalar": "reg/d:1", "old-payload": "reg/e:1,reg/f:1", "new-pod": "reg/g:1"} {
		var refs []string
		if err := pool.QueryRow(ctx, `SELECT image_refs FROM pipeline_events WHERE delivery_id = $1`, id).Scan(&refs); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(refs, ","); got != want {
			t.Errorf("%s: image_refs = %q, want %q", id, got, want)
		}
	}
	var enabled string
	if err := pool.QueryRow(ctx, `SELECT tgenabled FROM pg_trigger
		WHERE tgname = 'trg_pipeline_events_modified_at' AND tgrelid = 'pipeline_events'::regclass`).Scan(&enabled); err != nil || enabled != "O" {
		t.Errorf("modified_at trigger state = %q, %v", enabled, err)
	}
}

// 0004 copies a build_type a pre-0004 collector kept only in the payload into
// the new column by the parser's rules, without touching modified_at, and its
// insert trigger fills it for old pods mid-rollout.
func TestMigrationBuildTypeBackfill(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t, testdb.DSN(t))
	apply := func(name string) {
		t.Helper()
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	apply("0001_initial.sql")
	apply("0002_noergler_pr_rollups.sql")
	apply("0003_pipeline_image_refs.sql")
	if _, err := pool.Exec(ctx, `INSERT INTO pipeline_events
		(delivery_id, source, pipeline_name, run_id, phase, occurred_at, modified_at, payload) VALUES
		('release', 'jenkins', 'p', '1', 'COMPLETED', now(), '2000-01-01', '{"build_type": " Release "}'),
		('blank',   'jenkins', 'p', '2', 'COMPLETED', now(), '2000-01-01', '{"build_type": "  "}'),
		('number',  'jenkins', 'p', '3', 'COMPLETED', now(), '2000-01-01', '{"build_type": 3}'),
		('none',    'jenkins', 'p', '4', 'COMPLETED', now(), '2000-01-01', '{}'),
		('newline', 'jenkins', 'p', '7', 'COMPLETED', now(), '2000-01-01', '{"build_type": "release\n"}'),
		('tab',     'jenkins', 'p', '8', 'COMPLETED', now(), '2000-01-01', '{"build_type": "\tHotfix"}'),
		('ws-only', 'jenkins', 'p', '9', 'COMPLETED', now(), '2000-01-01', '{"build_type": "\n\t "}')`); err != nil {
		t.Fatal(err)
	}
	apply("0004_pipeline_build_type.sql")

	check := func(backfilled bool, want map[string]string) {
		t.Helper()
		for id, w := range want {
			var got *string
			var untouched bool
			if err := pool.QueryRow(ctx, `SELECT build_type, modified_at = '2000-01-01' FROM pipeline_events WHERE delivery_id = $1`, id).
				Scan(&got, &untouched); err != nil {
				t.Fatal(err)
			}
			g := "<nil>"
			if got != nil {
				g = *got
			}
			if g != w {
				t.Errorf("%s: build_type = %q, want %q", id, g, w)
			}
			if backfilled && !untouched {
				t.Errorf("%s: modified_at bumped by the backfill", id)
			}
		}
	}
	// Trimmed like Go's strings.TrimSpace: tabs and newlines too, not only spaces.
	check(true, map[string]string{"release": "release", "blank": "<nil>", "number": "<nil>", "none": "<nil>",
		"newline": "release", "tab": "hotfix", "ws-only": "<nil>"})

	if _, err := pool.Exec(ctx, `INSERT INTO pipeline_events (delivery_id, source, pipeline_name, run_id, phase, occurred_at, payload) VALUES
		('old-pod', 'jenkins', 'p', '5', 'COMPLETED', now(), '{"build_type": "HOTFIX"}'),
		('old-pod-tab', 'jenkins', 'p', '10', 'COMPLETED', now(), '{"build_type": "\tHotfix"}'),
		('old-pod-newline', 'jenkins', 'p', '11', 'COMPLETED', now(), '{"build_type": "release\n"}')`); err != nil {
		t.Fatalf("old-pod insert: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pipeline_events (delivery_id, source, pipeline_name, run_id, phase, occurred_at, build_type, payload)
		VALUES ('new-pod', 'jenkins', 'p', '6', 'COMPLETED', now(), 'build', '{"build_type": "other"}')`); err != nil {
		t.Fatalf("new-pod insert: %v", err)
	}
	check(false, map[string]string{"old-pod": "hotfix", "new-pod": "build", "old-pod-tab": "hotfix", "old-pod-newline": "release"})
}

// 0005 builds the read API's indexes inside kit's transaction: CONCURRENTLY
// deadlocks against a second migrator waiting for kit's advisory lock.
func TestReadAPIIndexes(t *testing.T) {
	_, pool := testStore(t)
	body, err := migrationFS.ReadFile("migrations/0005_read_api_indexes.sql")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(string(body), migrate.NoTxMarker) || strings.Contains(string(body), "INDEX CONCURRENTLY") {
		t.Error("0005 must build its indexes in a transaction")
	}
	var valid int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = current_schema()
		WHERE c.relname IN ('ix_bitbucket_events_team_occurred', 'ix_pipeline_events_team_occurred',
		  'ix_argocd_events_team_occurred', 'ix_noergler_events_team_occurred', 'ix_bitbucket_events_repo_pr')
		  AND i.indisvalid`).Scan(&valid); err != nil || valid != 5 {
		t.Errorf("valid read API indexes = %d, %v", valid, err)
	}
}

func TestInsertPipelineBuildType(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	release := "release"
	d := &parse.PipelineDraft{DeliveryID: "x", Source: "jenkins", PipelineName: "p", RunID: "1", Phase: "COMPLETED",
		CommitSHA: "abc1234", BuildType: &release, StartedAt: time.Now(), OccurredAt: time.Now(), Payload: []byte(`{}`)}
	if _, err := s.InsertPipeline(ctx, d, "t"); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := pool.QueryRow(ctx, `SELECT build_type FROM pipeline_events`).Scan(&got); err != nil || got != "release" {
		t.Errorf("build_type = %q, %v", got, err)
	}
}

func TestModifiedAtTrigger(t *testing.T) {
	s, pool := testStore(t)
	ctx := context.Background()
	d := &parse.PipelineDraft{DeliveryID: "x", Source: "jenkins", PipelineName: "p", RunID: "1", Phase: "S",
		CommitSHA: "abc1234", StartedAt: time.Now(), OccurredAt: time.Now(), Payload: []byte(`{}`)} // nil ImageRefs: stored as '{}'
	if _, err := s.InsertPipeline(ctx, d, "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE pipeline_events SET modified_at = '2000-01-01', status = 'fixed'`); err != nil {
		t.Fatal(err)
	}
	var bumped bool
	if err := pool.QueryRow(ctx, `SELECT modified_at > created_at - interval '1 minute' FROM pipeline_events`).Scan(&bumped); err != nil || !bumped {
		t.Errorf("trigger did not set modified_at: %v %v", bumped, err)
	}
}

func TestRecentActivity(t *testing.T) {
	s, _ := testStore(t)
	ctx := context.Background()
	now := time.Now()
	_, _ = s.InsertPipeline(ctx, &parse.PipelineDraft{DeliveryID: "1", Source: "jenkins", PipelineName: "build",
		RunID: "1", Phase: "S", CommitSHA: "abc1234", ImageRefs: []string{}, StartedAt: now, OccurredAt: now, Payload: []byte(`{}`)}, "checkout")
	_, _ = s.InsertPipeline(ctx, &parse.PipelineDraft{DeliveryID: "2", Source: "jenkins", PipelineName: "build",
		RunID: "2", Phase: "S", CommitSHA: "abc1234", ImageRefs: []string{}, StartedAt: now, OccurredAt: now, Payload: []byte(`{}`)}, "checkout")
	_, _ = s.InsertArgoCD(ctx, &parse.ArgoCDDraft{DeliveryID: "a", AppName: "app-prod", Revision: "abc1234",
		OccurredAt: now, Payload: []byte(`{}`)}, "platform")

	all, err := s.RecentActivity(ctx, now.Add(-time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Source != "argocd" || all[1].Identifier != "build" || all[1].Events != 2 {
		t.Errorf("activity = %+v", all)
	}
	mine, err := s.RecentActivity(ctx, now.Add(-time.Hour), "platform")
	if err != nil || len(mine) != 1 || mine[0].Identifier != "app-prod" {
		t.Errorf("platform activity = %+v, %v", mine, err)
	}
	later, _ := s.RecentActivity(ctx, now.Add(time.Hour), "")
	if len(later) != 0 {
		t.Errorf("future window = %+v", later)
	}
}

// --- lead-time views -------------------------------------------------------

var viewBase = time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC)

const (
	appRepo    = "acme/payments-api"
	gitopsRepo = "acme/payments-infra"
)

func sha(c string) string { return strings.Repeat(c, 40) }

type commitSpec struct {
	sha, author, authorType, message string
	at                               time.Time
	parents                          int
}

func commitJSON(c commitSpec) map[string]any {
	if c.author == "" {
		c.author = "alice"
	}
	if c.authorType == "" {
		c.authorType = "NORMAL"
	}
	if c.parents == 0 {
		c.parents = 1
	}
	if c.message == "" {
		c.message = "change"
	}
	parents := make([]any, c.parents)
	for i := range parents {
		parents[i] = map[string]any{"id": sha("0")}
	}
	ms := c.at.UnixMilli()
	return map[string]any{
		"id": c.sha, "message": c.message, "authorTimestamp": ms, "committerTimestamp": ms,
		"author":  map[string]any{"name": c.author, "displayName": c.author, "type": c.authorType},
		"parents": parents,
	}
}

func seedViews(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	push := func(id, repo, branch string, at time.Time, commits ...commitSpec) {
		cs := make([]any, len(commits))
		for i, c := range commits {
			cs[i] = commitJSON(c)
		}
		payload, _ := json.Marshal(map[string]any{"eventKey": "repo:refs_changed", "commits": cs})
		if _, err := pool.Exec(ctx, `INSERT INTO bitbucket_events
			(delivery_id, event_type, repo_full_name, branch_name, commit_sha, occurred_at, team, jira_keys, payload)
			VALUES ($1, 'repo:refs_changed', $2, $3, $4, $5, 'checkout', '{}', $6::jsonb)`,
			id, repo, branch, commits[len(commits)-1].sha, at, string(payload)); err != nil {
			t.Fatal(err)
		}
	}
	deploy := func(id, app, env, rev string, at time.Time) {
		if _, err := pool.Exec(ctx, `INSERT INTO argocd_events
			(delivery_id, app_name, revision, operation_phase, environment, occurred_at, team, payload)
			VALUES ($1, $2, $3, 'Succeeded', $4, $5, 'checkout', '{}')`, id, app, rev, env, at); err != nil {
			t.Fatal(err)
		}
	}
	h := func(n int) time.Time { return viewBase.Add(time.Duration(n) * time.Hour) }
	releaseNote := "Payments PROD 2.0.41\n\n[__payments-api 2.0.37 → 2.0.41__]" +
		"(https://git.example.com/projects/acme/repos/payments-api/compare/diff?targetBranch=" +
		sha("a") + "&sourceBranch=" + sha("d") + ")"

	push("p0", appRepo, "master", h(0), commitSpec{sha: sha("a"), at: h(0), message: "previous release head"})
	push("p1", appRepo, "master", h(2), commitSpec{sha: sha("b"), at: h(1)}, commitSpec{sha: sha("9"), at: h(2)})
	push("p2", appRepo, "master", h(4), commitSpec{sha: sha("c"), at: h(4), parents: 2, message: "Pull request #7: merge"})
	push("p3", appRepo, "master", h(6), commitSpec{sha: sha("d"), at: h(6), author: "ci-service", authorType: "SERVICE",
		message: "[maven-release-plugin] prepare release payments-api-2.0.41"})
	push("p4", appRepo, "feature/x", h(5), commitSpec{sha: sha("e"), at: h(5)})
	push("g1", gitopsRepo, "master", h(7), commitSpec{sha: sha("f"), at: h(7), message: releaseNote})
	deploy("d-intg", "payments-intg", "intg", sha("f"), h(8))
	deploy("d-prod-1", "payments-intranet-prod", "prod", sha("f"), h(10))
	deploy("d-prod-2", "payments-extranet-prod", "prod", sha("f"), h(12))
}

func strings2set(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) map[string]bool {
	t.Helper()
	rows, err := pool.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out[s] = true
	}
	return out
}

func hoursBy(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) map[string]float64 {
	t.Helper()
	rows, err := pool.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var k string
		var v float64
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		out[k] = v
	}
	return out
}

func sameSet(got map[string]bool, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		if !got[w] {
			return false
		}
	}
	return true
}

func TestLeadTimeViews(t *testing.T) {
	_, pool := testStore(t)
	seedViews(t, pool)
	ctx := context.Background()

	t.Run("only master commits are sighted", func(t *testing.T) {
		got := strings2set(t, pool, `SELECT commit_sha FROM commit_sightings WHERE repo_full_name = $1`, appRepo)
		if !sameSet(got, sha("a"), sha("b"), sha("9"), sha("c"), sha("d")) {
			t.Errorf("sightings = %v", got)
		}
	})
	t.Run("classification columns", func(t *testing.T) {
		rows, err := pool.Query(ctx, `SELECT commit_sha, (is_merge, author_is_service_account)::text
			FROM commit_sightings WHERE repo_full_name = $1`, appRepo)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := map[string]string{}
		for rows.Next() {
			var k, v string
			_ = rows.Scan(&k, &v)
			got[k] = v
		}
		if got[sha("c")] != "(t,f)" || got[sha("d")] != "(f,t)" || got[sha("b")] != "(f,f)" {
			t.Errorf("classification = %v", got)
		}
	})
	t.Run("lead time is commit to first deploy", func(t *testing.T) {
		var hours float64
		if err := pool.QueryRow(ctx, `SELECT extract(epoch FROM lead_time)/3600 FROM lead_time_changes
			WHERE environment = 'prod' AND commit_sha = $1`, sha("b")).Scan(&hours); err != nil || hours != 9 {
			t.Errorf("lead time = %v, %v", hours, err)
		}
	})
	t.Run("a second deploy of the release does not recount", func(t *testing.T) {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM lead_time_changes
			WHERE environment = 'prod' AND commit_sha = $1`, sha("b")).Scan(&n); err != nil || n != 1 {
			t.Errorf("count = %d, %v", n, err)
		}
	})
	t.Run("range excludes the previous release head", func(t *testing.T) {
		got := strings2set(t, pool, `SELECT commit_sha FROM lead_time_changes WHERE environment = 'prod'`)
		if !sameSet(got, sha("b"), sha("9"), sha("c"), sha("d")) {
			t.Errorf("prod commits = %v", got)
		}
	})
	t.Run("every commit of a push counts with its own timestamp", func(t *testing.T) {
		got := hoursBy(t, pool, `SELECT commit_sha, extract(epoch FROM lead_time)/3600 FROM lead_time_changes
			WHERE environment = 'prod' AND commit_sha IN ($1, $2)`, sha("b"), sha("9"))
		if got[sha("b")] != 9 || got[sha("9")] != 8 || len(got) != 2 {
			t.Errorf("lead times = %v", got)
		}
	})
	t.Run("environments are measured separately", func(t *testing.T) {
		got := hoursBy(t, pool, `SELECT environment, extract(epoch FROM lead_time)/3600 FROM lead_time_changes
			WHERE commit_sha = $1`, sha("b"))
		if got["intg"] != 7 || got["prod"] != 9 || len(got) != 2 {
			t.Errorf("by environment = %v", got)
		}
	})
	t.Run("default metric filter keeps only real changes", func(t *testing.T) {
		got := strings2set(t, pool, `SELECT commit_sha FROM lead_time_changes
			WHERE environment = 'prod' AND NOT is_merge AND NOT author_is_service_account`)
		if !sameSet(got, sha("b"), sha("9")) {
			t.Errorf("filtered = %v", got)
		}
	})
}
