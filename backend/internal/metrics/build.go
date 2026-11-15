package metrics

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/trick77/riptide/internal/dataset"
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

var runKinds = []string{KindRelease, KindMain, KindFeature, KindOther, KindUnknown}

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
	Hotspots Hotspots     `json:"hotspots"`
}

// teamRuns are the runs the build section counts: with a status, in the
// product.
func teamRuns(ds *dataset.Dataset, f filter) map[string][]dataset.Run {
	by := map[string][]dataset.Run{}
	for _, r := range ds.Runs {
		if r.Status == nil || !f.in(r.Project) {
			continue
		}
		k := RunKind(r, ds.Meta.BuildTypeFrom)
		by[k] = append(by[k], r)
	}
	return by
}

func build(ds *dataset.Dataset, p Period, f filter) Build {
	by := teamRuns(ds, f)
	out := Build{DataFrom: ds.Meta.BuildDataFrom, Kinds: []BuildStats{}}
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
	repoKey := newRepoNamer(ds.Runs)
	out.Hotspots = Hotspots{
		MinRuns: HotspotMinRuns, SlowFactor: SlowFactor, FlakyFactor: FlakyFactor,
		OutlierFactor: OutlierFactor, OutlierMinS: OutlierMinS, OutlierRepoMinRuns: OutlierRepoMinRuns,
		Kinds: []KindHotspots{}, Outliers: []Outlier{}, OutlierGroups: []OutlierGroup{},
	}
	for _, kind := range runKinds {
		rs := by[kind]
		if len(rs) == 0 {
			continue
		}
		kr := newKindRuns(rs, repoKey)
		b := BuildStats{Kind: kind}
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
				continue
			case VerdictFailure:
				b.WastedSeconds += seconds(r)
			}
			if r.DurationS != nil {
				durations = append(durations, float64(*r.DurationS))
				if wait, work, ok := agentSplit(r); ok {
					waits = append(waits, wait)
					works = append(works, work)
				}
			}
		}
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
		k, outliers := kindHotspots(kind, kr)
		out.Hotspots.Kinds = append(out.Hotspots.Kinds, k)
		out.Hotspots.Outliers = append(out.Hotspots.Outliers, outliers...)
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
	// HotspotMinRuns is the fewest runs a repo needs in a kind to be
	// flagged: below it one bad run makes a hotspot.
	HotspotMinRuns = 10
	// SlowFactor flags a repo whose median is at least this many times the
	// team's median of the same kind.
	SlowFactor = 2.0
	// FlakyFactor flags a repo whose failure rate is at least this many
	// times the team's of the same kind; see flakyThreshold.
	FlakyFactor = 2.0
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
)

// RepoDuration is one repo's run durations against its kind's team median,
// outliers aside.
type RepoDuration struct {
	Repo string `json:"repo"`
	// Runs are those with a verdict and a duration, outliers included: one
	// stuck run does not clear a slow repo. Duration leaves them out.
	Runs     int    `json:"runs"`
	Duration Spread `json:"duration"`
	// Factor is the repo's median over the team's; null when the team's is
	// 0 or unknown.
	Factor *float64 `json:"factor"`
	// Slow: at least SlowFactor times the team's median, over at least
	// HotspotMinRuns runs.
	Slow bool `json:"slow"`
}

// RepoFailures is one repo's broken runs.
type RepoFailures struct {
	Repo string `json:"repo"`
	// Runs are those with a verdict.
	Runs        int      `json:"runs"`
	Failures    int      `json:"failures"`
	FailureRate *float64 `json:"failure_rate"`
	// WastedSeconds is the CI time of the FAILURE and UNSTABLE runs,
	// outliers aside.
	WastedSeconds float64 `json:"wasted_seconds"`
	// Flaky: a failure rate of at least flakyThreshold of the team's, over
	// at least HotspotMinRuns runs.
	Flaky bool `json:"flaky"`
}

// KindHotspots are one kind's repos against the team.
type KindHotspots struct {
	Kind string `json:"kind"`
	// Duration is the team's, all repos of the kind, outliers aside.
	Duration    Spread   `json:"duration"`
	FailureRate *float64 `json:"failure_rate"`
	// Durations are the repos by median, longest first: the top 8 and every
	// slow one.
	Durations []RepoDuration `json:"durations"`
	// Failing are the repos with broken runs by wasted CI time: the top 8
	// and every flaky one.
	Failing []RepoFailures `json:"failing"`
	// Repos are both lists in one, a row per repo: the flaky ones by wasted
	// CI time, then the slow ones by factor, then the rest by wasted time
	// and median; the top 8 and every flagged one.
	Repos []RepoHotspot `json:"repos"`
	// OutlierThresholdS is OutlierFactor times the team's median, outliers
	// included, at least OutlierMinS; null without a run with a verdict and
	// a duration. A repo with a longer median of its own has a higher one.
	OutlierThresholdS *float64 `json:"outlier_threshold_s"`
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

// Hotspots are the builds worth a look: slow or failing repos against the
// team's baseline of the same kind, and runs far past it.
type Hotspots struct {
	MinRuns            int            `json:"min_runs"`
	SlowFactor         float64        `json:"slow_factor"`
	FlakyFactor        float64        `json:"flaky_factor"`
	OutlierFactor      float64        `json:"outlier_factor"`
	OutlierMinS        float64        `json:"outlier_min_s"`
	OutlierRepoMinRuns int            `json:"outlier_repo_min_runs"`
	Kinds              []KindHotspots `json:"kinds"`
	// Outliers across kinds, longest first, the top 10: runs of several
	// repos stuck at the same time point at shared infrastructure.
	Outliers []Outlier `json:"outliers"`
	// OutlierCount is every outlier of the period, before the cut to the top.
	OutlierCount int `json:"outlier_count"`
	// OutlierGroups are the outliers of all kinds that started close
	// together, two runs or more, the largest first.
	OutlierGroups []OutlierGroup `json:"outlier_groups"`
}

// RepoHotspot is one repo against the team's runs of the same kind,
// duration and failures in one row.
type RepoHotspot struct {
	Repo string `json:"repo"`
	// Runs have a verdict; Duration is over those with a duration too,
	// outliers aside.
	Runs          int      `json:"runs"`
	Duration      Spread   `json:"duration"`
	Factor        *float64 `json:"factor"`
	Slow          bool     `json:"slow"`
	Failures      int      `json:"failures"`
	FailureRate   *float64 `json:"failure_rate"`
	WastedSeconds float64  `json:"wasted_seconds"`
	Flaky         bool     `json:"flaky"`
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

// flakyThreshold is the failure rate that flags a repo against the team's
// rate t: FlakyFactor times it, capped halfway from it to 100 %. Without the
// cap a team failing over half its runs could never have a flaky repo, not
// even one that always fails.
func flakyThreshold(t float64) float64 { return min(FlakyFactor*t, (1+t)/2) }

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

type repoRuns struct {
	durations []float64
	// timed are the runs with a verdict and a duration, outliers included.
	timed, counted, failures int
	wasted                   float64
}

func kindHotspots(kind string, kr kindRuns) (KindHotspots, []Outlier) {
	var durations []float64
	var outliers []Outlier
	counted, failures := 0, 0
	repos := map[string]*repoRuns{}
	for i, r := range kr.runs {
		key, out := kr.repos[i], kr.stuck[i]
		rr := repos[key]
		if rr == nil {
			rr = &repoRuns{}
			repos[key] = rr
		}
		// Runs without a verdict too: a run stuck for a day and then
		// cancelled is what an outlier is there to show.
		if out {
			o := Outlier{
				Kind: kind, PipelineName: r.PipelineName, Repo: key, RunID: r.RunID,
				StartedAt: r.StartedAt, DurationS: float64(*r.DurationS), Status: r.Status,
				Factor: kr.cut.factor(r),
			}
			if wait, _, ok := agentSplit(r); ok {
				o.WaitS = ptr(wait)
			}
			outliers = append(outliers, o)
		}
		// A run without a verdict is no run here either, nor its duration.
		v := kr.verdicts[i]
		if v == VerdictNone {
			continue
		}
		// An outlier still counts as a run and in the failure rate, but
		// none of its time does.
		if kr.timed(i) {
			rr.timed++
			if !out {
				durations = append(durations, float64(*r.DurationS))
				rr.durations = append(rr.durations, float64(*r.DurationS))
			}
		}
		counted++
		rr.counted++
		if v == VerdictFailure {
			failures++
			rr.failures++
			if !out {
				rr.wasted += seconds(r)
			}
		}
	}
	k := KindHotspots{
		Kind: kind, Duration: spread(durations), FailureRate: ratio(failures, counted),
		Durations: []RepoDuration{}, Failing: []RepoFailures{}, Repos: []RepoHotspot{},
	}
	for repo, rr := range repos {
		h := RepoHotspot{
			Repo: repo, Runs: rr.counted, Duration: spread(rr.durations), Failures: rr.failures,
			FailureRate: ratio(rr.failures, rr.counted), WastedSeconds: rr.wasted,
		}
		if len(rr.durations) > 0 {
			d := RepoDuration{Repo: repo, Runs: rr.timed, Duration: h.Duration}
			if t := k.Duration.P50; t != nil && *t > 0 {
				d.Factor = ptr(*d.Duration.P50 / *t)
				d.Slow = d.Runs >= HotspotMinRuns && *d.Factor >= SlowFactor
			}
			k.Durations = append(k.Durations, d)
			h.Factor, h.Slow = d.Factor, d.Slow
		}
		if rr.failures > 0 {
			f := RepoFailures{
				Repo: repo, Runs: rr.counted, Failures: rr.failures,
				FailureRate: h.FailureRate, WastedSeconds: rr.wasted,
			}
			if t := k.FailureRate; t != nil && *t > 0 {
				f.Flaky = f.Runs >= HotspotMinRuns && *f.FailureRate >= flakyThreshold(*t)
			}
			k.Failing = append(k.Failing, f)
			h.Flaky = f.Flaky
		}
		if rr.counted > 0 {
			k.Repos = append(k.Repos, h)
		}
	}
	slices.SortFunc(k.Repos, compareHotspots)
	k.Repos = topOrFlagged(k.Repos, func(h RepoHotspot) bool { return h.Flaky || h.Slow })
	slices.SortFunc(k.Durations, func(a, b RepoDuration) int {
		return cmp.Or(cmp.Compare(*b.Duration.P50, *a.Duration.P50), cmp.Compare(a.Repo, b.Repo))
	})
	slices.SortFunc(k.Failing, func(a, b RepoFailures) int {
		return cmp.Or(cmp.Compare(b.WastedSeconds, a.WastedSeconds), cmp.Compare(b.Failures, a.Failures),
			cmp.Compare(a.Repo, b.Repo))
	})
	k.Durations = topOrFlagged(k.Durations, func(d RepoDuration) bool { return d.Slow })
	k.Failing = topOrFlagged(k.Failing, func(f RepoFailures) bool { return f.Flaky })

	k.OutlierThresholdS = kr.cut.threshold
	return k, outliers
}
