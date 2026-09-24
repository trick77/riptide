// Package testdb gives a test its own Postgres schema. Gated on
// RIPTIDE_TEST_DSN: CI provides a Postgres service container; locally
// `docker compose up -d db` and
// RIPTIDE_TEST_DSN=postgres://riptide:riptide@localhost:5432/riptide?sslmode=disable.
package testdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var seq atomic.Int64

// DSN creates a fresh schema, drops it at cleanup, and returns a DSN whose
// search_path points at it. Skips the test when RIPTIDE_TEST_DSN is unset.
func DSN(t testing.TB) string {
	t.Helper()
	base := os.Getenv("RIPTIDE_TEST_DSN")
	if base == "" {
		t.Skip("RIPTIDE_TEST_DSN not set")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("t_%d_%d_%d", time.Now().UnixNano(), os.Getpid(), seq.Add(1))
	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// Pool opens a plain pool on dsn for assertions, closed at cleanup.
func Pool(t testing.TB, dsn string) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}
