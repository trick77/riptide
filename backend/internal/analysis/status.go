package analysis

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/trick77/riptide/internal/metrics"
)

// Shown is the analysis a view shows: when, how, what it cost, its text.
type Shown struct {
	ID         int64     `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	FinishedAt time.Time `json:"finished_at"`
	Trigger    string    `json:"trigger"`
	Model      string    `json:"model"`
	// CostUSD is three decimals, null when unpriced.
	CostUSD *string `json:"cost_usd"`
	// WeeksOld is how many closed weeks older than the last one it is.
	WeeksOld int    `json:"weeks_old"`
	Text     Output `json:"text"`
	Report   Report `json:"report"`
}

// Layout is what the page shows of the assessment, decided here, never in
// the browser: the focus repos and the cards in order, and the signals
// spotted without words.
type Layout struct {
	Focus []LayoutFocus `json:"focus"`
	Cards []LayoutCard  `json:"cards"`
	// Also are the signals spotted without words, as chips.
	Also []LayoutCard `json:"also"`
}

// LayoutFocus is a focus repo as shown: its lever (the model's, else the
// rules') and the facts table it links to.
type LayoutFocus struct {
	Repo  string `json:"repo"`
	Lever string `json:"lever"`
	Tab   string `json:"tab"`
}

// LayoutCard is a team-wide card as shown: its column and lever.
type LayoutCard struct {
	Signal string `json:"signal"`
	Column string `json:"column"`
	Lever  string `json:"lever"`
}

// Columns of the team-wide cards.
const (
	ColumnGood = "good"
	ColumnBad  = "bad"
	ColumnDone = "done"
)

func column(s metrics.Signal) string {
	switch {
	case s.Kind == metrics.SignalResolved:
		return ColumnDone
	case s.Tone == metrics.ToneGood:
		return ColumnGood
	}
	return ColumnBad
}

// ruleLever is a signal's lever without the model: its first.
func ruleLever(s metrics.Signal) string {
	if ls := leversFor(s); len(ls) > 0 {
		return ls[0]
	}
	return ""
}

// tab is the facts table a repo's signal links to: runs for a build, PRs
// otherwise.
func tab(s metrics.Signal) string {
	switch s.Kind {
	case metrics.SignalSteadySlow, metrics.SignalSlower, metrics.SignalFlaky:
		return "runs"
	}
	return "prs"
}

// layout is the page's choice: what the analysis words, or without one the
// first metrics.FocusMax candidates and MaxCards team-wide signals; the
// rest, gates and the question aside, is "also spotted".
func layout(a metrics.Assessment, text *Text) Layout {
	l := Layout{Focus: []LayoutFocus{}, Cards: []LayoutCard{}, Also: []LayoutCard{}}
	covered := map[string]bool{}
	byID := map[string]metrics.Signal{}
	for _, s := range a.Signals {
		byID[s.ID] = s
	}
	focus := map[string]metrics.FocusRepo{}
	for _, f := range a.Focus {
		focus[f.Repo] = f
	}
	// lever is the model's where it worded the item, the rules' otherwise.
	addFocus := func(repo, lever string) {
		f, ok := focus[repo]
		if !ok || len(l.Focus) == MaxFocus {
			return
		}
		lf := LayoutFocus{Repo: repo, Lever: lever, Tab: "prs"}
		if len(f.Signals) > 0 {
			first := byID[f.Signals[0]]
			lf.Tab = tab(first)
			if lf.Lever == "" {
				lf.Lever = ruleLever(first)
			}
		}
		l.Focus = append(l.Focus, lf)
		for _, id := range f.Signals {
			covered[id] = true
		}
	}
	addCard := func(id, lever string) {
		s, ok := byID[id]
		if !ok || !cardSignal(s) || covered[id] || len(l.Cards) == MaxCards {
			return
		}
		l.Cards = append(l.Cards, LayoutCard{Signal: id, Column: column(s), Lever: cmp.Or(lever, ruleLever(s))})
		covered[id] = true
	}
	if text != nil {
		for _, f := range text.Focus {
			addFocus(f.Repo, f.Lever)
		}
		for _, k := range text.Cards {
			addCard(k.Signal, k.Lever)
		}
	} else {
		for _, f := range a.Focus[:min(len(a.Focus), metrics.FocusMax)] {
			addFocus(f.Repo, "")
		}
		for _, s := range a.Signals {
			addCard(s.ID, "")
		}
	}
	for _, s := range a.Signals {
		if s.Tone != metrics.ToneGate && !covered[s.ID] {
			l.Also = append(l.Also, LayoutCard{Signal: s.ID, Column: column(s), Lever: ruleLever(s)})
		}
	}
	return l
}

// Failure is the view's latest attempt, failed after what is shown.
type Failure struct {
	At     time.Time `json:"at"`
	Detail string    `json:"detail"`
}

// Status is the body of GET /api/analysis: the assessment of a view's
// closed weeks and, where there is one, the analysis that words it.
type Status struct {
	State string `json:"state"`
	// Enabled: a model is configured, so the page offers to ask.
	Enabled bool `json:"enabled"`
	// Running: an attempt is under way.
	Running bool `json:"running"`
	// Assessment is the shown analysis' own, numbers and all, so its text
	// and numbers agree; without one the view's closed weeks now.
	Assessment metrics.Assessment `json:"assessment"`
	// Layout is what of it the page shows where.
	Layout   Layout   `json:"layout"`
	Analysis *Shown   `json:"analysis"`
	Failure  *Failure `json:"failure"`
	// NextRun is when the schedule words the view next; null for a view it
	// does not run (a product, bots).
	NextRun *time.Time `json:"next_run"`
	// CooldownUntil is when the view takes a manual run again.
	CooldownUntil *time.Time `json:"cooldown_until"`
}

// usd formats nano-USD with three decimals.
func usd(nano *int64) *string {
	if nano == nil {
		return nil
	}
	s := fmt.Sprintf("%.3f", float64(*nano)/1e9)
	return &s
}

// Status is the view's analysis as the page shows it.
func (s *Service) Status(ctx context.Context, v View) (Status, error) {
	st := Status{State: StateOff, Enabled: s.LLM != nil}
	cur, _ := s.periods(v.Weeks)
	if scheduled(v) {
		next := s.NextRun(v.Weeks)
		st.NextRun = &next
	}
	if s.LLM != nil {
		st.State = StateNone
		if last, err := s.Store.LatestAttempt(ctx, v); err != nil {
			return st, err
		} else if last != nil {
			st.Running = last.Status == "running" && s.now().Sub(last.CreatedAt) < StaleAfter
			if until := last.CreatedAt.Add(Cooldown); last.Trigger == TriggerManual && s.now().Before(until) {
				st.CooldownUntil = &until
			}
			if last.Status == "failed" && last.Error != nil {
				st.Failure = &Failure{At: last.CreatedAt, Detail: *last.Error}
			}
		}
		if _, busy := s.inflight.Load(viewKey(v)); busy {
			st.Running = true
		}
	}
	done, err := s.Store.DoneAnalyses(ctx, v, 1)
	if err != nil {
		return st, err
	}
	if len(done) == 1 {
		r := done[0]
		old := int(cur.To.Sub(r.PeriodTo).Hours()/24+0.5) / 7
		var out Output
		var rep Report
		var a metrics.Assessment
		if old <= MaxAge(v.Weeks) && json.Unmarshal(r.Output, &out) == nil && json.Unmarshal(r.Assessment, &a) == nil {
			_ = json.Unmarshal(r.Report, &rep)
			st.State, st.Assessment = StateFresh, a
			if old > 0 {
				st.State = StateOlder
			}
			fin := r.CreatedAt
			if r.FinishedAt != nil {
				fin = *r.FinishedAt
			}
			st.Analysis = &Shown{ID: r.ID, CreatedAt: r.CreatedAt, FinishedAt: fin, Trigger: r.Trigger, Model: r.Model,
				CostUSD: usd(r.CostNanoUSD), WeeksOld: old, Text: out, Report: rep}
			if st.Failure != nil && st.Failure.At.Before(fin) {
				st.Failure = nil
			}
			// German and English word the same items: validate mirrors them.
			st.Layout = layout(a, &out.DE)
			return st, nil
		}
	}
	a, err := s.cachedAssess(ctx, v, cur.To)
	if err != nil {
		return st, err
	}
	st.Assessment, st.Layout = a, layout(a, nil)
	return st, nil
}

// cacheFor is how long a view's assessment of closed weeks is served from
// memory: the page polls every few seconds while a run is under way, and
// closed weeks change only by late events.
const cacheFor = 5 * time.Minute

type cached struct {
	to time.Time
	at time.Time
	a  metrics.Assessment
}

func (s *Service) cachedAssess(ctx context.Context, v View, to time.Time) (metrics.Assessment, error) {
	key := viewKey(v)
	if c, ok := s.cache.Load(key); ok {
		if c := c.(cached); c.to.Equal(to) && s.now().Sub(c.at) < cacheFor {
			return c.a, nil
		}
	}
	a, err := s.assess(ctx, v)
	if err == nil {
		s.sweep()
		s.cache.Store(key, cached{to: to, at: s.now(), a: a})
	}
	return a, err
}

// cacheMax bounds the cache: GET takes any product string, and a view per
// made-up name must not grow it without end.
const cacheMax = 64

// sweep drops the expired entries, and all of them past cacheMax.
func (s *Service) sweep() {
	n := 0
	s.cache.Range(func(k, c any) bool {
		if s.now().Sub(c.(cached).at) >= cacheFor {
			s.cache.Delete(k)
		} else {
			n++
		}
		return true
	})
	if n >= cacheMax {
		s.cache.Clear()
	}
}
