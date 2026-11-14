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
		// In the window: opened or merged in it, this team's only.
		keys := slices.Sorted(maps.Keys(byKey))
		if strings.Join(keys, " ") != "kasse/kasse-api#11 kasse/kasse-api#5 kasse/kasse-api#7 kasse/kasse-api#9 kasse/kasse-gitops#3 portal/portal-web#2" {
			t.Fatalf("prs = %v", keys)
		}
		p7 := byKey[kasseAPI+"#7"]
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
	})

	t.Run("lead times", func(t *testing.T) {
		got := map[string]string{}
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
			got[l.Environment+":"+l.CommitSHA[:1]] = flags
			if l.Repo != kasseAPI || l.Project != "kasse" {
				t.Errorf("lead time repo = %s %s", l.Repo, l.Project)
			}
		}
		want := map[string]string{
			"intg:1": "", "intg:3": "bot", "intg:f": "bot", "intg:c": "bot", "intg:2": "merge", "intg:4": "service",
			"prod:1": "", "prod:3": "bot", "prod:f": "bot", "prod:c": "bot", "prod:2": "merge", "prod:4": "service",
		}
		if len(got) != len(want) {
			t.Fatalf("lead times = %v", got)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s = %q, want %q", k, got[k], v)
			}
		}
		for _, l := range d.LeadTimes {
			if l.Environment == "prod" && l.CommitSHA == sha("1") &&
				(l.LeadTimeS != int64((days(3).Sub(hours(19))).Seconds()) || !l.AuthoredAt.Equal(hours(19))) {
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
			"kasse-api/release%2F1.1|release/1.1|release|hotfix|kasse/kasse-api|kasse",
			"nightly||other|<nil>|<nil>|<nil>",
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("runs =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
		r := d.Runs[2]
		if r.Source != "jenkins" || r.RunID != "10" || str(r.Status) != "SUCCESS" || r.DurationS == nil || *r.DurationS != 600 ||
			strings.Join(r.ImageRefs, ",") != "reg/kasse-api:1.1" || r.StartedAt == nil || r.FinishedAt == nil {
			t.Errorf("run = %+v", r)
		}
	})

	t.Run("reviews", func(t *testing.T) {
		if len(d.Reviews) != 1 {
			t.Fatalf("reviews = %+v", d.Reviews)
		}
		r := d.Reviews[0]
		if r.PRKey != kasseAPI+"#7" || str(r.Repo) != kasseAPI || str(r.Project) != "kasse" || str(r.Outcome) != "merged" ||
			str(r.CostUSD) != "0.123456" || *r.LinesAdded != 10 || *r.LinesRemoved != 2 || *r.FilesChanged != 3 ||
			*r.FindingsCount != 4 || *r.TotalRuns != 2 || !r.ClosedAt.Equal(days(1).Add(5*time.Hour)) {
			t.Errorf("review = %+v", r)
		}
	})
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

// The facts reproduce the README's reference queries run on the same data:
// the release count of the deployment-frequency query and every stage of the
// lead-time breakdown.
func TestDatasetMatchesTheREADMEQueries(t *testing.T) {
	st, pool := migrated(t)
	seed(t, pool)
	d := load(t, st, days(-60), days(60))

	t.Run("deployment frequency", func(t *testing.T) {
		rows := readmeRows(t, pool, "#### Deployment-frequency query",
			`SELECT week, releases, first_seen, syncs FROM (%s) q WHERE team = 'checkout'`)
		readme := map[string][3]int64{}
		for _, r := range rows {
			readme[r[0].(time.Time).Format(time.DateOnly)] = [3]int64{r[1].(int64), r[2].(int64), r[3].(int64)}
		}
		// The same counts from the facts: distinct revisions per week.
		type set = map[string]bool
		weeks := map[string][3]set{}
		for _, dep := range d.Deploys {
			if str(dep.Environment) != "prod" || dep.Phase != "Succeeded" {
				continue
			}
			wk := weekStart(dep.At).Format(time.DateOnly)
			s, ok := weeks[wk]
			if !ok {
				s = [3]set{{}, {}, {}}
				weeks[wk] = s
			}
			switch str(dep.Kind) {
			case "release":
				s[0][dep.Revision] = true
			case "first_seen":
				s[1][dep.Revision] = true
			}
			s[2][dep.Revision] = true
		}
		facts := map[string][3]int64{}
		var releases int64
		for wk, s := range weeks {
			facts[wk] = [3]int64{int64(len(s[0])), int64(len(s[1])), int64(len(s[2]))}
			releases += int64(len(s[0]))
		}
		if len(readme) != len(facts) || releases != 1 {
			t.Fatalf("weeks: readme %v, facts %v (releases %d)", readme, facts, releases)
		}
		for wk, want := range readme {
			if facts[wk] != want {
				t.Errorf("week %s: facts %v, readme %v", wk, facts[wk], want)
			}
		}
	})

	t.Run("lead-time breakdown", func(t *testing.T) {
		rows := readmeRows(t, pool, "#### Lead-time breakdown query",
			`SELECT step, prs, extract(epoch FROM p50)::float8, approved_on_first_touch FROM (%s) q`)
		var chain []dataset.PR
		for _, p := range d.PRs {
			if !p.Bot && !p.GitOps && p.MergedAt != nil && p.ReadyAt != nil {
				chain = append(chain, p)
			}
		}
		stage := func(step int64) []float64 {
			var out []float64
			add := func(a, b *time.Time) {
				if a != nil && b != nil && !b.Before(*a) {
					out = append(out, b.Sub(*a).Seconds())
				}
			}
			for _, p := range chain {
				intg, prod := envAt(p, "intg"), envAt(p, "prod")
				switch step {
				case 1:
					add(p.FirstCommitAt, p.ReadyAt)
				case 2:
					add(p.ReadyAt, p.ApprovedAt)
				case 3:
					add(p.ApprovedAt, p.MergedAt)
				case 4:
					add(p.MergedAt, intg)
				case 5:
					add(intg, prod)
				}
			}
			return out
		}
		if len(rows) != 5 {
			t.Fatalf("readme stages = %v", rows)
		}
		for _, r := range rows {
			step, prs, p50 := r[0].(int32), r[1].(int64), r[2].(float64)
			got := stage(int64(step))
			if int64(len(got)) != prs || math.Abs(median(got)-p50) > 1 {
				t.Errorf("stage %d: facts %d PRs p50 %.0fs, readme %d PRs p50 %.0fs", step, len(got), median(got), prs, p50)
			}
			if step == 2 {
				var first int64
				for _, p := range chain {
					if p.ApprovedOnFirstTouch {
						first++
					}
				}
				if r[3] == nil || r[3].(int64) != first || first != 1 {
					t.Errorf("approved on first touch: facts %d, readme %v", first, r[3])
				}
			}
		}
	})
}

func envAt(p dataset.PR, env string) *time.Time {
	for _, e := range p.Environments {
		if e.Environment == env {
			return &e.FirstDeployedAt
		}
	}
	return nil
}

// median is percentile_cont(0.5).
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	pos := 0.5 * float64(len(s)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return s[lo] + (s[hi]-s[lo])*(pos-float64(lo))
}

// weekStart is date_trunc('week', t) in UTC: the Monday.
func weekStart(t time.Time) time.Time {
	t = t.UTC().Truncate(24 * time.Hour)
	return t.AddDate(0, 0, -((int(t.Weekday()) + 6) % 7))
}

// readmeRows runs the first SQL block under heading in the README, wrapped
// by outer (a %s for the README query), in UTC.
func readmeRows(t *testing.T, pool *pgxpool.Pool, heading, outer string) [][]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(raw), heading)
	if !ok {
		t.Fatalf("README has no %q", heading)
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
		t.Fatalf("README query under %q: %v", heading, err)
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
		{"kasse-api/release%2F1.1", "release/1.1", "release"},
		{"kasse-api/release%2f1.1", "release/1.1", "release"},
		{"kasse-api/feature%2FKAS-1-x", "feature/KAS-1-x", "feature"},
		{"kasse-api/bugfix%2Fy", "bugfix/y", "feature"},
		{"kasse-api/hotfix%2Fz", "hotfix/z", "feature"},
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
