// Package store is the Postgres layer: pool, embedded migrations and every
// query the collector runs. One function per query, positional pgx args.
// Event tables are append-only: INSERT ... ON CONFLICT (delivery_id) DO
// NOTHING, never UPDATE or DELETE.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/migrate"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/parse"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrateLockID is the advisory-lock key held for the whole of Migrate. From
// "ript". Unchanged since before kit/migrate: an old and a new binary
// migrating at once must still serialise on the same key.
const migrateLockID int64 = 0x72697074

// migrateLockTimeout bounds how long one migrate waits for another's.
var migrateLockTimeout = 5 * time.Minute

// Store owns the pool.
type Store struct {
	pool *pgxpool.Pool
	// sqlDB is database/sql over the pool, for kit's migrate.Pending and
	// schemadoc; it holds no connections of its own.
	sqlDB *sql.DB
	log   *slog.Logger
}

// Open connects and checks the connection, logging the server version.
func Open(ctx context.Context, dsn string, log *slog.Logger) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse BACKEND_DB_URL: %w", err)
	}
	cfg.MaxConns = 10
	return openConfig(ctx, cfg, log)
}

func openConfig(ctx context.Context, cfg *pgxpool.Config, log *slog.Logger) (*Store, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	var version string
	if err := pool.QueryRow(ctx, `SELECT version()`).Scan(&version); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	log.Info("postgres_connected", "server_version", version)
	return &Store{pool: pool, sqlDB: stdlib.OpenDBFromPool(pool), log: log}, nil
}

// Close releases the pool.
func (s *Store) Close() {
	_ = s.sqlDB.Close()
	s.pool.Close()
}

// Ping asks the database for a round trip.
func (s *Store) Ping(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `SELECT 1`)
	return err
}

// ReadyChecks are /readyz's: the database answers. Not the schema: serve
// refuses to start on one that is behind, and nothing un-applies a migration
// under a running binary.
func (s *Store) ReadyChecks() []httpx.Check {
	return []httpx.Check{{Name: "database", Run: s.Ping}}
}

// migrations is the embedded migrations directory as kit/migrate reads it.
func migrations() fs.FS {
	sub, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		panic(err) // a constant, valid path into an embedded FS
	}
	return sub
}

// migrateOptions logs kit's per-file line as it commits, under riptide's
// name for it (migrationLog).
func (s *Store) migrateOptions() migrate.Options {
	return migrate.Options{Dialect: migrate.Postgres(migrateLockID), Log: slog.New(migrationLog{s.log.Handler()}), LockTimeout: migrateLockTimeout}
}

// migrationLog renames kit/migrate's "Migration applied" file=<name> to the
// migration_applied migration=<name> operators search for. Every other
// record passes through unchanged.
type migrationLog struct{ slog.Handler }

func (h migrationLog) Handle(ctx context.Context, r slog.Record) error {
	if r.Message != "Migration applied" {
		return h.Handler.Handle(ctx, r)
	}
	out := slog.NewRecord(r.Time, r.Level, "migration_applied", r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "file" {
			a.Key = "migration"
		}
		out.AddAttrs(a)
		return true
	})
	return h.Handler.Handle(ctx, out)
}

func (h migrationLog) WithAttrs(as []slog.Attr) slog.Handler {
	return migrationLog{h.Handler.WithAttrs(as)}
}

func (h migrationLog) WithGroup(name string) slog.Handler {
	return migrationLog{h.Handler.WithGroup(name)}
}

// Migrate applies every pending migration in filename order, exactly once,
// each in its own transaction, serialised by a session advisory lock. A
// second run applies nothing. Each file is logged as it commits, so a run
// killed midway still says how far it got. It runs on connections of its
// own, never the app's pool: the lock is session state.
func (s *Store) Migrate(ctx context.Context) error {
	db := stdlib.OpenDB(*s.pool.Config().ConnConfig)
	defer func() { _ = db.Close() }()
	_, err := migrate.Up(ctx, db, migrations(), s.migrateOptions())
	// kit's lock-wait timeout wraps the context's deadline (%w) and already
	// says it timed out: add only which lock to look for. pg_locks shows a
	// bigint advisory key's low half as objid, in decimal.
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("migration lock key %#x (objid %d in pg_locks): %w", migrateLockID, migrateLockID, err)
	}
	return err
}

// SchemaCurrent reports whether every embedded migration has been applied.
// serve checks it at startup, so a deploy that skipped the migrate init
// container fails at boot with a name, not at the first insert.
func (s *Store) SchemaCurrent(ctx context.Context) error {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errors.New("database schema is empty: run `riptide migrate` first")
	}
	pending, err := migrate.Pending(ctx, s.sqlDB, migrations(), s.migrateOptions())
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("database schema is behind, pending %v: run `riptide migrate` first", pending)
	}
	return nil
}

// insert runs one INSERT ... ON CONFLICT (delivery_id) DO NOTHING RETURNING
// delivery_id. inserted is false when the row already existed: that is the
// `deduped` outcome.
func (s *Store) insert(ctx context.Context, sql string, args ...any) (inserted bool, err error) {
	var id string
	err = s.pool.QueryRow(ctx, sql, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// InsertBitbucket stores a Bitbucket delivery.
func (s *Store) InsertBitbucket(ctx context.Context, d *parse.BitbucketDraft, automationSource, team string) (bool, error) {
	return s.insert(ctx, `INSERT INTO bitbucket_events (
		delivery_id, event_type, repo_full_name, pr_id, commit_sha, author, author_display_name,
		branch_name, change_type, jira_keys, automation_source, is_revert, occurred_at, team, payload
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)
	ON CONFLICT (delivery_id) DO NOTHING RETURNING delivery_id`,
		d.DeliveryID, d.EventType, d.RepoFullName, d.PRID, d.CommitSHA, d.Author, d.AuthorDisplayName,
		d.BranchName, d.ChangeType, d.JiraKeys, nullable(automationSource), d.IsRevert, d.OccurredAt, team, string(d.Payload))
}

// InsertPipeline stores a CI event.
func (s *Store) InsertPipeline(ctx context.Context, d *parse.PipelineDraft, team string) (bool, error) {
	// A nil slice encodes as NULL, which image_refs (NOT NULL) rejects: a draft
	// that published nothing stores the empty list.
	images := d.ImageRefs
	if images == nil {
		images = []string{}
	}
	return s.insert(ctx, `INSERT INTO pipeline_events (
		delivery_id, source, pipeline_name, run_id, phase, status, commit_sha, image_refs,
		actor_handle, actor_account_kind, build_type, started_at, finished_at, occurred_at, team, payload
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::jsonb)
	ON CONFLICT (delivery_id) DO NOTHING RETURNING delivery_id`,
		d.DeliveryID, d.Source, d.PipelineName, d.RunID, d.Phase, d.Status, d.CommitSHA, images,
		d.ActorHandle, d.ActorAccountKind, d.BuildType, d.StartedAt, d.FinishedAt, d.OccurredAt, team, string(d.Payload))
}

// InsertArgoCD stores an Argo CD sync notification.
func (s *Store) InsertArgoCD(ctx context.Context, d *parse.ArgoCDDraft, team string) (bool, error) {
	return s.insert(ctx, `INSERT INTO argocd_events (
		delivery_id, app_name, revision, sync_status, operation_phase, started_at, finished_at,
		occurred_at, team, destination_namespace, environment, payload
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb)
	ON CONFLICT (delivery_id) DO NOTHING RETURNING delivery_id`,
		d.DeliveryID, d.AppName, d.Revision, d.SyncStatus, d.OperationPhase, d.StartedAt, d.FinishedAt,
		d.OccurredAt, team, d.DestinationNamespace, d.Environment, string(d.Payload))
}

// InsertNoergler stores a noergler rollup or feedback verdict.
func (s *Store) InsertNoergler(ctx context.Context, d *parse.NoerglerDraft, team string) (bool, error) {
	return s.insert(ctx, `INSERT INTO noergler_events (
		delivery_id, event_type, pr_key, repo, commit_sha, outcome, reviewer_handle,
		reviewer_account_kind, merge_commit_sha, lines_added, lines_removed, files_changed,
		total_runs, models_used, first_review_at, prompt_tokens, completion_tokens, elapsed_ms,
		findings_count, cost_usd, finding_id, verdict, actor, occurred_at, team, payload
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
		$19, $20::numeric, $21, $22, $23, $24, $25, $26::jsonb)
	ON CONFLICT (delivery_id) DO NOTHING RETURNING delivery_id`,
		d.DeliveryID, d.EventType, d.PRKey, d.Repo, d.CommitSHA, d.Outcome, d.ReviewerHandle,
		d.ReviewerAccountKind, d.MergeCommitSHA, d.LinesAdded, d.LinesRemoved, d.FilesChanged,
		d.TotalRuns, d.ModelsUsed, d.FirstReviewAt, d.PromptTokens, d.CompletionTokens, d.ElapsedMS,
		d.FindingsCount, d.CostUSD, d.FindingID, d.Verdict, d.Actor, d.OccurredAt, team, string(d.Payload))
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Dataset reads one team's facts for the read API.
func (s *Store) Dataset(ctx context.Context, p dataset.Params) (*dataset.Dataset, error) {
	return dataset.Load(ctx, s.pool, p)
}

// RecentActivity counts events received since `since`, per source and per
// the identifier each source aggregates by, optionally for one team.
func (s *Store) RecentActivity(ctx context.Context, since time.Time, team string) ([]Activity, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT 'bitbucket', coalesce(repo_full_name, '?'), coalesce(team, ''), count(*)
		  FROM bitbucket_events WHERE created_at >= $1 AND ($2 = '' OR team = $2) GROUP BY 2, 3
		UNION ALL
		SELECT 'pipeline', pipeline_name, coalesce(team, ''), count(*)
		  FROM pipeline_events WHERE created_at >= $1 AND ($2 = '' OR team = $2) GROUP BY 2, 3
		UNION ALL
		SELECT 'argocd', app_name, coalesce(team, ''), count(*)
		  FROM argocd_events WHERE created_at >= $1 AND ($2 = '' OR team = $2) GROUP BY 2, 3
		UNION ALL
		SELECT 'noergler', coalesce(repo, '?'), coalesce(team, ''), count(*)
		  FROM noergler_events WHERE created_at >= $1 AND ($2 = '' OR team = $2) GROUP BY 2, 3
		ORDER BY 1, 3, 2`, since, team)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Activity, error) {
		var a Activity
		err := r.Scan(&a.Source, &a.Identifier, &a.Team, &a.Events)
		return a, err
	})
}
