package analysis

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/metrics"
	"github.com/trick77/riptide/internal/store"
)

// stateSig is a state card naming its units.
func stateSig(key, tone string, units ...string) metrics.Signal {
	c := &metrics.StateCard{Key: key, Unit: metrics.UnitProject, Count: len(units), Judged: len(units) + 1,
		Worst: []metrics.StateUnit{}, Units: []metrics.StateUnit{},
		Threshold: &metrics.StateThreshold{Key: "release_breach_days", Value: 28, Unit: metrics.ThresholdDays}}
	for _, u := range units {
		c.Units = append(c.Units, metrics.StateUnit{Name: u, Value: 31, Names: []string{u + "-prod"}})
	}
	c.Worst = c.Units[:min(3, len(c.Units))]
	id := metrics.SignalState + ":" + key
	return metrics.Signal{ID: id, Kind: metrics.SignalState, Tone: tone, State: c,
		Facts: []metrics.Fact{{ID: id + ".count", Label: "state_units", Unit: metrics.UnitCount, Value: ptr(1.0)}}}
}

// The model reads a state's counts, never its units' names: the page names
// them. It gets the page's own words for every state.
func TestDigestStateWithoutNames(t *testing.T) {
	a := metrics.Assessment{Signals: []metrics.Signal{stateSig(metrics.StateNoRelease, metrics.ToneAttention, "lager")}}
	raw, _, err := buildDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	d := string(raw)
	if strings.Contains(d, "lager") {
		t.Errorf("a unit's name reached the digest: %s", d)
	}
	for _, want := range []string{`"key":"no_release"`, `"count":1`, `"Lange kein Release nach PROD"`, `"state:no_release.count"`} {
		if !strings.Contains(d, want) {
			t.Errorf("digest lacks %s", want)
		}
	}
	if got := leversFor(a.Signals[0]); !slices.Equal(got, []string{LeverPlanRelease}) {
		t.Errorf("levers = %v", got)
	}
	// A failed sync says nothing of a build; nothing to do about a unit the
	// data no longer judges.
	if got := leversFor(stateSig(metrics.StateDeployFailing, metrics.ToneAttention, "kasse-prod")); !slices.Equal(got,
		[]string{LeverInspectDeploys}) {
		t.Errorf("deploy levers = %v", got)
	}
	if got := leversFor(stateSig(metrics.StateUnmeasurable, metrics.ToneLimit)); len(got) != 0 {
		t.Errorf("unmeasurable levers = %v, want none", got)
	}
	// The prompt never teaches a fact id built from the key: an
	// unmeasurable card has ids of its own.
	if strings.Contains(promptText, "{{state:<key>") {
		t.Errorf("the prompt teaches a templated state fact id")
	}
	// Every state key has its page words and levers.
	for _, k := range []string{metrics.StateNoRelease, metrics.StateReleaseDue, metrics.StateNotOnProd, metrics.StateSlowToProd,
		metrics.StateMergePause, metrics.StateApprovedWaiting, metrics.StateBotLoop, metrics.StateUntraced,
		metrics.StateUnattributed, metrics.StateNoProdStage, metrics.StateUnmeasurable, metrics.StateDeployFailing,
		metrics.StateDeployFailures, metrics.StateFirstReviewWait, metrics.StateTrunkFailing, metrics.StateBranchFailing,
		metrics.StateFlakyBuilds, metrics.StateLongBuilds, metrics.StateReleaseFailing, metrics.StateBuildsSilent,
		metrics.StateBigPRs, metrics.StateFastApproval, metrics.StateUnapproved, metrics.StateLateDecline,
		metrics.StateBranchLife, metrics.StateBranchOpen, metrics.StateBranchSilent} {
		if g := states[k]; g.DE == "" || g.EN == "" || g.Means == "" || strings.ContainsRune(g.DE, 'ß') {
			t.Errorf("state %s gloss = %+v", k, g)
		}
		if _, ok := stateLevers[k]; !ok {
			t.Errorf("state %s has no levers entry", k)
		}
		if _, ok := labels["state_"+k]; !ok && k != metrics.StateUnmeasurable {
			t.Errorf("state %s has no fact label", k)
		}
	}
}

// The model's words for a state are the page's: its titles name the
// production stage the team configured, as the page writes it.
func TestDigestStateTitlesNameTheStage(t *testing.T) {
	for _, c := range []struct{ stage, want string }{{"prd", "Lange kein Release nach PRD"}, {"", "Lange kein Release nach PROD"}} {
		a := metrics.Assessment{ProductionStage: c.stage,
			Signals: []metrics.Signal{stateSig(metrics.StateNoRelease, metrics.ToneAttention, "lager")}}
		raw, _, err := buildDigest(a)
		if err != nil {
			t.Fatal(err)
		}
		if d := string(raw); !strings.Contains(d, `"`+c.want+`"`) || strings.Contains(d, metrics.StageWord) {
			t.Errorf("stage %q: digest lacks %q or keeps the placeholder", c.stage, c.want)
		}
	}
}

// Every state's title the model is told is the page's own, in both
// languages (ui/src/i18n: overview.assess.state.<key>.title).
func TestStateTitlesAreThePages(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		raw, err := os.ReadFile("../../../ui/src/i18n/" + lang + ".ts")
		if err != nil {
			t.Fatal(err)
		}
		for k, g := range states {
			if k == metrics.StateUnmeasurable {
				// The page's title names the state it was.
				continue
			}
			m := regexp.MustCompile(`(?m)^\s+` + k + `: \{\n\s+title: "([^"]*)"`).FindSubmatch(raw)
			want := map[string]string{"de": g.DE, "en": g.EN}[lang]
			if m == nil || string(m[1]) != want {
				t.Errorf("%s %s: page title %q, glossary %q", lang, k, m, want)
			}
		}
	}
}

// catalog is a UI catalog's text (ui/src/i18n/<lang>.ts).
func catalog(t *testing.T, lang string) string {
	t.Helper()
	raw, err := os.ReadFile("../../../ui/src/i18n/" + lang + ".ts")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// block is the catalog from the object opened by head on, so a key is read
// where the page reads it (overview.assess.lever, not freq.lever).
func block(t *testing.T, src, head string) string {
	t.Helper()
	i := strings.Index(src, "\n"+head+" {\n")
	if i < 0 {
		t.Fatalf("catalog has no %q block", head)
	}
	return src[i:]
}

// The model picks levers and names kinds in the page's own words: each
// lever's text and each kind's word is the catalog's, in both languages,
// the production stage as the page's placeholder.
func TestLeversAndKindsAreThePages(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		src := catalog(t, lang)
		pick := func(w Words) string { return map[string]string{"de": w.DE, "en": w.EN}[lang] }
		levers := block(t, src, "      lever:")
		for k, w := range leverText {
			m := regexp.MustCompile(`\n\s+` + k + `:\s+"([^"]*)"`).FindStringSubmatch(levers)
			if m == nil || m[1] != pick(w) {
				t.Errorf("%s lever %s: page %q, glossary %q", lang, k, m, pick(w))
			}
		}
		kindsSrc := block(t, src, "      kind:")
		for k, g := range kinds {
			m := regexp.MustCompile(`\n\s+` + k + `:\s+"([^"]*)"`).FindStringSubmatch(kindsSrc)
			if m == nil || m[1] != pick(g.word) {
				t.Errorf("%s kind %s: page %q, glossary %q", lang, k, m, pick(g.word))
			}
		}
	}
	// Every lever a signal may carry has its words.
	for _, ls := range stateLevers {
		for _, l := range ls {
			if _, ok := leverText[l]; !ok {
				t.Errorf("lever %s has no words", l)
			}
		}
	}
}

// pageText is what a language's page can show: its catalog's strings, its
// comments left out (a word named in a comment is on no page), and its
// facts terms (ui/src/i18n/terms.json).
func pageText(t *testing.T, lang string) string {
	t.Helper()
	var lines []string
	for _, l := range strings.Split(catalog(t, lang), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "//") {
			lines = append(lines, l)
		}
	}
	raw, err := os.ReadFile("../../../ui/src/i18n/terms.json")
	if err != nil {
		t.Fatal(err)
	}
	var terms map[string]json.RawMessage
	if err := json.Unmarshal(raw, &terms); err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n") + "\n" + string(terms[lang])
}

// The model's vocabulary is the page's: every word of it stands in the
// page text of its language (docs/metrics.md, "Words on the page").
func TestVocabularyIsThePages(t *testing.T) {
	paren := regexp.MustCompile(` \([^)]*\)`)
	for _, lang := range []string{"de", "en"} {
		src := strings.ToLower(pageText(t, lang))
		for _, v := range vocabulary {
			w := map[string]string{"de": v.DE, "en": v.EN}[lang]
			for _, part := range strings.Split(paren.ReplaceAllString(w, ""), ", ") {
				if !strings.Contains(src, strings.ToLower(part)) {
					t.Errorf("%s: %q (%s) is not on the page", lang, part, v.Thing)
				}
			}
		}
	}
	// The owner's rejected words: in the German never-list, and on no
	// German page (2026-11-26).
	de := pageText(t, "de")
	var notDE string
	for _, v := range vocabulary {
		notDE += ", " + v.NotDE
	}
	for _, w := range []string{"Hauptbranch", "Produkt", "Bibliothek", "das Ausrollen"} {
		if !strings.Contains(notDE, w) {
			t.Errorf("%q is in no never-list", w)
		}
	}
	for _, re := range []string{`Hauptbranch`, `Produkt(e|en|s|ansicht)?\b`, `Bibliothek`, `(das|dem|des) Ausrollen`} {
		if m := regexp.MustCompile(re).FindString(de); m != "" {
			t.Errorf("the German page writes %q", m)
		}
	}
	// A page word is in no never-list of its own language.
	for _, v := range vocabulary {
		for _, c := range []struct{ word, not string }{{v.DE, v.NotDE}, {v.EN, v.NotEN}} {
			for _, part := range strings.Split(paren.ReplaceAllString(c.word, ""), ", ") {
				if slices.Contains(strings.Split(paren.ReplaceAllString(c.not, ""), ", "), part) {
					t.Errorf("%s: %q is both the word and a never-word", v.Thing, part)
				}
			}
		}
	}
	// The prompt tells the model to keep to it.
	if !strings.Contains(promptText, "- words: the page's fixed vocabulary") || !strings.Contains(promptText, "Write the page's words only") {
		t.Errorf("the prompt does not hold the model to the page's words")
	}
	// The digest carries it with the view's stage, never the placeholder.
	for _, v := range vocabularyFor("PRD") {
		if strings.Contains(v.DE+v.EN, metrics.StageWord) {
			t.Errorf("vocabulary keeps the stage placeholder: %+v", v)
		}
	}
	raw, _, err := buildDigest(metrics.Assessment{Signals: []metrics.Signal{
		stateSig(metrics.StateNoRelease, metrics.ToneAttention, "lager"),
		{ID: "pr_jam:kasse/kasse-api", Kind: metrics.SignalPRJam, Tone: metrics.ToneAttention, Repo: ptr("kasse/kasse-api"),
			Facts: []metrics.Fact{}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"words":[`, `"PR-Grösse"`, `"Zeit vom ersten Commit bis PROD"`,
		`"word":{"de":"PR-Stau","en":"PR jam"}`, `"plan the next release to PROD, or name what holds it"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("digest lacks %s", want)
		}
	}
	// A state is named by its title: a hint or a limit is no "Befund".
	if strings.Count(string(raw), `"word":`) != 1 {
		t.Errorf("a state signal carries a kind word: %s", raw)
	}
}

// A state card is a team-wide card: worded with its own lever, it shows;
// a data-honesty card sits in its own column.
func TestStateCardsInLayout(t *testing.T) {
	a := metrics.Assessment{Signals: []metrics.Signal{
		stateSig(metrics.StateNoRelease, metrics.ToneAttention, "lager"),
		stateSig(metrics.StateBotLoop, metrics.ToneHint, "kasse/kasse-api:renovate/x"),
		stateSig(metrics.StateUntraced, metrics.ToneLimit, "kasse/kasse-web"),
	}}
	l := layout(a, nil)
	if got := columns(l.Cards); !slices.Equal(got[ColumnBad], []string{"state:no_release"}) ||
		!slices.Equal(got[ColumnHint], []string{"state:bot_loop"}) || !slices.Equal(got[ColumnLimit], []string{"state:untraced"}) {
		t.Errorf("columns = %v", got)
	}
	if l.Tally != (Tally{Bad: 1, Hint: 1, Limit: 1}) {
		t.Errorf("tally = %+v", l.Tally)
	}
	al := newAliases(a)
	text := Text{Headline: "Ein Projekt ohne Release.", Focus: []FocusText{},
		Cards: []CardText{{Signal: "state:no_release", Take: "{{state:no_release.count}} Projekt ohne Release.", Lever: LeverPlanRelease}}}
	b, _ := json.Marshal(Output{DE: text, EN: text})
	out, rep, err := validate(string(b), a, al)
	if err != nil || len(out.DE.Cards) != 1 || len(rep.Dropped) != 0 {
		t.Errorf("worded state card = %+v, dropped %v, %v", out.DE.Cards, rep.Dropped, err)
	}
	// A state card links its own facts table; one without links the PRs.
	a.Signals[0].State.Facts = "deploys"
	if got := tab(a.Signals[0]); got != "deploys" {
		t.Errorf("the release card links %q, want its deploys", got)
	}
	if got := tab(a.Signals[1]); got != "prs" {
		t.Errorf("a card without its facts table links %q", got)
	}
}

// A stored analysis counts a state's streak (seen), never its units: what
// the next assessment holds by hysteresis, resolves or calls no longer
// measurable is judged again from the data (metrics), so a view works the
// same without a model.
func TestHistoryStateSeen(t *testing.T) {
	st := &memStore{}
	s := service(&fakeLLM{answer: goodAnswer}, st)
	to := metrics.ClosedPeriod(4, now, zurich).To
	raw, _ := json.Marshal(metrics.Assessment{Signals: []metrics.Signal{
		stateSig(metrics.StateNotOnProd, metrics.ToneAttention, "kasse/kasse-api", "lager/lager-api")}})
	at := to.AddDate(0, 0, -7)
	st.rows = append(st.rows, store.Analysis{ID: 1, Team: view.Team, Weeks: view.Weeks, Status: "done",
		Trigger: TriggerScheduled, PeriodTo: at, Assessment: raw, FinishedAt: ptr(at.Add(time.Hour)),
		RulesVersion: ptr(metrics.RulesVersion)})
	h, err := s.history(context.Background(), view, to)
	if err != nil {
		t.Fatal(err)
	}
	if id := "state:" + metrics.StateNotOnProd; h.Seen[id] != 1 {
		t.Errorf("seen = %v, want the state seen once", h.Seen)
	}
}

// The service passes the config's thresholds to the rules: a changed one is
// the one the card prints.
func TestAssessUsesThresholds(t *testing.T) {
	s := service(nil, &memStore{})
	// The jammed PRs untouched: waiting for a first review.
	s.Load = each(func(ctx context.Context, team string, p metrics.Period) (*dataset.Dataset, error) {
		ds, err := jammed(ctx, team, p)
		for i := range ds.OpenPRs {
			ds.OpenPRs[i].Touched = false
		}
		return ds, err
	})
	s.Thresholds = func() config.Thresholds {
		th := config.DefaultThresholds()
		th.FirstReviewDays = 3.5
		return th
	}
	a, err := s.assess(context.Background(), view)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(a.Signals, func(s metrics.Signal) bool { return s.ID == "state:"+metrics.StateFirstReviewWait })
	if i < 0 || a.Signals[i].State.Threshold == nil || a.Signals[i].State.Threshold.Value != 3.5 {
		t.Errorf("signals %+v, want the first-review card against 3.5 working days", a.Signals)
	}
}
