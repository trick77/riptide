package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the viewer's zone, whatever the image ships

	"github.com/trick77/ship/kit/logging"

	"github.com/trick77/riptide/internal/config"
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
	srv.HandleFunc("GET /api/analysis", d.analysisStatus)
	srv.HandleFunc("POST /api/analysis", d.analysisAsk)
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
// product.
type view struct {
	team string
	now  time.Time
	// cfg is the config as the request found it: both periods read under
	// one, whatever a hot reload does meanwhile.
	cfg    *config.Config
	params metrics.Params
}

// readView parses the query the read endpoints share. On failure it has
// written the response. No authentication here: the UI listener is reached
// through the auth proxy only.
func (d Deps) readView(w http.ResponseWriter, r *http.Request) (view, bool) {
	q := r.URL.Query()
	team := strings.TrimSpace(q.Get("team"))
	if team == "" {
		httpapi.WriteDetail(w, http.StatusUnprocessableEntity, "query parameter team is required")
		return view{}, false
	}
	cfg := d.Runtime.Config()
	if _, ok := cfg.Teams[team]; !ok {
		httpapi.WriteDetail(w, http.StatusNotFound, "unknown team")
		return view{}, false
	}
	weeks := defaultWeeks
	if v := strings.TrimSpace(q.Get("weeks")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || !metrics.ValidWindow(n) {
			httpapi.WriteDetail(w, http.StatusUnprocessableEntity, badWeeks)
			return view{}, false
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
			return view{}, false
		}
		loc = l
	}
	now := d.now().UTC().Truncate(time.Second)
	// Closed weeks only, as the assessment reads them: a week still running
	// is counted apart (Running), never in a number or a comparison.
	return view{team: team, now: now, cfg: cfg, params: metrics.Params{
		Period: metrics.ClosedPeriod(weeks, now, loc), Previous: metrics.ClosedPrevious(weeks, now, loc),
		Running: metrics.RunningPeriod(now, loc),
		Product: strings.TrimSpace(q.Get("product")), JiraURL: cfg.JiraBaseURL, BitbucketURL: d.Onboarding.BitbucketURL,
	}}, true
}

// load reads the team's dataset from from to to.
func (d Deps) load(ctx context.Context, v view, from, to time.Time, factsOnly bool) (*dataset.Dataset, error) {
	cfg := v.cfg
	return d.Reader.Dataset(ctx, dataset.Params{
		Team: v.team, From: from, To: to, ProductionStage: cfg.Environments.ProductionStage,
		// The detection a webhook's author gets at ingest, so a commit and a
		// PR by the same bot agree. A commit on master has no branch to
		// match a prefix against, and the SERVICE flag is the view's.
		Automated: func(author, displayName string) bool {
			return cfg.DetectAutomationSource(author, displayName, "", false) != ""
		},
		Excluded:  cfg.ExcludedProject,
		Now:       v.now,
		FactsOnly: factsOnly,
	})
}

// failed logs a dataset that would not load and answers 500.
func (d Deps) failed(w http.ResponseWriter, r *http.Request, team string, err error) {
	d.Log.ErrorContext(r.Context(), "dataset_failed", "team", team, logging.Err(err))
	httpapi.WriteDetail(w, http.StatusInternalServerError, "Internal Server Error")
}

// metrics serves every number of the page for one team and period.
func (d Deps) metrics(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	v, ok := d.readView(w, r)
	if !ok {
		return
	}
	// The period, the previous one to compare with, and the week running
	// now (open PRs as they are, its counts): three reads side by side; the
	// first to fail cancels the others. Not one longer read: lead times and
	// costs take the dataset's bounds as their window.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var ds, prev, running *dataset.Dataset
	var errs [3]error
	var wg sync.WaitGroup
	read := func(p metrics.Period, factsOnly bool, out **dataset.Dataset, err *error) func() {
		return func() {
			if *out, *err = d.load(ctx, v, p.From, p.To, factsOnly); *err != nil {
				cancel()
			}
		}
	}
	wg.Go(read(v.params.Period, false, &ds, &errs[0]))
	wg.Go(read(v.params.Previous, false, &prev, &errs[1]))
	// Open PRs, releases, merges and runs: no lead times or reviews.
	wg.Go(read(v.params.Running, true, &running, &errs[2]))
	wg.Wait()
	// The read that failed, not the one its failure cancelled.
	var first error
	for _, err := range errs {
		if err != nil && (first == nil || errors.Is(first, context.Canceled)) {
			first = err
		}
	}
	if first != nil {
		d.failed(w, r, v.team, first)
		return
	}
	m := metrics.Compute(ds, prev, running, v.params)
	d.Log.InfoContext(r.Context(), "metrics_served", "team", v.team, "from", v.params.Period.From,
		"to", v.params.Period.To, "duration_ms", time.Since(started).Milliseconds())
	httpapi.WriteJSON(w, http.StatusOK, m)
}

// facts serves the facts tables for one team and period, searched.
func (d Deps) facts(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	v, ok := d.readView(w, r)
	if !ok {
		return
	}
	// The period's closed weeks and on to now: the running week's rows are
	// listed, marked, counted nowhere.
	ds, err := d.load(r.Context(), v, v.params.Period.From, v.now, true)
	if err != nil {
		d.failed(w, r, v.team, err)
		return
	}
	f := metrics.ComputeFacts(ds, v.params, r.URL.Query().Get("q"))
	d.Log.InfoContext(r.Context(), "facts_served", "team", v.team, "from", v.params.Period.From,
		"to", v.now, "prs", f.PRs.Count, "deploys", f.Deploys.Count, "runs", f.Runs.Count,
		"duration_ms", time.Since(started).Milliseconds())
	httpapi.WriteJSON(w, http.StatusOK, f)
}
