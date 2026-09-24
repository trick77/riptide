package parse

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

// fixture loads testdata/<name> as a mutable map.
func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func m(v any) map[string]any { return v.(map[string]any) }

func extract(t *testing.T, body any, eventKey, requestID string) (*BitbucketDraft, *BitbucketSkip) {
	t.Helper()
	return Bitbucket(encode(t, body), BitbucketHeaders{EventKey: eventKey, RequestID: requestID}, fixedNow)
}

func mustDraft(t *testing.T, body any, eventKey string) *BitbucketDraft {
	t.Helper()
	d, s := extract(t, body, eventKey, "r")
	if d == nil {
		t.Fatalf("skipped: %+v", s)
	}
	return d
}

func mustSkip(t *testing.T, body any, eventKey string) *BitbucketSkip {
	t.Helper()
	d, s := extract(t, body, eventKey, "r")
	if s == nil {
		t.Fatalf("not skipped: %+v", d)
	}
	return s
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestPRMergedHasLowercasedJoinKeys(t *testing.T) {
	for _, c := range []struct{ fixture, event string }{
		{"bitbucket_pr_merged.json", "pr:merged"},
		{"bitbucket_pr_declined.json", "pr:declined"},
	} {
		t.Run(c.event, func(t *testing.T) {
			body := fixture(t, c.fixture)
			raw := encode(t, body)
			d, s := Bitbucket(raw, BitbucketHeaders{EventKey: c.event, RequestID: "req-1"}, fixedNow)
			if s != nil {
				t.Fatalf("skipped: %+v", s)
			}
			if d.DeliveryID != "req-1" || d.EventType != c.event {
				t.Errorf("id/type = %q/%q", d.DeliveryID, d.EventType)
			}
			if str(d.RepoFullName) != "acme/payments-api" {
				t.Errorf("repo = %s", str(d.RepoFullName))
			}
			if d.PRID == nil || *d.PRID != 42 {
				t.Errorf("pr_id = %v", d.PRID)
			}
			if str(d.CommitSHA) != "abc1234567890abc1234567890abc1234567890a" {
				t.Errorf("commit = %s", str(d.CommitSHA))
			}
			if str(d.BranchName) != "feature/abc-123-retries" {
				t.Errorf("branch = %s", str(d.BranchName))
			}
			if str(d.Author) != "alice" || d.IsRevert || str(d.ChangeType) != "feature" {
				t.Errorf("author/revert/change_type = %s/%v/%s", str(d.Author), d.IsRevert, str(d.ChangeType))
			}
			keys := strings.Join(d.JiraKeys, ",")
			if !strings.Contains(keys, "ABC-123") || !strings.Contains(keys, "PROJ-9") {
				t.Errorf("jira keys = %v", d.JiraKeys)
			}
			if string(d.Payload) != string(raw) {
				t.Error("payload is not the raw body")
			}
		})
	}
}

// Python lowercased the branch before reading Jira keys off it, so a key
// that appeared only in the branch name was never found.
func TestJiraKeyFromBranchOnly(t *testing.T) {
	body := fixture(t, "bitbucket_pr_merged.json")
	pr := m(body["pullRequest"])
	pr["title"] = "no key here"
	pr["description"] = ""
	m(pr["fromRef"])["displayId"] = "feature/PAY-77-retry"
	d := mustDraft(t, body, "pr:merged")
	if strings.Join(d.JiraKeys, ",") != "PAY-77" {
		t.Errorf("jira keys = %v", d.JiraKeys)
	}
	if str(d.BranchName) != "feature/pay-77-retry" {
		t.Errorf("branch = %s", str(d.BranchName))
	}
}

func TestRevertTitle(t *testing.T) {
	body := fixture(t, "bitbucket_pr_merged.json")
	m(body["pullRequest"])["title"] = `Revert "Add payment retry"`
	if d := mustDraft(t, body, "pr:merged"); !d.IsRevert {
		t.Error("revert not detected")
	}
}

func TestDraftToReadyIsSyntheticReadyForReview(t *testing.T) {
	body := fixture(t, "bitbucket_pr_modified_draft_to_ready.json")
	d := mustDraft(t, body, "pr:modified")
	if d.EventType != ReadyForReview || *d.PRID != 42 || str(d.RepoFullName) != "acme/payments-api" {
		t.Errorf("draft = %+v", d)
	}
	var payload map[string]any
	_ = json.Unmarshal(d.Payload, &payload)
	if payload["eventKey"] != "pr:modified" || payload["previousDraft"] != true {
		t.Error("raw payload lost eventKey/previousDraft")
	}
}

func TestDraftToReadyUsesActorAsAuthor(t *testing.T) {
	body := fixture(t, "bitbucket_pr_modified_draft_to_ready.json")
	body["actor"] = map[string]any{"name": "carol", "displayName": "Carol Maintainer", "slug": "carol"}
	if d := mustDraft(t, body, "pr:modified"); str(d.Author) != "carol" {
		t.Errorf("author = %s", str(d.Author))
	}
}

func TestPRModifiedWithoutFlipIsSkipped(t *testing.T) {
	t.Run("title only", func(t *testing.T) {
		s := mustSkip(t, fixture(t, "bitbucket_pr_modified_title_only.json"), "pr:modified")
		if s.Reason != "pr:modified without draft→ready flip" || s.EventType != "pr:modified" || str(s.RepoFullName) != "acme/payments-api" {
			t.Errorf("skip = %+v", s)
		}
	})
	t.Run("ready to draft", func(t *testing.T) {
		body := fixture(t, "bitbucket_pr_modified_draft_to_ready.json")
		body["previousDraft"] = false
		m(body["pullRequest"])["draft"] = true
		mustSkip(t, body, "pr:modified")
	})
	t.Run("no draft fields", func(t *testing.T) {
		body := fixture(t, "bitbucket_pr_modified_title_only.json")
		delete(body, "previousDraft")
		delete(m(body["pullRequest"]), "draft")
		mustSkip(t, body, "pr:modified")
	})
	t.Run("draft flags that are not booleans", func(t *testing.T) {
		body := fixture(t, "bitbucket_pr_modified_draft_to_ready.json")
		body["previousDraft"] = "true"
		mustSkip(t, body, "pr:modified")
	})
}

func TestPROpenedAsDraftPassesThrough(t *testing.T) {
	body := fixture(t, "bitbucket_pr_merged.json")
	body["eventKey"] = "pr:opened"
	pr := m(body["pullRequest"])
	pr["state"] = "OPEN"
	pr["draft"] = true
	d := mustDraft(t, body, "pr:opened")
	if d.EventType != "pr:opened" || !strings.Contains(string(d.Payload), `"draft":true`) {
		t.Errorf("draft = %+v", d)
	}
}

func TestBranchPush(t *testing.T) {
	d := mustDraft(t, fixture(t, "bitbucket_refs_changed.json"), "repo:refs_changed")
	if str(d.RepoFullName) != "acme/payments-api" || str(d.BranchName) != "master" ||
		str(d.CommitSHA) != "feedfacefeedfacefeedfacefeedfacefeedface" || str(d.Author) != "alice" || d.PRID != nil {
		t.Errorf("draft = %+v", d)
	}
}

func TestPushWithoutBranchChangeIsSkipped(t *testing.T) {
	t.Run("tag only", func(t *testing.T) {
		body := fixture(t, "bitbucket_refs_changed.json")
		body["changes"] = []any{map[string]any{
			"ref":      map[string]any{"id": "refs/tags/v1", "displayId": "v1", "type": "TAG"},
			"fromHash": strings.Repeat("0", 40), "toHash": strings.Repeat("1", 40), "type": "ADD",
		}}
		s := mustSkip(t, body, "repo:refs_changed")
		if s.Reason != "no branch change in push" || str(s.RepoFullName) != "acme/payments-api" {
			t.Errorf("skip = %+v", s)
		}
	})
	t.Run("delete only", func(t *testing.T) {
		body := fixture(t, "bitbucket_refs_changed.json")
		m(body["changes"].([]any)[0])["type"] = "DELETE"
		if s := mustSkip(t, body, "repo:refs_changed"); s.Reason != "no branch change in push" {
			t.Errorf("skip = %+v", s)
		}
	})
	t.Run("null ref type is not a branch", func(t *testing.T) {
		body := fixture(t, "bitbucket_refs_changed.json")
		m(m(body["changes"].([]any)[0])["ref"])["type"] = nil
		mustSkip(t, body, "repo:refs_changed")
	})
}

func TestSecondBranchChangeFillsMissingFields(t *testing.T) {
	body := fixture(t, "bitbucket_refs_changed.json")
	first := m(body["changes"].([]any)[0])
	delete(first, "toHash")
	body["changes"] = append(body["changes"].([]any), map[string]any{
		"ref":    map[string]any{"displayId": "other", "type": "BRANCH"},
		"toHash": "ABCDEF1234567", "type": "UPDATE",
	})
	d := mustDraft(t, body, "repo:refs_changed")
	if str(d.BranchName) != "master" || str(d.CommitSHA) != "abcdef1234567" {
		t.Errorf("branch/commit = %s/%s", str(d.BranchName), str(d.CommitSHA))
	}
}

func TestDeliveryID(t *testing.T) {
	raw := []byte(`{}`)
	t.Run("X-Request-Id first", func(t *testing.T) {
		d, _ := Bitbucket(raw, BitbucketHeaders{EventKey: "x", RequestID: "id-1", RequestUUID: "uuid-1"}, fixedNow)
		if d.DeliveryID != "id-1" {
			t.Errorf("id = %q", d.DeliveryID)
		}
	})
	t.Run("X-Request-UUID second", func(t *testing.T) {
		d, _ := Bitbucket(raw, BitbucketHeaders{EventKey: "x", RequestUUID: "uuid-1"}, fixedNow)
		if d.DeliveryID != "uuid-1" {
			t.Errorf("id = %q", d.DeliveryID)
		}
	})
	t.Run("oversized header ignored", func(t *testing.T) {
		d, _ := Bitbucket(raw, BitbucketHeaders{EventKey: "x", RequestID: strings.Repeat("a", 201)}, fixedNow)
		if !strings.HasPrefix(d.DeliveryID, "synthetic#x#") {
			t.Errorf("id = %q", d.DeliveryID)
		}
	})
	t.Run("synthetic id is stable per body and distinct across bodies", func(t *testing.T) {
		base := fixture(t, "bitbucket_refs_changed.json")
		other := fixture(t, "bitbucket_refs_changed.json")
		m(other["changes"].([]any)[0])["toHash"] = strings.Repeat("deadbeef", 5)
		h := BitbucketHeaders{EventKey: "repo:refs_changed"}
		a, _ := Bitbucket(encode(t, base), h, fixedNow)
		again, _ := Bitbucket(encode(t, base), h, fixedNow)
		b, _ := Bitbucket(encode(t, other), h, fixedNow)
		if a.DeliveryID != again.DeliveryID {
			t.Error("a redelivery got a different id")
		}
		if a.DeliveryID == b.DeliveryID {
			t.Error("two pushes collided")
		}
		if !strings.HasPrefix(a.DeliveryID, "synthetic#repo:refs_changed#") {
			t.Errorf("id = %q", a.DeliveryID)
		}
	})
	t.Run("two comments in the same second do not collide", func(t *testing.T) {
		a := fixture(t, "bitbucket_pr_comment_added.json")
		b := fixture(t, "bitbucket_pr_comment_added.json")
		m(b["comment"])["text"] = "a different comment"
		h := BitbucketHeaders{EventKey: "pr:comment:added"}
		da, _ := Bitbucket(encode(t, a), h, fixedNow)
		db, _ := Bitbucket(encode(t, b), h, fixedNow)
		if da.DeliveryID == db.DeliveryID {
			t.Error("collided")
		}
	})
}

func TestAuthorSelection(t *testing.T) {
	t.Run("PR author login preferred", func(t *testing.T) {
		body := fixture(t, "bitbucket_pr_merged.json")
		m(m(body["pullRequest"])["author"])["user"] = map[string]any{"name": "login-name", "slug": "slug-name", "displayName": "Display Name"}
		if d := mustDraft(t, body, "pr:merged"); str(d.Author) != "login-name" {
			t.Errorf("author = %s", str(d.Author))
		}
	})
	t.Run("slug then display name", func(t *testing.T) {
		body := fixture(t, "bitbucket_pr_merged.json")
		m(m(body["pullRequest"])["author"])["user"] = map[string]any{"name": "", "displayName": "Only Display"}
		if d := mustDraft(t, body, "pr:merged"); str(d.Author) != "Only Display" {
			t.Errorf("author = %s", str(d.Author))
		}
	})
	t.Run("push falls back to actor", func(t *testing.T) {
		if d := mustDraft(t, fixture(t, "bitbucket_refs_changed.json"), "repo:refs_changed"); str(d.Author) != "alice" {
			t.Errorf("author = %s", str(d.Author))
		}
	})
	for _, event := range []string{"pr:reviewer:approved", "pr:reviewer:unapproved", "pr:reviewer:needs_work", "pr:reviewer:updated"} {
		t.Run(event+" uses the actor", func(t *testing.T) {
			d := mustDraft(t, fixture(t, "bitbucket_pr_reviewer_approved.json"), event)
			if str(d.Author) != "bob" || *d.PRID != 42 || d.EventType != event {
				t.Errorf("draft = %+v", d)
			}
		})
	}
	t.Run("comment uses the actor", func(t *testing.T) {
		if d := mustDraft(t, fixture(t, "bitbucket_pr_comment_added.json"), "pr:comment:added"); str(d.Author) != "bob" {
			t.Errorf("author = %s", str(d.Author))
		}
	})
	t.Run("reviewer event without PR author", func(t *testing.T) {
		body := fixture(t, "bitbucket_pr_reviewer_approved.json")
		m(body["pullRequest"])["author"] = map[string]any{}
		if d := mustDraft(t, body, "pr:reviewer:approved"); str(d.Author) != "bob" {
			t.Errorf("author = %s", str(d.Author))
		}
	})
	t.Run("lifecycle event keeps the PR author", func(t *testing.T) {
		body := fixture(t, "bitbucket_pr_merged.json")
		body["actor"] = map[string]any{"name": "carol", "slug": "carol"}
		if d := mustDraft(t, body, "pr:merged"); str(d.Author) != "alice" {
			t.Errorf("author = %s", str(d.Author))
		}
	})
}

func TestEventTypeAndOccurredAt(t *testing.T) {
	d := mustDraft(t, fixture(t, "bitbucket_refs_changed.json"), "")
	if d.EventType != "unknown" {
		t.Errorf("event type = %q", d.EventType)
	}
	d = mustDraft(t, fixture(t, "bitbucket_pr_merged.json"), "pr:merged")
	if d.OccurredAt.Year() != 2026 || d.OccurredAt.Location() != time.UTC || d.DateUnparsed != "" {
		t.Errorf("occurred_at = %v", d.OccurredAt)
	}
	body := fixture(t, "bitbucket_pr_merged.json")
	body["date"] = "2026-04-28T20:15:00+1000"
	if d = mustDraft(t, body, "pr:merged"); !d.OccurredAt.Equal(time.Date(2026, 4, 28, 10, 15, 0, 0, time.UTC)) {
		t.Errorf("colon-less offset: %v", d.OccurredAt)
	}
	body["date"] = "not a date"
	if d = mustDraft(t, body, "pr:merged"); !d.OccurredAt.Equal(fixedNow) || d.DateUnparsed != "not a date" {
		t.Errorf("fallback: %v %q", d.OccurredAt, d.DateUnparsed)
	}
}

func TestServiceAccount(t *testing.T) {
	for _, c := range []struct {
		typ  any
		want bool
	}{{"SERVICE", true}, {"service", true}, {"NORMAL", false}, {nil, false}} {
		body := fixture(t, "bitbucket_pr_comment_added.json")
		if c.typ == nil {
			delete(m(body["actor"]), "type")
		} else {
			m(body["actor"])["type"] = c.typ
		}
		if d := mustDraft(t, body, "pr:comment:added"); d.AuthorIsServiceAccount != c.want {
			t.Errorf("type %v: service = %v", c.typ, d.AuthorIsServiceAccount)
		}
	}
	body := fixture(t, "bitbucket_pr_merged.json")
	m(m(m(body["pullRequest"])["author"])["user"])["type"] = "SERVICE"
	if d := mustDraft(t, body, "pr:merged"); !d.AuthorIsServiceAccount {
		t.Error("PR author SERVICE not flagged")
	}
}

func TestDisplayName(t *testing.T) {
	d := mustDraft(t, fixture(t, "bitbucket_pr_comment_added.json"), "pr:comment:added")
	if str(d.Author) != "bob" || str(d.AuthorDisplayName) != "Bob Reviewer" {
		t.Errorf("author = %s / %s", str(d.Author), str(d.AuthorDisplayName))
	}
	body := fixture(t, "bitbucket_pr_merged.json")
	m(m(m(body["pullRequest"])["author"])["user"])["displayName"] = "Alice Example"
	if d = mustDraft(t, body, "pr:merged"); str(d.AuthorDisplayName) != "Alice Example" {
		t.Errorf("display = %s", str(d.AuthorDisplayName))
	}
	body = fixture(t, "bitbucket_pr_comment_added.json")
	delete(m(body["actor"]), "displayName")
	if d = mustDraft(t, body, "pr:comment:added"); d.AuthorDisplayName != nil {
		t.Errorf("display = %s", str(d.AuthorDisplayName))
	}
}

func TestUnusableBodiesAreSkipped(t *testing.T) {
	for _, c := range []struct {
		name, body, reason string
	}{
		{"empty", "", "empty payload"},
		{"whitespace", "  \n", "empty payload"},
		{"not json", "{nope", "non-json payload"},
		{"trailing garbage", `{} {}`, "non-json payload"},
		// json.Decoder alone accepts this; Postgres would not.
		{"trailing brace", `{"a": 1}}`, "non-json payload"},
		{"array", `["not", "an", "object"]`, "non-object payload"},
		{"invalid UTF-8", "{\"a\": \"\xff\"}", "non-json payload"},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, s := Bitbucket([]byte(c.body), BitbucketHeaders{EventKey: "pr:merged", RequestID: "rid"}, fixedNow)
			if s == nil || s.Reason != c.reason || s.DeliveryID != "rid" {
				t.Errorf("got %+v / %+v", d, s)
			}
		})
	}
	s := mustSkip(t, map[string]any{"test": true}, "diagnostics:ping")
	if s.Reason != "diagnostics ping" {
		t.Errorf("ping: %+v", s)
	}
}

func TestPRIDMustBeAnInteger(t *testing.T) {
	body := fixture(t, "bitbucket_pr_merged.json")
	m(body["pullRequest"])["id"] = "42"
	if d := mustDraft(t, body, "pr:merged"); d.PRID != nil {
		t.Errorf("pr_id = %d", *d.PRID)
	}
	m(body["pullRequest"])["id"] = 4.5
	if d := mustDraft(t, body, "pr:merged"); d.PRID != nil {
		t.Errorf("pr_id = %d", *d.PRID)
	}
}

func TestRepoFromPRToRef(t *testing.T) {
	body := fixture(t, "bitbucket_pr_merged.json")
	delete(body, "repository")
	d := mustDraft(t, body, "pr:merged")
	if str(d.RepoFullName) != "acme/payments-api" {
		t.Errorf("repo = %s", str(d.RepoFullName))
	}
}
