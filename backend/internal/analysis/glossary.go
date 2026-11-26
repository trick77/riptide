package analysis

import "github.com/trick77/riptide/internal/metrics"

// The glossary is what the model is told a signal and a number mean, and
// the only levers it may pick. A lever names a step, never a cause riptide
// does not see: riptide knows a run's duration, status and agent wait, not
// its stages or tests; a PR's events, not why nobody merges it (a merge
// freeze, a release date, a dependency, holidays). Only where the data
// shows the cause (the agent wait) is a lever concrete. User, 2026-11-16:
// "ohne dass das LLM weiss, was genau wie getestet wird, sollte es nicht
// empfehlen, Integrationstests rauszunehmen".

// Lever keys. The UI words them, in both languages.
const (
	LeverWalkPRs         = "walk_prs"
	LeverAskWhyWaiting   = "ask_why_waiting"
	LeverInspectBuildLog = "inspect_build_log"
	LeverInspectFailures = "inspect_failures"
	LeverPlatformAgents  = "platform_agents"
	LeverFindWhatChanged = "find_what_changed"
	LeverNameWhatHelped  = "name_what_helped"
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
		"Four or more PRs open a week or longer in one repo, drafts never counted. 'waiting' counts what those long-open PRs wait on (first_review, review, changes, merge, draft), not every open PR. Why they wait is not in the data: a merge freeze, a release date, a dependency, holidays or nobody feeling responsible are all possible.",
		[]string{LeverWalkPRs, LeverAskWhyWaiting},
	},
	metrics.SignalSlower: {
		"A repo's builds of a kind take 1.5 times their own median of the previous period or more: slower than itself, not than other repos. What inside the build got slower is not in the data.",
		[]string{LeverInspectBuildLog},
	},
	metrics.SignalFailingMore: {
		"A repo's builds of a kind fail far more often than in the previous period. Whether tests, infrastructure or real breakage is not in the data.",
		[]string{LeverInspectFailures},
	},
	metrics.SignalChange: {
		"A team number moved notably against the previous period: 25 % or more, 5 points for a share, and for a share on few cases or deploys on few releases a step of at least two. tone good: notably better; tone attention: notably worse. drivers: the repos that carry a worsening, a rough split.",
		[]string{LeverFindWhatChanged, LeverNameWhatHelped},
	},
	metrics.SignalAgentWait: {
		"Builds of a kind spend half their time or more, and two minutes or more, waiting for a build agent: the platform's capacity, not the team's builds.",
		[]string{LeverPlatformAgents},
	},
	metrics.SignalResolved: {
		"A problem an earlier assessment of this window raised is back at or better than its value before it was raised (a jam: clearly below four long-open PRs). metric names the signal it was. No step: say what is back, do not recommend.",
		nil,
	},
}

// labels are the numbers' meanings, and which way is better where one is.
var labels = map[string]string{
	"silent_days":     "days since the source's last event, at the period's end",
	"open_long":       "PRs of the repo open 7 days or longer; fewer is better",
	"open":            "open PRs of the repo",
	"age_p50":         "median age of the repo's open PRs since they turned ready for review, drafts out",
	"failure_rate":    "share of the repo's builds of the kind that failed; lower is better",
	"wasted":          "CI time of the failed builds",
	"runs":            "builds with a result",
	"build_p50":       "median duration of the succeeded builds; shorter is better",
	"build_wait_p50":  "median wait for a build agent per succeeded build",
	"build_work_p50":  "median build time per succeeded build, after it got an agent",
	"deploy_per_week": "production releases per week; more is better",
	"deploy_success":  "share of production deploys that succeeded; higher is better",
	"lead_time_p50":   "median time from a PR's first commit to production; shorter is better",
	"review_p50":      "median time from PR ready to approval; shorter is better",
	"within_day":      "share of PRs merged within a day of opening; higher is better",
	"jammed_repos":    "repos with four or more PRs open a week or longer; fewer is better",
	"build_success":   "share of a build kind's builds that succeeded; higher is better",
	"merged_per_week": "human PRs merged per week, the team's throughput; more is better",
	"batch_size":      "median changed lines (added plus removed) per merged human PR, the batch size; smaller is better",
	"wip_weeks":       "open PRs over PRs merged per week: weeks of work standing open; fewer is better",
	"feature_share":   "share of merged PRs on feature branches; no better direction",
	"untracked":       "share of merged PRs without a Jira key; no better direction",
	"wasted_share":    "share of AI review spend on PRs declined or deleted; no better direction",
	"cost_per_kloc":   "AI review spend per 1000 changed lines; no better direction",
	"open_prs":        "open PRs at the period's end; no better direction",
	"first_touch":     "share of approvals given without a prior comment; a PR that needs no change is approved without one; no better direction",
}

// leverText is what each lever says, for the model to pick by.
var leverText = map[string]string{
	LeverWalkPRs:         "walk through the PRs one by one: review, merge, close or park them on purpose",
	LeverAskWhyWaiting:   "find out what they wait on: a freeze, a dependency, or nobody feeling responsible",
	LeverInspectBuildLog: "look in the build log of the slowest builds which step takes the time",
	LeverInspectFailures: "look at the failed builds: the same failure each time, or a different one",
	LeverPlatformAgents:  "raise the agent capacity with the platform team",
	LeverFindWhatChanged: "first find out what changed, then agree on something",
	LeverNameWhatHelped:  "name what helped, so it stays",
	LeverCheckSource:     "check the source's connection",
}

// leversFor are the levers a signal may carry, never nil; a worse change
// and a better one get the one that fits, a resolution none.
func leversFor(s metrics.Signal) []string {
	if s.Kind == metrics.SignalChange {
		if s.Tone == metrics.ToneGood {
			return []string{LeverNameWhatHelped}
		}
		return []string{LeverFindWhatChanged}
	}
	if ls := kinds[s.Kind].levers; ls != nil {
		return ls
	}
	return []string{}
}
