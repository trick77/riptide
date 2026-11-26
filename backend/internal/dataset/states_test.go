package dataset_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// The approvals the states read: every PR approved since the period's
// start less the look-back, whether or not riptide saw it opened; the
// author's own approval and a bot's are none; a bot's PR is flagged.
func TestDatasetApprovals(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	// #1: opened while the events were lost: its approval and merge only.
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 1, author: "dev-ben", prAuthor: "dev-anna", at: days(3)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 1, author: "dev-carl", prAuthor: "dev-anna", at: days(6)})
	// #2: approved by its author first, then by a person; still open.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 2, author: "dev-anna", prAuthor: "dev-anna", branch: "feature/kas-2",
		at: days(1)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 2, author: "dev-anna", prAuthor: "dev-anna", at: days(2)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 2, author: "dev-ben", prAuthor: "dev-anna", at: days(4)})
	// #3: a bot's PR, approved by a person, declined.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 3, author: "renovate-bot", prAuthor: "renovate-bot",
		branch: "renovate/lib-1.x", automation: "renovate", at: days(1)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 3, author: "dev-ben", prAuthor: "renovate-bot",
		automation: "renovate", at: days(2)})
	s.pr(prEvent{event: "pr:declined", repo: kasseAPI, id: 3, author: "renovate-bot", prAuthor: "renovate-bot", at: days(5)})
	// #4: approved long before the look-back: not read.
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 4, author: "dev-ben", prAuthor: "dev-anna", at: days(-60)})
	// Another team's approval never shows.
	s.pr(prEvent{teamName: otherTeam, event: "pr:reviewer:approved", repo: "other/other-api", id: 1, author: "dev-zoe",
		prAuthor: "dev-yan", at: days(2)})

	sh, err := st.Shared(context.Background(), dataset.SharedParams{Team: team, From: t0, ClassEnds: []time.Time{days(10)},
		Now: days(28), States: true, ApprovalsFrom: t0, Automated: automated(t)})
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.Dataset(context.Background(), dataset.Params{Team: team, From: t0, To: days(10), ProductionStage: "prod",
		Automated: automated(t), Now: days(28), Shared: sh, CountsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	by := map[int64]dataset.Approval{}
	for _, a := range d.Approvals {
		by[a.PRID] = a
	}
	if len(by) != 3 {
		t.Fatalf("approvals = %+v, want #1, #2, #3", d.Approvals)
	}
	if a := by[1]; a.Opener != "dev-anna" || a.ApprovedAt == nil || !a.ApprovedAt.Equal(days(3)) || a.MergedAt == nil ||
		!a.MergedAt.Equal(days(6)) || a.Bot {
		t.Errorf("#1 = %+v, want dev-anna's, approved day 3, merged day 6", a)
	}
	if a := by[2]; a.ApprovedAt == nil || !a.ApprovedAt.Equal(days(4)) || a.MergedAt != nil || a.ClosedAt != nil {
		t.Errorf("#2 = %+v, want the person's approval on day 4, still open", a)
	}
	if a := by[3]; !a.Bot || a.ClosedAt == nil || !a.ClosedAt.Equal(days(5)) {
		t.Errorf("#3 = %+v, want a bot's PR, declined day 5", a)
	}
	// #2's approval stands from day 4: one span, open.
	if a := by[2]; len(a.Spans) != 1 || !a.Spans[0].From.Equal(days(4)) || a.Spans[0].To != nil ||
		a.LastActivity(days(10)) == nil || !a.LastActivity(days(10)).Equal(days(4)) {
		t.Errorf("#2 spans %+v activity %v, want approved from day 4 on", a.Spans, a.Activity)
	}
}

// An approval need not last: withdrawn by its reviewer, set to needs
// work, or reset by Bitbucket when the source branch moves (the payload's
// reviewer status says so); given again, it stands from then.
func TestDatasetApprovalSpans(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	ev := func(event string, id int64, author string, at float64, reviewers map[string]string) {
		s.pr(prEvent{event: event, repo: kasseAPI, id: id, author: author, prAuthor: "dev-anna", at: days(at),
			reviewers: reviewers})
	}
	// #1: approved day 1, withdrawn day 2 (unapproved), approved again day 3.
	ev("pr:reviewer:approved", 1, "dev-ben", 1, map[string]string{"dev-ben": "APPROVED"})
	ev("pr:reviewer:unapproved", 1, "dev-ben", 2, map[string]string{"dev-ben": "UNAPPROVED"})
	ev("pr:reviewer:approved", 1, "dev-ben", 3, map[string]string{"dev-ben": "APPROVED"})
	// #2: approved day 1, needs work by another reviewer day 2: Ben's
	// approval still stands.
	ev("pr:reviewer:approved", 2, "dev-ben", 1, map[string]string{"dev-ben": "APPROVED", "dev-carl": "UNAPPROVED"})
	ev("pr:reviewer:needs_work", 2, "dev-carl", 2, map[string]string{"dev-ben": "APPROVED", "dev-carl": "NEEDS_WORK"})
	// #3: approved day 1, a push day 2 resets it (the payload shows none
	// approving), a comment day 3 (no status read).
	ev("pr:reviewer:approved", 3, "dev-ben", 1, map[string]string{"dev-ben": "APPROVED"})
	ev("pr:from_ref_updated", 3, "dev-anna", 2, map[string]string{"dev-ben": "UNAPPROVED"})
	ev("pr:comment:added", 3, "dev-ben", 3, nil)
	// #4: approved by its author and by a bot only: no person's approval.
	ev("pr:reviewer:approved", 4, "dev-anna", 1, map[string]string{"dev-anna": "APPROVED"})
	sh, err := st.Shared(context.Background(), dataset.SharedParams{Team: team, From: t0, ClassEnds: []time.Time{days(10)},
		Now: days(28), States: true, ApprovalsFrom: t0, Automated: automated(t)})
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.Dataset(context.Background(), dataset.Params{Team: team, From: t0, To: days(10), ProductionStage: "prod",
		Automated: automated(t), Now: days(28), Shared: sh, CountsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	by := map[int64]dataset.Approval{}
	for _, a := range d.Approvals {
		by[a.PRID] = a
	}
	span := func(from float64, to *float64) dataset.ApprovalSpan {
		s := dataset.ApprovalSpan{From: days(from)}
		if to != nil {
			s.To = ptrTime(days(*to))
		}
		return s
	}
	same := func(got, want []dataset.ApprovalSpan) bool {
		return slices.EqualFunc(got, want, func(x, y dataset.ApprovalSpan) bool {
			return x.From.Equal(y.From) && ((x.To == nil && y.To == nil) || (x.To != nil && y.To != nil && x.To.Equal(*y.To)))
		})
	}
	two := 2.0
	if a := by[1]; !same(a.Spans, []dataset.ApprovalSpan{span(1, &two), span(3, nil)}) || !a.ApprovedAt.Equal(days(1)) {
		t.Errorf("#1 spans %+v, want day 1 to 2, then from day 3", a.Spans)
	}
	if a := by[2]; !same(a.Spans, []dataset.ApprovalSpan{span(1, nil)}) {
		t.Errorf("#2 spans %+v, want Ben's from day 1 on", a.Spans)
	}
	if a := by[3]; !same(a.Spans, []dataset.ApprovalSpan{span(1, &two)}) || a.ApprovedSince(days(5)) != nil ||
		!a.LastActivity(days(5)).Equal(days(3)) {
		t.Errorf("#3 spans %+v activity %v, want reset on day 2, the comment its last event", a.Spans, a.Activity)
	}
	if a := by[4]; len(a.Spans) != 0 || a.ApprovedAt != nil {
		t.Errorf("#4 = %+v, want no person's approval", a)
	}
}

// A lean window, the states' look-back: its merged PRs and deploys, no open
// PRs, runs or reviews.
func TestDatasetLeanWindow(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 1, author: "dev-anna", branch: "feature/kas-1", at: days(1)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 1, author: "dev-anna", branch: "feature/kas-1", at: days(2),
		mergeCommit: sha("m")})
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 2, author: "dev-anna", branch: "feature/kas-2", at: days(3)})
	s.run(run{pipeline: "kasse-api/master", runID: "1", commit: sha("m"), status: "SUCCESS", started: days(2.5)})
	s.deploy(team, "kasse-prod", "prod", sha("d"), "Succeeded", days(4), "reg/kasse-api:1.0")
	sh, err := st.Shared(context.Background(), dataset.SharedParams{Team: team, From: t0, ClassEnds: []time.Time{days(10)},
		Now: days(28), Libraries: true, Trace: true})
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.Dataset(context.Background(), dataset.Params{Team: team, From: t0, To: days(10), ProductionStage: "prod",
		Automated: automated(t), Now: days(28), Shared: sh, ClassAt: days(10), Lean: true, FactsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.PRs) != 2 || len(d.Deploys) != 1 || len(d.OpenPRs) != 0 || len(d.Runs) != 0 || len(d.Reviews) != 0 {
		t.Errorf("lean window: %d PRs, %d deploys, %d open, %d runs, %d reviews; want PRs and deploys only",
			len(d.PRs), len(d.Deploys), len(d.OpenPRs), len(d.Runs), len(d.Reviews))
	}
}

// A tool's branch (its head commits by an .invalid identity) is a bot's on
// every read, the states' or not: its builds are a bot's (#8).
func TestDatasetToolBranchRunsAreBots(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.push(kasseAPI, "feature/describe-repo", days(2), commit{sha: sha("e"), authored: days(2), author: "Describer",
		email: "describer@tool.invalid"})
	s.push(kasseAPI, "feature/kas-1", days(2), commit{sha: sha("a"), authored: days(2)})
	s.run(run{pipeline: "kasse-api/feature%2Fdescribe-repo", runID: "1", commit: sha("e"), status: "FAILURE",
		started: days(3)})
	s.run(run{pipeline: "kasse-api/feature%2Fkas-1", runID: "2", commit: sha("a"), status: "FAILURE", started: days(3)})
	// The tool's PR from its branch, built as PR-7.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 7, author: "dev-anna", branch: "feature/describe-repo", at: days(2.5)})
	s.run(run{pipeline: "kasse-api/PR-7", runID: "3", commit: sha("e"), status: "FAILURE", started: days(3)})
	d := load(t, st, t0, days(10))
	bots := map[string]bool{}
	for _, r := range d.Runs {
		bots[r.Branch] = r.Bot
	}
	if len(bots) != 3 || !bots["feature/describe-repo"] || !bots["PR-7"] || bots["feature/kas-1"] {
		t.Errorf("bot runs %v, want the tool's branch's and its PR's builds a bot's, the person's not", bots)
	}
}

// The branches the states read: first and last push, a PR from it after
// its first push, a PR's target, a bot's.
func TestDatasetBranches(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.push(kasseAPI, "feature/kas-1", days(1), commit{sha: sha("a"), authored: days(1)})
	s.push(kasseAPI, "feature/kas-1", days(4), commit{sha: sha("b"), authored: days(4)})
	s.push(kasseAPI, "feature/kas-2", days(2), commit{sha: sha("c"), authored: days(2)})
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 2, author: "dev-anna", branch: "feature/kas-2", target: "develop",
		at: days(3)})
	s.push(kasseAPI, "develop", days(2), commit{sha: sha("d"), authored: days(2)})
	// A tool's commit (an e-mail of the .invalid domain) pushed by a person,
	// and a commit by an identity the config declares automation.
	s.push(kasseAPI, "feature/describe-repo", days(2), commit{sha: sha("e"), authored: days(2), author: "Describer",
		email: "describer@tool.invalid"})
	s.push(kasseAPI, "feature/helper", days(2), commit{sha: sha("f"), authored: days(2), author: "ci-helper"})
	// Merged onto the trunk without a PR: by a merge commit naming the
	// branch's head a parent, and by a fast-forward to it.
	s.push(kasseAPI, "feature/kas-3", days(2), commit{sha: sha("1"), authored: days(2)})
	s.push(kasseAPI, "master", days(3), commit{sha: sha("2"), authored: days(3), parents: []string{sha("0"), sha("1")}})
	s.push(kasseAPI, "feature/kas-4", days(2), commit{sha: sha("3"), authored: days(2)})
	s.push(kasseAPI, "master", days(3), commit{sha: sha("3"), authored: days(2)})
	// Cut from master's tip after it got there, no commit of its own.
	s.push(kasseAPI, "feature/kas-6", days(5), commit{sha: sha("3"), authored: days(2)})
	sh, err := st.Shared(context.Background(), dataset.SharedParams{Team: team, From: t0, ClassEnds: []time.Time{days(10)},
		Now: days(28), States: true, Automated: automated(t)})
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.Dataset(context.Background(), dataset.Params{Team: team, From: t0, To: days(10), ProductionStage: "prod",
		Automated: automated(t), Now: days(28), Shared: sh, CountsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]dataset.BranchLife{}
	for _, b := range d.Branches {
		by[b.Branch] = b
	}
	if b := by["feature/kas-1"]; !b.FirstAt.Equal(days(1)) || !b.LastAt.Equal(days(4)) || b.Pushes != 2 || b.PRAt != nil ||
		b.Target || b.Bot || b.MergedAt != nil || len(b.PushTimes) != 2 || !b.PushTimes[1].Equal(days(4)) {
		t.Errorf("kas-1 = %+v, want pushed days 1 and 4, no PR, not merged", b)
	}
	for _, name := range []string{"feature/kas-3", "feature/kas-4"} {
		if b := by[name]; b.MergedAt == nil || !b.MergedAt.Equal(days(3)) || b.PRAt != nil {
			t.Errorf("%s = %+v, want merged without a PR on day 3", name, b)
		}
	}
	// Cut from the trunk's tip with no commit of its own: on the trunk
	// before its first push, done.
	if b := by["feature/kas-6"]; b.MergedAt == nil || !b.MergedAt.Equal(days(3)) {
		t.Errorf("kas-6 = %+v, want its head on the trunk since day 3", b)
	}
	if b := by["feature/kas-2"]; b.PRAt == nil || !b.PRAt.Equal(days(3)) {
		t.Errorf("kas-2 = %+v, want its PR on day 3", b)
	}
	if b := by["develop"]; !b.Target {
		t.Errorf("develop = %+v, want a PR's target", b)
	}
	if b := by["feature/describe-repo"]; !b.Bot {
		t.Errorf("describe-repo = %+v, want a tool's branch", b)
	}
	if b := by["feature/helper"]; !b.Bot {
		t.Errorf("helper = %+v, want the declared automation's", b)
	}
	// Read since BranchesFrom: a branch pushed before it only is none.
	sh, err = st.Shared(context.Background(), dataset.SharedParams{Team: team, From: t0, ClassEnds: []time.Time{days(10)},
		Now: days(28), States: true, BranchesFrom: days(3), Automated: automated(t)})
	if err != nil {
		t.Fatal(err)
	}
	d, err = st.Dataset(context.Background(), dataset.Params{Team: team, From: t0, To: days(10), ProductionStage: "prod",
		Automated: automated(t), Now: days(28), Shared: sh, CountsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(d.Branches, func(b dataset.BranchLife) bool { return b.Branch == "feature/kas-2" }) {
		t.Errorf("branches %+v, want none pushed only before BranchesFrom", d.Branches)
	}
}

// Every App's releases per environment over all history: images changed,
// or its first deploy; a re-sync of the same images and a failed deploy
// are none.
func TestDatasetReleases(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.deploy(team, "kasse-prod", "prod", sha("a"), "Succeeded", days(-40), "reg/kasse-api:1.0")
	s.deploy(team, "kasse-prod", "prod", sha("b"), "Succeeded", days(-30), "reg/kasse-api:1.0")
	s.deploy(team, "kasse-prod", "prod", sha("c"), "Failed", days(-20), "reg/kasse-api:1.1")
	s.deploy(team, "kasse-prod", "prod", sha("d"), "Succeeded", days(-10), "reg/kasse-api:1.1")
	s.deploy(team, "kasse-intg", "intg", sha("d"), "Succeeded", days(-12), "reg/kasse-api:1.1")
	d := load(t, st, t0, days(10))
	var got []string
	for _, r := range d.Releases {
		got = append(got, r.App+"/"+r.Environment+"@"+r.At.Format("01-02")+map[bool]string{true: " first", false: ""}[r.First])
	}
	want := []string{"kasse-prod/prod@01-23 first", "kasse-intg/intg@02-20 first", "kasse-prod/prod@02-22"}
	if len(got) != len(want) {
		t.Fatalf("releases = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("releases = %v, want %v", got, want)
		}
	}
}
