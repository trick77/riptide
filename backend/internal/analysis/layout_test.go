package analysis

import (
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

// Gains and resolutions get their own slots on top of MaxCards, worded or
// not: ranked last, they would never get past the worries otherwise.
func TestLayoutSlots(t *testing.T) {
	a := metrics.Assessment{Signals: []metrics.Signal{
		sig("gap", metrics.ToneGate),
		sig("b1", metrics.ToneAttention), sig("b2", metrics.ToneAttention), sig("b3", metrics.ToneHint),
		sig("b4", metrics.ToneHint), sig("b5", metrics.ToneHint),
		sig("g1", metrics.ToneGood), sig("g2", metrics.ToneGood), sig("g3", metrics.ToneGood),
		sig("d1", "done"), sig("d2", "done"),
	}, Team: []metrics.Fact{{Verdict: metrics.ChangeNone}, {Verdict: metrics.ChangeBetter}}}
	want := Tally{Good: 3, Done: 2, Bad: 5, Baseline: true}

	off := layout(a, nil)
	got := columns(off.Cards)
	if !slices.Equal(got[ColumnBad], []string{"b1", "b2", "b3", "b4"}) ||
		!slices.Equal(got[ColumnGood], []string{"g1", "g2"}) || !slices.Equal(got[ColumnDone], []string{"d1"}) {
		t.Errorf("cards without words = %v", got)
	}
	if also := columns(off.Also); len(also[ColumnBad]) != 1 || len(also[ColumnGood]) != 1 || len(also[ColumnDone]) != 1 {
		t.Errorf("also without words = %v", also)
	}
	if off.Tally != want {
		t.Errorf("tally = %+v, want %+v", off.Tally, want)
	}

	// The model worded two worries and the third gain: those show, and the
	// best gain and a resolution join them unworded, the best gain first;
	// no worry does.
	text := &Text{Cards: []CardText{{Signal: "b2"}, {Signal: "b5"}, {Signal: "g3"}}}
	worded := layout(a, text)
	got = columns(worded.Cards)
	if !slices.Equal(got[ColumnBad], []string{"b2", "b5"}) ||
		!slices.Equal(got[ColumnGood], []string{"g1", "g3"}) || !slices.Equal(got[ColumnDone], []string{"d1"}) {
		t.Errorf("cards with words = %v", got)
	}
	if worded.Tally != want {
		t.Errorf("tally with words = %+v", worded.Tally)
	}

	// An analysis stored before small gains were signals: nothing good, no
	// comparison, the page says "unremarkable".
	old := metrics.Assessment{Signals: []metrics.Signal{sig("b1", metrics.ToneAttention)},
		Team: []metrics.Fact{{Verdict: metrics.ChangeNone}}}
	if l := layout(old, &Text{Cards: []CardText{{Signal: "b1"}}}); l.Tally != (Tally{Bad: 1}) || len(l.Cards) != 1 {
		t.Errorf("old row = %+v", l)
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
