// Package dataset reads the per-team facts package metrics computes every
// number from: one row per PR, deploy, CI run and noergler review in a
// window, each PR placed on the deploys that shipped it (trace.go), never an
// aggregate. The rules are docs/metrics.md's
// queries, one embedded .sql file per section; product, GitOps and bot
// derivations come from the data, never from name lists. HTTP, parameters
// and config belong to package api.
package dataset

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/trick77/riptide/internal/parse"
)

//go:embed sql/*.sql sql/fragments/*.sql
var sqlFiles embed.FS

// queries are sql/*.sql by file name, fragments written in.
var queries = expand()

var (
	metaSQL       = query("meta.sql")
	prsSQL        = query("prs.sql")
	deploysSQL    = query("deploys.sql")
	runsSQL       = query("runs.sql")
	reviewsSQL    = query("reviews.sql")
	openPRsSQL    = query("open_prs.sql")
	gitopsSQL     = query("gitops.sql")
	silencesSQL   = query("silences.sql")
	reposSQL      = query("repos.sql")
	repoStartsSQL = query("repo_starts.sql")
	appsSQL       = query("apps.sql")
	librariesSQL  = query("libraries.sql")
	approvalsSQL  = query("approvals.sql")
	branchesSQL   = query("branches.sql")

	traceDeploysSQL   = query("trace_deploys.sql")
	traceReleasesSQL  = query("trace_releases.sql")
	traceRunsSQL      = query("trace_runs.sql")
	tracePositionsSQL = query("trace_positions.sql")
)

const (
	// FirstDeployGrace is how long Argo CD must have reported for the team
	// before an App's first deploy naming images counts as a release. Right
	// after onboarding every App's first deploy is one, release or re-sync
	// unknown; later an App deployed for the first time is a new App going
	// live (owner's decision, 2026-11).
	FirstDeployGrace = 14 * 24 * time.Hour
	// SilenceMin is the working time (Monday to Friday, UTC) a source must
	// store nothing for the team, while another source stores events, to be
	// silent: a weekend without a push is none.
	SilenceMin = 48 * time.Hour
	// SilenceLookback is how far before the window's end silences are
	// looked for, the window aside: an open PR's close lost in a silence
	// weeks ago is still lost. metrics.OpenInactiveDays: past it the PR is
	// inactive anyway.
	SilenceLookback = 30 * 24 * time.Hour
	// HousekeepingWindow and HousekeepingMinRepos tell a housekeeping PR: a
	// tool's change, the same title opened in HousekeepingMinRepos of the
	// team's repos within HousekeepingWindow (one config file bumped in
	// every repo at once), no person's approval, no ticket key. Not merged
	// work (owner's decision, 2026-11). How long it stood open says nothing:
	// some of one batch are merged at once, others days later.
	HousekeepingWindow = time.Hour
	// HousekeepingMinRepos is the number of repos the same title must be
	// opened in within HousekeepingWindow.
	HousekeepingMinRepos = 3
	// ImplicitMergeWindow is how close together PRs of one repo must merge,
	// at most one of them with a merge commit, to count as merged by that one.
	// Bitbucket marks every PR whose commits an umbrella branch brought in
	// merged in the umbrella's second, without a merge commit of its own.
	ImplicitMergeWindow = time.Second
	// AppHistory is how far back an App's deploys decide its product and
	// whether it is infrastructure (sql/apps.sql).
	AppHistory = 26 * 7 * 24 * time.Hour
	// LostCommitReach is how long before an App went live a lost Bitbucket
	// silence may have ended and still hold the GitOps commit it went live
	// with: a production sync waits for its commit up to a week, a hole
	// ending on Friday holds Monday's deploy.
	LostCommitReach = 7 * 24 * time.Hour
)

func query(name string) string {
	q, ok := queries[name]
	if !ok {
		panic("dataset: no sql/" + name)
	}
	return q
}

// fragments are sql/fragments/*.sql by name, without .sql and the trailing
// newline.
func fragments() map[string]string {
	out := map[string]string{}
	for file, body := range readSQL("sql/fragments/*.sql") {
		out[strings.TrimSuffix(file, ".sql")] = strings.TrimSuffix(body, "\n")
	}
	return out
}

func placeholder(fragment string) string { return "/*{{" + fragment + "}}*/" }

// expand writes each fragment into every query where it says
// /*{{<name>}}*/: plain text, so the query reads the same as if typed out.
// A fragment holds no placeholder (no nesting: the order fragments are
// written in is a map's) and no -- comment (one would swallow the rest of
// the line it is written into); comments go at the call site.
func expand() map[string]string {
	out := readSQL("sql/*.sql")
	for name, body := range fragments() {
		for q := range out {
			out[q] = strings.ReplaceAll(out[q], placeholder(name), body)
		}
	}
	return out
}

// readSQL reads the embedded files matching pattern, by file name.
func readSQL(pattern string) map[string]string {
	names, err := fs.Glob(sqlFiles, pattern)
	if err != nil {
		panic(err)
	}
	out := map[string]string{}
	for _, n := range names {
		body, err := sqlFiles.ReadFile(n)
		if err != nil {
			panic(err)
		}
		out[path.Base(n)] = string(body)
	}
	return out
}

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
	// BotBranch is config's automation branch_prefixes: a CI run of such a
	// branch (lowercased) is a bot's (Run.Bot). nil knows none.
	BotBranch func(branch string) bool
	// Now stamps meta.generated_at.
	Now time.Time
	// FactsOnly skips the reviews: the facts tables read none, and a
	// search reads the dataset again on every pause. Products then miss
	// what only those show.
	FactsOnly bool
	// CountsOnly skips the tracing and the library repos (PR.Environments
	// empty, PR.Library false, no Traces): the running week's read counts
	// its merged PRs, releases and runs, nothing traced.
	CountsOnly bool
	// Shared are the reads every window of one page shares (LoadShared),
	// read once for all of them; nil reads them for this window alone.
	Shared *Shared
	// Lean reads the PRs and deploys only, no open PRs, runs or reviews: a
	// window before the compared periods that the assessment's states look
	// back into (merged work still before production, a project's typical
	// week of merges).
	Lean bool
	// ClassAt is the end of the window whose Apps class this one's deploys
	// (Shared's, App): a page classes its period and the previous one by the
	// period's end, so an App is no infrastructure in one and a product's in
	// the other. Zero: To.
	ClassAt time.Time
}

// Dataset is one team's facts for a window.
type Dataset struct {
	Meta     Meta      `json:"meta"`
	Products []Product `json:"products"`
	PRs      []PR      `json:"prs"`
	Deploys  []Deploy  `json:"deploys"`
	Runs     []Run     `json:"runs"`
	Reviews  []Review  `json:"reviews"`
	// Traces are tracing's knowledge of each repo the window's PRs are in
	// (RepoTrace): whether an App deploys it, and its Apps' releases. What
	// tells a PR not on production yet from one tracing cannot place.
	Traces map[string]RepoTrace `json:"-"`
	// OpenPRs are the PRs open at the window's end, whenever opened.
	OpenPRs []OpenPR `json:"open_prs"`
	// GitOpsRepos are every GitOps repo (sql/gitops.sql), not the window's
	// only: a run whose commit Bitbucket never saw is matched by name.
	GitOpsRepos []string `json:"-"`
	// Apps are the team's Apps as of Params.ClassAt (sql/apps.sql): the
	// classes its deploys carry. A page classes its period and the previous
	// one by the period's end (Classified), so an App is no infrastructure
	// in one and a product's in the other.
	Apps []App `json:"-"`
	// Releases are every App's releases per environment over all history
	// (AppRelease), oldest first, whatever the window: since when a project
	// has not released. Empty for a read without tracing (CountsOnly).
	Releases []AppRelease `json:"-"`
	// Approvals are the PRs approved since SharedParams.From less
	// ApprovalLookback, whenever opened or merged (sql/approvals.sql);
	// empty unless the shared read was asked for them (SharedParams.States).
	Approvals []Approval `json:"-"`
	// Branches are every branch pushed to since SharedParams.From
	// (sql/branches.sql); empty unless SharedParams.States.
	Branches []BranchLife `json:"-"`
}

// BranchLife is a branch as its pushes show it (sql/branches.sql): when it
// was first and last pushed, whether a PR came from it, whether it is a
// PR's target. Its deletion is never stored.
type BranchLife struct {
	Repo    string    `db:"repo"`
	Project string    `db:"project"`
	Branch  string    `db:"branch"`
	FirstAt time.Time `db:"first_at"`
	LastAt  time.Time `db:"last_at"`
	Pushes  int64     `db:"pushes"`
	// Bot: a bot's branch, by riptide's automation detection on its last
	// pusher or its head commit's git identity, or Tool. Bots' pushes
	// (the ingest flag) are not read at all.
	Bot bool `db:"-"`
	// Tool: every push's head commit authored by a git identity of the
	// .invalid e-mail domain, a tool's (the housekeeping commits' author).
	Tool              bool   `db:"tool"`
	Author            string `db:"author"`
	AuthorDisplayName string `db:"author_display_name"`
	HeadName          string `db:"head_name"`
	HeadDisplayName   string `db:"head_display_name"`
	HasPR             bool   `db:"has_pr"`
	Target            bool   `db:"target"`
}

// ApprovalLookback is how far before the earliest window an approval is
// read: a PR approved then and still waiting into the window counts; one
// waiting longer is as inactive as an open PR (metrics.OpenInactiveDays).
const ApprovalLookback = 30 * 24 * time.Hour

// Approval is a PR with a person's approval (sql/approvals.sql): its author,
// merge or close, and its first approval by someone else, a person.
type Approval struct {
	Repo    string `db:"repo"`
	Project string `db:"project"`
	PRID    int64  `db:"pr_id"`
	// Bot: a bot's PR (ingest flag, a declared non-human, or riptide's
	// automation detection on its author), out of every state.
	Bot               bool       `db:"bot"`
	Opener            string     `db:"opener"`
	OpenerDisplayName string     `db:"opener_display_name"`
	MergedAt          *time.Time `db:"merged_at"`
	ClosedAt          *time.Time `db:"closed_at"`
	// ApprovedAt is the first approval by a person other than the author.
	ApprovedAt *time.Time  `db:"-"`
	Approvals  []approving `db:"approvals"`
}

// approving is one approval event of sql/approvals.sql.
type approving struct {
	At          time.Time `json:"at"`
	Author      string    `json:"author"`
	DisplayName string    `json:"display_name"`
	Bot         bool      `json:"bot"`
}

// approved sets each approval's bot flag and first person's approval, the
// author's own aside, by the same detection as every other row.
func approved(as []Approval, automated func(login, displayName string) bool) {
	for i := range as {
		a := &as[i]
		a.Bot = a.Bot || automated(a.Opener, a.OpenerDisplayName)
		for _, e := range a.Approvals {
			if e.Bot || strings.EqualFold(e.Author, a.Opener) || automated(e.Author, e.DisplayName) {
				continue
			}
			a.ApprovedAt = ptr(e.At.UTC())
			break
		}
	}
}

// App is one of the team's Apps as its deploys over AppHistory show it.
type App struct {
	Name string
	// Project is its product: the one most of its deploys' revisions name.
	// A deploy whose GitOps commit Bitbucket's events missed is still its.
	Project *string
	// Infra: none of its deploys names a product, though Bitbucket reported
	// before one of them and the team has deploys with one. Never an App
	// that went live as a release (Deploy.FirstRelease) while Bitbucket's
	// events were lost, or within LostCommitReach after: its GitOps commit
	// may lie in the hole. A team's second GitOps repo without the Bitbucket
	// webhook classes every App of it infrastructure (docs/metrics.md).
	Infra bool
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
	// Silences are the spans in which one source stored nothing for the
	// team while another did, SilenceMin of working time or more in all,
	// that overlap the window, oldest first. From and To are when riptide
	// stored the source's events around the span, so one reaching over the
	// window's start or end starts before From or ends after To (up to the
	// read's now). One still running at the read ends at its now (Ongoing).
	// DataStart says when a source started: no silence before it.
	Silences []Silence `json:"silences"`
	// Lost are the Silences whose events were lost (Silence.Lost): the ones
	// the page's data line names. The rest are a team that did not deploy or
	// review for two days as much as a hole.
	Lost []Silence `json:"lost"`
	// RepoStarts are noergler's first event per repository of the window's
	// PRs, by repository: it is set up repo by repo. Empty for a facts-only
	// read.
	RepoStarts []RepoStart `json:"repo_starts"`
}

// Sources, as Silence names them: SourceTimes' JSON names.
const (
	SourceBitbucket = "bitbucket"
	SourcePipeline  = "pipeline"
	SourceArgoCD    = "argocd"
	SourceNoergler  = "noergler"
)

// Silence is a span without an event of one source. A literal reading of the
// data: a team whose builds or deploys pause for two working days while its
// PRs go on shows a silence of that source too, which only Bitbucket's
// UnseenRuns tells apart from lost events.
type Silence struct {
	Source string    `json:"source"`
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	// UnseenRuns, Bitbucket's only: the CI runs in the span that built a
	// commit Bitbucket never saw, of a pipeline whose other commits it did.
	// More than 0: Bitbucket's events were lost, not the team idle.
	UnseenRuns int `json:"unseen_runs"`
	// Lost: Bitbucket's, with unseen runs. Its events were lost: what it
	// counts reads low, a PR closed in it may still read open. Every other
	// silence is a fact for the data line, no hole in the data.
	Lost bool `json:"lost"`
	// Ongoing: the source has stored nothing since From up to the read's
	// now, To: still silent, while another source reports.
	Ongoing bool `json:"ongoing"`
}

// RepoStart is when noergler first reported a repository; nil never.
type RepoStart struct {
	Repo     string     `json:"repo"`
	Noergler *time.Time `json:"noergler"`
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
	// TrunkAt is when its work reached the trunk (sql/prs.sql): its merge
	// commit's first push, its umbrella's, or, merged when an umbrella's
	// merge brought it in (ImplicitMerge), the umbrella's; nil when nothing
	// tells (no merge commit, or merged into a branch no PR went on from).
	// TrunkBranch is the trunk branch, nil when the payload names none.
	TrunkAt     *time.Time `json:"trunk_at" db:"trunk_at"`
	TrunkBranch *string    `json:"trunk_branch" db:"trunk_branch"`
	// Environments are its first deploy per environment whose stretch holds
	// TrunkAt (trace.go), earliest first, with no window end: a closed
	// period reads only those before its end. A list, not intg/prod fields:
	// the stages are whatever namespaces the team deploys to, and only
	// production_stage is configured.
	Environments []EnvironmentDeploy `json:"environments" db:"-"`
	// LinesAdded, LinesRemoved and FilesChanged are the merged PR's size as
	// noergler's final rollup counts it; nil without one.
	LinesAdded   *int64 `json:"lines_added" db:"lines_added"`
	LinesRemoved *int64 `json:"lines_removed" db:"lines_removed"`
	FilesChanged *int64 `json:"files_changed" db:"files_changed"`
	// TargetBranch is the branch it was merged (or opened) into, lowercased;
	// nil when the payload names none.
	TargetBranch *string `json:"target_branch" db:"target_branch"`
	// OffTrunk: merged into another branch than the repo's trunk (master,
	// main, or the branch most of its PRs go into; several when tied): a
	// feature or umbrella branch. Merged work all the same (a PR is a PR,
	// wherever it goes: owner, 2026-11); the facts say so.
	OffTrunk bool `json:"off_trunk" db:"off_trunk"`
	// ImplicitMerge: merged without a merge commit of its own in the same
	// second as other PRs of its repo, at most one of them with one
	// (ImplicitMergeWindow): Bitbucket marked it merged when an umbrella
	// branch brought its commits in. Merged work all the same.
	ImplicitMerge bool `json:"implicit_merge" db:"-"`
	// Umbrella: the vehicle of other people's merged PRs, none of its own
	// work: its source branch is where people merged other PRs of the repo,
	// or its merge brought in another person's PR (ImplicitMerge). Never a
	// bot's PR beside it: a bot's PR never makes a person's uncounted.
	Umbrella bool `json:"umbrella" db:"-"`
	// Twice: merged implicitly (ImplicitMerge) from a source branch another
	// merged PR of the repo, counted as work, came from: the same work,
	// counted once.
	Twice bool `json:"twice" db:"-"`
	// Housekeeping: a person's PR, its title opened in HousekeepingMinRepos
	// of the team's repos within HousekeepingWindow, without a person's
	// approval and without a ticket key in title or source branch. Decided
	// over the team's PR openings of all history, whatever window and
	// product a view shows.
	Housekeeping bool `json:"housekeeping" db:"-"`
	// Library: in a repo that is never rolled out (libraryEvidence), none of
	// whose PRs in the window reached a stage: merged work no deploy
	// carries, so no PR not traced to production.
	Library bool `json:"library" db:"-"`

	// Read for the derivations above, never sent: no handles leave the API.
	Touches           []touch `json:"-" db:"touches"`
	Opener            string  `json:"-" db:"opener"`
	OpenerDisplayName string  `json:"-" db:"opener_display_name"`
	// SourceRef is the source branch as the sender spelled it.
	SourceRef string `json:"-" db:"source_ref"`
	// UmbrellaOpeners open the other merged PRs into its source branch.
	UmbrellaOpeners []opening `json:"-" db:"umbrella_openers"`
	// Siblings are its title's openings within HousekeepingWindow of its own,
	// itself included; empty unless merged and opened with a title.
	Siblings []opening `json:"-" db:"siblings"`
	// OpenedTitle is its title as opened.
	OpenedTitle *string `json:"-" db:"opened_title"`
}

// opening is a PR as sql/prs.sql names another one: its repo, its opening
// and who opened it. Bot is the ingest flag or a declared non-human, a GitOps
// repo's PR among them (no person's work); Go adds its automation detection.
type opening struct {
	Repo        string    `json:"repo"`
	At          time.Time `json:"at"`
	Author      string    `json:"author"`
	DisplayName string    `json:"display_name"`
	Bot         bool      `json:"bot"`
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
	// ReadySince is its last turn ready, the age clock start: time as a
	// draft is its author's (user, 2026-11-20). Nil for a draft.
	ReadySince *time.Time `json:"ready_since" db:"ready_since"`
	Draft      bool       `json:"draft" db:"draft"`
	// Touched: a reviewer touched it after ready, as FirstReviewAt counts
	// reviewers. NeedsWork and Approved are the reviewers' and other
	// participants' status as the PR's latest event carries it, automation
	// left out.
	Touched   bool `json:"touched" db:"-"`
	NeedsWork bool `json:"needs_work" db:"-"`
	Approved  bool `json:"approved" db:"-"`
	// Unknown: its latest event came before a Bitbucket silence with
	// UnseenRuns, nothing since, so it may have been merged or declined
	// while Bitbucket's events were lost. Not open, not closed: noergler's
	// rollup never decides a PR's state (AGENTS.md).
	Unknown bool `json:"unknown" db:"-"`

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

// touch is a reviewer's event on a PR, as sql/fragments/touches.sql builds it.
type touch struct {
	At          time.Time `json:"at"`
	Approved    bool      `json:"approved"`
	Author      string    `json:"author"`
	DisplayName string    `json:"display_name"`
}

// EnvironmentDeploy is a PR's first deploy to one environment: the route
// its stretch was found by (RouteCompare, RouteImage), and whether that
// stretch's start was a lower bound (Stretch.LowerBound: the PR may have
// gone out earlier, with a deploy tracing could not resolve).
type EnvironmentDeploy struct {
	Environment     string    `json:"environment"`
	FirstDeployedAt time.Time `json:"first_deployed_at"`
	Route           string    `json:"route"`
	LowerBound      bool      `json:"lower_bound"`
}

// Deploy is one finished Argo CD sync. Kind is nil on a failed sync
// (Phase Failed or Error).
type Deploy struct {
	// ID is the Argo CD event's: what tracing's stretches name.
	ID          int64   `json:"-" db:"id"`
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
	// deploy naming images did not run; empty for anything but a release.
	NewImages []string `json:"new_images" db:"new_images"`
	// FirstRelease: a first_seen more than FirstDeployGrace after the team's
	// first Argo CD event, counted as a release: an App going live, not the
	// onboarding's first look at an App deployed long before.
	FirstRelease bool `json:"first_release" db:"-"`
	// Infra: its App is infrastructure (App.Infra): a log shipper, a bot,
	// no product of the team's. In no judged number (owner's decision,
	// 2026-11). Project, where the revision names none, is the App's.
	Infra bool `json:"infra" db:"-"`
	// Traced: tracing resolved a stretch of some repo for it (trace.go),
	// maybe empty; a release without one shipped PRs no one can name.
	Traced bool `json:"-" db:"-"`
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
	// Bot: a bot's branch built, by the config's automation branch_prefixes
	// (Params.BotBranch), or a bot PR's PR-<n>. In no build number: a bot's
	// failed build is no quality problem of the team's (owner, 2026-11).
	Bot bool `json:"bot" db:"-"`
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

// SharedParams select what one page request's windows share: the team,
// the earliest window start, the ends whose Apps class a window (each
// window's ClassAt), the read's now, and whether a window marks libraries.
type SharedParams struct {
	Team      string
	From      time.Time
	ClassEnds []time.Time
	Now       time.Time
	Libraries bool
	// Trace reads the team's tracing input (trace.go): a window whose PRs
	// are placed on the deploys that shipped them needs it.
	Trace bool
	// States reads what only the assessment's states need: the approvals
	// since ApprovalsFrom (zero: From) less ApprovalLookback
	// (Dataset.Approvals).
	States        bool
	ApprovalsFrom time.Time
	// Automated is Params.Automated, for the approvals read here.
	Automated func(author, displayName string) bool
}

// Shared are the reads every window of one page request uses alike, read
// once for all of them: the GitOps repos, the team's first event per source,
// the silences from the earliest window's look-back to now, the Apps as of
// each class end, the library evidence and the tracing's stretches. Never
// kept past the request: metrics are computed on read.
type Shared struct {
	gitops    []string
	starts    SourceTimes
	silences  []Silence
	apps      map[time.Time][]App
	libraries *libraryEvidence
	// trace is nil unless SharedParams.Trace.
	trace *trace
	// repoStarts is noergler's first event per repository, lowercased.
	repoStarts map[string]time.Time
	// approvals and branches are SharedParams.States' (Dataset.Approvals,
	// Dataset.Branches).
	approvals []Approval
	branches  []BranchLife
}

// LoadShared reads a page request's Shared: the tracing input side by side
// with the rest, each in a read-only snapshot; the stretches are built once
// both are there (the silences mark the releases lost events may explain).
func LoadShared(ctx context.Context, db DB, p SharedParams) (*Shared, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var sh *Shared
	var in *traceInput
	var errs [2]error
	var wg sync.WaitGroup
	wg.Go(func() {
		tx, err := begin(ctx, db)
		if err == nil {
			defer func() { _ = tx.Rollback(ctx) }()
			sh, err = loadShared(ctx, tx, SharedParams{Team: p.Team, From: p.From, ClassEnds: p.ClassEnds, Now: p.Now,
				Libraries: p.Libraries, States: p.States, ApprovalsFrom: p.ApprovalsFrom, Automated: p.Automated})
		}
		if errs[0] = err; err != nil {
			cancel()
		}
	})
	if p.Trace {
		wg.Go(func() {
			tx, err := begin(ctx, db)
			if err == nil {
				defer func() { _ = tx.Rollback(ctx) }()
				in, err = loadTrace(ctx, tx, p.Team)
			}
			if errs[1] = err; err != nil {
				cancel()
			}
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil && !errors.Is(err, context.Canceled) {
			return nil, err
		}
	}
	if err := errors.Join(errs[:]...); err != nil {
		return nil, err
	}
	if p.Trace {
		sh.trace = buildTrace(in, sh.silences)
	}
	return sh, nil
}

// begin opens a read-only snapshot, so a read's sections agree with each
// other while webhooks keep landing.
func begin(ctx context.Context, db DB) (pgx.Tx, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	// The lead-time views' row estimates are off by orders of magnitude
	// (measured: 77 M estimated, 10 k actual), which makes the planner
	// JIT-compile a 250 ms query for another 600 ms.
	// pgx prepares every query; after five runs Postgres may plan it once
	// for any parameters, and the team and window decide the plan: a
	// generic one read the Apps over twenty times slower and the PRs half
	// again as slow (measured). Planning costs about a millisecond a query.
	for _, set := range []string{"SET LOCAL jit = off", "SET LOCAL plan_cache_mode = force_custom_plan"} {
		if _, err := tx.Exec(ctx, set); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
	}
	return tx, nil
}

func loadShared(ctx context.Context, tx pgx.Tx, p SharedParams) (*Shared, error) {
	sh := &Shared{apps: map[time.Time][]App{}}
	// Never nil: pgx sends nil as NULL, `= ANY(NULL)` scans NULL into a
	// bool and `<> ALL(NULL)` drops every open PR.
	var gitops []string
	if err := tx.QueryRow(ctx, gitopsSQL).Scan(&gitops); err != nil {
		return nil, fmt.Errorf("dataset gitops: %w", err)
	}
	if gitops == nil {
		gitops = []string{}
	}
	sh.gitops = gitops
	meta, err := one[metaRow](ctx, tx, "meta", metaSQL, p.Team, nil)
	if err != nil {
		return nil, err
	}
	sh.starts = SourceTimes{Bitbucket: meta.Bitbucket, Pipeline: meta.Pipeline, ArgoCD: meta.ArgoCD, Noergler: meta.Noergler}
	rows := map[time.Time][]appRow{}
	var live []appRow
	for _, end := range p.ClassEnds {
		if _, ok := rows[end]; ok {
			continue
		}
		if rows[end], err = all[appRow](ctx, tx, "apps", appsSQL, p.Team, end); err != nil {
			return nil, err
		}
		live = append(live, rows[end]...)
	}
	// From the earliest window's open PRs' look-back, or an App going live
	// further back, to now: a silence the window ends inside closes after it.
	from := p.From.Add(-SilenceLookback)
	if back := liveSince(live, sh.starts.ArgoCD); back != nil && back.Before(from) {
		from = *back
	}
	if sh.silences, err = loadSilences(ctx, tx, p.Team, from, p.Now); err != nil {
		return nil, err
	}
	for end, r := range rows {
		sh.apps[end] = classify(r, sh.starts, sh.silences)
	}
	if p.Libraries {
		if sh.libraries, err = loadLibraries(ctx, tx, p.Team); err != nil {
			return nil, err
		}
	}
	if p.Trace {
		in, err := loadTrace(ctx, tx, p.Team)
		if err != nil {
			return nil, err
		}
		sh.trace = buildTrace(in, sh.silences)
	}
	starts, err := all[repoStartRow](ctx, tx, "repo_starts", repoStartsSQL, p.Team)
	if err != nil {
		return nil, err
	}
	sh.repoStarts = map[string]time.Time{}
	for _, r := range starts {
		sh.repoStarts[r.Repo] = r.Noergler
	}
	sh.approvals, sh.branches = []Approval{}, []BranchLife{}
	if p.States {
		since := p.ApprovalsFrom
		if since.IsZero() {
			since = p.From
		}
		if sh.approvals, err = all[Approval](ctx, tx, "approvals", approvalsSQL, p.Team, since.Add(-ApprovalLookback),
			sh.gitops); err != nil {
			return nil, err
		}
		automated := func(login, displayName string) bool {
			return p.Automated != nil && p.Automated(login, displayName)
		}
		approved(sh.approvals, automated)
		if sh.branches, err = all[BranchLife](ctx, tx, "branches", branchesSQL, p.Team, p.From, sh.gitops); err != nil {
			return nil, err
		}
		for i := range sh.branches {
			b := &sh.branches[i]
			b.Bot = b.Tool || automated(b.Author, b.AuthorDisplayName) || automated(b.HeadName, b.HeadDisplayName)
		}
	}
	return sh, nil
}

// Load reads the dataset in one read-only snapshot, so the sections agree
// with each other while webhooks keep landing.
func Load(ctx context.Context, db DB, p Params) (*Dataset, error) {
	tx, err := begin(ctx, db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	classAt := p.ClassAt
	if classAt.IsZero() {
		classAt = p.To
	}
	sh := p.Shared
	if sh == nil {
		if sh, err = loadShared(ctx, tx, SharedParams{Team: p.Team, From: p.From, ClassEnds: []time.Time{classAt}, Now: p.Now,
			Libraries: !p.CountsOnly, Trace: !p.CountsOnly}); err != nil {
			return nil, err
		}
	}
	if !p.CountsOnly && sh.trace == nil {
		return nil, errors.New("dataset: the shared read has no tracing")
	}
	apps, ok := sh.apps[classAt]
	if !ok {
		return nil, fmt.Errorf("dataset: no Apps as of %s in the shared read", classAt)
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
	d.Meta.Silences = inWindow(sh.silences, p.From, p.To)
	d.Meta.Lost = slices.DeleteFunc(slices.Clone(d.Meta.Silences), func(s Silence) bool { return !s.Lost })
	d.Apps = apps
	d.Meta.RepoStarts = []RepoStart{}

	d.GitOpsRepos = sh.gitops
	if d.PRs, err = all[PR](ctx, tx, "prs", prsSQL, p.Team, p.From, p.To, d.GitOpsRepos, HousekeepingWindow.Seconds()); err != nil {
		return nil, err
	}
	d.OpenPRs, d.Runs = []OpenPR{}, []Run{}
	if !p.Lean {
		if d.OpenPRs, err = all[OpenPR](ctx, tx, "open_prs", openPRsSQL, p.Team, p.To, d.GitOpsRepos); err != nil {
			return nil, err
		}
	}
	if d.Deploys, err = all[Deploy](ctx, tx, "deploys", deploysSQL, p.Team, p.From, p.To); err != nil {
		return nil, err
	}
	firstReleases(d.Deploys, d.Meta.DataStart.ArgoCD)
	classDeploys(d.Deploys, d.Apps)
	if !p.Lean {
		if d.Runs, err = all[Run](ctx, tx, "runs", runsSQL, p.Team, p.From, p.To, d.GitOpsRepos); err != nil {
			return nil, err
		}
	}
	d.Reviews = []Review{}
	d.Approvals, d.Branches = sh.approvals, sh.branches
	d.Releases = []AppRelease{}
	if sh.trace != nil {
		d.Releases = sh.trace.releases
	}
	if !p.FactsOnly && !p.Lean {
		if d.Reviews, err = all[Review](ctx, tx, "reviews", reviewsSQL, p.Team, p.From, p.To, d.GitOpsRepos); err != nil {
			return nil, err
		}
	}

	runRepos(d)
	if err := pipelineRepos(ctx, tx, d, p.Team); err != nil {
		return nil, err
	}
	exclude(d, p.Excluded)

	automated := func(login, displayName string) bool {
		return p.Automated != nil && p.Automated(login, displayName)
	}
	for i := range d.PRs {
		reviewed(&d.PRs[i], automated)
		// A branch named after its ticket (KAS-12-login) is a feature's, on
		// read: change_type is stored at ingest as "other".
		if pr := &d.PRs[i]; pr.ChangeType != nil && *pr.ChangeType == "other" && parse.TicketBranch(pr.SourceRef) {
			pr.ChangeType = ptr("feature")
		}
	}
	for i := range d.OpenPRs {
		reviewedOpen(&d.OpenPRs[i], automated)
	}
	for i := range d.PRs {
		markUmbrella(&d.PRs[i], automated)
	}
	markImplicitMerges(d.PRs)
	markHousekeeping(d.PRs, automated)
	markTwice(d.PRs)
	markUnknown(d.OpenPRs, sh.silences, p.To)
	for i := range d.Runs {
		d.Runs[i].Branch = Branch(d.Runs[i].PipelineName)
		d.Runs[i].BranchKind = BranchKind(d.Runs[i].Branch)
	}
	markBotRuns(d, p.BotBranch)
	if !p.FactsOnly {
		d.Meta.RepoStarts = repoStarts(d, sh.repoStarts)
	}
	if p.CountsOnly {
		d.Traces = map[string]RepoTrace{}
		for i := range d.PRs {
			d.PRs[i].Environments = []EnvironmentDeploy{}
		}
	} else {
		place(d, sh.trace)
	}
	// The facts too: their column to production must not say "no" for a
	// library's PR.
	if !p.CountsOnly && sh.libraries != nil {
		markLibraries(d, sh.libraries)
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

// pipelineRepos gives a run still without a repository the one its pipeline
// name starts with: a Jenkins multibranch job is `<repo-slug>/<branch>`, and
// a run whose commit Bitbucket never saw (its push lost in an outage) would
// otherwise be in no product. The slug must name exactly one repository the
// team is known for, by its Bitbucket events over all history or by another
// run; two projects with a repo of that name, or none, leave the run
// without. Only a name of exactly two segments: Jenkins encodes a '/'
// inside the branch, so `<folder>/<repo>/<branch>` is a folder's job, and
// a folder named like a repo is no repo.
func pipelineRepos(ctx context.Context, tx pgx.Tx, d *Dataset, team string) error {
	slugOf := func(repo string) string {
		_, slug, _ := strings.Cut(repo, "/")
		return strings.ToLower(slug)
	}
	headOf := func(r Run) (string, bool) {
		head, branch, ok := strings.Cut(r.PipelineName, "/")
		return strings.ToLower(head), ok && head != "" && branch != "" && !strings.Contains(branch, "/")
	}
	heads := map[string]bool{}
	for _, r := range d.Runs {
		if head, ok := headOf(r); ok && r.Repo == nil {
			heads[head] = true
		}
	}
	if len(heads) == 0 {
		return nil
	}
	var known []string
	if err := tx.QueryRow(ctx, reposSQL, team, slices.Sorted(maps.Keys(heads))).Scan(&known); err != nil {
		return fmt.Errorf("dataset repos: %w", err)
	}
	for _, r := range d.Runs {
		if r.Repo != nil && heads[slugOf(*r.Repo)] {
			known = append(known, *r.Repo)
		}
	}
	bySlug := map[string]map[string]bool{}
	for _, repo := range known {
		s := slugOf(repo)
		if bySlug[s] == nil {
			bySlug[s] = map[string]bool{}
		}
		bySlug[s][repo] = true
	}
	for i := range d.Runs {
		r := &d.Runs[i]
		head, ok := headOf(*r)
		if !ok || r.Repo != nil || len(bySlug[head]) != 1 {
			continue
		}
		for repo := range bySlug[head] {
			project, _, _ := strings.Cut(repo, "/")
			r.Repo, r.Project = ptr(repo), ptr(project)
			r.GitOps = slices.Contains(d.GitOpsRepos, repo)
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// silenceRow is a row of sql/silences.sql.
type silenceRow struct {
	Source     string    `db:"source"`
	From       time.Time `db:"from_at"`
	To         time.Time `db:"to_at"`
	UnseenRuns int64     `db:"unseen_runs"`
	Ongoing    bool      `db:"ongoing"`
}

// loadSilences reads the team's silences from from to now: those with
// SilenceMin of working time in all, oldest first, the one still running at
// now (Ongoing) included.
func loadSilences(ctx context.Context, tx pgx.Tx, team string, from, now time.Time) ([]Silence, error) {
	rows, err := all[silenceRow](ctx, tx, "silences", silencesSQL, team, from, now)
	if err != nil {
		return nil, err
	}
	out := []Silence{}
	for _, r := range rows {
		if workingTime(r.From, r.To) >= SilenceMin {
			out = append(out, Silence{Source: r.Source, From: r.From.UTC(), To: r.To.UTC(), UnseenRuns: int(r.UnseenRuns),
				Lost: r.Source == SourceBitbucket && r.UnseenRuns > 0, Ongoing: r.Ongoing})
		}
	}
	return out, nil
}

// inWindow are the silences that overlap [from, to): one of SilenceMin in
// all is a hole wherever it lies, also with 36 working hours on each side
// of a window's edge, or at its end closing on the Tuesday after.
func inWindow(silences []Silence, from, to time.Time) []Silence {
	out := []Silence{}
	for _, s := range silences {
		if s.From.Before(to) && s.To.After(from) {
			out = append(out, s)
		}
	}
	return out
}

// workingTime is the part of [from, to) on Monday to Friday, in UTC: the
// dataset knows no viewer's zone, and two working days are no instant.
func workingTime(from, to time.Time) time.Duration {
	from, to = from.UTC(), to.UTC()
	var out time.Duration
	for day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC); day.Before(to); day = day.AddDate(0, 0, 1) {
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		if s, e := later(day, from), earlier(day.AddDate(0, 0, 1), to); e.After(s) {
			out += e.Sub(s)
		}
	}
	return out
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// markUnknown flags the open PRs, at the window's end to, whose latest
// event came before a lost Bitbucket silence (Silence.Lost) starting before
// to and ending within SilenceLookback of it, and none since: their close
// may be what was lost. silences are loadSilences'. A PR riptide heard of
// only after the instant has the instant as its last event, so never.
func markUnknown(open []OpenPR, silences []Silence, to time.Time) {
	for i := range open {
		o := &open[i]
		o.Unknown = slices.ContainsFunc(silences, func(s Silence) bool {
			return s.Lost && s.From.Before(to) && s.To.After(to.Add(-SilenceLookback)) && !o.LastEventAt.After(s.From)
		})
	}
}

// firstRelease reports whether an App's first deploy at at counts as a
// release: Argo CD had reported for the team longer than FirstDeployGrace
// (argoStart, all history).
func firstRelease(at time.Time, argoStart *time.Time) bool {
	return argoStart != nil && at.Sub(*argoStart) > FirstDeployGrace
}

// firstReleases counts an App's first deploy as a release (firstRelease):
// the Kind stays first_seen, the row says it is still the App's first
// known deploy.
func firstReleases(deploys []Deploy, argoStart *time.Time) {
	for i := range deploys {
		d := &deploys[i]
		d.FirstRelease = d.Kind != nil && *d.Kind == "first_seen" && firstRelease(d.At, argoStart)
	}
}

// appRow is a row of sql/apps.sql.
type appRow struct {
	App     string     `db:"app"`
	Project *string    `db:"project"`
	LastAt  time.Time  `db:"last_at"`
	FirstAt *time.Time `db:"first_at"`
}

// liveSince is how far back the silences must reach to tell whether
// Bitbucket's events were lost when an App without a product went live as a
// release (lostBefore): LostCommitReach before the earliest such day. Nil
// without one.
func liveSince(apps []appRow, argoStart *time.Time) *time.Time {
	var out *time.Time
	for _, a := range apps {
		if a.Project != nil || a.FirstAt == nil || !firstRelease(*a.FirstAt, argoStart) {
			continue
		}
		if day := a.FirstAt.UTC().Truncate(24 * time.Hour).Add(-LostCommitReach); out == nil || day.Before(*out) {
			out = &day
		}
	}
	return out
}

// classify decides each App's product and whether it is infrastructure.
// An App none of whose deploys names a product is infrastructure only when
// the team has deploys with one (else its GitOps repo sends Bitbucket
// nothing, and every App would drop out), Bitbucket reported before one of
// its deploys, and it did not go live as a release while Bitbucket's events
// were lost or soon after (lostBefore): a new App's GitOps commit in a hole
// is a release without a product, not infrastructure.
func classify(apps []appRow, starts SourceTimes, silences []Silence) []App {
	product := slices.ContainsFunc(apps, func(a appRow) bool { return a.Project != nil })
	out := make([]App, 0, len(apps))
	for _, a := range apps {
		infra := product && a.Project == nil && starts.Bitbucket != nil && a.LastAt.After(*starts.Bitbucket) &&
			(a.FirstAt == nil || !firstRelease(*a.FirstAt, starts.ArgoCD) || !lostBefore(*a.FirstAt, silences))
		out = append(out, App{Name: a.App, Project: a.Project, Infra: infra})
	}
	return out
}

// lostBefore reports whether a lost silence began before at's day ended and
// ended within LostCommitReach before at's day began, in UTC as the
// silences' working time: the commit deployed at at may lie in it, though
// Argo CD synced it days after the hole closed.
func lostBefore(at time.Time, silences []Silence) bool {
	day := at.UTC().Truncate(24 * time.Hour)
	return slices.ContainsFunc(silences, func(s Silence) bool {
		return s.Lost && s.From.Before(day.Add(24*time.Hour)) && s.To.After(day.Add(-LostCommitReach))
	})
}

// classDeploys gives each deploy its App's class: infrastructure or not,
// and the App's product where the deploy's revision names none. An App
// apps does not know keeps what the deploy has.
func classDeploys(deploys []Deploy, apps []App) {
	by := map[string]App{}
	for _, a := range apps {
		by[a.Name] = a
	}
	for i := range deploys {
		d := &deploys[i]
		a, ok := by[d.App]
		if !ok {
			continue
		}
		d.Infra = a.Infra
		if d.Project == nil && a.Project != nil {
			d.Project = ptr(*a.Project)
		}
	}
}

// Classified is d with its deploys classed by apps, another dataset's Apps:
// a page classes its period and the previous one by the period's end, so
// the comparison never moves for an App classed apart. A copy; d is
// unchanged. Nil apps keep d's own.
func (d *Dataset) Classified(apps []App) *Dataset {
	if apps == nil {
		return d
	}
	cp := *d
	cp.Deploys = slices.Clone(d.Deploys)
	classDeploys(cp.Deploys, apps)
	cp.Apps = apps
	cp.Products = products(&cp)
	return &cp
}

// markBotRuns flags the runs of a bot's branch (botBranch, the config's
// prefixes) and the PR-<n> builds of the dataset's bot PRs: a Jenkins PR
// build names its PR, not the bot's branch.
func markBotRuns(d *Dataset, botBranch func(string) bool) {
	key := func(repo string, id int64) string { return repo + "\x00pr-" + strconv.FormatInt(id, 10) }
	botPRs := map[string]bool{}
	for _, pr := range d.PRs {
		if pr.Bot {
			botPRs[key(pr.Repo, pr.PRID)] = true
		}
	}
	for _, pr := range d.OpenPRs {
		if pr.Bot {
			botPRs[key(pr.Repo, pr.PRID)] = true
		}
	}
	for i := range d.Runs {
		r := &d.Runs[i]
		branch := strings.ToLower(r.Branch)
		r.Bot = (botBranch != nil && branch != "" && botBranch(branch)) ||
			(r.Repo != nil && botPRs[*r.Repo+"\x00"+branch])
	}
}

// libraryEvidence is sql/libraries.sql's row: what tells a library repo, one
// never rolled out, from a deployable one not traced (yet).
type libraryEvidence struct {
	Images    []string `db:"images"`
	Traced    []string `db:"traced"`
	Publishes []string `db:"publishes"`
	RolledOut bool     `db:"rolled_out"`
	// segments are every path segment of Images, the last one without its
	// tag or digest, lowercased.
	segments map[string]bool
}

func loadLibraries(ctx context.Context, tx pgx.Tx, team string) (*libraryEvidence, error) {
	e, err := one[libraryEvidence](ctx, tx, "libraries", librariesSQL, team)
	if err != nil {
		return nil, err
	}
	e.segments = map[string]bool{}
	for _, ref := range e.Images {
		for _, s := range imageSegments(ref) {
			e.segments[s] = true
		}
	}
	return &e, nil
}

// imageSegments are an image reference's path segments, lowercased, the
// last one without its digest and tag: "reg:5000/team/kasse-api:1.2" is
// reg:5000, team, kasse-api. A registry's port is no tag.
func imageSegments(ref string) []string {
	ref = strings.ToLower(ref)
	segs := strings.Split(ref, "/")
	last := segs[len(segs)-1]
	last, _, _ = strings.Cut(last, "@")
	last, _, _ = strings.Cut(last, ":")
	segs[len(segs)-1] = last
	return segs
}

// library reports whether repo is a library, on positive evidence only: the
// team's tracing works at all (a compare link traced some repo, a run's
// image was deployed), and of repo none of it does: no deployed GitOps
// release commit names it in a compare link, no run of it built an image
// (deployed or not yet), and no image Argo CD deployed for the team is
// named after it (a path segment equal to its slug: a deployable repo's
// image is, even when its CI does not report). A deployable repo whose CI
// does not report, whose image is named otherwise and whose release commits
// name no compare link reads as a library: docs/metrics.md.
func (e *libraryEvidence) library(repo string) bool {
	if !e.RolledOut || len(e.Traced) == 0 {
		return false
	}
	_, slug, _ := strings.Cut(repo, "/")
	slug = strings.ToLower(slug)
	return slug != "" && !slices.Contains(e.Traced, slug) && !slices.Contains(e.Publishes, repo) && !e.segments[slug]
}

// markLibraries flags the merged PRs of library repos (libraryEvidence). A
// repo one of whose PRs in the window reached any stage is none, whatever
// the evidence: its merge commit was rolled out. After place.
func markLibraries(d *Dataset, e *libraryEvidence) {
	traced := map[string]bool{}
	for _, pr := range d.PRs {
		if len(pr.Environments) > 0 {
			traced[pr.Repo] = true
		}
	}
	for i := range d.PRs {
		pr := &d.PRs[i]
		pr.Library = pr.State == "merged" && !pr.GitOps && !traced[pr.Repo] && e.library(pr.Repo)
	}
}

// markUmbrella makes a PR an umbrella when a person opened one of the other
// merged PRs into its source branch, by the same automation detection as
// every PR's opener (automated).
func markUmbrella(pr *PR, automated func(login, displayName string) bool) {
	pr.Umbrella = slices.ContainsFunc(pr.UmbrellaOpeners, func(o opening) bool {
		return !o.Bot && !automated(o.Author, o.DisplayName)
	})
}

// markTwice flags a PR merged implicitly whose source branch another merged
// PR of the repo came from, one counted as work: a branch merged into an
// umbrella by its own PR, and marked merged into master too when the
// umbrella went in, is one piece of work. After markImplicitMerges.
func markTwice(prs []PR) {
	counted := map[string]bool{}
	key := func(pr PR) string { return pr.Repo + "\x00" + strings.ToLower(pr.SourceRef) }
	for _, pr := range prs {
		if pr.State == "merged" && !pr.ImplicitMerge && !pr.Umbrella && !pr.Housekeeping && !pr.Bot && !pr.GitOps &&
			pr.SourceRef != "" {
			counted[key(pr)] = true
		}
	}
	for i := range prs {
		pr := &prs[i]
		pr.Twice = pr.ImplicitMerge && !pr.Umbrella && pr.SourceRef != "" && counted[key(*pr)]
	}
}

// markImplicitMerges flags the PRs Bitbucket marked merged when another
// branch brought their commits in: in one repo, merged within
// ImplicitMergeWindow of each other (chained), at most one of them with a
// merge commit; those without one. The one with it merged them: an
// umbrella, when one of them is a person's (a bot's PR never makes a
// person's uncounted). Two PRs merged in one second each with a merge
// commit are two merges. Stage promotions aside, as everywhere.
func markImplicitMerges(prs []PR) {
	byRepo := map[string][]int{}
	for i, pr := range prs {
		if pr.State == "merged" && pr.MergedAt != nil && !pr.GitOps {
			byRepo[pr.Repo] = append(byRepo[pr.Repo], i)
		}
	}
	for _, idx := range byRepo {
		slices.SortFunc(idx, func(a, b int) int {
			return cmp.Or(prs[a].MergedAt.Compare(*prs[b].MergedAt), cmp.Compare(prs[a].PRID, prs[b].PRID))
		})
		for a := 0; a < len(idx); {
			b := a + 1
			for b < len(idx) && prs[idx[b]].MergedAt.Sub(*prs[idx[b-1]].MergedAt) <= ImplicitMergeWindow {
				b++
			}
			group := idx[a:b]
			a = b
			keep, commits := -1, 0
			for _, i := range group {
				if prs[i].MergeCommit != nil {
					keep = i
					commits++
				}
			}
			if len(group) < 2 || commits > 1 {
				continue
			}
			for _, i := range group {
				prs[i].ImplicitMerge = i != keep
				// It reached the trunk with the merge that brought it in.
				if i != keep && keep >= 0 && prs[i].TrunkAt == nil {
					prs[i].TrunkAt, prs[i].TrunkBranch = prs[keep].TrunkAt, prs[keep].TrunkBranch
				}
			}
			if keep >= 0 && !prs[keep].Bot && slices.ContainsFunc(group, func(i int) bool { return i != keep && !prs[i].Bot }) {
				prs[keep].Umbrella = true
			}
		}
	}
}

// markHousekeeping flags the housekeeping PRs: a person's, merged, without
// a person's approval (ApprovedAt counts no bot, service or declared
// non-human account) and without a ticket key in its title or source
// branch, while some HousekeepingWindow holds its title as opened, opened
// by people in HousekeepingMinRepos repos (PR.Siblings: the team's openings
// of all history, so a batch across a window's edge is one in both
// windows). The title compares trimmed, case and inner spaces aside. A PR
// opened before ingestion has no opening: never one.
func markHousekeeping(prs []PR, automated func(login, displayName string) bool) {
	for i := range prs {
		pr := &prs[i]
		if pr.Bot || pr.GitOps || pr.State != "merged" || pr.OpenedAt == nil || pr.OpenedTitle == nil ||
			pr.ApprovedAt != nil || len(parse.JiraKeys(*pr.OpenedTitle, deref(pr.Title), pr.SourceRef)) > 0 {
			continue
		}
		var people []opening
		for _, o := range pr.Siblings {
			if !o.Bot && !automated(o.Author, o.DisplayName) {
				people = append(people, o)
			}
		}
		// A window starting at one of the title's openings and holding this
		// one's.
		pr.Housekeeping = slices.ContainsFunc(people, func(s opening) bool {
			if pr.OpenedAt.Before(s.At) || pr.OpenedAt.Sub(s.At) > HousekeepingWindow {
				return false
			}
			repos := map[string]bool{}
			for _, o := range people {
				if !o.At.Before(s.At) && o.At.Sub(s.At) <= HousekeepingWindow {
					repos[o.Repo] = true
				}
			}
			return len(repos) >= HousekeepingMinRepos
		})
	}
}

// repoStartRow is a row of sql/repo_starts.sql.
type repoStartRow struct {
	Repo     string    `db:"repo"`
	Noergler time.Time `db:"noergler"`
}

// repoStarts is noergler's first event for each repository the window's PRs
// name, open ones included, by repository; first is the shared read's.
func repoStarts(d *Dataset, first map[string]time.Time) []RepoStart {
	repos := map[string]bool{}
	for _, pr := range d.PRs {
		repos[pr.Repo] = true
	}
	for _, pr := range d.OpenPRs {
		repos[pr.Repo] = true
	}
	out := []RepoStart{}
	for _, repo := range slices.Sorted(maps.Keys(repos)) {
		s := RepoStart{Repo: repo}
		if t, ok := first[strings.ToLower(repo)]; ok {
			s.Noergler = &t
		}
		out = append(out, s)
	}
	return out
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
		ptrs = append(ptrs, p.ClosedAt, p.OpenedAt, p.ReadyAt, p.FirstReviewAt, p.ApprovedAt, p.MergedAt, p.FirstCommitAt,
			p.TrunkAt)
		for j := range p.Environments {
			ptrs = append(ptrs, &p.Environments[j].FirstDeployedAt)
		}
	}
	for i := range d.OpenPRs {
		o := &d.OpenPRs[i]
		ptrs = append(ptrs, &o.OpenedAt, &o.LastEventAt, o.ReadyAt, o.ReadySince)
	}
	for i := range d.Meta.Silences {
		ptrs = append(ptrs, &d.Meta.Silences[i].From, &d.Meta.Silences[i].To)
	}
	for i := range d.Meta.RepoStarts {
		ptrs = append(ptrs, d.Meta.RepoStarts[i].Noergler)
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
	// A branch named after its ticket without a prefix (KAS-12-login).
	case parse.TicketBranch(b):
		return BranchFeature
	}
	return BranchOther
}
