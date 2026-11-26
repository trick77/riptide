package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// A repo's trunk is master, main, or the branch most of its merged PRs went
// into (repo_trunks, 0017); commit_sightings reads every one of them, no
// longer master alone, and a payload of an odd shape fails no read.
func TestRepoTrunks(t *testing.T) {
	_, pool := testStore(t)
	ctx := context.Background()
	n := 0
	merged := func(repo, target string, id int) {
		t.Helper()
		n++
		payload, _ := json.Marshal(map[string]any{"pullRequest": map[string]any{"id": id,
			"toRef": map[string]any{"displayId": target}}})
		if _, err := pool.Exec(ctx, `INSERT INTO bitbucket_events (delivery_id, event_type, repo_full_name, pr_id,
			occurred_at, team, jira_keys, payload) VALUES ($1, 'pr:merged', $2, $3, $4, 'checkout', '{}', $5::jsonb)`,
			fmt.Sprintf("m-%d", n), repo, id, viewBase, string(payload)); err != nil {
			t.Fatal(err)
		}
	}
	push := func(repo, branch, payload string) {
		t.Helper()
		n++
		if _, err := pool.Exec(ctx, `INSERT INTO bitbucket_events (delivery_id, event_type, repo_full_name, branch_name,
			occurred_at, team, jira_keys, payload) VALUES ($1, 'repo:refs_changed', $2, $3, $4, 'checkout', '{}', $5::jsonb)`,
			fmt.Sprintf("p-%d", n), repo, branch, viewBase, payload); err != nil {
			t.Fatal(err)
		}
	}
	// Most PRs into develop, the case aside; one into a feature branch.
	merged("acme/ledger", "Develop", 1)
	merged("acme/ledger", "develop", 2)
	merged("acme/ledger", "feature/x", 3)
	// A tie: both are trunks.
	merged("acme/billing", "main", 1)
	merged("acme/billing", "release", 2)
	got := strings2set(t, pool, `SELECT repo_full_name || ':' || trunk FROM repo_trunks`)
	if !sameSet(got, "acme/ledger:develop", "acme/billing:main", "acme/billing:release") {
		t.Errorf("trunks = %v", got)
	}
	commits := func(id string) string {
		b, _ := json.Marshal(map[string]any{"commits": []any{map[string]any{"id": id, "authorTimestamp": 0,
			"committerTimestamp": 0}}})
		return string(b)
	}
	push("acme/ledger", "develop", commits(sha("1")))
	push("acme/ledger", "feature/x", commits(sha("2")))
	push("acme/ledger", "main", commits(sha("3")))
	push("acme/ledger", "master", `{"commits": "not a list"}`)
	push("acme/billing", "release", commits(sha("4")))
	sighted := strings2set(t, pool, `SELECT commit_sha FROM commit_sightings`)
	if !sameSet(sighted, sha("1"), sha("3"), sha("4")) {
		t.Errorf("sightings = %v", sighted)
	}
}
