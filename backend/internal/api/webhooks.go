package api

import (
	"net/http"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/httpapi"
	"github.com/trick77/riptide/internal/parse"
)

// Every webhook request that authenticates and parses emits exactly one
// `webhook_processed` line: webhook_source, outcome (accepted | deduped |
// ignored | skipped), delivery_id and team, plus source-specific fields.

// bitbucket is the Bitbucket Data Center sink. HMAC-authenticated with the
// team's bitbucket secret; the team comes from the path.
func (d Deps) bitbucket(w http.ResponseWriter, r *http.Request) {
	team := r.PathValue("team")
	raw, ok := readBody(w, r, maxBitbucketBody)
	if !ok {
		return
	}
	secret, hasSecret := d.Runtime.Keys().Secret(team, config.SourceBitbucket)
	verifyWith := secret
	if !hasSecret {
		verifyWith = dummySecret
	}
	sig := r.Header.Get("X-Hub-Signature")
	if !verifySignature(verifyWith, raw, sig) || !hasSecret {
		d.Log.WarnContext(r.Context(), "hmac_rejected",
			"webhook_source", "bitbucket", "team", team, "has_secret", hasSecret, "has_signature", sig != "")
		httpapi.WriteDetail(w, http.StatusUnauthorized, "Invalid signature.")
		return
	}

	draft, skip := parse.Bitbucket(raw, parse.BitbucketHeaders{
		EventKey:    r.Header.Get("X-Event-Key"),
		RequestID:   r.Header.Get("X-Request-Id"),
		RequestUUID: r.Header.Get("X-Request-UUID"),
	}, d.now())
	if skip != nil {
		d.Log.InfoContext(r.Context(), "webhook_processed",
			"webhook_source", "bitbucket", "outcome", "skipped", "reason", skip.Reason,
			"delivery_id", skip.DeliveryID, "event_type", skip.EventType, "repo", skip.RepoFullName, "team", team)
		httpapi.WriteJSON(w, http.StatusAccepted, statusReasonBody{Status: "ignored", Reason: skip.Reason})
		return
	}
	if draft.DateUnparsed != "" {
		d.Log.WarnContext(r.Context(), "bitbucket_date_unparsed",
			"delivery_id", draft.DeliveryID, "date", draft.DateUnparsed, "team", team)
	}

	cfg := d.Runtime.Config()
	automation := cfg.DetectAutomationSource(deref(draft.Author), deref(draft.AuthorDisplayName),
		deref(draft.BranchName), draft.AuthorIsServiceAccount)
	inserted, err := d.Store.InsertBitbucket(r.Context(), draft, automation, team)
	if err != nil {
		d.persistFailed(w, r, "bitbucket", draft.DeliveryID, team, err)
		return
	}
	d.Log.InfoContext(r.Context(), "webhook_processed",
		"webhook_source", "bitbucket", "outcome", outcome(inserted), "delivery_id", draft.DeliveryID,
		"event_type", draft.EventType, "repo", draft.RepoFullName, "team", team)
	httpapi.WriteJSON(w, http.StatusAccepted, statusBody{Status: "accepted"})
}

// pipeline is the CI sink: Jenkins, Tekton and any other CI, told apart by
// the `source` field, all authenticated with the team's jenkins secret.
func (d Deps) pipeline(w http.ResponseWriter, r *http.Request) {
	team, ok := d.bearer(w, r, config.SourceJenkins)
	if !ok {
		return
	}
	raw, ok := readBody(w, r, maxOwnedBody)
	if !ok {
		return
	}
	draft, err := parse.Pipeline(raw)
	if err != nil {
		writeValidation(w, err)
		return
	}
	inserted, err := d.Store.InsertPipeline(r.Context(), draft, team)
	if err != nil {
		d.persistFailed(w, r, "pipeline", draft.DeliveryID, team, err)
		return
	}
	// `source` is Splunk-reserved, so the CI vendor travels as ci_system.
	d.Log.InfoContext(r.Context(), "webhook_processed",
		"webhook_source", "pipeline", "outcome", outcome(inserted), "delivery_id", draft.DeliveryID,
		"ci_system", draft.Source, "pipeline", draft.PipelineName, "run_id", draft.RunID,
		"phase", draft.Phase, "status", draft.Status, "team", team)
	httpapi.WriteJSON(w, http.StatusAccepted, statusBody{Status: "accepted"})
}

// argocd is the Argo CD notification sink. Deploys to a stage listed in
// environments.ignored_stages are acknowledged and dropped.
func (d Deps) argocd(w http.ResponseWriter, r *http.Request) {
	team, ok := d.bearer(w, r, config.SourceArgoCD)
	if !ok {
		return
	}
	raw, ok := readBody(w, r, maxOwnedBody)
	if !ok {
		return
	}
	draft, err := parse.ArgoCD(raw, d.now())
	if err != nil {
		writeValidation(w, err)
		return
	}
	if draft.Environment != nil && d.Runtime.Config().Environments.IgnoredStages[*draft.Environment] {
		d.Log.InfoContext(r.Context(), "webhook_processed",
			"webhook_source", "argocd", "outcome", "ignored", "reason", "stage_in_ignored_stages",
			"delivery_id", draft.DeliveryID, "app", draft.AppName, "revision", draft.Revision,
			"phase", draft.OperationPhase, "environment", draft.Environment,
			"destination_namespace", draft.DestinationNamespace, "team", team)
		httpapi.WriteJSON(w, http.StatusAccepted, statusBody{Status: "ignored"})
		return
	}
	inserted, err := d.Store.InsertArgoCD(r.Context(), draft, team)
	if err != nil {
		d.persistFailed(w, r, "argocd", draft.DeliveryID, team, err)
		return
	}
	d.Log.InfoContext(r.Context(), "webhook_processed",
		"webhook_source", "argocd", "outcome", outcome(inserted), "delivery_id", draft.DeliveryID,
		"app", draft.AppName, "revision", draft.Revision, "phase", draft.OperationPhase,
		"environment", draft.Environment, "team", team)
	httpapi.WriteJSON(w, http.StatusAccepted, statusBody{Status: "accepted"})
}

// noergler is the PR-review sink: per-PR finops rollups and
// reviewer-precision verdicts.
func (d Deps) noergler(w http.ResponseWriter, r *http.Request) {
	team, ok := d.bearer(w, r, config.SourceNoergler)
	if !ok {
		return
	}
	raw, ok := readBody(w, r, maxOwnedBody)
	if !ok {
		return
	}
	draft, err := parse.Noergler(raw)
	if err != nil {
		writeValidation(w, err)
		return
	}
	inserted, err := d.Store.InsertNoergler(r.Context(), draft, team)
	if err != nil {
		d.persistFailed(w, r, "noergler", draft.DeliveryID, team, err)
		return
	}
	d.Log.InfoContext(r.Context(), "webhook_processed",
		"webhook_source", "noergler", "outcome", outcome(inserted), "delivery_id", draft.DeliveryID,
		"event_type", draft.EventType, "team", team)
	httpapi.WriteJSON(w, http.StatusAccepted, statusBody{Status: "accepted"})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
