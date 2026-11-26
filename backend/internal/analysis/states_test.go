package analysis

import (
	"context"
	"encoding/json"
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
		c.Units = append(c.Units, metrics.StateUnit{Name: u, Value: 37, Names: []string{u + "-prod"}})
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
