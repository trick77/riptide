// Package dataset reads the per-team facts package metrics computes every
// number from: one row per PR, deploy, shipped commit, CI run and noergler
// review in a window, never an aggregate. The rules are docs/metrics.md's
// queries, one embedded .sql file per section; product, GitOps and bot
// derivations come from the data, never from name lists. HTTP, parameters
// and config belong to package api.
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

	"github.com/trick77/riptide/internal/parse"
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
	//go:embed sql/open_prs.sql
	openPRsSQL string
	//go:embed sql/gitops.sql
	gitopsSQL string
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
	// Excluded is config's excluded_projects: rows of an excluded Bitbucket
	// project key are dropped, so neither a number nor the product list
	// sees them; a row without a project stays. nil excludes nothing.
	Excluded func(project string) bool
	// Now stamps meta.generated_at.
	Now time.Time
	// FactsOnly skips the lead times and the reviews: the facts tables read
	// none, and a search reads the dataset again on every pause. Products
	// then miss what only those two show.
	FactsOnly bool
}

// Dataset is one team's facts for a window.
type Dataset struct {
	Meta      Meta       `json:"meta"`
	Products  []Product  `json:"products"`
	PRs       []PR       `json:"prs"`
	Deploys   []Deploy   `json:"deploys"`
	LeadTimes []LeadTime `json:"lead_times"`
	Runs      []Run      `json:"runs"`
	Reviews   []Review   `json:"reviews"`
	// OpenPRs are the PRs open at the window's end, whenever opened.
	OpenPRs []OpenPR `json:"open_prs"`
	// GitOpsRepos are every GitOps repo (sql/gitops.sql), not the window's
	// only: a run whose commit Bitbucket never saw is matched by name.
	GitOpsRepos []string `json:"-"`
}

// Meta describes the window and where each source's data starts.
type Meta struct {
	Team            string    `json:"team"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	GeneratedAt     time.Time `json:"generated_at"`
	ProductionStage string    `json:"production_stage"`
	// The team's first event per source over all history, and its latest:
	// before To for a past window, so it sees a source gone quiet within it.
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
	Repo    string  `json:"repo" db:"repo"`
	Project string  `json:"project" db:"project"`
	PRID    int64   `json:"pr_id" db:"pr_id"`
	Title   *string `json:"title" db:"title"`
	// URL is the PR's page from its latest event's links.self, unchecked.
	URL        *string  `json:"url" db:"url"`
	GitOps     bool     `json:"gitops" db:"gitops"`
	Bot        bool     `json:"bot" db:"bot"`
	ChangeType *string  `json:"change_type" db:"change_type"`
	JiraKeys   []string `json:"jira_keys" db:"jira_keys"`
	State      string   `json:"state" db:"state"`
	// ClosedAt is when a declined or deleted PR closed; nil otherwise.
	ClosedAt *time.Time `json:"closed_at" db:"closed_at"`
	OpenedAt *time.Time `json:"opened_at" db:"opened_at"`
	ReadyAt  *time.Time `json:"ready_at" db:"ready_at"`
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

// OpenPR is a pull request open at the window's end. See sql/open_prs.sql
// for the rules.
type OpenPR struct {
	Repo    string  `json:"repo" db:"repo"`
	Project string  `json:"project" db:"project"`
	PRID    int64   `json:"pr_id" db:"pr_id"`
	Title   *string `json:"title" db:"title"`
	// URL as on PR.
	URL        *string   `json:"url" db:"url"`
	Bot        bool      `json:"bot" db:"bot"`
	ChangeType *string   `json:"change_type" db:"change_type"`
	JiraKeys   []string  `json:"jira_keys" db:"jira_keys"`
	OpenedAt   time.Time `json:"opened_at" db:"opened_at"`
	// LastEventAt is its latest event before the window's end, any kind,
	// or its creation.
	LastEventAt time.Time `json:"last_event_at" db:"last_event_at"`
	// ReadyAt is the pickup clock start; nil for a draft never made ready.
	ReadyAt *time.Time `json:"ready_at" db:"ready_at"`
	Draft   bool       `json:"draft" db:"draft"`
	// Touched: a reviewer touched it after ready, as FirstReviewAt counts
	// reviewers. NeedsWork and Approved are the reviewers' and other
	// participants' status as the PR's latest event carries it, automation
	// left out.
	Touched   bool `json:"touched" db:"-"`
	NeedsWork bool `json:"needs_work" db:"-"`
	Approved  bool `json:"approved" db:"-"`

	// Read for the derivations above, never sent: no handles leave the API.
	Touches           []touch          `json:"-" db:"touches"`
	Reviewers         []reviewerStatus `json:"-" db:"reviewers"`
	Opener            string           `json:"-" db:"opener"`
	OpenerDisplayName string           `json:"-" db:"opener_display_name"`
}

// reviewerStatus is a reviewer as a PR event carries it.
type reviewerStatus struct {
	Author      string `json:"author"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
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
	App         string  `json:"app" db:"app"`
	Project     *string `json:"project" db:"project"`
	Environment *string `json:"environment" db:"environment"`
	Revision    string  `json:"revision" db:"revision"`
	// RevisionFirstAt is the revision's first successful sync to this
	// environment, over all history; nil when it never succeeded.
	RevisionFirstAt *time.Time `json:"revision_first_at" db:"revision_first_at"`
	At              time.Time  `json:"at" db:"at"`
	Phase           string     `json:"phase" db:"phase"`
	DurationS       *int64     `json:"duration_s" db:"duration_s"`
	Images          []string   `json:"images" db:"images"`
	Kind            *string    `json:"kind" db:"kind"`
	// NewImages are the images a release brought, those the previous
	// deploy did not run; empty for anything but a release.
	NewImages []string `json:"new_images" db:"new_images"`
}

// LeadTime is one commit's first deploy to one environment.
type LeadTime struct {
	Repo           string `json:"repo" db:"repo"`
	Project        string `json:"project" db:"project"`
	Environment    string `json:"environment" db:"environment"`
	LeadTimeS      int64  `json:"lead_time_s" db:"lead_time_s"`
	IsMerge        bool   `json:"is_merge" db:"is_merge"`
	ServiceAccount bool   `json:"service_account" db:"service_account"`
	Bot            bool   `json:"bot" db:"-"`
	// Read for the bot flag, never sent: no author names leave the API.
	DeclaredBot       bool   `json:"-" db:"declared_bot"`
	Author            string `json:"-" db:"author"`
	AuthorDisplayName string `json:"-" db:"author_display_name"`
}

// Run is one finished CI run.
type Run struct {
	Source       string  `json:"source" db:"source"`
	PipelineName string  `json:"pipeline_name" db:"pipeline_name"`
	Branch       string  `json:"branch" db:"-"`
	BranchKind   string  `json:"branch_kind" db:"-"`
	BuildType    *string `json:"build_type" db:"build_type"`
	RunID        string  `json:"run_id" db:"run_id"`
	CommitSHA    *string `json:"commit_sha" db:"commit_sha"`
	// HeadSHA is a PR build's own head when CommitSHA is a merge the CI made
	// of it with the target branch, BaseSHA the target's tip it was merged
	// onto; nil otherwise.
	HeadSHA   *string    `json:"head_sha" db:"head_sha"`
	BaseSHA   *string    `json:"base_sha" db:"base_sha"`
	Status    *string    `json:"status" db:"status"`
	StartedAt *time.Time `json:"started_at" db:"started_at"`
	// AgentStartedAt is when the run got its build agent, as the CI
	// reported it; nil when it did not.
	AgentStartedAt *time.Time `json:"agent_started_at" db:"agent_started_at"`
	FinishedAt     *time.Time `json:"finished_at" db:"finished_at"`
	// OccurredAt is when the COMPLETED event arrived: the instant the
	// windows split runs on, so a run counts in one window only.
	OccurredAt time.Time `json:"occurred_at" db:"occurred_at"`
	DurationS  *int64    `json:"duration_s" db:"duration_s"`
	Repo       *string   `json:"repo" db:"repo"`
	Project    *string   `json:"project" db:"project"`
	ImageRefs  []string  `json:"image_refs" db:"image_refs"`
	// GitOps: a run in a GitOps repo, a stage promotion's check.
	GitOps bool `json:"gitops" db:"gitops"`
}

// Review is one PR's final noergler rollup. CostUSD is the stored decimal
// as text, so no float rounds it.
type Review struct {
	Repo         *string `json:"repo" db:"repo"`
	Project      *string `json:"project" db:"project"`
	GitOps       bool    `json:"gitops" db:"gitops"`
	Outcome      *string `json:"outcome" db:"outcome"`
	CostUSD      *string `json:"cost_usd" db:"cost_usd"`
	LinesAdded   *int64  `json:"lines_added" db:"lines_added"`
	LinesRemoved *int64  `json:"lines_removed" db:"lines_removed"`
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
	// A past window's sources end at its end; one ending now reads them all.
	var end *time.Time
	if p.To.Before(p.Now) {
		end = &p.To
	}
	meta, err := one[metaRow](ctx, tx, "meta", metaSQL, p.Team, end)
	if err != nil {
		return nil, err
	}
	d.Meta.DataStart = SourceTimes{Bitbucket: meta.Bitbucket, Pipeline: meta.Pipeline, ArgoCD: meta.ArgoCD, Noergler: meta.Noergler}
	d.Meta.DataEnd = SourceTimes{Bitbucket: meta.BitbucketEnd, Pipeline: meta.PipelineEnd, ArgoCD: meta.ArgoCDEnd,
		Noergler: meta.NoerglerEnd}
	d.Meta.BuildDataFrom, d.Meta.BuildTypeFrom = meta.BuildDataFrom, meta.BuildTypeFrom

	// Never nil: pgx sends nil as NULL, `= ANY(NULL)` scans NULL into a
	// bool and `<> ALL(NULL)` drops every open PR.
	var gitops []string
	if err := tx.QueryRow(ctx, gitopsSQL).Scan(&gitops); err != nil {
		return nil, fmt.Errorf("dataset gitops: %w", err)
	}
	if gitops == nil {
		gitops = []string{}
	}
	d.GitOpsRepos = gitops
	if d.PRs, err = all[PR](ctx, tx, "prs", prsSQL, p.Team, p.From, p.To, d.GitOpsRepos); err != nil {
		return nil, err
	}
	if d.OpenPRs, err = all[OpenPR](ctx, tx, "open_prs", openPRsSQL, p.Team, p.To, d.GitOpsRepos); err != nil {
		return nil, err
	}
	if d.Deploys, err = all[Deploy](ctx, tx, "deploys", deploysSQL, p.Team, p.From, p.To); err != nil {
		return nil, err
	}
	if d.Runs, err = all[Run](ctx, tx, "runs", runsSQL, p.Team, p.From, p.To, d.GitOpsRepos); err != nil {
		return nil, err
	}
	d.LeadTimes, d.Reviews = []LeadTime{}, []Review{}
	if !p.FactsOnly {
		if d.LeadTimes, err = all[LeadTime](ctx, tx, "lead_times", leadTimesSQL, p.Team, p.From, p.To); err != nil {
			return nil, err
		}
		if d.Reviews, err = all[Review](ctx, tx, "reviews", reviewsSQL, p.Team, p.From, p.To, d.GitOpsRepos); err != nil {
			return nil, err
		}
	}

	runRepos(d)
	exclude(d, p.Excluded)

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
	for i := range d.OpenPRs {
		reviewedOpen(&d.OpenPRs[i], automated)
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

// runRepos gives a run without a repository the one other runs of the same
// pipeline found in Bitbucket: a run that failed before its checkout has no
// commit to look it up by, and the project exclusion, the product filter and
// the GitOps flag would otherwise never see it (an excluded project's agent
// failures would count as the team's). A pipeline name two repositories
// share stays without.
func runRepos(d *Dataset) {
	type found struct{ repo, project *string }
	by, ambiguous := map[string]found{}, map[string]bool{}
	key := func(r Run) string { return r.Source + "#" + r.PipelineName }
	for _, r := range d.Runs {
		if r.Repo == nil {
			continue
		}
		if prev, ok := by[key(r)]; ok && *prev.repo != *r.Repo {
			ambiguous[key(r)] = true
		}
		by[key(r)] = found{r.Repo, r.Project}
	}
	for i := range d.Runs {
		r := &d.Runs[i]
		if f, ok := by[key(*r)]; ok && r.Repo == nil && !ambiguous[key(*r)] {
			r.Repo, r.Project = f.repo, f.project
			r.GitOps = slices.Contains(d.GitOpsRepos, *f.repo)
		}
	}
}

// exclude drops the rows of excluded projects before anything is derived
// from them: a PR's environments, the product list. Meta keeps the team's
// whole data range, which is when sources reported, not a metric.
func exclude(d *Dataset, excluded func(string) bool) {
	if excluded == nil {
		return
	}
	out := func(project *string) bool { return project != nil && excluded(*project) }
	d.PRs = slices.DeleteFunc(d.PRs, func(x PR) bool { return excluded(x.Project) })
	d.OpenPRs = slices.DeleteFunc(d.OpenPRs, func(x OpenPR) bool { return excluded(x.Project) })
	d.Deploys = slices.DeleteFunc(d.Deploys, func(x Deploy) bool { return out(x.Project) })
	d.Runs = slices.DeleteFunc(d.Runs, func(x Run) bool { return out(x.Project) })
	d.LeadTimes = slices.DeleteFunc(d.LeadTimes, func(x LeadTime) bool { return excluded(x.Project) })
	d.Reviews = slices.DeleteFunc(d.Reviews, func(x Review) bool { return out(x.Project) })
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
		ptrs = append(ptrs, p.ClosedAt, p.OpenedAt, p.ReadyAt, p.FirstReviewAt, p.ApprovedAt, p.MergedAt, p.FirstCommitAt)
		for j := range p.Environments {
			ptrs = append(ptrs, &p.Environments[j].FirstDeployedAt)
		}
	}
	for i := range d.OpenPRs {
		o := &d.OpenPRs[i]
		ptrs = append(ptrs, &o.OpenedAt, &o.LastEventAt, o.ReadyAt)
	}
	for i := range d.Deploys {
		ptrs = append(ptrs, &d.Deploys[i].At, d.Deploys[i].RevisionFirstAt)
	}
	for i := range d.Runs {
		ptrs = append(ptrs, d.Runs[i].StartedAt, d.Runs[i].AgentStartedAt, d.Runs[i].FinishedAt, &d.Runs[i].OccurredAt)
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

// reviewedOpen is reviewed for an open PR: the opener and every reviewer
// through the same automation detection, so a bot's approval (an AI
// reviewer approves too) never reads as a person's. A touch counts as in
// prs.sql: not the opener's, from ready on.
func reviewedOpen(pr *OpenPR, automated func(login, displayName string) bool) {
	pr.Bot = pr.Bot || automated(pr.Opener, pr.OpenerDisplayName)
	pr.Touched = pr.ReadyAt != nil && slices.ContainsFunc(pr.Touches, func(t touch) bool {
		return !strings.EqualFold(t.Author, pr.Opener) && !t.At.Before(*pr.ReadyAt) && !automated(t.Author, t.DisplayName)
	})
	for _, r := range pr.Reviewers {
		if automated(r.Author, r.DisplayName) {
			continue
		}
		switch r.Status {
		case "NEEDS_WORK":
			pr.NeedsWork = true
		case "APPROVED":
			pr.Approved = true
		}
	}
}

// attachEnvironments gives every PR its merge commit's first deploy per
// environment. docs/metrics.md's breakdown query looks each one up in
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
	for _, p := range d.OpenPRs {
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
	BranchFeature = "feature"
	BranchPR      = "pr"
	BranchOther   = "other"
)

// BranchKind classifies a branch: main (master, main), feature (every prefix
// the work mix knows but docs: feature/, feat/, bugfix/, fix/, hotfix/,
// chore/, refactor/), pr (Jenkins' PR-<n>), other (release/* included: a
// release build is told by its build_type, not its branch). A feature build
// is what a developer waits on: a branch scheme missing here hides those
// runs in other. The prefixes are the work mix's (parse.FeatureBranch), one
// list for both; only here a prefix needs its '/'. docs/ and doc/ are other
// (parse.DocsBranch says why). A PR-<n> build of a docs branch stays pr: its
// name does not tell the source branch.
func BranchKind(branch string) string {
	b := strings.ToLower(branch)
	switch {
	case b == "master" || b == "main":
		return BranchMain
	case parse.FeatureBranch(b):
		return BranchFeature
	case prBranch.MatchString(b):
		return BranchPR
	}
	return BranchOther
}
