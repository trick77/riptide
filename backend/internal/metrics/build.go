package metrics

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/parse"
)

// Run kinds: what a run was, so durations and success rates are never
// averaged across kinds (a release skips tests, a feature build fails more
// often by nature).
const (
	KindRelease = "release"
	KindMain    = "main"
	KindFeature = "feature"
	KindOther   = "other"
	KindUnknown = "unknown"
)

// runKinds in the order the UI shows them: feature first, the builds
// developers wait on, where their time goes.
var runKinds = []string{KindFeature, KindMain, KindRelease, KindOther, KindUnknown}

// PrefixTop is how many branch prefixes the other kind lists.
const PrefixTop = 5

// BranchPrefix is a branch scheme among the other kind's runs, so a team
// sees which of its feature builds riptide does not recognise.
type BranchPrefix struct {
	// Prefix is up to the first '/' ("release/"), else up to the first '-'
	// ("kas-…", a Jira key's project or a person's name), else the branch
	// ("nightly"), lowercased; "" for a pipeline name without a branch.
	Prefix string `json:"prefix"`
	Runs   int    `json:"runs"`
}

// branchPrefix is a branch's scheme: kas-12-login and KAS-13-cart are one.
// Leading separators are no scheme: /kas-1-x is kas-….
func branchPrefix(branch string) string {
	b := strings.TrimLeft(strings.ToLower(branch), "/-")
	if i := strings.Index(b, "/"); i > 0 {
		return b[:i+1]
	}
	if i := strings.Index(b, "-"); i > 0 {
		return b[:i] + "-…"
	}
	return b
}

// notFeatureStems are schemes that are no developer's work in progress,
// with a '/' or a '-' alike: release/1.2 and release-1.2, the dependency
// bots, a long-lived integration branch.
var notFeatureStems = map[string]bool{
	"release": true, "renovate": true, "dependabot": true, "snyk": true,
	"develop": true, "development": true,
}

// notFeature: an other-kind scheme whose runs are no sign of unrecognised
// feature builds. A pipeline name without a branch (Tekton) says nothing
// either way. docs/ and doc/ only with their '/' (parse.DocsBranch): a
// DOC-123 Jira key is a feature scheme.
func notFeature(prefix string) bool {
	stem := strings.TrimSuffix(strings.TrimSuffix(prefix, "/"), "-…")
	return stem == "" || notFeatureStems[stem] || strings.Contains(stem, "nightly") ||
		parse.DocsBranch(prefix)
}

// FeatureHint says no run counted as a feature build, while other runs on
// branches that look like work in progress did. Their scheme is most likely
// one BranchKind does not know; the other kind's prefixes name it.
type FeatureHint struct {
	// Runs are those other runs, with a verdict.
	Runs int `json:"runs"`
}

// hint fills in what only the shown period needs: the other kind's
// prefixes and the feature hint, both over runs with a verdict. A feature
// kind of aborted runs only is no recognised feature build.
func (b *Build) hint(by map[string][]dataset.Run) {
	n := map[string]int{}
	candidates := 0
	for _, r := range by[KindOther] {
		if verdict(r) == VerdictNone {
			continue
		}
		p := branchPrefix(r.Branch)
		n[p]++
		if !notFeature(p) {
			candidates++
		}
	}
	out := []BranchPrefix{}
	for p, c := range n {
		out = append(out, BranchPrefix{Prefix: p, Runs: c})
	}
	slices.SortFunc(out, func(a, b BranchPrefix) int {
		return cmp.Or(cmp.Compare(b.Runs, a.Runs), cmp.Compare(a.Prefix, b.Prefix))
	})
	featureRuns := 0
	for i := range b.Kinds {
		switch b.Kinds[i].Kind {
		case KindOther:
			b.Kinds[i].Prefixes = out[:min(len(out), PrefixTop)]
		case KindFeature:
			featureRuns = b.Kinds[i].Runs
		}
	}
	if featureRuns == 0 && candidates > 0 {
		b.FeatureHint = &FeatureHint{Runs: candidates}
	}
}

// runAt is when a run happened: its end, else its start.
func runAt(r dataset.Run) *time.Time {
	if r.FinishedAt != nil {
		return r.FinishedAt
	}
	return r.StartedAt
}

// RunKind classifies a run. Before the pipelines reported a build_type, a
// main-branch run may have been a release: unknown, not a main build.
func RunKind(r dataset.Run, buildTypeFrom *time.Time) string {
	t := ""
	if r.BuildType != nil {
		t = strings.ToLower(*r.BuildType)
	}
	if t == "release" || t == "hotfix" {
		return KindRelease
	}
	if r.BuildType == nil && r.BranchKind == dataset.BranchMain {
		at := runAt(r)
		if buildTypeFrom == nil || (at != nil && at.Before(*buildTypeFrom)) {
			return KindUnknown
		}
	}
	switch r.BranchKind {
	case dataset.BranchMain:
		return KindMain
	case dataset.BranchFeature, dataset.BranchPR:
		return KindFeature
	}
	return KindOther
}

// Verdicts: what a run's status says about the build, across CIs.
const (
	VerdictSuccess = "success"
	VerdictFailure = "failure"
	// VerdictNone: the run ended without saying whether the build works:
	// ABORTED (a superseded build is cancelled, not broken), NOT_BUILT,
	// Tekton's None. Neither a failure nor a run; only its CI time counts.
	VerdictNone = "none"
)

// verdict reads Jenkins' SUCCESS / FAILURE / UNSTABLE and Tekton's
// Succeeded / Completed (succeeded, some tasks skipped) / Failed, as
// docs/setup-*-notification.md have them send it; anything else is none.
func verdict(r dataset.Run) string {
	if r.Status == nil {
		return VerdictNone
	}
	switch strings.ToUpper(*r.Status) {
	case "SUCCESS", "SUCCEEDED", "COMPLETED":
		return VerdictSuccess
	case "FAILURE", "UNSTABLE", "FAILED":
		return VerdictFailure
	}
	return VerdictNone
}

// seconds is r's CI time, none without a duration (teamRuns drops a
// negative one).
func seconds(r dataset.Run) float64 {
	if r.DurationS == nil {
		return 0
	}
	return float64(*r.DurationS)
}

// agentSplit splits a run at the moment it got its build agent: the wait
// for one, and the build itself. ok is false when the CI did not report
// that moment, or reported one outside the run (clock skew); such a run has
// neither, never a wait of 0.
func agentSplit(r dataset.Run) (wait, work float64, ok bool) {
	a, s, f := r.AgentStartedAt, r.StartedAt, r.FinishedAt
	if a == nil || s == nil || f == nil || a.Before(*s) || a.After(*f) {
		return 0, 0, false
	}
	return a.Sub(*s).Seconds(), f.Sub(*a).Seconds(), true
}

// repoNamer names a run's repository: the one Bitbucket saw its commit in,
// else the one whose name its pipeline starts with, else that segment
// itself. A run of an unseen commit thus counts with its repository's
// other runs, not as a repository of its own.
type repoNamer func(dataset.Run) string

func newRepoNamer(runs []dataset.Run) repoNamer {
	byName := map[string]string{}
	ambiguous := map[string]bool{}
	for _, r := range runs {
		if r.Repo == nil {
			continue
		}
		_, name, ok := strings.Cut(*r.Repo, "/")
		if !ok {
			continue
		}
		if prev, seen := byName[name]; seen && prev != *r.Repo {
			ambiguous[name] = true
		}
		byName[name] = *r.Repo
	}
	return func(r dataset.Run) string {
		if r.Repo != nil {
			return *r.Repo
		}
		head, _, _ := strings.Cut(r.PipelineName, "/")
		// The same name in two projects: no telling which.
		if repo, ok := byName[head]; ok && !ambiguous[head] {
			return repo
		}
		return head
	}
}

// BuildStats is one kind of run over the period. A run without a verdict
// (ABORTED, NOT_BUILT, ...) is neither a failure nor a run, as in the
// hotspots: it counts only in NoVerdict and the CI time it took, so Runs is
// always Succeeded plus Failed.
type BuildStats struct {
	Kind      string `json:"kind"`
	Runs      int    `json:"runs"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
	NoVerdict int    `json:"no_verdict"`
	// SuccessRate is Succeeded over Runs.
	SuccessRate *float64 `json:"success_rate"`
	// Duration is over Runs, outliers aside: an aborted run's is no build
	// time, nor is a day spent waiting for an agent.
	Duration Spread `json:"duration"`
	// Waiting and Building split Duration's runs at the moment each got its
	// build agent: the wait for one, which grows with cluster load, and the
	// build itself. Only over the runs whose CI reports that moment, so
	// their N may be well below Duration's.
	Waiting  Spread `json:"waiting"`
	Building Spread `json:"building"`
	// CISeconds is every run's time, those without a verdict included,
	// outliers aside.
	CISeconds float64 `json:"ci_seconds"`
	// WastedSeconds is the time of the failed runs, outliers aside.
	WastedSeconds float64 `json:"wasted_seconds"`
	// OutlierRuns are the runs of the kind past its outlier threshold, with
	// or without a verdict; they still count in Runs and the success rate.
	OutlierRuns int `json:"outlier_runs"`
	// OutlierSeconds is their time, kept out of every other duration.
	OutlierSeconds float64 `json:"outlier_seconds"`
	// Weekly is the success rate per week, null for a week without runs.
	Weekly []*float64 `json:"weekly"`
	// Prefixes are the other kind's PrefixTop most frequent branch schemes
	// over Runs; empty for every other kind and in the previous period.
	Prefixes []BranchPrefix `json:"prefixes"`

	// The kind's time split by outcome without overlap, an outlier only
	// ever counting as an outlier whatever its verdict: succeeded +
	// WastedSeconds + noVerdict = CISeconds, plus OutlierSeconds = total.
	// Sent once, as Build.CIFlow.
	total, succeeded, noVerdict float64
}

// tally counts runs by verdict; runs are those with one.
type tally struct{ runs, succeeded, failed, none int }

// add counts r and returns its verdict.
func (t *tally) add(r dataset.Run) string { return t.count(verdict(r)) }

// count counts a run of verdict v and returns it.
func (t *tally) count(v string) string {
	switch v {
	case VerdictSuccess:
		t.runs++
		t.succeeded++
	case VerdictFailure:
		t.runs++
		t.failed++
	default:
		t.none++
	}
	return v
}

// Build is the CI section.
type Build struct {
	// DataFrom is when riptide first received a run of the team.
	DataFrom *time.Time `json:"data_from"`
	// StartsInside: DataFrom falls after the period's start.
	StartsInside bool `json:"starts_inside"`
	// GapWeeks are the leading weeks before DataFrom, empty rather than
	// wrong; all of them without any run data.
	GapWeeks int          `json:"gap_weeks"`
	Kinds    []BuildStats `json:"kinds"`
	// CIFlow is Kinds' CI time from kind to outcome.
	CIFlow   CIFlow   `json:"ci_flow"`
	Hotspots Hotspots `json:"hotspots"`
	// FeatureHint is nil unless feature builds look unrecognised.
	FeatureHint *FeatureHint `json:"feature_hint"`
	// repos are every repo's runs per kind, the next period's baseline;
	// compared the repos per kind with a previous period to compare with.
	// Both before the hotspots' cut to the top.
	repos    map[string]map[string]*repoRuns
	compared map[string]int
}

// gitopsRun tells a run in a GitOps repo, a stage promotion's check: flagged
// by its commit, or, without one Bitbucket saw, named after one of the
// dataset's GitOps repos.
func gitopsRun(ds *dataset.Dataset) func(dataset.Run) bool {
	repos, short, ambiguous := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, repo := range ds.GitOpsRepos {
		repos[repo] = true
		if _, name, ok := strings.Cut(repo, "/"); ok {
			ambiguous[name] = short[name]
			short[name] = true
		}
	}
	name := newRepoNamer(ds.Runs)
	return func(r dataset.Run) bool {
		if r.GitOps {
			return true
		}
		n := name(r)
		// Unresolved, the namer answers the pipeline's head: a GitOps repo's
		// name when no other project has one like it.
		return repos[n] || (r.Repo == nil && short[n] && !ambiguous[n])
	}
}

// teamRuns are the runs the build section counts: with a status, in the
// product, GitOps repos' runs left out (stage promotions, mechanical).
func teamRuns(ds *dataset.Dataset, f filter) map[string][]dataset.Run {
	by := map[string][]dataset.Run{}
	gitops := gitopsRun(ds)
	for _, r := range ds.Runs {
		if r.Status == nil || gitops(r) || !f.in(r.Project) {
			continue
		}
		// A finish before the start (clock skew) is no duration: read once
		// here, so CI time, its split, medians and outliers all agree.
		if r.DurationS != nil && *r.DurationS < 0 {
			r.DurationS = nil
		}
		k := RunKind(r, ds.Meta.BuildTypeFrom)
		by[k] = append(by[k], r)
	}
	return by
}

// baseline is what each repo's flags compare with: the previous period's
// runs of every repo per kind, not the hotspots' cut to the top.
type baseline struct {
	// repos names the runs of both periods alike.
	repos repoNamer
	// kinds is Build.repos of the previous period; nil while computing it.
	kinds map[string]map[string]*repoRuns
	// incomplete: the previous period is not covered by the run data, or
	// its runs fall into other kinds (see buildsIncomplete).
	incomplete bool
	// previous: computing the previous period itself, which nobody draws,
	// so no heatmap.
	previous bool
}

// build is the CI section. base names the repos and holds the previous
// period's runs; nil names them by ds alone and compares nothing.
func build(ds *dataset.Dataset, p Period, f filter, base *baseline) Build {
	by := teamRuns(ds, f)
	out := Build{
		DataFrom: ds.Meta.BuildDataFrom, Kinds: []BuildStats{},
		repos: map[string]map[string]*repoRuns{}, compared: map[string]int{},
	}
	if from := ds.Meta.BuildDataFrom; from != nil {
		out.StartsInside = from.After(p.From)
		for _, w := range p.Weeks {
			if !w.End.After(*from) {
				out.GapWeeks++
			}
		}
	} else {
		out.GapWeeks = len(p.Weeks)
	}
	if base == nil {
		base = &baseline{repos: newRepoNamer(ds.Runs)}
	}
	out.Hotspots = Hotspots{
		MinRuns: HotspotMinRuns, SlowFactor: SlowFactor, FlakyFactor: FlakyFactor,
		FlakyMinPoints: FlakyMinPoints, FlakyMinFailures: FlakyMinFailures,
		OutlierFactor: OutlierFactor, OutlierMinS: OutlierMinS, OutlierRepoMinRuns: OutlierRepoMinRuns,
		SlowestMinRuns: SlowestMinRuns, Kinds: []KindHotspots{}, Outliers: []Outlier{}, OutlierGroups: []OutlierGroup{},
		HeatMinRuns: HeatMinRuns, HeatSteps: slices.Clone(HeatSteps), HeatClasses: heatClasses(),
	}
	for _, kind := range runKinds {
		rs := by[kind]
		if len(rs) == 0 {
			continue
		}
		kr := newKindRuns(rs, base.repos)
		b := BuildStats{Kind: kind, Prefixes: []BranchPrefix{}}
		var t tally
		var durations, waits, works []float64
		for i, r := range rs {
			v := t.count(kr.verdicts[i])
			if kr.stuck[i] {
				b.OutlierRuns++
				b.OutlierSeconds += seconds(r)
				continue
			}
			b.CISeconds += seconds(r)
			switch v {
			case VerdictNone:
				b.noVerdict += seconds(r)
				continue
			case VerdictFailure:
				b.WastedSeconds += seconds(r)
			default:
				b.succeeded += seconds(r)
			}
			if r.DurationS != nil {
				durations = append(durations, float64(*r.DurationS))
				if wait, work, ok := agentSplit(r); ok {
					waits = append(waits, wait)
					works = append(works, work)
				}
			}
		}
		b.total = b.CISeconds + b.OutlierSeconds
		b.Runs, b.Succeeded, b.Failed, b.NoVerdict = t.runs, t.succeeded, t.failed, t.none
		b.SuccessRate = ratio(b.Succeeded, b.Runs)
		b.Duration = spread(durations)
		b.Waiting, b.Building = spread(waits), spread(works)
		for _, w := range bucket(p, rs, runAt) {
			var wt tally
			for _, r := range w {
				wt.add(r)
			}
			b.Weekly = append(b.Weekly, ratio(wt.succeeded, wt.runs))
		}
		out.Kinds = append(out.Kinds, b)
		repos := kr.byRepo()
		out.repos[kind] = repos
		k, outliers, compared := kindHotspots(kind, kr, repos, base)
		k.Heatmap = []HeatRow{}
		if !base.previous {
			k.Heatmap = heatmap(p, kr, k.Repos, out.GapWeeks)
		}
		out.compared[kind] = compared
		out.Hotspots.Kinds = append(out.Hotspots.Kinds, k)
		out.Hotspots.Outliers = append(out.Hotspots.Outliers, outliers...)
	}
	// The previous period's flow is never drawn: no repo split for it.
	repos := out.repos
	if base.previous {
		repos = nil
	}
	out.CIFlow = ciFlow(out.Kinds, repos)
	if !base.previous {
		out.hint(by)
	}
	o := out.Hotspots.Outliers
	// Grouped before the cut to the longest: a short stuck run still tells
	// that the agents were gone.
	out.Hotspots.OutlierGroups = outlierGroups(o)
	out.Hotspots.OutlierCount = len(o)
	slices.SortStableFunc(o, func(a, b Outlier) int { return cmp.Compare(b.DurationS, a.DurationS) })
	out.Hotspots.Outliers = o[:min(len(o), outlierTop)]
	return out
}

// Flow outcomes (the spec's FlowOutcome): where a run's CI time went, each
// second in exactly one. Not the facts' run outcomes: UNSTABLE is failed CI
// time here.
const (
	FlowSucceeded = "succeeded"
	// FlowFailed is BuildStats.WastedSeconds: FAILURE, UNSTABLE, Failed.
	FlowFailed = "failed"
	// FlowNoVerdict: ABORTED, NOT_BUILT, Tekton's None, a Jenkins
	// timeout().
	FlowNoVerdict = "no_verdict"
	// FlowOutlier: a run past its kind's outlier threshold, whatever its
	// verdict.
	FlowOutlier = "outlier"
)

// CIFlow is the CI time of every kind by outcome, outliers included. Hours
// add up across kinds, so the outcomes total them; a share of a kind is of
// that kind's own time only, never compared with another kind's.
type CIFlow struct {
	// TotalSeconds is every kind's time, outliers included.
	TotalSeconds float64 `json:"total_seconds"`
	// Kinds are those with time, in Build.Kinds' order: the left nodes,
	// each its CISeconds plus OutlierSeconds.
	Kinds []FlowKind `json:"kinds"`
	// Outcomes are those with time, in the order succeeded, failed,
	// no_verdict, outlier: the right nodes.
	Outcomes []FlowOutcomeShare `json:"outcomes"`
	// Links are the kinds' outcomes with time, by kind as Kinds, then by
	// outcome as Outcomes.
	Links []FlowLink `json:"links"`
	// Repos are the FlowTopRepos repos with the most time across kinds,
	// outliers included like every other number here; the rest fold into
	// Other*. No verdict: more CI time can be more work.
	Repos        []FlowRepo `json:"repos"`
	OtherRepos   int        `json:"other_repos"`
	OtherSeconds float64    `json:"other_seconds"`
	OtherFailed  float64    `json:"other_failed"`
}

// FlowTopRepos are the repos CIFlow names; the rest fold into one.
const FlowTopRepos = 3

// FlowRepo is one repo's time across kinds, outliers included; Failed is
// its part in the failed outcome.
type FlowRepo struct {
	Repo    string  `json:"repo"`
	Seconds float64 `json:"seconds"`
	Failed  float64 `json:"failed"`
}

// FlowKind is one kind's time, all its outcomes.
type FlowKind struct {
	Kind    string  `json:"kind"`
	Seconds float64 `json:"seconds"`
	// Share is of CIFlow.TotalSeconds.
	Share float64 `json:"share"`
}

// FlowOutcomeShare is one outcome's time across kinds.
type FlowOutcomeShare struct {
	Outcome string  `json:"outcome"`
	Seconds float64 `json:"seconds"`
	// Share is of CIFlow.TotalSeconds.
	Share float64 `json:"share"`
}

// FlowLink is one kind's time with one outcome.
type FlowLink struct {
	Kind    string  `json:"kind"`
	Outcome string  `json:"outcome"`
	Seconds float64 `json:"seconds"`
	// ShareOfKind is of the kind's FlowKind.Seconds.
	ShareOfKind float64 `json:"share_of_kind"`
	// Share is of CIFlow.TotalSeconds.
	Share float64 `json:"share"`
}

// outcomeSeconds is b's total split by outcome, in CIFlow's order.
func outcomeSeconds(b BuildStats) [4]float64 {
	return [4]float64{b.succeeded, b.WastedSeconds, b.noVerdict, b.OutlierSeconds}
}

var flowOutcomes = [4]string{FlowSucceeded, FlowFailed, FlowNoVerdict, FlowOutlier}

func ciFlow(kinds []BuildStats, repos map[string]map[string]*repoRuns) CIFlow {
	f := CIFlow{Kinds: []FlowKind{}, Outcomes: []FlowOutcomeShare{}, Links: []FlowLink{}, Repos: []FlowRepo{}}
	var sums [4]float64
	for _, b := range kinds {
		f.TotalSeconds += b.total
		for i, s := range outcomeSeconds(b) {
			sums[i] += s
		}
	}
	// No time to split: no run with a duration.
	if f.TotalSeconds <= 0 {
		return f
	}
	for i, s := range sums {
		if s > 0 {
			f.Outcomes = append(f.Outcomes, FlowOutcomeShare{Outcome: flowOutcomes[i], Seconds: s, Share: s / f.TotalSeconds})
		}
	}
	for _, b := range kinds {
		if b.total <= 0 {
			continue
		}
		f.Kinds = append(f.Kinds, FlowKind{Kind: b.Kind, Seconds: b.total, Share: b.total / f.TotalSeconds})
		for i, s := range outcomeSeconds(b) {
			if s > 0 {
				f.Links = append(f.Links, FlowLink{
					Kind: b.Kind, Outcome: flowOutcomes[i], Seconds: s,
					ShareOfKind: s / b.total, Share: s / f.TotalSeconds,
				})
			}
		}
	}
	by := map[string]*FlowRepo{}
	for _, kind := range repos {
		for repo, rr := range kind {
			if by[repo] == nil {
				by[repo] = &FlowRepo{Repo: repo}
			}
			by[repo].Seconds += rr.ci + rr.stuck
			by[repo].Failed += rr.wasted
		}
	}
	all := make([]FlowRepo, 0, len(by))
	for _, r := range by {
		if r.Seconds > 0 {
			all = append(all, *r)
		}
	}
	slices.SortFunc(all, func(x, y FlowRepo) int { return cmp.Or(cmp.Compare(y.Seconds, x.Seconds), cmp.Compare(x.Repo, y.Repo)) })
	for _, r := range all {
		// A run without a repo name has no row of its own: it folds.
		if len(f.Repos) < FlowTopRepos && r.Repo != "" {
			f.Repos = append(f.Repos, r)
			continue
		}
		f.OtherRepos++
		f.OtherSeconds += r.Seconds
		f.OtherFailed += r.Failed
	}
	return f
}

// kindRuns are one kind's runs, each with its repo, its verdict and whether
// it is an outlier, decided once for the kind table and the hotspots alike.
type kindRuns struct {
	runs     []dataset.Run
	repos    []string
	verdicts []string
	stuck    []bool
	cut      outlierCut
}

func newKindRuns(rs []dataset.Run, repoKey repoNamer) kindRuns {
	n := len(rs)
	kr := kindRuns{runs: rs, repos: make([]string, n), verdicts: make([]string, n), stuck: make([]bool, n)}
	for i, r := range rs {
		kr.repos[i], kr.verdicts[i] = repoKey(r), verdict(r)
	}
	kr.cut = newOutlierCut(kr)
	for i, r := range rs {
		kr.stuck[i] = kr.cut.stuck(r, kr.repos[i])
	}
	return kr
}

// timed: a run with a verdict and a duration, the runs medians are over.
func (kr kindRuns) timed(i int) bool {
	return kr.verdicts[i] != VerdictNone && kr.runs[i].DurationS != nil
}

// outlierCut tells one kind's outliers: runs past OutlierFactor times the
// team's median of the kind and past OutlierMinS, and for a repo with
// OutlierRepoMinRuns runs past OutlierFactor times its own median too. A
// repo always slower than the rest is no outlier, its stuck run is.
//
// Medians are over the timed runs, outliers included: a median holds until
// half the runs are stuck, a p90 not past a tenth, and a Jenkins outage
// stalls many runs at once. Same limit per repo: one with more than half
// of its runs stuck has a stuck median and no outliers; within one period
// that is no different from a slow repo. A repo with fewer runs, slower
// than the rest by hours, loses them: two runs say nothing about a repo.
type outlierCut struct {
	// median is the team's, the base of an outlier's factor.
	median float64
	// threshold is the team's; nil without a timed run.
	threshold *float64
	// repos are the repos' own thresholds, those with OutlierRepoMinRuns
	// runs.
	repos map[string]float64
}

func newOutlierCut(kr kindRuns) outlierCut {
	var all []float64
	byRepo := map[string][]float64{}
	for i, r := range kr.runs {
		if !kr.timed(i) {
			continue
		}
		d := float64(*r.DurationS)
		all = append(all, d)
		byRepo[kr.repos[i]] = append(byRepo[kr.repos[i]], d)
	}
	c := outlierCut{repos: map[string]float64{}}
	if len(all) == 0 {
		return c
	}
	c.median = *Median(all)
	c.threshold = ptr(max(OutlierFactor*c.median, OutlierMinS))
	for repo, ds := range byRepo {
		if len(ds) >= OutlierRepoMinRuns {
			c.repos[repo] = OutlierFactor * *Median(ds)
		}
	}
	return c
}

// stuck reports whether r, of repo, is an outlier.
func (c outlierCut) stuck(r dataset.Run, repo string) bool {
	return c.threshold != nil && r.DurationS != nil && float64(*r.DurationS) > max(*c.threshold, c.repos[repo])
}

// factor is r's duration over the team's median; nil at a median of 0.
func (c outlierCut) factor(r dataset.Run) *float64 {
	if c.median <= 0 {
		return nil
	}
	return ptr(float64(*r.DurationS) / c.median)
}

// ---- Hotspots --------------------------------------------------------------

// Hotspot thresholds. Starting values, not yet measured against real
// failures.
const (
	// HotspotMinRuns is the fewest runs a repo needs in a kind, in this
	// period and the previous one, to be flagged: below it one bad run
	// makes a hotspot.
	HotspotMinRuns = 10
	// SlowFactor flags a repo whose median is at least this many times its
	// own of the previous period, same kind. Against itself, never the
	// team: a kind mixes toolchains (a Gradle build against an npm build
	// with Playwright smoke tests), so the team's median made every UI repo
	// slow and hid a Gradle repo that doubled.
	SlowFactor = 1.5
	// FlakyFactor flags a repo whose failure rate is at least this many
	// times its own of the previous period; see flakyThreshold.
	FlakyFactor = 2.0
	// FlakyMinPoints: a flaky repo's failure rate is also at least this
	// much above its previous one, in points of 0..1. From 0 % any factor
	// is 0.
	FlakyMinPoints = 0.05
	// FlakyMinFailures: one red run never makes a repo flaky. Over 10 to
	// 19 runs a single one is already 5 points or more.
	FlakyMinFailures = 2
	// OutlierFactor makes a run an outlier past this many times the median
	// of its kind (see outlierCut): a run waiting for an agent or an input
	// step rather than a slow repo, and enough to wreck any sum. 6× the
	// median flags on clean data about what 3× the p90 did, without the
	// p90's give under a cluster of stuck runs.
	OutlierFactor = 6.0
	// OutlierMinS: no run under an hour is an outlier, whatever the median.
	// A stuck run waits hours; a repo with too few runs for a median of its
	// own, slower than the rest, must not lose its time to the factor, nor
	// a kind whose median is 0 (cached runs) all outlier detection.
	OutlierMinS = 3600.0
	// OutlierRepoMinRuns are the runs a repo needs for a median of its own
	// in the outlier cut: fewer than HotspotMinRuns, so a nightly of a few
	// runs a period, hours long, keeps its time.
	OutlierRepoMinRuns = 3
	// OutlierGroupWindow: an outlier started within this of a group's last
	// one belongs to it; stuck together, they point at the agents, not at
	// the repos.
	OutlierGroupWindow = 30 * time.Minute
	// OutlierGroupRepos is the fewest repos a group needs to be shared
	// infrastructure rather than one repo stuck twice.
	OutlierGroupRepos = 2
	hotspotTop        = 8
	outlierTop        = 10
	// slowestTop: the longest repos per kind, flagged or not. No threshold:
	// a feature build steady at 25 minutes is never slow against itself.
	slowestTop = 5
)

// SlowestMinRuns are the timed runs, outliers aside, a repo's median needs
// to rank among the longest: two runs say nothing about a repo. Its own
// constant, so tuning the outlier cut leaves the list alone. A starting
// value, user-chosen 2026-11-15.
const SlowestMinRuns = 3

// KindHotspots are one kind's repos, each against its own previous period.
type KindHotspots struct {
	Kind string `json:"kind"`
	// Duration is the team's, all repos of the kind, outliers aside.
	Duration    Spread   `json:"duration"`
	FailureRate *float64 `json:"failure_rate"`
	// Repos are a row per repo: the flaky ones by wasted CI time, then the
	// slow ones by factor, then the rest by wasted time and median; the top
	// 8 and every flagged one.
	Repos []RepoHotspot `json:"repos"`
	// Slowest are the slowestTop repos by median, longest first, those with
	// SlowestMinRuns timed runs or more, outliers aside; the rows of Repos,
	// before its cut.
	Slowest []RepoHotspot `json:"slowest"`
	// OutlierThresholdS is OutlierFactor times the team's median, outliers
	// included, at least OutlierMinS; null without a run with a verdict and
	// a duration. A repo with a longer median of its own has a higher one.
	OutlierThresholdS *float64 `json:"outlier_threshold_s"`
	// Unplotted are the kind's repos without a median and a failure rate in
	// Repos: past its cut, without a run with a verdict, or every timed run
	// an outlier.
	Unplotted int `json:"unplotted"`
	// Heatmap is a row per repo of Repos, in its order: the failure rate per
	// week.
	Heatmap []HeatRow `json:"heatmap"`
}

// Outlier is a run far longer than its kind usually takes.
type Outlier struct {
	Kind         string     `json:"kind"`
	PipelineName string     `json:"pipeline_name"`
	Repo         string     `json:"repo"`
	RunID        string     `json:"run_id"`
	StartedAt    *time.Time `json:"started_at"`
	DurationS    float64    `json:"duration_s"`
	Status       *string    `json:"status"`
	// Factor is the run's duration over its kind's team median; null when
	// that is 0.
	Factor *float64 `json:"factor"`
	// WaitS is the part of DurationS spent waiting for a build agent; null
	// when the CI did not report when the run got one.
	WaitS *float64 `json:"wait_s"`
}

// Hotspots are the builds worth a look: repos slower or failing more than
// in the previous period, same kind, and runs far past the kind's median.
type Hotspots struct {
	MinRuns            int            `json:"min_runs"`
	SlowFactor         float64        `json:"slow_factor"`
	FlakyFactor        float64        `json:"flaky_factor"`
	FlakyMinPoints     float64        `json:"flaky_min_points"`
	FlakyMinFailures   int            `json:"flaky_min_failures"`
	OutlierFactor      float64        `json:"outlier_factor"`
	OutlierMinS        float64        `json:"outlier_min_s"`
	OutlierRepoMinRuns int            `json:"outlier_repo_min_runs"`
	SlowestMinRuns     int            `json:"slowest_min_runs"`
	Kinds              []KindHotspots `json:"kinds"`
	// Outliers across kinds, longest first, the top 10: runs of several
	// repos stuck at the same time point at shared infrastructure.
	Outliers []Outlier `json:"outliers"`
	// OutlierCount is every outlier of the period, before the cut to the top.
	OutlierCount int `json:"outlier_count"`
	// OutlierGroups are the outliers of all kinds that started close
	// together, two runs or more, the largest first.
	OutlierGroups []OutlierGroup `json:"outlier_groups"`
	// HeatMinRuns and HeatSteps class the heatmap's cells, HeatClasses are
	// those classes for the legend.
	HeatMinRuns int         `json:"heat_min_runs"`
	HeatSteps   []float64   `json:"heat_steps"`
	HeatClasses []HeatClass `json:"heat_classes"`
}

// RepoHotspot is one repo of a kind against its own runs of the kind in
// the previous period, duration and failures in one row.
type RepoHotspot struct {
	Repo string `json:"repo"`
	// Runs have a verdict; Duration is over those with a duration too,
	// outliers aside.
	Runs     int    `json:"runs"`
	Duration Spread `json:"duration"`
	// Waiting and Building split Duration's runs as BuildStats' do, over
	// those whose CI reports when they got an agent.
	Waiting  Spread `json:"waiting"`
	Building Spread `json:"building"`
	// DurationChange is the median against the previous period's, both
	// over at least HotspotMinRuns runs with a verdict and a duration.
	DurationChange Change `json:"duration_change"`
	// Factor is the median over the previous period's; null without a
	// comparison or at a previous median of 0.
	Factor *float64 `json:"factor"`
	// Slow: Factor at least SlowFactor.
	Slow        bool     `json:"slow"`
	Failures    int      `json:"failures"`
	FailureRate *float64 `json:"failure_rate"`
	// FailureChange is the failure rate against the previous period's, both
	// over at least HotspotMinRuns runs with a verdict.
	FailureChange Change `json:"failure_change"`
	// WastedSeconds is the CI time of the FAILURE and UNSTABLE runs,
	// outliers aside.
	WastedSeconds float64 `json:"wasted_seconds"`
	// Flaky: a failure rate of at least flakyThreshold of the previous
	// period's and FlakyMinPoints above it.
	Flaky bool `json:"flaky"`
	// CISeconds is every run's time, those without a verdict included,
	// outliers aside, counted as BuildStats.CISeconds. The rows add up to
	// at most the kind's: a repo without a run with a verdict has no row,
	// and Repos is cut to the top.
	CISeconds float64 `json:"ci_seconds"`
	// Compared: either previous value is there, DurationChange's or
	// FailureChange's.
	Compared bool `json:"compared"`
	// PrevComplete: both are, so the repo has a previous position on the
	// median and the failure rate alike.
	PrevComplete bool `json:"prev_complete"`
	// Plotted: a median and a failure rate, a position to draw the repo at.
	Plotted bool `json:"plotted"`
}

// OutlierGroup is outliers whose starts follow each other within
// OutlierGroupWindow: chained, so a long outage is one group of any span.
type OutlierGroup struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Runs int       `json:"runs"`
	// Repos are the distinct repos, by name.
	Repos []string `json:"repos"`
	// Shared: OutlierGroupRepos repos or more, so the agents, not a repo.
	Shared bool `json:"shared"`
}

// compareHotspots orders RepoHotspot rows: flaky by wasted CI time, then
// slow by factor, then the rest by wasted time and median.
func compareHotspots(a, b RepoHotspot) int {
	rank := func(h RepoHotspot) int {
		switch {
		case h.Flaky:
			return 0
		case h.Slow:
			return 1
		}
		return 2
	}
	p50 := func(h RepoHotspot) float64 {
		if h.Duration.P50 == nil {
			return 0
		}
		return *h.Duration.P50
	}
	factor := func(h RepoHotspot) float64 {
		if h.Factor == nil {
			return 0
		}
		return *h.Factor
	}
	c := cmp.Compare(rank(a), rank(b))
	if c == 0 && rank(a) == 1 {
		c = cmp.Compare(factor(b), factor(a))
	}
	return cmp.Or(c, cmp.Compare(b.WastedSeconds, a.WastedSeconds), cmp.Compare(p50(b), p50(a)),
		cmp.Compare(a.Repo, b.Repo))
}

// outlierGroups groups the outliers by start, chained: a run started within
// OutlierGroupWindow of the group's last one joins it, so an outage of hours
// stays one group. Single ones are no group.
func outlierGroups(outliers []Outlier) []OutlierGroup {
	var started []Outlier
	for _, o := range outliers {
		if o.StartedAt != nil {
			started = append(started, o)
		}
	}
	slices.SortStableFunc(started, func(a, b Outlier) int { return a.StartedAt.Compare(*b.StartedAt) })
	out := []OutlierGroup{}
	for i := 0; i < len(started); {
		j := i + 1
		for j < len(started) && started[j].StartedAt.Sub(*started[j-1].StartedAt) <= OutlierGroupWindow {
			j++
		}
		if j-i > 1 {
			var repos []string
			for _, o := range started[i:j] {
				if !slices.Contains(repos, o.Repo) {
					repos = append(repos, o.Repo)
				}
			}
			slices.Sort(repos)
			out = append(out, OutlierGroup{
				From: started[i].StartedAt.UTC(), To: started[j-1].StartedAt.UTC(), Runs: j - i,
				Repos: repos, Shared: len(repos) >= OutlierGroupRepos,
			})
		}
		i = j
	}
	slices.SortStableFunc(out, func(a, b OutlierGroup) int { return cmp.Compare(b.Runs, a.Runs) })
	return out
}

// flakyThreshold is the failure rate that flags a repo whose rate was t in
// the previous period: FlakyFactor times it and at least FlakyMinPoints
// above it, capped halfway from it to 100 %. Without the cap a repo failing
// over half its runs could never get flaky, not even when it starts
// failing them all; the cap comes last, or a floor past 100 % would.
func flakyThreshold(t float64) float64 { return min(max(FlakyFactor*t, t+FlakyMinPoints), (1+t)/2) }

// topOrFlagged keeps the first hotspotTop rows and every flagged one past
// them, in order: a flagged repo ranked low by a few short-lived repos
// above it still shows.
func topOrFlagged[T any](rows []T, flagged func(T) bool) []T {
	out := rows[:0]
	for i, r := range rows {
		if i < hotspotTop || flagged(r) {
			out = append(out, r)
		}
	}
	return out
}

// repoRuns are one repo's runs of a kind, as the hotspots count them.
type repoRuns struct {
	durations []float64
	// waits and works split the runs of durations that report their agent
	// start.
	waits, works []float64
	// timed are the runs with a verdict and a duration, outliers included.
	timed, counted, failures int
	wasted                   float64
	// ci is every run's time, those without a verdict included; stuck the
	// outliers' time, in no other number of the repo.
	ci, stuck float64
}

// byRepo counts the runs per repo. A run without a verdict is no run
// here, nor its duration, only its CI time; an outlier counts as a run
// and in the failure rate, its time only in stuck, never in ci or wasted.
func (kr kindRuns) byRepo() map[string]*repoRuns {
	repos := map[string]*repoRuns{}
	for i, r := range kr.runs {
		rr := repos[kr.repos[i]]
		if rr == nil {
			rr = &repoRuns{}
			repos[kr.repos[i]] = rr
		}
		if kr.stuck[i] {
			rr.stuck += seconds(r)
		} else {
			rr.ci += seconds(r)
		}
		v := kr.verdicts[i]
		if v == VerdictNone {
			continue
		}
		if kr.timed(i) {
			rr.timed++
			if !kr.stuck[i] {
				rr.durations = append(rr.durations, float64(*r.DurationS))
				if wait, work, ok := agentSplit(r); ok {
					rr.waits = append(rr.waits, wait)
					rr.works = append(rr.works, work)
				}
			}
		}
		rr.counted++
		if v == VerdictFailure {
			rr.failures++
			if !kr.stuck[i] {
				rr.wasted += seconds(r)
			}
		}
	}
	return repos
}

// against judges the repo's runs rr against its own of the previous
// period, prev (nil without any), and sets its flags.
func (h *RepoHotspot) against(rr, prev *repoRuns, incomplete bool) {
	if prev == nil && !incomplete {
		h.DurationChange, h.FailureChange = repoDurationMetric.none(ReasonNoValue), repoFailureMetric.none(ReasonNoValue)
		return
	}
	if prev == nil {
		prev = &repoRuns{}
	}
	h.DurationChange = repoDurationMetric.judge(side{Median(rr.durations), rr.timed >= HotspotMinRuns},
		side{Median(prev.durations), prev.timed >= HotspotMinRuns}, incomplete)
	if p := h.DurationChange.Prev; p != nil && *p > 0 {
		h.Factor = ptr(*h.Duration.P50 / *p)
		h.Slow = *h.Factor+eps >= SlowFactor
	}
	h.FailureChange = repoFailureMetric.judge(side{h.FailureRate, rr.counted >= HotspotMinRuns},
		side{ratio(prev.failures, prev.counted), prev.counted >= HotspotMinRuns}, incomplete)
	if p := h.FailureChange.Prev; p != nil {
		h.Flaky = h.Failures >= FlakyMinFailures && *h.FailureRate+eps >= flakyThreshold(*p)
	}
}

// kindHotspots is one kind's repos, repos its runs by repo, each against
// base's of the previous period; compared counts those with a comparison,
// before the cut to the top.
func kindHotspots(kind string, kr kindRuns, repos map[string]*repoRuns, base *baseline) (
	k KindHotspots, outliers []Outlier, compared int,
) {
	var durations []float64
	counted, failures := 0, 0
	for i, r := range kr.runs {
		out := kr.stuck[i]
		// Runs without a verdict too: a run stuck for a day and then
		// cancelled is what an outlier is there to show.
		if out {
			o := Outlier{
				Kind: kind, PipelineName: r.PipelineName, Repo: kr.repos[i], RunID: r.RunID,
				StartedAt: r.StartedAt, DurationS: float64(*r.DurationS), Status: r.Status,
				Factor: kr.cut.factor(r),
			}
			if wait, _, ok := agentSplit(r); ok {
				o.WaitS = ptr(wait)
			}
			outliers = append(outliers, o)
		}
		v := kr.verdicts[i]
		if v == VerdictNone {
			continue
		}
		if kr.timed(i) && !out {
			durations = append(durations, float64(*r.DurationS))
		}
		counted++
		if v == VerdictFailure {
			failures++
		}
	}
	k = KindHotspots{
		Kind: kind, Duration: spread(durations), FailureRate: ratio(failures, counted),
		Repos: []RepoHotspot{}, Slowest: []RepoHotspot{},
	}
	prev := base.kinds[kind]
	for repo, rr := range repos {
		if rr.counted == 0 {
			continue
		}
		h := RepoHotspot{
			Repo: repo, Runs: rr.counted, Duration: spread(rr.durations),
			Waiting: spread(rr.waits), Building: spread(rr.works), Failures: rr.failures,
			FailureRate: ratio(rr.failures, rr.counted), WastedSeconds: rr.wasted, CISeconds: rr.ci,
		}
		h.against(rr, prev[repo], base.incomplete)
		h.Compared = h.DurationChange.Prev != nil || h.FailureChange.Prev != nil
		h.PrevComplete = h.DurationChange.Prev != nil && h.FailureChange.Prev != nil
		h.Plotted = h.Duration.P50 != nil && h.FailureRate != nil
		if h.Compared {
			compared++
		}
		k.Repos = append(k.Repos, h)
		// Its own slice: topOrFlagged cuts Repos in place.
		if h.Duration.N >= SlowestMinRuns {
			k.Slowest = append(k.Slowest, h)
		}
	}
	slices.SortFunc(k.Slowest, func(a, b RepoHotspot) int {
		return cmp.Or(cmp.Compare(*b.Duration.P50, *a.Duration.P50), cmp.Compare(a.Repo, b.Repo))
	})
	k.Slowest = k.Slowest[:min(len(k.Slowest), slowestTop)]
	slices.SortFunc(k.Repos, compareHotspots)
	// Every flagged repo stays: compare counts them off this list.
	k.Repos = topOrFlagged(k.Repos, func(h RepoHotspot) bool { return h.Flaky || h.Slow })
	k.Unplotted = len(repos)
	for _, h := range k.Repos {
		if h.Plotted {
			k.Unplotted--
		}
	}

	k.OutlierThresholdS = kr.cut.threshold
	return k, outliers, compared
}
