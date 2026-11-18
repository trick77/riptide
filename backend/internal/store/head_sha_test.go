package store

import (
	"context"
	"testing"

	"github.com/trick77/riptide/internal/testdb"
)

// 0010 copies a head_sha and base_sha a pre-0010 collector kept only in the
// payload into the new columns by the parser's rules (whitespace-only is
// blank, as strings.TrimSpace has it), without touching modified_at, and its
// insert trigger fills them for old pods mid-rollout.
func TestMigrationHeadSHABackfill(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t, testdb.DSN(t))
	const migration = "0010_pipeline_head_sha.sql"
	apply := migratedBefore(t, pool, migration)
	if _, err := pool.Exec(ctx, `INSERT INTO pipeline_events
		(delivery_id, source, pipeline_name, run_id, phase, occurred_at, modified_at, commit_sha, payload) VALUES
		('sent',   'jenkins', 'p', '1', 'COMPLETED', now(), '2000-01-01', 'abc1234', '{"head_sha": "DEF4567890\n", "base_sha": "AAA1111"}'),
		('short',  'jenkins', 'p', '2', 'COMPLETED', now(), '2000-01-01', 'abc1234', '{"head_sha": "def"}'),
		('number', 'jenkins', 'p', '3', 'COMPLETED', now(), '2000-01-01', 'abc1234', '{"head_sha": 12345678}'),
		('blank',  'jenkins', 'p', '4', 'COMPLETED', now(), '2000-01-01', 'abc1234', '{"head_sha": "\t\t\t\n\n\n\n"}'),
		('none',   'jenkins', 'p', '5', 'COMPLETED', now(), '2000-01-01', 'abc1234', '{}')`); err != nil {
		t.Fatal(err)
	}
	apply(migration)
	for id, want := range map[string]string{"sent": "def4567890|aaa1111", "short": "|", "number": "|", "blank": "|", "none": "|"} {
		var head, base *string
		var untouched bool
		if err := pool.QueryRow(ctx, `SELECT head_sha, base_sha, modified_at = '2000-01-01' FROM pipeline_events WHERE delivery_id = $1`, id).
			Scan(&head, &base, &untouched); err != nil {
			t.Fatal(err)
		}
		if g := deref(head) + "|" + deref(base); g != want || !untouched {
			t.Errorf("%s: head|base = %q (want %q), modified_at untouched = %v", id, g, want, untouched)
		}
	}
	// Old pods mid-rollout keep both in the payload only: the trigger fills
	// them; a row that carries one is left alone.
	if _, err := pool.Exec(ctx, `INSERT INTO pipeline_events (delivery_id, source, pipeline_name, run_id, phase, occurred_at, payload)
		VALUES ('old-pod', 'jenkins', 'p', '6', 'COMPLETED', now(), '{"head_sha": "aaa1111", "base_sha": "bbb2222"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pipeline_events (delivery_id, source, pipeline_name, run_id, phase, occurred_at, head_sha, payload)
		VALUES ('new-pod', 'jenkins', 'p', '7', 'COMPLETED', now(), 'ccc3333', '{"head_sha": "ddd4444"}')`); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"old-pod": "aaa1111|bbb2222", "new-pod": "ccc3333|"} {
		var head, base *string
		if err := pool.QueryRow(ctx, `SELECT head_sha, base_sha FROM pipeline_events WHERE delivery_id = $1`, id).Scan(&head, &base); err != nil {
			t.Fatal(err)
		}
		if g := deref(head) + "|" + deref(base); g != want {
			t.Errorf("%s: head|base = %q, want %q", id, g, want)
		}
	}
}
