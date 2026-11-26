package metrics

import (
	"slices"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
)

// ---- Releases (the sprint rule) -------------------------------------------

// releaseStates judge each project rolled out against the sprint rule (at
// least one release to production per two-week sprint, owner 2026-11),
// without a sprint calendar: days since its last release at the instant.
// A project's Apps are the non-infrastructure Apps of the product
// (dataset.App); its last release the newest of theirs, an App's first
// deploy counting (a lower bound inside FirstDeployGrace: it may have run
// those images long before). Only a project with merged work since that
// release or within the last ReleaseBreachDays is judged; one nobody works
// on is idle, no finding. Library-only projects have no App and are never
// judged; infrastructure Apps neither. A project past the breach is the
// breach's, escalated out of the hint: never "gelöst" there.
func (a *assessor) releaseStates(at stateAt) []stateEval {
	th, end := a.states.th, at.end
	last := lastReleases(a.ds, a.f, end)
	work := map[string][]time.Time{}
	for _, pr := range a.states.prs {
		if !pr.Library && notCounted(pr) == "" && pr.MergedAt.Before(end) {
			work[pr.Project] = append(work[pr.Project], *pr.MergedAt)
		}
	}
	recent := end.AddDate(0, 0, -th.ReleaseBreachDays)
	breach := stateEval{key: StateNoRelease, unit: UnitProject, source: dataset.SourceArgoCD, tone: ToneAttention,
		facts: "deploys", tier: tierRule,
		threshold: StateThreshold{Key: "release_breach_days", Value: float64(th.ReleaseBreachDays), Unit: ThresholdDays}}
	due := stateEval{key: StateReleaseDue, unit: UnitProject, source: dataset.SourceArgoCD, tone: ToneHint,
		facts: "deploys", tier: tierLasting,
		threshold: StateThreshold{Key: "release_hint_days", Value: float64(th.ReleaseHintDays), Unit: ThresholdDays}}
	for _, p := range slices.Sorted(mapsKeys(last)) {
		l := last[p]
		r := l.release
		since := r.At
		days := calendarDays(since, end)
		merged := 0
		active := false
		for _, t := range work[p] {
			if t.After(since) {
				merged++
			}
			if t.After(since) || !t.Before(recent) {
				active = true
			}
		}
		// The deploy table holds no project: its last release's App finds that
		// release and the App's deploys around it.
		u := stateUnit{StateUnit: StateUnit{Name: p, Project: ptr(p), Value: days, Count: ptr(merged), Since: ptr(since),
			LowerBound: l.lower, Names: l.apps, Query: ptr(r.App)}}
		switch {
		case !active:
			u.status = unitIdle
		case a.gaps[dataset.SourceBitbucket]:
			// Whether anyone worked on it is Bitbucket's to tell.
			u.status = unitUnjudged
		default:
			u.status = unitClear
		}
		b, d := u, u
		if u.status == unitClear {
			switch releaseStanding(days, th) {
			case releaseBreach:
				b.status, d.status = unitFires, unitEscalated
			case releaseDue:
				d.status = unitFires
			}
		}
		breach.units = append(breach.units, b)
		due.units = append(due.units, d)
	}
	return []stateEval{breach, due}
}

// projectLast is a project's last release to production before an instant:
// the release, the project's Apps that released to production, and whether
// it is a lower bound (lastReleases).
type projectLast struct {
	release dataset.AppRelease
	apps    []string
	lower   bool
}

// lastReleases are the last releases to production before end of the
// view's projects (f), by project: the newest release of a project's
// non-infrastructure Apps. Inside FirstDeployGrace of Argo CD's first event
// an App's first deploy is no known release: the last one was then or
// before (lower). The release rule's one reading, for its states and the
// per-repo view alike.
func lastReleases(ds *dataset.Dataset, f filter, end time.Time) map[string]projectLast {
	prod := ds.Meta.ProductionStage
	argo := ds.Meta.DataStart.ArgoCD
	project := map[string]string{}
	for _, app := range ds.Apps {
		if !app.Infra && app.Project != nil && f.inKey(*app.Project) {
			project[app.Name] = *app.Project
		}
	}
	out := map[string]projectLast{}
	for _, r := range ds.Releases {
		p, ok := project[r.App]
		if !ok || r.Environment != prod || !r.At.Before(end) {
			continue
		}
		l, seen := out[p]
		if !slices.Contains(l.apps, r.App) {
			l.apps = append(l.apps, r.App)
		}
		if !seen || r.At.After(l.release.At) {
			l.release = r
		}
		out[p] = l
	}
	for p, l := range out {
		slices.Sort(l.apps)
		l.lower = l.release.First && (argo == nil || l.release.At.Sub(*argo) <= dataset.FirstDeployGrace)
		out[p] = l
	}
	return out
}

// Where a unit's last release to production stands against the sprint rule
// (releaseStanding).
const (
	releaseRegular = iota
	releaseDue
	releaseBreach
)

// releaseStanding is days since a unit's last release against the team's
// thresholds: past ReleaseBreachDays the rule is broken, past
// ReleaseHintDays the release is due. One cut for the states and the
// per-repo view alike.
func releaseStanding(days float64, th config.Thresholds) int {
	switch {
	case days > float64(th.ReleaseBreachDays):
		return releaseBreach
	case days > float64(th.ReleaseHintDays):
		return releaseDue
	}
	return releaseRegular
}

// ---- Merged work before production -----------------------------------------

// trunkArrival is when a merged PR's work reached the trunk: TrunkAt, else
// its merge. Where the wait for production starts.
func trunkArrival(pr dataset.PR) *time.Time {
	if pr.TrunkAt != nil {
		return pr.TrunkAt
	}
	return pr.MergedAt
}

// toProdCandidate reports whether a merged PR of a repo counts for the
// rule that merged work reaches production within a sprint: a person's
// counted work, no library's, placed on a trunk, in a repo an App is known
// to deploy that is no infrastructure Apps' only (infra: infraRepos).
func toProdCandidate(pr dataset.PR, traces map[string]dataset.RepoTrace, infra map[string]bool) bool {
	if pr.Library || notCounted(pr) != "" || (pr.TrunkAt == nil && pr.OffTrunk) || infra[pr.Repo] {
		return false
	}
	rt, ok := traces[pr.Repo]
	return ok && rt.DeployedSince != nil
}

// toProdDays is the days from a PR's arrival on the trunk to at, its first
// production deploy.
func toProdDays(pr dataset.PR, at time.Time) float64 {
	return at.Sub(*trunkArrival(pr)).Hours() / 24
}

// typicalToProd is a repo's typical wait from the trunk to production over
// waits (toProdDays), rounded to a tenth of a day: judged on
// NotOnProdMinPRs PRs or more, past NotOnProdDays the rule is broken.
func typicalToProd(waits []float64, th config.Thresholds) (value float64, judged, fires bool) {
	if len(waits) < th.NotOnProdMinPRs || len(waits) == 0 {
		return 0, false, false
	}
	value = round1(*Median(waits))
	return value, true, value > float64(th.NotOnProdDays)
}

// notOnProdStates judge each deployable repo against the team's rule that
// merged work reaches production within a sprint (owner, 2026-11): its
// merged PRs (people's, counted work) not on production at the instant, the
// oldest by its arrival on the trunk; and its typical wait from merge to
// production over the period's PRs that got there. A library's PRs are
// never rolled out; a repo no deploy shows an App deploying (not traceable)
// gets StateUntraced, never this; a repo only infrastructure Apps deploy is
// no team's delivery (infraRepos); a PR tracing cannot place at all makes
// the repo unjudged rather than clear when it is the oldest.
func (a *assessor) notOnProdStates(at stateAt) []stateEval {
	th, end := a.states.th, at.end
	limit := float64(th.NotOnProdDays)
	type acc struct {
		prodWait
		project string
		waits   []float64
	}
	by := map[string]*acc{}
	for _, pr := range a.states.prs {
		if !pr.MergedAt.Before(end) || !toProdCandidate(pr, a.states.traces, a.states.infra) {
			continue
		}
		r := by[pr.Repo]
		if r == nil {
			r = &acc{project: pr.Project}
			by[pr.Repo] = r
		}
		pl := a.states.placed(pr, end)
		r.add(pr, pl)
		if pl.state == ProdOnProd && at.period.In(pr.MergedAt) && pl.at != nil {
			r.waits = append(r.waits, toProdDays(pr, *pl.at))
		}
	}
	lying := stateEval{key: StateNotOnProd, unit: UnitRepo, source: dataset.SourceArgoCD, tone: ToneAttention,
		facts: "prs", tier: tierRule, lower: th.HysteresisShare * limit, judges: true,
		threshold: StateThreshold{Key: "not_on_prod_days", Value: limit, Unit: ThresholdDays}}
	slow := stateEval{key: StateSlowToProd, unit: UnitRepo, source: dataset.SourceArgoCD, tone: ToneAttention,
		facts: "prs", tier: tierRule, lower: th.HysteresisShare * limit, judges: true,
		threshold: StateThreshold{Key: "not_on_prod_days", Value: limit, Unit: ThresholdDays,
			MinKey: ptr("not_on_prod_min_prs"), Min: ptr(th.NotOnProdMinPRs)}}
	for _, repo := range slices.Sorted(mapsKeys(by)) {
		r := by[repo]
		u := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(r.project), Names: []string{}},
			status: r.status(end, limit)}
		if r.oldest != nil {
			u.Value, u.Count, u.Since = calendarDays(*r.oldest, end), ptr(r.notYet), ptr(*r.oldest)
			u.LowerBound = r.oldest.Before(a.states.from.AddDate(0, 0, 7))
		}
		lying.units = append(lying.units, u)
		s := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(r.project), Count: ptr(len(r.waits)),
			Names: []string{}}, status: unitUnjudged}
		if v, judged, fires := typicalToProd(r.waits, th); judged {
			s.Value, s.status = v, unitClear
			if fires {
				s.status = unitFires
			}
		}
		slow.units = append(slow.units, s)
	}
	return []stateEval{lying, slow}
}

// prodWait gathers a repo's merged PRs as placed on production at an
// instant: how many are not there yet, the oldest of them by its arrival on
// the trunk, and the oldest tracing cannot place (notOnProdStates, the
// per-repo view).
type prodWait struct {
	notYet           int
	oldest, oldestNP *time.Time
}

// add counts pr, placed pl.
func (w *prodWait) add(pr dataset.PR, pl placement) {
	arrived := trunkArrival(pr)
	switch pl.state {
	case ProdOnProd:
	case ProdNotYet:
		w.notYet++
		if w.oldest == nil || arrived.Before(*w.oldest) {
			w.oldest = arrived
		}
	default:
		if w.oldestNP == nil || arrived.Before(*w.oldestNP) {
			w.oldestNP = arrived
		}
	}
}

// status is the repo against the rule at end: it fires where the oldest
// PR not on production arrived more than limit days back; unjudged where
// the oldest PR tracing cannot place did, which may lie there or have
// shipped; clear otherwise.
func (w prodWait) status(end time.Time, limit float64) int {
	switch {
	case w.oldest != nil && calendarDays(*w.oldest, end) > limit:
		return unitFires
	case w.oldestNP != nil && calendarDays(*w.oldestNP, end) > limit:
		return unitUnjudged
	}
	return unitClear
}

// ---- Data honesty ----------------------------------------------------------

// honestyStates are what the data cannot judge, each in its own card of
// the limit group: a repo's merged PRs tracing cannot place (with the
// reason most of them have, Coverage's), releases to production of no
// project, an Argo CD that never reports the production stage. Judged at
// the period's end only (the period's coverage), against no agreement of
// the team's: no threshold.
func (a *assessor) honestyStates(stateAt) []stateEval {
	untraced := stateEval{key: StateUntraced, unit: UnitRepo, source: dataset.SourceArgoCD, tone: ToneLimit,
		facts: "prs", tier: tierLimit, threshold: StateThreshold{Value: 1, Unit: ThresholdCount}}
	for _, r := range a.m.Coverage.Repos {
		if r.Merged == 0 || a.states.infra[r.Repo] {
			continue
		}
		// A PR merged into a branch not yet on the trunk is the team's
		// workflow, no source that fails to report: in the coverage and the
		// facts, never this card.
		n := r.NotPlaceable - r.Reasons.OffTrunk
		u := stateUnit{StateUnit: StateUnit{Name: r.Repo, Project: ptr(r.Project), Value: float64(n),
			Count: ptr(n), Of: ptr(r.Merged), Names: []string{}}, status: unitClear}
		if n > 0 {
			u.status, u.Reason = unitFires, ptr(mainReason(r.Reasons))
		}
		untraced.units = append(untraced.units, u)
	}

	freq := a.m.Delivery.Frequency
	unattributed := stateEval{key: StateUnattributed, unit: UnitTeam, source: dataset.SourceArgoCD, tone: ToneLimit,
		facts: "deploys", tier: tierLimit, threshold: StateThreshold{Value: 1, Unit: ThresholdCount}}
	if freq.ArgoReporting {
		u := stateUnit{StateUnit: StateUnit{Name: "", Value: float64(freq.Unattributed), Count: ptr(freq.Unattributed),
			Names: []string{}}, status: unitClear}
		if freq.Unattributed > 0 {
			u.status = unitFires
		}
		unattributed.units = append(unattributed.units, u)
	}

	noProd := stateEval{key: StateNoProdStage, unit: UnitTeam, source: dataset.SourceArgoCD, tone: ToneLimit,
		facts: "deploys", tier: tierLimit, threshold: StateThreshold{Value: 1, Unit: ThresholdCount}, namesKind: NamesStage}
	if len(a.ds.Releases) > 0 {
		envs := map[string]bool{}
		for _, r := range a.ds.Releases {
			envs[r.Environment] = true
		}
		u := stateUnit{StateUnit: StateUnit{Name: "", Value: float64(len(envs)), Count: ptr(len(envs)),
			Names: slices.Sorted(mapsKeys(envs))}, status: unitClear}
		if !envs[a.ds.Meta.ProductionStage] {
			u.status = unitFires
		}
		noProd.units = append(noProd.units, u)
	}
	return []stateEval{untraced, unattributed, noProd}
}

// mainReason is the reason most of a repo's PRs tracing cannot place have,
// in TraceReasons' order on a tie; off the trunk is none (honestyStates).
func mainReason(r TraceReasons) string {
	best, n := TraceNoMergeCommit, r.NoMergeCommit
	for _, c := range []struct {
		reason string
		n      int
	}{{TraceReleaseUnresolved, r.ReleaseUnresolved}, {TraceRepoUntraced, r.RepoUntraced}, {TraceInSilence, r.InSilence}} {
		if c.n > n {
			best, n = c.reason, c.n
		}
	}
	return best
}
