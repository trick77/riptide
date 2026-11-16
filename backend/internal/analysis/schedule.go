package analysis

import (
	"context"
	"time"

	"github.com/trick77/riptide/internal/metrics"
	"github.com/trick77/riptide/internal/store"
)

// The schedule words every team's default view (all products, no bots)
// once its week has closed: 4 weeks every Monday, 12 every second Monday,
// 26 on the first Monday of the month (user, 2026-11-16: a long window
// barely moves in a week, and a weekly run only repeats itself). A view
// another product or bots selects is worded on request only.
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

func scheduled(v View) bool { return v.Product == "" && !v.Bots }

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

// due reports whether the window is worded for the closed weeks ending at
// weekStart (a Monday 00:00). Every second week counts weeks from a fixed
// Monday, never the ISO number: a year of 53 weeks has KW 53 and KW 1 both
// odd, a gap of three weeks past MaxAge.
func due(weeks int, weekStart time.Time) bool {
	switch weeks {
	case 12:
		return weekNumber(weekStart)%2 == 0
	case 26:
		return weekStart.Day() <= 7
	}
	return true
}

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
// Monday from now on, RunAfter in; this week's if not yet past.
func (s *Service) NextRun(weeks int) time.Time {
	n := s.now()
	w := weekStart(n, s.Loc)
	for i := 0; ; i++ {
		start := w.AddDate(0, 0, 7*i)
		if at := start.Add(RunAfter); due(weeks, start) && at.After(n) {
			return at.UTC()
		}
	}
}

// Schedule runs the due analyses until ctx ends: every team's default
// view, every window due for the last closed week, one after the other.
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
	for _, team := range teams {
		for _, weeks := range metrics.Windows {
			if ctx.Err() != nil {
				return
			}
			if !due(weeks, w) {
				continue
			}
			v := View{Team: team, Weeks: weeks}
			// A cheap look first: reading a team's datasets every tick only
			// to find the week done would load the database for nothing.
			// Run's claim still decides across replicas.
			if a, err := s.Store.ScheduledAttempt(ctx, v, w.UTC()); err != nil || (a != nil && settled(*a, n)) {
				continue
			}
			key := viewKey(v)
			if until, ok := s.backoff.Load(key); ok && n.Before(until.(time.Time)) {
				continue
			}
			// A run failing before its claim leaves no row to count its
			// attempts on (a dataset that does not load): wait before the
			// next try rather than reload the team every tick.
			if err := s.Run(ctx, v, TriggerScheduled); err != nil && !IsBusy(err) {
				s.backoff.Store(key, n.Add(retryAfter))
			} else {
				s.backoff.Delete(key)
			}
		}
	}
}
