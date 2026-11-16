package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/trick77/ship/kit/logging"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/metrics"
	"github.com/trick77/riptide/internal/store"
)

// Store is what the service keeps analyses in.
type Store interface {
	ClaimAnalysis(ctx context.Context, c store.AnalysisClaim) (int64, bool, error)
	FinishAnalysis(ctx context.Context, id int64, r store.AnalysisResult) error
	FailAnalysis(ctx context.Context, id int64, r store.AnalysisResult) error
	DoneAnalyses(ctx context.Context, v store.AnalysisView, limit int) ([]store.Analysis, error)
	LatestAttempt(ctx context.Context, v store.AnalysisView) (*store.Analysis, error)
	ScheduledAttempt(ctx context.Context, v store.AnalysisView, to time.Time) (*store.Analysis, error)
}

// Loader reads a team's dataset over a period, as the read API does.
type Loader func(ctx context.Context, team string, p metrics.Period) (*dataset.Dataset, error)

// Triggers.
const (
	TriggerScheduled = "scheduled"
	TriggerManual    = "manual"
)

// States of a view's analysis on the page.
const (
	// StateFresh: an analysis of the last closed week.
	StateFresh = "fresh"
	// StateOlder: an older one, within the window's MaxAge.
	StateOlder = "older"
	// StateNone: none to show; the page shows the assessment alone.
	StateNone = "none"
	// StateOff: no model configured.
	StateOff = "off"
)

const (
	// Cooldown is how long after a manual attempt the same view takes
	// none: one click per view and ten minutes, whoever clicks (the UI
	// listener knows nobody).
	Cooldown = 10 * time.Minute
	// MaxAttempts is how often a failed scheduled run is tried, on later
	// ticks.
	MaxAttempts = 3
	// StaleAfter is how long an attempt runs before it counts as died with
	// its pod.
	StaleAfter = 30 * time.Minute
	// historyDepth is how many earlier closed weeks tell what recurs.
	historyDepth = 8
	// historyRowsPerWeek bounds the rows read for them: a scheduled run
	// and a few clicks a week.
	historyRowsPerWeek = 6
)

// MaxAge is how many weeks older than the last closed week a window's
// analysis may be and still show: its schedule's gap (4 weeks weekly, 12
// every second week, 26 monthly, user 2026-11-16).
func MaxAge(weeks int) int {
	switch weeks {
	case 12:
		return 2
	case 26:
		return 5
	}
	return 1
}

// View is a page view: team, window, product.
type View = store.AnalysisView

// Service runs and serves the analyses.
type Service struct {
	Store Store
	Load  Loader
	// LLM is nil when no model is configured: the page shows the
	// assessment alone.
	LLM LLM
	// Loc is the calendar the closed weeks are cut in.
	Loc *time.Location
	Log *slog.Logger
	Now func() time.Time

	// base is the context manual runs outlive their request on.
	base context.Context
	// gate keeps one model call at a time across the service.
	gate chan struct{}
	// inflight are the views with a run under way here.
	inflight sync.Map
	// backoff holds, per view, when the schedule tries a failed run again.
	backoff sync.Map
	// cache keeps a view's assessment of the current closed weeks briefly:
	// the page polls while a run is under way, and the weeks are closed.
	cache sync.Map
	// products holds, per team, window and week, the products the schedule
	// words: read once a week, not every tick.
	products sync.Map
	// settledAt holds, per view, the week its scheduled run settled.
	settledAt sync.Map
	// compared holds Compared per analysis id for rows stored before it.
	compared sync.Map
	// manual are the start times of the recent manual runs, all views.
	manualMu sync.Mutex
	manual   []time.Time
}

// Start binds the service to ctx, the process's: manual runs end with it.
func (s *Service) Start(ctx context.Context) {
	s.base = ctx
	s.gate = make(chan struct{}, 1)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// periods are a view's closed weeks now and the ones before.
func (s *Service) periods(weeks int) (cur, prev metrics.Period) {
	n := s.now()
	return metrics.ClosedPeriod(weeks, n, s.Loc), metrics.ClosedPrevious(weeks, n, s.Loc)
}

// assess is the view's assessment over its closed weeks, with what the
// analyses before period's end raised.
func (s *Service) assess(ctx context.Context, v View) (metrics.Assessment, error) {
	cur, prev := s.periods(v.Weeks)
	var ds, pds *dataset.Dataset
	var errs [2]error
	var wg sync.WaitGroup
	wg.Go(func() { ds, errs[0] = s.load(ctx, v.Team, cur) })
	wg.Go(func() { pds, errs[1] = s.load(ctx, v.Team, prev) })
	wg.Wait()
	if err := errors.Join(errs[:]...); err != nil {
		return metrics.Assessment{}, err
	}
	h, err := s.history(ctx, v, cur.To)
	if err != nil {
		return metrics.Assessment{}, err
	}
	return metrics.Assess(ds, pds, metrics.Params{Period: cur, Previous: prev, Product: v.Product}, h), nil
}

// history reads the view's done analyses before to: the latest one's
// signals, and how many in a row before to raised each.
func (s *Service) history(ctx context.Context, v View, to time.Time) (metrics.History, error) {
	h := metrics.History{Seen: map[string]int{}, Latest: map[string]string{}}
	// Rows, not weeks: a week holds its scheduled run and any number of
	// clicks, and the current week's reruns are skipped. Room for several
	// per week; the weeks are cut to historyDepth below.
	rows, err := s.Store.DoneAnalyses(ctx, v, historyDepth*historyRowsPerWeek)
	if err != nil {
		return h, err
	}
	type run struct {
		to  time.Time
		ids map[string]string
	}
	var runs []run
	for _, r := range rows {
		// One per closed week, the newest of it; the current week's own
		// reruns are no history.
		if !r.PeriodTo.Before(to) || (len(runs) > 0 && r.PeriodTo.Equal(runs[len(runs)-1].to)) {
			continue
		}
		if len(runs) == historyDepth {
			break
		}
		var a metrics.Assessment
		if err := json.Unmarshal(r.Assessment, &a); err != nil {
			continue
		}
		ids := map[string]string{}
		for _, sg := range a.Signals {
			ids[sg.ID] = sg.Tone
		}
		runs = append(runs, run{r.PeriodTo, ids})
	}
	if len(runs) == 0 {
		return h, nil
	}
	h.Latest, h.Runs = runs[0].ids, len(runs)
	// In a row means on the window's schedule: a run missed (failed, the
	// pod down) is a gap, and the streak ends there.
	gap := time.Duration(MaxAge(v.Weeks)) * 7 * 24 * time.Hour
	for id := range runs[0].ids {
		n, at := 0, to
		for _, r := range runs {
			if _, ok := r.ids[id]; !ok || at.Sub(r.to) > gap+time.Hour {
				break
			}
			n, at = n+1, r.to
		}
		h.Seen[id] = n
	}
	return h, nil
}

// Digest is the model's input for the view now, repos aliased, as Run
// would send it: for a dry run.
func (s *Service) Digest(ctx context.Context, v View) ([]byte, error) {
	a, err := s.assess(ctx, v)
	if err != nil {
		return nil, err
	}
	d, _, err := buildDigest(a)
	return d, err
}

// errBusy: the view has a run under way, or a manual one too recent.
var errBusy = errors.New("an analysis of this view is under way or was just asked for")

// errOff: no model configured.
var errOff = errors.New("no analysis model configured")

// errQuiet: a scheduled product view without a repo active in the window.
var errQuiet = errors.New("no activity in the product")

// quiet reports whether a product view has no repo with a merged PR, a run
// or an open PR in the window: the schedule leaves it out.
func quiet(v View, a metrics.Assessment) bool { return v.Product != "" && a.Active == 0 }

// IsOff reports whether err is the refusal of a service without a model.
func IsOff(err error) bool { return errors.Is(err, errOff) }

// Run assesses the view, words it and stores it. A scheduled run is one
// per view and closed week across replicas; an unchanged input is not
// asked again.
func (s *Service) Run(ctx context.Context, v View, trigger string) error {
	if s.LLM == nil {
		return errOff
	}
	key := viewKey(v)
	if _, busy := s.inflight.LoadOrStore(key, true); busy {
		return errBusy
	}
	defer s.inflight.Delete(key)
	return s.run(ctx, v, trigger)
}

// run is Run with the view's inflight mark already held.
func (s *Service) run(ctx context.Context, v View, trigger string) error {
	started := time.Now()
	log := s.Log.With("team", v.Team, "weeks", v.Weeks, "product", v.Product, "trigger", trigger,
		"model", s.LLM.Model())

	a, err := s.assess(ctx, v)
	if err != nil {
		log.ErrorContext(ctx, "analysis_failed", "stage", "assess", logging.Err(err))
		return err
	}
	// A product listed only for bots' PRs, or data outside what counts,
	// has nothing to word: no paid call on the schedule.
	if trigger == TriggerScheduled && quiet(v, a) {
		log.InfoContext(ctx, "analysis_skipped", "reason", "no_activity")
		return errQuiet
	}
	digest, al, err := buildDigest(a)
	if err != nil {
		log.ErrorContext(ctx, "analysis_failed", "stage", "digest", logging.Err(err))
		return err
	}
	hash := inputHash(s.LLM.Model(), digest)
	// A click on unchanged data shows what is there. A scheduled run still
	// runs: skipped, it would leave no row, and every tick would look again.
	if done, err := s.Store.DoneAnalyses(ctx, v, 1); trigger == TriggerManual && err == nil && len(done) == 1 &&
		done[0].PeriodTo.Equal(a.Period.To) && done[0].InputHash != nil && *done[0].InputHash == hash {
		log.InfoContext(ctx, "analysis_skipped", "reason", "input_unchanged", "id", done[0].ID)
		// Nothing asked: the hour's manual slot goes back.
		s.releaseManual()
		return nil
	}
	id, ok, err := s.Store.ClaimAnalysis(ctx, store.AnalysisClaim{
		View: v, From: a.Period.From, To: a.Period.To, Trigger: trigger, Model: s.LLM.Model(), At: s.now(),
		MaxAttempts: MaxAttempts, StaleAfter: StaleAfter,
	})
	if err != nil || !ok {
		return err
	}
	// From the claim on, every way out ends the row: one left running shows
	// a spinner and is claimed, and paid for, again.
	res := store.AnalysisResult{InputHash: &hash}
	if res.Assessment, err = json.Marshal(a); err != nil {
		return s.fail(ctx, log, id, res, err, started)
	}

	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return s.fail(ctx, log, id, res, ctx.Err(), started)
	}
	call, err := s.LLM.Complete(ctx, promptText, string(digest))
	<-s.gate
	res.InputTokens, res.OutputTokens, res.CostNanoUSD = call.InputTokens, call.OutputTokens, call.NanoUSD
	if err != nil {
		return s.fail(ctx, log, id, res, err, started)
	}
	out, report, err := validate(call.Content, a, al)
	if rep, e := json.Marshal(report); e == nil {
		res.Report = rep
	}
	if err != nil {
		return s.fail(ctx, log, id, res, fmt.Errorf("finish %s: %w", call.FinishReason, err), started)
	}
	if res.Output, err = json.Marshal(out); err != nil {
		return s.fail(ctx, log, id, res, err, started)
	}
	res.At = s.now()
	if err := s.Store.FinishAnalysis(ctx, id, res); err != nil {
		// Paid for: keep what it cost on the row, failed rather than running.
		res.Output = nil
		return s.fail(ctx, log, id, res, fmt.Errorf("store: %w", err), started)
	}
	if len(report.Missed) > 0 {
		log.WarnContext(ctx, "analysis_signal_missed", "id", id, "signals", report.Missed)
	}
	log.InfoContext(ctx, "analysis_completed", "id", id, "input_tokens", deref(call.InputTokens),
		"output_tokens", deref(call.OutputTokens), "cost_nano_usd", deref(call.NanoUSD), "dropped",
		len(report.Dropped), "duration_ms", time.Since(started).Milliseconds())
	return nil
}

func (s *Service) fail(ctx context.Context, log *slog.Logger, id int64, res store.AnalysisResult, err error,
	started time.Time) error {
	msg := err.Error()
	res.Error, res.At = &msg, s.now()
	// The process's own context may be the one that ended: store the
	// failure on a fresh one, briefly.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if e := s.Store.FailAnalysis(sctx, id, res); e != nil {
		log.ErrorContext(ctx, "analysis_failed", "stage", "store", "id", id, logging.Err(e))
	}
	log.ErrorContext(ctx, "analysis_failed", "stage", "llm", "id", id, "cost_nano_usd", deref(res.CostNanoUSD),
		"duration_ms", time.Since(started).Milliseconds(), logging.Err(err))
	return err
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func viewKey(v View) string {
	return v.Team + "|" + strconv.Itoa(v.Weeks) + "|" + v.Product
}

// Ask starts a manual run of the view in the background, unless one is
// under way or the view's last manual attempt is within Cooldown; it then
// returns errBusy and when the view takes one again.
func (s *Service) Ask(ctx context.Context, v View) (*time.Time, error) {
	if s.LLM == nil {
		return nil, errOff
	}
	// Marked now, not when the run starts: two clicks at once must not both
	// pass. Every refusal below lets go of it; the run keeps it to its end.
	key := viewKey(v)
	if _, busy := s.inflight.LoadOrStore(key, true); busy {
		return nil, errBusy
	}
	until, err := s.mayAsk(ctx, v)
	if err != nil {
		s.inflight.Delete(key)
		return until, err
	}
	go func() {
		defer s.inflight.Delete(key)
		_ = s.run(s.base, v, TriggerManual)
	}()
	return nil, nil
}

// mayAsk refuses a manual run: one under way or within Cooldown, an unknown
// product, the hour's runs taken. Allowed, it books the hour's slot.
func (s *Service) mayAsk(ctx context.Context, v View) (*time.Time, error) {
	last, err := s.Store.LatestAttempt(ctx, v)
	if err != nil {
		return nil, err
	}
	if last != nil {
		if until := last.CreatedAt.Add(Cooldown); last.Trigger == TriggerManual && s.now().Before(until) {
			return &until, errBusy
		}
		if last.Status == "running" && s.now().Sub(last.CreatedAt) < StaleAfter {
			return nil, errBusy
		}
	}
	// A product the team's data does not know would be a view of its own,
	// with a cooldown of its own: one paid call per made-up name.
	if v.Product != "" {
		cur, _ := s.periods(v.Weeks)
		ds, err := s.Load(ctx, v.Team, cur)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(ds.Products, func(p dataset.Product) bool { return p.Key == v.Product }) {
			return nil, errUnknownProduct
		}
	}
	if until, ok := s.takeManual(); !ok {
		return &until, errBusy
	}
	return nil, nil
}

// releaseManual gives back the latest booked slot: a run that asked the
// model nothing.
func (s *Service) releaseManual() {
	s.manualMu.Lock()
	defer s.manualMu.Unlock()
	if n := len(s.manual); n > 0 {
		s.manual = s.manual[:n-1]
	}
}

// ManualPerHour caps the manual runs across all views and teams: the
// cooldown is per view, and the products and windows make several. Per
// process: riptide runs one replica (infra repo); with N the cap is N×.
const ManualPerHour = 10

// takeManual books a manual run if the hour has room; else when it has.
func (s *Service) takeManual() (time.Time, bool) {
	s.manualMu.Lock()
	defer s.manualMu.Unlock()
	n := s.now()
	kept := s.manual[:0]
	for _, at := range s.manual {
		if n.Sub(at) < time.Hour {
			kept = append(kept, at)
		}
	}
	s.manual = kept
	if len(s.manual) >= ManualPerHour {
		return s.manual[0].Add(time.Hour), false
	}
	s.manual = append(s.manual, n)
	return time.Time{}, true
}

// errUnknownProduct: Ask's view names a product the team's data lacks.
var errUnknownProduct = errors.New("unknown product")

// IsUnknownProduct reports whether err is Ask's refusal of a product.
func IsUnknownProduct(err error) bool { return errors.Is(err, errUnknownProduct) }

// IsBusy reports whether err is Ask's refusal.
func IsBusy(err error) bool { return errors.Is(err, errBusy) }
