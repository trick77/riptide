package dataset_test

import (
	"context"
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
	if b := by["feature/kas-1"]; !b.FirstAt.Equal(days(1)) || !b.LastAt.Equal(days(4)) || b.Pushes != 2 || b.HasPR || b.Target || b.Bot ||
		b.Merged {
		t.Errorf("kas-1 = %+v, want pushed days 1 and 4, no PR, not merged", b)
	}
	for _, name := range []string{"feature/kas-3", "feature/kas-4"} {
		if b := by[name]; !b.Merged || b.HasPR {
			t.Errorf("%s = %+v, want merged without a PR", name, b)
		}
	}
	if b := by["feature/kas-2"]; !b.HasPR {
		t.Errorf("kas-2 = %+v, want its PR", b)
	}
	if b := by["develop"]; !b.Target {
		t.Errorf("develop = %+v, want a PR's target", b)
	}
	if b := by["feature/describe-repo"]; !b.Tool || !b.Bot {
		t.Errorf("describe-repo = %+v, want a tool's branch", b)
	}
	if b := by["feature/helper"]; b.Tool || !b.Bot {
		t.Errorf("helper = %+v, want the declared automation's", b)
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
