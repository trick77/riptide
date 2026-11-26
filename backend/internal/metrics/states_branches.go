package metrics

import (
	"slices"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// Branch state keys (StateCard.Key).
const (
	// StateBranchLife: a repo's branches typically live longer than
	// BranchLifeDays working days from their first commit to their PR
	// being ready.
	StateBranchLife = "branch_life"
	// StateBranchOpen: branches without a PR first pushed more than
	// BranchOpenDays ago and pushed again within BranchActiveDays working
	// days: work running past a sprint without a PR.
	StateBranchOpen = "branch_open"
	// StateBranchSilent: branches without a PR last pushed between
	// BranchSilentDays and BranchSilentMaxDays ago, BranchSilentMin or more
	// per repo. Whether they still exist Bitbucket does not report.
	StateBranchSilent = "branch_silent"
)

// longLived reports whether a branch is long-lived by design, never judged
// as work: the trunk, a PR's target (develop, a release line), a scheme no
// developer's work in progress has (release, a dependency bot's, nightly).
func longLived(b dataset.BranchLife) bool {
	return b.Target || dataset.BranchKind(b.Branch) == dataset.BranchMain || notFeature(branchPrefix(b.Branch))
}

// pushesBefore are b's first and last push before t; ok false when none.
func pushesBefore(b dataset.BranchLife, t time.Time) (first, last time.Time, ok bool) {
	for _, p := range b.PushTimes {
		if !p.Before(t) {
			break
		}
		if !ok {
			first, ok = p, true
		}
		last = p
	}
	return first, last, ok
}

// branchStates judge each repo's branches (states-catalogue B-4, BR-1,
// BR-2) as of the instant: how long its merged PRs' branches typically
// lived before the PR was ready (a lower bound: a rebase resets the first
// commit), on BranchLifeMinPRs PRs; branches without a PR still worked on
// past BranchOpenDays; branches without a PR silent for BranchSilentDays to
// BranchSilentMaxDays (a hint: Bitbucket does not report a deletion, so
// whether they exist is unknown). Only pushes before the instant count: a
// branch pushed after a closed period's end was silent at it. A branch is
// without a PR until its first PR event, and not one merged onto the trunk
// without a PR by then. Bots' branches, a tool's, long-lived ones by design
// and GitOps repos are out.
func (a *assessor) branchStates(at stateAt) []stateEval {
	th, end, loc := a.states.th, at.end, a.states.loc
	life := stateEval{key: StateBranchLife, unit: UnitRepo, source: dataset.SourceBitbucket, tone: ToneHint,
		facts: "prs", tier: tierLasting, lasting: true, lower: th.HysteresisShare * th.BranchLifeDays, judges: true,
		threshold: StateThreshold{Key: "branch_life_days", Value: th.BranchLifeDays, Unit: ThresholdWorkingDays,
			MinKey: ptr("branch_life_min_prs"), Min: ptr(th.BranchLifeMinPRs)}}
	lives := map[string][]float64{}
	for _, pr := range mergedPRs(a.states.all, at.period, a.f) {
		if pr.FirstCommitAt != nil && pr.ReadyAt != nil && !pr.ReadyAt.Before(*pr.FirstCommitAt) {
			lives[pr.Repo] = append(lives[pr.Repo], workingDays(*pr.FirstCommitAt, *pr.ReadyAt, loc))
		}
	}
	for _, repo := range slices.Sorted(mapsKeys(lives)) {
		ws := lives[repo]
		u := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(projectOf(repo)), Count: ptr(len(ws)),
			Names: []string{}}, status: unitUnjudged}
		if len(ws) >= th.BranchLifeMinPRs {
			u.Value, u.status = round1(*Median(ws)), unitClear
			if u.Value > th.BranchLifeDays {
				u.status = unitFires
			}
		}
		life.units = append(life.units, u)
	}

	type acc struct {
		open, silent []string
		oldest       float64
		since        *time.Time
		lower        bool
		silAt        *time.Time
	}
	by := map[string]*acc{}
	openSince := end.AddDate(0, 0, -th.BranchOpenDays)
	silentFrom, silentTo := end.AddDate(0, 0, -th.BranchSilentMaxDays), end.AddDate(0, 0, -th.BranchSilentDays)
	for _, b := range a.ds.Branches {
		first, last, ok := pushesBefore(b, end)
		// Merged onto the trunk without a PR: done work, not one "without a
		// PR" still worked on or left lying.
		if !ok || b.Bot || (b.PRAt != nil && b.PRAt.Before(end)) || (b.MergedAt != nil && b.MergedAt.Before(end)) ||
			longLived(b) || !a.f.inKey(b.Project) {
			continue
		}
		r := by[b.Repo]
		if r == nil {
			r = &acc{}
			by[b.Repo] = r
		}
		switch {
		case first.Before(openSince) && workingDays(last, end, loc) <= th.BranchActiveDays:
			r.open = append(r.open, b.Branch)
			if age := calendarDays(first, end); age > r.oldest {
				// Worked on past BranchOpenDays since then; the branch is older
				// where its first push lies at the look-back's start.
				r.oldest, r.since = age, ptr(first.AddDate(0, 0, th.BranchOpenDays))
				r.lower = !first.After(a.states.from.AddDate(0, 0, 1))
			}
		case !last.Before(silentFrom) && last.Before(silentTo):
			r.silent = append(r.silent, b.Branch)
			if r.silAt == nil || last.Before(*r.silAt) {
				r.silAt = ptr(last)
			}
		}
	}
	// A branch without a PR is a row of no facts table: no link.
	open := stateEval{key: StateBranchOpen, unit: UnitRepo, source: dataset.SourceBitbucket, tone: ToneHint,
		tier: tierLasting, lasting: true,
		threshold: StateThreshold{Key: "branch_open_days", Value: float64(th.BranchOpenDays), Unit: ThresholdDays,
			WithinKey: ptr("branch_active_days"), Within: ptr(th.BranchActiveDays)}}
	silent := stateEval{key: StateBranchSilent, unit: UnitRepo, source: dataset.SourceBitbucket, tone: ToneHint,
		tier: tierLasting, valueUnit: UnitCount,
		threshold: StateThreshold{Key: "branch_silent_days", Value: float64(th.BranchSilentDays), Unit: ThresholdDays,
			MinKey: ptr("branch_silent_min"), Min: ptr(th.BranchSilentMin)}}
	for _, repo := range slices.Sorted(mapsKeys(by)) {
		r := by[repo]
		slices.Sort(r.open)
		slices.Sort(r.silent)
		o := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(projectOf(repo)), Value: r.oldest,
			Count: ptr(len(r.open)), Since: r.since, LowerBound: r.lower, Names: r.open}, status: firesIf(len(r.open) > 0)}
		if o.Names == nil {
			o.Names = []string{}
		}
		if len(r.open) > 0 {
			o.began = r.since
		}
		open.units = append(open.units, o)
		s := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(projectOf(repo)), Value: float64(len(r.silent)),
			Count: ptr(len(r.silent)), Since: r.silAt, Names: r.silent}, status: firesIf(len(r.silent) >= th.BranchSilentMin)}
		if s.Names == nil {
			s.Names = []string{}
		}
		silent.units = append(silent.units, s)
	}
	return []stateEval{life, open, silent}
}
