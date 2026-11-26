package metrics

import (
	"cmp"
	"fmt"
	"net/url"
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
	// PRStateUnknown: a PR open at the end whose close may have been lost.
	PRStateUnknown: {"Unbekannt", "Unknown"},
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

// notCountedTerm is why a merged PR counts in no number, its label in the
// PR table; viaUmbrellaTerm, libraryTerm and infraTerm the rows' other
// marks.
var (
	notCountedTerm = map[string]uiTerm{
		NotCountedUmbrella:     {"Nicht gezählt: Merge eines Sammel-Branches", "Not counted: umbrella-branch merge"},
		NotCountedHousekeeping: {"Nicht gezählt: Aufräum-PR", "Not counted: housekeeping PR"},
		NotCountedTwice:        {"Nicht gezählt: Branch schon mit einem anderen PR gezählt", "Not counted: branch already counted with another PR"},
	}
	viaUmbrellaTerm = uiTerm{"über Sammel-Branch gemerged", "merged via an umbrella branch"}
	libraryTerm     = uiTerm{"Bibliothek, wird nicht ausgerollt", "Library, never rolled out"}
	infraTerm       = uiTerm{"Infrastruktur, nicht gezählt", "Infrastructure, not counted"}
)

// prMarks are a PR row's marks: why a merged PR is no merged work, whether
// one counted came in through an umbrella branch, whether it is a
// library's.
func prMarks(pr dataset.PR) (uncounted *string, via, library bool) {
	merged := pr.State == "merged" && !pr.GitOps
	if why := notCounted(pr); why != "" && merged {
		uncounted = &why
	}
	return uncounted, merged && uncounted == nil && (pr.OffTrunk || pr.ImplicitMerge), merged && pr.Library
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
// deployFrequency counts it in, or failed (Failed and Error alike, the
// phase its term). An App's first deploy counted as a release is a release
// found by the first deploy's words too.
func deployClass(d dataset.Deploy) (string, string) {
	if d.Phase != "Succeeded" {
		return DeployFailed, d.Phase
	}
	var class string
	switch revisionClass(d) {
	case classRelease:
		class = DeployRelease
	case classFirstRelease:
		return DeployRelease, deployTerm[DeployRelease].both() + " " + deployTerm[DeployFirstSeen].both()
	case classFirstSeen:
		class = DeployFirstSeen
	case classSync:
		class = DeploySync
	}
	return class, deployTerm[class].both()
}

// factDeploy is a deploy table row's deploy, classified on first use.
type factDeploy struct {
	deploy      dataset.Deploy
	class, term string
	classified  bool
}

func (x *factDeploy) classify() (string, string) {
	if !x.classified {
		x.class, x.term = deployClass(x.deploy)
		x.classified = true
	}
	return x.class, x.term
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

// prLink is the PR's page as its payload links it (the dataset drops it for
// a PR deleted since): an absolute http(s) URL without credentials, on the
// Bitbucket host when BACKEND_BITBUCKET_URL names one, else nil. The sender
// writes it and the UI puts it in an href: a team key must not plant a
// link to elsewhere under "open in Bitbucket".
func prLink(raw *string, bitbucket string) *string {
	if raw == nil {
		return nil
	}
	u, err := url.Parse(*raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return nil
	}
	if bitbucket != "" {
		b, err := url.Parse(bitbucket)
		if err != nil || u.Scheme != b.Scheme || !strings.EqualFold(u.Host, b.Host) {
			return nil
		}
	}
	s := u.String()
	return &s
}

// PRFact is one row of the PR table.
type PRFact struct {
	Repo string `json:"repo"`
	PRID int64  `json:"pr_id"`
	// URL is the PR in Bitbucket; null for a deleted PR or none sent.
	URL        *string     `json:"url"`
	Title      *string     `json:"title"`
	TitleParts []TitlePart `json:"title_parts"`
	JiraPrefix []JiraRef   `json:"jira_prefix"`
	JiraMore   int         `json:"jira_more"`
	GitOps     bool        `json:"gitops"`
	ChangeType *string     `json:"change_type"`
	// State is merged, open, declined or deleted, or unknown for a PR open
	// at the dataset's end whose close may have been lost
	// (dataset.OpenPR.Unknown): no age, no wait.
	State string `json:"state"`
	// NotCounted is why a merged PR is in no PR number (notCounted:
	// umbrella, housekeeping); null when it counts, and for every other PR.
	NotCounted *string `json:"not_counted"`
	// ViaUmbrella: a merged PR that went into a feature or umbrella branch,
	// or that an umbrella's merge brought in (dataset.PR.OffTrunk,
	// ImplicitMerge). Counted all the same; the row says how it was merged.
	ViaUmbrella bool `json:"via_umbrella"`
	// Library: a merged PR of a repo never rolled out (dataset.PR.Library):
	// no production deploy is no gap.
	Library bool `json:"library"`
	// ReviewS is ready to approval, CycleS opened to merged, ToProdS merged
	// to the first production deploy; null when unknown or negative.
	ReviewS  *float64   `json:"review_s"`
	CycleS   *float64   `json:"cycle_s"`
	MergedAt *time.Time `json:"merged_at"`
	ProdAt   *time.Time `json:"prod_at"`
	ToProdS  *float64   `json:"to_prod_s"`
	// ProdState is where a merged PR stands on production now, the facts'
	// end (prodPlacement): on_prod, not_yet or not_placeable; null for
	// every other PR, a library's and a promotion's included. ProdReason
	// is why tracing cannot place it (no_merge_commit, release_unresolved,
	// repo_untraced, in_silence), null otherwise.
	ProdState  *string `json:"prod_state"`
	ProdReason *string `json:"prod_reason"`
	// ProdRoute is how its first production deploy was found: compare (a
	// compare link in the release commit) or image (the run that built a
	// deployed image); null when not on production. ProdLowerBound: that
	// deploy's stretch started at a lower bound, the PR may have gone out
	// earlier with a deploy tracing could not resolve.
	ProdRoute      *string `json:"prod_route"`
	ProdLowerBound bool    `json:"prod_lower_bound"`
	// AgeS and Waiting are a PR open at the dataset's end (now on the page),
	// as the open-PR section counts it: its age then and
	// what it waits on, inactive when idle OpenInactiveDays; IdleS the time
	// since its last event. Null for every other PR.
	AgeS    *float64 `json:"age_s"`
	Waiting *string  `json:"waiting"`
	IdleS   *float64 `json:"idle_s"`
	// Running: merged or closed, else opened, in the week running now, so counted
	// in no number of the page. Never an open PR: those are a snapshot.
	Running bool `json:"running"`
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
	// FirstDeploy: the App's first known deploy naming images (Kind
	// first_seen), whether Class counts it a release (more than
	// dataset.FirstDeployGrace after Argo CD first reported) or first_seen.
	FirstDeploy bool `json:"first_deploy"`
	// Infra: an infrastructure App's deploy (dataset.Deploy.Infra): listed,
	// counted nowhere.
	Infra bool `json:"infra"`
	// Running: deployed in the week running now, counted nowhere.
	Running bool `json:"running"`
}

// runningTerm is the mark on a row of the running week, searched in both
// languages.
var runningTerm = uiTerm{"Laufende Woche", "This week"}

// inRunning reports whether t is set and in the week running after the
// period's end: the facts read on to now, the numbers stop at the period.
func inRunning(t *time.Time, p Period) bool { return t != nil && !t.Before(p.To) }

// RunFact is one row of the run table.
type RunFact struct {
	PipelineName string  `json:"pipeline_name"`
	RunID        string  `json:"run_id"`
	Kind         string  `json:"kind"`
	CommitSHA    *string `json:"commit_sha"`
	// ShownSHA is the commit the row shows (shownSHA): a PR build's own head
	// where CommitSHA is a merge the CI made of it, else CommitSHA.
	ShownSHA  *string  `json:"shown_sha"`
	DurationS *int64   `json:"duration_s"`
	ImageRefs []string `json:"image_refs"`
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
	// Running: its COMPLETED event arrived in the week running now, as the
	// windows split runs: counted nowhere.
	Running bool `json:"running"`
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

// newestFirst sorts rows in place by an instant, newest first, rows without
// one last; ties keep the dataset's order. at returns the instant by value:
// a pointer into the compared row would move every row to the heap.
func newestFirst[T any](rows []T, at func(T) (time.Time, bool)) {
	slices.SortStableFunc(rows, func(a, b T) int {
		x, xok := at(a)
		y, yok := at(b)
		switch {
		case !xok && !yok:
			return 0
		case !xok:
			return 1
		case !yok:
			return -1
		}
		return y.Compare(x)
	})
}

// instant is t's value, and whether there is one.
func instant(t *time.Time) (time.Time, bool) {
	if t == nil {
		return time.Time{}, false
	}
	return *t, true
}

// table counts the rows keep takes and builds the first FactsLimit. Both get
// the row's address, so a row can classify itself once for the two.
func table[S, T any](rows []S, keep func(*S) bool, row func(*S) T) Table[T] {
	t := Table[T]{Rows: []T{}}
	for i := range rows {
		r := &rows[i]
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
	// unknown: open at the end, its state unknown (no waiting, no age).
	unknown bool
}

// PRStateUnknown is the PR table's state of a PR open at the dataset's end
// whose close may have been lost (dataset.OpenPR.Unknown).
const PRStateUnknown = "unknown"

// factPRs are the PRs open at the dataset's end first, oldest first, the
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
		pr dataset.OpenPR
		// inactive also for an unknown one: both after the PRs waiting.
		inactive bool
	}
	open := make([]openRow, len(ds.OpenPRs))
	for i, o := range ds.OpenPRs {
		open[i] = openRow{o, o.Unknown || openInactive(o, at)}
	}
	// Oldest first by the age the row shows, since its last turn ready; the
	// drafts, which have none, after the ready ones, by their opening.
	since := func(o dataset.OpenPR) time.Time {
		if s := o.ReadySince; s != nil && !o.Draft {
			return *s
		}
		return o.OpenedAt
	}
	slices.SortStableFunc(open, func(a, b openRow) int {
		return cmp.Or(cmp.Compare(first(!a.inactive), first(!b.inactive)),
			cmp.Compare(first(!a.pr.Draft), first(!b.pr.Draft)), since(a.pr).Compare(since(b.pr)))
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
				Repo: o.Repo, Project: o.Project, PRID: o.PRID, Title: o.Title, URL: o.URL, Bot: o.Bot,
				ChangeType: o.ChangeType, JiraKeys: o.JiraKeys, OpenedAt: &opened, ReadyAt: o.ReadyAt,
				Environments: []dataset.EnvironmentDeploy{},
			}
		}
		// Open then, whatever happened to it after the period's end, and
		// filtered as the open-PR numbers count it.
		pr.State = "open"
		pr.Bot, pr.Title, pr.JiraKeys = o.Bot, o.Title, o.JiraKeys
		// The window's link is the newest of the PR's whole history.
		pr.URL = cmp.Or(pr.URL, o.URL)
		if o.Unknown {
			pr.State = PRStateUnknown
			out = append(out, factPR{pr: pr, idle: ptr(openIdle(o, at)), unknown: true})
			continue
		}
		on := waitsOn(o)
		if row.inactive {
			on = WaitInactive
		}
		out = append(out, factPR{pr: pr, age: openAge(o, at), waiting: &on, idle: ptr(openIdle(o, at))})
	}
	rest := slices.Clone(ds.PRs)
	newestFirst(rest, func(pr dataset.PR) (time.Time, bool) { return instant(cmp.Or(pr.MergedAt, pr.OpenedAt)) })
	for _, pr := range rest {
		if !seen[key{pr.Repo, pr.PRID}] {
			out = append(out, factPR{pr: pr})
		}
	}
	return out
}

// factRun is a run table row's run, its kind classified on first use.
type factRun struct {
	run  dataset.Run
	kind string
}

func (x *factRun) runKind(buildTypeFrom *time.Time) string {
	if x.kind == "" {
		x.kind = RunKind(x.run, buildTypeFrom)
	}
	return x.kind
}

// ComputeFacts is the facts tables for the search q. The product filters as
// given: the facts read no lead times or reviews, so their products are not
// all of the page's; the UI sends the product /api/metrics resolved.
func ComputeFacts(ds *dataset.Dataset, p Params, q string) Facts {
	f := filter{product: p.Product}
	m := newMatcher(q)
	prod := ds.Meta.ProductionStage

	prs := factPRs(ds)
	// The dataset reads on to now, the period ends with the last closed week.
	// Each row is marked by the instant its window took it on: a PR by its
	// merge or close (else its opening), a deploy and a run by the event's
	// arrival.
	prRunning := func(x *factPR) bool {
		return x.waiting == nil && !x.unknown && inRunning(cmp.Or(x.pr.MergedAt, x.pr.ClosedAt, x.pr.OpenedAt), p.Period)
	}
	runRunning := func(r dataset.Run) bool { return inRunning(&r.OccurredAt, p.Period) }
	deploys := make([]factDeploy, len(ds.Deploys))
	for i, d := range ds.Deploys {
		deploys[i].deploy = d
	}
	newestFirst(deploys, func(x factDeploy) (time.Time, bool) { return x.deploy.At, true })
	runs := make([]factRun, len(ds.Runs))
	for i, r := range ds.Runs {
		runs[i].run = r
	}
	newestFirst(runs, func(x factRun) (time.Time, bool) { return instant(runAt(x.run)) })
	gitops := gitopsRun(ds)
	// As the build section counts them: its runs, its pairing. A run of the
	// period pairs with the period's runs only, as the build section saw
	// them; a rerun after the period's end does not make it flaky there.
	closed := *ds
	closed.Runs = slices.DeleteFunc(slices.Clone(ds.Runs), runRunning)
	flakyClosed := flakyRuns(teamRuns(&closed, f, gitops, nil))
	flakyAll := flakyRuns(teamRuns(ds, f, gitops, nil))
	isFlaky := func(r dataset.Run) bool {
		if verdict(r) != VerdictFailure {
			return false
		}
		if runRunning(r) {
			return flakyAll[refOf(r)]
		}
		return flakyClosed[refOf(r)]
	}
	// The mark's own word as the whole search lists exactly the marked rows:
	// a title saying "this week" is no row of the running week.
	onlyRunning := m != "" && (string(m) == strings.ToLower(runningTerm.De) || string(m) == strings.ToLower(runningTerm.En))

	return Facts{
		ProductionStage: prod,
		Limit:           FactsLimit,
		// The search reads the words on screen, never a hidden code: a
		// promotion's change type is not searched. The words are built only
		// when there is a search.
		PRs: table(prs, func(x *factPR) bool {
			pr := x.pr
			if !f.inKey(pr.Project) || pr.Bot {
				return false
			}
			if m == "" || onlyRunning {
				return m == "" || prRunning(x)
			}
			term, typ := prStateTerms(pr.State), prTypeTerm(pr)
			if x.waiting != nil {
				term += " " + waitTerm[*x.waiting].both()
			}
			uncounted, via, library := prMarks(pr)
			if uncounted != nil {
				term += " " + notCountedTerm[*uncounted].both()
			}
			if via {
				term += " " + viaUmbrellaTerm.both()
			}
			if library {
				term += " " + libraryTerm.both()
			}
			if prRunning(x) {
				term += " " + runningTerm.both()
			}
			id := fmt.Sprintf("#%d", pr.PRID)
			return m.match(append([]*string{&pr.Repo, pr.Title, &typ, &id}, append(strs(pr.JiraKeys), &term)...)...)
		}, func(x *factPR) PRFact {
			pr := x.pr
			prodAt := firstDeploy(pr, prod)
			parts := titleParts(pr.Title, p.JiraURL)
			keys, more := jiraPrefix(pr.JiraKeys, parts, p.JiraURL)
			uncounted, via, library := prMarks(pr)
			var state, reason, route *string
			lowerBound := false
			if pr.State == "merged" && !pr.GitOps && !pr.Bot && !library {
				pl := prodPlacement(ds, pr, ds.Meta.To)
				state, lowerBound = ptr(pl.state), pl.lowerBound
				if pl.reason != "" {
					reason = ptr(pl.reason)
				}
				if pl.route != "" {
					route = ptr(pl.route)
				}
			}
			return PRFact{
				ProdState: state, ProdReason: reason, ProdRoute: route, ProdLowerBound: lowerBound,
				Repo: pr.Repo, PRID: pr.PRID, URL: prLink(pr.URL, p.BitbucketURL), Title: pr.Title, TitleParts: parts,
				JiraPrefix: keys, JiraMore: more,
				GitOps: pr.GitOps, ChangeType: pr.ChangeType,
				State: pr.State, NotCounted: uncounted,
				ViaUmbrella: via,
				Library:     library,
				ReviewS:     wait(pr.ReadyAt, pr.ApprovedAt), CycleS: wait(pr.OpenedAt, pr.MergedAt),
				MergedAt: pr.MergedAt, ProdAt: prodAt, ToProdS: wait(pr.MergedAt, prodAt),
				AgeS: x.age, Waiting: x.waiting, IdleS: x.idle, Running: prRunning(x),
			}
		}),
		// Every image a deploy ran, not only the new ones: a search for an
		// image finds its re-syncs and failed syncs too.
		Deploys: table(deploys, func(x *factDeploy) bool {
			d := &x.deploy
			if !f.in(d.Project) {
				return false
			}
			if m == "" || onlyRunning {
				return m == "" || inRunning(&d.At, p.Period)
			}
			_, term := x.classify()
			if inRunning(&d.At, p.Period) {
				term += " " + runningTerm.both()
			}
			if d.Infra {
				term += " " + infraTerm.both()
			}
			return m.match(append([]*string{&d.App, d.Environment, &d.Revision}, append(strs(d.Images), &term)...)...)
		}, func(x *factDeploy) DeployFact {
			d := &x.deploy
			class, _ := x.classify()
			return DeployFact{
				At: d.At, App: d.App, Environment: d.Environment, Revision: d.Revision,
				NewImages: d.NewImages, Class: class, Phase: d.Phase,
				FirstDeploy: d.Kind != nil && *d.Kind == "first_seen", Infra: d.Infra, Running: inRunning(&d.At, p.Period),
			}
		}),
		Runs: table(runs, func(x *factRun) bool {
			r := x.run
			if !f.in(r.Project) {
				return false
			}
			if m == "" || onlyRunning {
				return m == "" || runRunning(r)
			}
			status, kind := runStatusTerms(r), runKindTerms(x.runKind(ds.Meta.BuildTypeFrom))
			if isFlaky(r) {
				status += " " + flakyTerm.both()
			}
			if runRunning(r) {
				status += " " + runningTerm.both()
			}
			return m.match(append([]*string{&r.PipelineName, r.Repo, r.CommitSHA, r.HeadSHA, r.BaseSHA, r.Status, &status, &kind, r.BuildType}, strs(r.ImageRefs)...)...)
		}, func(x *factRun) RunFact {
			r := x.run
			return RunFact{
				PipelineName: r.PipelineName, RunID: r.RunID, Kind: x.runKind(ds.Meta.BuildTypeFrom),
				CommitSHA: r.CommitSHA, ShownSHA: shownSHA(r), DurationS: r.DurationS, ImageRefs: r.ImageRefs, Status: r.Status,
				Verdict: verdict(r), Outcome: runOutcome(r), GitOps: gitops(r), Flaky: isFlaky(r),
				Running: runRunning(r),
			}
		}),
	}
}
