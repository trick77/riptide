package analysis

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/metrics"
	"github.com/trick77/riptide/internal/store"
)

// stateSig is a state card naming its units.
func stateSig(key, tone string, units ...string) metrics.Signal {
	c := &metrics.StateCard{Key: key, Unit: metrics.UnitProject, Count: len(units), Judged: len(units) + 1,
		Worst: []metrics.StateUnit{}, Units: []metrics.StateUnit{},
		Threshold: metrics.StateThreshold{Key: "release_breach_days", Value: 28, Unit: metrics.ThresholdDays}}
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
	if tab(a.Signals[0]) != "prs" {
		t.Errorf("a card without its facts table links the PRs")
	}
}

// The latest assessment's state units are what the next one holds by
// hysteresis.
func TestHistoryStateUnits(t *testing.T) {
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
	id := "state:" + metrics.StateNotOnProd
	if got := h.StateUnits[id]; !slices.Equal(got, []string{"kasse/kasse-api", "lager/lager-api"}) {
		t.Errorf("units = %v", got)
	}
	if _, ok := h.Open[id]; !ok {
		t.Errorf("a raised state is open: %v", h.Open)
	}
}

// The service passes the config's thresholds to the rules.
func TestAssessUsesThresholds(t *testing.T) {
	s := service(nil, &memStore{})
	called := false
	s.Thresholds = func() config.Thresholds {
		called = true
		return config.DefaultThresholds()
	}
	if _, err := s.assess(context.Background(), view); err != nil || !called {
		t.Errorf("assess: %v, thresholds read %v", err, called)
	}
}
