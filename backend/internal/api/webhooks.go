package api

import (
	"context"
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

// owned is a bearer-authenticated sink whose contract riptide owns.
type owned[D any] struct {
	source string // webhook_source
	secret string // the team secret that authenticates it
	parse  func(raw []byte) (D, error)
	// logged (required) is the draft's delivery_id and the source's own
	// webhook_processed fields, logged between delivery_id and team.
	logged func(draft D) (deliveryID string, fields []any)
	// ignore, when set, names the reason a draft is acknowledged and not
	// stored ("" stores it), with fields only its ignored line carries.
	ignore func(draft D) (reason string, fields []any)
	insert func(s Store, ctx context.Context, draft D, team string) (bool, error)
}

// serveOwned authenticates before it reads the body, so a bad body behind a
// bad key is a 401, then parses, stores and logs the one webhook_processed.
func serveOwned[D any](d Deps, w http.ResponseWriter, r *http.Request, s owned[D]) {
	team, ok := d.bearer(w, r, s.secret)
	if !ok {
		return
	}
	raw, ok := readBody(w, r, maxOwnedBody)
	if !ok {
		return
	}
	draft, err := s.parse(raw)
	if err != nil {
		writeValidation(w, err)
		return
	}
	deliveryID, fields := s.logged(draft)
	processed := func(outcome string, lead, tail []any) {
		args := append([]any{"webhook_source", s.source, "outcome", outcome}, lead...)
		args = append(append(args, "delivery_id", deliveryID), fields...)
		args = append(append(args, tail...), "team", team)
		d.Log.InfoContext(r.Context(), "webhook_processed", args...)
	}
	if s.ignore != nil {
		if reason, extra := s.ignore(draft); reason != "" {
			processed("ignored", []any{"reason", reason}, extra)
			httpapi.WriteJSON(w, http.StatusAccepted, statusBody{Status: "ignored"})
			return
		}
	}
	inserted, err := s.insert(d.Store, r.Context(), draft, team)
	if err != nil {
		d.persistFailed(w, r, s.source, deliveryID, team, err)
		return
	}
	processed(outcome(inserted), nil, nil)
	httpapi.WriteJSON(w, http.StatusAccepted, statusBody{Status: "accepted"})
}

// pipeline is the CI sink: Jenkins, Tekton and any other CI, told apart by
// the `source` field, all authenticated with the team's jenkins secret.
func (d Deps) pipeline(w http.ResponseWriter, r *http.Request) {
	serveOwned(d, w, r, owned[*parse.PipelineDraft]{
		source: "pipeline",
		secret: config.SourceJenkins,
		parse:  parse.Pipeline,
		insert: Store.InsertPipeline,
		logged: func(draft *parse.PipelineDraft) (string, []any) {
			// `source` is Splunk-reserved, so the CI vendor travels as ci_system.
			return draft.DeliveryID, []any{"ci_system", draft.Source, "pipeline", draft.PipelineName,
				"run_id", draft.RunID, "phase", draft.Phase, "status", draft.Status}
		},
	})
}

// argocd is the Argo CD notification sink. Deploys to a stage listed in
// environments.ignored_stages are acknowledged and dropped.
func (d Deps) argocd(w http.ResponseWriter, r *http.Request) {
	serveOwned(d, w, r, owned[*parse.ArgoCDDraft]{
		source: "argocd",
		secret: config.SourceArgoCD,
		parse: func(raw []byte) (*parse.ArgoCDDraft, error) {
			return parse.ArgoCD(raw, d.now())
		},
		logged: func(draft *parse.ArgoCDDraft) (string, []any) {
			return draft.DeliveryID, []any{"app", draft.AppName, "revision", draft.Revision,
				"phase", draft.OperationPhase, "environment", draft.Environment}
		},
		ignore: func(draft *parse.ArgoCDDraft) (string, []any) {
			if draft.Environment == nil || !d.Runtime.Config().Environments.IgnoredStages[*draft.Environment] {
				return "", nil
			}
			return "stage_in_ignored_stages", []any{"destination_namespace", draft.DestinationNamespace}
		},
		insert: Store.InsertArgoCD,
	})
}

// noergler is the PR-review sink: per-PR finops rollups and
// reviewer-precision verdicts.
func (d Deps) noergler(w http.ResponseWriter, r *http.Request) {
	serveOwned(d, w, r, owned[*parse.NoerglerDraft]{
		source: "noergler",
		secret: config.SourceNoergler,
		parse:  parse.Noergler,
		insert: Store.InsertNoergler,
		logged: func(draft *parse.NoerglerDraft) (string, []any) {
			return draft.DeliveryID, []any{"event_type", draft.EventType}
		},
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
