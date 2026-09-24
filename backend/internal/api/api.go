// Package api is the collector's HTTP surface: the four webhook sinks,
// /auth/ping and the probes. Handlers do HTTP, auth, dispatch to the pure
// extractors in package parse, config-derived fields, and persistence.
package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/httpapi"
	"github.com/trick77/riptide/internal/parse"
)

// Body size limits. Bitbucket pushes carry up to five commits with messages
// and PR events carry descriptions, so they get more room than the contracts
// riptide owns.
const (
	maxOwnedBody     = 1 << 20
	maxBitbucketBody = 10 << 20
)

// Store is the persistence the handlers need.
type Store interface {
	Ping(ctx context.Context) error
	InsertBitbucket(ctx context.Context, d *parse.BitbucketDraft, automationSource, team string) (bool, error)
	InsertPipeline(ctx context.Context, d *parse.PipelineDraft, team string) (bool, error)
	InsertArgoCD(ctx context.Context, d *parse.ArgoCDDraft, team string) (bool, error)
	InsertNoergler(ctx context.Context, d *parse.NoerglerDraft, team string) (bool, error)
}

// Deps is what the routes share.
type Deps struct {
	Store   Store
	Runtime *config.Runtime
	Log     *slog.Logger
	// Now is the clock for occurred_at fallbacks; time.Now when nil.
	Now func() time.Time
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Register adds every route to srv.
func Register(srv *httpapi.Server, d Deps) {
	srv.HandleFunc("GET /health", d.health)
	srv.HandleFunc("GET /ready", d.ready)
	srv.HandleFunc("GET /auth/ping", d.authPing)
	srv.HandleFunc("POST /webhooks/bitbucket/{team}", d.bitbucket)
	srv.HandleFunc("POST /webhooks/pipeline", d.pipeline)
	srv.HandleFunc("POST /webhooks/argocd", d.argocd)
	srv.HandleFunc("POST /webhooks/noergler", d.noergler)
	srv.HandleFunc("GET /openapi.yaml", d.openapiSpec)
	srv.HandleFunc("GET /docs", d.docs)
}

type statusBody struct {
	Status string `json:"status"`
}

type statusReasonBody struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type pingBody struct {
	Status string `json:"status"`
	Team   string `json:"team"`
}

type readyBody struct {
	Status                 string `json:"status"`
	Teams                  int    `json:"teams"`
	TeamKeys               int    `json:"team_keys"`
	ConfigReloadFailures   int64  `json:"config_reload_failures"`
	TeamKeysReloadFailures int64  `json:"team_keys_reload_failures"`
}

// health is liveness: 200 while the process is up.
func (d Deps) health(w http.ResponseWriter, _ *http.Request) {
	httpapi.WriteJSON(w, http.StatusOK, statusBody{Status: "ok"})
}

// ready is readiness: 503 while the database is unreachable. The cause goes
// to the log, not to the unauthenticated response.
func (d Deps) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := d.Store.Ping(ctx); err != nil {
		d.Log.ErrorContext(r.Context(), "readiness_db_unreachable", "error", err.Error())
		httpapi.WriteDetail(w, http.StatusServiceUnavailable, "db unreachable")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, readyBody{
		Status:                 "ok",
		Teams:                  len(d.Runtime.Config().Teams),
		TeamKeys:               len(d.Runtime.Keys().TeamNames()),
		ConfigReloadFailures:   d.Runtime.ConfigReloadFailures(),
		TeamKeysReloadFailures: d.Runtime.KeysReloadFailures(),
	})
}

// authPing lets a sender verify its token at startup: any of the team's
// secrets authenticates, and the answer names the team.
func (d Deps) authPing(w http.ResponseWriter, r *http.Request) {
	team, ok := d.bearer(w, r, "")
	if !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, pingBody{Status: "ok", Team: team})
}

// bearer authenticates `Authorization: Bearer <token>` against source's
// secrets ("" for any source) and returns the caller's team. On failure it
// has already written the 401.
func (d Deps) bearer(w http.ResponseWriter, r *http.Request, source string) (string, bool) {
	scheme, value, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	token := strings.TrimSpace(value)
	if !strings.EqualFold(scheme, "bearer") || token == "" {
		httpapi.WriteDetail(w, http.StatusUnauthorized, "Missing or malformed Authorization header.")
		return "", false
	}
	keys := d.Runtime.Keys()
	var team string
	var ok bool
	if source == "" {
		team, ok = keys.LookupAnySource(token)
	} else {
		team, ok = keys.Lookup(token, source)
	}
	if !ok {
		httpapi.WriteDetail(w, http.StatusUnauthorized, "Invalid credentials.")
		return "", false
	}
	return team, true
}

// dummySecret stands in for an unknown team's key, so rejecting an unknown
// team costs the same HMAC as rejecting a known one and team names cannot be
// enumerated by timing.
var dummySecret = strings.Repeat("\x00", 32)

// verifySignature checks `X-Hub-Signature: sha256=<hex>` over body.
func verifySignature(secret string, body []byte, header string) bool {
	prefix, sig, _ := strings.Cut(header, "=")
	if !strings.EqualFold(prefix, "sha256") || sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(sig)), []byte(expected)) == 1
}

// readBody reads at most limit bytes. On failure it has written the 413
// (or 400 for a broken read) already.
func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpapi.WriteDetail(w, http.StatusRequestEntityTooLarge, "payload too large")
		} else {
			httpapi.WriteDetail(w, http.StatusBadRequest, "could not read request body")
		}
		return nil, false
	}
	return body, true
}

// writeValidation answers a rejected body with FastAPI's 422 shape.
func writeValidation(w http.ResponseWriter, err error) {
	var ve *parse.ValidationError
	if errors.As(err, &ve) {
		httpapi.WriteJSON(w, http.StatusUnprocessableEntity, httpapi.Detail{Detail: ve.Errors})
		return
	}
	httpapi.WriteDetail(w, http.StatusUnprocessableEntity, err.Error())
}

// persistFailed logs a failed insert and answers 500. Never swallowed: the
// sender sees the failure and retries, and the retry dedupes. Bodies Postgres
// could never store are rejected by the parser before this point.
func (d Deps) persistFailed(w http.ResponseWriter, r *http.Request, source, deliveryID, team string, err error) {
	d.Log.ErrorContext(r.Context(), "webhook_persist_failed",
		"webhook_source", source, "delivery_id", deliveryID, "team", team, "error", err.Error())
	httpapi.WriteDetail(w, http.StatusInternalServerError, "Internal Server Error")
}

func outcome(inserted bool) string {
	if inserted {
		return "accepted"
	}
	return "deduped"
}
