// Package dataset reads the per-team facts the UI computes its metrics from:
// one row per PR, deploy, shipped commit, CI run and noergler review in a
// window, never an aggregate (percentiles are the UI's). The rules are the
// README's metric queries, one embedded .sql file per section; product,
// GitOps and bot derivations come from the data, never from name lists.
// HTTP, parameters and config belong to package api.
package dataset

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/meta.sql
	metaSQL string
	//go:embed sql/prs.sql
	prsSQL string
	//go:embed sql/deploys.sql
	deploysSQL string
	//go:embed sql/lead_times.sql
	leadTimesSQL string
	//go:embed sql/environments.sql
	environmentsSQL string
	//go:embed sql/runs.sql
	runsSQL string
	//go:embed sql/reviews.sql
	reviewsSQL string
)

// Params selects one team's window. From is inclusive, To exclusive.
type Params struct {
	Team string
	From time.Time
	To   time.Time
	// ProductionStage is config's environments.production_stage, echoed in
	// meta so the UI knows which environment is production.
	ProductionStage string
	// Automated is riptide's ingest-time automation detection for a commit
	// author (login and display name), so a commit and a PR by the same bot
	// agree; nil detects nothing.
	Automated func(author, displayName string) bool
	// Now stamps meta.generated_at.
	Now time.Time
}

// Dataset is the /api/dataset body.
type Dataset struct {
	Meta      Meta       `json:"meta"`
	Products  []Product  `json:"products"`
	PRs       []PR       `json:"prs"`
	Deploys   []Deploy   `json:"deploys"`
	LeadTimes []LeadTime `json:"lead_times"`
	Runs      []Run      `json:"runs"`
	Reviews   []Review   `json:"reviews"`
}

// Meta describes the window and where each source's data starts.
type Meta struct {
	Team            string    `json:"team"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	GeneratedAt     time.Time `json:"generated_at"`
	ProductionStage string    `json:"production_stage"`
	// The team's first and latest event per source, over all history.
	DataStart     SourceTimes `json:"data_start"`
	DataEnd       SourceTimes `json:"data_end"`
	BuildDataFrom *time.Time  `json:"build_data_from"`
	// BuildTypeFrom is the team's first pipeline row carrying a build_type:
	// before it a NULL build_type means not collected yet.
	BuildTypeFrom *time.Time `json:"build_type_from"`
}

// SourceTimes is one instant per source; nil when the source never
// reported for the team.
type SourceTimes struct {
	Bitbucket *time.Time `json:"bitbucket"`
	Pipeline  *time.Time `json:"pipeline"`
	ArgoCD    *time.Time `json:"argocd"`
	Noergler  *time.Time `json:"noergler"`
}

func (s *SourceTimes) ptrs() []*time.Time {
	return []*time.Time{s.Bitbucket, s.Pipeline, s.ArgoCD, s.Noergler}
}

// Product is a Bitbucket project key and what of it shows up in the window.
type Product struct {
	Key   string   `json:"key"`
	Repos []string `json:"repos"`
	Apps  []string `json:"apps"`
}

// PR is one pull request. See sql/prs.sql for the rules.
type PR struct {
	Repo       string     `json:"repo" db:"repo"`
	Project    string     `json:"project" db:"project"`
	PRID       int64      `json:"pr_id" db:"pr_id"`
	Title      *string    `json:"title" db:"title"`
	GitOps     bool       `json:"gitops" db:"gitops"`
	Bot        bool       `json:"bot" db:"bot"`
	ChangeType *string    `json:"change_type" db:"change_type"`
	JiraKeys   []string   `json:"jira_keys" db:"jira_keys"`
	State      string     `json:"state" db:"state"`
	OpenedAt   *time.Time `json:"opened_at" db:"opened_at"`
	ReadyAt    *time.Time `json:"ready_at" db:"ready_at"`
	// FirstReviewAt and ApprovedAt count the same reviewers: not the opener,
	// not automation (at ingest, declared, or detected now), after ready_at.
	FirstReviewAt *time.Time `json:"first_review_at" db:"-"`
	ApprovedAt    *time.Time `json:"approved_at" db:"-"`
	// ApprovedOnFirstTouch: approved with no reviewer touch before it.
	ApprovedOnFirstTouch bool       `json:"approved_on_first_touch" db:"-"`
	MergedAt             *time.Time `json:"merged_at" db:"merged_at"`
	FirstCommitAt        *time.Time `json:"first_commit_at" db:"first_commit_at"`
	MergeCommit          *string    `json:"merge_commit" db:"merge_commit"`
	// Environments is the merge commit's first deploy per environment, from
	// lead_time_changes, earliest first, with no window end. A list, not
	// intg/prod fields: the stages are whatever namespaces the team deploys
	// to, and only production_stage is configured.
	Environments []EnvironmentDeploy `json:"environments" db:"-"`

	// Read for the derivations above, never sent: no handles leave the API.
	Touches           []touch `json:"-" db:"touches"`
	Opener            string  `json:"-" db:"opener"`
	OpenerDisplayName string  `json:"-" db:"opener_display_name"`
}

// touch is a reviewer's event on a PR, as prs.sql lists them.
type touch struct {
	At          time.Time `json:"at"`
	Approved    bool      `json:"approved"`
	Author      string    `json:"author"`
	DisplayName string    `json:"display_name"`
}

// EnvironmentDeploy is a commit's first deploy to one environment.
type EnvironmentDeploy struct {
	Environment     string    `json:"environment"`
	FirstDeployedAt time.Time `json:"first_deployed_at"`
}

// Deploy is one finished Argo CD sync. Kind is nil on a failed sync.
type Deploy struct {
	App         string    `json:"app" db:"app"`
	Project     *string   `json:"project" db:"project"`
	Environment *string   `json:"environment" db:"environment"`
	Revision    string    `json:"revision" db:"revision"`
	At          time.Time `json:"at" db:"at"`
	Phase       string    `json:"phase" db:"phase"`
	DurationS   *int64    `json:"duration_s" db:"duration_s"`
	Images      []string  `json:"images" db:"images"`
	Kind        *string   `json:"kind" db:"kind"`
	// NewImages are the images a release brought, those the previous
	// deploy did not run; empty for anything but a release.
	NewImages []string `json:"new_images" db:"new_images"`
}

// LeadTime is one commit's first deploy to one environment.
type LeadTime struct {
	Repo            string    `json:"repo" db:"repo"`
	Project         string    `json:"project" db:"project"`
	Environment     string    `json:"environment" db:"environment"`
	CommitSHA       string    `json:"commit_sha" db:"commit_sha"`
	AuthoredAt      time.Time `json:"authored_at" db:"authored_at"`
	FirstDeployedAt time.Time `json:"first_deployed_at" db:"first_deployed_at"`
	LeadTimeS       int64     `json:"lead_time_s" db:"lead_time_s"`
	IsMerge         bool      `json:"is_merge" db:"is_merge"`
	ServiceAccount  bool      `json:"service_account" db:"service_account"`
	Bot             bool      `json:"bot" db:"-"`
	// Read for the bot flag, never sent: no author names leave the API.
	DeclaredBot       bool   `json:"-" db:"declared_bot"`
	Author            string `json:"-" db:"author"`
	AuthorDisplayName string `json:"-" db:"author_display_name"`
}

// Run is one finished CI run.
type Run struct {
	Source       string     `json:"source" db:"source"`
	PipelineName string     `json:"pipeline_name" db:"pipeline_name"`
	Branch       string     `json:"branch" db:"-"`
	BranchKind   string     `json:"branch_kind" db:"-"`
	BuildType    *string    `json:"build_type" db:"build_type"`
	RunID        string     `json:"run_id" db:"run_id"`
	CommitSHA    *string    `json:"commit_sha" db:"commit_sha"`
	Status       *string    `json:"status" db:"status"`
	StartedAt    *time.Time `json:"started_at" db:"started_at"`
	FinishedAt   *time.Time `json:"finished_at" db:"finished_at"`
	DurationS    *int64     `json:"duration_s" db:"duration_s"`
	Repo         *string    `json:"repo" db:"repo"`
	Project      *string    `json:"project" db:"project"`
	ImageRefs    []string   `json:"image_refs" db:"image_refs"`
}

// Review is one PR's final noergler rollup. CostUSD is the stored decimal
// as text, so no float rounds it.
type Review struct {
	PRKey         string    `json:"pr_key" db:"pr_key"`
	Repo          *string   `json:"repo" db:"repo"`
	Project       *string   `json:"project" db:"project"`
	Outcome       *string   `json:"outcome" db:"outcome"`
	CostUSD       *string   `json:"cost_usd" db:"cost_usd"`
	LinesAdded    *int64    `json:"lines_added" db:"lines_added"`
	LinesRemoved  *int64    `json:"lines_removed" db:"lines_removed"`
	FilesChanged  *int64    `json:"files_changed" db:"files_changed"`
	FindingsCount *int64    `json:"findings_count" db:"findings_count"`
	TotalRuns     *int64    `json:"total_runs" db:"total_runs"`
	ClosedAt      time.Time `json:"closed_at" db:"closed_at"`
}

// DB is what Load needs: a pgxpool.Pool.
type DB interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Load reads the dataset in one read-only snapshot, so the sections agree
// with each other while webhooks keep landing.
func Load(ctx context.Context, db DB, p Params) (*Dataset, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The lead-time views' row estimates are off by orders of magnitude
	// (measured: 77 M estimated, 10 k actual), which makes the planner
	// JIT-compile a 250 ms query for another 600 ms.
	if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
		return nil, err
	}
	d := &Dataset{Meta: Meta{
		Team: p.Team, From: p.From.UTC(), To: p.To.UTC(), GeneratedAt: p.Now.UTC(),
		ProductionStage: p.ProductionStage,
	}}
	meta, err := one[metaRow](ctx, tx, "meta", metaSQL, p.Team)
	if err != nil {
		return nil, err
	}
	d.Meta.DataStart = SourceTimes{Bitbucket: meta.Bitbucket, Pipeline: meta.Pipeline, ArgoCD: meta.ArgoCD, Noergler: meta.Noergler}
	d.Meta.DataEnd = SourceTimes{Bitbucket: meta.BitbucketEnd, Pipeline: meta.PipelineEnd, ArgoCD: meta.ArgoCDEnd,
		Noergler: meta.NoerglerEnd}
	d.Meta.BuildDataFrom, d.Meta.BuildTypeFrom = meta.BuildDataFrom, meta.BuildTypeFrom

	if d.PRs, err = all[PR](ctx, tx, "prs", prsSQL, p.Team, p.From, p.To); err != nil {
		return nil, err
	}
	if d.Deploys, err = all[Deploy](ctx, tx, "deploys", deploysSQL, p.Team, p.From, p.To); err != nil {
		return nil, err
	}
	if d.LeadTimes, err = all[LeadTime](ctx, tx, "lead_times", leadTimesSQL, p.Team, p.From, p.To); err != nil {
		return nil, err
	}
	if d.Runs, err = all[Run](ctx, tx, "runs", runsSQL, p.Team, p.From, p.To); err != nil {
		return nil, err
	}
	if d.Reviews, err = all[Review](ctx, tx, "reviews", reviewsSQL, p.Team, p.From, p.To); err != nil {
		return nil, err
	}

	automated := func(login, displayName string) bool {
		return p.Automated != nil && p.Automated(login, displayName)
	}
	for i := range d.LeadTimes {
		l := &d.LeadTimes[i]
		l.Bot = l.DeclaredBot || automated(l.Author, l.AuthorDisplayName)
	}
	for i := range d.PRs {
		reviewed(&d.PRs[i], automated)
	}
	var merges []string
	for _, pr := range d.PRs {
		if pr.MergeCommit != nil {
			merges = append(merges, *pr.MergeCommit)
		}
	}
	shipped := []shippedCommit{}
	if len(merges) > 0 {
		if shipped, err = all[shippedCommit](ctx, tx, "environments", environmentsSQL, p.Team, merges); err != nil {
			return nil, err
		}
	}
	attachEnvironments(d.PRs, shipped)
	for i := range d.Runs {
		d.Runs[i].Branch = Branch(d.Runs[i].PipelineName)
		d.Runs[i].BranchKind = BranchKind(d.Runs[i].Branch)
	}
	d.Products = products(d)
	d.utc()
	return d, nil
}

// metaRow is sql/meta.sql's row: both SourceTimes flat, under their own
// column names.
type metaRow struct {
	Bitbucket     *time.Time `db:"bitbucket"`
	Pipeline      *time.Time `db:"pipeline"`
	ArgoCD        *time.Time `db:"argocd"`
	Noergler      *time.Time `db:"noergler"`
	BitbucketEnd  *time.Time `db:"bitbucket_end"`
	PipelineEnd   *time.Time `db:"pipeline_end"`
	ArgoCDEnd     *time.Time `db:"argocd_end"`
	NoerglerEnd   *time.Time `db:"noergler_end"`
	BuildDataFrom *time.Time `db:"build_data_from"`
	BuildTypeFrom *time.Time `db:"build_type_from"`
}

// utc puts every timestamp in UTC: pgx scans timestamptz in the process's
// local zone, and the API speaks UTC whatever the host is set to.
func (d *Dataset) utc() {
	ptrs := append(append(d.Meta.DataStart.ptrs(), d.Meta.DataEnd.ptrs()...), d.Meta.BuildDataFrom, d.Meta.BuildTypeFrom)
	for i := range d.PRs {
		p := &d.PRs[i]
		ptrs = append(ptrs, p.OpenedAt, p.ReadyAt, p.FirstReviewAt, p.ApprovedAt, p.MergedAt, p.FirstCommitAt)
		for j := range p.Environments {
			ptrs = append(ptrs, &p.Environments[j].FirstDeployedAt)
		}
	}
	for i := range d.Deploys {
		ptrs = append(ptrs, &d.Deploys[i].At)
	}
	for i := range d.LeadTimes {
		ptrs = append(ptrs, &d.LeadTimes[i].AuthoredAt, &d.LeadTimes[i].FirstDeployedAt)
	}
	for i := range d.Runs {
		ptrs = append(ptrs, d.Runs[i].StartedAt, d.Runs[i].FinishedAt)
	}
	for i := range d.Reviews {
		ptrs = append(ptrs, &d.Reviews[i].ClosedAt)
	}
	for _, t := range ptrs {
		if t != nil {
			*t = t.UTC()
		}
	}
}

// one reads a single row into T by column name.
func one[T any](ctx context.Context, tx pgx.Tx, name, sql string, args ...any) (T, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("dataset %s: %w", name, err)
	}
	v, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[T])
	if err != nil {
		return v, fmt.Errorf("dataset %s: %w", name, err)
	}
	return v, nil
}

// all reads every row into T by column name; never nil, so an empty
// section encodes as [].
func all[T any](ctx context.Context, tx pgx.Tx, name, sql string, args ...any) ([]T, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("dataset %s: %w", name, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowToStructByName[T])
	if err != nil {
		return nil, fmt.Errorf("dataset %s: %w", name, err)
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

// shippedCommit is a row of sql/environments.sql.
type shippedCommit struct {
	CommitSHA       string    `db:"commit_sha"`
	Environment     string    `db:"environment"`
	FirstDeployedAt time.Time `db:"first_deployed_at"`
}

// reviewed sets a PR's bot flag and review times from what prs.sql read:
// the opener and the reviewer touches go through the same automation
// detection as commit authors, so a bot added to the config counts as one
// on every row, and first review and approval count the same reviewers.
func reviewed(pr *PR, automated func(login, displayName string) bool) {
	pr.Bot = pr.Bot || automated(pr.Opener, pr.OpenerDisplayName)
	for _, t := range pr.Touches {
		if automated(t.Author, t.DisplayName) {
			continue
		}
		at := t.At
		if pr.FirstReviewAt == nil {
			pr.FirstReviewAt = &at
		}
		if t.Approved {
			pr.ApprovedAt = &at
			break
		}
	}
	pr.ApprovedOnFirstTouch = pr.ApprovedAt != nil && pr.ApprovedAt.Equal(*pr.FirstReviewAt)
}

// attachEnvironments gives every PR its merge commit's first deploy per
// environment. The README's breakdown query looks each one up in
// lead_time_changes; here the view is read once for all of them. Both SHAs
// arrive lowercased from SQL.
func attachEnvironments(prs []PR, shipped []shippedCommit) {
	byCommit := map[string][]EnvironmentDeploy{}
	for _, s := range shipped {
		byCommit[s.CommitSHA] = append(byCommit[s.CommitSHA], EnvironmentDeploy{Environment: s.Environment, FirstDeployedAt: s.FirstDeployedAt})
	}
	for i := range prs {
		envs := []EnvironmentDeploy{}
		if prs[i].MergeCommit != nil {
			envs = append(envs, byCommit[*prs[i].MergeCommit]...)
		}
		slices.SortFunc(envs, func(a, b EnvironmentDeploy) int {
			return cmp.Or(a.FirstDeployedAt.Compare(b.FirstDeployedAt), cmp.Compare(a.Environment, b.Environment))
		})
		prs[i].Environments = envs
	}
}

// products lists every project key the window's rows name, with the repos
// and Apps seen under it. An App's project is its GitOps repo's.
func products(d *Dataset) []Product {
	repos := map[string]map[string]bool{}
	apps := map[string]map[string]bool{}
	add := func(m map[string]map[string]bool, project, item string) {
		if project == "" || item == "" {
			return
		}
		if m[project] == nil {
			m[project] = map[string]bool{}
		}
		m[project][item] = true
	}
	for _, p := range d.PRs {
		add(repos, p.Project, p.Repo)
	}
	for _, l := range d.LeadTimes {
		add(repos, l.Project, l.Repo)
	}
	for _, r := range d.Runs {
		if r.Repo != nil && r.Project != nil {
			add(repos, *r.Project, *r.Repo)
		}
	}
	for _, r := range d.Reviews {
		if r.Repo != nil && r.Project != nil {
			add(repos, *r.Project, *r.Repo)
		}
	}
	for _, dep := range d.Deploys {
		if dep.Project != nil {
			add(apps, *dep.Project, dep.App)
		}
	}
	keys := map[string]bool{}
	for k := range repos {
		keys[k] = true
	}
	for k := range apps {
		keys[k] = true
	}
	out := []Product{}
	for k := range keys {
		out = append(out, Product{Key: k, Repos: sorted(repos[k]), Apps: sorted(apps[k])})
	}
	slices.SortFunc(out, func(a, b Product) int { return cmp.Compare(a.Key, b.Key) })
	return out
}

func sorted(set map[string]bool) []string {
	out := []string{}
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Branch is the branch a CI run built, read off its pipeline name: the last
// '/'-segment, as a Jenkins multibranch job names it (`<repo>/<branch>`,
// `<folder>/<repo>/<branch>`). Jenkins encodes a '/' inside the branch name
// as %2F, so the split comes first and the decoding after. Empty for a
// pipeline name without a '/'.
func Branch(pipelineName string) string {
	i := strings.LastIndex(pipelineName, "/")
	if i < 0 {
		return ""
	}
	return encodedSlash.ReplaceAllString(pipelineName[i+1:], "/")
}

var (
	encodedSlash = regexp.MustCompile(`(?i)%2f`)
	prBranch     = regexp.MustCompile(`(?i)^pr-\d+$`)
)

// Branch kinds.
const (
	BranchMain    = "main"
	BranchRelease = "release"
	BranchFeature = "feature"
	BranchPR      = "pr"
	BranchOther   = "other"
)

// BranchKind classifies a branch: main (master, main), release (release/*),
// feature (feature/*, bugfix/*, hotfix/*), pr (Jenkins' PR-<n>), other.
func BranchKind(branch string) string {
	b := strings.ToLower(branch)
	switch {
	case b == "master" || b == "main":
		return BranchMain
	case strings.HasPrefix(b, "release/"):
		return BranchRelease
	case strings.HasPrefix(b, "feature/"), strings.HasPrefix(b, "bugfix/"), strings.HasPrefix(b, "hotfix/"):
		return BranchFeature
	case prBranch.MatchString(b):
		return BranchPR
	}
	return BranchOther
}
