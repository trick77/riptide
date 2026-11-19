package analysis

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/trick77/riptide/internal/metrics"
	"github.com/trick77/riptide/internal/store"
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
	Also  []LayoutCard `json:"also"`
	Tally Tally        `json:"tally"`
}

// Tally is the assessment in one line: its signals by column, gates aside,
// counted whether worded or not. Good or done above 0 is what lets the page
// say "runs"; without either it says "unremarkable" (user: "wenn nichts
// läuft, ist läuft gut übertrieben").
type Tally struct {
	Good int `json:"good"`
	Done int `json:"done"`
	Bad  int `json:"bad"`
	// Baseline: a team number has a previous period to read better against;
	// without one nothing can.
	Baseline bool `json:"baseline"`
}

// Card slots of the good and done columns, on top of MaxCards: ranked
// last, a gain would otherwise never get past the worries.
const (
	GoodCards = 2
	DoneCards = 1
)

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
	case metrics.SignalSteadySlow, metrics.SignalSlower, metrics.SignalFailingMore:
		return "runs"
	}
	return "prs"
}

// layout is the page's choice: what the analysis words, or without one the
// first metrics.FocusMax candidates and MaxCards team-wide signals. Gains
// and resolutions get GoodCards and DoneCards slots on top: without words
// the best ones; with words the best gain and resolution always, the worded
// ones beside them (past the slots if need be), unworded ones in a slot
// left. The rest, gates and the question aside, is "also spotted".
func layout(a metrics.Assessment, text *Text) Layout {
	l := Layout{Focus: []LayoutFocus{}, Cards: []LayoutCard{}, Also: []LayoutCard{}, Tally: tally(a)}
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
	// A worded card always shows (the validator caps them at MaxCards), even
	// where a focus repo carries its signal: the model's words are never
	// dropped. An unworded one takes a free slot of its column (take), and
	// none a focus already covers.
	slots := map[string]int{ColumnBad: MaxCards, ColumnGood: GoodCards, ColumnDone: DoneCards}
	shown := map[string]bool{}
	addCard := func(id, lever string, worded, take bool) {
		s, ok := byID[id]
		if !ok || !cardSignal(s) || shown[id] || covered[id] && !worded {
			return
		}
		col := column(s)
		if take {
			if slots[col] <= 0 {
				return
			}
			slots[col]--
		}
		l.Cards = append(l.Cards, LayoutCard{Signal: id, Column: col, Lever: cmp.Or(lever, ruleLever(s))})
		shown[id], covered[id] = true, true
	}
	if text != nil {
		for _, f := range text.Focus {
			addFocus(f.Repo, f.Lever)
		}
		// The worries in the model's order. What runs in the backend's rank:
		// the best gain and the best resolution always, worded or not (the
		// validator asks for them but cannot force them), then every worded
		// one, its slot booked whatever its rank, then unworded ones in a
		// slot left. Unworded, a card takes the rules' words.
		worded := map[string]string{}
		for _, k := range text.Cards {
			if s, ok := byID[k.Signal]; ok && column(s) != ColumnBad {
				worded[k.Signal] = k.Lever
				continue
			}
			addCard(k.Signal, k.Lever, true, false)
		}
		// Which show first, then in the backend's rank across both columns:
		// the page lists gains and resolutions as ranked.
		pick, best := map[string]bool{}, map[string]bool{}
		free := func(s metrics.Signal) bool {
			return column(s) != ColumnBad && cardSignal(s) && !covered[s.ID] && !pick[s.ID]
		}
		for _, s := range a.Signals {
			if col := column(s); free(s) && !best[col] {
				best[col], pick[s.ID] = true, true
				slots[col]--
			}
		}
		for id := range worded {
			if s, ok := byID[id]; ok && cardSignal(s) && !pick[id] {
				pick[id] = true
				slots[column(s)]--
			}
		}
		for _, s := range a.Signals {
			if col := column(s); free(s) && slots[col] > 0 {
				pick[s.ID] = true
				slots[col]--
			}
		}
		for _, s := range a.Signals {
			if pick[s.ID] {
				lever, ok := worded[s.ID]
				addCard(s.ID, lever, ok, false)
			}
		}
	} else {
		for _, f := range a.Focus[:min(len(a.Focus), metrics.FocusMax)] {
			addFocus(f.Repo, "")
		}
		for _, s := range a.Signals {
			addCard(s.ID, "", false, true)
		}
	}
	for _, s := range a.Signals {
		if s.Tone != metrics.ToneGate && !covered[s.ID] {
			l.Also = append(l.Also, LayoutCard{Signal: s.ID, Column: column(s), Lever: ruleLever(s)})
		}
	}
	return l
}

// tally counts a's signals by column, gates aside.
func tally(a metrics.Assessment) Tally {
	var t Tally
	for _, s := range a.Signals {
		if s.Tone == metrics.ToneGate {
			continue
		}
		switch column(s) {
		case ColumnGood:
			t.Good++
		case ColumnDone:
			t.Done++
		default:
			t.Bad++
		}
	}
	t.Baseline = slices.ContainsFunc(a.Team, func(f metrics.Fact) bool { return f.Verdict != metrics.ChangeNone })
	return t
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
	// NextRun is when the schedule words the view next; null without a
	// model, and for a product it leaves out (no data, no activity, unknown).
	NextRun *time.Time `json:"next_run"`
	// CooldownUntil is when the view takes a manual run again.
	CooldownUntil *time.Time `json:"cooldown_until"`
	// Zone is the calendar the closed weeks are cut in: the page shows
	// their days there, not in the viewer's.
	Zone string `json:"zone"`
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
	st := Status{State: StateOff, Enabled: s.LLM != nil, Zone: ianaZone(s.Loc)}
	// The product list for next_run and the assessment read the same
	// window: once.
	ctx = withLoads(ctx)
	cur, _ := s.periods(v.Weeks)
	if s.LLM != nil {
		st.State = StateNone
		if last, err := s.Store.LatestAttempt(ctx, v); err != nil {
			return st, err
		} else if last != nil {
			st.Running = last.Status == "running" && s.now().Sub(last.CreatedAt) < StaleAfter
			st.CooldownUntil = cooldownUntil(last, s.now())
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
			if err := s.backfillCompared(ctx, v, r, &a); err != nil {
				return st, err
			}
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
			s.nextRun(ctx, v, &st)
			return st, nil
		}
	}
	a, err := s.cachedAssess(ctx, v, cur.To)
	if err != nil {
		return st, err
	}
	st.Assessment, st.Layout = a, layout(a, nil)
	s.nextRun(ctx, v, &st)
	return st, nil
}

// nextRun sets when the schedule words the view next, if it does: all
// products always, a product only with data in the window and a repo
// active in it, as tick and run decide.
func (s *Service) nextRun(ctx context.Context, v View, st *Status) {
	if s.LLM == nil {
		return
	}
	if v.Product != "" {
		n := s.now()
		if quiet(v, st.Assessment) ||
			!slices.Contains(s.productsFor(ctx, v.Team, v.Weeks, weekStart(n, s.Loc), n, false), v.Product) {
			return
		}
	}
	next := s.NextRun(v.Weeks)
	st.NextRun = &next
}

// backfillCompared reads Compared from the view's history for an analysis
// stored before the field: unmarshalled, it would read false and claim no
// earlier week was assessed. Once per row: the page polls. Only rows from
// before the field, so the memo stays as small as their number.
func (s *Service) backfillCompared(ctx context.Context, v View, r store.Analysis, a *metrics.Assessment) error {
	var probe struct {
		Compared *bool `json:"compared"`
	}
	// The row unmarshalled into a just before: it is JSON.
	_ = json.Unmarshal(r.Assessment, &probe)
	if probe.Compared != nil {
		return nil
	}
	if c, ok := s.compared.Load(r.ID); ok {
		a.Compared = c.(bool)
		return nil
	}
	h, err := s.history(ctx, v, r.PeriodTo)
	if err != nil {
		return err
	}
	a.Compared = h.Runs > 0
	s.compared.Store(r.ID, a.Compared)
	return nil
}

// ianaZone is loc's IANA name for the page; empty for "Local", which no
// browser knows: the page then shows the days in the viewer's zone.
func ianaZone(loc *time.Location) string {
	if loc == nil || loc.String() == "Local" {
		return ""
	}
	return loc.String()
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
