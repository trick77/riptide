package metrics

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// FactsLimit caps each facts table; rows past it are left out of the
// table, not of any number.
const FactsLimit = 300

// Search terms: the words the UI shows for a PR state or a deploy class,
// so a search for what is on screen finds the row. Never sent: the UI owns
// its wording, the API sends the codes.
var prStateTerm = map[string]string{
	"merged":   "Gemergt",
	"open":     "Offen",
	"declined": "Abgelehnt",
	"deleted":  "Gelöscht",
}

// Deploy classes in the facts table.
const (
	DeployFailed    = "failed"
	DeployRelease   = "release"
	DeployFirstSeen = "first_seen"
	DeploySync      = "sync"
)

// deployClass is what a deploy counts as, with its search term; a failed
// sync's is its phase.
func deployClass(d dataset.Deploy) (string, string) {
	switch {
	case d.Phase != "Succeeded":
		return DeployFailed, d.Phase
	case d.Kind != nil && *d.Kind == "release":
		return DeployRelease, "Release"
	case d.Kind != nil && *d.Kind == "first_seen":
		return DeployFirstSeen, "Erstdeploy"
	}
	return DeploySync, "Sync ohne neue Images"
}

// jiraPrefix is the Jira keys a PR's title does not already show, three at
// most: a promotion PR collects every key it ships.
func jiraPrefix(pr dataset.PR) string {
	title := ""
	if pr.Title != nil {
		title = *pr.Title
	}
	var keys []string
	for _, k := range pr.JiraKeys {
		if !strings.Contains(title, k) {
			keys = append(keys, k)
		}
	}
	if len(keys) <= 3 {
		return strings.Join(keys, " ")
	}
	return fmt.Sprintf("%s +%d", strings.Join(keys[:3], " "), len(keys)-3)
}

// PRFact is one row of the PR table.
type PRFact struct {
	Repo       string  `json:"repo"`
	PRID       int64   `json:"pr_id"`
	Title      *string `json:"title"`
	JiraPrefix string  `json:"jira_prefix"`
	GitOps     bool    `json:"gitops"`
	Bot        bool    `json:"bot"`
	ChangeType *string `json:"change_type"`
	State      string  `json:"state"`
	// ReviewS is ready to approval, CycleS opened to merged, ToProdS merged
	// to the first production deploy; null when unknown or negative.
	ReviewS  *float64   `json:"review_s"`
	CycleS   *float64   `json:"cycle_s"`
	MergedAt *time.Time `json:"merged_at"`
	ProdAt   *time.Time `json:"prod_at"`
	ToProdS  *float64   `json:"to_prod_s"`
}

// DeployFact is one row of the deploy table.
type DeployFact struct {
	At          time.Time `json:"at"`
	App         string    `json:"app"`
	Environment *string   `json:"environment"`
	Revision    string    `json:"revision"`
	NewImages   []string  `json:"new_images"`
	Class       string    `json:"class"`
	Phase       string    `json:"phase"`
}

// RunFact is one row of the run table.
type RunFact struct {
	PipelineName string   `json:"pipeline_name"`
	RunID        string   `json:"run_id"`
	Kind         string   `json:"kind"`
	CommitSHA    *string  `json:"commit_sha"`
	DurationS    *int64   `json:"duration_s"`
	ImageRefs    []string `json:"image_refs"`
	Status       *string  `json:"status"`
}

// Table is one facts table: how many rows match, the first FactsLimit.
type Table[T any] struct {
	Count int `json:"count"`
	Rows  []T `json:"rows"`
}

// Facts is the /api/facts body: the single facts behind every number,
// newest first, filtered by product, bots (PRs only) and the search.
type Facts struct {
	ProductionStage string            `json:"production_stage"`
	Limit           int               `json:"limit"`
	PRs             Table[PRFact]     `json:"prs"`
	Deploys         Table[DeployFact] `json:"deploys"`
	Runs            Table[RunFact]    `json:"runs"`
}

// matcher is a case-insensitive substring search over a row's fields; an
// empty query matches everything.
type matcher string

func newMatcher(q string) matcher { return matcher(strings.ToLower(strings.TrimSpace(q))) }

func (m matcher) match(fields ...*string) bool {
	if m == "" {
		return true
	}
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(' ')
		}
		if f != nil {
			b.WriteString(*f)
		}
	}
	return strings.Contains(strings.ToLower(b.String()), string(m))
}

func strs(v []string) []*string {
	out := make([]*string, len(v))
	for i := range v {
		out[i] = &v[i]
	}
	return out
}

// newestFirst sorts by an instant, newest first, rows without one last;
// ties keep the dataset's order.
func newestFirst[T any](rows []T, at func(T) *time.Time) []T {
	out := slices.Clone(rows)
	slices.SortStableFunc(out, func(a, b T) int {
		x, y := at(a), at(b)
		switch {
		case x == nil && y == nil:
			return 0
		case x == nil:
			return 1
		case y == nil:
			return -1
		}
		return y.Compare(*x)
	})
	return out
}

func table[S, T any](rows []S, keep func(S) bool, row func(S) T) Table[T] {
	t := Table[T]{Rows: []T{}}
	for _, r := range rows {
		if !keep(r) {
			continue
		}
		t.Count++
		if len(t.Rows) < FactsLimit {
			t.Rows = append(t.Rows, row(r))
		}
	}
	return t
}

// ComputeFacts is the facts tables for the search q. The product filters as
// given: the facts read no lead times or reviews, so their products are not
// all of the page's; the UI sends the product /api/metrics resolved.
func ComputeFacts(ds *dataset.Dataset, p Params, q string) Facts {
	f := filter{product: p.Product, bots: p.Bots}
	m := newMatcher(q)
	prod := ds.Meta.ProductionStage

	prs := newestFirst(ds.PRs, func(pr dataset.PR) *time.Time { return cmp.Or(pr.MergedAt, pr.OpenedAt) })
	deploys := newestFirst(ds.Deploys, func(d dataset.Deploy) *time.Time { return &d.At })
	runs := newestFirst(ds.Runs, runAt)

	return Facts{
		ProductionStage: prod,
		Limit:           FactsLimit,
		PRs: table(prs, func(pr dataset.PR) bool {
			term := prStateTerm[pr.State]
			id := fmt.Sprintf("#%d", pr.PRID)
			return f.inKey(pr.Project) && (f.bots || !pr.Bot) &&
				m.match(append([]*string{&pr.Repo, pr.Title, pr.ChangeType, &id}, append(strs(pr.JiraKeys), &term)...)...)
		}, func(pr dataset.PR) PRFact {
			prodAt := firstDeploy(pr, prod)
			return PRFact{
				Repo: pr.Repo, PRID: pr.PRID, Title: pr.Title, JiraPrefix: jiraPrefix(pr),
				GitOps: pr.GitOps, Bot: pr.Bot, ChangeType: pr.ChangeType,
				State:   pr.State,
				ReviewS: wait(pr.ReadyAt, pr.ApprovedAt), CycleS: wait(pr.OpenedAt, pr.MergedAt),
				MergedAt: pr.MergedAt, ProdAt: prodAt, ToProdS: wait(pr.MergedAt, prodAt),
			}
		}),
		// Every image a deploy ran, not only the new ones: a search for an
		// image finds its re-syncs and failed syncs too.
		Deploys: table(deploys, func(d dataset.Deploy) bool {
			_, term := deployClass(d)
			return f.in(d.Project) &&
				m.match(append([]*string{&d.App, d.Environment, &d.Revision}, append(strs(d.Images), &term)...)...)
		}, func(d dataset.Deploy) DeployFact {
			class, _ := deployClass(d)
			return DeployFact{
				At: d.At, App: d.App, Environment: d.Environment, Revision: d.Revision,
				NewImages: d.NewImages, Class: class, Phase: d.Phase,
			}
		}),
		Runs: table(runs, func(r dataset.Run) bool {
			return f.in(r.Project) &&
				m.match(append([]*string{&r.PipelineName, r.Repo, r.CommitSHA, r.Status, r.BuildType}, strs(r.ImageRefs)...)...)
		}, func(r dataset.Run) RunFact {
			return RunFact{
				PipelineName: r.PipelineName, RunID: r.RunID, Kind: RunKind(r, ds.Meta.BuildTypeFrom),
				CommitSHA: r.CommitSHA, DurationS: r.DurationS, ImageRefs: r.ImageRefs, Status: r.Status,
			}
		}),
	}
}
