package dataset_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// loadAt is load with the reader's now: a silence closing after the
// window's end is found up to it.
func loadAt(t *testing.T, s interface {
	Dataset(context.Context, dataset.Params) (*dataset.Dataset, error)
}, from, to, now time.Time) *dataset.Dataset {
	t.Helper()
	d, err := s.Dataset(context.Background(), dataset.Params{
		Team: team, From: from, To: to, ProductionStage: "prod", Automated: automated(t), Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// silences are a dataset's closed silences as "source from-to unseen", the
// instants in days after T0; ongoing the ones still running at the read.
func silences(d *dataset.Dataset) []string { return silencesOf(d, false) }

func ongoing(d *dataset.Dataset) []string { return silencesOf(d, true) }

func silencesOf(d *dataset.Dataset, running bool) []string {
	day := func(x time.Time) string { return fmt.Sprintf("%.1f", x.Sub(t0).Round(time.Minute).Hours()/24) }
	out := []string{}
	for _, x := range d.Meta.Silences {
		if x.Ongoing == running {
			out = append(out, fmt.Sprintf("%s %s-%s %d", x.Source, day(x.From), day(x.To), x.UnseenRuns))
		}
	}
	return out
}

// busy is a team at work from day from to day to: a push to repo's master
// and a build of it each day, so neither source is silent then.
func busy(s *seeder, repo, pipeline string, from, to float64) {
	s.t.Helper()
	for d := from; d <= to; d++ {
		s.push(repo, "master", days(d), commit{sha: sha("a"), authored: days(d)})
		s.run(run{pipeline: pipeline, runID: fmt.Sprintf("busy-%v", d), status: "SUCCESS", commit: sha("a"),
			started: days(d).Add(time.Minute)})
	}
}

func unknownPRs(d *dataset.Dataset) map[int64]bool {
	out := map[int64]bool{}
	for _, o := range d.OpenPRs {
		out[o.PRID] = o.Unknown
	}
	return out
}

// A PR merged in the UI has no push: Bitbucket knows its merge commit only
// from the pr:merged payload. CI building that commit right after is no run
// of an unseen commit, and the quiet days after are a team on holiday, not
// lost events: its open PR stays open.
func TestSilenceUIMergeCommitIsSeen(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const repo = "kasse/kasse-web"
	busy(s, repo, "kasse-web/master", -3, 1)
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 8, author: "dev-anna", branch: "feature/wait", state: "OPEN", at: days(0.3)})
	// Tuesday: the last Bitbucket event before the quiet days is a UI merge,
	// its merge commit in the sender's casing.
	m := strings.Repeat("9F", 20)
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 9, author: "dev-anna", branch: "feature/x", at: days(1.1)})
	s.pr(prEvent{event: "pr:merged", repo: repo, id: 9, author: "dev-anna", branch: "feature/x", mergeCommit: m, at: days(1.4)})
	s.run(run{pipeline: "kasse-web/master", runID: "2", status: "SUCCESS", commit: strings.ToLower(m), started: days(1.4).Add(time.Minute)})
	for _, x := range []float64{1.6, 2.5, 3.5} {
		s.deploy(team, "kasse-prod", "prod", sha("c"), "Succeeded", days(x), "reg/kasse:1")
	}
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 10, author: "dev-anna", branch: "feature/y", at: days(4.5)})
	d := load(t, st, t0, days(7))
	if got := silences(d); fmt.Sprint(got) != "[bitbucket 1.4-4.5 0]" {
		t.Errorf("silences = %v", got)
	}
	if u := unknownPRs(d); u[8] {
		t.Errorf("open PR unknown after a UI merge: %v", u)
	}
}

// A silence counts for a window when it has SilenceMin of working time in
// all and overlaps it: one straddling the boundary, 36 working hours on
// each side, is in both windows, and the open PR last heard of before it is
// unknown at the later window's end.
func TestSilenceStraddlingTheBoundary(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const repo = "kasse/kasse-web"
	busy(s, repo, "kasse-web/master", -10, -4)
	// Last Bitbucket event Thursday 12:00 before T0, a Monday; a push it lost
	// built on Friday; back on Tuesday 12:00.
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 8, author: "dev-anna", branch: "feature/wait", state: "OPEN", at: days(-3.5)})
	s.run(run{pipeline: "kasse-web/master", runID: "2", status: "SUCCESS", commit: strings.Repeat("5e", 20), started: days(-3)})
	for _, x := range []float64{-3.2, -2.5, 0.2, 1.2} {
		s.deploy(team, "kasse-prod", "prod", sha("c"), "Succeeded", days(x), "reg/kasse:1")
	}
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 10, author: "dev-anna", branch: "feature/y", at: days(1.5)})
	cur, prev := load(t, st, t0, days(28)), load(t, st, days(-28), t0)
	want := "[bitbucket -3.5-1.5 1]"
	if got := silences(cur); fmt.Sprint(got) != want {
		t.Errorf("current silences = %v, want %v", got, want)
	}
	if got := silences(prev); fmt.Sprint(got) != want {
		t.Errorf("previous silences = %v, want %v", got, want)
	}
	if u := unknownPRs(cur); !u[8] || u[10] {
		t.Errorf("unknown = %v", u)
	}
}

// A silence closing after the window's end counts for the window with the
// same minimum as one inside it: two quiet working days at the period's end
// are no less a hole for the closing event coming on Tuesday. A window
// ending before it starts knows nothing of it: no open PR turns unknown for
// a hole after its end.
func TestSilenceAtTheWindowEnd(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const repo = "kasse/kasse-web"
	busy(s, repo, "kasse-web/master", 22, 24)
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 8, author: "dev-anna", branch: "feature/wait", state: "OPEN", at: days(23)})
	// Last Bitbucket event Thursday 12:00 of the period's last week, a lost
	// push built on Friday, back on Tuesday 12:00 after the period's end.
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 9, author: "dev-anna", branch: "feature/x", state: "OPEN", at: days(24.5)})
	s.run(run{pipeline: "kasse-web/master", runID: "2", status: "SUCCESS", commit: strings.Repeat("5e", 20), started: days(25.2)})
	for _, x := range []float64{24.8, 25.5, 28.2} {
		s.deploy(team, "kasse-prod", "prod", sha("c"), "Succeeded", days(x), "reg/kasse:1")
	}
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 10, author: "dev-anna", branch: "feature/y", at: days(29.5)})
	d := loadAt(t, st, t0, days(28), days(35))
	if got := silences(d); fmt.Sprint(got) != "[bitbucket 24.5-29.5 1]" {
		t.Errorf("silences = %v", got)
	}
	if u := unknownPRs(d); !u[8] || !u[9] {
		t.Errorf("unknown = %v", u)
	}
	before := loadAt(t, st, t0, days(24), days(35))
	if got := silences(before); len(got) != 0 {
		t.Errorf("silences before the hole = %v", got)
	}
	if u := unknownPRs(before); u[8] {
		t.Errorf("unknown before the hole = %v", u)
	}
}

// A Bitbucket outage still running at the read (Monday's scheduled one)
// is a hole already: from its last event to now, while CI builds commits it
// never saw. Lost, ongoing; every open PR last heard of before it is
// unknown.
func TestSilenceStillOpenAtTheRead(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const repo = "kasse/kasse-web"
	busy(s, repo, "kasse-web/master", 21, 23)
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 8, author: "dev-anna", branch: "feature/wait", state: "OPEN", at: days(23.4)})
	// Bitbucket's last event Wednesday 09:36 of the period's last week; CI
	// builds a push it lost on Friday and on Monday morning.
	s.run(run{pipeline: "kasse-web/master", runID: "2", status: "SUCCESS", commit: strings.Repeat("5e", 20), started: days(24.5)})
	s.run(run{pipeline: "kasse-web/master", runID: "3", status: "SUCCESS", commit: strings.Repeat("5f", 20), started: days(28.2)})
	d := loadAt(t, st, t0, days(28), days(28.4))
	if got := ongoing(d); fmt.Sprint(got) != "[bitbucket 23.4-28.4 2]" {
		t.Errorf("ongoing = %v", got)
	}
	if len(d.Meta.Lost) != 1 || !d.Meta.Lost[0].Ongoing {
		t.Errorf("lost = %+v", d.Meta.Lost)
	}
	if u := unknownPRs(d); !u[8] {
		t.Errorf("unknown = %v", u)
	}
}

// Silences are read off when riptide stored the events, not when they say
// they happened: a sender's retry storing an old event days later, dated
// inside the hole, neither splits nor erases it.
func TestSilenceOnStoredTime(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const repo = "kasse/kasse-web"
	busy(s, repo, "kasse-web/master", -3, 1)
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 8, author: "dev-anna", branch: "feature/wait", state: "OPEN", at: days(1.4)})
	s.run(run{pipeline: "kasse-web/master", runID: "2", status: "SUCCESS", commit: strings.Repeat("5e", 20), started: days(2)})
	for _, x := range []float64{1.6, 2.5, 3.5} {
		s.deploy(team, "kasse-prod", "prod", sha("c"), "Succeeded", days(x), "reg/kasse:1")
	}
	// Dated Wednesday noon, inside the hole, stored with the rest on Friday.
	s.pr(prEvent{event: "pr:comment:added", repo: repo, id: 8, author: "rita", state: "OPEN", at: days(2.5), stored: days(4.4)})
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 10, author: "dev-anna", branch: "feature/y", at: days(4.4)})
	d := load(t, st, t0, days(7))
	if got := silences(d); fmt.Sprint(got) != "[bitbucket 1.4-4.4 1]" {
		t.Errorf("silences = %v", got)
	}
}
