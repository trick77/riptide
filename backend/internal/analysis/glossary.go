package analysis

import (
	"strings"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/metrics"
)

// The glossary is what the model is told a signal and a number mean, and
// the only levers it may pick. A lever names a step, never a cause riptide
// does not see: riptide knows a run's duration, status and agent wait, not
// its stages or tests; a PR's events, not why nobody merges it (a merge
// stop, a release date, a dependency, holidays). Only where the data
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

// Words are the page's words for one thing, German and English: what the
// model writes for it, never a term of its own (docs/metrics.md, "Words on
// the page"). StageWord stands for the production stage as the page
// writes it.
type Words struct {
	DE string `json:"de"`
	EN string `json:"en"`
}

// staged is w with the production stage put in, upper case as the page
// writes it.
func (w Words) staged(stage string) Words {
	return Words{strings.ReplaceAll(w.DE, metrics.StageWord, stage), strings.ReplaceAll(w.EN, metrics.StageWord, stage)}
}

// kindGloss is a signal kind: what it means, its levers, and the page's
// word for the kind (ui/src/i18n: overview.assess.kind.<kind>).
type kindGloss struct {
	means  string
	levers []string
	word   Words
}

var kinds = map[string]kindGloss{
	metrics.SignalDataGap: {
		"A source stopped sending data days before the period's end. Its numbers say nothing about the work; never read them as a team that stopped.",
		[]string{LeverCheckSource}, Words{"Datenlücke", "data gap"},
	},
	metrics.SignalPRJam: {
		"Four or more PRs open a week or longer in one repo, drafts never counted. 'waiting' counts what those long-open PRs wait on (first_review, review, changes, merge, draft), not every open PR. Why they wait is not in the data: a merge stop, a release date, a dependency, holidays or nobody feeling responsible are all possible.",
		[]string{LeverWalkPRs, LeverAskWhyWaiting}, Words{"PR-Stau", "PR jam"},
	},
	metrics.SignalSlower: {
		"A repo's builds of a kind typically take 1.5 times as long as in the previous period or more: slower than itself, not than other repos. What inside the build got slower is not in the data.",
		[]string{LeverInspectBuildLog}, Words{"langsamer geworden", "got slower"},
	},
	metrics.SignalFailingMore: {
		"A repo's builds of a kind fail far more often than in the previous period. Whether tests, infrastructure or real breakage is not in the data.",
		[]string{LeverInspectFailures}, Words{"scheitert öfter", "fails more often"},
	},
	metrics.SignalChange: {
		"A team number moved notably against the previous period: 25 % or more, 5 points for a share, and for a share on few cases or deploys on few releases a step of at least two. tone good: notably better; tone attention: notably worse. drivers: the repos that carry a worsening, a rough split.",
		[]string{LeverFindWhatChanged, LeverNameWhatHelped}, Words{"wichtige Veränderung", "notable change"},
	},
	metrics.SignalAgentWait: {
		"Builds of a kind spend half their time or more, and two minutes or more, waiting for a build agent: the platform's capacity, not the team's builds.",
		[]string{LeverPlatformAgents}, Words{"Warten auf Build-Agents", "waiting for build agents"},
	},
	metrics.SignalResolved: {
		"A problem an earlier assessment of this window raised is back at or better than its value before it was raised (a jam: clearly below four long-open PRs; a state: red a week before, judged clear again, or its dated episode over, a merge pause or a run of failed builds, which its card may still show with its date). metric names the signal it was. No step: say what is back, do not recommend.",
		nil, Words{"erledigt", "resolved"},
	},
	metrics.SignalState: {
		"A state: a finding that stands without a previous period, judged per unit (project, repo, App, branch) against the team's own threshold, never pooled. state.key says which (states lists their meaning and the page's words); state.count units of state.judged fire, state.unjudged could not be judged, state.threshold is what they were measured against (null for what the data cannot judge: it measures against no agreement). tone attention: a finding; hint: a hint; limit: what the data cannot judge, never a finding about the team.",
		nil, Words{"Befund", "finding"},
	},
}

// Term is one thing the page names, in its words. NotDE and NotEN are the
// words the page never uses for it in that language; the model must not
// either, nor translate a term otherwise than the page does. A word under
// one language's list may be the other's page word (Approval in German,
// approval in English).
type Term struct {
	Thing string `json:"thing"`
	Words
	NotDE string `json:"not_de,omitempty"`
	NotEN string `json:"not_en,omitempty"`
}

// vocabulary is the page's word for each thing it names, the model's fixed
// words: docs/metrics.md "Words on the page" keeps the same list for the
// page's writers. TestVocabularyIsThePages holds each word to the catalogs.
// The owner's words (2026-11-26): Default-Branch, Projekt, Library and the
// noun Rollout, never Hauptbranch, Produkt, Bibliothek or the noun
// Ausrollen.
var vocabulary = []Term{
	{"a PR merged into its repo", Words{"gemergter PR (gemerged)", "merged PR"}, "merged das Team", ""},
	{"PRs merged per week", Words{"gemergte PRs pro Woche", "merged PRs per week"}, "Throughput, PR Throughput", "throughput"},
	{"changed lines of a PR", Words{"PR-Grösse", "PR size"}, "Batch Size", "batch size"},
	{"a deploy to production with a new version", Words{"Release nach " + metrics.StageWord, "release to " + metrics.StageWord}, "", ""},
	{"how often releases come", Words{"Releases nach " + metrics.StageWord + " pro Woche", "releases to " + metrics.StageWord + " per week"},
		"Deploy-Frequenz", "deployment frequency"},
	{"one Argo CD sync of an App", Words{"das Deploy", "deploy"}, "Sync, Deployment", "sync"},
	{"what a deploy brings", Words{"Version", "version"}, "Image", "image"},
	{"a deploy that brings no new version", Words{"Deploy ohne neue Version", "deploy without a new version"},
		"Re-Sync, Sync ohne neue Images", "re-sync"},
	{"an App's first deploy", Words{"erstes Deploy", "first deploy"}, "Erstdeploy", ""},
	{"first commit to production", Words{"Zeit vom ersten Commit bis " + metrics.StageWord, "time from first commit to " + metrics.StageWord},
		"Lead Time", "lead time"},
	{"a piece of the way to production", Words{"Abschnitt", "section"}, "Etappe, Stufe", "leg"},
	{"INTG, SYST, PROD", Words{"Stage", "stage"}, "Stufe, Umgebung", "environment"},
	{"bring code to production (the verb)", Words{"ausgerollt (ausrollen)", "rolled out (roll out)"}, "ausliefern", "deliver"},
	{"bringing code to production (the noun)", Words{"Rollout", "rollout"}, "das Ausrollen, Auslieferung", "delivery"},
	{"the repo's main line", Words{"Default-Branch", "default branch"}, "Hauptbranch, Trunk", "main branch, trunk"},
	{"a person's OK on a PR", Words{"Freigabe, freigegeben", "approval, approved"}, "Approval, approved, approvt", ""},
	{"a repo no deploy ships", Words{"Library", "library"}, "Bibliothek", ""},
	{"the median", Words{"typisch", "typical"}, "Median, p50", "median, p50"},
	{"the 90th percentile", Words{"die langsamsten 10 %", "the slowest 10 %"}, "p90", "p90"},
	{"the weeks judged and the weeks before", Words{"Zeitraum, Vorperiode", "period, previous period"}, "Periode, Periodenende", ""},
	{"a Bitbucket project", Words{"Projekt", "project"}, "Produkt", "product"},
	{"a source stops sending", Words{"liefert keine Daten", "sends no data"}, "Stall, meldet nichts", "stall, reports nothing"},
	{"a CI run", Words{"Build", "build"}, "Lauf, Run", "run"},
	{"a build's result", Words{"erfolgreich, fehlgeschlagen", "succeeded, failed"}, "grün, rot", "green, red"},
	{"failed, then passed on the same commit", Words{"unzuverlässig", "flaky"}, "Flaky", ""},
	{"a repo without merged work", Words{"ohne Aktivität", "no activity"}, "inaktiv (for a repo)", "inactive (for a repo)"},
	{"a PR without events for weeks", Words{"inaktiv", "inactive"}, "", ""},
	{"the team's own threshold", Words{"Abmachung", "agreement"}, "", ""},
	{"what a rule found", Words{"Befund, Hinweis, nicht beurteilbar", "finding, hint, cannot be judged"}, "", ""},
	{"trouble after a rollout", Words{"Ärger nach dem Rollout", "trouble after a rollout"}, "Instability", "instability"},
}

// vocabularyFor is the vocabulary with the view's production stage.
func vocabularyFor(stage string) []Term {
	out := make([]Term, len(vocabulary))
	for i, v := range vocabulary {
		v.Words = v.staged(stage)
		out[i] = v
	}
	return out
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
// en.ts overview.assess.state.<key>.title), the production stage as
// metrics.StageWord (stateGlosses puts in the view's).
var states = map[string]StateGloss{
	metrics.StateNoRelease: {"A project rolled out, with merged work, released to production longer ago than the team's agreement allows (at least one release per two-week sprint). value: days since its last release.",
		"Lange kein Release nach {{prod}}", "No release to {{prod}} for long"},
	metrics.StateReleaseDue: {"A project's last release to production lies more than a sprint back, not yet past the agreement's limit. A hint.",
		"Release nach {{prod}} bald fällig", "Release to {{prod}} due soon"},
	metrics.StateNotOnProd: {"A repo's merged PRs are not on production yet, the oldest longer than the team's agreement allows. value: days since the oldest reached the default branch.",
		"Gemergte PRs lange nicht auf {{prod}}", "Merged PRs not on {{prod}} for long"},
	metrics.StateSlowToProd: {"A repo's merged PRs typically take longer from merge to production than the team's agreement allows.",
		"Lange Wartezeit vom Merge bis {{prod}}", "Long wait from merge to {{prod}}"},
	metrics.StateMergePause: {"A project's week in which hardly any PR was merged against its typical week, while at the week's end several PRs were open with a person's approval. value: those PRs. Why is not in the data (a merge stop, a release date).",
		"Kaum gemerged, obwohl freigegebene PRs warteten", "Hardly merged while approved PRs waited"},
	metrics.StateApprovedWaiting: {"A repo's approved PRs wait for their merge longer than the team's threshold, at the period's end.",
		"Freigegeben, aber nicht gemerged", "Approved, but not merged"},
	metrics.StateBotLoop: {"A bot branch's builds failed again and again in a row: CI time wasted. Never a problem of the team's code.",
		"Bot-Branch verbraucht CI-Zeit mit fehlgeschlagenen Builds", "Bot branch burning CI time on failed builds"},
	metrics.StateUntraced: {"A repo's merged PRs cannot be placed on production: its delivery cannot be judged.",
		"Nicht messbar, ob gemergte PRs auf {{prod}} sind", "Not measurable whether merged PRs are on {{prod}}"},
	metrics.StateUnattributed: {"Releases to production that belong to no project: the release agreement cannot be judged for the projects they belong to.",
		"Releases nach {{prod}} ohne Projekt", "Releases to {{prod}} without a project"},
	metrics.StateNoProdStage: {"Argo CD reports deploys, but none to the production stage: the stage configuration or the notification is off.",
		"Argo CD liefert keine Daten zur Stage {{prod}}", "Argo CD sends no data for the stage {{prod}}"},
	metrics.StateUnmeasurable: {"A state (state.was) that held a week before can no longer be judged for units it named then: the data no longer tells. It has no numbers to word. Never call it solved, never a problem of the team.",
		"Nicht mehr messbar", "No longer measurable"},
	metrics.StateDeployFailing: {"An App's last deploy to production failed and none succeeded since, for longer than the threshold.",
		"Letztes Deploy nach {{prod}} fehlgeschlagen", "Last deploy to {{prod}} failed"},
	metrics.StateDeployFailures: {"An App's deploys to production failed again and again in the period, each time succeeding later. A hint.",
		"Deploys nach {{prod}} scheitern immer wieder", "Deploys to {{prod}} fail again and again"},
	metrics.StateFirstReviewWait: {"A repo's ready PRs wait for their first review longer than the threshold, or its PRs typically do.",
		"PRs warten auf ein erstes Review", "PRs wait for a first review"},
	metrics.StateTrunkFailing: {"A repo's default-branch builds failed for longer than the threshold, with several failed builds.",
		"Builds auf dem Default-Branch lange fehlgeschlagen", "Builds on the default branch failing for long"},
	metrics.StateBranchFailing: {"A person's feature branch whose builds failed many times in a row.",
		"Builds eines Branches scheitern immer wieder", "A branch's builds fail again and again"},
	metrics.StateFlakyBuilds: {"A repo's builds of a kind mostly failed for no code change: a retry on the same commit succeeded.",
		"Unzuverlässige Builds", "Flaky builds"},
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

// stageWord is the production stage as the page writes it: upper case.
func stageWord(stage string) string {
	if stage == "" {
		stage = config.DefaultProductionStage
	}
	return strings.ToUpper(stage)
}

// stateGlosses are the states' glosses with the production stage (as
// stageWord writes it) in their titles.
func stateGlosses(word string) map[string]StateGloss {
	out := make(map[string]StateGloss, len(states))
	for k, g := range states {
		w := Words{g.DE, g.EN}.staged(word)
		g.DE, g.EN = w.DE, w.EN
		out[k] = g
	}
	return out
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
	"silent_days":             "days since the source's last data, at the period's end",
	"open_long":               "PRs of the repo open 7 days or longer; fewer is better",
	"open":                    "open PRs of the repo",
	"age_p50":                 "typical (median) age of the repo's open PRs since they turned ready for review, drafts out",
	"failure_rate":            "share of the repo's builds of the kind that failed; lower is better",
	"wasted":                  "CI time of the failed builds",
	"runs":                    "builds with a result",
	"build_p50":               "typical (median) duration of the successful builds; shorter is better",
	"build_wait_p50":          "typical (median) wait for a build agent per successful build",
	"build_work_p50":          "typical (median) build time per successful build, after it got a build agent",
	"deploy_per_week":         "releases to production per week; more is better",
	"deploy_success":          "share of deploys to production that succeeded; higher is better",
	"lead_time_p50":           "typical (median) time from a PR's first commit to production; shorter is better",
	"review_p50":              "typical (median) time from PR ready to its first approval; shorter is better",
	"within_day":              "share of PRs merged within a day of opening; higher is better",
	"jammed_repos":            "repos with four or more PRs open a week or longer; fewer is better",
	"build_success":           "share of a build kind's builds that succeeded; higher is better",
	"merged_per_week":         "people's PRs merged per week; more is better",
	"batch_size":              "PR size: typical (median) changed lines (added plus removed) per merged PR of a person; smaller is better",
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
	"state_not_on_prod":       "days since the merge of the worst repo's oldest PR not yet on production",
	"state_slow_to_prod":      "typical days from merge to production in the worst repo",
	"state_merge_pause":       "PRs open with a person's approval at the end of the worst project's paused week",
	"state_approved_waiting":  "working days the worst repo's oldest approved PR has waited for its merge",
	"state_bot_loop":          "failed builds in a row on the worst bot branch",
	"state_untraced":          "merged PRs of the worst repo that cannot be placed on production",
	"state_unattributed":      "releases to production that belong to no project",
	"state_no_prod_stage":     "stages Argo CD reported deploys to, none of them production",
	"state_deploy_failing":    "working days the worst App's production deploy has stood on a failure",
	"state_deploy_failures":   "times the worst App's production deploys failed in a row, each time until one succeeded",
	"state_first_review_wait": "working days the worst repo's oldest ready PR waits for its first review, or its typical wait for one",
	"state_trunk_failing":     "working days the worst repo's default-branch builds failed in a row",
	"state_branch_failing":    "failed builds in a row on the worst feature branch",
	"state_flaky_builds":      "builds of the worst repo that failed, then passed on the same commit (unzuverlässig / flaky)",
	"state_long_builds":       "minutes a successful build of the worst repo typically takes",
	"state_release_failing":   "share of the worst repo's release builds that failed",
	"state_builds_silent":     "PRs the worst repo merged while its CI reported no build",
	"state_big_prs":           "share of the worst repo's sized PRs over the line threshold",
	"state_fast_approval":     "big PRs of the worst repo approved within minutes",
	"state_unapproved":        "share of the worst repo's merged PRs without a person's approval",
	"state_late_decline":      "PRs declined after long work",
	"state_branch_life":       "typical working days from a branch's first commit to its PR being ready, worst repo",
	"state_branch_open":       "days the worst repo's oldest branch without a PR has been worked on since its last long silence",
	"state_branch_silent":     "branches without a PR not pushed for weeks, worst repo",
}

// leverText is what each lever says, the page's own words
// (ui/src/i18n: overview.assess.lever.<key>), for the model to pick by; the
// page shows the chosen one in them, the model never rewords it.
var leverText = map[string]Words{
	LeverWalkPRs: {"die PRs einzeln durchgehen: reviewen, mergen, schliessen oder bewusst parken",
		"walk through the PRs one by one: review, merge, close or park them on purpose"},
	LeverAskWhyWaiting: {"klären, worauf sie warten: ein Merge-Stopp, eine Abhängigkeit, oder niemand fühlt sich zuständig",
		"find out what they wait on: a merge stop, a dependency, or nobody feeling responsible"},
	LeverInspectBuildLog: {"im Build-Log der langsamsten Builds nachsehen, welcher Schritt die Zeit braucht",
		"look in the build log of the slowest builds which step takes the time"},
	LeverInspectFailures: {"die fehlgeschlagenen Builds ansehen: jedes Mal derselbe Fehler oder ein anderer?",
		"look at the failed builds: the same failure each time, or a different one?"},
	LeverPlatformAgents: {"mit dem Plattform-Team über mehr Build-Agents sprechen",
		"talk with the platform team about more build agents"},
	LeverFindWhatChanged: {"erst klären, was sich geändert hat, dann etwas vereinbaren",
		"first find out what changed, then agree on something"},
	LeverNameWhatHelped: {"benennen, was geholfen hat, damit es bleibt", "name what helped, so it stays"},
	LeverCheckSource: {"prüfen, wie die Quelle an riptide angebunden ist",
		"check how the source is connected to riptide"},
	LeverPlanRelease: {"das nächste Release nach " + metrics.StageWord + " planen oder benennen, was es aufhält",
		"plan the next release to " + metrics.StageWord + ", or name what holds it"},
	LeverStopBotLoop: {"den Bot-Branch ansehen: beheben, woran seine Builds scheitern, oder ihn schliessen",
		"look at the bot branch: fix what its builds trip over, or close it"},
	LeverWalkBranches: {"die Branches durchgehen: erledigte löschen, einen PR öffnen oder den Rest bewusst parken",
		"go through the branches: delete the finished ones, open a PR or park the rest on purpose"},
	LeverInspectDeploys: {"die fehlgeschlagenen Deploys in Argo CD ansehen: jedes Mal derselbe Fehler oder ein anderer?",
		"look at the failed deploys in Argo CD: the same error each time, or a different one?"},
}

// leverTexts are the levers' words with the view's production stage.
func leverTexts(stage string) map[string]Words {
	out := make(map[string]Words, len(leverText))
	for k, w := range leverText {
		out[k] = w.staged(stage)
	}
	return out
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
