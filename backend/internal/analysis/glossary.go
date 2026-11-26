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
	LeverPlanRelease     = "plan_release"
	LeverStopBotLoop     = "stop_bot_loop"
	LeverWalkBranches    = "walk_branches"
	LeverInspectDeploys  = "inspect_deploys"
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
		"A problem an earlier assessment of this window raised is back at or better than its value before it was raised (a jam: clearly below four long-open PRs; a state: judged clear again). metric names the signal it was. No step: say what is back, do not recommend.",
		nil,
	},
	metrics.SignalState: {
		"A state: a finding that stands without a previous period, judged per unit (project, repo, App, branch) against the team's own threshold, never pooled. state.key says which (states lists their meaning and the page's words); state.count units of state.judged fire, state.unjudged could not be judged, state.threshold is what they were measured against (null for what the data cannot judge: it measures against no agreement). tone attention: red; hint: a hint; limit: what the data cannot judge, never a finding about the team.",
		nil,
	},
}

// StateGloss is a state as the model is told it: what it means, and the
// page's words for its title in both languages, so the model never coins a
// term of its own.
type StateGloss struct {
	Means string `json:"means"`
	DE    string `json:"de"`
	EN    string `json:"en"`
}

// states are the state keys' glosses; the titles are the page's (de.ts,
// en.ts overview.assess.state.<key>.title).
var states = map[string]StateGloss{
	metrics.StateNoRelease: {"A project rolled out, with merged work, released to production longer ago than the team's rule allows (at least one release per two-week sprint). value: days since its last release.",
		"Lange kein Release nach PROD", "No release to PROD for long"},
	metrics.StateReleaseDue: {"A project's last release to production lies more than a sprint back, not yet past the rule's limit. A hint.",
		"Release nach PROD bald fällig", "Release to PROD due soon"},
	metrics.StateNotOnProd: {"A repo's merged PRs are not on production yet, the oldest longer than the team's rule allows. value: days since the oldest reached the trunk.",
		"Gemergte PRs warten lange auf PROD", "Merged PRs wait long for PROD"},
	metrics.StateSlowToProd: {"A repo's merged PRs typically take longer from merge to production than the team's rule allows.",
		"Lange Wartezeit vom Merge bis PROD", "Long wait from merge to PROD"},
	metrics.StateMergePause: {"A project's week in which hardly any PR was merged against its typical week, while approved PRs waited. Why is not in the data (a freeze, a release date).",
		"Kaum gemerged, obwohl freigegebene PRs warteten", "Hardly merged while approved PRs waited"},
	metrics.StateApprovedWaiting: {"A repo's approved PRs wait for their merge longer than the team's threshold, at the period's end.",
		"Freigegeben, aber nicht gemerged", "Approved, but not merged"},
	metrics.StateBotLoop: {"A bot branch's builds failed again and again in a row: CI time wasted. Never a problem of the team's code.",
		"Bot-Branch verbraucht CI-Zeit mit fehlgeschlagenen Builds", "Bot branch burning CI time on failed builds"},
	metrics.StateUntraced: {"A repo's merged PRs cannot be placed on production: its delivery cannot be judged.",
		"Nicht messbar, ob gemergte PRs auf PROD sind", "Not measurable whether merged PRs are on PROD"},
	metrics.StateUnattributed: {"Releases to production that belong to no project: the release rule cannot be judged for the projects they belong to.",
		"Releases nach PROD ohne Projekt", "Releases to PROD without a project"},
	metrics.StateNoProdStage: {"Argo CD reports deploys, but none to the production stage: the stage configuration or the notification is off.",
		"Argo CD meldet nichts nach PROD", "Argo CD reports nothing to PROD"},
	metrics.StateUnmeasurable: {"A state (state.was) the assessment a week before raised can no longer be judged for units it named: the data no longer tells. It has no numbers to word. Never call it solved, never a problem of the team.",
		"Nicht mehr messbar", "No longer measurable"},
	metrics.StateDeployFailing: {"An App's last deploy to production failed and none succeeded since, for longer than the threshold.",
		"Letztes Deploy nach PROD fehlgeschlagen", "Last deploy to PROD failed"},
	metrics.StateDeployFailures: {"An App's deploys to production failed again and again in the period, each time succeeding later. A hint.",
		"Deploys nach PROD scheitern immer wieder", "Deploys to PROD fail again and again"},
	metrics.StateFirstReviewWait: {"A repo's ready PRs wait for their first review longer than the threshold, or its PRs typically do.",
		"PRs warten auf ein erstes Review", "PRs wait for a first review"},
	metrics.StateTrunkFailing: {"A repo's trunk builds failed for longer than the threshold, with several failed builds.",
		"Builds auf dem Default-Branch lange fehlgeschlagen", "Builds on the default branch failing for long"},
	metrics.StateBranchFailing: {"A person's feature branch whose builds failed many times in a row.",
		"Builds eines Branches scheitern immer wieder", "A branch's builds fail again and again"},
	metrics.StateFlakyBuilds: {"A repo's builds of a kind are red mostly for no code change: a rerun on the same commit succeeded.",
		"Unzuverlässige Builds", "Unreliable builds"},
	metrics.StateLongBuilds: {"A repo's successful builds of a kind typically take longer than the team's threshold.",
		"Builds dauern lang", "Builds take long"},
	metrics.StateReleaseFailing: {"A repo's release builds fail in a notable share.",
		"Release-Builds scheitern", "Release builds fail"},
	metrics.StateBuildsSilent: {"A repo merged PRs but its CI reported no build in that span after many before: the data is missing, never call it a team that stopped building.",
		"CI meldet für ein Repo keine Builds mehr", "CI reports no builds for a repo any more"},
	metrics.StateBigPRs: {"A repo's merged PRs change many lines in a notable share.",
		"Viele grosse PRs", "Many big PRs"},
	metrics.StateFastApproval: {"Big PRs approved within minutes of being ready, without a person's comment before. The data shows the speed, not how they were reviewed.",
		"Grosse PRs schnell freigegeben", "Big PRs approved fast"},
	metrics.StateUnapproved: {"A repo's PRs merged without a person's approval in a notable share.",
		"Ohne Freigabe gemerged", "Merged without approval"},
	metrics.StateLateDecline: {"PRs declined after long work.",
		"PRs nach langer Arbeit abgelehnt", "PRs declined after long work"},
	metrics.StateBranchLife: {"A repo's branches typically run long before their PR is ready.",
		"Branches leben lange, bevor der PR bereit ist", "Branches live long before the PR is ready"},
	metrics.StateBranchOpen: {"Branches without a PR worked on for longer than a sprint.",
		"Branches ohne PR, lange in Arbeit", "Branches without a PR, worked on for long"},
	metrics.StateBranchSilent: {"Branches without a PR not pushed for weeks. Whether they still exist Bitbucket does not report.",
		"Branches ohne PR, lange still", "Branches without a PR, silent for long"},
}

// stateLevers are each state's levers: only where a step follows from what
// the data shows; the data-honesty states point at the data.
var stateLevers = map[string][]string{
	metrics.StateNoRelease: {LeverPlanRelease}, metrics.StateReleaseDue: {LeverPlanRelease},
	metrics.StateNotOnProd: {LeverPlanRelease}, metrics.StateSlowToProd: {LeverPlanRelease},
	metrics.StateMergePause: {LeverAskWhyWaiting}, metrics.StateApprovedWaiting: {LeverWalkPRs, LeverAskWhyWaiting},
	metrics.StateBotLoop:  {LeverStopBotLoop},
	metrics.StateUntraced: {LeverCheckSource}, metrics.StateUnattributed: {LeverCheckSource},
	// Nothing to do about a unit the data no longer judges: no step.
	metrics.StateNoProdStage: {LeverCheckSource}, metrics.StateUnmeasurable: {},
	// A failed sync says nothing of a build: look at the deploys.
	metrics.StateDeployFailing: {LeverInspectDeploys}, metrics.StateDeployFailures: {LeverInspectDeploys},
	metrics.StateFirstReviewWait: {LeverWalkPRs, LeverAskWhyWaiting},
	metrics.StateTrunkFailing:    {LeverInspectFailures}, metrics.StateBranchFailing: {LeverInspectFailures},
	metrics.StateFlakyBuilds: {LeverInspectFailures}, metrics.StateLongBuilds: {LeverInspectBuildLog},
	metrics.StateReleaseFailing: {LeverInspectFailures}, metrics.StateBuildsSilent: {LeverCheckSource},
	// Size and review: what the data shows, no cause; the team decides.
	metrics.StateBigPRs: {}, metrics.StateFastApproval: {}, metrics.StateUnapproved: {}, metrics.StateLateDecline: {},
	metrics.StateBranchLife: {}, metrics.StateBranchOpen: {}, metrics.StateBranchSilent: {LeverWalkBranches},
}

// labels are the numbers' meanings, and which way is better where one is.
var labels = map[string]string{
	"silent_days":             "days since the source's last event, at the period's end",
	"open_long":               "PRs of the repo open 7 days or longer; fewer is better",
	"open":                    "open PRs of the repo",
	"age_p50":                 "median age of the repo's open PRs since they turned ready for review, drafts out",
	"failure_rate":            "share of the repo's builds of the kind that failed; lower is better",
	"wasted":                  "CI time of the failed builds",
	"runs":                    "builds with a result",
	"build_p50":               "median duration of the succeeded builds; shorter is better",
	"build_wait_p50":          "median wait for a build agent per succeeded build",
	"build_work_p50":          "median build time per succeeded build, after it got an agent",
	"deploy_per_week":         "production releases per week; more is better",
	"deploy_success":          "share of production deploys that succeeded; higher is better",
	"lead_time_p50":           "median time from a PR's first commit to production; shorter is better",
	"review_p50":              "median time from PR ready to approval; shorter is better",
	"within_day":              "share of PRs merged within a day of opening; higher is better",
	"jammed_repos":            "repos with four or more PRs open a week or longer; fewer is better",
	"build_success":           "share of a build kind's builds that succeeded; higher is better",
	"merged_per_week":         "human PRs merged per week, the team's throughput; more is better",
	"batch_size":              "median changed lines (added plus removed) per merged human PR, the batch size; smaller is better",
	"wip_weeks":               "open PRs over PRs merged per week: weeks of work standing open; fewer is better",
	"feature_share":           "share of merged PRs on feature branches; no better direction",
	"untracked":               "share of merged PRs without a Jira key; no better direction",
	"wasted_share":            "share of AI review spend on PRs declined or deleted; no better direction",
	"cost_per_kloc":           "AI review spend per 1000 changed lines; no better direction",
	"open_prs":                "open PRs at the period's end; no better direction",
	"first_touch":             "share of approvals given without a prior comment; a PR that needs no change is approved without one; no better direction",
	"state_units":             "units the state names (projects, repos, Apps or branches, as state.unit says)",
	"state_judged":            "units the state's rule could judge, the base of state_units",
	"state_threshold":         "the team's threshold the state measured against (state.threshold names its key)",
	"state_no_release":        "days since the worst project's last release to production",
	"state_release_due":       "days since the worst project's last release to production",
	"state_not_on_prod":       "days the worst repo's oldest merged PR has waited for production",
	"state_slow_to_prod":      "typical days from merge to production in the worst repo",
	"state_merge_pause":       "approved PRs that waited in the worst project's paused week",
	"state_approved_waiting":  "working days the worst repo's oldest approved PR has waited for its merge",
	"state_bot_loop":          "failed builds in a row on the worst bot branch",
	"state_untraced":          "merged PRs of the worst repo that cannot be placed on production",
	"state_unattributed":      "releases to production that belong to no project",
	"state_no_prod_stage":     "stages Argo CD reported deploys to, none of them production",
	"state_deploy_failing":    "working days the worst App's production deploy has stood on a failure",
	"state_deploy_failures":   "runs of failed production deploys of the worst App",
	"state_first_review_wait": "working days the worst repo's oldest ready PR waits for its first review, or its typical pickup",
	"state_trunk_failing":     "working days the worst repo's trunk builds failed in a row",
	"state_branch_failing":    "failed builds in a row on the worst feature branch",
	"state_flaky_builds":      "flaky builds of the worst repo",
	"state_long_builds":       "minutes a successful build of the worst repo typically takes",
	"state_release_failing":   "share of the worst repo's release builds that failed",
	"state_builds_silent":     "PRs the worst repo merged while its CI reported no build",
	"state_big_prs":           "share of the worst repo's sized PRs over the line threshold",
	"state_fast_approval":     "big PRs of the worst repo approved within minutes",
	"state_unapproved":        "share of the worst repo's merged PRs without a person's approval",
	"state_late_decline":      "PRs declined after long work",
	"state_branch_life":       "typical working days from a branch's first commit to its PR being ready, worst repo",
	"state_branch_open":       "days the worst repo's oldest branch without a PR has run",
	"state_branch_silent":     "branches without a PR not pushed for weeks, worst repo",
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
	LeverPlanRelease:     "plan the next release to production, or name what holds it",
	LeverStopBotLoop:     "look at the bot branch: fix what its builds trip over, or close it",
	LeverWalkBranches:    "go through the branches: delete the finished ones, open a PR or park the rest on purpose",
	LeverInspectDeploys:  "look at the failed deploys in Argo CD: the same error each time, or a different one",
}

// leversFor are the levers a signal may carry, never nil; a worse change
// and a better one get the one that fits, a resolution none.
func leversFor(s metrics.Signal) []string {
	if s.State != nil {
		if ls, ok := stateLevers[s.State.Key]; ok {
			return ls
		}
		return []string{}
	}
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
