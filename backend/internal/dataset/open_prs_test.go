package dataset_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// Without any Argo CD data there is no GitOps repo: the empty set keeps
// every open PR (`<> ALL` of NULL would drop them all).
func TestDatasetOpenPRsWithoutGitOps(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 1, author: "dev-anna", branch: "feature/kas-1",
		state: "OPEN", at: days(1)})
	d := load(t, st, t0, days(10))
	if len(d.OpenPRs) != 1 || d.GitOpsRepos == nil || len(d.GitOpsRepos) != 0 {
		t.Errorf("open = %+v, gitops repos %#v", d.OpenPRs, d.GitOpsRepos)
	}
	if len(d.PRs) != 1 || d.PRs[0].GitOps {
		t.Errorf("prs = %+v", d.PRs)
	}
}

// The PRs open at the window's end, from the events before it only.
func TestDatasetOpenPRs(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	end := days(10)
	opened := func(id int64, at time.Time, mod ...func(*prEvent)) {
		e := prEvent{event: "pr:opened", repo: kasseAPI, id: id, author: "dev-anna", branch: "feature/kas-" + strconv.FormatInt(id, 10),
			jira: []string{"KAS-" + strconv.FormatInt(id, 10)}, state: "OPEN", created: at, at: at}
		for _, m := range mod {
			m(&e)
		}
		s.pr(e)
	}
	event := func(id int64, event, author, state string, at time.Time, mod ...func(*prEvent)) {
		e := prEvent{event: event, repo: kasseAPI, id: id, author: author, state: state, at: at}
		for _, m := range mod {
			m(&e)
		}
		s.pr(e)
	}

	// #21: opened long before the window, a reviewer commented: open.
	opened(21, days(-30))
	event(21, "pr:comment:added", "rita", "OPEN", days(-29))
	// #22: merged in the window. #23: merged after its end: open then.
	opened(22, days(1))
	event(22, "pr:merged", "dev-anna", "MERGED", days(2))
	opened(23, days(1))
	event(23, "pr:merged", "dev-anna", "MERGED", days(12))
	// #24: declined. #25: declined, then reopened (its next event carries
	// it open). #26: deleted.
	opened(24, days(1))
	event(24, "pr:declined", "dev-anna", "DECLINED", days(2))
	opened(25, days(1))
	event(25, "pr:declined", "dev-anna", "DECLINED", days(2))
	event(25, "pr:from_ref_updated", "dev-anna", "OPEN", days(3))
	opened(26, days(1))
	event(26, "pr:deleted", "dev-anna", "OPEN", days(2))
	// #27: opened before ingestion started: no pr:opened, its age from the
	// createdDate a later event carries, its opener the PR's author.
	event(27, "pr:comment:added", "rita", "OPEN", days(5), func(e *prEvent) {
		e.created, e.prAuthor, e.title = days(-60), "dev-otto", "Old work"
	})
	// #28: a draft.
	opened(28, days(1), func(e *prEvent) { e.draft = true })
	// #29: a person asked for changes, a bot approved: changes, no approval.
	opened(29, days(1))
	event(29, "pr:reviewer:needs_work", "rita", "OPEN", days(2), func(e *prEvent) {
		e.reviewers = map[string]string{"rita": "NEEDS_WORK", "ci-helper": "APPROVED"}
	})
	// #30: approved, the approval reset by a push since: Bitbucket's
	// status as the latest event carries it.
	opened(30, days(1))
	event(30, "pr:reviewer:approved", "rita", "OPEN", days(2), func(e *prEvent) {
		e.reviewers = map[string]string{"rita": "APPROVED"}
	})
	event(30, "pr:from_ref_updated", "dev-anna", "OPEN", days(3), func(e *prEvent) {
		e.reviewers = map[string]string{"rita": "UNAPPROVED"}
	})
	// #31: opened after the window's end.
	opened(31, days(11))
	// #32: a renovate PR: a bot's.
	opened(32, days(1), func(e *prEvent) { e.author, e.automation, e.branch = "renovate-bot", "renovate", "renovate/lib-3" })
	// #33: a bot's PR opened before ingestion: its push tells.
	event(33, "pr:from_ref_updated", "svc-deps", "OPEN", days(2), func(e *prEvent) {
		e.automation, e.created, e.prAuthor, e.branch = "renovate", days(-40), "svc-deps", "renovate/lib-4"
	})
	// #34: declined, then a comment whose PR carries no state: still
	// declined.
	opened(34, days(1))
	event(34, "pr:declined", "dev-anna", "DECLINED", days(2))
	event(34, "pr:comment:added", "rita", "", days(3))
	// #35: approved by someone not asked to review: a participant.
	opened(35, days(1))
	event(35, "pr:reviewer:approved", "otto", "OPEN", days(2), func(e *prEvent) {
		e.participants = map[string]string{"otto": "APPROVED"}
	})
	// #36: idle since before ingestion, merged in the window: open at its
	// start. #37: declined before ingestion, commented since: never open.
	event(36, "pr:merged", "dev-anna", "MERGED", days(3), func(e *prEvent) { e.created = days(-40) })
	event(37, "pr:comment:added", "rita", "DECLINED", days(3), func(e *prEvent) { e.created = days(-40) })
	// #38: opened as a draft, made ready without the flip the parser
	// recognises, then commented: in review, not waiting for its first.
	opened(38, days(1), func(e *prEvent) { e.draft = true })
	event(38, "pr:modified", "dev-anna", "OPEN", days(2))
	event(38, "pr:comment:added", "rita", "OPEN", days(3))
	// A GitOps repo's PR and another team's: never.
	s.push(kasseOps, "master", days(1), commit{sha: sha("c"), authored: days(1), message: "kasse 1.1"})
	s.deploy(team, "kasse-prod", "prod", sha("c"), "Succeeded", days(2), "reg/kasse-api:1.1")
	s.pr(prEvent{event: "pr:opened", repo: kasseOps, id: 3, author: "dev-anna", branch: "kasse_PROD_1.1", state: "OPEN", at: days(1)})
	s.pr(prEvent{teamName: otherTeam, event: "pr:opened", repo: ledger, id: 4, author: "dev-otto", branch: "feature/l-4", state: "OPEN", at: days(1)})

	d := load(t, st, t0, end)
	byID := map[int64]dataset.OpenPR{}
	var ids []string
	for _, o := range d.OpenPRs {
		if o.Repo != kasseAPI {
			t.Errorf("open PR of %s", o.Repo)
		}
		byID[o.PRID] = o
		ids = append(ids, strconv.FormatInt(o.PRID, 10))
	}
	if got := strings.Join(ids, " "); got != "21 23 25 27 28 29 30 32 33 35 38" {
		t.Fatalf("open = %s", got)
	}
	if o33 := byID[33]; !o33.Bot || !o33.OpenedAt.Equal(days(-40)) {
		t.Errorf("#33 = %+v", o33)
	}
	if o38 := byID[38]; o38.Draft || !o38.Touched || o38.ReadyAt == nil || !o38.ReadyAt.Equal(days(1)) {
		t.Errorf("#38 = %+v", o38)
	}
	if o35 := byID[35]; !o35.Approved || !o35.Touched {
		t.Errorf("#35 = %+v", o35)
	}

	o21 := byID[21]
	if !o21.OpenedAt.Equal(days(-30)) || o21.Draft || !o21.Touched || o21.NeedsWork || o21.Approved || o21.Bot ||
		str(o21.ChangeType) != "feature" || !slices.Equal(o21.JiraKeys, []string{"KAS-21"}) ||
		o21.ReadyAt == nil || !o21.ReadyAt.Equal(days(-30)) {
		t.Errorf("#21 = %+v", o21)
	}
	if o21.OpenedAt.Location() != time.UTC || o21.ReadyAt.Location() != time.UTC {
		t.Error("open PR timestamps are not UTC")
	}
	o27 := byID[27]
	if !o27.OpenedAt.Equal(days(-60)) || str(o27.Title) != "Old work" || o27.ReadyAt == nil || !o27.ReadyAt.Equal(days(-60)) ||
		!o27.Touched || o27.Bot {
		t.Errorf("#27 = %+v", o27)
	}
	if o28 := byID[28]; !o28.Draft || o28.ReadyAt != nil {
		t.Errorf("#28 = %+v", o28)
	}
	if o29 := byID[29]; !o29.NeedsWork || o29.Approved || !o29.Touched {
		t.Errorf("#29 = %+v", o29)
	}
	if o30 := byID[30]; o30.Approved || !o30.Touched {
		t.Errorf("#30 = %+v", o30)
	}
	if !byID[32].Bot {
		t.Error("#32 is no bot's")
	}
	// The last event before the end, any kind: #23's merge came after.
	for id, want := range map[int64]time.Time{21: days(-29), 23: days(1), 25: days(3), 27: days(5)} {
		if got := byID[id].LastEventAt; !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("#%d last event %v, want %v", id, got, want)
		}
	}

	// The previous period's end reads what was open then: #21, and the
	// PRs open all along riptide heard of only later (#27, #33, #36), as
	// nothing but open: what they waited on then is unknown.
	prev := load(t, st, days(-28), t0)
	ids = nil
	byID = map[int64]dataset.OpenPR{}
	for _, o := range prev.OpenPRs {
		byID[o.PRID] = o
		ids = append(ids, strconv.FormatInt(o.PRID, 10))
	}
	if got := strings.Join(ids, " "); got != "21 27 33 36" {
		t.Errorf("open at the previous end = %s", got)
	}
	if o := byID[21]; !o.Touched {
		t.Errorf("previous #21 = %+v", o)
	}
	if o := byID[27]; o.Touched || o.ReadyAt != nil || !o.OpenedAt.Equal(days(-60)) {
		t.Errorf("previous #27 = %+v", o)
	}
	// Heard of only later: its later event shows it alive, never inactive
	// at the end.
	if o := byID[27]; !o.LastEventAt.Equal(t0) {
		t.Errorf("previous #27 last event %v", o.LastEventAt)
	}
	if o := byID[21]; !o.LastEventAt.Equal(days(-29)) {
		t.Errorf("previous #21 last event %v", o.LastEventAt)
	}

	// Products: a repo with only an open PR is a product's repo.
	if !slices.ContainsFunc(prev.Products, func(p dataset.Product) bool { return p.Key == "kasse" }) {
		t.Errorf("products = %+v", prev.Products)
	}
}
