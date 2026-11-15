package metrics

import (
	"strings"
	"testing"

	"github.com/trick77/riptide/internal/dataset"
)

func openPR(mod ...func(*dataset.OpenPR)) dataset.OpenPR {
	o := dataset.OpenPR{
		Repo: "kasse/kasse-api", Project: "kasse", PRID: 20, Title: s("Refund by card"), ChangeType: s("feature"),
		JiraKeys: []string{"KAS-20"}, OpenedAt: ago(2), ReadyAt: agoP(2),
	}
	for _, m := range mod {
		m(&o)
	}
	return o
}

func TestWaitsOn(t *testing.T) {
	for _, c := range []struct {
		name string
		mod  func(*dataset.OpenPR)
		want string
	}{
		{"untouched", func(*dataset.OpenPR) {}, WaitFirstReview},
		{"touched", func(o *dataset.OpenPR) { o.Touched = true }, WaitReview},
		{"approved", func(o *dataset.OpenPR) { o.Touched, o.Approved = true, true }, WaitMerge},
		// one reviewer's needs-work holds it, another's approval or not
		{"needs work", func(o *dataset.OpenPR) { o.Touched, o.Approved, o.NeedsWork = true, true, true }, WaitChanges},
		{"draft", func(o *dataset.OpenPR) { o.Draft, o.Touched, o.NeedsWork = true, true, true }, WaitDraft},
	} {
		if got := waitsOn(openPR(c.mod)); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestOpenPRs(t *testing.T) {
	id := int64(100)
	at := func(repo string, days float64, mod ...func(*dataset.OpenPR)) dataset.OpenPR {
		id++
		return openPR(append([]func(*dataset.OpenPR){func(o *dataset.OpenPR) {
			o.Repo, o.PRID, o.OpenedAt = repo, id, ago(days)
		}}, mod...)...)
	}
	ds := data(func(ds *dataset.Dataset) {
		ds.OpenPRs = []dataset.OpenPR{
			at("kasse/kasse-api", 30),
			at("kasse/kasse-api", 7, func(o *dataset.OpenPR) { o.Touched, o.Approved = true, true }), // exactly a week: long
			at("kasse/kasse-api", 1),
			at("kasse/kasse-web", 6.9, func(o *dataset.OpenPR) { o.Draft = true }),
			at("kasse/kasse-web", 2),
			at("portal/portal-web", 10, func(o *dataset.OpenPR) { o.Project = "portal" }),
			at("kasse/kasse-api", 40, func(o *dataset.OpenPR) { o.Bot = true }),
			// opened after the instant: never negative
			at("kasse/kasse-batch", -1),
		}
	})

	o := openSection(ds, filter{})
	if !o.At.Equal(now) || o.LongDays != 7 {
		t.Errorf("at %v, long days %d", o.At, o.LongDays)
	}
	if o.Open != 7 || o.Long != 3 {
		t.Errorf("open %d long %d, want 7 and 3 (bot out)", o.Open, o.Long)
	}
	if w := o.Waiting; w.FirstReview != 5 || w.Merge != 1 || w.Draft != 1 || w.Review != 0 || w.Changes != 0 {
		t.Errorf("waiting = %+v", w)
	}
	if w := o.LongWaiting; w.FirstReview != 2 || w.Merge != 1 || w.Draft != 0 {
		t.Errorf("long waiting = %+v", w)
	}
	if o.Age.N != 7 {
		t.Errorf("age n = %d", o.Age.N)
	}
	// Long-open first by how many, then by the oldest; the rest after.
	var repos []string
	for _, r := range o.Repos {
		repos = append(repos, r.Repo)
	}
	if got := strings.Join(repos, " "); got != "kasse/kasse-api portal/portal-web kasse/kasse-web kasse/kasse-batch" {
		t.Errorf("repos = %s", got)
	}
	api := o.Repos[0]
	if api.Open != 3 || api.Long != 2 || !near(api.OldestS, 30*86400) || api.Waiting.Merge != 1 || api.Waiting.FirstReview != 2 {
		t.Errorf("kasse-api = %+v", api)
	}
	if batch := o.Repos[3]; batch.OldestS != 0 {
		t.Errorf("future PR age = %v", batch.OldestS)
	}

	// Bots counted in when asked; the product filters.
	if got := openSection(ds, filter{bots: true}); got.Open != 8 || got.Long != 4 {
		t.Errorf("with bots open %d long %d", got.Open, got.Long)
	}
	if got := openSection(ds, filter{product: "portal"}); got.Open != 1 || got.Long != 1 || len(got.Repos) != 1 {
		t.Errorf("portal = %+v", got)
	}
	// Nothing open: empty, not null.
	if got := openSection(data(), filter{}); got.Open != 0 || got.Repos == nil || len(got.Repos) != 0 || got.Age.P50 != nil {
		t.Errorf("empty = %+v", got)
	}
}

// The top 8 by long-open count and age, and every repo with a long-open PR
// past them.
func TestOpenPRsCut(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		for i := range 12 {
			days := 1.0
			if i >= 10 {
				days = 9
			}
			repo := "kasse/r" + string(rune('a'+i))
			ds.OpenPRs = append(ds.OpenPRs, openPR(func(o *dataset.OpenPR) {
				o.Repo, o.PRID, o.OpenedAt = repo, int64(i), ago(days)
			}))
		}
	})
	o := openSection(ds, filter{})
	if len(o.Repos) != openTop || o.Repos[0].Repo != "kasse/rk" || o.Repos[1].Repo != "kasse/rl" {
		t.Errorf("cut = %d rows, first %s", len(o.Repos), o.Repos[0].Repo)
	}
}

func TestComputeOpenPRsAgainstThePrevious(t *testing.T) {
	prevEnd := p4().From
	cur := data(func(ds *dataset.Dataset) {
		ds.OpenPRs = []dataset.OpenPR{
			openPR(func(o *dataset.OpenPR) { o.PRID, o.OpenedAt = 1, ago(40) }),
			openPR(func(o *dataset.OpenPR) { o.PRID, o.OpenedAt = 2, ago(9) }),
		}
	})
	prev := data(func(ds *dataset.Dataset) {
		ds.Meta.To = prevEnd
		ds.OpenPRs = []dataset.OpenPR{
			// the same PR, younger at the previous period's end
			openPR(func(o *dataset.OpenPR) { o.PRID, o.OpenedAt = 1, ago(40) }),
		}
	})
	m := Compute(cur, prev, Params{Period: p4(), Previous: PreviousPeriod(4, now, zurich)})
	if m.OpenPRs.Long != 2 {
		t.Fatalf("long = %d", m.OpenPRs.Long)
	}
	if c := m.Comparison.OpenLong; c.Verdict != ChangeWorse || c.Prev == nil || *c.Prev != 1 {
		t.Errorf("open long = %+v", c)
	}
	if c := m.Comparison.OpenPRs; c.Verdict != ChangeNone || show(c.Prev) != 1.0 {
		t.Errorf("open = %+v", c)
	}
	found := false
	for _, f := range m.Findings {
		if f.Kind == FindingOpenPRs && f.Tone == ToneAttention {
			found = true
		}
	}
	if !found {
		t.Errorf("findings = %+v", m.Findings)
	}
}

func TestFactsOpenPRsFirst(t *testing.T) {
	ds := data(func(ds *dataset.Dataset) {
		ds.PRs = []dataset.PR{
			pr(func(p *dataset.PR) { p.PRID = 1 }),
			// open in the window, and at its end
			pr(func(p *dataset.PR) {
				p.PRID, p.State, p.OpenedAt, p.MergedAt, p.ApprovedAt = 2, "open", agoP(3), nil, nil
			}),
		}
		ds.OpenPRs = []dataset.OpenPR{
			openPR(func(o *dataset.OpenPR) { o.PRID, o.OpenedAt = 2, ago(3) }),
			// opened long before the window: only here
			openPR(func(o *dataset.OpenPR) {
				o.PRID, o.OpenedAt, o.Title, o.Touched, o.NeedsWork = 3, ago(50), s("Old one"), true, true
			}),
		}
	})
	f := ComputeFacts(ds, all(), "")
	if f.PRs.Count != 3 {
		t.Fatalf("count = %d", f.PRs.Count)
	}
	r := f.PRs.Rows
	if r[0].PRID != 3 || r[1].PRID != 2 || r[2].PRID != 1 {
		t.Errorf("order = %d %d %d", r[0].PRID, r[1].PRID, r[2].PRID)
	}
	if r[0].State != "open" || r[0].AgeS == nil || !near(*r[0].AgeS, 50*86400) || r[0].Waiting == nil ||
		*r[0].Waiting != WaitChanges || str(r[0].Title) != "Old one" {
		t.Errorf("old open = %+v", r[0])
	}
	if r[2].AgeS != nil || r[2].Waiting != nil {
		t.Errorf("merged has age %v, waiting %v", r[2].AgeS, r[2].Waiting)
	}
	// What it waits on is searched in both languages.
	for _, q := range []string{"änderungen verlangt", "Changes requested"} {
		if got := ComputeFacts(ds, all(), q); got.PRs.Count != 1 || got.PRs.Rows[0].PRID != 3 {
			t.Errorf("%q = %d rows", q, got.PRs.Count)
		}
	}
	// An open row is a bot's as the open PRs count it, whatever the
	// window's row says.
	ds.OpenPRs[0].Bot = true
	if got := ComputeFacts(ds, all(), ""); got.PRs.Count != 2 {
		t.Errorf("bot's open PR listed: %d rows", got.PRs.Count)
	}
}

func str(v *string) string {
	if v == nil {
		return "<nil>"
	}
	return *v
}
