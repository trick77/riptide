package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
)

// Thresholds is the `thresholds` block: the team's own agreements and the
// values the assessment's states fire at (metrics.Assess). Every key is
// optional; an absent one keeps its starting value (DefaultThresholds). The
// page prints the value a card measured against, so a team that sets its
// own sees its own. Days are calendar days unless the name says working
// days (Monday to Friday in BACKEND_ANALYSIS_TZ, no public holidays).
type Thresholds struct {
	// ReleaseBreachDays: a project whose last release to production lies
	// more days back breaks the sprint rule (at least one release per
	// two-week sprint): a red card. ReleaseHintDays: more days back than
	// this, a sprint has passed without one: a hint.
	ReleaseBreachDays int `json:"release_breach_days"`
	ReleaseHintDays   int `json:"release_hint_days"`
	// NotOnProdDays: merged work of a repo lies more days before production
	// than this: the team's rule broken. NotOnProdMinPRs is the PRs its
	// typical wait from merge to production needs to be judged.
	NotOnProdDays   int `json:"not_on_prod_days"`
	NotOnProdMinPRs int `json:"not_on_prod_min_prs"`
	// MergePauseShare: a week whose merges are at most this share of the
	// project's typical week (median of MergePauseWeeks weeks before, at
	// least MergePauseMinTypical) while MergePauseWaiting approved PRs
	// waited ApprovedWaitDays working days or longer is a merge pause.
	MergePauseShare      float64 `json:"merge_pause_share"`
	MergePauseWeeks      int     `json:"merge_pause_weeks"`
	MergePauseMinTypical float64 `json:"merge_pause_min_typical"`
	MergePauseWaiting    int     `json:"merge_pause_waiting"`
	// MergePauseWaitDays is the working days an approved PR must have
	// waited by the week's end to count as waiting in a merge pause: frozen
	// at the week's end, so a PR approved late in the week has little of it
	// left. Below ApprovedWaitDays on purpose: a week blocked from Thursday
	// on would need its PRs approved by Wednesday to count at a whole day.
	MergePauseWaitDays float64 `json:"merge_pause_wait_days"`
	// ApprovedWaitDays is the working days an approved PR may wait for its
	// merge; ApprovedWaitingPRs approved PRs of a repo waiting longer at the
	// period's end are a finding.
	ApprovedWaitDays   float64 `json:"approved_wait_days"`
	ApprovedWaitingPRs int     `json:"approved_waiting_prs"`
	// FirstReviewDays is the working days a ready PR may wait for its first
	// review: FirstReviewWaiting PRs of a repo waiting longer at the
	// period's end, or a typical pickup over it on FirstReviewMinPRs merged
	// PRs, is a finding.
	FirstReviewDays    float64 `json:"first_review_days"`
	FirstReviewWaiting int     `json:"first_review_waiting"`
	FirstReviewMinPRs  int     `json:"first_review_min_prs"`
	// DeployFailingDays: an App whose last production deploy failed and
	// none succeeded for this many working days. DeployFailureStreaks: as
	// many runs of failed deploys, each ended by a success, in the period.
	DeployFailingDays    float64 `json:"deploy_failing_days"`
	DeployFailureStreaks int     `json:"deploy_failure_streaks"`
	// TrunkFailingDays and TrunkFailedBuilds: a repo's trunk builds failed
	// for more working days than this with at least as many failed builds.
	TrunkFailingDays  float64 `json:"trunk_failing_days"`
	TrunkFailedBuilds int     `json:"trunk_failed_builds"`
	// BranchFailingBuilds and BranchFailingDays: a feature or bugfix
	// branch's builds failed this many times in a row over at least as many
	// working days.
	BranchFailingBuilds int     `json:"branch_failing_builds"`
	BranchFailingDays   float64 `json:"branch_failing_days"`
	// BuildMinutes is the typical successful feature or trunk build a team
	// waits on, in minutes; BuildMinRuns the successful builds it needs.
	BuildMinutes float64 `json:"build_minutes"`
	BuildMinRuns int     `json:"build_min_runs"`
	// FlakyBuilds, FlakyShare and FlakyMinRuns: flaky builds of a repo and
	// kind, their share of its failed builds, and the judged builds needed.
	FlakyBuilds  int     `json:"flaky_builds"`
	FlakyShare   float64 `json:"flaky_share"`
	FlakyMinRuns int     `json:"flaky_min_runs"`
	// ReleaseBuildsFailed, ReleaseBuildsShare and ReleaseBuildsMin: failed
	// release builds of a repo, their share, and the release builds needed.
	ReleaseBuildsFailed int     `json:"release_builds_failed"`
	ReleaseBuildsShare  float64 `json:"release_builds_share"`
	ReleaseBuildsMin    int     `json:"release_builds_min"`
	// SilentBuildsMerges, SilentBuildsDays and SilentBuildsBefore: a repo
	// with this many merged PRs in the last working days and no build in
	// them, after at least SilentBuildsBefore builds in the four weeks before.
	SilentBuildsMerges int     `json:"silent_builds_merges"`
	SilentBuildsDays   float64 `json:"silent_builds_days"`
	SilentBuildsBefore int     `json:"silent_builds_before"`
	// BotLoopFailures: one bot branch failing this many builds in a row is a
	// loop, wasted CI time (never a quality finding).
	BotLoopFailures int `json:"bot_loop_failures"`
	// BranchLifeDays and BranchLifeMinPRs: the typical working days from a
	// branch's first commit to its PR being ready, on at least as many PRs.
	BranchLifeDays   float64 `json:"branch_life_days"`
	BranchLifeMinPRs int     `json:"branch_life_min_prs"`
	// BranchOpenDays and BranchActiveDays: a branch without a PR first
	// pushed more days ago and pushed again within BranchActiveDays working
	// days. BranchSilentDays and BranchSilentMaxDays: a branch without a PR
	// last pushed between these days ago (a hint: Bitbucket does not report
	// whether it still exists).
	BranchOpenDays      int     `json:"branch_open_days"`
	BranchActiveDays    float64 `json:"branch_active_days"`
	BranchSilentDays    int     `json:"branch_silent_days"`
	BranchSilentMaxDays int     `json:"branch_silent_max_days"`
	BranchSilentMin     int     `json:"branch_silent_min"`
	// BigPRLines, BigPRShare and BigPRMin: a repo whose merged PRs change
	// more than BigPRLines lines in at least this share, on BigPRMin sized PRs.
	BigPRLines int     `json:"big_pr_lines"`
	BigPRShare float64 `json:"big_pr_share"`
	BigPRMin   int     `json:"big_pr_min"`
	// FastApprovalMinutes and FastApprovalPRs: big PRs approved within these
	// minutes of being ready, without a person's touch before.
	FastApprovalMinutes float64 `json:"fast_approval_minutes"`
	FastApprovalPRs     int     `json:"fast_approval_prs"`
	// UnapprovedShare and UnapprovedMin: merged PRs without a person's
	// approval, their share, on at least as many merged PRs.
	UnapprovedShare float64 `json:"unapproved_share"`
	UnapprovedMin   int     `json:"unapproved_min"`
	// DeclinedDays and DeclinedPRs: PRs declined after more working days
	// open than this, at least as many in the period.
	DeclinedDays float64 `json:"declined_days"`
	DeclinedPRs  int     `json:"declined_prs"`
	// HysteresisShare: a state keeps firing for a unit it named last time
	// until its number falls below this share of the threshold, so it does
	// not flap.
	HysteresisShare float64 `json:"hysteresis_share"`
}

// DefaultThresholds are the starting values (owner's decisions 2026-11 for
// the release and production rules, proposals for the rest), each a team's
// to change.
func DefaultThresholds() Thresholds {
	return Thresholds{
		ReleaseBreachDays: 28, ReleaseHintDays: 14,
		NotOnProdDays: 14, NotOnProdMinPRs: 5,
		MergePauseShare: 0.2, MergePauseWeeks: 8, MergePauseMinTypical: 3, MergePauseWaiting: 3, MergePauseWaitDays: 0.5,
		ApprovedWaitDays: 1, ApprovedWaitingPRs: 2,
		FirstReviewDays: 2, FirstReviewWaiting: 3, FirstReviewMinPRs: 5,
		DeployFailingDays: 1, DeployFailureStreaks: 3,
		TrunkFailingDays: 1, TrunkFailedBuilds: 2,
		BranchFailingBuilds: 5, BranchFailingDays: 1,
		BuildMinutes: 15, BuildMinRuns: 5,
		FlakyBuilds: 3, FlakyShare: 1.0 / 3, FlakyMinRuns: 10,
		ReleaseBuildsFailed: 2, ReleaseBuildsShare: 0.2, ReleaseBuildsMin: 5,
		SilentBuildsMerges: 3, SilentBuildsDays: 10, SilentBuildsBefore: 10,
		BotLoopFailures: 10,
		BranchLifeDays:  5, BranchLifeMinPRs: 5,
		BranchOpenDays: 14, BranchActiveDays: 5, BranchSilentDays: 14, BranchSilentMaxDays: 60, BranchSilentMin: 3,
		BigPRLines: 400, BigPRShare: 1.0 / 3, BigPRMin: 6,
		FastApprovalMinutes: 10, FastApprovalPRs: 3,
		UnapprovedShare: 0.2, UnapprovedMin: 10,
		DeclinedDays: 10, DeclinedPRs: 3,
		HysteresisShare: 0.8,
	}
}

// StateLookbackWeeks bounds every state's look back (metrics): older data
// is read by no state, so a threshold reaching past it could never fire.
const StateLookbackWeeks = 26

// The most a threshold of a kind may be: what the look-back holds, a day of
// minutes, a whole count.
const (
	maxDays        = 7 * StateLookbackWeeks
	maxWorkingDays = 5 * StateLookbackWeeks
	maxMinutes     = 24 * 60
	maxCount       = 10000
)

// thresholdBounds are the thresholds that are no plain count, by key: a
// share at most 1, calendar or working days within the look-back, minutes
// within a day, weeks of baseline within it. A key not listed is a count.
var thresholdBounds = map[string]float64{
	"merge_pause_share": 1, "flaky_share": 1, "release_builds_share": 1, "big_pr_share": 1,
	"unapproved_share": 1, "hysteresis_share": 1,
	"release_breach_days": maxDays, "release_hint_days": maxDays, "not_on_prod_days": maxDays,
	"branch_open_days": maxDays, "branch_silent_days": maxDays, "branch_silent_max_days": maxDays,
	"approved_wait_days": maxWorkingDays, "merge_pause_wait_days": maxWorkingDays, "first_review_days": maxWorkingDays,
	"deploy_failing_days": maxWorkingDays, "trunk_failing_days": maxWorkingDays, "branch_failing_days": maxWorkingDays,
	"silent_builds_days": maxWorkingDays, "branch_life_days": maxWorkingDays, "branch_active_days": maxWorkingDays,
	"declined_days": maxWorkingDays,
	"build_minutes": maxMinutes, "fast_approval_minutes": maxMinutes,
	"merge_pause_weeks": StateLookbackWeeks,
}

// parseThresholds reads the optional `thresholds` block over the defaults:
// an unknown key is refused (a typo would silently keep the default), every
// value must be a positive number within its bound (thresholdBounds), a
// count whole. Keys are checked in name order, so of two bad keys the same
// one is named every time.
func parseThresholds(raw json.RawMessage) (Thresholds, error) {
	th := DefaultThresholds()
	if raw == nil || isNull(raw) {
		return th, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return th, errors.New("`thresholds` must be an object")
	}
	v := reflect.ValueOf(&th).Elem()
	fields := map[string]reflect.Value{}
	for i := range v.NumField() {
		fields[v.Type().Field(i).Tag.Get("json")] = v.Field(i)
	}
	for _, key := range slices.Sorted(maps.Keys(obj)) {
		val := obj[key]
		f, ok := fields[key]
		if !ok {
			return th, fmt.Errorf("`thresholds.%s` is not a known threshold", key)
		}
		var n float64
		dec := json.NewDecoder(bytes.NewReader(val))
		if err := dec.Decode(&n); err != nil || isNull(val) || n <= 0 || math.IsInf(n, 0) {
			return th, fmt.Errorf("`thresholds.%s` must be a number above 0", key)
		}
		limit, bounded := thresholdBounds[key]
		if !bounded {
			limit = maxCount
		}
		if n > limit {
			return th, fmt.Errorf("`thresholds.%s` must be at most %g", key, limit)
		}
		switch f.Kind() {
		case reflect.Int:
			if n != math.Trunc(n) {
				return th, fmt.Errorf("`thresholds.%s` must be a whole number", key)
			}
			f.SetInt(int64(n))
		default:
			f.SetFloat(n)
		}
	}
	if th.ReleaseHintDays >= th.ReleaseBreachDays {
		return th, errors.New("`thresholds.release_hint_days` must be below `release_breach_days`")
	}
	if th.BranchSilentDays >= th.BranchSilentMaxDays {
		return th, errors.New("`thresholds.branch_silent_days` must be below `branch_silent_max_days`")
	}
	return th, nil
}
