package metrics

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/parse"
)

// FactsLimit caps each facts table; rows past it are left out of the
// table, not of any number.
const FactsLimit = 300

// Search terms: the words the UI shows for a PR state, a PR's type or a
// deploy class, so a search for what is on screen finds the row. Both UI
// languages match whichever one the viewer reads: a shared ?q= link works
// for everyone. Never sent: the UI owns its wording, the API sends the
// codes. TestSearchTermsMatchTheUI keeps these equal to
// ui/src/i18n/terms.json.
type uiTerm struct{ De, En string }

func (t uiTerm) both() string { return t.De + " " + t.En }

var prStateTerm = map[string]uiTerm{
	"merged":   {"Gemerged", "Merged"},
	"open":     {"Offen", "Open"},
	"declined": {"Abgelehnt", "Declined"},
	"deleted":  {"Gelöscht", "Deleted"},
}

// prStateAlias is a state's word the UI showed before, still searched so a
// ?q= link shared back then finds what it found (gemergt became gemerged,
// 2026-11-15).
var prStateAlias = map[string]string{"merged": "Gemergt"}

// prStateTerms is a PR state's search words.
func prStateTerms(state string) string {
	return strings.TrimSpace(prStateTerm[state].both() + " " + prStateAlias[state])
}

var changeTypeTerm = map[string]uiTerm{
	"feature":  {"Feature", "Feature"},
	"bugfix":   {"Bugfix", "Bugfix"},
	"hotfix":   {"Hotfix", "Hotfix"},
	"chore":    {"Chore", "Chore"},
	"refactor": {"Refactor", "Refactor"},
	"docs":     {"Docs", "Docs"},
	"other":    {"Anderes", "Other"},
}

// waitTerm is what an open PR waits on, in the PR table's state column.
var waitTerm = map[string]uiTerm{
	WaitDraft:       {"Entwurf", "Draft"},
	WaitFirstReview: {"Wartet auf Review", "Awaiting review"},
	WaitReview:      {"In Review", "In review"},
	WaitChanges:     {"Änderungen verlangt", "Changes requested"},
	WaitMerge:       {"Bereit zum Merge", "Ready to merge"},
	WaitInactive:    {"Inaktiv", "Inactive"},
}

// promotionTerm is a GitOps PR's type.
var promotionTerm = uiTerm{"Promotion", "Promotion"}

// prTypeTerm is the Typ column's words for a PR: a promotion's own, a known
// change type's in both languages, else the type as the UI shows it, raw.
func prTypeTerm(pr dataset.PR) string {
	switch {
	case pr.GitOps:
		return promotionTerm.both()
	case pr.ChangeType == nil:
		return ""
	}
	if t, ok := changeTypeTerm[*pr.ChangeType]; ok {
		return t.both()
	}
	return *pr.ChangeType
}

// runKindTerm is a run kind's words in the runs table's Art column.
var runKindTerm = map[string]uiTerm{
	KindRelease: {"Releases", "Releases"},
	KindMain:    {"Default-Branch-Builds", "Default branch builds"},
	KindFeature: {"Feature- und Bugfix-Builds", "Feature and bugfix builds"},
	KindOther:   {"Übrige Branches", "Other branches"},
	KindUnknown: {"Default-Branch, Art unbekannt", "Default branch, kind unknown"},
}

// runKindAlias is a kind's words the UI showed before 2026-11-17, still
// searched, as prStateAlias.
var runKindAlias = map[string]string{
	KindMain:    "master-Builds master builds",
	KindFeature: "Feature- und PR-Builds Feature and PR builds",
	KindOther:   "Andere Branches",
	KindUnknown: "master, Art unbekannt master, kind unknown",
}

// runKindTerms is a run kind's search words.
func runKindTerms(kind string) string {
	return strings.TrimSpace(runKindTerm[kind].both() + " " + runKindAlias[kind])
}

// Deploy classes in the facts table.
const (
	DeployFailed    = "failed"
	DeployRelease   = "release"
	DeployFirstSeen = "first_seen"
	DeploySync      = "sync"
)

// deployTerm is a class's search terms; a failed sync's is its phase.
var deployTerm = map[string]uiTerm{
	DeployRelease:   {"Release", "Release"},
	DeployFirstSeen: {"Erstdeploy", "First deploy"},
	DeploySync:      {"Sync ohne neue Images", "Sync without new images"},
}

// deployClass is what a deploy counts as, with its search term: the class
// deployFrequency counts it in, or failed.
func deployClass(d dataset.Deploy) (string, string) {
	if d.Phase != "Succeeded" {
		return DeployFailed, d.Phase
	}
	class := map[int]string{
		classRelease: DeployRelease, classFirstSeen: DeployFirstSeen, classSync: DeploySync,
	}[revisionClass(d)]
	return class, deployTerm[class].both()
}

// JiraRef is a Jira key and its ticket's URL; null without jira.base_url.
type JiraRef struct {
	Key string  `json:"key"`
	URL *string `json:"url"`
}

// TitlePart is a piece of a PR title: plain text, or a Jira key.
type TitlePart struct {
	Text string   `json:"text"`
	Jira *JiraRef `json:"jira"`
}

// jiraLink is <base>/browse/<key>, the ticket URL on Jira Server and Cloud.
func jiraLink(base, key string) JiraRef {
	if base == "" {
		return JiraRef{Key: key}
	}
	u := base + "/browse/" + key
	return JiraRef{Key: key, URL: &u}
}

// titleParts splits a PR title at its Jira keys, by the rule ingest
// extracts them with.
func titleParts(title *string, base string) []TitlePart {
	out := []TitlePart{}
	if title == nil {
		return out
	}
	s, at := *title, 0
	for _, sp := range parse.JiraKeySpans(s) {
		if sp[0] > at {
			out = append(out, TitlePart{Text: s[at:sp[0]]})
		}
		ref := jiraLink(base, s[sp[0]:sp[1]])
		out = append(out, TitlePart{Text: ref.Key, Jira: &ref})
		at = sp[1]
	}
	if at < len(s) {
		out = append(out, TitlePart{Text: s[at:]})
	}
	return out
}

// jiraPrefix is the Jira keys the title's parts do not already show, three
// at most, and how many more there are: a promotion PR collects every key
// it ships.
func jiraPrefix(keys []string, title []TitlePart, base string) ([]JiraRef, int) {
	shown := map[string]bool{}
	for _, p := range title {
		if p.Jira != nil {
			shown[p.Jira.Key] = true
		}
	}
	refs := []JiraRef{}
	more := 0
	for _, k := range keys {
		switch {
		case shown[k]:
		case len(refs) < 3:
			refs = append(refs, jiraLink(base, k))
		default:
			more++
		}
	}
	return refs, more
}

// PRFact is one row of the PR table.
type PRFact struct {
	Repo       string      `json:"repo"`
	PRID       int64       `json:"pr_id"`
	Title      *string     `json:"title"`
	TitleParts []TitlePart `json:"title_parts"`
	JiraPrefix []JiraRef   `json:"jira_prefix"`
	JiraMore   int         `json:"jira_more"`
	GitOps     bool        `json:"gitops"`
	ChangeType *string     `json:"change_type"`
	State      string      `json:"state"`
	// ReviewS is ready to approval, CycleS opened to merged, ToProdS merged
	// to the first production deploy; null when unknown or negative.
	ReviewS  *float64   `json:"review_s"`
	CycleS   *float64   `json:"cycle_s"`
	MergedAt *time.Time `json:"merged_at"`
	ProdAt   *time.Time `json:"prod_at"`
	ToProdS  *float64   `json:"to_prod_s"`
	// AgeS and Waiting are a PR open at the period's end: its age then and
	// what it waits on, inactive when idle OpenInactiveDays; IdleS the time
	// since its last event. Null for every other PR.
	AgeS    *float64 `json:"age_s"`
	Waiting *string  `json:"waiting"`
	IdleS   *float64 `json:"idle_s"`
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
	// Status is as the CI sent it, Verdict what it says (see verdict),
	// Outcome the status when it plainly says so (see runOutcome).
	Status  *string `json:"status"`
	Verdict string  `json:"verdict"`
	Outcome *string `json:"outcome"`
	// GitOps: a run in a GitOps repo, a stage promotion's check; listed,
	// counted nowhere.
	GitOps bool `json:"gitops"`
	// Flaky: failed, and a later run of the pipeline passed on the same
	// commit, as the build section counts it (flakyRuns).
	Flaky bool `json:"flaky"`
}

// flakyTerm is a flaky run's mark on screen, searched in both languages.
var flakyTerm = uiTerm{"Flaky", "Flaky"}

// Run outcomes: a status that plainly says succeeded or failed, which the
// UI words in the viewer's language. Every other status (UNSTABLE, ABORTED,
// NOT_BUILT, Tekton's None) has none: the UI shows the CI's own word, as
// translating it would hide what the CI meant (an unstable build is red,
// but not the same as a broken one).
const (
	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
)

// runOutcome follows verdict, so the pill's colour and its word read one
// list of CI words; only UNSTABLE, red but not broken, keeps its own word.
func runOutcome(r dataset.Run) *string {
	var o string
	switch verdict(r) {
	case VerdictSuccess:
		o = OutcomeSucceeded
	case VerdictFailure:
		if strings.EqualFold(*r.Status, "UNSTABLE") {
			return nil
		}
		o = OutcomeFailed
	default:
		return nil
	}
	return &o
}

// runOutcomeTerm is an outcome's search terms.
var runOutcomeTerm = map[string]uiTerm{
	OutcomeSucceeded: {"Erfolgreich", "Succeeded"},
	OutcomeFailed:    {"Fehlgeschlagen", "Failed"},
}

// runStatusTerms is a run's status as the UI may show it: the outcome's
// words, or the CI's word with spaces for underscores ("NOT_BUILT" reads
// "Not built", ciWord in ui/src/format.ts).
func runStatusTerms(r dataset.Run) string {
	if o := runOutcome(r); o != nil {
		return runOutcomeTerm[*o].both()
	}
	if r.Status == nil {
		return ""
	}
	return strings.ReplaceAll(*r.Status, "_", " ")
}

// Table is one facts table: how many rows match, the first FactsLimit.
type Table[T any] struct {
	Count int `json:"count"`
	Rows  []T `json:"rows"`
}

// Facts is the /api/facts body: the single facts behind every number,
// newest first, filtered by product and the search; bots' PRs left out.
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

// factPR is a PR table row's PR, with its age and what it waits on when it
// was open at the period's end.
type factPR struct {
	pr      dataset.PR
	age     *float64
	waiting *string
	idle    *float64
}

// factPRs are the PRs open at the period's end first, oldest first, the
// inactive ones after them: the ones waiting, and the open PRs' link lands
// on them. Then the window's other PRs, newest first. An open PR the window
// has its row from the window, with the review times read there.
func factPRs(ds *dataset.Dataset) []factPR {
	at := ds.Meta.To
	type key struct {
		repo string
		id   int64
	}
	window := map[key]dataset.PR{}
	for _, pr := range ds.PRs {
		window[key{pr.Repo, pr.PRID}] = pr
	}
	type openRow struct {
		pr       dataset.OpenPR
		inactive bool
	}
	open := make([]openRow, len(ds.OpenPRs))
	for i, o := range ds.OpenPRs {
		open[i] = openRow{o, openInactive(o, at)}
	}
	slices.SortStableFunc(open, func(a, b openRow) int {
		return cmp.Or(cmp.Compare(first(!a.inactive), first(!b.inactive)), a.pr.OpenedAt.Compare(b.pr.OpenedAt))
	})
	out := make([]factPR, 0, len(open)+len(ds.PRs))
	seen := map[key]bool{}
	for _, row := range open {
		o := row.pr
		k := key{o.Repo, o.PRID}
		seen[k] = true
		pr, ok := window[k]
		if !ok {
			opened := o.OpenedAt
			pr = dataset.PR{
				Repo: o.Repo, Project: o.Project, PRID: o.PRID, Title: o.Title, Bot: o.Bot,
				ChangeType: o.ChangeType, JiraKeys: o.JiraKeys, OpenedAt: &opened, ReadyAt: o.ReadyAt,
				Environments: []dataset.EnvironmentDeploy{},
			}
		}
		// Open then, whatever happened to it after the period's end, and
		// filtered as the open-PR numbers count it.
		pr.State = "open"
		pr.Bot, pr.Title, pr.JiraKeys = o.Bot, o.Title, o.JiraKeys
		on := waitsOn(o)
		if row.inactive {
			on = WaitInactive
		}
		out = append(out, factPR{pr: pr, age: ptr(openAge(o, at)), waiting: &on, idle: ptr(openIdle(o, at))})
	}
	rest := newestFirst(ds.PRs, func(pr dataset.PR) *time.Time { return cmp.Or(pr.MergedAt, pr.OpenedAt) })
	for _, pr := range rest {
		if !seen[key{pr.Repo, pr.PRID}] {
			out = append(out, factPR{pr: pr})
		}
	}
	return out
}

// ComputeFacts is the facts tables for the search q. The product filters as
// given: the facts read no lead times or reviews, so their products are not
// all of the page's; the UI sends the product /api/metrics resolved.
func ComputeFacts(ds *dataset.Dataset, p Params, q string) Facts {
	f := filter{product: p.Product}
	m := newMatcher(q)
	prod := ds.Meta.ProductionStage

	prs := factPRs(ds)
	deploys := newestFirst(ds.Deploys, func(d dataset.Deploy) *time.Time { return &d.At })
	runs := newestFirst(ds.Runs, runAt)
	gitops := gitopsRun(ds)
	// As the build section counts them: its runs, its pairing.
	flaky := flakyRuns(teamRuns(ds, f, gitops))
	isFlaky := func(r dataset.Run) bool { return verdict(r) == VerdictFailure && flaky[refOf(r)] }

	return Facts{
		ProductionStage: prod,
		Limit:           FactsLimit,
		// The search reads the words on screen, never a hidden code: a
		// promotion's change type is not searched. The words are built only
		// when there is a search.
		PRs: table(prs, func(x factPR) bool {
			pr := x.pr
			if !f.inKey(pr.Project) || pr.Bot {
				return false
			}
			if m == "" {
				return true
			}
			term, typ := prStateTerms(pr.State), prTypeTerm(pr)
			if x.waiting != nil {
				term += " " + waitTerm[*x.waiting].both()
			}
			id := fmt.Sprintf("#%d", pr.PRID)
			return m.match(append([]*string{&pr.Repo, pr.Title, &typ, &id}, append(strs(pr.JiraKeys), &term)...)...)
		}, func(x factPR) PRFact {
			pr := x.pr
			prodAt := firstDeploy(pr, prod)
			parts := titleParts(pr.Title, p.JiraURL)
			keys, more := jiraPrefix(pr.JiraKeys, parts, p.JiraURL)
			return PRFact{
				Repo: pr.Repo, PRID: pr.PRID, Title: pr.Title, TitleParts: parts,
				JiraPrefix: keys, JiraMore: more,
				GitOps: pr.GitOps, ChangeType: pr.ChangeType,
				State:   pr.State,
				ReviewS: wait(pr.ReadyAt, pr.ApprovedAt), CycleS: wait(pr.OpenedAt, pr.MergedAt),
				MergedAt: pr.MergedAt, ProdAt: prodAt, ToProdS: wait(pr.MergedAt, prodAt),
				AgeS: x.age, Waiting: x.waiting, IdleS: x.idle,
			}
		}),
		// Every image a deploy ran, not only the new ones: a search for an
		// image finds its re-syncs and failed syncs too.
		Deploys: table(deploys, func(d dataset.Deploy) bool {
			if !f.in(d.Project) {
				return false
			}
			if m == "" {
				return true
			}
			_, term := deployClass(d)
			return m.match(append([]*string{&d.App, d.Environment, &d.Revision}, append(strs(d.Images), &term)...)...)
		}, func(d dataset.Deploy) DeployFact {
			class, _ := deployClass(d)
			return DeployFact{
				At: d.At, App: d.App, Environment: d.Environment, Revision: d.Revision,
				NewImages: d.NewImages, Class: class, Phase: d.Phase,
			}
		}),
		Runs: table(runs, func(r dataset.Run) bool {
			if !f.in(r.Project) {
				return false
			}
			if m == "" {
				return true
			}
			status, kind := runStatusTerms(r), runKindTerms(RunKind(r, ds.Meta.BuildTypeFrom))
			if isFlaky(r) {
				status += " " + flakyTerm.both()
			}
			return m.match(append([]*string{&r.PipelineName, r.Repo, r.CommitSHA, r.Status, &status, &kind, r.BuildType}, strs(r.ImageRefs)...)...)
		}, func(r dataset.Run) RunFact {
			return RunFact{
				PipelineName: r.PipelineName, RunID: r.RunID, Kind: RunKind(r, ds.Meta.BuildTypeFrom),
				CommitSHA: r.CommitSHA, DurationS: r.DurationS, ImageRefs: r.ImageRefs, Status: r.Status,
				Verdict: verdict(r), Outcome: runOutcome(r), GitOps: gitops(r), Flaky: isFlaky(r),
			}
		}),
	}
}
