package analysis

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/toolkit/llmwire/llmwiretest"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/metrics"
	"github.com/trick77/riptide/internal/store"
)

// Synthetic names only.

var zurich = func() *time.Location {
	l, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		panic(err)
	}
	return l
}()

// now is a Thursday of KW 46: the last closed week is KW 45.
var now = time.Date(2026, 11, 12, 12, 0, 0, 0, zurich)

// jammed is a dataset whose repo kasse/kasse-api has four PRs open a week
// or longer at the period's end, all waiting on a first review.
func jammed(_ context.Context, team string, p metrics.Period) (*dataset.Dataset, error) {
	ds := &dataset.Dataset{
		Meta: dataset.Meta{Team: team, From: p.From, To: p.To, GeneratedAt: p.To, ProductionStage: "prod"},
		PRs:  []dataset.PR{}, Deploys: []dataset.Deploy{}, LeadTimes: []dataset.LeadTime{}, Runs: []dataset.Run{},
		Reviews:  []dataset.Review{},
		Products: []dataset.Product{{Key: "kasse", Repos: []string{"kasse/kasse-api"}, Apps: []string{}}},
	}
	for i, days := range []int{9, 10, 12, 14} {
		at := p.To.AddDate(0, 0, -days)
		ds.OpenPRs = append(ds.OpenPRs, dataset.OpenPR{
			Repo: "kasse/kasse-api", Project: "kasse", PRID: int64(i + 1), OpenedAt: at, ReadyAt: &at, ReadySince: &at,
			LastEventAt: p.To.AddDate(0, 0, -1),
		})
	}
	return ds, nil
}

// memStore keeps analyses in memory, as the store's SQL does.
type memStore struct {
	mu   sync.Mutex
	rows []store.Analysis
	// doneCalls counts DoneAnalyses calls.
	doneCalls int
}

func (m *memStore) ClaimAnalysis(_ context.Context, c store.AnalysisClaim) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.Trigger == TriggerScheduled {
		for i, r := range m.rows {
			if r.Trigger == TriggerScheduled && r.Team == c.View.Team && r.Weeks == c.View.Weeks &&
				r.Product == c.View.Product && r.PeriodTo.Equal(c.To) {
				if r.Status == "failed" && r.Attempts < c.MaxAttempts {
					m.rows[i].Status, m.rows[i].Attempts = "running", r.Attempts+1
					return r.ID, true, nil
				}
				return 0, false, nil
			}
		}
	}
	id := int64(len(m.rows) + 1)
	m.rows = append(m.rows, store.Analysis{ID: id, Team: c.View.Team, Weeks: c.View.Weeks, Product: c.View.Product,
		PeriodFrom: c.From, PeriodTo: c.To, Trigger: c.Trigger, Status: "running", Model: c.Model,
		Attempts: 1, CreatedAt: c.At})
	return id, true, nil
}

func (m *memStore) end(id int64, status string, r store.AnalysisResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := &m.rows[id-1]
	a.Status, a.InputHash, a.Assessment, a.Output, a.Report = status, r.InputHash, r.Assessment, r.Output, r.Report
	a.InputTokens, a.OutputTokens, a.CostNanoUSD, a.Error = r.InputTokens, r.OutputTokens, r.CostNanoUSD, r.Error
	a.FinishedAt = ptr(r.At)
	// As the SQL does: only a done row names its rules.
	if a.RulesVersion = nil; status == "done" {
		a.RulesVersion = r.RulesVersion
	}
	return nil
}

func (m *memStore) FinishAnalysis(_ context.Context, id int64, r store.AnalysisResult) error {
	return m.end(id, "done", r)
}

func (m *memStore) FailAnalysis(_ context.Context, id int64, r store.AnalysisResult) error {
	return m.end(id, "failed", r)
}

// done is the view's done rows ending before to (zero: any), ordered as the
// SQL does: period_to, then finished_at (NULL first), then id, newest first.
func (m *memStore) done(v store.AnalysisView, to time.Time) []store.Analysis {
	var out []store.Analysis
	for _, r := range m.rows {
		if r.Status == "done" && r.Team == v.Team && r.Weeks == v.Weeks && r.Product == v.Product &&
			(to.IsZero() || r.PeriodTo.Before(to)) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b store.Analysis) int {
		if c := b.PeriodTo.Compare(a.PeriodTo); c != 0 {
			return c
		}
		// DESC puts NULL first in Postgres.
		switch {
		case a.FinishedAt == nil && b.FinishedAt != nil:
			return -1
		case a.FinishedAt != nil && b.FinishedAt == nil:
			return 1
		case a.FinishedAt != nil:
			if c := b.FinishedAt.Compare(*a.FinishedAt); c != 0 {
				return c
			}
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return out
}

func (m *memStore) DoneAnalyses(_ context.Context, v store.AnalysisView, limit int) ([]store.Analysis, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.doneCalls++
	out := m.done(v, time.Time{})
	return out[:min(limit, len(out))], nil
}

func (m *memStore) DoneWeeksBefore(_ context.Context, v store.AnalysisView, to time.Time,
	weeks int) ([]store.Analysis, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Analysis
	for _, r := range m.done(v, to) {
		if len(out) == weeks {
			break
		}
		if len(r.Assessment) > 0 && (len(out) == 0 || !r.PeriodTo.Equal(out[len(out)-1].PeriodTo)) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memStore) LatestAttempt(_ context.Context, v store.AnalysisView) (*store.Analysis, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.rows) - 1; i >= 0; i-- {
		if r := m.rows[i]; r.Team == v.Team && r.Weeks == v.Weeks && r.Product == v.Product {
			return &r, nil
		}
	}
	return nil, nil
}

func (m *memStore) ScheduledAttempt(_ context.Context, v store.AnalysisView, to time.Time) (*store.Analysis, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.Trigger == TriggerScheduled && r.Team == v.Team && r.Weeks == v.Weeks && r.Product == v.Product &&
			r.PeriodTo.Equal(to) {
			return &r, nil
		}
	}
	return nil, nil
}

// fakeLLM answers with a function of the digest it got.
type fakeLLM struct {
	answer func(digest string) string
	finish string
	calls  int
	digest string
}

func (f *fakeLLM) Model() string { return "fake-model" }

func (f *fakeLLM) Complete(_ context.Context, _, user string) (Call, error) {
	f.calls++
	f.digest = user
	return Call{Content: f.answer(user), FinishReason: f.finish, InputTokens: ptr(int64(1000)),
		OutputTokens: ptr(int64(500)), NanoUSD: ptr(int64(7_000_000))}, nil
}

// goodAnswer words the jam of repo_a, in both languages.
func goodAnswer(string) string {
	text := func(head string) Text {
		return Text{
			Headline: head,
			Trend:    "Ruhig bis auf einen Stau.",
			Focus: []FocusText{{Repo: "repo_a", Take: "{{pr_jam:repo_a.long}} PRs warten auf den ersten Blick.",
				Retro: ptr("Warum liegen sie?"), Lever: LeverWalkPRs}},
			Cards:    []CardText{},
			Question: "",
		}
	}
	b, _ := json.Marshal(Output{DE: text("Ein Stau bremst."), EN: text("One jam slows things down.")})
	return string(b)
}

func service(llm LLM, st *memStore) *Service {
	s := &Service{Store: st, Load: jammed, LLM: llm, Loc: zurich,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return now }}
	s.Start(context.Background())
	return s
}

var view = View{Team: "team-a", Weeks: 4}

func TestDigestHidesRepos(t *testing.T) {
	llm := &fakeLLM{answer: goodAnswer}
	st := &memStore{}
	if err := service(llm, st).Run(context.Background(), view, TriggerManual); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(llm.digest, "kasse") {
		t.Errorf("the digest names a repo: %s", llm.digest)
	}
	if !strings.Contains(llm.digest, `"pr_jam:repo_a"`) {
		t.Errorf("the jam is not in the digest under its alias")
	}
	// The weeks are the page's: not a key of the model's input.
	if strings.Contains(llm.digest, `"series"`) {
		t.Errorf("the digest carries the weeks: %s", llm.digest)
	}
	var out Output
	if err := json.Unmarshal(st.rows[0].Output, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.DE.Focus) != 1 || out.DE.Focus[0].Repo != "kasse/kasse-api" ||
		out.DE.Focus[0].Take != "{{pr_jam:kasse/kasse-api.long}} PRs warten auf den ersten Blick." {
		t.Errorf("focus not shown under the repo's name: %+v", out.DE.Focus)
	}
	if st.rows[0].Status != "done" || *st.rows[0].CostNanoUSD != 7_000_000 {
		t.Errorf("row = %+v", st.rows[0])
	}
}

// A repo named like a key or a word: only repo fields are aliased, and
// the model's answer comes back with the name.
func TestAliasesTouchOnlyRepos(t *testing.T) {
	load := func(ctx context.Context, team string, p metrics.Period) (*dataset.Dataset, error) {
		ds, err := jammed(ctx, team, p)
		for i := range ds.OpenPRs {
			ds.OpenPRs[i].Repo = "ci"
		}
		return ds, err
	}
	s := service(nil, &memStore{})
	s.Load = load
	a, err := s.assess(context.Background(), view)
	if err != nil {
		t.Fatal(err)
	}
	raw, al, err := buildDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	d := string(raw)
	for _, want := range []string{`"pr_jam:repo_a"`, `"repo":"repo_a"`, `"pr_jam:repo_a.long"`} {
		if !strings.Contains(d, want) {
			t.Errorf("digest lacks %s", want)
		}
	}
	if strings.Contains(d, `"pr_jam:ci`) || strings.Contains(d, `"repo":"ci"`) {
		t.Errorf("the repo's name reached the digest")
	}
	if got := al.show("In repo_a: {{pr_jam:repo_a.long}} PRs, no repo_ab."); got != "In ci: {{pr_jam:ci.long}} PRs, no repo_ab." {
		t.Errorf("shown = %q", got)
	}
}

func TestValidate(t *testing.T) {
	a, err := service(nil, &memStore{}).assess(context.Background(), view)
	if err != nil {
		t.Fatal(err)
	}
	_, al, err := buildDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	// The German text as each case has it, the English one clean: what
	// German loses, English loses too.
	answer := func(mod func(*Text)) string {
		de := Text{Headline: "Ein Stau bremst.", Focus: []FocusText{}, Cards: []CardText{}}
		en := Text{Headline: "One jam.", Cards: []CardText{},
			Focus: []FocusText{{Repo: "repo_a", Take: "It is jammed.", Lever: LeverAskWhyWaiting}}}
		mod(&de)
		b, _ := json.Marshal(Output{DE: de, EN: en})
		return string(b)
	}
	focus := func(take, lever string) func(*Text) {
		return func(t *Text) { t.Focus = []FocusText{{Repo: "repo_a", Take: take, Lever: lever}} }
	}
	for _, c := range []struct {
		name    string
		raw     string
		fails   bool
		dropped string
		missed  bool
	}{
		{name: "clean", raw: answer(focus("Er staut sich.", LeverWalkPRs))},
		{name: "prose around", raw: "Here:\n```json\n" + answer(focus("Er staut sich.", LeverWalkPRs)) + "\n```"},
		{name: "a digit", raw: answer(focus("Seit 3 Wochen.", LeverWalkPRs)), dropped: "a digit", missed: true},
		{name: "unknown fact", raw: answer(focus("{{x.y}} offen.", LeverWalkPRs)), dropped: "unknown fact", missed: true},
		{name: "foreign lever", raw: answer(focus("Er staut sich.", LeverInspectBuildLog)), dropped: "lever", missed: true},
		{name: "not a candidate", raw: answer(func(t *Text) {
			t.Focus = []FocusText{{Repo: "repo_z", Take: "x", Lever: LeverWalkPRs}}
		}), dropped: "not a candidate", missed: true},
		{name: "ss", raw: answer(focus("Grosser Stau, gross.", LeverWalkPRs))},
		{name: "sharp s", raw: answer(focus("Ein großer Stau.", LeverWalkPRs)), dropped: "sharp s", missed: true},
		{name: "headline too long", raw: answer(func(t *Text) { t.Headline = strings.Repeat("a", MaxHeadline+1) }), fails: true},
		{name: "headline digit", raw: answer(func(t *Text) { t.Headline = "3 Staus." }), fails: true},
		{name: "no json", raw: "Sorry.", fails: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, rep, err := validate(c.raw, a, al)
			if (err != nil) != c.fails {
				t.Fatalf("err = %v", err)
			}
			if c.fails {
				return
			}
			// Both languages show the same: the same repos, the same lever.
			if len(out.DE.Focus) != len(out.EN.Focus) {
				t.Fatalf("focus de %d en %d", len(out.DE.Focus), len(out.EN.Focus))
			}
			for i := range out.DE.Focus {
				if out.DE.Focus[i].Repo != out.EN.Focus[i].Repo || out.DE.Focus[i].Lever != out.EN.Focus[i].Lever {
					t.Errorf("de %+v en %+v", out.DE.Focus[i], out.EN.Focus[i])
				}
			}
			if got := strings.Join(rep.Dropped, "; "); (c.dropped == "") != (got == "") || !strings.Contains(got, c.dropped) {
				t.Errorf("dropped = %q, want %q", got, c.dropped)
			}
			if (len(rep.Missed) > 0) != c.missed {
				t.Errorf("missed = %v", rep.Missed)
			}
		})
	}
}

func TestRunOncePerScheduledWeek(t *testing.T) {
	llm := &fakeLLM{answer: goodAnswer}
	st := &memStore{}
	s := service(llm, st)
	ctx := context.Background()
	for range 2 {
		if err := s.Run(ctx, view, TriggerScheduled); err != nil {
			t.Fatal(err)
		}
	}
	if llm.calls != 1 {
		t.Errorf("model called %d times for one closed week", llm.calls)
	}
	// The unchanged-input skip is a manual run's only.
	if st.doneCalls != 0 {
		t.Errorf("scheduled runs looked up done analyses %d times", st.doneCalls)
	}
	// A click on unchanged data asks nothing.
	if err := s.Run(ctx, view, TriggerManual); err != nil || llm.calls != 1 {
		t.Errorf("unchanged input asked again: %d calls, %v", llm.calls, err)
	}
}

func TestFailedAnswerIsStored(t *testing.T) {
	llm := &fakeLLM{answer: func(string) string { return "no json" }, finish: "length"}
	st := &memStore{}
	s := service(llm, st)
	var logs strings.Builder
	s.Log = testLog(&logs)
	if err := s.Run(context.Background(), view, TriggerManual); err == nil {
		t.Fatal("no error")
	}
	if len(st.rows) != 1 {
		t.Fatalf("rows = %+v", st.rows)
	}
	r := st.rows[0]
	if r.Status != "failed" || r.Error == nil || r.CostNanoUSD == nil {
		t.Errorf("failed row = %+v: the failure and what it cost belong in it", r)
	}
	// The model answered: the answer failed, not the call.
	want := `level=ERROR msg=analysis_failed team=team-a weeks=4 product="" trigger=manual model=fake-model ` +
		`stage=validate id=1 cost_nano_usd=7000000 err="finish_reason=length: no JSON object in the answer"` + "\n"
	if logs.String() != want {
		t.Errorf("log =\n%s\nwant\n%s", logs.String(), want)
	}
}

// testLog writes text lines without what varies from run to run.
func testLog(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{ReplaceAttr: func(g []string, a slog.Attr) slog.Attr {
		if len(g) == 0 && (a.Key == slog.TimeKey || a.Key == "duration_ms") {
			return slog.Attr{}
		}
		return a
	}}))
}

type errLLM struct{}

func (errLLM) Model() string { return "fake-model" }

func (errLLM) Complete(context.Context, string, string) (Call, error) {
	return Call{}, errors.New("gateway down")
}

// failingStore is a memStore that calls claimed after a claim and fails
// ending a row where told to.
type failingStore struct {
	*memStore
	claimed      func()
	finish, fail error
}

func (f failingStore) ClaimAnalysis(ctx context.Context, c store.AnalysisClaim) (int64, bool, error) {
	id, ok, err := f.memStore.ClaimAnalysis(ctx, c)
	if f.claimed != nil {
		f.claimed()
	}
	return id, ok, err
}

func (f failingStore) FinishAnalysis(ctx context.Context, id int64, r store.AnalysisResult) error {
	if f.finish != nil {
		return f.finish
	}
	return f.memStore.FinishAnalysis(ctx, id, r)
}

func (f failingStore) FailAnalysis(ctx context.Context, id int64, r store.AnalysisResult) error {
	if f.fail != nil {
		return f.fail
	}
	return f.memStore.FailAnalysis(ctx, id, r)
}

func TestFailureNamesItsStage(t *testing.T) {
	const head = `level=ERROR msg=analysis_failed team=team-a weeks=4 product="" trigger=scheduled model=fake-model `
	dbDown := errors.New("db down")
	for _, c := range []struct {
		name   string
		setup  func(s *Service, st *failingStore, cancel context.CancelFunc)
		status string
		want   string
	}{
		{name: "llm", setup: func(s *Service, _ *failingStore, _ context.CancelFunc) { s.LLM = errLLM{} },
			status: "failed", want: head + `stage=llm id=1 cost_nano_usd=0 err="gateway down"` + "\n"},
		{name: "queue", setup: func(_ *Service, st *failingStore, cancel context.CancelFunc) {
			// The run's context ends once the row is claimed.
			st.claimed = cancel
		}, status: "failed", want: head + `stage=queue id=1 cost_nano_usd=0 err="context canceled"` + "\n"},
		{name: "queue waiting", setup: func(s *Service, st *failingStore, cancel context.CancelFunc) {
			// Another call holds the model: with the slot taken, the ended
			// context is the only way out of the wait, whenever it ends.
			s.gate <- struct{}{}
			t.Cleanup(func() { <-s.gate })
			st.claimed = cancel
		}, status: "failed", want: head + `stage=queue id=1 cost_nano_usd=0 err="context canceled"` + "\n"},
		{name: "finish", setup: func(_ *Service, st *failingStore, _ context.CancelFunc) { st.finish = dbDown },
			status: "failed", want: head + `stage=finish id=1 cost_nano_usd=7000000 err="store: db down"` + "\n"},
		{name: "store", setup: func(_ *Service, st *failingStore, _ context.CancelFunc) {
			st.finish, st.fail = dbDown, dbDown
		}, status: "running", want: head + `stage=store id=1 err="db down"` + "\n" +
			head + `stage=finish id=1 cost_nano_usd=7000000 err="store: db down"` + "\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			mem := &memStore{}
			st := &failingStore{memStore: mem}
			s := service(&fakeLLM{answer: goodAnswer}, mem)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.setup(s, st, cancel)
			s.Store = st
			var logs strings.Builder
			s.Log = testLog(&logs)
			if err := s.Run(ctx, view, TriggerScheduled); err == nil {
				t.Fatal("no error")
			}
			if len(mem.rows) != 1 {
				t.Fatalf("rows = %+v", mem.rows)
			}
			if mem.rows[0].Status != c.status {
				t.Errorf("row = %+v", mem.rows[0])
			}
			if logs.String() != c.want {
				t.Errorf("log =\n%s\nwant\n%s", logs.String(), c.want)
			}
		})
	}
}

// A free slot and an ended context are both ready: select picks either,
// and the slot never stays taken. 64 rounds take both ways but once in 2^63.
func TestAcquireDeadContext(t *testing.T) {
	s := service(nil, &memStore{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 64 {
		if err := s.acquire(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
		if len(s.gate) != 0 {
			t.Fatal("a dead context holds the slot")
		}
	}
	if err := s.acquire(context.Background()); err != nil || len(s.gate) != 1 {
		t.Errorf("live context: %v, slot held %d", err, len(s.gate))
	}
}

func TestStatus(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	off, err := service(nil, st).Status(ctx, view)
	if err != nil || off.State != StateOff || off.Enabled || len(off.Assessment.Signals) == 0 {
		t.Fatalf("off = %+v, %v", off.State, err)
	}
	// The page's layout comes from here: the jammed repo in focus, its jam
	// covered, nothing left to spot.
	// Without the model the rules' lever, and the jam links to the PRs.
	want := LayoutFocus{Repo: "kasse/kasse-api", Lever: LeverWalkPRs, Tab: "prs"}
	if l := off.Layout; len(l.Focus) != 1 || l.Focus[0] != want || len(l.Also) != 0 {
		t.Errorf("layout without an analysis = %+v", l)
	}
	s := service(&fakeLLM{answer: goodAnswer}, st)
	none, _ := s.Status(ctx, view)
	if none.State != StateNone || none.NextRun == nil {
		t.Errorf("none = %s next %v", none.State, none.NextRun)
	}
	if err := s.Run(ctx, view, TriggerManual); err != nil {
		t.Fatal(err)
	}
	fresh, _ := s.Status(ctx, view)
	if fresh.State != StateFresh || fresh.Analysis == nil || *fresh.Analysis.CostUSD != "0.007" ||
		fresh.CooldownUntil == nil {
		t.Errorf("fresh = %+v", fresh)
	}
	if l := fresh.Layout; len(l.Focus) != 1 || l.Focus[0] != want {
		t.Errorf("layout of the analysis = %+v", l)
	}
	if err := s.Ask(ctx, view); !IsBusy(err) {
		t.Errorf("asked again within the cooldown: %v", err)
	}
	// Two weeks on, a 4-week analysis is too old to show; a 26-week one
	// would not be.
	s.Now = func() time.Time { return now.AddDate(0, 0, 14) }
	old, _ := s.Status(ctx, view)
	if old.State != StateNone || old.Analysis != nil {
		t.Errorf("a stale analysis shown: %s", old.State)
	}
	s.Now = func() time.Time { return now.AddDate(0, 0, 7) }
	week, _ := s.Status(ctx, view)
	if week.State != StateOlder || week.Analysis.WeeksOld != 1 {
		t.Errorf("a week on = %s", week.State)
	}
}

func TestManualPerHour(t *testing.T) {
	s := service(&fakeLLM{answer: goodAnswer}, &memStore{})
	at := now
	s.Now = func() time.Time { return at }
	for i := range ManualPerHour {
		if !s.takeManual() {
			t.Fatalf("run %d refused", i+1)
		}
		at = at.Add(time.Minute)
	}
	if s.takeManual() {
		t.Fatal("eleventh run allowed")
	}
	if err := s.Ask(context.Background(), view); !IsBusy(err) {
		t.Errorf("asked with the hour full: %v", err)
	}
	if _, held := s.inflight.Load(viewKey(view)); held {
		t.Error("a refused ask still holds the view")
	}
	s.inflight.Store(viewKey(view), true)
	if err := s.Ask(context.Background(), view); !IsBusy(err) {
		t.Errorf("asked while one is under way: %v", err)
	}
	s.inflight.Delete(viewKey(view))
	if err := (&Service{}).Ask(context.Background(), view); !IsOff(err) {
		t.Errorf("asked with no model: %v", err)
	}
	at = now.Add(time.Hour)
	if !s.takeManual() {
		t.Error("the first run's hour is over, still refused")
	}
	// A product the team's data does not know spends nothing.
	if err := s.Ask(context.Background(), View{Team: "team-a", Weeks: 4, Product: "made-up"}); !IsUnknownProduct(err) {
		t.Errorf("made-up product: %v", err)
	}
}

func TestHistory(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	s := service(&fakeLLM{answer: goodAnswer}, st)
	for _, d := range []int{-14, -7, 0} {
		s.Now = func() time.Time { return now.AddDate(0, 0, d) }
		if err := s.Run(ctx, view, TriggerScheduled); err != nil {
			t.Fatal(err)
		}
	}
	h, err := s.history(ctx, view, metrics.ClosedPeriod(4, now.AddDate(0, 0, 7), zurich).To)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Seen["pr_jam:kasse/kasse-api"]; got != 3 {
		t.Errorf("jam seen %d times in a row, want 3", got)
	}
	if h.Latest["pr_jam:kasse/kasse-api"] == "" || h.Runs != 3 {
		t.Errorf("latest = %v, runs %d", h.Latest, h.Runs)
	}
	// The view's first assessment has nothing to compare with; the next do.
	for i, want := range []bool{false, true, true} {
		var a metrics.Assessment
		if err := json.Unmarshal(st.rows[i].Assessment, &a); err != nil || a.Compared != want {
			t.Errorf("run %d compared %v, want %v (%v)", i+1, a.Compared, want, err)
		}
	}
	// A missed week ends the streak: two weeks on, the latest run is a week
	// behind the closed weeks, the one before two more.
	h, err = s.history(ctx, view, metrics.ClosedPeriod(4, now.AddDate(0, 0, 14), zurich).To)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Seen["pr_jam:kasse/kasse-api"]; got != 0 {
		t.Errorf("jam seen %d times across a missed week, want 0", got)
	}
}

// Reruns, however many, of a closed week or of the current one hide no
// earlier week from the history.
func TestHistoryPastReruns(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	s := service(&fakeLLM{answer: goodAnswer}, st)
	for _, d := range []int{-14, -7, 0} {
		s.Now = func() time.Time { return now.AddDate(0, 0, d) }
		if err := s.Run(ctx, view, TriggerScheduled); err != nil {
			t.Fatal(err)
		}
	}
	// 50 clicks on week W (now's closed weeks), then 50 on W+1 while it is
	// the current one.
	w := st.rows[2]
	for i := range 100 {
		r := w
		r.ID, r.Trigger = int64(len(st.rows)+1), TriggerManual
		r.FinishedAt = ptr(now.Add(time.Duration(i) * Cooldown))
		if i >= 50 {
			r.PeriodTo = w.PeriodTo.AddDate(0, 0, 7)
		}
		st.rows = append(st.rows, r)
	}
	h, err := s.history(ctx, view, w.PeriodTo.AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	if h.Runs != 3 || h.Seen["pr_jam:kasse/kasse-api"] != 3 {
		t.Errorf("runs %d, seen %v, want 3 weeks behind the reruns", h.Runs, h.Seen)
	}
}

// The schedule words all products and then each product with data, once
// per closed week; a product tab never shows all of them.
func TestTickWordsEveryProduct(t *testing.T) {
	llm := &fakeLLM{answer: goodAnswer}
	st := &memStore{}
	s := service(llm, st)
	ctx := context.Background()
	s.tick(ctx, []string{"team-a"})
	got := map[string]int{}
	for _, r := range st.rows {
		if r.Trigger != TriggerScheduled || r.Status != "done" {
			t.Errorf("row = %+v", r)
		}
		got[strconv.Itoa(r.Weeks)+"|"+r.Product]++
	}
	// KW 45 closed on 9 November: 4 weeks due, 26 not (not the month's
	// first Monday), 12 by its fortnight.
	want := map[string]int{"4|": 1, "4|kasse": 1}
	if due(12, weekStart(now, zurich)) {
		want["12|"], want["12|kasse"] = 1, 1
	}
	if !maps.Equal(got, want) {
		t.Errorf("scheduled views = %v, want %v", got, want)
	}
	calls := llm.calls
	s.tick(ctx, []string{"team-a"})
	if llm.calls != calls {
		t.Errorf("a second tick asked again: %d calls, then %d", calls, llm.calls)
	}
}

// A window's views read its two datasets once; a product with data but no
// repo active in it is left out, and promised nothing.
func TestTickLoadsOnceSkipsQuiet(t *testing.T) {
	llm := &fakeLLM{answer: goodAnswer}
	st := &memStore{}
	s := service(llm, st)
	var loadsN atomic.Int32
	s.Load = func(ctx context.Context, team string, p metrics.Period) (*dataset.Dataset, error) {
		loadsN.Add(1)
		ds, err := jammed(ctx, team, p)
		ds.Products = append(ds.Products, dataset.Product{Key: "idle", Repos: []string{}, Apps: []string{}})
		return ds, err
	}
	ctx := context.Background()
	s.tick(ctx, []string{"team-a"})
	windows := 1
	if due(12, weekStart(now, zurich)) {
		windows++
	}
	if int(loadsN.Load()) != 2*windows {
		t.Errorf("%d dataset loads for %d due windows, want 2 each", loadsN.Load(), windows)
	}
	for _, r := range st.rows {
		if r.Product == "idle" {
			t.Errorf("a quiet product was worded: %+v", r)
		}
	}
	idle, _ := s.Status(ctx, View{Team: "team-a", Weeks: 4, Product: "idle"})
	made, _ := s.Status(ctx, View{Team: "team-a", Weeks: 4, Product: "made-up"})
	kasse, _ := s.Status(ctx, View{Team: "team-a", Weeks: 4, Product: "kasse"})
	if idle.NextRun != nil || made.NextRun != nil || kasse.NextRun == nil {
		t.Errorf("next run: idle %v, made-up %v, kasse %v", idle.NextRun, made.NextRun, kasse.NextRun)
	}
	// The next tick asks nothing: the quiet product waits for next week.
	calls := llm.calls
	s.tick(ctx, []string{"team-a"})
	if llm.calls != calls {
		t.Errorf("asked again: %d, then %d calls", calls, llm.calls)
	}
}

// A dataset that does not load is not read again on every tick.
func TestProductsBackOff(t *testing.T) {
	s := service(&fakeLLM{answer: goodAnswer}, &memStore{})
	var loadsN atomic.Int32
	s.Load = func(context.Context, string, metrics.Period) (*dataset.Dataset, error) {
		loadsN.Add(1)
		return nil, io.ErrUnexpectedEOF
	}
	w := weekStart(now, zurich)
	// A page's failed read (a closed tab) does not hold the schedule back.
	s.productsFor(context.Background(), "team-a", 4, w, now, false)
	for range 3 {
		if p := s.productsFor(context.Background(), "team-a", 4, w, now, true); p != nil {
			t.Errorf("products of a failed load = %v", p)
		}
	}
	if loadsN.Load() != 2 {
		t.Errorf("%d loads, want the page's and one of the schedule's", loadsN.Load())
	}
	// Over, the backoff goes with the next week's sweep.
	s.sweepWeek(w.AddDate(0, 0, 7), now.Add(2*retryAfter))
	if _, ok := s.backoff.Load("products|team-a|4|" + strconv.FormatInt(w.Unix(), 10)); ok {
		t.Error("an expired backoff kept")
	}
	if ianaZone(time.Local) != "" || ianaZone(zurich) != "Europe/Zurich" {
		t.Errorf("zones: %q, %q", ianaZone(time.Local), ianaZone(zurich))
	}
}

// An analysis stored under older rules shows nothing of itself: neither
// its text nor its frozen signals and question. The page gets the rules'
// view now, and is told the analysis is outdated, to offer assessing again.
func TestOutdatedAnalysis(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	s := service(&fakeLLM{answer: goodAnswer}, st)
	if err := s.Run(ctx, view, TriggerScheduled); err != nil {
		t.Fatal(err)
	}
	cur, err := s.Status(ctx, view)
	if err != nil || cur.State != StateFresh || cur.Outdated || cur.Analysis == nil {
		t.Fatalf("an analysis of today's rules = %s, outdated %v (%v)", cur.State, cur.Outdated, err)
	}
	if *st.rows[0].RulesVersion != metrics.RulesVersion {
		t.Errorf("stored rules = %d", *st.rows[0].RulesVersion)
	}
	// A row as an older binary wrote it: no rules version, a card on the
	// longest stage and the fixed question about approvals.
	old := metrics.Assessment{
		Signals: []metrics.Signal{{ID: "slow_stage:to_prod", Kind: "slow_stage", Tone: metrics.ToneAttention,
			Facts: []metrics.Fact{{ID: "slow_stage.share", Label: "stage_share", Verdict: metrics.ChangeNone}}}},
		Question: &metrics.Signal{ID: "question:first_touch", Kind: metrics.SignalQuestion, Tone: metrics.ToneTalk},
	}
	st.rows[0].Assessment, _ = json.Marshal(old)
	for _, rules := range []*int{nil, ptr(metrics.RulesVersion - 1)} {
		st.rows[0].RulesVersion = rules
		got, err := s.Status(ctx, view)
		if err != nil || !got.Outdated || got.Analysis != nil || got.State != StateNone {
			t.Fatalf("rules %v: state %s, outdated %v, analysis %v (%v)", rules, got.State, got.Outdated, got.Analysis, err)
		}
		if got.Assessment.Question != nil || slices.ContainsFunc(got.Assessment.Signals, func(s metrics.Signal) bool {
			return s.Kind == "slow_stage"
		}) {
			t.Errorf("the old row's signals shown: %+v", got.Assessment)
		}
		if l := got.Layout; len(l.Focus) != 1 || l.Focus[0].Repo != "kasse/kasse-api" || l.Tally.Talk != 0 {
			t.Errorf("layout of the rules' view = %+v", l)
		}
	}
	// Without a model the same: off, outdated, the rules' view.
	got, err := service(nil, st).Status(ctx, view)
	if err != nil || got.State != StateOff || !got.Outdated || got.Analysis != nil {
		t.Errorf("off = %s, outdated %v (%v)", got.State, got.Outdated, err)
	}
	// Too old to show anyway: not outdated, simply none.
	s.Now = func() time.Time { return now.AddDate(0, 0, 14) }
	if got, _ := s.Status(ctx, view); got.Outdated || got.State != StateNone {
		t.Errorf("a stale old row = %s, outdated %v", got.State, got.Outdated)
	}
	if got.Zone != "Europe/Zurich" {
		t.Errorf("zone = %q", got.Zone)
	}
}

// The problems open since: one raised and no longer raised stays open with
// the value from before it was first raised; one resolved later is closed.
func TestHistoryOpen(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	s := service(&fakeLLM{answer: goodAnswer}, st)
	to := metrics.ClosedPeriod(4, now, zurich).To
	raised := func(prev float64) metrics.Signal {
		return metrics.Signal{ID: "change:lead_time", Kind: metrics.SignalChange, Tone: metrics.ToneAttention,
			Facts: []metrics.Fact{{ID: "change:lead_time.value", Prev: ptr(prev), Verdict: metrics.ChangeWorse}}}
	}
	week := func(weeksBack int, sigs ...metrics.Signal) {
		a := metrics.Assessment{Signals: sigs}
		raw, _ := json.Marshal(a)
		at := to.AddDate(0, 0, -7*weeksBack)
		st.rows = append(st.rows, store.Analysis{ID: int64(len(st.rows) + 1), Team: view.Team, Weeks: view.Weeks,
			Status: "done", Trigger: TriggerScheduled, PeriodTo: at, Assessment: raw, FinishedAt: ptr(at.Add(time.Hour)),
			RulesVersion: ptr(metrics.RulesVersion)})
	}
	// Raised three weeks back against 100, again against 140 (the previous
	// period drifting up), then no longer: the lead time stayed up.
	week(3, raised(100))
	week(2, raised(140))
	week(1)
	h, err := s.history(ctx, view, to)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := h.Open["change:lead_time"]
	if !ok || p.Tone != metrics.ToneAttention || p.Before == nil || *p.Before != 100 {
		t.Fatalf("open = %+v, want the lead time from before it was first raised", h.Open)
	}
	if _, ok := h.Latest["change:lead_time"]; ok {
		t.Errorf("latest = %v", h.Latest)
	}
	// Resolved since: closed.
	st.rows = nil
	week(3, raised(100))
	week(2, metrics.Signal{ID: "resolved:change:lead_time", Kind: metrics.SignalResolved, Tone: metrics.ToneGood})
	week(1)
	if h, _ = s.history(ctx, view, to); len(h.Open) != 0 {
		t.Errorf("open after a resolution = %+v", h.Open)
	}
	// Raised again after the resolution: open from the new raise on.
	week(0, raised(120))
	if h, _ = s.history(ctx, view, to.AddDate(0, 0, 7)); h.Open["change:lead_time"].Before == nil ||
		*h.Open["change:lead_time"].Before != 120 {
		t.Errorf("open after a new raise = %+v", h.Open)
	}
}

func TestDue(t *testing.T) {
	monday := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, zurich) }
	for _, c := range []struct {
		weeks int
		at    time.Time
		want  bool
	}{
		{4, monday(9), true},
		{26, monday(2), true}, // first Monday of November
		{26, monday(9), false},
	} {
		if got := due(c.weeks, c.at); got != c.want {
			t.Errorf("due(%d, %s) = %v", c.weeks, c.at.Format("02.01."), got)
		}
	}
	// 12 weeks every second Monday, also across 2026's KW 53: never two
	// weeks running, never a gap of three.
	at := time.Date(2026, 11, 2, 0, 0, 0, 0, zurich)
	last := -1
	for i := range 20 {
		w := at.AddDate(0, 0, 7*i)
		if !due(12, w) {
			continue
		}
		if last >= 0 && i-last != 2 {
			t.Errorf("12-week runs %d weeks apart at %s", i-last, w.Format("02.01.2006"))
		}
		last = i
	}
	s := service(&fakeLLM{answer: goodAnswer}, &memStore{})
	if got, ok := s.NextRun(26); !ok || !got.Equal(time.Date(2026, 12, 7, 6, 0, 0, 0, zurich)) {
		t.Errorf("next 26-week run %v (%v), want 07.12. 06:00", got, ok)
	}
	if got, ok := s.NextRun(4); !ok || !got.Equal(time.Date(2026, 11, 16, 6, 0, 0, 0, zurich)) {
		t.Errorf("next 4-week run %v (%v), want 16.11. 06:00", got, ok)
	}
	// A cadence never due ends the search instead of spinning.
	cadences[-1] = cadence{due: func(time.Time) bool { return false }, maxAge: 1}
	defer delete(cadences, -1)
	if got, ok := s.NextRun(-1); ok {
		t.Errorf("never-due cadence runs at %v", got)
	}
}

// Every cadence comes due within maxAge weeks of any week, and its widest
// gap between two due weeks is maxAge: MaxAge keeps the last analysis
// showing until the next. Mondays across years and DST switches in Zurich.
func TestCadenceGaps(t *testing.T) {
	all := map[string]cadence{"weekly": weekly}
	for w, c := range cadences {
		all[strconv.Itoa(w)] = c
	}
	first := weekStart(time.Date(2020, 1, 1, 12, 0, 0, 0, zurich), zurich)
	for name, c := range all {
		widest, last := 0, -1
		for i := range 52 * 12 {
			if !c.due(first.AddDate(0, 0, 7*i)) {
				continue
			}
			// The first due week counts from the start: due within maxAge.
			widest = max(widest, i-last)
			last = i
		}
		if widest != c.maxAge {
			t.Errorf("%s: widest gap %d weeks, maxAge %d", name, widest, c.maxAge)
		}
	}
}

func TestWireAsksForTheSchema(t *testing.T) {
	srv := llmwiretest.NewServer(t)
	srv.SetReply(goodAnswer(""))
	w, err := NewWire(llmwiretest.ChatModel, "", srv.Lookup, slog.New(slog.NewTextHandler(io.Discard, nil)),
		llmwiretest.Registry())
	if err != nil {
		t.Fatal(err)
	}
	call, err := w.Complete(context.Background(), promptText, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(call.Content, "Ein Stau bremst.") {
		t.Errorf("content = %q", call.Content)
	}
	last := srv.Last()
	rf, _ := json.Marshal(last.Body["response_format"])
	if !strings.Contains(string(rf), SchemaName) || !last.Stream() {
		t.Errorf("request: stream %v, format %s", last.Stream(), rf)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if call, err := w.Complete(cancelled, promptText, `{}`); err == nil || call.Content != "" {
		t.Errorf("cancelled call = %+v, %v", call, err)
	}
	if _, err := NewWire("no-such-model", "", srv.Lookup, nil, llmwiretest.Registry()); err == nil {
		t.Error("an unknown model built")
	}
}
