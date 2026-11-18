package store

import (
	"context"
	"strings"
	"testing"

	"github.com/trick77/riptide/internal/testdb"
)

// An analysis stored while the trend signal was called flaky reads under
// failing_more after 0009: its kind, IDs, fact IDs, {{facts}}, resolved
// IDs and the report's card entries. Prose and repos named flaky stay.
func TestMigrationRenamesFlakySignals(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t, testdb.DSN(t))
	const rename = "0009_team_analyses_failing_more.sql"
	apply := migratedBefore(t, pool, rename)
	assessment := `{"signals": [{"id": "flaky:main:kasse/flaky", "kind": "flaky", "repo": "kasse/flaky",
		"facts": [{"id": "flaky:main:kasse/flaky.runs"}]},
		{"id": "resolved:flaky:feature:kasse/a", "kind": "resolved", "metric": "flaky:feature:kasse/a"}]}`
	output := `{"de": {"take": "{{flaky:main:kasse/flaky.failure_rate}} rot, der flaky: Build",
		"trend": "flaky: Tests sind es nicht", "cards": [{"signal": "resolved:flaky:feature:kasse/a"}]}}`
	report := `{"dropped": ["de card flaky:main:kasse/flaky: twice", "en card resolved:flaky:feature:kasse/a: twice"],
		"missed": ["flaky:main:kasse/b"]}`
	if _, err := pool.Exec(ctx, `INSERT INTO team_analyses (team, weeks, period_from, period_to, trigger, status, model,
		assessment, output, report) VALUES ('checkout', 4, now(), now(), 'scheduled', 'done', 'm', $1, $2, $3)`,
		assessment, output, report); err != nil {
		t.Fatal(err)
	}
	apply(rename)
	var a, o, r string
	if err := pool.QueryRow(ctx, `SELECT assessment::text, output::text, report::text FROM team_analyses`).
		Scan(&a, &o, &r); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id": "failing_more:main:kasse/flaky"`, `"kind": "failing_more"`,
		`"repo": "kasse/flaky"`, `"id": "failing_more:main:kasse/flaky.runs"`,
		`"id": "resolved:failing_more:feature:kasse/a"`, `"metric": "failing_more:feature:kasse/a"`} {
		if !strings.Contains(a, want) {
			t.Errorf("assessment lacks %s: %s", want, a)
		}
	}
	if strings.Contains(a, `"flaky`) || strings.Contains(a, `:flaky:`) {
		t.Errorf("assessment keeps flaky: %s", a)
	}
	// The model's prose stays, even a string starting with flaky:.
	for _, want := range []string{`{{failing_more:main:kasse/flaky.failure_rate}} rot, der flaky: Build`,
		`"trend": "flaky: Tests sind es nicht"`, `"signal": "resolved:failing_more:feature:kasse/a"`} {
		if !strings.Contains(o, want) {
			t.Errorf("output lacks %s: %s", want, o)
		}
	}
	for _, want := range []string{
		`["de card failing_more:main:kasse/flaky: twice", "en card resolved:failing_more:feature:kasse/a: twice"]`,
		`"missed": ["failing_more:main:kasse/b"]`} {
		if !strings.Contains(r, want) {
			t.Errorf("report lacks %s: %s", want, r)
		}
	}
}
