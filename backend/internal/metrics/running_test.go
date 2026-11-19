package metrics

import (
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// The page as /api/metrics builds it: four closed weeks (KW 42 to 45), the
// weeks before, and the week running now (KW 46, from Monday 9 November).
func closedParams() Params {
	return Params{Period: ClosedPeriod(4, now, zurich), Previous: ClosedPrevious(4, now, zurich),
		Running: RunningPeriod(now, zurich)}
}

// window is data() read from from to to, as the API reads a dataset.
func window(from, to time.Time, mod ...func(*dataset.Dataset)) *dataset.Dataset {
	ds := data(mod...)
	ds.Meta.From, ds.Meta.To = from.UTC(), to.UTC()
	return ds
}

func openRow(id int64, opened time.Time) dataset.OpenPR {
	return dataset.OpenPR{Repo: "kasse/kasse-api", Project: "kasse", PRID: id, Title: s("Open"),
		OpenedAt: opened, LastEventAt: opened, ReadyAt: &opened}
}

// A week still running is counted apart: no number of the period, no
// comparison, no week bar takes it; the running week's counts do, and the
// open PRs are the ones open now.
func TestTheRunningWeekCountsApart(t *testing.T) {
	p := closedParams()
	if len(p.Period.Weeks) != 4 || p.Period.Weeks[3].KW != 45 || !p.Period.To.Equal(p.Running.From) {
		t.Fatalf("period = %+v, running from %v", p.Period, p.Running.From)
	}
	ds := window(p.Period.From, p.Period.To, func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "r1", ago(5) }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "r2", ago(12) }),
		}
		ds.PRs = []dataset.PR{pr(func(x *dataset.PR) { x.PRID, x.OpenedAt, x.MergedAt = 2, agoP(7), agoP(6) })}
		// Open at the period's end: the assessment's snapshot, not the page's.
		ds.OpenPRs = []dataset.OpenPR{openRow(7, ago(20)), openRow(8, ago(20))}
	})
	prev := window(p.Previous.From, p.Previous.To)
	ds.Meta.DataEnd.Bitbucket = agoP(4)
	running := window(p.Running.From, now, func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{deploy(func(d *dataset.Deploy) { d.Revision, d.At = "r3", ago(1) })}
		ds.PRs = []dataset.PR{pr(func(x *dataset.PR) { x.PRID = 3 })}
		ds.Runs = []dataset.Run{run()}
		ds.OpenPRs = []dataset.OpenPR{openRow(9, ago(10))}
	})

	m := Compute(ds, prev, running, p)
	if f := m.Delivery.Frequency; f.Releases != 2 || len(f.Weeks) != 4 {
		t.Errorf("releases = %d over %d weeks, want 2 over 4: the running week's r3 is not the period's", f.Releases, len(f.Weeks))
	}
	expectNear(t, "per week", &m.Delivery.Frequency.PerWeek, 2/p.Period.Span)
	if m.Review.Merged != 1 {
		t.Errorf("merged = %d, want 1", m.Review.Merged)
	}
	rw := m.RunningWeek
	if rw.Week.KW != 46 || !rw.To.Equal(now) || rw.Releases != 1 || rw.Merged != 1 || rw.Runs != 1 {
		t.Errorf("running week = %+v", rw)
	}
	if o := m.OpenPRs; !o.At.Equal(now) || o.Open != 1 || o.Repos[0].Open != 1 {
		t.Errorf("open PRs at %v: %d, want now: 1", o.At, o.Open)
	}
	// The data's freshness is now's, not the period's end's.
	if e := m.Meta.DataEnd.Bitbucket; e == nil || !e.Equal(*running.Meta.DataEnd.Bitbucket) {
		t.Errorf("data end = %v, want the running read's", e)
	}

	// The assessment reads the same closed weeks: its team numbers are the
	// page's (same now, same zone). Its open PRs stay at the period's end.
	a := Assess(ds, prev, p, History{})
	found := false
	for _, f := range a.Team {
		if f.ID == "team.deploy_frequency" {
			found = true
			if f.Value == nil || !near(*f.Value, m.Delivery.Frequency.PerWeek) {
				t.Errorf("assessment's deploys per week = %v, the page's %v", show(f.Value), m.Delivery.Frequency.PerWeek)
			}
		}
	}
	if !found {
		t.Errorf("no team.deploy_frequency in %+v", a.Team)
	}
	if bare := Compute(ds, prev, nil, p); bare.OpenPRs.Open != 2 || !bare.OpenPRs.At.Equal(p.Period.To) {
		t.Errorf("without the running week: open PRs %d at %v, want 2 at the period's end", bare.OpenPRs.Open, bare.OpenPRs.At)
	}
}

// The facts read on to now; the running week's rows are marked, and the
// mark is a word the search finds. An open PR is a snapshot, never marked.
func TestFactsMarkTheRunningWeek(t *testing.T) {
	p := closedParams()
	ds := window(p.Period.From, now, func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "old", ago(5) }),
			deploy(func(d *dataset.Deploy) { d.Revision, d.At = "new", ago(1) }),
		}
		ds.PRs = []dataset.PR{
			pr(func(x *dataset.PR) { x.PRID, x.OpenedAt, x.MergedAt = 2, agoP(7), agoP(6) }),
			pr(func(x *dataset.PR) { x.PRID = 3 }),
			// Opened before, merged this week: the merge decides.
			pr(func(x *dataset.PR) { x.PRID, x.OpenedAt = 4, agoP(9) }),
			// Opened before, declined this week: the close decides.
			pr(func(x *dataset.PR) {
				x.PRID, x.State, x.OpenedAt, x.MergedAt, x.ClosedAt = 6, "declined", agoP(20), nil, agoP(2)
			}),
		}
		ds.Runs = []dataset.Run{
			run(func(r *dataset.Run) {
				r.RunID, r.StartedAt, r.FinishedAt, r.OccurredAt = "1", agoP(6), agoP(5.9), ago(5.9)
			}),
			run(func(r *dataset.Run) { r.RunID = "2" }),
			// Finished on Sunday, its event in on Monday: the running week's,
			// as the windows split runs on the event.
			run(func(r *dataset.Run) {
				r.RunID, r.StartedAt, r.FinishedAt, r.OccurredAt = "3", agoP(3.6), agoP(3.51), ago(3.49)
			}),
		}
		ds.OpenPRs = []dataset.OpenPR{openRow(9, ago(1))}
	})

	f := ComputeFacts(ds, p, "")
	prs := map[int64]bool{}
	for _, r := range f.PRs.Rows {
		prs[r.PRID] = r.Running
	}
	if len(prs) != 5 || prs[2] || !prs[3] || !prs[4] || !prs[6] || prs[9] {
		t.Errorf("PRs running = %v, want 3, 4 and 6 only", prs)
	}
	for _, r := range f.Deploys.Rows {
		if r.Running != (r.Revision == "new") {
			t.Errorf("deploy %s running = %v", r.Revision, r.Running)
		}
	}
	for _, r := range f.Runs.Rows {
		if r.Running != (r.RunID != "1") {
			t.Errorf("run %s running = %v", r.RunID, r.Running)
		}
	}
	// The mark's word lists the marked rows, not a title that says it.
	ds.PRs = append(ds.PRs, pr(func(x *dataset.PR) {
		x.PRID, x.Title, x.OpenedAt, x.MergedAt = 5, s("Report for this week"), agoP(9), agoP(8)
	}))
	for _, q := range []string{"This week", " laufende woche "} {
		s := ComputeFacts(ds, p, q)
		if s.PRs.Count != 3 || s.Deploys.Count != 1 || s.Runs.Count != 2 {
			t.Errorf("%q finds %d PRs, %d deploys, %d runs; want 3, 1, 2", q, s.PRs.Count, s.Deploys.Count, s.Runs.Count)
		}
	}
	if s := ComputeFacts(ds, p, "for this week"); s.PRs.Count != 1 {
		t.Errorf("a title search finds %d PRs, want 1", s.PRs.Count)
	}
}

// A run of the period is flaky as the build section saw it: a green rerun
// after the period's end does not make it flaky in the facts either.
func TestFactsFlakyStopsAtThePeriod(t *testing.T) {
	p := closedParams()
	red := run(func(r *dataset.Run) {
		r.RunID, r.Status, r.StartedAt, r.FinishedAt, r.OccurredAt = "1", s("FAILURE"), agoP(4), agoP(3.99), ago(3.99)
	})
	green := run(func(r *dataset.Run) { r.RunID = "2" })
	ds := window(p.Period.From, now, func(ds *dataset.Dataset) { ds.Runs = []dataset.Run{red, green} })
	for _, r := range ComputeFacts(ds, p, "").Runs.Rows {
		if r.Flaky {
			t.Errorf("run %s flaky: its rerun is after the period", r.RunID)
		}
	}
	period := window(p.Period.From, p.Period.To, func(ds *dataset.Dataset) { ds.Runs = []dataset.Run{red} })
	for _, b := range Compute(period, noPrevious(period), nil, p).Build.Kinds {
		if b.FlakyRuns != 0 {
			t.Errorf("build %s flaky runs = %d", b.Kind, b.FlakyRuns)
		}
	}
}

// A release rolled out across Monday 00:00 is an earlier week's, however
// far back it first deployed: the running week does not count it again,
// whatever period the page reads.
func TestARolloutAcrossMondayCountsOnce(t *testing.T) {
	p := closedParams()
	ds := window(p.Period.From, p.Period.To, func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{deploy(func(d *dataset.Deploy) {
			d.Revision, d.At, d.RevisionFirstAt = "r1", ago(3.6), agoP(3.6)
		})}
	})
	running := window(p.Running.From, now, func(ds *dataset.Dataset) {
		ds.Deploys = []dataset.Deploy{
			deploy(func(d *dataset.Deploy) {
				d.App, d.Revision, d.At, d.RevisionFirstAt = "kasse-b", "r1", ago(3.4), agoP(3.6)
			}),
			// First deployed six weeks ago, outside any 4-week period.
			deploy(func(d *dataset.Deploy) {
				d.App, d.Revision, d.At, d.RevisionFirstAt = "kasse-c", "r0", ago(2), agoP(42)
			}),
			deploy(func(d *dataset.Deploy) { d.Revision, d.At, d.RevisionFirstAt = "r2", ago(1), agoP(1) }),
		}
	})
	m := Compute(ds, noPrevious(ds), running, p)
	if m.Delivery.Frequency.Releases != 1 || m.RunningWeek.Releases != 1 {
		t.Errorf("period %d, running week %d releases; want 1 and 1", m.Delivery.Frequency.Releases, m.RunningWeek.Releases)
	}
	// Without the running week's period, no running week and no panic.
	bare := Compute(ds, noPrevious(ds), running, Params{Period: p.Period, Previous: p.Previous})
	if bare.RunningWeek.Week.KW != 0 || !bare.OpenPRs.At.Equal(p.Period.To) {
		t.Errorf("running week without its period = %+v", bare.RunningWeek)
	}
	if p.Period.LastDay != "2026-11-08" || p.Running.LastDay != "2026-11-12" {
		t.Errorf("last days %s, %s; want the period's Sunday and today", p.Period.LastDay, p.Running.LastDay)
	}
}
