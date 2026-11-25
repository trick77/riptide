package dataset_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
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
// branch is off the trunk, PRs marked merged in one second with at most
// one merge commit are one merge, and the same quick unapproved change
// across three repos within an hour is housekeeping.
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
	// None with a merge commit: the highest PR number stays.
	merged(kasseAPI, 40, "master", "k", "", days(2), days(4.5))
	merged(kasseAPI, 41, "master", "l", "", days(2), days(4.5))
	// Housekeeping: the same title, quick and unapproved, in three repos
	// within the hour.
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
		if p.Housekeeping {
			f = append(f, "housekeeping")
		}
		if len(f) > 0 {
			flags[fmt.Sprintf("%s#%d", p.Repo, p.PRID)] = strings.Join(f, ",")
		}
	}
	want := map[string]string{
		web + "#5":        "off_trunk",
		kasseAPI + "#11":  "off_trunk",
		kasseAPI + "#21":  "implicit",
		kasseAPI + "#22":  "implicit",
		kasseAPI + "#40":  "implicit",
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
		got = append(got, fmt.Sprintf("%s %s-%s %d", x.Source, day(x.From), day(x.To), x.UnseenRuns))
	}
	want := []string{"noergler 0.800-10.800 0", "bitbucket 1.400-4.400 1"}
	if !slices.Equal(got, want) {
		t.Errorf("silences = %v, want %v", got, want)
	}
	unknown := map[int64]bool{}
	for _, o := range d.OpenPRs {
		unknown[o.PRID] = o.Unknown
	}
	if len(unknown) != 2 || !unknown[1] || unknown[2] {
		t.Errorf("unknown = %v", unknown)
	}

	// A window starting after the silence still finds it for its open PRs
	// (SilenceLookback), but names no silence of its own.
	later := load(t, st, days(14), days(21))
	if len(later.Meta.Silences) != 0 {
		t.Errorf("later silences = %+v", later.Meta.Silences)
	}
	if len(later.OpenPRs) != 2 || !slices.ContainsFunc(later.OpenPRs, func(o dataset.OpenPR) bool { return o.PRID == 1 && o.Unknown }) {
		t.Errorf("later open = %+v", later.OpenPRs)
	}

	q, err := st.Dataset(context.Background(), dataset.Params{Team: quiet, From: t0, To: days(7), Now: days(28)})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Meta.Silences) != 1 || q.Meta.Silences[0].Source != dataset.SourceBitbucket || q.Meta.Silences[0].UnseenRuns != 0 {
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
