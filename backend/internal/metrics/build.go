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
	// Duration is over Runs: an aborted run's is no build time.
	Duration Spread `json:"duration"`
	// CISeconds is every run's time, those without a verdict included.
	CISeconds float64 `json:"ci_seconds"`
	// WastedSeconds is the time of the failed runs.
	WastedSeconds float64 `json:"wasted_seconds"`
	// Weekly is the success rate per week, null for a week without runs.
	Weekly []*float64 `json:"weekly"`
}

// tally counts runs by verdict; runs are those with one.
type tally struct{ runs, succeeded, failed, none int }

// add counts r and returns its verdict.
func (t *tally) add(r dataset.Run) string {
	v := verdict(r)
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
	for _, kind := range runKinds {
		rs := by[kind]
		if len(rs) == 0 {
			continue
		}
		b := BuildStats{Kind: kind}
		var t tally
		var durations []float64
		for _, r := range rs {
			b.CISeconds += seconds(r)
			switch t.add(r) {
			case VerdictNone:
				continue
			case VerdictFailure:
				b.WastedSeconds += seconds(r)
			}
			if r.DurationS != nil {
				durations = append(durations, float64(*r.DurationS))
			}
		}
		b.Runs, b.Succeeded, b.Failed, b.NoVerdict = t.runs, t.succeeded, t.failed, t.none
		b.SuccessRate = ratio(b.Succeeded, b.Runs)
		b.Duration = spread(durations)
		for _, w := range bucket(p, rs, runAt) {
			var wt tally
			for _, r := range w {
				wt.add(r)
			}
			b.Weekly = append(b.Weekly, ratio(wt.succeeded, wt.runs))
		}
		out.Kinds = append(out.Kinds, b)
	}
	out.Hotspots = hotspots(by, newRepoNamer(ds.Runs))
	return out
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
	// OutlierFactor makes a run an outlier past this many times the team's
	// p90 of its kind: a stuck agent or a waiting input step rather than a
	// slow repo, and enough to wreck any mean.
	OutlierFactor = 3.0
	hotspotTop    = 8
	outlierTop    = 10
)

// RepoDuration is one repo's run durations against its kind's team median.
type RepoDuration struct {
	Repo     string `json:"repo"`
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
	// WastedSeconds is the CI time of the FAILURE and UNSTABLE runs.
	WastedSeconds float64 `json:"wasted_seconds"`
	// Flaky: a failure rate of at least flakyThreshold of the team's, over
	// at least HotspotMinRuns runs.
	Flaky bool `json:"flaky"`
}

// KindHotspots are one kind's repos against the team.
type KindHotspots struct {
	Kind string `json:"kind"`
	// Duration is the team's, all repos of the kind.
	Duration    Spread   `json:"duration"`
	FailureRate *float64 `json:"failure_rate"`
	// Durations are the repos by median, longest first: the top 8 and every
	// slow one.
	Durations []RepoDuration `json:"durations"`
	// Failing are the repos with broken runs by wasted CI time: the top 8
	// and every flaky one.
	Failing []RepoFailures `json:"failing"`
	// OutlierThresholdS is OutlierFactor times the team's p90; null below
	// HotspotMinRuns runs with a verdict and a duration.
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
	// Factor is the run's duration over its kind's team p90.
	Factor float64 `json:"factor"`
}

// Hotspots are the builds worth a look: slow or failing repos against the
// team's baseline of the same kind, and runs far past it.
type Hotspots struct {
	MinRuns       int            `json:"min_runs"`
	SlowFactor    float64        `json:"slow_factor"`
	FlakyFactor   float64        `json:"flaky_factor"`
	OutlierFactor float64        `json:"outlier_factor"`
	Kinds         []KindHotspots `json:"kinds"`
	// Outliers across kinds, longest first, the top 10: runs of several
	// repos stuck at the same time point at shared infrastructure.
	Outliers []Outlier `json:"outliers"`
}

func hotspots(by map[string][]dataset.Run, repoKey repoNamer) Hotspots {
	out := Hotspots{
		MinRuns: HotspotMinRuns, SlowFactor: SlowFactor, FlakyFactor: FlakyFactor,
		OutlierFactor: OutlierFactor, Kinds: []KindHotspots{}, Outliers: []Outlier{},
	}
	for _, kind := range runKinds {
		rs := by[kind]
		if len(rs) == 0 {
			continue
		}
		k, outliers := kindHotspots(kind, rs, repoKey)
		out.Kinds = append(out.Kinds, k)
		out.Outliers = append(out.Outliers, outliers...)
	}
	slices.SortStableFunc(out.Outliers, func(a, b Outlier) int { return cmp.Compare(b.DurationS, a.DurationS) })
	if len(out.Outliers) > outlierTop {
		out.Outliers = out.Outliers[:outlierTop]
	}
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
	durations         []float64
	counted, failures int
	wasted            float64
}

func kindHotspots(kind string, rs []dataset.Run, repoKey repoNamer) (KindHotspots, []Outlier) {
	var durations []float64
	counted, failures := 0, 0
	repos := map[string]*repoRuns{}
	for _, r := range rs {
		key := repoKey(r)
		rr := repos[key]
		if rr == nil {
			rr = &repoRuns{}
			repos[key] = rr
		}
		// A run without a verdict is no run here either, nor its duration.
		v := verdict(r)
		if v == VerdictNone {
			continue
		}
		if r.DurationS != nil {
			durations = append(durations, float64(*r.DurationS))
			rr.durations = append(rr.durations, float64(*r.DurationS))
		}
		counted++
		rr.counted++
		if v == VerdictFailure {
			failures++
			rr.failures++
			rr.wasted += seconds(r)
		}
	}
	k := KindHotspots{
		Kind: kind, Duration: spread(durations), FailureRate: ratio(failures, counted),
		Durations: []RepoDuration{}, Failing: []RepoFailures{},
	}
	for repo, rr := range repos {
		if len(rr.durations) > 0 {
			d := RepoDuration{Repo: repo, Runs: len(rr.durations), Duration: spread(rr.durations)}
			if t := k.Duration.P50; t != nil && *t > 0 {
				d.Factor = ptr(*d.Duration.P50 / *t)
				d.Slow = d.Runs >= HotspotMinRuns && *d.Factor >= SlowFactor
			}
			k.Durations = append(k.Durations, d)
		}
		if rr.failures > 0 {
			f := RepoFailures{
				Repo: repo, Runs: rr.counted, Failures: rr.failures,
				FailureRate: ratio(rr.failures, rr.counted), WastedSeconds: rr.wasted,
			}
			if t := k.FailureRate; t != nil && *t > 0 {
				f.Flaky = f.Runs >= HotspotMinRuns && *f.FailureRate >= flakyThreshold(*t)
			}
			k.Failing = append(k.Failing, f)
		}
	}
	slices.SortFunc(k.Durations, func(a, b RepoDuration) int {
		return cmp.Or(cmp.Compare(*b.Duration.P50, *a.Duration.P50), cmp.Compare(a.Repo, b.Repo))
	})
	slices.SortFunc(k.Failing, func(a, b RepoFailures) int {
		return cmp.Or(cmp.Compare(b.WastedSeconds, a.WastedSeconds), cmp.Compare(b.Failures, a.Failures),
			cmp.Compare(a.Repo, b.Repo))
	})
	k.Durations = topOrFlagged(k.Durations, func(d RepoDuration) bool { return d.Slow })
	k.Failing = topOrFlagged(k.Failing, func(f RepoFailures) bool { return f.Flaky })

	var outliers []Outlier
	if len(durations) >= HotspotMinRuns && k.Duration.P90 != nil && *k.Duration.P90 > 0 {
		p90 := *k.Duration.P90
		k.OutlierThresholdS = ptr(OutlierFactor * p90)
		// Runs without a verdict too: a run stuck for a day and then
		// cancelled is what an outlier is there to show.
		for _, r := range rs {
			if r.DurationS == nil || float64(*r.DurationS) <= *k.OutlierThresholdS {
				continue
			}
			outliers = append(outliers, Outlier{
				Kind: kind, PipelineName: r.PipelineName, Repo: repoKey(r), RunID: r.RunID,
				StartedAt: r.StartedAt, DurationS: float64(*r.DurationS), Status: r.Status,
				Factor: float64(*r.DurationS) / p90,
			})
		}
	}
	return k, outliers
}
