package analysis

import "github.com/trick77/riptide/internal/metrics"

// The glossary is what the model is told a signal and a number mean, and
// the only levers it may pick. A lever names a step, never a cause riptide
// does not see: riptide knows a run's duration, status and agent wait, not
// its stages or tests; a PR's events, not why nobody merges it (a merge
// freeze, a release date, a dependency, holidays). Only where the data
// shows the cause (the agent wait, several repos stuck at once) is a lever
// concrete. User, 2026-11-16: "ohne dass das LLM weiss, was genau wie
// getestet wird, sollte es nicht empfehlen, Integrationstests rauszunehmen".

// Lever keys. The UI words them, in both languages.
const (
	LeverWalkPRs         = "walk_prs"
	LeverAskWhyWaiting   = "ask_why_waiting"
	LeverInspectBuildLog = "inspect_build_log"
	LeverInspectFailures = "inspect_failures"
	LeverPlatformAgents  = "platform_agents"
	LeverPlatformStall   = "platform_stall"
	LeverFindWhatChanged = "find_what_changed"
	LeverNameWhatHelped  = "name_what_helped"
	LeverLookAtStage     = "look_at_stage"
	LeverCheckFailed     = "check_failed_deploys"
	LeverTidyUp          = "tidy_up"
	LeverCheckResyncs    = "check_resyncs"
	LeverCheckSource     = "check_source"
)

// kindGloss is a signal kind: what it means, and its levers.
type kindGloss struct {
	means  string
	levers []string
}

var kinds = map[string]kindGloss{
	metrics.SignalDataGap: {
		"A source stopped reporting days before the period's end. Its numbers say nothing about the work; never read them as a team that stopped.",
		[]string{LeverCheckSource},
	},
	metrics.SignalPRJam: {
		"Three or more PRs open a week or longer in one repo. 'waiting' counts what those long-open PRs wait on (first_review, review, changes, merge, draft), not every open PR. Why they wait is not in the data: a merge freeze, a release date, a dependency, holidays or nobody feeling responsible are all possible.",
		[]string{LeverWalkPRs, LeverAskWhyWaiting},
	},
	metrics.SignalStage: {
		"The lead-time stage with the longest median (coding, review, merge, to_pre, to_prod) and its part of the time to production. Some stage always takes longest: tone attention only from half the time on enough PRs.",
		[]string{LeverLookAtStage},
	},
	metrics.SignalSlower: {
		"A repo's runs of a kind take 1.5 times their own median of the previous period or more: slower than itself, not than other repos. What inside the build got slower is not in the data.",
		[]string{LeverInspectBuildLog},
	},
	metrics.SignalSteadySlow: {
		"A repo among the slowest of its run kind in this period and the previous one, slower than the kind's median: slow as a habit, not as a change. build_wait_p50 against build_work_p50 tells whether the time is spent waiting for an agent or building. What inside the build takes the time is not in the data.",
		[]string{LeverInspectBuildLog},
	},
	metrics.SignalFailingMore: {
		"A repo's runs of a kind fail far more often than in the previous period. Whether tests, infrastructure or real breakage is not in the data.",
		[]string{LeverInspectFailures},
	},
	metrics.SignalChange: {
		"A team number moved against the previous period, past the tolerance (not noise). tone good: better, by the fact's magnitude minor (a small gain) or notable; tone attention: notably worse. drivers: the repos that carry a worsening, a rough split.",
		[]string{LeverFindWhatChanged, LeverNameWhatHelped},
	},
	metrics.SignalBottleneck: {
		"The lead-time stage that takes longest is another one than in the previous period.",
		[]string{LeverLookAtStage},
	},
	metrics.SignalTradeoff: {
		"Deploy frequency and deploy success moved in opposite directions: more throughput with rising instability is no progress (DORA reads both together).",
		[]string{LeverCheckFailed, LeverFindWhatChanged},
	},
	metrics.SignalAgentWait: {
		"Runs of a kind spend half their time or more waiting for a build agent: the platform's capacity, not the team's builds.",
		[]string{LeverPlatformAgents},
	},
	metrics.SignalSharedStall: {
		"Runs of several repos got stuck at the same time: shared CI infrastructure, not one repo.",
		[]string{LeverPlatformStall},
	},
	metrics.SignalInactive: {
		"Open PRs without any event for 30 days: forgotten, kept apart from any jam.",
		[]string{LeverTidyUp},
	},
	metrics.SignalResync: {
		"Half the production syncs or more brought no new images: deploys that ship nothing.",
		[]string{LeverCheckResyncs},
	},
	metrics.SignalResolved: {
		"A problem the latest assessment of this window raised is gone now. metric names the signal it was.",
		[]string{LeverNameWhatHelped},
	},
}

// labels are the numbers' meanings, and which way is better where one is.
var labels = map[string]string{
	"silent_days":     "days since the source's last event, at the period's end",
	"open_long":       "PRs of the repo open 7 days or longer; fewer is better",
	"open":            "open PRs of the repo",
	"age_p50":         "median age of the repo's open PRs",
	"failure_rate":    "share of the repo's runs of the kind that failed; lower is better",
	"wasted":          "CI time of the failed runs",
	"runs":            "runs with a verdict",
	"build_p50":       "median duration of the green runs; shorter is better",
	"kind_p50":        "median duration of the kind's green runs, the team's",
	"build_wait_p50":  "median wait for a build agent per green run",
	"build_work_p50":  "median build time per green run, after it got an agent",
	"deploy_per_week": "production releases per week; more is better",
	"deploy_success":  "share of production deploys that succeeded; higher is better",
	"lead_time_p50":   "median time from commit to production; shorter is better",
	"review_p50":      "median time from PR ready to approval; shorter is better",
	"within_day":      "share of PRs merged within a day of opening; higher is better",
	"jammed_repos":    "repos with three or more PRs open a week or longer; fewer is better",
	"build_success":   "share of a run kind's runs that succeeded; higher is better",
	"merged_per_week": "human PRs merged per week, the team's throughput; more is better",
	"wip_weeks":       "open PRs over PRs merged per week: weeks of work standing open; fewer is better",
	"wait_share":      "part of the lead time spent waiting (review, merge, deploys) rather than coding; lower is better; null when no PR reached production",
	"stage_share":     "the stage's part of the lead time",
	"stage_p50":       "the stage's median",
	"stage_p90":       "the stage's slowest tenth starts here",
	"resync_share":    "share of production syncs without new images",
	"syncs":           "production syncs",
	"inactive":        "open PRs without an event for 30 days",
	"stall_groups":    "times several repos' runs got stuck together",
	"feature_share":   "share of merged PRs on feature branches; no better direction",
	"untracked":       "share of merged PRs without a Jira key; no better direction",
	"wasted_share":    "share of AI review spend on PRs declined or deleted; no better direction",
	"cost_per_kloc":   "AI review spend per 1000 changed lines; no better direction",
	"open_prs":        "open PRs at the period's end; no better direction",
	"first_touch":     "share of approvals given without a prior comment; a PR that needs no change is approved without one; no better direction",
}

// stageText names the lead-time stages, so the text says what a stage is
// rather than its key.
var stageText = map[string]string{
	metrics.StageCoding: "coding: first commit to PR ready",
	metrics.StageReview: "review: PR ready to approval",
	metrics.StageMerge:  "merge: approval to merge",
	metrics.StageToPre:  "to the pre-production stage: merge to its first deploy there",
	metrics.StageToProd: "to production: the pre-production stage (or the merge) to the first production deploy",
}

// leverText is what each lever says, for the model to pick by.
var leverText = map[string]string{
	LeverWalkPRs:         "walk through the PRs one by one: review, merge, close or park them on purpose",
	LeverAskWhyWaiting:   "find out what they wait on: a freeze, a dependency, or nobody feeling responsible",
	LeverInspectBuildLog: "look in the build log of the slowest runs which step takes the time",
	LeverInspectFailures: "look at the failed runs: the same failure each time, or a different one",
	LeverPlatformAgents:  "raise the agent capacity with the platform team",
	LeverPlatformStall:   "report the stalls to the platform team",
	LeverFindWhatChanged: "first find out what changed, then agree on something",
	LeverNameWhatHelped:  "name what helped, so it stays",
	LeverLookAtStage:     "look at the stage that now takes longest",
	LeverCheckFailed:     "look at the failed deploys",
	LeverTidyUp:          "close the old PRs or park them on purpose",
	LeverCheckResyncs:    "find out where the syncs without new images come from",
	LeverCheckSource:     "check the source's connection",
}

// leversFor are the levers a signal may carry; a worse change and a better
// one get the one that fits.
func leversFor(s metrics.Signal) []string {
	if s.Kind == metrics.SignalChange {
		if s.Tone == metrics.ToneGood {
			return []string{LeverNameWhatHelped}
		}
		return []string{LeverFindWhatChanged}
	}
	return kinds[s.Kind].levers
}
