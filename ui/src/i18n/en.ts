// English UI catalog, the source of truth for the shape: de.ts must mirror it
// (`satisfies Catalog`), so a key added here and forgotten there fails the
// build. Grouped by section. Numbers arrive formatted (format.ts, de-CH in
// both languages): a catalog never formats.
//
// DORA, SPACE, DevEx, Core 4 and Flow terms are the frameworks' own English
// names in both languages. Pill words start with a capital in both.
//
// The words the facts search matches (PR states, deploy classes, change
// types) live in terms.json: the backend's search keeps a copy of them
// (prStateTerm, deployTerm, changeTypeTerm in internal/metrics/facts.go),
// and TestSearchTermsMatchTheUI fails when the two drift.
import terms from "./terms.json";

export const en = {
  lang: {
    code: "EN",
    switch: "Language: English. Switch to German",
  },
  nav: {
    label: "Sections",
    ueberblick: "Overview",
    fluss: "Flow",
    deploys: "Deploys",
    arbeitsmix: "Work mix",
    build: "Build",
    kosten: "Costs",
    daten: "Data",
    frameworks: "Frameworks",
  },
  common: {
    cw: "CW {{kw}}",
    hours: "{{n}} h",
    nOf: "{{n}} of {{total}}",
    nPRs_one: "{{count}} PR",
    nPRs_other: "{{count}} PRs",
    lever: "Lever",
    typical: "{{unit}}, typical value",
    slowest: "the slowest 10 %: over {{p90}}",
    lowSample: "not very reliable",
    noMergedPRs: "No merged PRs in the period.",
    shorterBetter: "shorter is better",
    ownHistory: "compare only with your own history",
    noDirection: "no direction",
    teamQuestion: "Question for the team",
    withoutBots: "without bots",
    withoutGitOps: "without GitOps repos",
    // Counted nouns, for sentences that hold more than one count.
    releases_one: "{{count}} release",
    releases_other: "{{count}} releases",
    resyncs_one: "{{count}} sync without new images",
    resyncs_other: "{{count}} syncs without new images",
    firstDeploys_one: "{{count}} first deploy",
    firstDeploys_other: "{{count}} first deploys",
    revisions_one: "{{count}} revision",
    revisions_other: "{{count}} revisions",
  },
  units: {
    s: "{{n}} s",
    min: "{{n}} min",
    h: "{{n}} h",
    days: "{{n}} days",
  },
  page: {
    team: "Team",
    print: "Print / PDF",
    title: "Engineering metrics",
    scope:
      "Team {{team}}, {{product}} · CW {{from}}–{{to}} · {{start}} to {{end}} · compared with the {{weeks}} weeks before",
    oneProduct: "product {{product}}",
    allProductsLower: "all products",
    mirror:
      "A mirror for the team, not a ranking of teams or people. Every number says what it means, which way is good and whether it changed against the previous period.",
    dataAsOf: "Data as of:",
    source: "{{source}} {{at}}",
    noData: "no data",
    filter: "Filter",
    period: "Period",
    weeks: "{{n}} W",
    product: "Product",
    allProducts: "All products",
    repos_one: "{{count}} repo",
    repos_other: "{{count}} repos",
    bots: "Include bots",
    reposHead: "Repos",
    appsHead: "Argo apps",
    none: "none",
    mapping:
      "Mapped from the data: repos by the Bitbucket project key, apps by the GitOps repo whose revision they deploy. No name list.",
    loadFailed: "Could not load the data: {{msg}}",
    noTeam: "No team is configured.",
    loading: "Loading",
    footer:
      "Riptide only reads the raw data. Every number traces back to its query under “Source”.",
  },
  compare: {
    better: "Better",
    worse: "Worse",
    same: "Unchanged",
    none: "no comparison",
    low_sample: "no comparison: too little data in one of the two periods",
    incomplete:
      "no comparison: the previous period's data is incomplete, or first deploys dominate",
    no_value: "no comparison: a value is missing",
    not_reporting: "no comparison: Argo CD does not report for the team yet",
    previous: "Previous period",
    now: "now",
    undirected: "no direction",
  },
  strip: {
    title: "Against the previous period",
    text: "Worse to the left, better to the right; the length is the size of the change, capped at halving or doubling. Numbers without a direction are left out.",
    aria: "Each number against the previous period",
    reason: {
      low_sample: "too little data",
      incomplete: "previous period incomplete",
      no_value: "no value",
    },
    buildSuccess: "Success rate, {{kind}}",
  },
  why: {
    button: "What does this mean?",
    means: "What does this mean? · {{title}}",
    watch: "Look closer when",
    def: "Source · definition",
    basis: "Data basis",
    coverage: "Coverage",
    query: "Query:",
  },
  frameworksLabel: "Classification",
  chart: {
    noData: "no reliable data",
  },
  overview: {
    eyebrow: "Overview",
    title: "What now?",
    intro:
      "Findings from the rules Riptide applies, plus a fixed conversation starter. Each names its rule and number and leads to the facts behind it.",
    none: "Nothing stands out under the rules Riptide applies.",
    tone: {
      attention: "Attention",
      hint: "Hint",
      talk: "Conversation starter",
    },
    area: {
      flow: "Flow",
      review: "Review",
      build: "Build",
    },
    stageLever: {
      coding:
        "smaller steps, open the PR earlier so review and deploy start earlier",
      review: "agree on review times in the team, cut PRs smaller",
      merge: "merge right after the approval; check merge checks and sign-offs",
      to_pre: "deploy to the pre-environment automatically after the merge",
      to_prod:
        "smaller, more frequent releases; check whether PROD is tied to a fixed date",
    },
    stageTitleProd:
      "{{share}} of the time to {{prod}}, the code is finished and only waits for the deployment.",
    stageTitle:
      "{{share}} of the time to {{prod}} is spent in the “{{name}}” stage.",
    stageBodyProd:
      "A change is typically finished after {{before}}; then it typically waits {{wait}} for the deployment.",
    stageBody: "{{desc}}: typically {{p50}}, the slowest 10 % over {{p90}}.",
    ruleAttention:
      "Rule: stage with the longest median, from {{share}} of the time and {{min}} PRs (starting value)",
    ruleLowSample:
      "Rule: stage with the longest median. Below {{min}} PRs too thin for “Attention”, hence only a hint",
    ruleHint:
      "Rule: stage with the longest median. There always is one, hence only a hint; “Attention” from {{share}} of the time and {{min}} PRs",
    toFlow: "To the flow →",
    firstTouchTitle: "{{rate}} of approvals come without a prior comment.",
    firstTouchBody:
      "The value is neither good nor bad: well-practised pairs approve without a comment, and so does anyone who waves changes through.",
    firstTouchQuestion: "Do we discuss big changes outside Bitbucket?",
    firstTouchRule:
      "Always shown: no threshold, no direction · {{n}} of {{total}} approvals",
    toReview: "To the review →",
    openTitle_one: "PRs pile up in {{count}} repo.",
    openTitle_other: "PRs pile up in {{count}} repos.",
    openBody:
      "Most in {{repo}}: {{n}} PRs open {{days}} days or longer, median {{p50}}. Still without a first review: {{first}} of the {{jammed}} jammed PRs.",
    openLever: "bring old PRs up in the daily: review, merge or close them",
    openRuleAttention:
      "Rule: a jam is {{min}} or more PRs open {{days}} days or longer in the same repo; “Attention” when more repos are jammed than at the end of the previous period ({{prev}}), otherwise a hint",
    openRuleHint:
      "Rule: a jam is {{min}} or more PRs open {{days}} days or longer in the same repo; “Attention” when more repos are jammed than at the end of the previous period",
    openPRsOf: "View open PRs of {{repo}} →",
    runsOf: "View the runs of {{repo}} →",
    flakyTitle: "{{repo}}: {{rate}} of {{kind}} fail.",
    flakyBody:
      "{{failures}} of {{runs}} runs red, {{prevRate}} in the previous period. That costs {{hours}} h of CI time and a restart each time.",
    flakyLever:
      "find and isolate unstable tests instead of just restarting runs",
    flakyRule:
      "Rule: flagged from {{factor}}× its own rate of the previous period and {{points}} points above it, at most halfway to 100 %, with {{failures}} red runs or more, from {{min}} runs in both periods (starting value)",
    slowTitle:
      "{{repo}} delivers feedback {{factor}}× slower than in the previous period.",
    slowBody:
      "{{kind}} typically take {{p50}}, {{prevP50}} in the previous period. That is how long whoever pushes waits.",
    slowLever:
      "use the build cache, run tests in parallel, move slow integration tests out of the PR build",
    slowRule:
      "Rule: median from {{factor}}× its own of the previous period, from {{min}} runs in both periods (starting value)",
    sinceNote:
      "Argo CD only reports since {{date}}: the weeks before are empty, the rate applies from then on.",
    sinceLow: " Less than a week of data: not very reliable.",
    throughput: "Throughput and instability",
    throughputText:
      "This is how DORA groups the five metrics. Both sides belong together: more throughput with rising instability is no progress.",
    freq: {
      label: "Deployment frequency",
      dora: "DORA · Deployment frequency · Throughput",
      question: "How often does new code go to {{prod}}?",
      unit: "releases per week",
      note_one: "{{count}} release in the period",
      note_other: "{{count}} releases in the period",
      noDeploys: "no deploys in the period",
      notReporting: "Argo CD does not report for the team yet",
      direction: "more often is better",
      directionWhy: "smaller releases, less risk per release",
      firstSeen_one:
        "{{count}} first deploy without a comparison value, not counted",
      firstSeen_other:
        "{{count}} first deploys without a comparison value, not counted",
      means:
        "How often new code reaches the users. Frequent small releases carry little risk per release and bring feedback early.",
      watch:
        "the weeks swing widely or releases stop: then changes pile up into big releases.",
      lever:
        "smaller changes, automated deploys, no fixed release date for PROD.",
      def: "Successful deploys to {{prod}} whose images differ from the previous deploy of the same Argo app, counted once per GitOps revision, divided by the weeks in the period in which Argo CD reported.",
      basis:
        "{{releases}}, {{resyncs}} and {{firstSeen}} from {{syncs}}. A release that updates several apps through the same GitOps revision counts once.",
      filterImages: "images ≠ previous deploy",
      filterRevision: "per revision",
      coverageFirst:
        "An app's first deploy since the data began has no predecessor: release or re-sync is unknown, so it does not count as a release.",
      coverageAll:
        "Every app has a predecessor in the data, all deploys are comparable.",
    },
    lead: {
      label: "Lead time to {{prod}}",
      dora: "DORA · Change lead time · Throughput",
      question: "How long does a change take from commit to {{prod}}?",
      noHuman: "no human commits mapped",
      slowest_one:
        "the slowest 10 %: over {{p90}} · {{count}} commit by people",
      slowest_other:
        "the slowest 10 %: over {{p90}} · {{count}} commits by people",
      bots: " · bots typically {{p50}} ({{n}})",
      directionWhy: "faster feedback from real users",
      lowSample: "Fewer than 10 commits: not very reliable",
      means:
        "The time in which a finished change helps no user yet. Long means: feedback comes late, failures are hard to pin to a change, releases grow big.",
      watch:
        "the slowest 10 % sit far above the typical value: then single changes get stuck, often behind a release date.",
      lever:
        "Where the time goes is shown by the flow below; that is where the lever is, not in the short stages.",
      def: "Per commit, from the author time to the first deploy to {{prod}} that contains it. Merge commits and release tooling excluded, bots separate.",
      basis_one:
        "{{count}} human commit, first deployed in the period{{bots}}.",
      basis_other:
        "{{count}} human commits, first deployed in the period{{bots}}.",
      basisBots: ", plus {{n}} by bots",
      filterBots: "bots separate",
      coverage:
        "Only commits Riptide can map to a deployment (the build's image reference or release notes); a deployment without a mapping is missing here.",
    },
    dsucc: {
      label: "Deploy success",
      dora: "not a DORA metric",
      question: "Does the rollout itself work?",
      unit: "of Argo syncs",
      note_one: "{{failed}} of {{count}} sync failed",
      note_other: "{{failed}} of {{count}} syncs failed",
      direction: "higher is better",
      means:
        "Whether the rollout itself works: an aborted or failed sync means someone has to step in.",
      watch:
        "the value drops: then deploys fail on configuration, resources or dependencies.",
      lever:
        "look at the failed syncs under “Data”; it is mostly the same app or the same cause.",
      def: "Share of syncs to {{prod}} with operation_phase Succeeded among all finished syncs. Not a DORA metric: the change fail rate counts deploys after which someone has to step in, even when the sync succeeded.",
      basis: "{{finished}} finished syncs, {{failed}} of them Failed.",
      coverage: "Complete: Argo CD reports every finished sync.",
    },
    instability: {
      label: "Instability",
      dora: "DORA · Change fail rate · Deployment rework rate · Failed deployment recovery time",
      question:
        "How often does a deployment cause trouble, and how fast is it fixed?",
      value: "not measured",
      text: "This needs an incident source. Without one, Riptide shows throughput only; the instability side is missing. The hotfix share or deploy success do not replace it.",
    },
  },
  flow: {
    stage: {
      coding: "Coding",
      codingDesc: "first commit → PR ready",
      review: "Review",
      reviewDesc: "PR ready → approval",
      merge: "Merge",
      mergeDesc: "approval → merged",
      toPre: "To {{pre}}",
      toPreDesc: "merged → on {{pre}}",
      toProd: "Waiting for {{prod}}",
      toProdDescPre: "on {{pre}} (without: merged) → on {{prod}}",
      toProdDesc: "merged → on {{prod}}",
    },
    bucket: {
      days: "1–3 days",
      overDays: "> 3 days",
      upTo: "up to {{d}}",
      from: "from {{d}}",
    },
    chainTitle: "Share of each stage in the time to {{prod}}",
    chainSub:
      "Per PR in app repos, typical duration per stage. The lead time above counts per commit; the stages show where the time goes.",
    leadTo: "Lead time to {{env}}:",
    typicalSlowest: "typical value · the slowest 10 %: over {{p90}}",
    gap: "The gap between the two is the time before {{prod}}: if it lies there, the pipeline is fast and the release is the bottleneck.",
    shareAria: "Share of the time to {{prod}}: {{parts}}",
    ofTime: "· {{share}} of the time",
    hot: "The lever is at “{{name}}”",
    hotRest:
      ": {{share}} of the time to {{prod}}. Speeding up the short stages brings little.",
    chainNote:
      "Review is a stage of its own because the approval is mostly the reviewer's first contact. Only PRs whose merge commit maps to a deployment reach the last two stages.",
    people: "People",
    bots: "Bots",
    eyebrow: "From commit to {{prod}} · app repos only",
    title: "Where does a change wait?",
    lead: "The time to the user, split into stages. Where most of the time lies, the lever lies; speeding up the short stages brings little.",
    fwLeadTime: "Change lead time, broken down",
    reviewTitle: "Review: fast, but how thorough?",
    reviewSub:
      "Read both numbers together: optimising only one means waving through or holding up. PRs by people only{{bots}}.",
    reviewSubBots: "; bots separate in the histogram",
    review: {
      label: "Review time",
      question: "How long does a PR wait for its first approval?",
      note_one: "the slowest 10 %: over {{p90}} · {{count}} PR",
      note_other: "the slowest 10 %: over {{p90}} · {{count}} PRs",
      means:
        "How long a finished PR waits for its first approval. During that time the work stands still, and the author often starts on something new.",
      watch:
        "the slowest 10 % sit far above the typical value: single PRs are left lying, often big ones or ones without a clear reviewer.",
      lever:
        "agree on review times in the team, keep PRs small, name reviewers right when opening.",
      def: "From PR ready (opened; for drafts ready_for_review) to the first approval by a human reviewer.",
      basis_one: "{{count}} PR with an approval.",
      basis_other: "{{count}} PRs with an approval.",
      filterReviewer: "reviewer ≠ PR author",
      coverage: "Complete for Bitbucket events.",
    },
    touch: {
      label: "Approval without comment",
      question: "How often is the approval the reviewer's first reaction?",
      directionWhy: "a conversation starter, not a target",
      means:
        "How often the approval is the reviewer's first reaction. High means: review without visible discussion in Bitbucket.",
      watch:
        "the value rises together with very short review times: then changes may be waved through.",
      lever:
        "no target. Discuss in the team whether big changes are discussed elsewhere.",
      def: "Share of approved PRs whose first reviewer contact is the approval itself.",
      basis: "{{n}} of {{total}} approved PRs.",
      filterComments: "without bot comments",
      coverage: "Riptide does not see spoken reviews and pairing.",
    },
    cycle: {
      label: "Merged within 24 h",
      question: "How many PRs are merged within 24 h of opening?",
      unit_one: "{{n}} of {{count}} PR",
      unit_other: "{{n}} of {{count}} PRs",
      direction: "more is better",
      directionWhy: "small PRs, fast review",
      means:
        "How many PRs are merged within 24 hours of opening. After that the author has mostly moved on; questions then cost double.",
      watch:
        "many PRs stay open for more than three days: mostly big PRs or ones waiting on someone.",
      lever: "cut PRs smaller, ask for review early.",
      def: "pr:merged minus pr:opened per PR, key (repo_full_name, pr_id). The PR cycle time is typically {{p50}}, the slowest 10 % over {{p90}}.",
      basis: "{{count}} of {{n}} human PRs, merged in the period.",
      filterMerged: "pr:merged in the period",
      coverage:
        "PRs opened before the data began have no pr:opened and are missing.",
    },
    histHead: "PRs by time from opening to merge",
    histLine: " · the line marks 24 h",
    histAria: "PR cycle time: {{parts}}",
  },
  open: {
    title: "Open PRs: where do they pile up?",
    sub: "As of {{at}}, whenever opened. A PR open {{days}} days or more counts as long open; {{min}} long-open ones in the same repo are a jam. GitOps repos left out{{bots}}.",
    subBots: ", bots counted in",
    jammed: {
      label: "Jammed repos",
      question:
        "In how many repos have {{min}} or more PRs been open {{days}} days or longer?",
      unit_one: "with {{count}} long-open PR",
      unit_other: "with {{count}} long-open PRs",
      note_one: "{{count}} PR long open in all",
      note_other: "{{count}} PRs long open in all",
      direction: "fewer is better",
      directionWhy:
        "several PRs left lying in the same repo are a jam; a single one is a single case",
    },
    all: {
      label: "Open PRs",
      question: "How many PRs are open right now?",
      unit: "typically {{p50}} old",
      note: "the oldest 10 %: over {{p90}}",
      directionWhy: "more open PRs can also mean more work under way",
    },
    longWaitOn: "The long-open ones wait on:",
    inactive_one:
      "{{count}} PR inactive: no event for {{days}} days, in no number above. A lost merge event looks the same.",
    inactive_other:
      "{{count}} PRs inactive: no event for {{days}} days, in no number above. A lost merge event looks the same.",
    viewInactive: "View →",
    jam: "Jam",
    colRepo: "Repo",
    colOpen: "Open",
    colLong: "≥ {{days}} d",
    colMedian: "Median age",
    colWaiting: "Waiting on",
    colInactive: "Inactive",
    viewPRs: "View PRs →",
    none: "No PR was open at the end of the period.",
    mekko: {
      head: "Who waits on what: width = open PRs in the repo, height = share per wait, inactive ones left out.",
      aria: "Open PRs per repo, by what they wait on",
      of_one: "{{n}} of {{open}} open PR",
      of_other: "{{n}} of {{open}} open PRs",
      long: "{{long}} of them open ≥ {{days}} days",
      longMark: "open ≥ {{days}} days",
      draft: "not waiting on the team",
    },
    why: {
      means:
        "Every other PR number counts a PR only once it is merged. A PR open for weeks is missing there entirely; here it shows, per repo and with what it waits on.",
      watch:
        "several long-open PRs in the same repo wait for a first review: a reviewer is missing there, or the PRs are too big.",
      lever:
        "name reviewers when opening, bring old PRs up in the daily, close PRs nobody needs anymore.",
      def: "PRs open at the end of the period: opened before it, neither merged, declined nor deleted. Age from the PR's creation date, long open from {{days}} days, a jam from {{min}} long-open ones in the same repo. Per repo the median age, never the oldest: one alone decides that.",
      basis_one: "{{count}} open PR, as of {{at}}.",
      basis_other: "{{count}} open PRs, as of {{at}}.",
      coverage:
        "A PR whose merge or closing Bitbucket never reported stays open until, after {{inactive}} days without an event, it counts as inactive; alone it never makes a jam. A PR without any event since data began is missing.",
    },
  },
  deploys: {
    eyebrow: "Rollout",
    title: "How evenly does code reach {{prod}}?",
    lead: "Even is good: gaps and spikes point to bundled releases that ship many changes at once.",
    fwSpeed: "Speed, secondary",
    cardTitle: "Releases to {{prod}} per week",
    cardSub:
      "Only releases with new images; the line is the team's typical week",
    release: "Release",
    releases: "Releases",
    typical: "typically {{n}} per week",
    aria: "Releases to {{prod}} per week: {{parts}}",
    note: "{{resyncs}} and {{firstSeen}} do not count; what they are is under “What does this mean?” at the deployment frequency. The typical week is the median over the whole weeks in which Argo CD reported.",
  },
  mix: {
    feature: "Feature",
    bugfix: "Bugfix",
    hotfix: "Hotfix",
    other: "Other / no prefix",
    eyebrow: "Work mix",
    title: "What is the team working on?",
    lead: "Share of merged PRs by branch prefix, not of time. No target: what matters is whether the mix fits the team's plan.",
    fwImpact: "Impact, proxy for “time on new work”",
    cardTitle: "Merged PRs by type",
    cardSub_one: "{{count}} PR in the period, {{bots}}",
    cardSub_other: "{{count}} PRs in the period, {{bots}}",
    botsIn: "bots included",
    aria: "Work mix: {{parts}}",
    barTitle_one: "{{label}}: {{count}} PR, {{share}}",
    barTitle_other: "{{label}}: {{count}} PRs, {{share}}",
    untracked: "{{rate}} without a Jira key",
    untrackedText:
      "({{n}} of {{total}}). This work shows up in no planning. Question for the team: key forgotten, or unplanned work?",
  },
  build: {
    // Searched by the backend too: terms.json.
    kind: terms.en.runKind,
    kindNote: {
      release: "build_type release or hotfix",
      main: "main branch, no release",
      feature: "feature/…, bugfix/…, PR-…",
      other: "release/… without a release, nightlies",
      unknown: "before the first build_type report",
    },
    sparkAria: "Success rate per week, {{kind}}: {{parts}}",
    successful: "successful",
    typicalDuration: "typical duration, all repos",
    repos:
      "Of {{compared}} repos, {{slower}} got slower and {{flakier}} flakier than in the previous period",
    reposNone: "Repos: no comparison with the previous period",
    redRuns: "red runs, of {{h}} h CI time",
    waitingTitle:
      "Median over {{n}} of the {{total}} runs that report when they got an agent",
    waiting:
      "of which typically {{wait}} waiting for an agent, {{build}} building",
    scale: "CW {{kw}} · success, scale {{low}}–100 %",
    runs_one: "{{count}} run",
    runs_other: "{{count}} runs",
    noVerdict: " + {{n}} without a result",
    outliers: " · {{n}} outliers ({{h}} h) not in duration and CI time",
    flaky: "Got flakier",
    flakyText: "previous period {{prev}} → now {{rate}} red",
    slow: "Got slower",
    slowText: "previous period {{prev}} → now {{p50}} ({{factor}}×)",
    fine: "Unremarkable",
    noCompare: "No comparison",
    noCompareNoValue: "no comparable runs of this kind in the previous period",
    noCompareIncomplete: "build data of the previous period incomplete",
    noCompareLowSample: "under {{min}} runs in one of the two periods",
    fineText: "{{p50}}, {{rate}} red",
    fineWait: " · {{wait}} waiting, {{build}} building",
    repoWait: "Typically {{wait}} waiting for an agent, {{build}} building",
    group_one: "{{count}} run from {{repos}} repos started {{when}}",
    group_other: "{{count}} runs from {{repos}} repos started {{when}}",
    groupSameDay: "on {{day}} between {{from}} and {{to}}",
    groupSpan: "between {{from}} and {{to}}",
    groupRest:
      ": that points to the infrastructure (agents gone, a waiting input step), not to the repos.",
    colPipeline: "Pipeline",
    colKind: "Kind",
    colStart: "Start",
    colDuration: "Duration",
    colWaiting: "Waiting",
    colFactor: "× median",
    noAgent: "The run does not report when it got an agent",
    eyebrow: "CI · Jenkins · Tekton · each kind of run on its own",
    title: "How fast and reliable is the feedback?",
    lead: "Releases skip tests, feature builds fail more often. So there is no total across kinds: each card stands on its own.",
    fwEffectiveness: "Effectiveness, driver",
    dataFrom:
      "Build data from {{date}}: the first run Riptide received for the team. Older weeks stay empty rather than wrong.",
    noRuns: "No finished runs in the period.",
    legend:
      "Successful: SUCCESS (Tekton: Succeeded, Completed). Red: FAILURE, UNSTABLE or Failed. Without a result (ABORTED, NOT_BUILT): no run, only CI time; a Jenkins timeout() also ends ABORTED.",
    hotspotsTitle: "Repos that slow down the feedback",
    hotspotsSub:
      "Each repo against its own runs of the same kind in the previous period · the thresholds are starting values, not measured",
    kindGroup: "Kind of run",
    colRepo: "Repo",
    colWhy: "Why",
    colRed: "Red CI time",
    viewRuns: "View runs →",
    noneOfKind: "No runs of this kind with a result.",
    slowestTitle: "Longest builds",
    slowestSub:
      "By median, outliers aside, from {{min}} runs with a result and a duration. No threshold: unremarkable repos too.",
    colMedian: "Median",
    colSlowest: "Slowest 10 %",
    colWaitBuild: "Waiting / building",
    colRuns: "Runs",
    noAgentRepo: "None of the runs reports when it got an agent",
    hotspotsRule:
      "Got flakier: rate at least {{flaky}}× the previous period's and {{points}} points above it, at most halfway between it and 100 %, with {{failures}} red runs or more. Got slower: median at least {{slow}}× the previous period's. Both from {{min}} runs in both periods. Never against the team: a kind mixes Gradle, npm and other builds, so a UI build with smoke tests would always be slow.",
    stuckTitle: "Stuck runs",
    stuckSub: "Runs over {{factor}}× the median of their kind and over {{min}}",
    stuck_one: "1 run got stuck",
    stuck_other: "{{count}} runs got stuck",
    stuckRest:
      " in the period. Their time counts in neither duration nor CI time.",
    showLongest: "Show the {{n}} longest",
    showAll: "Show runs",
    noOutliers: "No outliers in the period.",
    stuckRule:
      "A repo with at least {{min}} runs of its own with a result and a duration must also exceed its own median {{factor}}×: a slow repo is no outlier. Aborted runs still appear.",
  },
  quiver: {
    aria: "{{kind}}: each repo from the previous period to now, median duration against failure rate",
    xAxis: "Median duration (log) →",
    yAxis: "↑ Failure rate",
    slower: "▲ ×{{factor}} duration",
    flakier: "▲ failure rate {{prev}} → {{rate}}",
    median: "Median",
    rate: "Failure rate",
    runs: "Runs",
    ci: "CI time",
    legend:
      "Arrow: previous period → now; a dot in or at the circle: barely moved · filled circle without a dot: only the median or only the failure rate compares · circle area: share of this kind's CI time · red: got slower or flakier, as below · dashed: no comparison.",
    unplotted_one:
      "{{count}} repo of this kind is not drawn: past the list, without a run with a result, or without a median.",
    unplotted_other:
      "{{count}} repos of this kind are not drawn: past the list, without a run with a result, or without a median.",
  },
  costs: {
    eyebrow: "FinOps · noergler",
    title: "What do the AI reviews cost?",
    lead: "Spend on the automatic reviews in the period. No framework measures this; the numbers help weigh the reviews' cost against their benefit.",
    spend: "Spend",
    spendNote_one: "{{merged}} of it for {{count}} merged PR",
    spendNote_other: "{{merged}} of it for {{count}} merged PRs",
    promotionNote_one: "{{promotion}} for {{count}} promotion review",
    promotionNote_other: "{{promotion}} for {{count}} promotion reviews",
    wasted: "Discarded",
    wastedQuestion: "How much went to PRs that were never merged?",
    wastedWhy: "high means: many drafts get reviewed and then discarded",
    perKloc: "Per 1000 changed lines",
    perKlocQuestion: "What does the review of a merged change cost?",
    trendOnly: "read only over time",
    perKlocWhy: "if the value rises, reviews get more expensive per change",
    byProduct: "By product",
    byRepo: "By repo",
    sub: "All reviews in the period, latest rollup per PR",
    none: "No noergler reviews in the period.",
    reviewedRepos: "Only repos noergler reviews: {{n}} in the period.",
  },
  facts: {
    eyebrow: "The single facts behind every number",
    title: "Data",
    lead: "Every number above is made of these PRs, deploys and runs. The search filters only these tables.",
    searchLabel: "Search by repo, app, PR or Jira key",
    placeholder: "Repo, app, PR, Jira key …",
    failed: "The single facts could not be loaded; the message is above.",
    tabs: "Facts table",
    tabPRs: "PRs",
    tabDeploys: "Deploys",
    tabRuns: "Runs",
    filtered: "filtered by “{{q}}”",
    newest: "Newest first",
    openFirst:
      "Open ones first, oldest on top, inactive after; then the newest",
    openFor: "open for {{age}}",
    idleFor: "no event for {{idle}}",
    firstOf: " · the first {{limit}} of {{count}}",
    noHits: "No hits for “{{q}}”.",
    empty: "No entries in the period.",
    colPR: "PR",
    colRepo: "Repo",
    colType: "Type",
    colReview: "Review",
    colCycle: "Cycle",
    colToProd: "to {{prod}}",
    colStatus: "Status",
    colAt: "Time",
    colApp: "Argo app",
    colStage: "Stage",
    colRevision: "Revision",
    colNewImages: "New images",
    colCountsAs: "Counts as",
    colPipeline: "Pipeline",
    colRun: "Run",
    colKind: "Kind",
    colCommit: "Commit",
    colDuration: "Duration",
    colImages: "Images",
    notOnProd: "No",
    bot: " · Bot",
    ...terms.en,
  },
  frameworks: {
    eyebrow: "Classification",
    title: "Which frameworks Riptide covers",
    lead: "Riptide measures from the tools' data what can be measured without a survey: two of the five DORA metrics, DX Core 4 in part. SPACE and DevEx also need surveys.",
    row: {
      freq: "Deployment frequency",
      lead: "Lead time to PROD",
      stages: "Flow by stage",
      dsucc: "Deploy success",
      review: "Review time, merged within 24 h",
      touch: "Approval without comment",
      mix: "Work mix, without a Jira key",
      build: "Build: success, duration, red runs",
      costs: "Cost of the AI reviews",
      missingCfr: "missing: Change fail rate, Deployment rework rate",
      missingRecovery: "missing: Failed deployment recovery time",
      missingSurvey: "missing: satisfaction, cognitive load, flow state",
    },
    cell: {
      dfThroughput: "Deployment frequency · Throughput",
      speedSecondary: "Speed, secondary",
      velocityApprox: "Flow velocity, approximated",
      cltThroughput: "Change lead time · Throughput",
      effWait: "Efficiency & flow (waiting time)",
      feedback: "Feedback loops",
      cltBreakdown: "Change lead time, broken down",
      effHandoffs: "Efficiency & flow (handoffs, waiting time)",
      flowEffApprox: "Flow efficiency, approximated",
      dsuccNot:
        "not a DORA metric: counts sync failures, not the change fail rate",
      diffsNot:
        "Speed measures “diffs per engineer”: Riptide deliberately does not, it compares people",
      feedbackReview: "Feedback loops (code review)",
      flowTimeApprox: "Flow time, approximated",
      collaboration: "Communication & collaboration (review quality)",
      impact: "Impact: proxy for “time on new work”, counts PRs, not time",
      distribution:
        "Flow distribution: Feature and Bugfix, without Debt and Risk",
      effectiveness: "Effectiveness: a driver, not the DXI itself",
      feedbackBuild: "Feedback loops (build)",
      instability: "Instability",
      qualityCore: "Quality, core metric",
      throughput: "Throughput",
      qualitySecondary: "Quality, secondary",
      dxiSurvey: "Effectiveness: DXI, survey only",
      satisfactionSurvey: "Satisfaction & well-being, survey only",
      surveyOnly: "survey only",
    },
    footnote:
      "Change fail rate, deployment rework rate and failed deployment recovery time need an incident source Riptide does not have. Since its 2025 report DORA no longer uses performance levels and calls comparing the same service over time the most meaningful comparison. So Riptide compares each team only with itself, never with survey values or other teams. DORA and DX Core 4 both stress: metrics apply to teams and applications, never to people.",
  },
};

export type Catalog = typeof en;
