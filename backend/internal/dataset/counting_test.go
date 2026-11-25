package dataset_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/metrics"
)

// An App's first deploy counts as a release once Argo CD has reported for
// the team longer than FirstDeployGrace; before, it stays first_seen. The
// Kind stays first_seen either way: it is still the App's first deploy.
func TestDatasetFirstDeployAsRelease(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	s := &seeder{t: t, pool: pool, n: 1000}
	// Exactly FirstDeployGrace after Argo CD's first event (T0-10d): not
	// more, so no release.
	s.deploy(team, "edge-prod", "prod", sha("c"), "Succeeded", days(4), "reg/edge:1")
	d := load(t, st, t0, days(28))
	got := map[string]bool{}
	for _, dep := range d.Deploys {
		if str(dep.Kind) == "first_seen" {
			got[dep.App] = dep.FirstRelease
		}
	}
	// kasse-intg went live 12 days after Argo CD's first event, portal-prod
	// 16 and atlas-prod 17 days after.
	want := map[string]bool{"kasse-intg": false, "edge-prod": false, "portal-prod": true, "atlas-prod": true}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("first releases = %v, want %v", got, want)
	}
	for _, dep := range d.Deploys {
		if dep.FirstRelease && str(dep.Kind) != "first_seen" {
			t.Errorf("%s: first release of kind %s", dep.App, str(dep.Kind))
		}
	}
}

// A sync Argo CD reports as Error deployed nothing, as one reported Failed:
// a finished sync without a kind. One still running is no finished sync.
func TestDatasetDeployError(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	s := &seeder{t: t, pool: pool, n: 1000}
	s.deploy(team, "kasse-prod", "prod", sha("e"), "Error", days(5).Add(time.Hour), "reg/kasse-api:1.2")
	d := load(t, st, t0, days(28))
	var phases []string
	for _, dep := range d.Deploys {
		if dep.App == "kasse-prod" && dep.Kind == nil {
			phases = append(phases, dep.Phase)
		}
	}
	if !slices.Equal(phases, []string{"Failed", "Error"}) {
		t.Errorf("failed kasse-prod syncs = %v", phases)
	}
}

// A GitOps commit Bitbucket saw only as a PR's merge commit (merged in the
// UI, no push event) still names the deploy's product, and makes its repo
// a GitOps repo. An App none of whose deploys over 26 weeks has a product
// is infrastructure, once Bitbucket reported before one of them.
func TestDatasetProductFromMergeCommit(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	s := &seeder{t: t, pool: pool, n: 1000}
	const deployRepo = "kasse/kasse-deploy"
	rev := strings.Repeat("ab", 20)
	// The payload keeps the sender's casing, the revision is lowercased.
	s.pr(prEvent{event: "pr:opened", repo: deployRepo, id: 40, author: "dev-anna", branch: "kasse_PROD_1.3", at: days(9)})
	s.pr(prEvent{event: "pr:merged", repo: deployRepo, id: 40, author: "dev-anna", branch: "kasse_PROD_1.3",
		mergeCommit: strings.ToUpper(rev), at: days(9).Add(time.Minute)})
	s.deploy(team, "kasse-api-prod", "prod", rev, "Succeeded", days(9).Add(time.Hour), "reg/kasse-api:1.3")
	d := load(t, st, t0, days(28))
	got := map[string]string{}
	for _, dep := range d.Deploys {
		got[dep.App] = str(dep.Project) + fmt.Sprintf("/infra=%v", dep.Infra)
	}
	want := map[string]string{
		"kasse-api-prod": "kasse/infra=false",
		"kasse-prod":     "kasse/infra=false",
		"kasse-intg":     "kasse/infra=false",
		"portal-prod":    "portal/infra=false",
		// sha 8 is a commit Bitbucket never saw, its only deploy.
		"atlas-prod": "<nil>/infra=true",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("deploys = %v, want %v", got, want)
	}
	if !slices.Contains(d.GitOpsRepos, deployRepo) {
		t.Errorf("gitops repos = %v", d.GitOpsRepos)
	}
	for _, p := range d.PRs {
		if p.Repo == deployRepo && !p.GitOps {
			t.Errorf("promotion PR not gitops: %+v", p)
		}
	}
	if !slices.ContainsFunc(d.Products, func(p dataset.Product) bool {
		return p.Key == "kasse" && slices.Contains(p.Apps, "kasse-api-prod")
	}) {
		t.Errorf("products = %+v", d.Products)
	}
}

// A team whose Bitbucket never reported: its Apps without a product are
// unknown, not infrastructure, else its every deploy would drop out.
func TestDatasetNoInfraWithoutBitbucket(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.deploy("argo-only", "mon-prod", "prod", sha("1"), "Succeeded", days(1), "reg/mon:1")
	s.deploy("argo-only", "mon-prod", "prod", sha("2"), "Succeeded", days(2), "reg/mon:2")
	d, err := st.Dataset(context.Background(), dataset.Params{Team: "argo-only", From: t0, To: days(28), Now: days(28)})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Deploys) != 2 || d.Deploys[0].Infra || d.Deploys[1].Infra {
		t.Errorf("deploys = %+v", d.Deploys)
	}
}

// Merged work, as Bitbucket marks it: a PR into a feature or umbrella
// branch is off the trunk, PRs marked merged in one second without a merge
// commit were merged by the one with it, which is their umbrella, as is a
// PR from a branch people merged PRs into; the same unapproved change
// opened across three repos within an hour is housekeeping.
func TestDatasetMergedWork(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const web = "kasse/kasse-web"
	merged := func(repo string, id int64, target, title, commit string, opened, at time.Time) {
		s.pr(prEvent{event: "pr:opened", repo: repo, id: id, author: "dev-anna", branch: fmt.Sprintf("feature/x-%d", id),
			target: target, title: title, at: opened})
		s.pr(prEvent{event: "pr:merged", repo: repo, id: id, author: "dev-anna", branch: fmt.Sprintf("feature/x-%d", id),
			target: target, title: title, mergeCommit: commit, at: at})
	}
	c := func(i int) string { return fmt.Sprintf("%040x", i) }
	// kasse-web's trunk is develop: most of its PRs, over all history, go
	// there. One into develop in the window is no off-trunk PR, one into
	// master never is, one into an umbrella branch is.
	merged(web, 1, "develop", "a", c(1), days(-40), days(-39))
	merged(web, 2, "develop", "b", c(2), days(-30), days(-29))
	merged(web, 3, "develop", "c", c(3), days(1), days(1.5))
	merged(web, 4, "master", "d", c(4), days(1), days(1.6))
	merged(web, 5, "feature/umbrella", "e", c(5), days(1), days(1.7))
	// The umbrella itself, from the branch #5 went into.
	s.pr(prEvent{event: "pr:opened", repo: web, id: 6, author: "dev-anna", branch: "feature/umbrella", target: "develop",
		title: "umbrella", at: days(2)})
	s.pr(prEvent{event: "pr:merged", repo: web, id: 6, author: "dev-anna", branch: "feature/umbrella", target: "develop",
		title: "umbrella", mergeCommit: c(6), at: days(2.5)})
	// A bot's PR into a person's branch makes no umbrella of it.
	s.pr(prEvent{event: "pr:merged", repo: web, id: 7, author: "renovate-bot", automation: "renovate", branch: "renovate/x",
		target: "feature/deps", mergeCommit: c(7), at: days(2.6)})
	s.pr(prEvent{event: "pr:opened", repo: web, id: 8, author: "dev-anna", branch: "feature/deps", target: "develop",
		title: "deps", at: days(2.7)})
	s.pr(prEvent{event: "pr:merged", repo: web, id: 8, author: "dev-anna", branch: "feature/deps", target: "develop",
		title: "deps", mergeCommit: c(8), at: days(2.8)})
	// kasse-api's trunk is master; a PR the payload names no target of
	// counts as the trunk's.
	merged(kasseAPI, 10, "master", "f", c(10), days(1), days(1.8))
	merged(kasseAPI, 11, "feature/big", "g", c(11), days(1), days(1.9))
	merged(kasseAPI, 12, "", "h", c(12), days(1), days(1.95))
	// An umbrella merge: #20 with its merge commit, #21 and #22 in the same
	// second without one.
	at := days(3)
	merged(kasseAPI, 20, "master", "umbrella", c(20), days(2), at)
	merged(kasseAPI, 21, "master", "part 1", "", days(2), at.Add(300*time.Millisecond))
	merged(kasseAPI, 22, "master", "part 2", "", days(2), at.Add(600*time.Millisecond))
	// Two merges in one second, each with its merge commit: two merges.
	merged(kasseAPI, 30, "master", "i", c(30), days(2), days(4))
	merged(kasseAPI, 31, "master", "j", c(31), days(2), days(4).Add(500*time.Millisecond))
	// None with a merge commit: no telling which merged the other, no
	// umbrella.
	merged(kasseAPI, 40, "master", "k", "", days(2), days(4.5))
	merged(kasseAPI, 41, "master", "l", "", days(2), days(4.5))
	// Housekeeping: the same title, unapproved, opened in three repos within
	// the hour.
	hk := days(5)
	merged(kasseAPI, 50, "master", "Sync code owners file", c(50), hk, hk.Add(5*time.Minute))
	merged(web, 51, "develop", "sync code  owners file ", c(51), hk.Add(10*time.Minute), hk.Add(15*time.Minute))
	merged(portalWeb, 52, "master", "Sync code owners file", c(52), hk.Add(40*time.Minute), hk.Add(45*time.Minute))
	// The same title, approved: counts for the three repos, is no
	// housekeeping itself.
	merged(kasseAPI, 53, "master", "Sync code owners file", c(53), hk.Add(20*time.Minute), hk.Add(25*time.Minute))
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 53, author: "rita", at: hk.Add(22 * time.Minute)})
	// In two repos only.
	merged(kasseAPI, 54, "master", "Bump CI template", c(54), days(6), days(6).Add(time.Minute))
	merged(web, 55, "develop", "Bump CI template", c(55), days(6), days(6).Add(2*time.Minute))
	// In three repos, but over four hours.
	merged(kasseAPI, 56, "master", "Fix typo", c(56), days(7), days(7).Add(time.Minute))
	merged(web, 57, "develop", "Fix typo", c(57), days(7).Add(2*time.Hour), days(7).Add(2*time.Hour+time.Minute))
	merged(portalWeb, 58, "master", "Fix typo", c(58), days(7).Add(4*time.Hour), days(7).Add(4*time.Hour+time.Minute))
	// Quick and unapproved, its title its own: an ordinary PR.
	merged(kasseAPI, 60, "master", "Small fix", c(60), days(8), days(8).Add(3*time.Minute))

	d := load(t, st, t0, days(28))
	flags := map[string]string{}
	for _, p := range d.PRs {
		var f []string
		if p.OffTrunk {
			f = append(f, "off_trunk")
		}
		if p.ImplicitMerge {
			f = append(f, "implicit")
		}
		if p.Umbrella {
			f = append(f, "umbrella")
		}
		if p.Housekeeping {
			f = append(f, "housekeeping")
		}
		if len(f) > 0 {
			flags[fmt.Sprintf("%s#%d", p.Repo, p.PRID)] = strings.Join(f, ",")
		}
	}
	want := map[string]string{
		web + "#5":        "off_trunk",
		web + "#6":        "umbrella",
		web + "#7":        "off_trunk",
		kasseAPI + "#11":  "off_trunk",
		kasseAPI + "#20":  "umbrella",
		kasseAPI + "#21":  "implicit",
		kasseAPI + "#22":  "implicit",
		kasseAPI + "#40":  "implicit",
		kasseAPI + "#41":  "implicit",
		kasseAPI + "#50":  "housekeeping",
		web + "#51":       "housekeeping",
		portalWeb + "#52": "housekeeping",
	}
	if fmt.Sprint(flags) != fmt.Sprint(want) {
		t.Errorf("flags =\n%v\nwant\n%v", flags, want)
	}
	for _, p := range d.PRs {
		if p.Repo == web && p.PRID == 5 && str(p.TargetBranch) != "feature/umbrella" {
			t.Errorf("target = %s", str(p.TargetBranch))
		}
		if p.Repo == kasseAPI && p.PRID == 12 && p.TargetBranch != nil {
			t.Errorf("target without toRef = %s", str(p.TargetBranch))
		}
	}
}

// A run whose commit Bitbucket never saw takes the repository its pipeline
// name starts with, when exactly one of the team's repositories has that
// name; else it stays without.
func TestDatasetRunRepoFromPipelineName(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	s := &seeder{t: t, pool: pool, n: 1000}
	// Two projects with a repository of one name.
	s.push("kasse/shared-lib", "master", days(1), commit{sha: strings.Repeat("d1", 20), authored: days(1)})
	s.push("portal/shared-lib", "master", days(1), commit{sha: strings.Repeat("d2", 20), authored: days(1)})
	unseen := func(i int) string { return fmt.Sprintf("%040x", 0xfee000+i) }
	for i, p := range []string{
		"kasse-api/feature%2Fkas-99", // the team's kasse-api
		"Kasse-API/feature%2Fkas-98", // whatever the case
		"kasse-gitops/feature%2Fx",   // a GitOps repo: its run is one
		"shared-lib/master",          // two projects: no telling
		"unknown-repo/master",        // no such repository
		"ledger-core/master",         // the other team's
		"folder/kasse-api/master",    // the first segment is the folder
		"kasse-api/scan/master",      // a folder named like a repo: no repo
	} {
		s.run(run{pipeline: p, runID: "1", status: "FAILURE", commit: unseen(i), started: days(2)})
	}
	d := load(t, st, t0, days(28))
	got := map[string]string{}
	for _, r := range d.Runs {
		got[r.PipelineName] = str(r.Repo) + "|" + str(r.Project) + fmt.Sprintf("|%v", r.GitOps)
	}
	want := map[string]string{
		"kasse-api/feature%2Fkas-99": kasseAPI + "|kasse|false",
		"Kasse-API/feature%2Fkas-98": kasseAPI + "|kasse|false",
		"kasse-gitops/feature%2Fx":   kasseOps + "|kasse|true",
		"shared-lib/master":          "<nil>|<nil>|false",
		"unknown-repo/master":        "<nil>|<nil>|false",
		"ledger-core/master":         "<nil>|<nil>|false",
		"folder/kasse-api/master":    "<nil>|<nil>|false",
		"kasse-api/scan/master":      "<nil>|<nil>|false",
		"nightly":                    "<nil>|<nil>|false",
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s = %s, want %s", p, got[p], w)
		}
	}
}

// An account a dependency bot runs as, named after its tool and team but of
// no bot shape and without the bot's branch prefix, is a bot on its PR; a
// person whose name holds the tool's letters in a longer word is none.
func TestDatasetBotNamedAfterItsTool(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 70, author: "renovate_kasse", display: "Renovate Kasse",
		branch: "chore/deps", at: days(1)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 70, author: "renovate_kasse", display: "Renovate Kasse",
		branch: "chore/deps", at: days(1).Add(time.Hour)})
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 71, author: "inga.renovatebaum", display: "Inga Renovatebaum",
		branch: "feature/kas-71", at: days(1)})
	d := load(t, st, t0, days(28))
	bot := map[int64]bool{}
	for _, p := range d.PRs {
		bot[p.PRID] = p.Bot
	}
	if !bot[70] || bot[71] || len(bot) != 2 {
		t.Errorf("bots = %v", bot)
	}
}

// The same account's direct pushes to the trunk are bot commits in the lead
// time; the person's in the same release are not.
func TestDatasetBotPushNamedAfterItsTool(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	base, botCommit, head, ops := strings.Repeat("e1", 20), strings.Repeat("e4", 20), strings.Repeat("e2", 20), strings.Repeat("e3", 20)
	s.push(portalWeb, "master", days(5), commit{sha: base, authored: days(5)})
	s.push(portalWeb, "master", days(6),
		commit{sha: botCommit, author: "renovate_portal", authored: days(6).Add(-time.Hour), message: "update lib"},
		commit{sha: head, author: "inga.renovatebaum", authored: days(6), message: "fix layout"})
	s.push(portalOps, "master", days(6).Add(time.Hour), commit{sha: ops, authored: days(6).Add(time.Hour),
		message: "portal PROD 2.1\n\n[portal-web 2.0 -> 2.1](https://git.example.com/projects/portal/repos/portal-web/compare/diff?targetBranch=" +
			base + "&sourceBranch=" + head + ")"})
	s.deploy(team, "portal-prod", "prod", ops, "Succeeded", days(6).Add(2*time.Hour), "reg/portal-web:2.1")
	d := load(t, st, t0, days(28))
	bot := map[string]bool{}
	for _, l := range d.LeadTimes {
		bot[l.Author] = l.Bot
	}
	if len(bot) != 2 || !bot["renovate_portal"] || bot["inga.renovatebaum"] {
		t.Errorf("lead time bots = %v", bot)
	}
}

// at is the instant T0 plus d days.
func at(d float64) time.Time { return days(d) }

// Silences: a source storing nothing for two working days while another
// stores events. A weekend is none, nor a span every source is quiet in.
// An open PR last heard of before a Bitbucket silence with unseen runs,
// nothing since, has an unknown state; a quiet team's idle PR is open.
func TestDatasetSilences(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	// T0 is a Monday. Bitbucket: quiet Tuesday 09:36 to Friday 09:36 (a
	// silence), over the weekend after (none), and with every other source
	// from Sunday night to Friday of the third week (none).
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 1, author: "dev-anna", branch: "feature/a", state: "OPEN", at: at(0.5)})
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 2, author: "dev-anna", branch: "feature/b", state: "OPEN", at: at(1.4)})
	s.pr(prEvent{event: "pr:comment:added", repo: kasseAPI, id: 2, author: "rita", state: "OPEN", at: at(4.4)})
	for i, d := range []float64{7.4, 8, 9, 10, 11, 13.5, 13.9, 18.2, 19} {
		s.push(kasseAPI, "master", at(d), commit{sha: fmt.Sprintf("%040x", 0xb000+i), authored: at(d)})
	}
	// Pipeline: in Bitbucket's silence a build of a commit it never saw, of
	// a pipeline whose other commits it did see.
	for i, d := range []float64{0.6, 2, 3, 4.5, 7.5, 8.5, 9.5, 10.5, 13.9, 18.2} {
		commitSHA := sha("1") // on every kasse-api PR event
		if d == 2 {
			commitSHA = strings.Repeat("5e", 20)
		}
		s.run(run{pipeline: "kasse-api/feature%2Fa", runID: fmt.Sprint(i), status: "SUCCESS", commit: commitSHA, started: at(d).Add(-10 * time.Minute)})
	}
	for _, d := range []float64{0.7, 1.7, 2.5, 3.5, 4.6, 5.5, 7.6, 8.7, 9.7, 10.7, 13.9, 18.2} {
		s.deploy(team, "kasse-prod", "prod", sha("c"), "Succeeded", at(d), "reg/kasse-api:1.1")
	}
	// noergler: nothing from Monday night to Thursday of the next week while
	// the others report, a silence of its own.
	for _, d := range []float64{0.8, 10.8, 13.9, 18.2} {
		s.rollup(team, kasseAPI+"#1", "merged", at(d))
	}
	// Another team on holiday: Bitbucket quiet for days while only a nightly
	// job of commits Bitbucket never saw keeps running.
	const quiet = "quiet"
	s.pr(prEvent{teamName: quiet, event: "pr:opened", repo: "quiet/quiet-app", id: 3, author: "dev-otto", branch: "feature/q",
		state: "OPEN", at: at(0.5)})
	s.pr(prEvent{teamName: quiet, event: "pr:opened", repo: "quiet/quiet-app", id: 4, author: "dev-otto", branch: "feature/r",
		state: "OPEN", at: at(4.4)})
	for i, d := range []float64{1, 2, 3, 4} {
		s.run(run{teamName: quiet, pipeline: "nightly", runID: fmt.Sprint(i), status: "SUCCESS",
			commit: strings.Repeat("7e", 20), started: at(d)})
	}

	d := load(t, st, t0, days(21))
	// Days after T0, to the minute: Postgres keeps microseconds.
	day := func(x time.Time) string { return fmt.Sprintf("%.3f", x.Sub(t0).Round(time.Minute).Hours()/24) }
	var got []string
	for _, x := range d.Meta.Silences {
		if x.From.Location() != time.UTC || x.To.Location() != time.UTC {
			t.Errorf("silence not in UTC: %v", x)
		}
		got = append(got, fmt.Sprintf("%s %s-%s %d %v", x.Source, day(x.From), day(x.To), x.UnseenRuns, x.Ongoing))
	}
	// Bitbucket's last push on day 19 is after the others' last events: they
	// are silent since, up to the read's now (day 28).
	want := []string{"noergler 0.800-10.800 0 false", "bitbucket 1.400-4.400 1 false", "argocd 18.200-28.000 0 true",
		"noergler 18.200-28.000 0 true", "pipeline 18.200-28.000 0 true"}
	if !slices.Equal(got, want) {
		t.Errorf("silences = %v, want %v", got, want)
	}
	if len(d.Meta.Lost) != 1 || d.Meta.Lost[0].Source != dataset.SourceBitbucket {
		t.Errorf("lost = %+v", d.Meta.Lost)
	}
	unknown := map[int64]bool{}
	for _, o := range d.OpenPRs {
		unknown[o.PRID] = o.Unknown
	}
	if len(unknown) != 2 || !unknown[1] || unknown[2] {
		t.Errorf("unknown = %v", unknown)
	}

	// A window starting after the silence still finds it for its open PRs
	// (SilenceLookback), but names no silence of its own but the ones still
	// running.
	later := load(t, st, days(14), days(21))
	if slices.ContainsFunc(later.Meta.Silences, func(s dataset.Silence) bool { return !s.Ongoing }) {
		t.Errorf("later silences = %+v", later.Meta.Silences)
	}
	if len(later.OpenPRs) != 2 || !slices.ContainsFunc(later.OpenPRs, func(o dataset.OpenPR) bool { return o.PRID == 1 && o.Unknown }) {
		t.Errorf("later open = %+v", later.OpenPRs)
	}

	q, err := st.Dataset(context.Background(), dataset.Params{Team: quiet, From: t0, To: days(7), Now: days(28)})
	if err != nil {
		t.Fatal(err)
	}
	// Bitbucket quiet with no lost push, then the nightly job stopping while
	// Bitbucket went on: two silences, neither a hole.
	if len(q.Meta.Silences) != 2 || q.Meta.Silences[0].Source != dataset.SourceBitbucket || q.Meta.Silences[0].UnseenRuns != 0 ||
		!q.Meta.Silences[1].Ongoing || len(q.Meta.Lost) != 0 {
		t.Errorf("quiet silences = %+v", q.Meta.Silences)
	}
	for _, o := range q.OpenPRs {
		if o.Unknown {
			t.Errorf("quiet team's PR #%d unknown", o.PRID)
		}
	}
	if len(q.OpenPRs) != 2 {
		t.Errorf("quiet open = %+v", q.OpenPRs)
	}
}

// Housekeeping is a tool's change: the same title opened in
// HousekeepingMinRepos of the team's repos within HousekeepingWindow, no
// person's approval, no ticket key in title or source branch. How long it
// stood open before its merge says nothing: one merged days later is one
// too. A real change across repos carries its ticket.
func TestDatasetHousekeepingRule(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const web, ops = "kasse/kasse-web", "kasse/kasse-contract"
	merged := func(repo string, id int64, branch, title string, opened, at time.Time) {
		s.pr(prEvent{event: "pr:opened", repo: repo, id: id, author: "dev-anna", branch: branch, target: "master", title: title, at: opened})
		s.pr(prEvent{event: "pr:merged", repo: repo, id: id, author: "dev-anna", branch: branch, target: "master", title: title,
			mergeCommit: fmt.Sprintf("%040x", id), at: at})
	}
	// Opened in three repos within the hour; merged at once, an hour later
	// and three days later. The review bot's approval is no person's.
	hk := days(2)
	merged(kasseAPI, 50, "chore/settings", "Rotate lint config", hk, hk.Add(10*time.Minute))
	merged(web, 51, "chore/settings", "Rotate lint config", hk.Add(5*time.Minute), hk.Add(time.Hour))
	merged(ops, 52, "chore/settings", "Rotate lint config", hk.Add(30*time.Minute), hk.Add(72*time.Hour))
	s.pr(prEvent{event: "pr:reviewer:approved", repo: ops, id: 52, author: "noergler", automation: "noergler", at: hk.Add(time.Hour)})
	// One bugfix across three repos, its ticket in the title.
	fix := days(3)
	for i, repo := range []string{kasseAPI, web, ops} {
		merged(repo, int64(60+i), "bugfix/rounding", "KAS-99 Fix rounding of totals", fix.Add(time.Duration(i)*5*time.Minute),
			fix.Add(time.Duration(i)*5*time.Minute+10*time.Minute))
	}
	// Its ticket only in the branch.
	key := days(4)
	for i, repo := range []string{kasseAPI, web, ops} {
		merged(repo, int64(70+i), "bugfix/KAS-98-rounding", "Fix rounding of discounts", key.Add(time.Duration(i)*5*time.Minute),
			key.Add(time.Duration(i)*5*time.Minute+10*time.Minute))
	}
	// Approved by a person: no housekeeping itself, its siblings still are.
	same := days(5)
	merged(kasseAPI, 80, "chore/ci", "Bump CI template", same, same.Add(time.Minute))
	merged(web, 81, "chore/ci", "Bump CI template", same.Add(time.Minute), same.Add(2*time.Minute))
	merged(ops, 82, "chore/ci", "Bump CI template", same.Add(2*time.Minute), same.Add(3*time.Minute))
	s.pr(prEvent{event: "pr:reviewer:approved", repo: ops, id: 82, author: "rita", at: same.Add(150 * time.Second)})
	// Opened over four hours: three changes.
	apart := days(6)
	merged(kasseAPI, 90, "chore/typo", "Fix typo", apart, apart.Add(time.Minute))
	merged(web, 91, "chore/typo", "Fix typo", apart.Add(2*time.Hour), apart.Add(2*time.Hour+time.Minute))
	merged(ops, 92, "chore/typo", "Fix typo", apart.Add(4*time.Hour), apart.Add(4*time.Hour+time.Minute))

	d := load(t, st, t0, days(28))
	var got []string
	for _, p := range d.PRs {
		if p.Housekeeping {
			got = append(got, fmt.Sprintf("%s#%d", p.Repo, p.PRID))
		}
	}
	slices.Sort(got)
	want := []string{ops + "#52", kasseAPI + "#50", kasseAPI + "#80", web + "#51", web + "#81"}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("housekeeping = %v, want %v", got, want)
	}
}

// A repo's trunk besides master and main is the branch most of its PRs go
// into; two tied are both trunk, never one of them by its name.
func TestDatasetTrunkTie(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const repo = "kasse/kasse-lib"
	n := int64(30)
	add := func(target string, at time.Time) {
		n++
		branch := fmt.Sprintf("feature/%d", n)
		s.pr(prEvent{event: "pr:opened", repo: repo, id: n, author: "dev-anna", branch: branch, target: target, at: at.Add(-2 * time.Hour)})
		s.pr(prEvent{event: "pr:merged", repo: repo, id: n, author: "dev-anna", branch: branch, target: target,
			mergeCommit: fmt.Sprintf("%040x", n), at: at})
	}
	add("develop", days(1))
	add("develop", days(2))
	add("release/2.x", days(3))
	add("release/2.x", days(4))
	add("feature/umbau", days(5))
	d := load(t, st, t0, days(7))
	off := map[int64]bool{}
	for _, p := range d.PRs {
		off[p.PRID] = p.OffTrunk
	}
	if fmt.Sprint(off) != "map[31:false 32:false 33:false 34:false 35:true]" {
		t.Errorf("off trunk = %v", off)
	}
}

// A person's PR Bitbucket marked merged in the second a bot's PR merged
// with a merge commit is merged work: a bot's PR never makes a person's
// uncounted.
func TestDatasetImplicitMergeBesideABot(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const repo = "kasse/kasse-web"
	at := days(3)
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 20, author: "dev-anna", branch: "feature/a", target: "master", at: at.Add(-time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: repo, id: 20, author: "dev-anna", branch: "feature/a", target: "master", at: at})
	s.pr(prEvent{event: "pr:opened", repo: repo, id: 21, author: "renovate-bot", automation: "renovate", branch: "renovate/x",
		target: "master", at: at.Add(-time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: repo, id: 21, author: "renovate-bot", automation: "renovate", branch: "renovate/x",
		target: "master", mergeCommit: sha("d"), at: at.Add(300 * time.Millisecond)})
	d := load(t, st, t0, days(7))
	p := metrics.ClosedPeriod(1, days(7), time.UTC)
	m := metrics.Compute(d, &dataset.Dataset{Meta: d.Meta}, nil, metrics.Params{Period: p})
	if m.Throughput.Merged != 1 {
		t.Errorf("merged = %d, want the person's PR", m.Throughput.Merged)
	}
}

// Infrastructure is an App none of whose deploys over 26 weeks names a
// product, but never one going live while Bitbucket's events were lost: its
// GitOps commit fell into the hole, and its first release is a release.
func TestDatasetNewAppInAnOutageIsNoInfra(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	busy(s, kasseAPI, "kasse-api/master", -20, 8)
	s.push(kasseOps, "master", days(-20), commit{sha: sha("b"), authored: days(-20)})
	s.deploy(team, "kasse-prod", "prod", sha("b"), "Succeeded", days(-20), "reg/kasse-api:1.0")
	// A log shipper: deployed from a repo no Bitbucket event names, since
	// the team's onboarding.
	for i, d := range []float64{-19, 2, 5} {
		s.deploy(team, "logs-prod", "prod", fmt.Sprintf("%040x", 0x10+i), "Succeeded", days(d), fmt.Sprintf("reg/logs:%d", i))
	}
	// Bitbucket quiet from Tuesday 09:36 to Friday 09:36 while a push it
	// lost is built; a new App goes live on Wednesday.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 5, author: "dev-anna", branch: "feature/a", at: days(8.4)})
	s.run(run{pipeline: "kasse-api/master", runID: "lost", status: "SUCCESS", commit: strings.Repeat("5e", 20), started: days(9.5)})
	s.deploy(team, "neu-prod", "prod", strings.Repeat("6e", 20), "Succeeded", days(9), "reg/neu:1")
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 6, author: "dev-anna", branch: "feature/b", at: days(11.4)})
	d := load(t, st, t0, days(28))
	infra := map[string]bool{}
	for _, dep := range d.Deploys {
		infra[dep.App] = dep.Infra
	}
	if fmt.Sprint(infra) != "map[logs-prod:true neu-prod:false]" {
		t.Errorf("infra = %v", infra)
	}
}

// One App, classed once per page by the newest read: its deploys in the
// previous period, whose GitOps commits Bitbucket never saw, are its
// product's as its deploy in this period names it, never infrastructure in
// one period and the product's in the other.
func TestDatasetAppClassedOncePerPage(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.push(kasseAPI, "master", days(-40), commit{sha: sha("a"), authored: days(-40)})
	s.deploy(team, "neu-prod", "prod", sha("1"), "Succeeded", days(-60), "reg/neu:0")
	s.deploy(team, "neu-prod", "prod", sha("2"), "Succeeded", days(-20), "reg/neu:1")
	s.deploy(team, "neu-prod", "prod", sha("3"), "Succeeded", days(-10), "reg/neu:2")
	s.push(kasseOps, "master", days(5), commit{sha: sha("4"), authored: days(5)})
	s.deploy(team, "neu-prod", "prod", sha("4"), "Succeeded", days(5).Add(time.Hour), "reg/neu:3")
	cur, prev := load(t, st, t0, days(28)), load(t, st, days(-28), t0)
	for _, dep := range prev.Deploys {
		if dep.Project != nil || dep.Infra {
			t.Fatalf("the previous read on its own: %+v", dep)
		}
	}
	for _, dep := range prev.Classified(cur.Apps).Deploys {
		if str(dep.Project) != "kasse" || dep.Infra {
			t.Errorf("classed by the newest read: %s project %s infra %v", dep.Revision[:2], str(dep.Project), dep.Infra)
		}
	}
	p, pp := metrics.ClosedPeriod(4, days(28), time.UTC), metrics.ClosedPrevious(4, days(28), time.UTC)
	m := metrics.Compute(cur, prev, nil, metrics.Params{Period: p, Previous: pp, Product: "kasse"})
	if m.Delivery.Frequency.Releases != 1 || m.Comparison.DeployFreq.Prev == nil || *m.Comparison.DeployFreq.Prev != 0.5 {
		t.Errorf("kasse releases %d, previous %+v", m.Delivery.Frequency.Releases, m.Comparison.DeployFreq)
	}
}

// A team whose GitOps repo sends Bitbucket nothing has no deploy with a
// product: none of its Apps is infrastructure, else its every deploy would
// drop out of the team view.
func TestDatasetNoInfraWithoutAnyProduct(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	busy(s, kasseAPI, "kasse-api/master", -2, 6)
	s.deploy(team, "kasse-prod", "prod", sha("1"), "Succeeded", days(1), "reg/kasse:1")
	s.deploy(team, "kasse-prod", "prod", sha("2"), "Succeeded", days(2), "reg/kasse:2")
	d := load(t, st, t0, days(7))
	if len(d.Deploys) != 2 || d.Deploys[0].Infra || d.Deploys[1].Infra {
		t.Errorf("deploys = %+v", d.Deploys)
	}
}

// A library is never rolled out: its merged PRs are flagged, on positive
// evidence, builds or none. A repo is none when a compare link traced it,
// a run of it built an image (deployed or not yet: in doubt), or an image
// Argo CD deployed is named after it (its CI need not report; a registry's
// port is no tag, a longer name sharing its start is another repo's). Nor
// is any repo of a team whose tracing or image reports show nothing.
func TestDatasetLibraries(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	s := &seeder{t: t, pool: pool, n: 1000}
	merged := func(repo string, id int64) {
		s.pr(prEvent{event: "pr:opened", repo: repo, id: id, author: "dev-anna", branch: fmt.Sprintf("feature/l-%d", id), at: days(3)})
		s.pr(prEvent{event: "pr:merged", repo: repo, id: id, author: "dev-anna", branch: fmt.Sprintf("feature/l-%d", id),
			mergeCommit: fmt.Sprintf("%040x", 0xabc000+id), at: days(3.5)})
	}
	const lib, web, docs, fresh = "kasse/kasse-lib", "kasse/kasse-web", "kasse/kasse-docs", "kasse/kasse-new"
	libCommit, webCommit, freshCommit := strings.Repeat("b1", 20), strings.Repeat("b2", 20), strings.Repeat("b4", 20)
	// Builds an image nobody deployed yet: deployable, not traced yet. In
	// doubt, no library.
	s.push(fresh, "master", days(2), commit{sha: freshCommit, authored: days(2)})
	s.run(run{pipeline: "kasse-new/master", runID: "1", status: "SUCCESS", commit: freshCommit, started: days(2),
		images: []string{"reg/kasse-new:0.1"}})
	merged(fresh, 4)
	s.push(lib, "master", days(2), commit{sha: libCommit, authored: days(2)})
	s.run(run{pipeline: "kasse-lib/master", runID: "1", status: "SUCCESS", commit: libCommit, started: days(2)})
	// Deployed by its image, its GitOps commits naming no compare link.
	s.push(web, "master", days(2), commit{sha: webCommit, authored: days(2)})
	s.run(run{pipeline: "kasse-web/master", runID: "1", status: "SUCCESS", commit: webCommit, started: days(2),
		images: []string{"reg/kasse-web:3"}})
	s.deploy(team, "kasse-web-prod", "prod", strings.Repeat("b3", 20), "Succeeded", days(2.5), "reg/kasse-web:3")
	// No CI report, never traced, but its image deployed by name; a client
	// whose name starts like it is no such image.
	const batch, client, port = "kasse/kasse-batch", "kasse/kasse-batch-client", "kasse/kasse-port"
	s.deploy(team, "kasse-batch-prod", "prod", strings.Repeat("b5", 20), "Succeeded", days(2.6), "reg/team/kasse-batch:2.0")
	s.deploy(team, "kasse-port-prod", "prod", strings.Repeat("b6", 20), "Succeeded", days(2.7), "reg:5000/kasse-port@sha256:00ab")
	merged(lib, 1)
	merged(web, 2)
	merged(docs, 3)
	merged(batch, 5)
	merged(client, 6)
	merged(port, 7)
	d := load(t, st, t0, days(28))
	got := map[string]bool{}
	for _, p := range d.PRs {
		if p.State == "merged" {
			got[p.Repo] = got[p.Repo] || p.Library
		}
	}
	want := map[string]bool{lib: true, web: false, docs: true, fresh: false, kasseAPI: false, kasseOps: false,
		batch: false, client: true, port: false}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("libraries = %v, want %v", got, want)
	}
	// The facts' read flags them too: its column to production must not
	// say "no" for a library's PR.
	f, err := st.Dataset(context.Background(), dataset.Params{Team: team, From: t0, To: days(28), Now: days(28), FactsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.PRs, func(p dataset.PR) bool { return p.Library && p.Repo == lib }) {
		t.Error("facts-only read flagged no library")
	}
}

// Without a traced repo the team's tracing shows nothing: no repo is a
// library, whatever it builds.
func TestDatasetNoLibraryWithoutTracing(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const lib = "kasse/kasse-lib"
	s.push(lib, "master", days(2), commit{sha: sha("1"), authored: days(2)})
	s.run(run{pipeline: "kasse-lib/master", runID: "1", status: "SUCCESS", commit: sha("1"), started: days(2)})
	s.pr(prEvent{event: "pr:opened", repo: lib, id: 1, author: "dev-anna", branch: "feature/x", at: days(3)})
	s.pr(prEvent{event: "pr:merged", repo: lib, id: 1, author: "dev-anna", branch: "feature/x", mergeCommit: sha("2"), at: days(3.5)})
	d := load(t, st, t0, days(28))
	if len(d.PRs) != 1 || d.PRs[0].Library {
		t.Errorf("prs = %+v", d.PRs)
	}
}

// noergler's first event per repository of the window's PRs: none for a
// repo it never reviewed.
func TestDatasetRepoStarts(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	d := load(t, st, t0, days(28))
	var got []string
	for _, r := range d.Meta.RepoStarts {
		v := "<nil>"
		if r.Noergler != nil {
			v = r.Noergler.Sub(t0).String()
		}
		got = append(got, r.Repo+"="+v)
	}
	// kasse-api's first rollup is PR #1's, before the window.
	want := []string{kasseAPI + "=-456h0m0s", kasseOps + "=<nil>", portalWeb + "=<nil>"}
	if !slices.Equal(got, want) {
		t.Errorf("repo starts = %v, want %v", got, want)
	}
}
