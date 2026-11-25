package dataset_test

import (
	"context"
	"encoding/json"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/metrics"
)

func load(t *testing.T, s interface {
	Dataset(context.Context, dataset.Params) (*dataset.Dataset, error)
}, from, to time.Time) *dataset.Dataset {
	t.Helper()
	d, err := s.Dataset(context.Background(), dataset.Params{
		Team: team, From: from, To: to, ProductionStage: "prod", Automated: automated(t), Now: days(28),
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// automated is the handler's detector over a config naming renovate only:
// riptide's ingest-time rule, the bot-shaped heuristic included.
func automated(t *testing.T) func(string, string) bool {
	t.Helper()
	cfg, err := config.Parse([]byte(`{"automation": {
		"renovate": {"authors": ["Renovate-Bot"], "branch_prefixes": ["renovate/"]},
		"helper": {"authors": ["ci-helper"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return func(author, display string) bool { return cfg.DetectAutomationSource(author, display, "", false) != "" }
}

func ts(t time.Time) *time.Time { return &t }

func eqTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func str(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// pgx scans timestamptz into the process's zone, and the output must be UTC
// whatever that is: every test here runs with the zone off UTC. Set before
// any test starts a goroutine; changing it later races with pgx's.
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("UTC+5", 5*3600)
	os.Exit(m.Run())
}

func TestDataset(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	d := load(t, st, t0, days(28))

	t.Run("every timestamp is UTC", func(t *testing.T) {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "+05:00") || !strings.Contains(string(b), "Z\"") {
			t.Errorf("non-UTC timestamps in %s", b)
		}
	})

	t.Run("meta", func(t *testing.T) {
		m := d.Meta
		if m.Team != team || !m.From.Equal(t0) || !m.To.Equal(days(28)) || m.ProductionStage != "prod" || !m.GeneratedAt.Equal(days(28)) {
			t.Errorf("meta = %+v", m)
		}
		ds := m.DataStart
		if !eqTime(ds.Bitbucket, ts(days(-20))) || !eqTime(ds.ArgoCD, ts(days(-10))) ||
			!eqTime(ds.Pipeline, ts(days(-5).Add(10*time.Minute))) || !eqTime(ds.Noergler, ts(days(-19))) {
			t.Errorf("data start = %v %v %v %v", ds.Bitbucket, ds.Pipeline, ds.ArgoCD, ds.Noergler)
		}
		// A window ending now reads every event, the deploy on day 30 too: a
		// sender's clock ahead must not hide its latest.
		de := m.DataEnd
		if !eqTime(de.Bitbucket, ts(days(6).Add(time.Hour))) || !eqTime(de.ArgoCD, ts(days(30))) ||
			!eqTime(de.Pipeline, ts(days(9).Add(10*time.Minute))) || !eqTime(de.Noergler, ts(days(1).Add(5*time.Hour))) {
			t.Errorf("data end = %v %v %v %v", de.Bitbucket, de.Pipeline, de.ArgoCD, de.Noergler)
		}
		if m.BuildDataFrom == nil || m.BuildTypeFrom == nil {
			t.Error("build_data_from or build_type_from is nil")
		}
		if m.From.Location() != time.UTC || ds.Bitbucket.Location() != time.UTC {
			t.Error("timestamps are not UTC")
		}
	})

	t.Run("products derived from the data", func(t *testing.T) {
		got := map[string]string{}
		for _, p := range d.Products {
			got[p.Key] = strings.Join(p.Repos, ",") + " | " + strings.Join(p.Apps, ",")
		}
		want := map[string]string{
			"kasse":  kasseAPI + "," + kasseOps + " | kasse-intg,kasse-prod",
			"portal": portalWeb + " | portal-prod",
		}
		if len(got) != len(want) || got["kasse"] != want["kasse"] || got["portal"] != want["portal"] {
			t.Errorf("products = %v", got)
		}
	})

	t.Run("prs", func(t *testing.T) {
		byKey := map[string]dataset.PR{}
		for _, p := range d.PRs {
			byKey[p.Repo+"#"+strconv.FormatInt(p.PRID, 10)] = p
		}
		// The size is the merge rollup's: PR #7 has one, PR #5 was merged
		// without a review.
		num := func(v *int64) int64 {
			if v == nil {
				return -1
			}
			return *v
		}
		if p := byKey[kasseAPI+"#7"]; num(p.LinesAdded) != 10 || num(p.LinesRemoved) != 2 || num(p.FilesChanged) != 3 {
			t.Errorf("PR 7 size = %d/%d/%d", num(p.LinesAdded), num(p.LinesRemoved), num(p.FilesChanged))
		}
		if p := byKey[kasseAPI+"#5"]; p.LinesAdded != nil || p.FilesChanged != nil {
			t.Errorf("PR 5 size = %d, want none", num(p.LinesAdded))
		}
		// In the window: opened or merged in it, this team's only.
		keys := slices.Sorted(maps.Keys(byKey))
		if strings.Join(keys, " ") != "kasse/kasse-api#11 kasse/kasse-api#5 kasse/kasse-api#7 kasse/kasse-api#9 kasse/kasse-gitops#3 portal/portal-web#2" {
			t.Fatalf("prs = %v", keys)
		}
		p7 := byKey[kasseAPI+"#7"]
		// The title as last seen: edited before the merge.
		if str(p7.Title) != "Pay by card" || byKey[kasseAPI+"#5"].Title != nil {
			t.Errorf("titles = %v %v", str(p7.Title), str(byKey[kasseAPI+"#5"].Title))
		}
		// The link as last seen too: the project was renamed since opening.
		if str(p7.URL) != "https://bb.example.com/projects/KASSE/repos/kasse-api/pull-requests/7" || byKey[kasseAPI+"#5"].URL != nil {
			t.Errorf("urls = %v %v", str(p7.URL), str(byKey[kasseAPI+"#5"].URL))
		}
		if p7.Project != "kasse" || p7.GitOps || p7.Bot || str(p7.ChangeType) != "feature" || p7.State != "merged" ||
			strings.Join(p7.JiraKeys, ",") != "KAS-7" || str(p7.MergeCommit) != sha("2") {
			t.Errorf("pr 7 = %+v", p7)
		}
		// The bot's comment is not the pickup; the approval, the reviewer's
		// first touch, is.
		if !eqTime(p7.OpenedAt, ts(days(1))) || !eqTime(p7.ReadyAt, ts(days(1))) ||
			!eqTime(p7.FirstReviewAt, ts(days(1).Add(3*time.Hour))) || !eqTime(p7.ApprovedAt, p7.FirstReviewAt) ||
			!p7.ApprovedOnFirstTouch || !eqTime(p7.MergedAt, ts(days(1).Add(5*time.Hour))) ||
			!eqTime(p7.FirstCommitAt, ts(hours(19))) {
			t.Errorf("pr 7 times = %v %v %v %v %v %v", p7.OpenedAt, p7.ReadyAt, p7.FirstReviewAt, p7.ApprovedAt, p7.MergedAt, p7.FirstCommitAt)
		}
		// Earliest first.
		if e := p7.Environments; len(e) != 2 || e[0].Environment != "intg" || !e[0].FirstDeployedAt.Equal(days(2)) ||
			e[1].Environment != "prod" || !e[1].FirstDeployedAt.Equal(days(3)) {
			t.Errorf("pr 7 environments = %v", p7.Environments)
		}
		// Opened before the window as a draft: keeps its opened_at, the clock
		// starts at the flip, the draft-phase comment is no pickup.
		p5 := byKey[kasseAPI+"#5"]
		if !eqTime(p5.OpenedAt, ts(days(-3))) || !eqTime(p5.ReadyAt, ts(days(-2))) || !eqTime(p5.FirstReviewAt, ts(days(-1))) ||
			p5.ApprovedOnFirstTouch || !eqTime(p5.FirstCommitAt, ts(days(-4).Add(-2*time.Hour))) ||
			strings.Join(p5.JiraKeys, ",") != "KAS-5,KAS-6" || len(p5.Environments) != 0 || p5.Environments == nil {
			t.Errorf("pr 5 = %+v", p5)
		}
		if p3 := byKey[kasseOps+"#3"]; !p3.GitOps || p3.Bot || str(p3.ChangeType) != "other" {
			t.Errorf("gitops pr = %+v", p3)
		}
		// Not automated at ingest; the config names the opener now, as it
		// does the same account's commit in lead times.
		if p11 := byKey[kasseAPI+"#11"]; !p11.Bot {
			t.Errorf("config-named pr = %+v", p11)
		}
		if p9 := byKey[kasseAPI+"#9"]; !p9.Bot || p9.State != "open" || p9.MergedAt != nil {
			t.Errorf("renovate pr = %+v", p9)
		}
		// Bot by a sender's declaration alone: is_automated is false.
		if p2 := byKey[portalWeb+"#2"]; !p2.Bot || p2.State != "declined" || p2.ReadyAt == nil {
			t.Errorf("declared-service pr = %+v", p2)
		}
	})

	t.Run("deploys", func(t *testing.T) {
		var got []string
		for _, dep := range d.Deploys {
			got = append(got, dep.App+"@"+dep.At.Sub(t0).String()+"="+str(dep.Kind)+"/"+str(dep.Project))
		}
		want := []string{
			"kasse-intg@48h0m0s=first_seen/kasse",
			"kasse-prod@72h0m0s=release/kasse", // its first deploy was before the window
			"kasse-prod@96h0m0s=sync/kasse",
			"kasse-prod@120h0m0s=<nil>/kasse", // failed
			"portal-prod@144h0m0s=first_seen/portal",
			"atlas-prod@168h0m0s=first_seen/<nil>",
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("deploys =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		first := d.Deploys[1]
		if str(first.Environment) != "prod" || first.Revision != sha("c") || first.Phase != "Succeeded" ||
			first.DurationS == nil || *first.DurationS != 45 || strings.Join(first.Images, ",") != "reg/kasse-api:1.1" {
			t.Errorf("deploy = %+v", first)
		}
		// What each brought: the release its new image, the rest nothing.
		var brought []string
		for _, dep := range d.Deploys {
			if dep.NewImages == nil {
				t.Errorf("%s: new_images is nil", dep.App)
			}
			brought = append(brought, strings.Join(dep.NewImages, ","))
		}
		if got := strings.Join(brought, "|"); got != "|reg/kasse-api:1.1||||" {
			t.Errorf("new images = %q", got)
		}
	})

	t.Run("lead times", func(t *testing.T) {
		// Keyed by environment and commit author, each author's flags
		// sorted: dev-anna has two commits, 1 and the merge 2.
		got := map[string][]string{}
		for _, l := range d.LeadTimes {
			flags := ""
			for _, f := range []struct {
				on   bool
				name string
			}{{l.IsMerge, "merge"}, {l.ServiceAccount, "service"}, {l.Bot, "bot"}} {
				if f.on {
					flags += f.name
				}
			}
			if flags == "" {
				flags = "-"
			}
			key := l.Environment + ":" + l.Author
			got[key] = append(got[key], flags)
			if l.Repo != kasseAPI || l.Project != "kasse" {
				t.Errorf("lead time repo = %s %s", l.Repo, l.Project)
			}
		}
		want := map[string]string{}
		for _, env := range []string{"intg", "prod"} {
			for author, flags := range map[string]string{"dev-anna": "-,merge", "renovate-bot": "bot",
				"deploy-bot": "bot", "ci-helper": "bot", "ci-release": "service"} {
				want[env+":"+author] = flags
			}
		}
		if len(got) != len(want) {
			t.Fatalf("lead times = %v", got)
		}
		for k, v := range want {
			slices.Sort(got[k])
			if g := strings.Join(got[k], ","); g != v {
				t.Errorf("%s = %q, want %q", k, g, v)
			}
		}
		for _, l := range d.LeadTimes {
			if l.Environment == "prod" && l.Author == "dev-anna" && !l.IsMerge &&
				l.LeadTimeS != int64((days(3).Sub(hours(19))).Seconds()) {
				t.Errorf("lead time = %+v", l)
			}
		}
	})

	t.Run("runs", func(t *testing.T) {
		var got []string
		for _, r := range d.Runs {
			got = append(got, strings.Join([]string{r.PipelineName, r.Branch, r.BranchKind, str(r.BuildType), str(r.Repo), str(r.Project)}, "|"))
		}
		// One row per run however many Bitbucket rows share its commit.
		want := []string{
			"kasse-api/feature%2Fkas-7-pay|feature/kas-7-pay|feature|build|kasse/kasse-api|kasse",
			"kasse-api/PR-7|PR-7|pr|<nil>|kasse/kasse-api|kasse",
			"kasse-api/master|master|main|release|kasse/kasse-api|kasse",
			"kasse-api/release%2F1.1|release/1.1|other|hotfix|kasse/kasse-api|kasse",
			"nightly||other|<nil>|<nil>|<nil>",
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("runs =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		r := d.Runs[2]
		if r.Source != "jenkins" || r.RunID != "10" || str(r.CommitSHA) != sha("4") || str(r.Status) != "SUCCESS" || r.DurationS == nil || *r.DurationS != 600 ||
			strings.Join(r.ImageRefs, ",") != "reg/kasse-api:1.1" || r.StartedAt == nil || r.FinishedAt == nil ||
			r.AgentStartedAt == nil || !r.AgentStartedAt.Equal(r.StartedAt.Add(2*time.Minute)) {
			t.Errorf("run = %+v", r)
		}
		if pr := d.Runs[1]; pr.AgentStartedAt != nil {
			t.Errorf("run without agent time = %v", *pr.AgentStartedAt)
		}
	})

	t.Run("reviews", func(t *testing.T) {
		if len(d.Reviews) != 1 {
			t.Fatalf("reviews = %+v", d.Reviews)
		}
		r := d.Reviews[0]
		if str(r.Repo) != kasseAPI || str(r.Project) != "kasse" || r.GitOps || str(r.Outcome) != "merged" ||
			str(r.CostUSD) != "0.123456" || *r.LinesAdded != 10 || *r.LinesRemoved != 2 {
			t.Errorf("review = %+v", r)
		}
	})
}

// A run whose COMPLETED event lacks the agent time takes it from its
// STARTED event: the same team's, whatever the phase's case. Another
// team's run of the same name is not it.
func TestRunAgentTimeFromStarted(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	agent := days(1).Add(time.Hour + 3*time.Minute)
	insert := func(id, phase string, at time.Time, teamName string) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), `INSERT INTO pipeline_events (delivery_id, source, pipeline_name, run_id, phase,
			commit_sha, started_at, agent_started_at, occurred_at, team, payload)
			VALUES ($1, 'jenkins', 'kasse-api/PR-7', '1', $2, $3, $4, $5, $4, $6, '{}')`,
			id, phase, sha("1"), days(1).Add(time.Hour), at, teamName); err != nil {
			t.Fatal(err)
		}
	}
	insert("other-team", "STARTED", agent.Add(time.Hour), "other")
	insert("own", "Started", agent, team)
	d := load(t, st, t0, days(28))
	for _, r := range d.Runs {
		if r.PipelineName != "kasse-api/PR-7" {
			continue
		}
		if r.AgentStartedAt == nil || !r.AgentStartedAt.Equal(agent) {
			t.Errorf("agent started at = %v, want %v", r.AgentStartedAt, agent)
		}
		return
	}
	t.Fatal("run kasse-api/PR-7 missing")
}

// A PR build of a merge the CI made itself reports a commit Bitbucket never
// saw: its head_sha finds the repository. A run without a commit is read,
// with its pipeline's repository.
func TestRunRepoFromHeadSHA(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	at := days(1).Add(time.Hour)
	for _, r := range []struct {
		id, run            string
		commit, head, base any
	}{{"merge", "1", sha("m"), sha("1"), sha("n")}, {"no-commit", "2", nil, nil, nil}} {
		if _, err := pool.Exec(t.Context(), `INSERT INTO pipeline_events (delivery_id, source, pipeline_name, run_id, phase,
			status, commit_sha, head_sha, base_sha, started_at, occurred_at, team, payload)
			VALUES ($1, 'jenkins', 'other-name/PR-9', $2, 'COMPLETED', 'FAILURE', $3, $4, $5, $6, $6, $7, '{}')`,
			r.id, r.run, r.commit, r.head, r.base, at, team); err != nil {
			t.Fatal(err)
		}
	}
	d := load(t, st, t0, days(28))
	got := map[string]string{}
	for _, r := range d.Runs {
		if r.PipelineName == "other-name/PR-9" {
			got[r.RunID] = str(r.Repo) + "|" + str(r.HeadSHA) + "|" + str(r.BaseSHA) + "|" + str(r.CommitSHA)
		}
	}
	// The run without a commit takes its pipeline's repository: excluded
	// projects and products see it like the pipeline's other runs.
	if want := map[string]string{"1": kasseAPI + "|" + sha("1") + "|" + sha("n") + "|" + sha("m"), "2": kasseAPI + "|<nil>|<nil>|<nil>"}; !maps.Equal(got, want) {
		t.Errorf("runs = %v, want %v", got, want)
	}
}

// The window is [from, to): rows on the end bound fall out, a deploy's kind
// still comes from history before the window.
func TestDatasetWindow(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	d := load(t, st, days(3), days(4))
	if len(d.Deploys) != 1 || str(d.Deploys[0].Kind) != "release" {
		t.Errorf("deploys = %+v", d.Deploys)
	}
	if len(d.PRs) != 0 || len(d.Runs) != 1 || len(d.Reviews) != 0 {
		t.Errorf("prs %d, runs %d, reviews %d", len(d.PRs), len(d.Runs), len(d.Reviews))
	}
	for _, l := range d.LeadTimes {
		if l.Environment != "prod" {
			t.Errorf("lead time outside the window: %+v", l)
		}
	}
	if len(d.LeadTimes) != 6 {
		t.Errorf("lead times = %d", len(d.LeadTimes))
	}
	// A past window's source ends at its latest event before the window's
	// end, the deploy at exactly day 4 aside, so a closed period can tell a
	// source that went quiet within it; its start stays all history.
	if !eqTime(d.Meta.DataEnd.ArgoCD, ts(days(3))) || !eqTime(d.Meta.DataStart.ArgoCD, ts(days(-10))) {
		t.Errorf("argocd start %v end %v", d.Meta.DataStart.ArgoCD, d.Meta.DataEnd.ArgoCD)
	}
	// Nothing at all: every section is an empty list, never null.
	empty := load(t, st, days(100), days(101))
	if empty.PRs == nil || empty.Deploys == nil || empty.LeadTimes == nil || empty.Runs == nil ||
		empty.Reviews == nil || empty.Products == nil {
		t.Errorf("empty dataset has nil sections: %+v", empty)
	}
}

// The other team sees only its own rows, though it shares the database.
func TestDatasetTeamIsolation(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	d, err := st.Dataset(context.Background(), dataset.Params{Team: otherTeam, From: t0, To: days(28), Now: days(28)})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.PRs) != 1 || d.PRs[0].Repo != ledger || len(d.Deploys) != 1 || d.Deploys[0].App != "ledger-prod" ||
		len(d.Runs) != 1 || len(d.Reviews) != 1 || len(d.LeadTimes) != 0 {
		t.Errorf("other team = %d prs, %d deploys, %d runs, %d reviews, %d lead times",
			len(d.PRs), len(d.Deploys), len(d.Runs), len(d.Reviews), len(d.LeadTimes))
	}
	// Its pipelines never sent a build_type: not collected, unlike checkout's.
	if d.Meta.BuildDataFrom == nil || d.Meta.BuildTypeFrom != nil {
		t.Errorf("build data from %v, build type from %v", d.Meta.BuildDataFrom, d.Meta.BuildTypeFrom)
	}
}

// Another team's App of the same name never decides this team's kinds: the
// other team deploying kasse-prod's new images first does not make
// checkout's release a sync.
func TestDatasetAppNameSharedAcrossTeams(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	s := &seeder{t: t, pool: pool, n: 1000}
	s.deploy(otherTeam, "kasse-prod", "prod", sha("9"), "Succeeded", days(2.5), "reg/kasse-api:1.1")
	s.deploy(otherTeam, "portal-prod", "prod", sha("9"), "Succeeded", days(1), "reg/portal-web:2.0")
	d := load(t, st, t0, days(28))
	kinds := map[string]string{}
	for _, dep := range d.Deploys {
		kinds[dep.App+"@"+dep.At.Sub(t0).String()] = str(dep.Kind)
	}
	if kinds["kasse-prod@72h0m0s"] != "release" || kinds["portal-prod@144h0m0s"] != "first_seen" {
		t.Errorf("kinds = %v", kinds)
	}
}

// A reused source branch: each PR's coding time starts at the commits pushed
// during its own life, after the previous PR from that branch closed and
// until it became ready.
func TestDatasetFirstCommitOnAReusedBranch(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	const branch = "feature/train"
	s.push(kasseAPI, branch, days(-70), commit{sha: sha("a"), authored: days(-71)})
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 20, author: "dev-anna", branch: branch, at: days(-69)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 20, author: "dev-anna", branch: branch, at: days(-68)})
	s.push(kasseAPI, branch, days(-10), commit{sha: sha("b"), authored: days(-11)})
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 21, author: "dev-anna", branch: branch, at: days(-9)})
	// Pushed after #21 was ready: an old commit rebased in later is not its start.
	s.push(kasseAPI, branch, days(-8.5), commit{sha: sha("c"), authored: days(-80)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 21, author: "dev-anna", branch: branch, at: days(-8)})

	// #40, from another reused branch, was opened before ingestion started:
	// only its pr:merged is known, so no ready_at. Its life ends at the merge,
	// not at the later #41's pushes.
	const old = "feature/old"
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 40, author: "dev-anna", branch: old, at: days(-50)})
	s.push(kasseAPI, old, days(-10), commit{sha: sha("d"), authored: days(-12)})
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 41, author: "dev-anna", branch: old, at: days(-9)})

	d := load(t, st, days(-100), t0)
	got := map[int64]*time.Time{}
	for _, p := range d.PRs {
		got[p.PRID] = p.FirstCommitAt
	}
	if len(got) != 4 || !eqTime(got[20], ts(days(-71))) || !eqTime(got[21], ts(days(-11))) ||
		got[40] != nil || !eqTime(got[41], ts(days(-12))) {
		t.Errorf("first commits = #20 %v, #21 %v, #40 %v, #41 %v", got[20], got[21], got[40], got[41])
	}
}

// A PR merged in the window keeps the stages it reached after the window's
// end: environments have no upper bound, the lead_times section has.
func TestDatasetEnvironmentsAfterTheWindow(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	// PR #7 merged at T0+1d+5h; intg at T0+2d, prod at T0+3d: both after it.
	d := load(t, st, t0, days(2))
	var p7 *dataset.PR
	for i := range d.PRs {
		if d.PRs[i].Repo == kasseAPI && d.PRs[i].PRID == 7 {
			p7 = &d.PRs[i]
		}
	}
	if p7 == nil {
		t.Fatal("pr 7 not in the window")
	}
	if e := p7.Environments; len(e) != 2 || e[0].Environment != "intg" || !e[0].FirstDeployedAt.Equal(days(2)) ||
		e[1].Environment != "prod" || !e[1].FirstDeployedAt.Equal(days(3)) {
		t.Errorf("environments = %v", p7.Environments)
	}
	if len(d.LeadTimes) != 0 {
		t.Errorf("lead times past the window: %d", len(d.LeadTimes))
	}
}

// A noergler review of a stage promotion is flagged gitops, as the PR is:
// teams let noergler review them, on purpose or not.
func TestDatasetReviewOfPromotion(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	s := &seeder{t: t, pool: pool, n: 1000}
	s.rollup(team, kasseOps+"#3", "merged", days(2))
	d := load(t, st, t0, days(28))
	gitops := map[string]bool{}
	for _, r := range d.Reviews {
		gitops[str(r.Repo)] = r.GitOps
	}
	if len(gitops) != 2 || !gitops[kasseOps] || gitops[kasseAPI] {
		t.Errorf("reviews gitops = %v", gitops)
	}
}

// A CI run in a GitOps repo is flagged gitops, as its PRs are: a stage
// promotion's check, not the team's build.
func TestDatasetRunOfPromotion(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	s := &seeder{t: t, pool: pool, n: 1000}
	s.run(run{pipeline: "kasse-gitops/master", runID: "1", status: "SUCCESS", commit: sha("c"), started: days(2)})
	d := load(t, st, t0, days(28))
	gitops := map[string]bool{}
	for _, r := range d.Runs {
		gitops[r.PipelineName] = r.GitOps
	}
	if !gitops["kasse-gitops/master"] || gitops["kasse-api/master"] || gitops["nightly"] {
		t.Errorf("runs gitops = %v", gitops)
	}
	// Over all data, not the team's or window's only.
	if !slices.Equal(d.GitOpsRepos, []string{kasseOps, "portal/portal-gitops"}) {
		t.Errorf("gitops repos = %v", d.GitOpsRepos)
	}
}

// approved_at counts reviewers as first_review_at does: a bot's approval
// (recognised now, not at ingest) is neither, so a human approving on their
// first touch is approved_on_first_touch.
func TestDatasetApprovalCountsHumanReviewers(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 30, author: "dev-anna", branch: "feature/x", at: days(1)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 30, author: "review-bot", at: days(1).Add(time.Hour)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 30, author: "rita", at: days(1).Add(2 * time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 30, author: "dev-anna", branch: "feature/x",
		mergeCommit: strings.ToUpper(sha("a")), at: days(1).Add(3 * time.Hour)})
	d := load(t, st, t0, days(28))
	if len(d.PRs) != 1 {
		t.Fatalf("prs = %+v", d.PRs)
	}
	p := d.PRs[0]
	human := ts(days(1).Add(2 * time.Hour))
	if !eqTime(p.ApprovedAt, human) || !eqTime(p.FirstReviewAt, human) || !p.ApprovedOnFirstTouch || p.Bot {
		t.Errorf("approved %v, first review %v, first touch %v, bot %v", p.ApprovedAt, p.FirstReviewAt, p.ApprovedOnFirstTouch, p.Bot)
	}
	// The payload keeps the sender's casing; the output is lowercased.
	if str(p.MergeCommit) != sha("a") {
		t.Errorf("merge commit = %s", str(p.MergeCommit))
	}
}

// The opener's own comment is no review, whatever the case of the login:
// open PRs compare case-insensitively too.
func TestDatasetOpenerTouchIgnoresCase(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 31, author: "Jdoe", branch: "feature/y", at: days(1)})
	s.pr(prEvent{event: "pr:comment:added", repo: kasseAPI, id: 31, author: "jdoe", at: days(1).Add(time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 31, author: "Jdoe", branch: "feature/y",
		mergeCommit: sha("b"), at: days(1).Add(2 * time.Hour)})
	// A reviewer-status touch by the opener is none either: the real
	// reviewer's approval is the first touch.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 32, author: "Jdoe", branch: "feature/z", at: days(2)})
	s.pr(prEvent{event: "pr:reviewer:needs_work", repo: kasseAPI, id: 32, author: "jdoe", at: days(2).Add(time.Hour)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 32, author: "rita", at: days(2).Add(2 * time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 32, author: "Jdoe", branch: "feature/z",
		mergeCommit: sha("c"), at: days(2).Add(3 * time.Hour)})
	d := load(t, st, t0, days(28))
	if len(d.PRs) != 2 {
		t.Fatalf("prs = %+v", d.PRs)
	}
	if p := d.PRs[0]; p.FirstReviewAt != nil {
		t.Errorf("first review %v: the opener's own comment", p.FirstReviewAt)
	}
	approved := ts(days(2).Add(2 * time.Hour))
	if p := d.PRs[1]; !eqTime(p.FirstReviewAt, approved) || !eqTime(p.ApprovedAt, approved) || !p.ApprovedOnFirstTouch {
		t.Errorf("first review %v, approved %v, first touch %v", p.FirstReviewAt, p.ApprovedAt, p.ApprovedOnFirstTouch)
	}
}

// A sender declares its account by the name Bitbucket shows: a bot
// provisioned as an ordinary user, its login meaningless. Rows ingested
// before it was recognised are a bot's, as in lead times: no reviewer's
// touch or status, no person's PR.
func TestDatasetDeclaredBotByDisplayName(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	s.exec(`INSERT INTO noergler_events (delivery_id, event_type, pr_key, repo, outcome, reviewer_handle,
		reviewer_account_kind, occurred_at, team, payload)
		VALUES ('declared', 'pr_completed', $1, $2, 'merged', 'ai-reviewer', 'bot', $3, $4, '{}')`,
		kasseAPI+"#1", kasseAPI, days(-1), team)
	// A CI declares the account it merges through a service.
	s.run(run{pipeline: "kasse-api/master", runID: "1", status: "SUCCESS", actor: "ci-merger", started: days(-1)})
	bot := map[string]string{"u4711": "AI-Reviewer"}
	// Merged: the service account comments first, a person approves later.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 37, author: "dev-anna", branch: "feature/s", at: days(1)})
	s.pr(prEvent{event: "pr:comment:added", repo: kasseAPI, id: 37, author: "u5000", display: "CI-Merger",
		at: days(1).Add(time.Hour)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 37, author: "rita", at: days(1).Add(2 * time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 37, author: "dev-anna", branch: "feature/s", at: days(1).Add(3 * time.Hour)})
	// Merged, the bot's own PR opened before ingestion: only its merge, which
	// names the PR author.
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 36, author: "u4711", display: "AI-Reviewer", branch: "feature/t",
		at: days(2)})
	// Merged: the bot comments first, a person approves later.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 30, author: "dev-anna", branch: "feature/x", at: days(1)})
	s.pr(prEvent{event: "pr:comment:added", repo: kasseAPI, id: 30, author: "u4711", display: "AI-Reviewer",
		at: days(1).Add(time.Hour)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 30, author: "rita", at: days(1).Add(2 * time.Hour)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 30, author: "dev-anna", branch: "feature/x", at: days(1).Add(3 * time.Hour)})
	// Merged, the bot's own PR.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 32, author: "u4711", display: "AI-Reviewer", branch: "feature/z", at: days(1)})
	s.pr(prEvent{event: "pr:merged", repo: kasseAPI, id: 32, author: "u4711", display: "AI-Reviewer", branch: "feature/z",
		at: days(1).Add(time.Hour)})
	// Open: only the bot commented.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 31, author: "dev-anna", branch: "feature/y", state: "OPEN", at: days(1)})
	s.pr(prEvent{event: "pr:comment:added", repo: kasseAPI, id: 31, author: "u4711", display: "AI-Reviewer", state: "OPEN",
		at: days(1).Add(time.Hour)})
	// Open, the bot's own PR.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 33, author: "u4711", display: "AI-Reviewer", branch: "feature/w",
		state: "OPEN", at: days(1)})
	// Open, the bot's own PR, opened before ingestion: its author as the
	// PR carries it.
	s.pr(prEvent{event: "pr:from_ref_updated", repo: kasseAPI, id: 35, author: "u4711", display: "AI-Reviewer", branch: "feature/v",
		state: "OPEN", created: days(-5), prAuthor: "u4711", displays: bot, at: days(1)})
	// Open: the bot approved as a reviewer.
	s.pr(prEvent{event: "pr:opened", repo: kasseAPI, id: 34, author: "dev-anna", branch: "feature/u", state: "OPEN", at: days(1)})
	s.pr(prEvent{event: "pr:reviewer:approved", repo: kasseAPI, id: 34, author: "u4711", display: "AI-Reviewer", state: "OPEN",
		reviewers: map[string]string{"u4711": "APPROVED"}, displays: bot, at: days(1).Add(time.Hour)})
	d := load(t, st, t0, days(10))
	merged := map[int64]dataset.PR{}
	for _, p := range d.PRs {
		merged[p.PRID] = p
	}
	if p := merged[30]; !eqTime(p.FirstReviewAt, ts(days(1).Add(2*time.Hour))) {
		t.Errorf("#30 first review = %v", p.FirstReviewAt)
	}
	if p := merged[37]; !eqTime(p.FirstReviewAt, ts(days(1).Add(2*time.Hour))) {
		t.Errorf("#37 first review = %v", p.FirstReviewAt)
	}
	if p := merged[32]; !p.Bot {
		t.Errorf("#32 = %+v", p)
	}
	if p, ok := merged[36]; !ok || !p.Bot {
		t.Errorf("#36 = %+v", p)
	}
	open := map[int64]dataset.OpenPR{}
	for _, o := range d.OpenPRs {
		open[o.PRID] = o
	}
	if len(open) != 4 {
		t.Fatalf("open prs = %+v", d.OpenPRs)
	}
	if o := open[31]; o.Touched || o.Bot {
		t.Errorf("#31 = %+v", o)
	}
	if !open[33].Bot || !open[35].Bot {
		t.Errorf("#33 bot %v, #35 bot %v", open[33].Bot, open[35].Bot)
	}
	if o := open[34]; o.Approved || o.Touched || o.Bot {
		t.Errorf("#34 = %+v", o)
	}
}

// A sync whose payload names no images deployed none: it is no release, and
// the next sync of the images the App already ran is none either.
func TestDatasetDeployWithoutImages(t *testing.T) {
	st, pool := migrated(t)
	s := &seeder{t: t, pool: pool}
	// The App's very first deploy names none: a sync, the first naming
	// images is its first_seen.
	s.deploy(team, "kasse-prod", "prod", sha("0"), "Succeeded", days(0.5))
	s.deploy(team, "kasse-prod", "prod", sha("1"), "Succeeded", days(1), "reg/kasse-api:1.0")
	s.deploy(team, "kasse-prod", "prod", sha("2"), "Succeeded", days(2))
	s.deploy(team, "kasse-prod", "prod", sha("3"), "Succeeded", days(3), "reg/kasse-api:1.0")
	d := load(t, st, t0, days(28))
	var got []string
	for _, dep := range d.Deploys {
		got = append(got, str(dep.Kind)+"/"+strings.Join(dep.NewImages, ","))
	}
	if want := []string{"sync/", "first_seen/", "sync/", "sync/"}; !slices.Equal(got, want) {
		t.Errorf("kinds = %v, want %v", got, want)
	}
	// The documented query agrees.
	rows := docRows(t, pool, "#### Deployment-frequency query",
		`SELECT coalesce(sum(releases), 0)::int8, coalesce(sum(first_seen), 0)::int8, coalesce(sum(syncs), 0)::int8
		   FROM (%s) q WHERE team = 'checkout'`)
	if len(rows) != 1 || !slices.Equal(rows[0], []any{int64(0), int64(1), int64(4)}) {
		t.Errorf("doc releases, first seen, syncs = %v, want [0 1 4]", rows)
	}
}

// The facts tables' read skips the lead times and the reviews, nothing else.
func TestDatasetFactsOnly(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	full := load(t, st, days(-60), days(60))
	d, err := st.Dataset(context.Background(), dataset.Params{
		Team: team, From: days(-60), To: days(60), ProductionStage: "prod", Automated: automated(t), Now: days(28),
		FactsOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(full.LeadTimes) == 0 || len(full.Reviews) == 0 {
		t.Fatalf("seed has no lead times or reviews: %d/%d", len(full.LeadTimes), len(full.Reviews))
	}
	if d.LeadTimes == nil || len(d.LeadTimes) != 0 || d.Reviews == nil || len(d.Reviews) != 0 {
		t.Errorf("facts only read lead times %d, reviews %d", len(d.LeadTimes), len(d.Reviews))
	}
	if len(d.PRs) != len(full.PRs) || len(d.Deploys) != len(full.Deploys) || len(d.Runs) != len(full.Runs) {
		t.Errorf("facts only lost rows: %d/%d/%d", len(d.PRs), len(d.Deploys), len(d.Runs))
	}
}

func projectsOf[T any](xs []T, project func(T) *string) []*string {
	out := make([]*string, len(xs))
	for i, x := range xs {
		out[i] = project(x)
	}
	return out
}

// An excluded project drops out of every section and the product list; the
// other projects' rows are exactly those of a read without the exclusion.
func TestDatasetExcludedProject(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	// The seed's portal has a PR and a deploy; give it a run (its commit is
	// one Bitbucket saw in portal), a review and a release that ships a
	// portal-web commit, for a lead time.
	s := &seeder{t: t, pool: pool, n: 1000}
	s.run(run{pipeline: "portal-web/master", runID: "1", status: "SUCCESS", commit: sha("7"), started: days(6)})
	// Failed before its checkout: no commit, still the portal's.
	s.run(run{pipeline: "portal-web/master", runID: "2", status: "FAILURE", started: days(6).Add(time.Hour)})
	s.rollup(team, portalWeb+"#2", "declined", days(6).Add(time.Hour))
	base, head, ops := strings.Repeat("e1", 20), strings.Repeat("e2", 20), strings.Repeat("e3", 20)
	s.push(portalWeb, "master", days(5), commit{sha: base, authored: days(5)})
	s.push(portalWeb, "master", days(6), commit{sha: head, authored: days(6)})
	s.push(portalOps, "master", days(6).Add(time.Hour), commit{sha: ops, authored: days(6).Add(time.Hour),
		message: "portal PROD 2.1\n\n[portal-web 2.0 -> 2.1](https://git.example.com/projects/portal/repos/portal-web/compare/diff?targetBranch=" +
			base + "&sourceBranch=" + head + ")"})
	s.deploy(team, "portal-prod", "prod", ops, "Succeeded", days(6).Add(2*time.Hour), "reg/portal-web:2.1")
	full := load(t, st, days(-60), days(60))
	// Typed as Bitbucket shows the key; stored lowercased.
	cfg, err := config.Parse([]byte(`{"excluded_projects": ["PORTAL"]}`))
	if err != nil {
		t.Fatal(err)
	}
	d, err := st.Dataset(context.Background(), dataset.Params{
		Team: team, From: days(-60), To: days(60), ProductionStage: "prod", Automated: automated(t), Now: days(28),
		Excluded: cfg.ExcludedProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	projects := func(ps ...*string) (portal, other int) {
		for _, p := range ps {
			if p != nil && *p == "portal" {
				portal++
			} else {
				other++
			}
		}
		return
	}
	for _, c := range []struct {
		name      string
		full, got []*string
	}{
		{"prs", projectsOf(full.PRs, func(x dataset.PR) *string { return &x.Project }),
			projectsOf(d.PRs, func(x dataset.PR) *string { return &x.Project })},
		{"deploys", projectsOf(full.Deploys, func(x dataset.Deploy) *string { return x.Project }),
			projectsOf(d.Deploys, func(x dataset.Deploy) *string { return x.Project })},
		{"runs", projectsOf(full.Runs, func(x dataset.Run) *string { return x.Project }),
			projectsOf(d.Runs, func(x dataset.Run) *string { return x.Project })},
		{"lead times", projectsOf(full.LeadTimes, func(x dataset.LeadTime) *string { return &x.Project }),
			projectsOf(d.LeadTimes, func(x dataset.LeadTime) *string { return &x.Project })},
		{"reviews", projectsOf(full.Reviews, func(x dataset.Review) *string { return x.Project }),
			projectsOf(d.Reviews, func(x dataset.Review) *string { return x.Project })},
	} {
		fullPortal, fullOther := projects(c.full...)
		gotPortal, gotOther := projects(c.got...)
		if fullPortal == 0 {
			t.Errorf("%s: no portal rows without the exclusion, the test proves nothing", c.name)
		}
		if gotPortal != 0 || gotOther != fullOther {
			t.Errorf("%s: %d portal, %d other kept; want 0, %d", c.name, gotPortal, gotOther, fullOther)
		}
	}
	if len(d.Products) != 1 || d.Products[0].Key != "kasse" {
		t.Errorf("products = %+v", d.Products)
	}
	// The run without a commit goes with its pipeline's project.
	for _, r := range d.Runs {
		if r.PipelineName == "portal-web/master" {
			t.Errorf("excluded project's run kept: %+v", r)
		}
	}
	if !eqTime(d.Meta.DataStart.Bitbucket, full.Meta.DataStart.Bitbucket) {
		t.Errorf("data start moved: %v", d.Meta.DataStart.Bitbucket)
	}
}

// The metrics computed from the facts reproduce docs/metrics.md's reference
// queries run on the same data: the weekly counts of the
// deployment-frequency query and every stage of the lead-time breakdown.
// UTC weeks, as date_trunc('week') in the doc. The period ends with the
// seed's window: the doc query counts a revision re-synced in a later week in
// that week too, the metrics once, in the week it first deployed. The doc
// query knows no infrastructure App and no first deploy counted as a
// release: the metrics' are put back where the doc counts them.
func TestMetricsMatchTheDocumentedQueries(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	period := metrics.NewPeriod(26, days(28), time.UTC)
	ds := load(t, st, period.From, period.To)
	m := metrics.Compute(ds, &dataset.Dataset{Meta: ds.Meta}, nil, metrics.Params{Period: period})

	t.Run("deployment frequency", func(t *testing.T) {
		rows := docRows(t, pool, "#### Deployment-frequency query",
			`SELECT week, releases, first_seen, syncs FROM (%s) q WHERE team = 'checkout'`)
		doc := map[string][3]int64{}
		for _, r := range rows {
			if week := r[0].(time.Time); week.Before(period.To) {
				doc[week.Format(time.DateOnly)] = [3]int64{r[1].(int64), r[2].(int64), r[3].(int64)}
			}
		}
		// atlas-prod is infrastructure: no number counts it.
		f := m.Delivery.Frequency
		if f.Infra != 1 || !slices.Equal(f.InfraApps, []string{"atlas-prod"}) || f.Releases != 2 || f.FirstReleases != 1 {
			t.Fatalf("infra %d %v, releases %d, first releases %d", f.Infra, f.InfraApps, f.Releases, f.FirstReleases)
		}
		all := *ds
		all.Deploys = slices.Clone(ds.Deploys)
		for i := range all.Deploys {
			all.Deploys[i].Infra = false
		}
		f = metrics.Compute(&all, &dataset.Dataset{Meta: ds.Meta}, nil, metrics.Params{Period: period}).Delivery.Frequency
		// The same counts from the metrics: distinct revisions per week, a
		// first release as the doc's first_seen.
		facts := map[string][3]int64{}
		for i, w := range f.Weeks {
			if n := w.Release + w.FirstSeen + w.Sync; n > 0 {
				facts[period.Weeks[i].Start.Format(time.DateOnly)] = [3]int64{int64(w.Release - w.FirstRelease),
					int64(w.FirstSeen + w.FirstRelease), int64(n)}
			}
		}
		if len(doc) != len(facts) || f.Releases-f.FirstReleases != 1 {
			t.Fatalf("weeks: doc %v, metrics %v (releases %d)", doc, facts, f.Releases)
		}
		for wk, want := range doc {
			if facts[wk] != want {
				t.Errorf("week %s: metrics %v, doc %v", wk, facts[wk], want)
			}
		}
	})

	t.Run("lead-time breakdown", func(t *testing.T) {
		rows := docRows(t, pool, "#### Lead-time breakdown query",
			`SELECT step, prs, extract(epoch FROM p50)::float8, approved_on_first_touch FROM (%s) q`)
		if len(rows) != 5 || len(m.LeadChain.Stages) != 5 {
			t.Fatalf("doc stages = %v, metrics %+v", rows, m.LeadChain.Stages)
		}
		for _, r := range rows {
			step, prs, p50 := r[0].(int32), r[1].(int64), r[2].(float64)
			got := m.LeadChain.Stages[step-1]
			if int64(got.N) != prs || got.P50 == nil || math.Abs(*got.P50-p50) > 1 {
				t.Errorf("stage %d (%s): metrics %d PRs p50 %v, doc %d PRs p50 %.0fs", step, got.Key, got.N, got.P50, prs, p50)
			}
			if step == 2 {
				first := int64(m.Review.ApprovedOnFirstTouch)
				if r[3] == nil || r[3].(int64) != first || first != 1 {
					t.Errorf("approved on first touch: metrics %d, doc %v", first, r[3])
				}
			}
		}
	})
}

// docRows runs the first SQL block under heading in docs/metrics.md,
// wrapped by outer (a %s for the doc's query), in UTC.
func docRows(t *testing.T, pool *pgxpool.Pool, heading, outer string) [][]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "metrics.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(raw), heading)
	if !ok {
		t.Fatalf("docs/metrics.md has no %q", heading)
	}
	_, after, ok = strings.Cut(after, "```sql\n")
	if !ok {
		t.Fatalf("no sql block under %q", heading)
	}
	query, _, _ := strings.Cut(after, "```")
	query = strings.TrimSuffix(strings.TrimSpace(query), ";")
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL TIME ZONE 'UTC'"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, strings.Replace(outer, "%s", query, 1))
	if err != nil {
		t.Fatalf("docs/metrics.md query under %q: %v", heading, err)
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		v, err := rows.Values()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBranch(t *testing.T) {
	for _, c := range []struct{ name, branch, kind string }{
		{"kasse-api/master", "master", "main"},
		{"kasse-api/main", "main", "main"},
		{"kasse-api/MASTER", "MASTER", "main"},
		{"kasse-api/release%2F1.1", "release/1.1", "other"},
		{"kasse-api/release%2f1.1", "release/1.1", "other"},
		{"kasse-api/feature%2FKAS-1-x", "feature/KAS-1-x", "feature"},
		{"kasse-api/bugfix%2Fy", "bugfix/y", "feature"},
		{"kasse-api/hotfix%2Fz", "hotfix/z", "feature"},
		{"kasse-api/feat%2FKAS-2-x", "feat/KAS-2-x", "feature"},
		{"kasse-api/fix%2Fy", "fix/y", "feature"},
		{"kasse-api/FIX%2Fy", "FIX/y", "feature"},
		{"kasse-api/prefix%2Fy", "prefix/y", "other"},
		{"kasse-api/chore%2Fdeps", "chore/deps", "feature"},
		{"kasse-api/Refactor%2Fx", "Refactor/x", "feature"},
		// docs pipelines often skip the build: no feature build.
		{"kasse-api/docs%2Freadme", "docs/readme", "other"},
		{"kasse-api/Doc%2Freadme", "Doc/readme", "other"},
		{"kasse-api/chore", "chore", "other"},
		{"kasse-api/feature", "feature", "other"},
		{"kasse-api/PR-12", "PR-12", "pr"},
		{"kasse-api/pr-3", "pr-3", "pr"},
		{"kasse-api/PR-x", "PR-x", "other"},
		{"kasse-api/release", "release", "other"},
		{"kasse-api/renovate%2Flib", "renovate/lib", "other"},
		{"folder/kasse-api/master", "master", "main"},
		{"org/folder/kasse-api/feature%2Fx%2Fy", "feature/x/y", "feature"},
		{"nightly", "", "other"},
		{"kasse-api/", "", "other"},
	} {
		if got := dataset.Branch(c.name); got != c.branch {
			t.Errorf("Branch(%q) = %q, want %q", c.name, got, c.branch)
		}
		if got := dataset.BranchKind(c.branch); got != c.kind {
			t.Errorf("BranchKind(%q) = %q, want %q", c.branch, got, c.kind)
		}
	}
}
