package parse

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// Events where `actor` is who did the thing (the reviewer, the commenter)
// and `pullRequest.author` is only the PR opener. The author lookup is skipped
// for these so the row names the person who acted.
var reviewerActivityEvents = map[string]bool{
	"pr:comment:added":       true,
	"pr:reviewer:approved":   true,
	"pr:reviewer:needs_work": true,
	"pr:reviewer:updated":    true,
	"pr:reviewer:unapproved": true,
}

// ReadyForReview is the synthetic event type for a `pr:modified` delivery
// that flips a draft to ready: the pickup-clock start for PRs opened as
// drafts. The raw eventKey survives on payload.eventKey.
const ReadyForReview = "pr:ready_for_review"

// maxDeliveryHeader bounds a header-supplied delivery id.
const maxDeliveryHeader = 200

// BitbucketHeaders are the request headers the parser reads.
type BitbucketHeaders struct {
	EventKey    string // X-Event-Key
	RequestID   string // X-Request-Id: what Bitbucket Data Center sends per delivery
	RequestUUID string // X-Request-UUID: Bitbucket Cloud's name for the same
}

// BitbucketDraft is a parsed delivery, ready for insert. Join identifiers
// (repo, branch, commit) are lowercased; the original casing survives in
// Payload, which is the raw request body.
type BitbucketDraft struct {
	DeliveryID             string
	EventType              string
	RepoFullName           *string
	PRID                   *int64
	CommitSHA              *string
	Author                 *string
	AuthorDisplayName      *string
	AuthorIsServiceAccount bool
	BranchName             *string
	ChangeType             *string
	JiraKeys               []string
	IsRevert               bool
	OccurredAt             time.Time
	// DateUnparsed is set when the body carried a `date` that could not be
	// read, so OccurredAt fell back to the receive time.
	DateUnparsed string
	Payload      []byte
}

// BitbucketSkip is a delivery accepted but not stored.
type BitbucketSkip struct {
	Reason       string
	DeliveryID   string
	EventType    string
	RepoFullName *string
}

// Bitbucket parses one Bitbucket Data Center delivery. Exactly one of
// the results is non-nil. Skipped: bodies that are not a JSON object,
// `diagnostics:ping` (the "Test connection" button), `pr:modified` other than
// a draft->ready flip, and pushes whose changes carry no branch update (tag
// or delete only). now is the fallback when the body has no usable `date`.
func Bitbucket(raw []byte, h BitbucketHeaders, now time.Time) (*BitbucketDraft, *BitbucketSkip) {
	eventType := h.EventKey
	if eventType == "" {
		eventType = "unknown"
	}
	deliveryID := bitbucketDeliveryID(raw, h)
	skip := func(reason string, repo *string) (*BitbucketDraft, *BitbucketSkip) {
		return nil, &BitbucketSkip{Reason: reason, DeliveryID: deliveryID, EventType: eventType, RepoFullName: repo}
	}

	if len(bytes.TrimSpace(raw)) == 0 {
		return skip("empty payload", nil)
	}
	var decoded any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if !utf8.Valid(raw) || dec.Decode(&decoded) != nil {
		return skip("non-json payload", nil)
	}
	// The decoder stops after one value; anything after it, a stray `}`
	// included, makes the body invalid JSON that Postgres would refuse.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return skip("non-json payload", nil)
	}
	if !jsonbSafe(raw) {
		return skip("payload not storable as JSONB", nil)
	}
	body, ok := decoded.(map[string]any)
	if !ok {
		return skip("non-object payload", nil)
	}

	rawRepo := repoFullName(body)
	repo := ptr(strings.ToLower(rawRepo))
	if eventType == "diagnostics:ping" {
		return skip("diagnostics ping", repo)
	}

	pr := asMap(body["pullRequest"])
	var prID *int64
	if n, ok := pr["id"].(json.Number); ok {
		if v, err := n.Int64(); err == nil {
			prID = &v
		}
	}
	title, _ := pr["title"].(string)
	description, _ := pr["description"].(string)

	if eventType == "pr:modified" {
		wasDraft, ok1 := body["previousDraft"].(bool)
		isDraft, ok2 := pr["draft"].(bool)
		if !ok1 || !ok2 || !wasDraft || isDraft {
			return skip("pr:modified without draft→ready flip", repo)
		}
		eventType = ReadyForReview
	}

	var branch, commit, author, displayName string
	serviceAccount, isRevert := false, false
	actorAuthored := reviewerActivityEvents[eventType] || eventType == ReadyForReview

	if len(pr) > 0 {
		fromRef := asMap(pr["fromRef"])
		branch, _ = fromRef["displayId"].(string)
		commit, _ = fromRef["latestCommit"].(string)
		if !actorAuthored {
			user := asMap(asMap(pr["author"])["user"])
			author, displayName, serviceAccount = userFields(user)
		}
		isRevert = IsRevert(title)
	}

	// Push: top-level changes[] with {ref:{displayId,type}, toHash, type}.
	// DELETE changes carry no new commit, and a TAG ref's displayId is not a
	// branch, so both are skipped.
	changes, _ := body["changes"].([]any)
	hadBranchChange := false
	if branch == "" || commit == "" {
		for _, c := range changes {
			change := asMap(c)
			if strings.ToUpper(pyStr(change, "type", "")) == "DELETE" {
				continue
			}
			ref := asMap(change["ref"])
			if strings.ToUpper(pyStr(ref, "type", "BRANCH")) != "BRANCH" {
				continue
			}
			hadBranchChange = true
			if d, ok := ref["displayId"].(string); ok && branch == "" {
				branch = d
			}
			if to, ok := change["toHash"].(string); ok && commit == "" {
				commit = to
			}
			if branch != "" && commit != "" {
				break
			}
		}
	}
	if len(pr) == 0 && len(changes) > 0 && !hadBranchChange {
		return skip("no branch change in push", repo)
	}

	if author == "" {
		author, displayName, serviceAccount = userFields(asMap(body["actor"]))
	}

	d := &BitbucketDraft{
		DeliveryID:             deliveryID,
		EventType:              eventType,
		RepoFullName:           repo,
		PRID:                   prID,
		CommitSHA:              ptr(strings.ToLower(commit)),
		Author:                 ptr(author),
		AuthorDisplayName:      ptr(displayName),
		AuthorIsServiceAccount: serviceAccount,
		BranchName:             ptr(strings.ToLower(branch)),
		ChangeType:             ptr(ChangeType(strings.ToLower(branch))),
		// Keys are read off the branch as pushed: lowercased, `ABC-123`
		// would no longer match.
		JiraKeys:   JiraKeys(title, description, branch),
		IsRevert:   isRevert,
		OccurredAt: now.UTC(),
		Payload:    raw,
	}
	if date, ok := body["date"].(string); ok {
		if t, err := Time(date); err == nil {
			d.OccurredAt = t
		} else {
			d.DateUnparsed = date
		}
	}
	return d, nil
}

// bitbucketDeliveryID prefers the per-delivery header. Without one the id is
// derived from the event key and the body bytes: a redelivery of the same
// body dedupes, two distinct events never collide.
func bitbucketDeliveryID(raw []byte, h BitbucketHeaders) string {
	for _, v := range []string{h.RequestID, h.RequestUUID} {
		if v = strings.TrimSpace(v); v != "" && len(v) <= maxDeliveryHeader {
			return v
		}
	}
	key := h.EventKey
	if key == "" {
		key = "unknown"
	}
	sum := sha256.Sum256(raw)
	return "synthetic#" + key + "#" + hex.EncodeToString(sum[:16])
}

// repoFullName builds `<projectKey>/<slug>`. Push events carry `repository`
// at the top level; PR events nest it in pullRequest.toRef.repository.
func repoFullName(body map[string]any) string {
	repo := asMap(body["repository"])
	if len(repo) == 0 {
		repo = asMap(asMap(asMap(body["pullRequest"])["toRef"])["repository"])
	}
	project, ok1 := asMap(repo["project"])["key"].(string)
	slug, ok2 := repo["slug"].(string)
	if ok1 && ok2 {
		return project + "/" + slug
	}
	return ""
}

// userFields reads a Bitbucket user: the handle (login, then slug, then
// display name), the display name, and whether Bitbucket itself classifies
// the account as SERVICE (its built-in system user).
func userFields(user map[string]any) (handle, displayName string, service bool) {
	for _, k := range []string{"name", "slug", "displayName"} {
		if v, ok := user[k].(string); ok && v != "" {
			handle = v
			break
		}
	}
	displayName, _ = user["displayName"].(string)
	service = strings.ToUpper(pyStr(user, "type", "")) == "SERVICE"
	return handle, displayName, service
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// pyStr is Python's str(d.get(key, def)): the field's text whatever its JSON
// type, "None" for null.
func pyStr(m map[string]any, key, def string) string {
	v, ok := m[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case json.Number:
		return x.String()
	default:
		return ""
	}
}
