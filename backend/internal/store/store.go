// Package store is the Postgres layer: pool, embedded migrations and every
// query the collector runs. One function per query, positional pgx args.
// Event tables are append-only: INSERT ... ON CONFLICT (delivery_id) DO
// NOTHING, never UPDATE or DELETE.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/trick77/riptide/internal/parse"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrateLockID is the advisory-lock key held for the whole of Migrate. From
// "ript".
const migrateLockID int64 = 0x72697074

// migrateLockTimeout bounds how long one migrate waits for another's.
var migrateLockTimeout = 5 * time.Minute

// Store owns the pool.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// Open connects and checks the connection, logging the server version.
func Open(ctx context.Context, dsn string, log *slog.Logger) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse RIPTIDE_DB_URL: %w", err)
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
	return &Store{pool: pool, log: log}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Ping is the readiness check.
func (s *Store) Ping(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `SELECT 1`)
	return err
}

func migrationNames() ([]string, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Migrate applies every pending migration in filename order, exactly once,
// each in its own transaction, serialised by a session advisory lock on a
// dedicated connection. A second run applies nothing.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	lockCtx, cancelLock := context.WithTimeout(ctx, migrateLockTimeout)
	defer cancelLock()
	if _, err := conn.Exec(lockCtx, `SELECT pg_advisory_lock($1)`, migrateLockID); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("timed out after %s waiting for the migration lock (key %d in pg_locks): %w",
				migrateLockTimeout, migrateLockID, err)
		}
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrateLockID)
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := migrationNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		var applied bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		if applied {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
		s.log.Info("migration_applied", "migration", name)
	}
	return nil
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
	names, err := migrationNames()
	if err != nil {
		return err
	}
	var missing []string
	for _, name := range names {
		var applied bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&applied); err != nil {
			return err
		}
		if !applied {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("database schema is behind, pending %v: run `riptide migrate` first", missing)
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
	return s.insert(ctx, `INSERT INTO pipeline_events (
		delivery_id, source, pipeline_name, run_id, phase, status, commit_sha, image_ref,
		actor_handle, actor_account_kind, started_at, finished_at, occurred_at, team, payload
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb)
	ON CONFLICT (delivery_id) DO NOTHING RETURNING delivery_id`,
		d.DeliveryID, d.Source, d.PipelineName, d.RunID, d.Phase, d.Status, d.CommitSHA, d.ImageRef,
		d.ActorHandle, d.ActorAccountKind, d.StartedAt, d.FinishedAt, d.OccurredAt, team, string(d.Payload))
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

// Activity is how many events one sender identifier produced.
type Activity struct {
	Source     string // bitbucket | pipeline | argocd | noergler
	Identifier string // repo_full_name / pipeline_name / app_name / repo
	Team       string
	Events     int64
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
