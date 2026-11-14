package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the viewer's zone, whatever the image ships

	"github.com/trick77/ship/kit/logging"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/httpapi"
	"github.com/trick77/riptide/internal/metrics"
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
	srv.HandleFunc("GET /api/metrics", d.metrics)
	srv.HandleFunc("GET /api/facts", d.facts)
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

// defaultWeeks is the period when the caller names none.
const defaultWeeks = 4

// badWeeks is the 422 detail for an unknown period, the windows listed.
var badWeeks = func() string {
	w := make([]string, len(metrics.Windows))
	for i, n := range metrics.Windows {
		w[i] = strconv.Itoa(n)
	}
	return "weeks must be one of " + strings.Join(w, ", ")
}()

// zones caches loaded time zones by name: time.LoadLocation reads and
// parses the zone on every call, and every filter change asks twice.
var zones sync.Map

func loadZone(name string) (*time.Location, error) {
	if l, ok := zones.Load(name); ok {
		return l.(*time.Location), nil
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	zones.Store(name, l)
	return l, nil
}

// view is a read request's parsed query: whose data, which period, which
// product and bots.
type view struct {
	team   string
	params metrics.Params
}

// readView parses the query the read endpoints share and loads the
// team's dataset for the period. On failure it has written the response.
// No authentication here: the UI listener is reached through the auth
// proxy only.
func (d Deps) readView(w http.ResponseWriter, r *http.Request, factsOnly bool) (view, *dataset.Dataset, bool) {
	q := r.URL.Query()
	team := strings.TrimSpace(q.Get("team"))
	if team == "" {
		httpapi.WriteDetail(w, http.StatusUnprocessableEntity, "query parameter team is required")
		return view{}, nil, false
	}
	cfg := d.Runtime.Config()
	if _, ok := cfg.Teams[team]; !ok {
		httpapi.WriteDetail(w, http.StatusNotFound, "unknown team")
		return view{}, nil, false
	}
	weeks := defaultWeeks
	if v := strings.TrimSpace(q.Get("weeks")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || !metrics.ValidWindow(n) {
			httpapi.WriteDetail(w, http.StatusUnprocessableEntity, badWeeks)
			return view{}, nil, false
		}
		weeks = n
	}
	// Weeks and their numbers are the viewer's calendar: Monday 00:00 in
	// the browser's zone.
	loc := time.UTC
	if v := strings.TrimSpace(q.Get("tz")); v != "" {
		l, err := loadZone(v)
		if err != nil {
			httpapi.WriteDetail(w, http.StatusUnprocessableEntity, "tz must be an IANA time zone")
			return view{}, nil, false
		}
		loc = l
	}
	var bots bool
	switch q.Get("bots") {
	case "", "0", "false":
	case "1", "true":
		bots = true
	default:
		httpapi.WriteDetail(w, http.StatusUnprocessableEntity, "bots must be 0 or 1")
		return view{}, nil, false
	}
	now := d.now().UTC().Truncate(time.Second)
	period := metrics.NewPeriod(weeks, now, loc)
	ds, err := d.Reader.Dataset(r.Context(), dataset.Params{
		Team: team, From: period.From, To: period.To, ProductionStage: cfg.Environments.ProductionStage,
		// The detection a webhook's author gets at ingest, so a commit and a
		// PR by the same bot agree. A commit on master has no branch to
		// match a prefix against, and the SERVICE flag is the view's.
		Automated: func(author, displayName string) bool {
			return cfg.DetectAutomationSource(author, displayName, "", false) != ""
		},
		Now:       now,
		FactsOnly: factsOnly,
	})
	if err != nil {
		d.Log.ErrorContext(r.Context(), "dataset_failed", "team", team, logging.Err(err))
		httpapi.WriteDetail(w, http.StatusInternalServerError, "Internal Server Error")
		return view{}, nil, false
	}
	return view{team: team, params: metrics.Params{
		Period: period, Product: strings.TrimSpace(q.Get("product")), Bots: bots,
	}}, ds, true
}

// metrics serves every number of the page for one team and period.
func (d Deps) metrics(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	v, ds, ok := d.readView(w, r, false)
	if !ok {
		return
	}
	m := metrics.Compute(ds, v.params)
	d.Log.InfoContext(r.Context(), "metrics_served", "team", v.team, "from", v.params.Period.From,
		"to", v.params.Period.To, "duration_ms", time.Since(started).Milliseconds())
	httpapi.WriteJSON(w, http.StatusOK, m)
}

// facts serves the facts tables for one team and period, searched.
func (d Deps) facts(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	v, ds, ok := d.readView(w, r, true)
	if !ok {
		return
	}
	f := metrics.ComputeFacts(ds, v.params, r.URL.Query().Get("q"))
	d.Log.InfoContext(r.Context(), "facts_served", "team", v.team, "from", v.params.Period.From,
		"to", v.params.Period.To, "prs", f.PRs.Count, "deploys", f.Deploys.Count, "runs", f.Runs.Count,
		"duration_ms", time.Since(started).Milliseconds())
	httpapi.WriteJSON(w, http.StatusOK, f)
}
