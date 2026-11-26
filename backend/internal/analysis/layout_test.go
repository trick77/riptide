package analysis

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/trick77/riptide/internal/metrics"
)

// sig is a team-wide signal of the given tone; resolved ones by their kind.
func sig(id, tone string) metrics.Signal {
	kind := metrics.SignalChange
	if tone == "done" {
		kind, tone = metrics.SignalResolved, metrics.ToneGood
	}
	return metrics.Signal{ID: id, Kind: kind, Tone: tone,
		Facts: []metrics.Fact{{ID: id + ".value", Verdict: metrics.ChangeNone}}}
}

func columns(cs []LayoutCard) map[string][]string {
	out := map[string][]string{}
	for _, c := range cs {
		out[c.Column] = append(out[c.Column], c.Signal)
	}
	return out
}

// Gains, resolutions, hints and data honesty get their own slots on top of
// MaxCards, worded or not: ranked after the worries, they would never get
// past them otherwise. Hints are a neutral group of their own, never red
// (owner, 2026-11).
func TestLayoutSlots(t *testing.T) {
	a := metrics.Assessment{Signals: []metrics.Signal{
		sig("gap", metrics.ToneGate),
		sig("b1", metrics.ToneAttention), sig("b2", metrics.ToneAttention), sig("b3", metrics.ToneHint),
		sig("b4", metrics.ToneHint), sig("b5", metrics.ToneHint), sig("b6", metrics.ToneHint),
		sig("l1", metrics.ToneLimit), sig("l2", metrics.ToneLimit),
		sig("g1", metrics.ToneGood), sig("g2", metrics.ToneGood), sig("g3", metrics.ToneGood),
		sig("d1", "done"), sig("d2", "done"),
	}, Team: []metrics.Fact{{Verdict: metrics.ChangeNone}, {Verdict: metrics.ChangeBetter}}}
	want := Tally{Good: 3, Done: 2, Bad: 2, Hint: 4, Limit: 2}

	off := layout(a, nil)
	got := columns(off.Cards)
	if !slices.Equal(got[ColumnBad], []string{"b1", "b2"}) || !slices.Equal(got[ColumnHint], []string{"b3", "b4", "b5"}) ||
		!slices.Equal(got[ColumnLimit], []string{"l1"}) ||
		!slices.Equal(got[ColumnGood], []string{"g1", "g2"}) || !slices.Equal(got[ColumnDone], []string{"d1"}) {
		t.Errorf("cards without words = %v", got)
	}
	if also := columns(off.Also); len(also[ColumnHint]) != 1 || len(also[ColumnLimit]) != 1 || len(also[ColumnGood]) != 1 ||
		len(also[ColumnDone]) != 1 {
		t.Errorf("also without words = %v", also)
	}
	if off.Tally != want {
		t.Errorf("tally = %+v, want %+v", off.Tally, want)
	}

	// The model worded a worry, a hint and the third gain: those show, the
	// best gain, hint, limit and a resolution join them unworded; no worry
	// does.
	text := &Text{Cards: []CardText{{Signal: "b2"}, {Signal: "b5"}, {Signal: "g3"}}}
	worded := layout(a, text)
	got = columns(worded.Cards)
	if !slices.Equal(got[ColumnBad], []string{"b2"}) || !slices.Equal(got[ColumnHint], []string{"b3", "b4", "b5"}) ||
		!slices.Equal(got[ColumnGood], []string{"g1", "g3"}) || !slices.Equal(got[ColumnDone], []string{"d1"}) {
		t.Errorf("cards with words = %v", got)
	}
	if worded.Tally != want {
		t.Errorf("tally with words = %+v", worded.Tally)
	}

	// The best gain worded takes one slot, not two: the next gain joins it.
	// No unworded worry does; the rest are chips.
	best := layout(a, &Text{Cards: []CardText{{Signal: "b1"}, {Signal: "g1"}}})
	got = columns(best.Cards)
	if !slices.Equal(got[ColumnBad], []string{"b1"}) || !slices.Equal(got[ColumnGood], []string{"g1", "g2"}) ||
		!slices.Equal(got[ColumnDone], []string{"d1"}) {
		t.Errorf("cards with the best gain worded = %v", got)
	}
	if also := columns(best.Also); !slices.Equal(also[ColumnGood], []string{"g3"}) || !slices.Equal(also[ColumnDone], []string{"d2"}) {
		t.Errorf("also with the best gain worded = %v", also)
	}
	// A worded resolution books its one slot once too: d1 shows, d2 a chip.
	got = columns(layout(a, &Text{Cards: []CardText{{Signal: "d1"}, {Signal: "g1"}}}).Cards)
	if !slices.Equal(got[ColumnGood], []string{"g1", "g2"}) || !slices.Equal(got[ColumnDone], []string{"d1"}) {
		t.Errorf("cards with the first resolution worded = %v", got)
	}

	// The model worded only lesser gains and resolutions: the best of each
	// keeps its slot in the rules' words, the worded ones show beside it.
	got = columns(layout(a, &Text{Cards: []CardText{{Signal: "g2"}, {Signal: "g3"}, {Signal: "d2"}}}).Cards)
	if !slices.Equal(got[ColumnGood], []string{"g1", "g2", "g3"}) || !slices.Equal(got[ColumnDone], []string{"d1", "d2"}) {
		t.Errorf("cards with lesser gains worded = %v", got)
	}

	// Red states the model did not word take the red slots it left free, in
	// the backend's rank, up to MaxCards; a red change it did not word stays
	// a chip.
	reds := metrics.Assessment{Signals: []metrics.Signal{
		stateSig(metrics.StateNoRelease, metrics.ToneAttention, "kasse"),
		stateSig(metrics.StateNotOnProd, metrics.ToneAttention, "kasse/kasse-api"),
		sig("change:lead_time", metrics.ToneAttention),
		stateSig(metrics.StateMergePause, metrics.ToneAttention, "kasse"),
		stateSig(metrics.StateTrunkFailing, metrics.ToneAttention, "kasse/kasse-api"),
		stateSig(metrics.StateDeployFailing, metrics.ToneAttention, "kasse-prod"),
		stateSig(metrics.StateBotLoop, metrics.ToneHint, "kasse/kasse-api:renovate/x"),
	}}
	l := layout(reds, &Text{Cards: []CardText{{Signal: "state:merge_pause"}, {Signal: "state:bot_loop"}}})
	got = columns(l.Cards)
	if !slices.Equal(got[ColumnBad], []string{"state:merge_pause", "state:no_release", "state:not_on_prod", "state:trunk_failing"}) ||
		!slices.Equal(got[ColumnHint], []string{"state:bot_loop"}) {
		t.Errorf("cards with one red worded = %v, want three more red states in rank", got)
	}
	if also := columns(l.Also); !slices.Equal(also[ColumnBad], []string{"change:lead_time", "state:deploy_failing"}) {
		t.Errorf("also = %v, want the red change and the red state past the slots", also)
	}

	// An analysis stored before small gains were signals: nothing good, no
	// comparison, the page says "unremarkable".
	old := metrics.Assessment{Signals: []metrics.Signal{sig("b1", metrics.ToneAttention)},
		Team: []metrics.Fact{{Verdict: metrics.ChangeNone}}}
	if l := layout(old, &Text{Cards: []CardText{{Signal: "b1"}}}); l.Tally != (Tally{Bad: 1}) || len(l.Cards) != 1 {
		t.Errorf("old row = %+v", l)
	}
}

// The question counts in the tally: a number that moved notably stands
// out. Without one, nothing is counted for it.
func TestTallyCountsTheQuestion(t *testing.T) {
	a := metrics.Assessment{Signals: []metrics.Signal{sig("b1", metrics.ToneAttention)},
		Question: &metrics.Signal{ID: "question:open_prs", Kind: metrics.SignalQuestion, Tone: metrics.ToneTalk}}
	if got := tally(a); got != (Tally{Bad: 1, Talk: 1}) {
		t.Errorf("tally = %+v", got)
	}
	a.Question = nil
	if got := tally(a); got != (Tally{Bad: 1}) {
		t.Errorf("tally without a question = %+v", got)
	}
}

// A resolution carries no step: worded with one, the card is dropped;
// without, it stays. Rule-based, it has no lever either.
func TestResolvedWithoutLever(t *testing.T) {
	res := sig("resolved:change:lead_time", "done")
	a := metrics.Assessment{Signals: []metrics.Signal{res}}
	if l := layout(a, nil); len(l.Cards) != 1 || l.Cards[0].Lever != "" {
		t.Errorf("rule card = %+v", l.Cards)
	}
	al := newAliases(a)
	answer := func(lever string) string {
		text := Text{Headline: "Die Lead Time ist zurück.", Focus: []FocusText{},
			Cards: []CardText{{Signal: res.ID, Take: "Wieder wie vorher.", Lever: lever}}}
		b, _ := json.Marshal(Output{DE: text, EN: text})
		return string(b)
	}
	if out, _, err := validate(answer(""), a, al); err != nil || len(out.DE.Cards) != 1 {
		t.Errorf("without a lever: %+v, %v", out.DE.Cards, err)
	}
	if out, rep, err := validate(answer(LeverNameWhatHelped), a, al); err != nil || len(out.DE.Cards) != 0 || len(rep.Dropped) == 0 {
		t.Errorf("with a lever: %+v, dropped %v, %v", out.DE.Cards, rep.Dropped, err)
	}
}

// Gains and resolutions show as ranked, across both columns, whichever the
// model worded.
func TestLayoutKeepsRankAcrossColumns(t *testing.T) {
	a := metrics.Assessment{Signals: []metrics.Signal{
		sig("g1", metrics.ToneGood), sig("g2", metrics.ToneGood), sig("d1", "done"),
	}}
	var ids []string
	for _, c := range layout(a, &Text{Cards: []CardText{{Signal: "g2"}}}).Cards {
		ids = append(ids, c.Signal)
	}
	if !slices.Equal(ids, []string{"g1", "g2", "d1"}) {
		t.Errorf("cards = %v", ids)
	}
}

// A card the model worded shows though a focus repo carries its signal (a
// review worsening's drivers do): its words are never dropped.
func TestWordedCardBesideItsFocus(t *testing.T) {
	a := metrics.Assessment{
		Signals: []metrics.Signal{sig("review", metrics.ToneAttention), sig("b2", metrics.ToneAttention)},
		Focus:   []metrics.FocusRepo{{Repo: "kasse/kasse-api", Signals: []string{"review"}}},
	}
	l := layout(a, &Text{Focus: []FocusText{{Repo: "kasse/kasse-api"}}, Cards: []CardText{{Signal: "review", Lever: "pairing"}}})
	if got := columns(l.Cards); !slices.Equal(got[ColumnBad], []string{"review"}) || l.Cards[0].Lever != "pairing" {
		t.Errorf("cards = %+v", l.Cards)
	}
	if len(l.Focus) != 1 {
		t.Errorf("focus = %+v", l.Focus)
	}
	// Unworded, the focus speaks for it: no card.
	if l := layout(a, &Text{Focus: []FocusText{{Repo: "kasse/kasse-api"}}}); len(l.Cards) != 0 {
		t.Errorf("unworded cards = %+v", l.Cards)
	}
}

// The strongest gain is mandatory even when it ranks past MandatoryTop.
func TestMandatoryGood(t *testing.T) {
	a := metrics.Assessment{Signals: []metrics.Signal{
		sig("gap", metrics.ToneGate), sig("b1", metrics.ToneAttention), sig("b2", metrics.ToneAttention),
		sig("b3", metrics.ToneHint), sig("b4", metrics.ToneHint), sig("g1", metrics.ToneGood), sig("g2", metrics.ToneGood),
	}}
	if got := mandatory(a); !slices.Equal(got, []string{"b1", "b2", "b3", "g1"}) {
		t.Errorf("mandatory = %v", got)
	}
	a.Signals = []metrics.Signal{sig("b1", metrics.ToneAttention), sig("g1", metrics.ToneGood)}
	if got := mandatory(a); !slices.Equal(got, []string{"b1", "g1"}) {
		t.Errorf("mandatory, the gain among the top = %v", got)
	}
	// A resolution has its own slot: never mandatory past the top.
	a.Signals = []metrics.Signal{sig("b1", metrics.ToneAttention), sig("b2", metrics.ToneAttention),
		sig("b3", metrics.ToneHint), sig("d1", "done")}
	if got := mandatory(a); !slices.Equal(got, []string{"b1", "b2", "b3"}) {
		t.Errorf("mandatory with a resolution only = %v", got)
	}
}
