package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/trick77/ship/kit/logging"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/httpapi"
	"github.com/trick77/riptide/web"
)

// Reader is what the read API needs from the database.
type Reader interface {
	Dataset(ctx context.Context, p dataset.Params) (*dataset.Dataset, error)
}

// RegisterUI adds the read API and the UI itself to the UI listener's
// server. Never to the collector's: see serve in cmd/riptide.
func RegisterUI(srv *httpapi.Server, d Deps) {
	srv.HandleFunc("GET /api/teams", d.teams)
	srv.HandleFunc("GET /api/dataset", d.dataset)
	// Under /api nothing else exists. Explicit, because the SPA's catch-all
	// on "GET /" would otherwise answer an unknown /api path with the
	// shell's 404 instead of {"detail": ...}.
	// GET only: a method-less pattern would conflict with "GET /".
	srv.HandleFunc("GET /api/", func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteDetail(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))
	})
	srv.Handle("GET /", web.Handler())
}

// Teams is the body of GET /api/teams.
type Teams struct {
	Teams []string `json:"teams"`
}

// teams lists the configured teams, sorted, for the UI's team picker.
func (d Deps) teams(w http.ResponseWriter, _ *http.Request) {
	cfg := d.Runtime.Config()
	names := make([]string, 0, len(cfg.Teams))
	for name := range cfg.Teams {
		names = append(names, name)
	}
	slices.Sort(names)
	httpapi.WriteJSON(w, http.StatusOK, Teams{Teams: names})
}

// The window the dataset covers when the caller names none, and the widest
// it may ask for: a year plus a margin, so a year-over-year view fits.
const (
	defaultWindow = 28 * 24 * time.Hour
	maxWindow     = 400 * 24 * time.Hour
)

// dataset serves one team's facts for a window. No authentication here: the
// UI listener is reached through the auth proxy only.
func (d Deps) dataset(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	team := strings.TrimSpace(q.Get("team"))
	if team == "" {
		httpapi.WriteDetail(w, http.StatusUnprocessableEntity, "query parameter team is required")
		return
	}
	cfg := d.Runtime.Config()
	if _, ok := cfg.Teams[team]; !ok {
		httpapi.WriteDetail(w, http.StatusNotFound, "unknown team")
		return
	}
	now := d.now().UTC().Truncate(time.Second)
	to, ok := windowBound(w, q.Get("to"), "to", true, now)
	if !ok {
		return
	}
	from, ok := windowBound(w, q.Get("from"), "from", false, to.Add(-defaultWindow))
	if !ok {
		return
	}
	if !from.Before(to) {
		httpapi.WriteDetail(w, http.StatusUnprocessableEntity, "from must be before to")
		return
	}
	if to.Sub(from) > maxWindow {
		httpapi.WriteDetail(w, http.StatusUnprocessableEntity, "the window spans more than 400 days")
		return
	}

	started := time.Now()
	ds, err := d.Reader.Dataset(r.Context(), dataset.Params{
		Team: team, From: from, To: to, ProductionStage: cfg.Environments.ProductionStage,
		// The detection a webhook's author gets at ingest, so a commit and a
		// PR by the same bot agree. A commit on master has no branch to
		// match a prefix against, and the SERVICE flag is the view's.
		Automated: func(author, displayName string) bool {
			return cfg.DetectAutomationSource(author, displayName, "", false) != ""
		},
		Now: now,
	})
	if err != nil {
		d.Log.ErrorContext(r.Context(), "dataset_failed", "team", team, logging.Err(err))
		httpapi.WriteDetail(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	d.Log.InfoContext(r.Context(), "dataset_served", "team", team, "from", from, "to", to,
		"prs", len(ds.PRs), "deploys", len(ds.Deploys), "lead_times", len(ds.LeadTimes),
		"runs", len(ds.Runs), "reviews", len(ds.Reviews), "duration_ms", time.Since(started).Milliseconds())
	httpapi.WriteJSON(w, http.StatusOK, ds)
}

// windowBound parses a from/to parameter: RFC 3339, or a date (UTC). A date
// as `to` includes that day, so from=2026-01-01&to=2026-01-31 is January.
// Empty is def. On failure it has written the 422.
func windowBound(w http.ResponseWriter, v, name string, end bool, def time.Time) (time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return def, true
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse(time.DateOnly, v); err == nil {
		if end {
			t = t.AddDate(0, 0, 1)
		}
		return t, true
	}
	httpapi.WriteDetail(w, http.StatusUnprocessableEntity, name+" must be an RFC 3339 timestamp or a YYYY-MM-DD date")
	return time.Time{}, false
}
