package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the viewer's zone, whatever the image ships

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/httpapi"
	"github.com/trick77/riptide/internal/metrics"
	"github.com/trick77/riptide/web"
)

// Reader is what the read API needs from the database.
type Reader interface {
	Dataset(ctx context.Context, p dataset.Params) (*dataset.Dataset, error)
	// Shared reads what every window of one request shares, once.
	Shared(ctx context.Context, p dataset.SharedParams) (*dataset.Shared, error)
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
	httpapi.WriteJSON(w, http.StatusOK, Teams{Teams: d.Runtime.Config().TeamNames()})
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

// What a read is for: the numbers of a period, the facts tables, or the
// week running now (open PRs and counts: no lead times, reviews,
// environments or libraries).
type readKind int

const (
	readFull readKind = iota
	readFacts
	readRunning
	// readEarlier: PRs and deploys only, the window before the compared
	// periods the assessment's states look back into.
	readEarlier
)

// load reads the team's dataset from from to to, with sh, the request's
// shared reads, its deploys classed by the Apps as of classAt (zero: to).
// A nil sh reads them for this window alone.
func (d Deps) load(ctx context.Context, v view, from, to time.Time, kind readKind, sh *dataset.Shared,
	classAt time.Time) (*dataset.Dataset, error) {
	cfg := v.cfg
	return d.Reader.Dataset(ctx, dataset.Params{
		Team: v.team, From: from, To: to, ProductionStage: cfg.Environments.ProductionStage,
		// The detection a webhook's author gets at ingest, so a commit and a
		// PR by the same bot agree, plus the name rule only read applies. A
		// commit on master has no branch to match a prefix against, and the
		// SERVICE flag is the view's.
		Automated:  cfg.AutomatedAtRead,
		BotBranch:  cfg.BotBranch,
		Excluded:   cfg.ExcludedProject,
		Now:        v.now,
		FactsOnly:  kind != readFull,
		CountsOnly: kind == readRunning,
		Lean:       kind == readEarlier,
		Shared:     sh,
		ClassAt:    classAt,
	})
}

// periods reads the team's datasets over ps, the first one the period whose
// end classes them all (ClassAt), with the reads they share done once; the
// running week, when given, classed by its own end. Side by side; the first
// to fail cancels the others, and its error, not the cancellation, is
// returned. states: the assessment's read, whose shared read takes the
// approvals and whose periods past the second are the look-back of its
// states (readEarlier).
func (d Deps) periods(ctx context.Context, v view, ps []metrics.Period, running *metrics.Period,
	states bool) ([]*dataset.Dataset, *dataset.Dataset, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ends := []time.Time{ps[0].To}
	if running != nil {
		ends = append(ends, running.To)
	}
	from := ps[0].From
	for _, p := range ps {
		if p.From.Before(from) {
			from = p.From
		}
	}
	// The approvals from the period's start: the states judge its weeks; the
	// look-back reads merged PRs only.
	sh, err := d.Reader.Shared(ctx, dataset.SharedParams{Team: v.team, From: from, ClassEnds: ends, Now: v.now, Libraries: true,
		Trace: true, States: states, ApprovalsFrom: ps[0].From, Automated: v.cfg.AutomatedAtRead})
	if err != nil {
		return nil, nil, err
	}
	out := make([]*dataset.Dataset, len(ps))
	errs := make([]error, len(ps)+1)
	var run *dataset.Dataset
	var wg sync.WaitGroup
	for i, p := range ps {
		kind := readFull
		if states && i >= 2 {
			kind = readEarlier
		}
		wg.Go(func() {
			if out[i], errs[i] = d.load(ctx, v, p.From, p.To, kind, sh, ps[0].To); errs[i] != nil {
				cancel()
			}
		})
	}
	if running != nil {
		// Open PRs, releases, merges and runs: no lead times, reviews,
		// environments or libraries.
		wg.Go(func() {
			if run, errs[len(ps)] = d.load(ctx, v, running.From, running.To, readRunning, sh, running.To); errs[len(ps)] != nil {
				cancel()
			}
		})
	}
	wg.Wait()
	// The read that failed, not the one its failure cancelled.
	var first error
	for _, err := range errs {
		if err != nil && (first == nil || errors.Is(first, context.Canceled)) {
			first = err
		}
	}
	return out, run, first
}

// statusViewerLeft is nginx's 499 "client closed request".
const statusViewerLeft = 499

// viewerLeft reports a read cut short by its viewer (tab closed, team or
// period switched: the UI aborts) and answers it 499, no body: no failure,
// nothing to log, nobody to read it. The request's context is done and the
// read ended in a cancellation: context.Canceled, or Postgres' 57014
// query_canceled should pgx ever cancel by a cancel request instead of its
// default deadline (that one returns context.Canceled). A shutdown that
// cuts the request's context lands here too: accepted.
func viewerLeft(w http.ResponseWriter, r *http.Request, err error) bool {
	if r.Context().Err() == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if !errors.Is(err, context.Canceled) && (!errors.As(err, &pgErr) || pgErr.Code != pgerrQueryCanceled) {
		return false
	}
	w.WriteHeader(statusViewerLeft)
	return true
}

// pgerrQueryCanceled is Postgres' query_canceled.
const pgerrQueryCanceled = "57014"

// failed logs a dataset that would not load and answers 500, unless the
// viewer left.
func (d Deps) failed(w http.ResponseWriter, r *http.Request, team string, err error) {
	if viewerLeft(w, r, err) {
		return
	}
	d.serverError(w, r, "dataset_failed", err, "team", team)
}

// metrics serves every number of the page for one team and period.
func (d Deps) metrics(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	v, ok := d.readView(w, r)
	if !ok {
		return
	}
	// The period, the previous one to compare with, and the week running
	// now (open PRs as they are, its counts): three reads side by side, the
	// reads they share done once before. Not one longer read: lead times and
	// costs take the dataset's bounds as their window.
	ds, running, err := d.periods(r.Context(), v, []metrics.Period{v.params.Period, v.params.Previous}, &v.params.Running,
		false)
	if err != nil {
		d.failed(w, r, v.team, err)
		return
	}
	m := metrics.Compute(ds[0], ds[1], running, v.params)
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
	// The shared reads first, the tracing beside the rest, then the window.
	sh, err := d.Reader.Shared(r.Context(), dataset.SharedParams{Team: v.team, From: v.params.Period.From,
		ClassEnds: []time.Time{v.now}, Now: v.now, Libraries: true, Trace: true})
	var ds *dataset.Dataset
	if err == nil {
		ds, err = d.load(r.Context(), v, v.params.Period.From, v.now, readFacts, sh, v.now)
	}
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
