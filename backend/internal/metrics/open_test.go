package metrics

import (
	"slices"
	"strings"
	"testing"

	"github.com/trick77/riptide/internal/dataset"
)

func openPR(mod ...func(*dataset.OpenPR)) dataset.OpenPR {
	o := dataset.OpenPR{
		Repo: "kasse/kasse-api", Project: "kasse", PRID: 20, Title: s("Refund by card"), ChangeType: s("feature"),
		JiraKeys: []string{"KAS-20"}, OpenedAt: ago(2), ReadyAt: agoP(2), LastEventAt: ago(0),
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
	idle := func(days float64) func(*dataset.OpenPR) {
		return func(o *dataset.OpenPR) { o.LastEventAt = ago(days) }
	}
	ds := data(func(ds *dataset.Dataset) {
		ds.OpenPRs = []dataset.OpenPR{
			at("kasse/kasse-api", 30),
			at("kasse/kasse-api", 7, func(o *dataset.OpenPR) { o.Touched, o.Approved = true, true }), // exactly a week: long
			at("kasse/kasse-api", 12),
			at("kasse/kasse-api", 1),
			// no event for 31 days: inactive, out of every count but its own
			at("kasse/kasse-api", 300, idle(31)),
			at("kasse/kasse-web", 6.9, func(o *dataset.OpenPR) { o.Draft = true }),
			at("kasse/kasse-web", 2),
			at("kasse/kasse-web", 20, idle(29.9)),
			at("kasse/kasse-web", 45, idle(30)), // exactly 30 days: inactive
			at("portal/portal-web", 10, func(o *dataset.OpenPR) { o.Project = "portal" }),
			at("kasse/kasse-api", 40, func(o *dataset.OpenPR) { o.Bot = true }),
			// opened after the instant: never negative
			at("kasse/kasse-batch", -1),
			// inactive PRs alone: no row
			at("kasse/kasse-old", 50, idle(40)),
		}
	})

	o := openSection(ds, filter{})
	if !o.At.Equal(now) || o.LongDays != 7 || o.JamMin != 3 || o.InactiveDays != 30 {
		t.Errorf("at %v, long days %d, jam min %d, inactive days %d", o.At, o.LongDays, o.JamMin, o.InactiveDays)
	}
	if o.Open != 9 || o.Long != 5 || o.Inactive != 3 {
		t.Errorf("open %d long %d inactive %d, want 9, 5 and 3 (bot out)", o.Open, o.Long, o.Inactive)
	}
	// kasse-api alone has three long-open PRs; kasse-web's inactive one
	// would have made its third.
	if o.Jammed != 1 || o.JammedLong != 3 {
		t.Errorf("jammed %d, their long %d", o.Jammed, o.JammedLong)
	}
	if w := o.Waiting; w.FirstReview != 7 || w.Merge != 1 || w.Draft != 1 || w.Review != 0 || w.Changes != 0 {
		t.Errorf("waiting = %+v", w)
	}
	if w := o.LongWaiting; w.FirstReview != 4 || w.Merge != 1 || w.Draft != 0 {
		t.Errorf("long waiting = %+v", w)
	}
	// The jammed repo's long-open ones only: kasse-web's and portal's
	// waiting for a first review left out.
	if w := o.JammedWaiting; w.FirstReview != 2 || w.Merge != 1 || w.Draft != 0 {
		t.Errorf("jammed waiting = %+v", w)
	}
	if o.Age.N != 9 {
		t.Errorf("age n = %d", o.Age.N)
	}
	// Jammed first, then by long-open PRs, then by the median age: portal's
	// one PR of 10 days before kasse-web's median of 6.9.
	var repos []string
	for _, r := range o.Repos {
		repos = append(repos, r.Repo)
	}
	if got := strings.Join(repos, " "); got != "kasse/kasse-api portal/portal-web kasse/kasse-web kasse/kasse-batch" {
		t.Errorf("repos = %s", got)
	}
	api := o.Repos[0]
	if api.Open != 4 || api.Long != 3 || !api.Jammed || api.Inactive != 1 || api.Age.N != 4 ||
		api.Waiting.Merge != 1 || api.Waiting.FirstReview != 3 {
		t.Errorf("kasse-api = %+v", api)
	}
	// What its long-open ones wait on: the 1-day PR's first review left out.
	if w := api.LongWaiting; w.FirstReview != 2 || w.Merge != 1 || w.Draft != 0 || w.Review != 0 || w.Changes != 0 {
		t.Errorf("kasse-api long waiting = %+v", w)
	}
	// kasse-web's draft is 6.9 days old, not long open; its 20-day one is.
	if web := o.Repos[2]; web.LongWaiting != (Waiting{FirstReview: 1}) {
		t.Errorf("kasse-web long waiting = %+v", web.LongWaiting)
	}
	// The median, which the 300-day PR does not move.
	if web := o.Repos[2]; web.Jammed || web.Inactive != 1 || web.Age.P50 == nil || !near(*web.Age.P50, 6.9*86400) {
		t.Errorf("kasse-web = %+v", web)
	}
	if batch := o.Repos[3]; batch.Age.P50 == nil || *batch.Age.P50 != 0 {
		t.Errorf("future PR age = %v", batch.Age.P50)
	}

	// Bots counted in when asked; the product filters.
	if got := openSection(ds, filter{bots: true}); got.Open != 10 || got.Long != 6 || got.Jammed != 1 {
		t.Errorf("with bots open %d long %d jammed %d", got.Open, got.Long, got.Jammed)
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
	id := int64(0)
	long := func(repo string, days float64) dataset.OpenPR {
		id++
		return openPR(func(o *dataset.OpenPR) {
			o.Repo, o.PRID, o.OpenedAt, o.LastEventAt = repo, id, ago(days), ago(days)
		})
	}
	// Opened 40 to 42 days ago, each with its last event then: three
	// long-open PRs at the previous period's end, inactive now.
	jam := func(repo string) []dataset.OpenPR {
		return []dataset.OpenPR{long(repo, 40), long(repo, 41), long(repo, 42)}
	}
	cur := data(func(ds *dataset.Dataset) {
		ds.OpenPRs = append(jam("kasse/kasse-old"),
			long("kasse/kasse-api", 9), long("kasse/kasse-api", 10), long("kasse/kasse-api", 11),
			long("kasse/kasse-web", 8), long("kasse/kasse-web", 9), long("kasse/kasse-web", 10))
	})
	prev := data(func(ds *dataset.Dataset) {
		ds.Meta.To = prevEnd
		ds.OpenPRs = jam("kasse/kasse-old")
	})
	m := Compute(cur, prev, Params{Period: p4(), Previous: PreviousPeriod(4, now, zurich)})
	if o := m.OpenPRs; o.Jammed != 2 || o.Long != 6 || o.Inactive != 3 {
		t.Fatalf("jammed %d long %d inactive %d", o.Jammed, o.Long, o.Inactive)
	}
	if c := m.Comparison.OpenJammed; c.Verdict != ChangeWorse || c.Prev == nil || *c.Prev != 1 {
		t.Errorf("open jammed = %+v", c)
	}
	if c := m.Comparison.OpenPRs; c.Verdict != ChangeNone || show(c.Prev) != 3.0 {
		t.Errorf("open = %+v", c)
	}
	// More jammed repos than at the previous end: each jam needs attention.
	a := Assess(cur, prev, Params{Period: p4(), Previous: PreviousPeriod(4, now, zurich)}, History{})
	for _, repo := range []string{"kasse/kasse-api", "kasse/kasse-web"} {
		if !slices.ContainsFunc(a.Signals, func(s Signal) bool {
			return s.ID == "pr_jam:"+repo && s.Tone == ToneAttention
		}) {
			t.Errorf("no attention for %s: %+v", repo, a.Signals)
		}
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
				o.PRID, o.OpenedAt, o.LastEventAt, o.Title, o.Touched, o.NeedsWork =
					3, ago(50), ago(5), s("Old one"), true, true
			}),
			// the oldest, inactive: after the open ones
			openPR(func(o *dataset.OpenPR) { o.PRID, o.OpenedAt, o.LastEventAt = 4, ago(90), ago(60) }),
		}
	})
	f := ComputeFacts(ds, all(), "")
	if f.PRs.Count != 4 {
		t.Fatalf("count = %d", f.PRs.Count)
	}
	r := f.PRs.Rows
	if r[0].PRID != 3 || r[1].PRID != 2 || r[2].PRID != 4 || r[3].PRID != 1 {
		t.Errorf("order = %d %d %d %d", r[0].PRID, r[1].PRID, r[2].PRID, r[3].PRID)
	}
	if r[0].State != "open" || r[0].AgeS == nil || !near(*r[0].AgeS, 50*86400) || r[0].Waiting == nil ||
		*r[0].Waiting != WaitChanges || str(r[0].Title) != "Old one" || r[0].IdleS == nil || !near(*r[0].IdleS, 5*86400) {
		t.Errorf("old open = %+v", r[0])
	}
	if r[2].Waiting == nil || *r[2].Waiting != WaitInactive || r[2].IdleS == nil || !near(*r[2].IdleS, 60*86400) {
		t.Errorf("inactive = %+v", r[2])
	}
	if r[3].AgeS != nil || r[3].Waiting != nil || r[3].IdleS != nil {
		t.Errorf("merged has age %v, waiting %v, idle %v", r[3].AgeS, r[3].Waiting, r[3].IdleS)
	}
	// What it waits on is searched in both languages, inactive too.
	for q, want := range map[string]int64{"änderungen verlangt": 3, "Changes requested": 3, "inaktiv": 4, "Inactive": 4} {
		if got := ComputeFacts(ds, all(), q); got.PRs.Count != 1 || got.PRs.Rows[0].PRID != want {
			t.Errorf("%q = %d rows", q, got.PRs.Count)
		}
	}
	// An open row is a bot's as the open PRs count it, whatever the
	// window's row says.
	ds.OpenPRs[0].Bot = true
	if got := ComputeFacts(ds, all(), ""); got.PRs.Count != 3 {
		t.Errorf("bot's open PR listed: %d rows", got.PRs.Count)
	}
}

func str(v *string) string {
	if v == nil {
		return "<nil>"
	}
	return *v
}
