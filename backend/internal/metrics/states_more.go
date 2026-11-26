package metrics

import (
	"cmp"
	"slices"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// More state keys (StateCard.Key).
const (
	// StateDeployFailing: an App's last production deploy failed and none
	// succeeded since, DeployFailingDays working days or longer.
	StateDeployFailing = "deploy_failing"
	// StateDeployFailures: an App's production deploys failed again and
	// again (DeployFailureStreaks runs of failures, each ended by a
	// success): a hint.
	StateDeployFailures = "deploy_failures"
	// StateFirstReviewWait: a repo's ready PRs waiting for their first
	// review, or its typical pickup, past FirstReviewDays working days.
	StateFirstReviewWait = "first_review_wait"
	// StateTrunkFailing: a repo's trunk builds failed for more than
	// TrunkFailingDays working days with TrunkFailedBuilds failures.
	StateTrunkFailing = "trunk_failing"
	// StateBranchFailing: a person's feature or bugfix branch failing
	// BranchFailingBuilds builds in a row over BranchFailingDays working
	// days. Never a bot's branch.
	StateBranchFailing = "branch_failing"
	// StateFlakyBuilds, StateLongBuilds, StateReleaseFailing: a repo's
	// builds of a kind red mostly for no code change, typically longer than
	// BuildMinutes, its release builds failing.
	StateFlakyBuilds    = "flaky_builds"
	StateLongBuilds     = "long_builds"
	StateReleaseFailing = "release_failing"
	// StateBuildsSilent: a repo merges and its CI reports no build: data,
	// "meldet nicht", never "baut nicht".
	StateBuildsSilent = "builds_silent"
	// StateBigPRs, StateFastApproval, StateUnapproved, StateLateDecline:
	// review and size, hints until lasting two sprints.
	StateBigPRs       = "big_prs"
	StateFastApproval = "fast_approval"
	StateUnapproved   = "unapproved"
	StateLateDecline  = "late_decline"
)

func init() {
	for k, src := range map[string]string{
		StateDeployFailing: dataset.SourceArgoCD, StateDeployFailures: dataset.SourceArgoCD,
		StateFirstReviewWait: dataset.SourceBitbucket, StateTrunkFailing: dataset.SourcePipeline,
		StateBranchFailing: dataset.SourcePipeline, StateFlakyBuilds: dataset.SourcePipeline,
		StateLongBuilds: dataset.SourcePipeline, StateReleaseFailing: dataset.SourcePipeline,
		StateBuildsSilent: dataset.SourcePipeline, StateBigPRs: dataset.SourceNoergler,
		StateFastApproval: dataset.SourceBitbucket, StateUnapproved: dataset.SourceBitbucket,
		StateLateDecline: dataset.SourceBitbucket, StateBranchLife: dataset.SourceBitbucket,
		StateBranchOpen: dataset.SourceBitbucket, StateBranchSilent: dataset.SourceBitbucket,
	} {
		stateSource[k] = src
	}
}

// ---- Deploys on failure ----------------------------------------------------

// deployFailingStates judge each App's production deploys of the period
// (states-catalogue A-7): its last one failed and none succeeded for
// DeployFailingDays working days by the period's end (red, held up now);
// DeployFailureStreaks runs of failed deploys, each ended by a success
// (a retry storm counts once), a hint. Infrastructure Apps are never
// judged; a re-sync is no failure.
func (a *assessor) deployFailingStates() {
	th, end, loc := a.states.th, a.p.Period.To, a.states.loc
	prod := a.ds.Meta.ProductionStage
	by := map[string][]dataset.Deploy{}
	for _, d := range a.ds.Deploys {
		if d.Infra || d.Environment == nil || *d.Environment != prod || !a.f.in(d.Project) || !a.p.Period.In(&d.At) {
			continue
		}
		if d.Phase == "Succeeded" || failedPhase(d.Phase) {
			by[d.App] = append(by[d.App], d)
		}
	}
	now := stateEval{key: StateDeployFailing, unit: UnitApp, source: dataset.SourceArgoCD, tone: ToneAttention,
		facts: "deploys", tier: tierNow,
		threshold: StateThreshold{Key: "deploy_failing_days", Value: th.DeployFailingDays, Unit: ThresholdWorkingDays}}
	again := stateEval{key: StateDeployFailures, unit: UnitApp, source: dataset.SourceArgoCD, tone: ToneHint,
		facts: "deploys", tier: tierLasting, valueUnit: UnitCount,
		threshold: StateThreshold{Key: "deploy_failure_streaks", Value: float64(th.DeployFailureStreaks), Unit: ThresholdCount}}
	for _, app := range slices.Sorted(mapsKeys(by)) {
		ds := by[app]
		slices.SortStableFunc(ds, func(x, y dataset.Deploy) int { return cmp.Or(x.At.Compare(y.At), cmp.Compare(x.ID, y.ID)) })
		var project *string
		streaks, failing := 0, 0
		var first *time.Time
		for _, d := range ds {
			project = d.Project
			if failedPhase(d.Phase) {
				if failing == 0 {
					first = ptr(d.At)
				}
				failing++
				continue
			}
			if failing > 0 {
				streaks++
			}
			failing, first = 0, nil
		}
		u := stateUnit{StateUnit: StateUnit{Name: app, Project: project, Names: []string{}}, status: unitClear}
		if failing > 0 && first != nil {
			u.Value, u.Count, u.Since = round1(workingDays(*first, end, loc)), ptr(failing), first
			if u.Value >= th.DeployFailingDays {
				u.status = unitFires
			}
		}
		now.units = append(now.units, u)
		e := stateUnit{StateUnit: StateUnit{Name: app, Project: project, Value: float64(streaks), Count: ptr(len(ds)),
			Names: []string{}}, status: unitClear}
		if streaks >= th.DeployFailureStreaks {
			e.status = unitFires
		}
		again.units = append(again.units, e)
	}
	a.judge(now)
	a.judge(again)
}

// ---- Waiting for the first review ------------------------------------------

// firstReviewState judges each repo with people's work at the period's
// end (states-catalogue B-1): FirstReviewWaiting ready PRs waiting longer
// than FirstReviewDays working days for anyone's first touch, or its
// typical pickup (ready to first review) over it on FirstReviewMinPRs
// merged PRs. Drafts, bots' PRs, inactive ones and those whose state is
// unknown (a lost close) are none. Value: the oldest wait, else the
// typical pickup.
func (a *assessor) firstReviewState() {
	th, end, loc := a.states.th, a.p.Period.To, a.states.loc
	type acc struct {
		project string
		n       int
		oldest  float64
		since   *time.Time
		pickups []float64
	}
	by := map[string]*acc{}
	get := func(repo, project string) *acc {
		if by[repo] == nil {
			by[repo] = &acc{project: project}
		}
		return by[repo]
	}
	for _, pr := range a.ds.OpenPRs {
		if pr.Bot || pr.Draft || pr.Unknown || pr.Touched || pr.ReadySince == nil || !a.f.inKey(pr.Project) ||
			openInactive(pr, end) {
			continue
		}
		r := get(pr.Repo, pr.Project)
		if w := workingDays(*pr.ReadySince, end, loc); w > th.FirstReviewDays {
			r.n++
			if w > r.oldest {
				r.oldest, r.since = w, pr.ReadySince
			}
		}
	}
	for _, pr := range mergedPRs(a.ds, a.p.Period, a.f) {
		if pr.ReadyAt != nil && pr.FirstReviewAt != nil && !pr.FirstReviewAt.Before(*pr.ReadyAt) {
			r := get(pr.Repo, pr.Project)
			r.pickups = append(r.pickups, workingDays(*pr.ReadyAt, *pr.FirstReviewAt, loc))
		}
	}
	e := stateEval{key: StateFirstReviewWait, unit: UnitRepo, source: dataset.SourceBitbucket, tone: ToneHint,
		facts: "prs", tier: tierLasting, lasting: true, lower: th.HysteresisShare * th.FirstReviewDays, judges: true,
		threshold: StateThreshold{Key: "first_review_days", Value: th.FirstReviewDays, Unit: ThresholdWorkingDays,
			MinKey: ptr("first_review_waiting"), Min: ptr(th.FirstReviewWaiting)}}
	for _, repo := range slices.Sorted(mapsKeys(by)) {
		r := by[repo]
		u := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(r.project), Names: []string{}}, status: unitClear}
		switch {
		case r.n >= th.FirstReviewWaiting:
			// Since the oldest waiting PR was ready, as an approved PR's wait
			// counts from its approval (approvedWaitingState).
			u.Value, u.Count, u.status = round1(r.oldest), ptr(r.n), unitFires
			u.Since, u.Measure = ptr(*r.since), ptr(MeasureOldest)
		case len(r.pickups) >= th.FirstReviewMinPRs:
			u.Value, u.Count, u.Measure = round1(*Median(r.pickups)), ptr(len(r.pickups)), ptr(MeasureTypical)
			if u.Value > th.FirstReviewDays {
				u.status = unitFires
			}
		case r.n == 0:
			u.status = unitUnjudged
		}
		e.units = append(e.units, u)
	}
	a.judge(e)
}

// ---- Builds ----------------------------------------------------------------

// trunkRuns are the period's people's runs per repo of the kinds, oldest
// first, GitOps repos and bots aside (teamRuns).
func (a *assessor) kindRuns(kinds ...string) map[string][]dataset.Run {
	names := newRepoNamer(a.ds.Runs)
	out := map[string][]dataset.Run{}
	for k, rs := range teamRuns(a.ds, a.f, gitopsRun(a.ds), nil) {
		if !slices.Contains(kinds, k) {
			continue
		}
		for _, r := range rs {
			if a.p.Period.In(runAt(r)) {
				out[names(r)] = append(out[names(r)], r)
			}
		}
	}
	for _, rs := range out {
		slices.SortStableFunc(rs, func(x, y dataset.Run) int { return cmp.Or(runAt(x).Compare(*runAt(y)), cmp.Compare(x.RunID, y.RunID)) })
	}
	return out
}

// trunkFailingState judges each repo's trunk builds (states-catalogue D-1):
// a run of failed builds from the first failure to the next success, or
// to the period's end while still failing, longer than TrunkFailingDays
// working days with TrunkFailedBuilds failures or more. A single red
// build a rerun turned green is flaky, no episode. Release builds are their
// own state (StateReleaseFailing).
func (a *assessor) trunkFailingState() {
	th, end, loc := a.states.th, a.p.Period.To, a.states.loc
	e := stateEval{key: StateTrunkFailing, unit: UnitRepo, source: dataset.SourcePipeline, tone: ToneAttention,
		facts: "runs", tier: tierLasting,
		threshold: StateThreshold{Key: "trunk_failing_days", Value: th.TrunkFailingDays, Unit: ThresholdWorkingDays,
			MinKey: ptr("trunk_failed_builds"), Min: ptr(th.TrunkFailedBuilds)}}
	for repo, rs := range a.kindRuns(KindMain) {
		u := stateUnit{StateUnit: StateUnit{Name: repo, Project: rs[0].Project, Names: []string{}}, status: unitClear}
		var first *time.Time
		n := 0
		check := func(until time.Time, now bool) {
			if first == nil {
				return
			}
			w := round1(workingDays(*first, until, loc))
			if n >= th.TrunkFailedBuilds && w > th.TrunkFailingDays && w > u.Value {
				u.Value, u.Count, u.Since, u.status = w, ptr(n), first, unitFires
				if now {
					e.tier = tierNow
				}
			}
		}
		for _, r := range rs {
			switch verdict(r) {
			case VerdictFailure:
				if first == nil {
					first = runAt(r)
				}
				n++
			case VerdictSuccess:
				check(*runAt(r), false)
				first, n = nil, 0
			}
		}
		check(end, true)
		e.units = append(e.units, u)
	}
	a.judge(e)
}

// branchFailingState judges each person's feature or bugfix branch: its
// builds failing BranchFailingBuilds times in a row over at least
// BranchFailingDays working days. A bot's branch never: its loop is
// StateBotLoop, CI time only.
func (a *assessor) branchFailingState() {
	th, loc := a.states.th, a.states.loc
	by := map[string][]dataset.Run{}
	for repo, rs := range a.kindRuns(KindFeature) {
		for _, r := range rs {
			if r.BranchKind == dataset.BranchFeature {
				by[repo+":"+r.Branch] = append(by[repo+":"+r.Branch], r)
			}
		}
	}
	e := stateEval{key: StateBranchFailing, unit: UnitBranch, source: dataset.SourcePipeline, tone: ToneHint,
		facts: "runs", tier: tierLasting, valueUnit: UnitCount,
		threshold: StateThreshold{Key: "branch_failing_builds", Value: float64(th.BranchFailingBuilds), Unit: ThresholdCount}}
	for _, key := range slices.Sorted(mapsKeys(by)) {
		rs := by[key]
		u := stateUnit{StateUnit: StateUnit{Name: key, Project: rs[0].Project, Count: ptr(len(rs)), Names: []string{},
			Query: ptr(rs[len(rs)-1].PipelineName)}, status: unitClear}
		var first, last *time.Time
		n := 0
		for _, r := range rs {
			switch verdict(r) {
			case VerdictFailure:
				if n == 0 {
					first = runAt(r)
				}
				n, last = n+1, runAt(r)
				if n >= th.BranchFailingBuilds && workingDays(*first, *last, loc) >= th.BranchFailingDays && float64(n) > u.Value {
					u.Value, u.Since, u.status = float64(n), first, unitFires
				}
			case VerdictSuccess:
				n = 0
			}
		}
		e.units = append(e.units, u)
	}
	a.judge(e)
}

// buildStates judge each repo's builds per kind from the hotspots, never
// across kinds (states-catalogue D-2, D-4, D-5): red mostly for no code
// change (flaky: FlakyBuilds and FlakyShare of the failed on FlakyMinRuns
// builds), a typical successful feature or trunk build longer than
// BuildMinutes on BuildMinRuns, release builds failing (ReleaseBuildsFailed
// and ReleaseBuildsShare on ReleaseBuildsMin). Bots' builds are in no
// hotspot. The flaky detection is a lower bound (a retry inside a build is
// not seen).
func (a *assessor) buildStates() {
	th := a.states.th
	flaky := stateEval{key: StateFlakyBuilds, unit: UnitRepo, source: dataset.SourcePipeline, tone: ToneHint,
		facts: "runs", tier: tierLasting, lasting: true, valueUnit: UnitCount, judges: true,
		threshold: StateThreshold{Key: "flaky_share", Value: th.FlakyShare, Unit: ThresholdShare,
			MinKey: ptr("flaky_builds"), Min: ptr(th.FlakyBuilds)}}
	long := stateEval{key: StateLongBuilds, unit: UnitRepo, source: dataset.SourcePipeline, tone: ToneHint,
		facts: "runs", tier: tierLasting, lasting: true, lower: th.HysteresisShare * th.BuildMinutes, judges: true,
		threshold: StateThreshold{Key: "build_minutes", Value: th.BuildMinutes, Unit: ThresholdMinutes,
			MinKey: ptr("build_min_runs"), Min: ptr(th.BuildMinRuns)}}
	release := stateEval{key: StateReleaseFailing, unit: UnitRepo, source: dataset.SourcePipeline, tone: ToneHint,
		facts: "runs", tier: tierLasting, lasting: true, valueUnit: UnitShare, judges: true,
		threshold: StateThreshold{Key: "release_builds_share", Value: th.ReleaseBuildsShare, Unit: ThresholdShare,
			MinKey: ptr("release_builds_failed"), Min: ptr(th.ReleaseBuildsFailed)}}
	for _, k := range a.m.Build.Hotspots.Kinds {
		for _, h := range k.all {
			unit := func() stateUnit {
				return stateUnit{StateUnit: StateUnit{Name: h.Repo, Project: ptr(projectOf(h.Repo)), Count: ptr(h.Runs),
					Names: []string{k.Kind}}, status: unitUnjudged}
			}
			switch k.Kind {
			case KindFeature, KindMain:
				f := unit()
				if h.Runs >= th.FlakyMinRuns {
					f.status, f.Value, f.Of = unitClear, float64(h.FlakyRuns), ptr(h.Failures)
					if h.FlakyRuns >= th.FlakyBuilds && float64(h.FlakyRuns) >= th.FlakyShare*float64(h.Failures) {
						f.status = unitFires
					}
				}
				flaky.units = append(flaky.units, f)
				l := unit()
				if h.Duration.N >= th.BuildMinRuns && h.Duration.P50 != nil {
					l.status, l.Value, l.Count = unitClear, round1(*h.Duration.P50/60), ptr(h.Duration.N)
					if l.Value > th.BuildMinutes {
						l.status = unitFires
					}
				}
				long.units = append(long.units, l)
			case KindRelease:
				r := unit()
				if h.Runs >= th.ReleaseBuildsMin && h.FailureRate != nil {
					r.status, r.Value, r.Count, r.Of = unitClear, *h.FailureRate, ptr(h.Failures), ptr(h.Runs)
					if h.Failures >= th.ReleaseBuildsFailed && *h.FailureRate >= th.ReleaseBuildsShare {
						r.status = unitFires
					}
				}
				release.units = append(release.units, r)
			}
		}
	}
	a.judge(flaky)
	a.judge(long)
	a.judge(release)
}

// buildsSilentState judges each repo the CI reported for (states-catalogue
// C-3): SilentBuildsMerges people's merges in the last SilentBuildsDays
// working days before the period's end, no build of it in them, after
// SilentBuildsBefore builds in the four weeks before. Worded "meldet
// nicht": the CI may have moved, the job been renamed. Data honesty, a
// limit, never a finding about the team.
func (a *assessor) buildsSilentState() {
	th, end, loc := a.states.th, a.p.Period.To, a.states.loc
	// The span back from the end holding SilentBuildsDays working days.
	from := end
	for workingDays(from, end, loc) < th.SilentBuildsDays {
		from = from.Add(-24 * time.Hour)
	}
	before := from.AddDate(0, 0, -28)
	names := newRepoNamer(slices.Concat(a.ds.Runs, a.prev.Runs))
	gitops := gitopsRun(a.ds)
	type acc struct {
		project       string
		inSpan, prior int
		last          *time.Time
	}
	by := map[string]*acc{}
	for _, ds := range []*dataset.Dataset{a.ds, a.prev} {
		for _, r := range ds.Runs {
			at := runAt(r)
			if r.Bot || r.Status == nil || gitops(r) || at == nil || !a.f.in(r.Project) || r.Repo == nil {
				continue
			}
			x := by[names(r)]
			if x == nil {
				x = &acc{project: projectOf(names(r))}
				by[names(r)] = x
			}
			switch {
			case !at.Before(from) && at.Before(end):
				x.inSpan++
			case !at.Before(before) && at.Before(from):
				x.prior++
			}
			if at.Before(end) && (x.last == nil || at.After(*x.last)) {
				x.last = at
			}
		}
	}
	merges := map[string]int{}
	for _, pr := range a.states.prs {
		if notCounted(pr) == "" && !pr.MergedAt.Before(from) && pr.MergedAt.Before(end) {
			merges[pr.Repo]++
		}
	}
	e := stateEval{key: StateBuildsSilent, unit: UnitRepo, source: dataset.SourcePipeline, tone: ToneLimit,
		facts: "runs", tier: tierLimit, valueUnit: UnitCount,
		threshold: StateThreshold{Key: "silent_builds_days", Value: th.SilentBuildsDays, Unit: ThresholdWorkingDays,
			MinKey: ptr("silent_builds_merges"), Min: ptr(th.SilentBuildsMerges)}}
	for _, repo := range slices.Sorted(mapsKeys(by)) {
		x := by[repo]
		if x.prior < th.SilentBuildsBefore {
			continue
		}
		u := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(x.project), Value: float64(merges[repo]),
			Count: ptr(x.prior), Since: x.last, Names: []string{}}, status: unitClear}
		if x.inSpan == 0 && merges[repo] >= th.SilentBuildsMerges {
			u.status = unitFires
		}
		e.units = append(e.units, u)
	}
	a.judge(e)
}

// ---- Review and size -------------------------------------------------------

// reviewStates judge the period's merged and declined PRs per repo
// (states-catalogue B-6, B-7, B-13, B-10), hints until a unit held one two
// sprints: big PRs (BigPRShare of the sized ones over BigPRLines lines, on
// BigPRMin), big PRs approved within FastApprovalMinutes of being ready
// without a person's touch before (FastApprovalPRs), merged without a
// person's approval (UnapprovedShare on UnapprovedMin), declined after
// more than DeclinedDays working days open (DeclinedPRs). Housekeeping and
// bots' PRs are no merged work and none of these.
func (a *assessor) reviewStates() {
	th, loc := a.states.th, a.states.loc
	type acc struct {
		project                                    string
		sized, big, fast, merged, unapproved, late int
	}
	by := map[string]*acc{}
	get := func(pr dataset.PR) *acc {
		if by[pr.Repo] == nil {
			by[pr.Repo] = &acc{project: pr.Project}
		}
		return by[pr.Repo]
	}
	for _, pr := range mergedPRs(a.ds, a.p.Period, a.f) {
		r := get(pr)
		r.merged++
		if pr.ApprovedAt == nil {
			r.unapproved++
		}
		lines, _, ok := prSize(pr)
		if !ok {
			continue
		}
		r.sized++
		if lines <= float64(th.BigPRLines) {
			continue
		}
		r.big++
		if pr.ApprovedOnFirstTouch && pr.ReadyAt != nil && pr.ApprovedAt != nil &&
			pr.ApprovedAt.Sub(*pr.ReadyAt) <= time.Duration(th.FastApprovalMinutes*float64(time.Minute)) {
			r.fast++
		}
	}
	for _, pr := range a.ds.PRs {
		if pr.Bot || pr.GitOps || pr.State != "declined" || pr.ClosedAt == nil || !a.p.Period.In(pr.ClosedAt) ||
			!a.f.inKey(pr.Project) {
			continue
		}
		start := pr.ReadyAt
		if start == nil {
			start = pr.OpenedAt
		}
		if start != nil && workingDays(*start, *pr.ClosedAt, loc) > th.DeclinedDays {
			get(pr).late++
		}
	}
	big := stateEval{key: StateBigPRs, unit: UnitRepo, source: dataset.SourceNoergler, tone: ToneHint, facts: "prs",
		tier: tierLasting, valueUnit: UnitShare, judges: true,
		threshold: StateThreshold{Key: "big_pr_share", Value: th.BigPRShare, Unit: ThresholdShare,
			MinKey: ptr("big_pr_lines"), Min: ptr(th.BigPRLines)}}
	fast := stateEval{key: StateFastApproval, unit: UnitRepo, source: dataset.SourceBitbucket, tone: ToneHint,
		facts: "prs", tier: tierLasting, valueUnit: UnitCount,
		threshold: StateThreshold{Key: "fast_approval_prs", Value: float64(th.FastApprovalPRs), Unit: ThresholdCount,
			MinKey: ptr("big_pr_lines"), Min: ptr(th.BigPRLines)}}
	unapproved := stateEval{key: StateUnapproved, unit: UnitRepo, source: dataset.SourceBitbucket, tone: ToneHint,
		facts: "prs", tier: tierLasting, valueUnit: UnitShare, judges: true,
		threshold: StateThreshold{Key: "unapproved_share", Value: th.UnapprovedShare, Unit: ThresholdShare,
			MinKey: ptr("unapproved_min"), Min: ptr(th.UnapprovedMin)}}
	late := stateEval{key: StateLateDecline, unit: UnitRepo, source: dataset.SourceBitbucket, tone: ToneHint,
		facts: "prs", tier: tierLasting, valueUnit: UnitCount,
		threshold: StateThreshold{Key: "declined_prs", Value: float64(th.DeclinedPRs), Unit: ThresholdCount,
			MinKey: nil, Min: nil}}
	// Declined after long work: team-wide, the repos named.
	teamLate, lateRepos := 0, []string{}
	for _, repo := range slices.Sorted(mapsKeys(by)) {
		r := by[repo]
		mk := func(status int, v float64, count, of int) stateUnit {
			return stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(r.project), Value: v, Count: ptr(count),
				Of: ptr(of), Names: []string{}}, status: status}
		}
		if r.sized >= th.BigPRMin {
			share := float64(r.big) / float64(r.sized)
			big.units = append(big.units, mk(firesIf(share >= th.BigPRShare), share, r.big, r.sized))
		} else if r.merged > 0 {
			big.units = append(big.units, mk(unitUnjudged, 0, r.big, r.sized))
		}
		if r.merged > 0 {
			fast.units = append(fast.units, mk(firesIf(r.fast >= th.FastApprovalPRs), float64(r.fast), r.fast, r.big))
		}
		if r.merged >= th.UnapprovedMin {
			share := float64(r.unapproved) / float64(r.merged)
			unapproved.units = append(unapproved.units, mk(firesIf(share >= th.UnapprovedShare), share, r.unapproved,
				r.merged))
		} else if r.merged > 0 {
			unapproved.units = append(unapproved.units, mk(unitUnjudged, 0, r.unapproved, r.merged))
		}
		if r.late > 0 {
			teamLate += r.late
			lateRepos = append(lateRepos, repo)
		}
	}
	if len(by) > 0 {
		u := stateUnit{StateUnit: StateUnit{Name: "", Value: float64(teamLate), Count: ptr(teamLate), Names: lateRepos},
			status: firesIf(teamLate >= th.DeclinedPRs)}
		late.unit = UnitTeam
		late.units = append(late.units, u)
	}
	a.judge(big)
	a.judge(fast)
	a.judge(unapproved)
	a.judge(late)
}

func firesIf(b bool) int {
	if b {
		return unitFires
	}
	return unitClear
}
