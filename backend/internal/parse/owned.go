package parse

import (
	"strings"
	"time"
)

// Account kinds a sender may declare for its own git-host account.
var accountKinds = []string{"bot", "service", "human"}

// PipelineDraft is a CI event from POST /webhooks/pipeline. Every CI lands
// here, told apart by Source.
type PipelineDraft struct {
	DeliveryID   string
	Source       string
	PipelineName string
	RunID        string
	Phase        string
	Status       *string
	CommitSHA    string // lowercased
	ImageRef     *string
	ActorHandle  *string
	// ActorAccountKind is nil without a handle: the kind describes the
	// handle, and there is nothing for it to describe.
	ActorAccountKind *string
	StartedAt        time.Time
	FinishedAt       *time.Time
	OccurredAt       time.Time
	Payload          []byte
}

// Pipeline validates a pipeline body. Unknown fields are allowed and
// kept in the payload.
func Pipeline(raw []byte) (*PipelineDraft, error) {
	o, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	d := &PipelineDraft{
		Source:       o.requiredString("source", 1),
		PipelineName: o.requiredString("pipeline_name", 1),
		RunID:        o.requiredString("run_id", 1),
		Phase:        o.requiredString("phase", 1),
		Status:       o.optionalString("status", 0),
		CommitSHA:    strings.ToLower(o.requiredString("commit_sha", 7)),
		ImageRef:     o.optionalTrimmed("image_ref"),
		ActorHandle:  o.optionalTrimmed("actor_handle"),
		StartedAt:    o.requiredTime("started_at"),
		FinishedAt:   o.optionalTime("finished_at", false),
		Payload:      raw,
	}
	kind := o.literal("actor_account_kind", "service", accountKinds...)
	o.checkSpan("finished_at", &d.StartedAt, d.FinishedAt)
	if err := o.err(); err != nil {
		return nil, err
	}
	if d.ActorHandle != nil {
		d.ActorAccountKind = &kind
	}
	// Source is part of the key so a Jenkins job and a Tekton pipeline that
	// share a name do not collide.
	d.DeliveryID = d.Source + "#" + d.PipelineName + "#" + d.RunID + "#" + d.Phase
	d.OccurredAt = d.StartedAt
	if d.FinishedAt != nil {
		d.OccurredAt = *d.FinishedAt
	}
	return d, nil
}

// ArgoCDDraft is a sync notification from POST /webhooks/argocd.
type ArgoCDDraft struct {
	DeliveryID           string
	AppName              string
	Revision             string // lowercased; the GitOps-repo SHA
	SyncStatus           *string
	OperationPhase       *string
	StartedAt            *time.Time
	FinishedAt           *time.Time
	DestinationNamespace *string // trimmed
	Environment          *string
	OccurredAt           time.Time
	Payload              []byte
}

// ArgoCD validates an Argo CD body. `images` is required (it may be
// empty) because it is the bridge from a deploy to the build that produced
// it; it is read from the payload, not stored in a column. now is the
// occurred_at fallback when the body has no timestamps.
func ArgoCD(raw []byte, now time.Time) (*ArgoCDDraft, error) {
	o, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	d := &ArgoCDDraft{
		AppName:              o.requiredString("app_name", 1),
		Revision:             strings.ToLower(o.requiredString("revision", 7)),
		SyncStatus:           o.optionalString("sync_status", 0),
		OperationPhase:       o.optionalString("operation_phase", 0),
		StartedAt:            o.optionalTime("started_at", false),
		FinishedAt:           o.optionalTime("finished_at", false),
		DestinationNamespace: o.optionalTrimmed("destination_namespace"),
		Payload:              raw,
	}
	o.stringList("images", true)
	o.checkSpan("finished_at", d.StartedAt, d.FinishedAt)
	if err := o.err(); err != nil {
		return nil, err
	}
	if d.DestinationNamespace != nil {
		d.Environment = ptr(Environment(*d.DestinationNamespace))
	}
	started, phase := "unknown", "unknown"
	if d.StartedAt != nil {
		started = isoFormat(*d.StartedAt)
	}
	if d.OperationPhase != nil {
		phase = *d.OperationPhase
	}
	d.DeliveryID = d.AppName + "#" + d.Revision + "#" + started + "#" + phase
	switch {
	case d.FinishedAt != nil:
		d.OccurredAt = *d.FinishedAt
	case d.StartedAt != nil:
		d.OccurredAt = *d.StartedAt
	default:
		d.OccurredAt = now.UTC()
	}
	return d, nil
}

// Noergler event types. Historical rows may carry `completed` (pre per-PR
// rollup); it is no longer accepted.
const (
	NoerglerPRCompleted = "pr_completed"
	NoerglerFeedback    = "feedback"
)

// NoerglerDraft is one row for noergler_events: a per-PR finops rollup or a
// reviewer-precision verdict. Noergler never re-emits PR lifecycle; the
// Bitbucket events cover open / merged / declined.
type NoerglerDraft struct {
	DeliveryID string
	EventType  string
	PRKey      string  // lowercased
	Repo       *string // lowercased
	CommitSHA  *string // lowercased
	OccurredAt time.Time
	Payload    []byte

	// pr_completed
	Outcome             *string
	ReviewerHandle      *string
	ReviewerAccountKind *string
	MergeCommitSHA      *string
	LinesAdded          *int64
	LinesRemoved        *int64
	FilesChanged        *int64
	TotalRuns           *int64
	ModelsUsed          []string
	FirstReviewAt       *time.Time
	PromptTokens        *int64
	CompletionTokens    *int64
	ElapsedMS           *int64
	FindingsCount       *int64
	CostUSD             *string

	// feedback
	FindingID *string
	Verdict   *string
	Actor     *string // lowercased
}

// Noergler validates a noergler body. Strict: unknown fields are a 422
// so a sender typo does not land silently in the payload column.
func Noergler(raw []byte) (*NoerglerDraft, error) {
	o, err := decodeObject(raw)
	if err != nil {
		return nil, err
	}
	eventType, _, typed := o.rawString("event_type")
	if !typed {
		return nil, o.err()
	}
	switch eventType {
	case NoerglerPRCompleted:
		return parsePRCompleted(o, raw)
	case NoerglerFeedback:
		return parseFeedback(o, raw)
	case "":
		if _, ok := o.fields["event_type"]; !ok {
			o.fail("event_type", "union_tag_not_found", "Unable to extract tag using discriminator 'event_type'")
			return nil, o.err()
		}
	}
	o.fail("event_type", "union_tag_invalid", "Input tag '"+eventType+"' found using 'event_type' does not match any of the expected tags: 'pr_completed', 'feedback'")
	return nil, o.err()
}

func parsePRCompleted(o *object, raw []byte) (*NoerglerDraft, error) {
	o.forbidExtra("event_type", "outcome", "pr_key", "repo", "reviewer_handle", "reviewer_account_kind",
		"source_commit_sha", "merge_commit_sha", "lines_added", "lines_removed", "files_changed",
		"total_runs", "total_prompt_tokens", "total_completion_tokens", "total_elapsed_ms",
		"total_findings_count", "total_cost_usd", "models_used", "first_review_at", "closed_at")
	outcome := o.literal("outcome", "", "merged", "declined", "deleted")
	prKey := o.requiredString("pr_key", 1)
	repo := o.requiredString("repo", 1)
	handle := o.optionalTrimmed("reviewer_handle")
	kind := o.literal("reviewer_account_kind", "bot", accountKinds...)
	source := o.requiredString("source_commit_sha", 7)
	merge := o.optionalString("merge_commit_sha", 7)
	int64p := func(field string, minimum int64) *int64 { v := o.integer(field, minimum); return &v }
	d := &NoerglerDraft{
		EventType:        NoerglerPRCompleted,
		LinesAdded:       int64p("lines_added", 0),
		LinesRemoved:     int64p("lines_removed", 0),
		FilesChanged:     int64p("files_changed", 0),
		TotalRuns:        int64p("total_runs", 1),
		PromptTokens:     int64p("total_prompt_tokens", 0),
		CompletionTokens: int64p("total_completion_tokens", 0),
		ElapsedMS:        int64p("total_elapsed_ms", 0),
		FindingsCount:    int64p("total_findings_count", 0),
		CostUSD:          o.optionalDecimal("total_cost_usd"),
		Payload:          raw,
	}
	if models, ok := o.stringList("models_used", true); ok {
		switch {
		case len(models) == 0:
			o.fail("models_used", "too_short", "List should have at least 1 item after validation, not 0")
		default:
			for _, m := range models {
				if strings.TrimSpace(m) == "" {
					o.fail("models_used", "value_error", "Value error, models_used entries must be non-empty strings")
					break
				}
			}
		}
		d.ModelsUsed = models
	}
	d.FirstReviewAt = o.optionalTime("first_review_at", true)
	closed := o.requiredTime("closed_at")
	// Only a merged PR has a merge commit, and a merged PR must report it.
	if outcome == "merged" && merge == nil && !o.hasError("merge_commit_sha") {
		o.fail("merge_commit_sha", "value_error", "Value error, merge_commit_sha is required when outcome='merged'")
	}
	if outcome != "" && outcome != "merged" && merge != nil {
		o.fail("merge_commit_sha", "value_error", "Value error, merge_commit_sha must be null when outcome='"+outcome+"'")
	}
	if err := o.err(); err != nil {
		return nil, err
	}
	d.PRKey = strings.ToLower(prKey)
	d.Repo = ptr(strings.ToLower(repo))
	d.CommitSHA = ptr(strings.ToLower(source))
	d.Outcome = &outcome
	// The handle stays case-preserved: it is matched against the git host's
	// own user handles, which riptide stores as delivered.
	d.ReviewerHandle = handle
	if handle != nil {
		d.ReviewerAccountKind = &kind
	}
	if merge != nil {
		d.MergeCommitSHA = ptr(strings.ToLower(*merge))
	}
	d.OccurredAt = closed
	// One terminal outcome per PR, redelivered on retries. The key includes
	// the outcome so deleted-after-declined lands two rows; pr_key is
	// lowercased so a casing-flipped redelivery still dedupes.
	d.DeliveryID = NoerglerPRCompleted + "#" + d.PRKey + "#" + outcome
	return d, nil
}

func parseFeedback(o *object, raw []byte) (*NoerglerDraft, error) {
	o.forbidExtra("event_type", "pr_key", "finding_id", "verdict", "actor", "repo", "commit_sha", "occurred_at")
	prKey := o.requiredString("pr_key", 1)
	findingID := o.requiredString("finding_id", 1)
	verdict := o.literal("verdict", "", "disagreed", "acknowledged")
	actor := o.requiredString("actor", 1)
	repo := o.optionalString("repo", 1)
	commit := o.optionalString("commit_sha", 7)
	occurred := o.requiredTime("occurred_at")
	if err := o.err(); err != nil {
		return nil, err
	}
	d := &NoerglerDraft{
		EventType:  NoerglerFeedback,
		PRKey:      strings.ToLower(prKey),
		FindingID:  &findingID,
		Verdict:    &verdict,
		Actor:      ptr(strings.ToLower(actor)),
		OccurredAt: occurred,
		Payload:    raw,
	}
	if repo != nil {
		d.Repo = ptr(strings.ToLower(*repo))
	}
	if commit != nil {
		d.CommitSHA = ptr(strings.ToLower(*commit))
	}
	// finding_id is stable per finding and never normalised; the verdict is
	// in the key so a flip lands a second row.
	d.DeliveryID = NoerglerFeedback + "#" + findingID + "#" + verdict
	return d, nil
}
