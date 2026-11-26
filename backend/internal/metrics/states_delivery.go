package metrics

import (
	"slices"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// ---- Releases (the sprint rule) -------------------------------------------

// releaseStates judge each project rolled out against the sprint rule (at
// least one release to production per two-week sprint, owner 2026-11),
// without a sprint calendar: days since its last release at the period's
// end. A project's Apps are the non-infrastructure Apps of the product
// (dataset.App); its last release the newest of theirs, an App's first
// deploy counting (a lower bound inside FirstDeployGrace: it may have run
// those images long before). Only a project with merged work since that
// release or within the last ReleaseBreachDays is judged; one nobody works
// on is idle, no finding. Library-only projects have no App and are never
// judged; infrastructure Apps neither.
func (a *assessor) releaseStates() {
	th, end := a.states.th, a.p.Period.To
	prod := a.ds.Meta.ProductionStage
	argo := a.ds.Meta.DataStart.ArgoCD
	project := map[string]string{}
	for _, app := range a.ds.Apps {
		if !app.Infra && app.Project != nil && a.f.inKey(*app.Project) {
			project[app.Name] = *app.Project
		}
	}
	last := map[string]dataset.AppRelease{}
	apps := map[string][]string{}
	for _, r := range a.ds.Releases {
		p, ok := project[r.App]
		if !ok || r.Environment != prod || !r.At.Before(end) {
			continue
		}
		if !slices.Contains(apps[p], r.App) {
			apps[p] = append(apps[p], r.App)
		}
		if l, ok := last[p]; !ok || r.At.After(l.At) {
			last[p] = r
		}
	}
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
		r := last[p]
		since := r.At
		days := calendarDays(since, end)
		// Inside the grace an App's first deploy is no known release: the last
		// one was then or before.
		lower := r.First && (argo == nil || r.At.Sub(*argo) <= dataset.FirstDeployGrace)
		merged := 0
		active := false
		for _, at := range work[p] {
			if at.After(since) {
				merged++
			}
			if at.After(since) || !at.Before(recent) {
				active = true
			}
		}
		slices.Sort(apps[p])
		u := stateUnit{StateUnit: StateUnit{Name: p, Project: ptr(p), Value: days, Count: ptr(merged), Since: ptr(since),
			LowerBound: lower, Names: apps[p]}}
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
		if u.status == unitClear && days > float64(th.ReleaseBreachDays) {
			b.status = unitFires
		}
		if u.status == unitClear && days > float64(th.ReleaseHintDays) && days <= float64(th.ReleaseBreachDays) {
			d.status = unitFires
		}
		breach.units = append(breach.units, b)
		due.units = append(due.units, d)
	}
	a.judge(breach)
	a.judge(due)
}

// ---- Merged work before production -----------------------------------------

// notOnProdStates judge each deployable repo against the team's rule that
// merged work reaches production within a sprint (owner, 2026-11): its
// merged PRs (people's, counted work) not on production at the period's
// end, the oldest by its arrival on the trunk; and its typical wait from
// merge to production over the period's PRs that got there. A library's
// PRs are never rolled out; a repo no deploy shows an App deploying (not
// traceable) gets StateUntraced, never this; a PR tracing cannot place at
// all makes the repo unjudged rather than clear when it is the oldest.
func (a *assessor) notOnProdStates() {
	th, end := a.states.th, a.p.Period.To
	limit := float64(th.NotOnProdDays)
	lookback := end.AddDate(0, 0, -7*StateLookbackWeeks)
	type acc struct {
		project          string
		notYet           int
		oldest, oldestNP *time.Time
		waits            []float64
	}
	by := map[string]*acc{}
	for _, pr := range a.states.prs {
		if pr.Library || notCounted(pr) != "" || !pr.MergedAt.Before(end) || (pr.TrunkAt == nil && pr.OffTrunk) {
			continue
		}
		rt, ok := a.states.traces[pr.Repo]
		if !ok || rt.DeployedSince == nil {
			continue
		}
		r := by[pr.Repo]
		if r == nil {
			r = &acc{project: pr.Project}
			by[pr.Repo] = r
		}
		at := pr.MergedAt
		if pr.TrunkAt != nil {
			at = pr.TrunkAt
		}
		pl := a.states.placed(pr, end)
		switch pl.state {
		case ProdOnProd:
			if a.p.Period.In(pr.MergedAt) && pl.at != nil {
				r.waits = append(r.waits, pl.at.Sub(*at).Hours()/24)
			}
		case ProdNotYet:
			r.notYet++
			if r.oldest == nil || at.Before(*r.oldest) {
				r.oldest = at
			}
		default:
			if r.oldestNP == nil || at.Before(*r.oldestNP) {
				r.oldestNP = at
			}
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
		u := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(r.project), Names: []string{}}, status: unitClear}
		if r.oldest != nil {
			u.Value, u.Count, u.Since = calendarDays(*r.oldest, end), ptr(r.notYet), ptr(*r.oldest)
			u.LowerBound = r.oldest.Before(lookback.AddDate(0, 0, 7))
		}
		switch {
		case u.Value > limit:
			u.status = unitFires
		case r.oldestNP != nil && calendarDays(*r.oldestNP, end) > limit:
			// The oldest PR may be lying there or shipped: no telling.
			u.status = unitUnjudged
		}
		lying.units = append(lying.units, u)
		s := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(r.project), Count: ptr(len(r.waits)),
			Names: []string{}}, status: unitUnjudged}
		if len(r.waits) >= th.NotOnProdMinPRs {
			s.Value, s.status = round1(*Median(r.waits)), unitClear
			if s.Value > limit {
				s.status = unitFires
			}
		}
		slow.units = append(slow.units, s)
	}
	a.judge(lying)
	a.judge(slow)
}

// ---- Data honesty ----------------------------------------------------------

// honestyStates are what the data cannot judge, each in its own card of
// the limit group: a repo's merged PRs tracing cannot place (with the
// reason, Coverage's), releases to production of no project, an Argo CD
// that never reports the production stage.
func (a *assessor) honestyStates() {
	untraced := stateEval{key: StateUntraced, unit: UnitRepo, source: dataset.SourceArgoCD, tone: ToneLimit,
		facts: "prs", tier: tierLimit, threshold: StateThreshold{Key: "", Value: 1, Unit: ThresholdCount}}
	for _, r := range a.m.Coverage.Repos {
		if r.Merged == 0 {
			continue
		}
		u := stateUnit{StateUnit: StateUnit{Name: r.Repo, Project: ptr(r.Project), Value: float64(r.NotPlaceable),
			Count: ptr(r.NotPlaceable), Of: ptr(r.Merged), Names: []string{}}, status: unitClear}
		if r.NotPlaceable > 0 {
			u.status, u.Reason = unitFires, ptr(mainReason(r.Reasons))
		}
		untraced.units = append(untraced.units, u)
	}
	a.judge(untraced)

	freq := a.m.Delivery.Frequency
	unattributed := stateEval{key: StateUnattributed, unit: UnitTeam, source: dataset.SourceArgoCD, tone: ToneLimit,
		facts: "deploys", tier: tierLimit, threshold: StateThreshold{Key: "", Value: 1, Unit: ThresholdCount}}
	if freq.ArgoReporting {
		u := stateUnit{StateUnit: StateUnit{Name: "", Value: float64(freq.Unattributed), Count: ptr(freq.Unattributed),
			Names: []string{}}, status: unitClear}
		if freq.Unattributed > 0 {
			u.status = unitFires
		}
		unattributed.units = append(unattributed.units, u)
	}
	a.judge(unattributed)

	noProd := stateEval{key: StateNoProdStage, unit: UnitTeam, source: dataset.SourceArgoCD, tone: ToneLimit,
		facts: "deploys", tier: tierLimit, threshold: StateThreshold{Key: "", Value: 1, Unit: ThresholdCount}}
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
	a.judge(noProd)
}

// mainReason is the reason most of a repo's unplaced PRs have, in
// TraceReasons' order on a tie.
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
