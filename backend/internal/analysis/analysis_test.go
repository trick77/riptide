package analysis

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
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

// jammed is a dataset whose repo kasse/kasse-api has three PRs open a week
// or longer at the period's end, all waiting on a first review.
func jammed(_ context.Context, team string, p metrics.Period) (*dataset.Dataset, error) {
	ds := &dataset.Dataset{
		Meta: dataset.Meta{Team: team, From: p.From, To: p.To, GeneratedAt: p.To, ProductionStage: "prod"},
		PRs:  []dataset.PR{}, Deploys: []dataset.Deploy{}, LeadTimes: []dataset.LeadTime{}, Runs: []dataset.Run{},
		Reviews: []dataset.Review{},
	}
	for i, days := range []int{9, 10, 12} {
		at := p.To.AddDate(0, 0, -days)
		ds.OpenPRs = append(ds.OpenPRs, dataset.OpenPR{
			Repo: "kasse/kasse-api", Project: "kasse", PRID: int64(i + 1), OpenedAt: at, ReadyAt: &at,
			LastEventAt: p.To.AddDate(0, 0, -1),
		})
	}
	return ds, nil
}

// memStore keeps analyses in memory, as the store's SQL does.
type memStore struct {
	mu   sync.Mutex
	rows []store.Analysis
}

func (m *memStore) ClaimAnalysis(_ context.Context, c store.AnalysisClaim) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.Trigger == TriggerScheduled {
		for i, r := range m.rows {
			if r.Trigger == TriggerScheduled && r.Team == c.View.Team && r.Weeks == c.View.Weeks && r.PeriodTo.Equal(c.To) {
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
		Bots: c.View.Bots, PeriodFrom: c.From, PeriodTo: c.To, Trigger: c.Trigger, Status: "running", Model: c.Model,
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
	return nil
}

func (m *memStore) FinishAnalysis(_ context.Context, id int64, r store.AnalysisResult) error {
	return m.end(id, "done", r)
}

func (m *memStore) FailAnalysis(_ context.Context, id int64, r store.AnalysisResult) error {
	return m.end(id, "failed", r)
}

func (m *memStore) DoneAnalyses(_ context.Context, v store.AnalysisView, limit int) ([]store.Analysis, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []store.Analysis
	for i := len(m.rows) - 1; i >= 0 && len(out) < limit; i-- {
		if r := m.rows[i]; r.Status == "done" && r.Team == v.Team && r.Weeks == v.Weeks && r.Product == v.Product {
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
		if r.Trigger == TriggerScheduled && r.Team == v.Team && r.Weeks == v.Weeks && r.PeriodTo.Equal(to) {
			return &r, nil
		}
	}
	return nil, nil
}

// fakeLLM answers with a function of the digest it got.
type fakeLLM struct {
	answer func(digest string) string
	calls  int
	digest string
}

func (f *fakeLLM) Model() string { return "fake-model" }

func (f *fakeLLM) Complete(_ context.Context, _, user string) (Call, error) {
	f.calls++
	f.digest = user
	return Call{Model: "fake-model", Content: f.answer(user), InputTokens: ptr(int64(1000)),
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
	for _, want := range []string{`"ci":{`, `"other_seconds"`, `"pr_jam:repo_a"`, `"repo":"repo_a"`, `"pr_jam:repo_a.long"`} {
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
	// A click on unchanged data asks nothing.
	if err := s.Run(ctx, view, TriggerManual); err != nil || llm.calls != 1 {
		t.Errorf("unchanged input asked again: %d calls, %v", llm.calls, err)
	}
}

func TestFailedAnswerIsStored(t *testing.T) {
	llm := &fakeLLM{answer: func(string) string { return "no json" }}
	st := &memStore{}
	if err := service(llm, st).Run(context.Background(), view, TriggerManual); err == nil {
		t.Fatal("no error")
	}
	r := st.rows[0]
	if r.Status != "failed" || r.Error == nil || r.CostNanoUSD == nil {
		t.Errorf("failed row = %+v: the failure and what it cost belong in it", r)
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
	if _, err := s.Ask(ctx, view); !IsBusy(err) {
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
		if _, ok := s.takeManual(); !ok {
			t.Fatalf("run %d refused", i+1)
		}
		at = at.Add(time.Minute)
	}
	until, ok := s.takeManual()
	if ok || !until.Equal(now.Add(time.Hour)) {
		t.Fatalf("eleventh run: ok %v until %v", ok, until)
	}
	at = now.Add(time.Hour)
	if _, ok := s.takeManual(); !ok {
		t.Error("the first run's hour is over, still refused")
	}
	// A product the team's data does not know spends nothing.
	if _, err := s.Ask(context.Background(), View{Team: "team-a", Weeks: 4, Product: "made-up"}); !IsUnknownProduct(err) {
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
	if h.Latest["pr_jam:kasse/kasse-api"] == "" {
		t.Errorf("latest = %v", h.Latest)
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
	if got, want := s.NextRun(26), time.Date(2026, 12, 7, 6, 0, 0, 0, zurich); !got.Equal(want) {
		t.Errorf("next 26-week run %v, want %v", got, want)
	}
	if got, want := s.NextRun(4), time.Date(2026, 11, 16, 6, 0, 0, 0, zurich); !got.Equal(want) {
		t.Errorf("next 4-week run %v, want %v", got, want)
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
	if _, err := NewWire("no-such-model", "", srv.Lookup, nil, llmwiretest.Registry()); err == nil {
		t.Error("an unknown model built")
	}
}
