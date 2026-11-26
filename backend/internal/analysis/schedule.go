package analysis

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/trick77/ship/kit/logging"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/metrics"
	"github.com/trick77/riptide/internal/store"
)

// The schedule words every view of every team once its week has closed,
// all products first, then each product with data in the window (user,
// 2026-11-16: a product tab must not show an assessment of all of them;
// the cost is no reason against it), each window on its cadence (cadences).
const (
	// RunAfter is how long into Monday the schedule waits: the week's last
	// webhooks are in, the team is not yet in its daily.
	RunAfter = 6 * time.Hour
	// tickEvery is how often the schedule looks for due runs; a failed one
	// is tried again on a later tick, MaxAttempts times.
	tickEvery = 15 * time.Minute
	// retryAfter is the pause after a failed scheduled run of a view.
	retryAfter = time.Hour
)

// settled reports whether a scheduled attempt needs no further try: done,
// tried MaxAttempts times, or running and not yet stale.
func settled(a store.Analysis, now time.Time) bool {
	switch {
	case a.Status == "done", a.Attempts >= MaxAttempts:
		return true
	case a.Status == "failed":
		return false
	}
	return now.Sub(a.CreatedAt) < StaleAfter
}

// cadence is how often the schedule words a window: which closed weeks, and
// so how old its analysis may grow.
type cadence struct {
	// due reports whether the closed weeks ending at weekStart (a Monday
	// 00:00) are worded.
	due func(weekStart time.Time) bool
	// maxAge is the widest gap between two due weeks, in weeks: MaxAge.
	maxAge int
}

// weekly is every window's cadence the table does not name: the 4 weeks.
var weekly = cadence{due: func(time.Time) bool { return true }, maxAge: 1}

// cadences are the windows worded less often than weekly: 12 weeks every
// second Monday, 26 on the first Monday of the month (user, 2026-11-16: a
// long window barely moves in a week, and a weekly run only repeats itself).
// Every second week counts weeks from a fixed Monday, never the ISO number:
// a year of 53 weeks has KW 53 and KW 1 both odd, a gap of three weeks past
// maxAge. The first Monday of a month follows the one before 4 or 5 weeks
// later.
var cadences = map[int]cadence{
	12: {due: func(w time.Time) bool { return weekNumber(w)%2 == 0 }, maxAge: 2},
	26: {due: func(w time.Time) bool { return w.Day() <= 7 }, maxAge: 5},
}

// cadenceOf is the window's cadence.
func cadenceOf(weeks int) cadence {
	if c, ok := cadences[weeks]; ok {
		return c
	}
	return weekly
}

// due reports whether the window is worded for the closed weeks ending at
// weekStart (a Monday 00:00).
func due(weeks int, weekStart time.Time) bool { return cadenceOf(weeks).due(weekStart) }

// weekNumber counts the weeks from Monday 5 January 1970 to the Monday
// weekStart, by calendar days, whatever the zone's DST.
func weekNumber(weekStart time.Time) int {
	y, m, d := weekStart.Date()
	days := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Sub(time.Date(1970, 1, 5, 0, 0, 0, 0, time.UTC)).Hours() / 24
	return int(days+0.5) / 7
}

// weekStart is metrics' own, so the schedule's week and the claimed
// period's end are one instant: two would leave a settled week unseen.
var weekStart = metrics.WeekStart

// NextRun is when the schedule next words the window: the first due
// Monday from now on, RunAfter in; this week's if not yet past. False when
// none falls within twice the cadence's maxAge: a cadence never due would
// spin forever.
func (s *Service) NextRun(weeks int) (time.Time, bool) {
	n := s.now()
	w := weekStart(n, s.Loc)
	c := cadenceOf(weeks)
	for i := range 2*c.maxAge + 1 {
		start := w.AddDate(0, 0, 7*i)
		if at := start.Add(RunAfter); c.due(start) && at.After(n) {
			return at.UTC(), true
		}
	}
	return time.Time{}, false
}

// Schedule runs the due analyses until ctx ends: every team's views, every
// window due for the last closed week, one after the other.
// Late is fine: a pod down on Monday catches up within the week.
func (s *Service) Schedule(ctx context.Context, teams func() []string) {
	t := time.NewTicker(tickEvery)
	defer t.Stop()
	for {
		s.tick(ctx, teams())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Service) tick(ctx context.Context, teams []string) {
	if s.LLM == nil {
		return
	}
	n := s.now()
	w := weekStart(n, s.Loc)
	if n.Before(w.Add(RunAfter)) {
		return
	}
	s.sweepWeek(w, n)
	for _, team := range teams {
		for _, weeks := range metrics.Windows {
			if ctx.Err() != nil {
				return
			}
			if !due(weeks, w) {
				continue
			}
			// The window's views share their two datasets: read once, not
			// once per product (and once more for the product list).
			wctx := withLoads(ctx)
			views := []View{{Team: team, Weeks: weeks}}
			// A team whose products do not load gets its all-products view,
			// which fails the same way and backs off; the products follow
			// on a later tick.
			for _, p := range s.productsFor(wctx, team, weeks, w, n, true) {
				views = append(views, View{Team: team, Weeks: weeks, Product: p})
			}
			for _, v := range views {
				if ctx.Err() != nil {
					return
				}
				s.tickView(wctx, v, w, n)
			}
		}
	}
}

func (s *Service) tickView(ctx context.Context, v View, w, n time.Time) {
	key := viewKey(v)
	// Settled for the week: no query either, every product of every team
	// would ask each tick all week.
	if at, ok := s.settledAt.Load(key); ok && at.(time.Time).Equal(w) {
		return
	}
	// A cheap look first: reading a team's datasets every tick only to find
	// the week done would load the database for nothing. Run's claim still
	// decides across replicas.
	a, err := s.Store.ScheduledAttempt(ctx, v, w.UTC())
	if err != nil {
		return
	}
	if a != nil && settled(*a, n) {
		s.settledAt.Store(key, w)
		return
	}
	if until, ok := s.backoff.Load(key); ok && n.Before(until.(time.Time)) {
		return
	}
	// A run failing before its claim leaves no row to count its attempts on
	// (a dataset that does not load): wait before the next try rather than
	// reload the team every tick.
	switch err := s.Run(ctx, v, TriggerScheduled); {
	case errors.Is(err, errQuiet):
		// Nothing to word this week; the next closed week may have.
		s.backoff.Store(key, w.AddDate(0, 0, 7))
	case err != nil && !IsBusy(err):
		s.backoff.Store(key, n.Add(retryAfter))
	default:
		s.backoff.Delete(key)
	}
}

// productsFor are the products with data in the window's closed weeks
// ending at w, read once per team, window and week. Nil when the dataset
// does not load. For the schedule (sched) a failure is logged and not
// tried again before retryAfter: every tick would read the team's whole
// window. A page's failed read (a closed tab cancels it) leaves the
// schedule alone.
func (s *Service) productsFor(ctx context.Context, team string, weeks int, w, n time.Time, sched bool) []string {
	key := team + "|" + strconv.Itoa(weeks) + "|" + strconv.FormatInt(w.Unix(), 10)
	if p, ok := s.products.Load(key); ok {
		return p.([]string)
	}
	if until, ok := s.backoff.Load("products|" + key); ok && n.Before(until.(time.Time)) {
		return nil
	}
	// Both periods, as every view's assessment reads them: the tick's cache
	// then serves them all.
	dss, err := s.load(ctx, team, s.reads(weeks)...)
	if err != nil {
		if sched {
			s.Log.ErrorContext(ctx, "analysis_failed", "stage", "products", "team", team, "weeks", weeks,
				logging.Err(err))
			s.backoff.Store("products|"+key, n.Add(retryAfter))
		}
		return nil
	}
	ds := dss[0]
	out := make([]string, 0, len(ds.Products))
	for _, p := range ds.Products {
		out = append(out, p.Key)
	}
	s.products.Store(key, out)
	return out
}

// sweepWeek drops what belongs to weeks before w: their product lists and
// settled marks, and every backoff already over.
func (s *Service) sweepWeek(w, n time.Time) {
	suffix := "|" + strconv.FormatInt(w.Unix(), 10)
	s.products.Range(func(k, _ any) bool {
		if !strings.HasSuffix(k.(string), suffix) {
			s.products.Delete(k)
		}
		return true
	})
	s.settledAt.Range(func(k, at any) bool {
		if !at.(time.Time).Equal(w) {
			s.settledAt.Delete(k)
		}
		return true
	})
	s.backoff.Range(func(k, until any) bool {
		if !n.Before(until.(time.Time)) {
			s.backoff.Delete(k)
		}
		return true
	})
}

// loadsKey holds a window's datasets in a tick's context.
type loadsKey struct{}

type loads struct {
	mu sync.Mutex
	m  map[string]*dataset.Dataset
}

func withLoads(ctx context.Context) context.Context {
	return context.WithValue(ctx, loadsKey{}, &loads{m: map[string]*dataset.Dataset{}})
}

// load is Load, kept for the rest of the tick when the context holds a
// cache: a window's product views read the same two periods. Each dataset
// is kept by its period and the end classing it, ps[0]'s.
func (s *Service) load(ctx context.Context, team string, ps ...metrics.Period) ([]*dataset.Dataset, error) {
	c, ok := ctx.Value(loadsKey{}).(*loads)
	if !ok {
		return s.Load(ctx, team, ps...)
	}
	key := func(p metrics.Period) string {
		return team + "|" + p.From.String() + "|" + p.To.String() + "|" + ps[0].To.String()
	}
	out := make([]*dataset.Dataset, len(ps))
	c.mu.Lock()
	hit := true
	for i, p := range ps {
		out[i] = c.m[key(p)]
		hit = hit && out[i] != nil
	}
	c.mu.Unlock()
	if hit {
		return out, nil
	}
	out, err := s.Load(ctx, team, ps...)
	if err == nil {
		c.mu.Lock()
		for i, p := range ps {
			c.m[key(p)] = out[i]
		}
		c.mu.Unlock()
	}
	return out, err
}
