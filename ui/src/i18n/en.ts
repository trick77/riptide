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
    build: "Build",
    deploys: "Deploys",
    arbeitsmix: "Work mix",
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
      "Team {{team}}, {{product}} · CW {{from}}–{{to}} · {{start}} to {{end}} · compared with the {{weeks}} weeks before, {{prevStart}} to {{prevEnd}}",
    scopeAlone:
      "Team {{team}}, {{product}} · CW {{from}}–{{to}} · {{start}} to {{end}}",
    running:
      "This week, CW {{kw}}, so far: {{releases}}, {{merged}}, {{runs}}. Counted in no number on this page; only the open PRs are as of now.",
    runningReleases_one: "{{count}} release",
    runningReleases_other: "{{count}} releases",
    runningMerged_one: "{{count}} merged PR",
    runningMerged_other: "{{count}} merged PRs",
    runningRuns_one: "{{count}} build",
    runningRuns_other: "{{count}} builds",
    runningView: "View →",
    oneProduct: "product {{product}}",
    allProductsLower: "all products",
    dataAsOf: "Data as of:",
    source: "{{source}} since {{since}}, latest {{at}}",
    sourceNone: "{{source}}: no data",
    repoStart: "{{repo}}: since {{date}}",
    repoNever: "{{repo}}: no data",
    lost: "{{source}} sent no data from {{from}} to {{to}} while builds ran: events were lost.",
    lostOngoing:
      "{{source}} has sent no data since {{from}} while builds run: events are being lost.",
    filter: "Filter",
    period: "Period",
    weeks: "{{n}} W",
    product: "Product",
    allProducts: "All products",
    repos_one: "{{count}} repo",
    repos_other: "{{count}} repos",
    reposHead: "Repos",
    appsHead: "Argo apps",
    none: "none",
    loadFailed: "Could not load the data: {{msg}}",
    noTeam: "No team is configured.",
    loading: "Loading",
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
    before: "before {{v}}",
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
    title: "Assessment",
    intro: "What the last {{weeks}} whole weeks show, and where most moves.",
    assess: {
      weeks: "CW {{from}}–{{to}}",
      ask: "↻ Assess again",
      askFirst: "Assess now",
      running: "running …",
      cooldown: "Can be asked again from {{time}}",
      madeAt: "Assessment of {{date}}",
      trigger: {
        scheduled: "scheduled",
        manual: "on request",
      },
      next: "next: {{date}}",
      older_one: "as of {{count}} week ago",
      older_other: "as of {{count}} weeks ago",
      runningNote:
        "The assessment of {{weeks}} is running, usually under a minute.",
      failed: "The last assessment failed: {{detail}}",
      askFailed: "Could not start the assessment: {{detail}}",
      outdated:
        "The latest assessment rests on older rules and is no longer shown. Below is what the current rules see.",
      outdatedAsk: " “Assess again” writes it anew.",
      limitsLabel: "What limits this assessment",
      limit: {
        starts: "{{source}} has only sent data since {{date}}.",
        silence:
          "{{source}} sent no data from {{from}} to {{to}} while builds ran: events were lost. The PR numbers stay without a verdict.",
        silenceProduct:
          "{{source}} sent no data from {{from}} to {{to}} while builds ran: events were lost. The PR numbers stay without a verdict, and with them deploys and builds: they reach the product through Bitbucket.",
        silenceOngoing:
          "{{source}} has sent no data since {{from}} while builds run: events are being lost. The PR numbers stay without a verdict.",
        silenceOngoingProduct:
          "{{source}} has sent no data since {{from}} while builds run: events are being lost. The PR numbers stay without a verdict, and with them deploys and builds: they reach the product through Bitbucket.",
        silent_end:
          "{{source}} has sent no data since {{from}}, up to the end of the period: its numbers are left out.",
      },
      reason: {
        no_baseline:
          "No full comparison with the previous period yet: not every number has a value there to compare with.",
        no_baseline_none:
          "No comparison possible yet: there is no comparable data for the previous period.",
        notMeasurable: {
          bitbucket: "Not measurable: Bitbucket sends no data for the team.",
          argocd: "Not measurable: Argo CD sends no data for the team.",
          noMerged:
            "Not measurable: no PR merged in these weeks counts as work.",
          noRelease: "Nothing went to {{prod}} in these weeks.",
          toProd_one:
            "Not measurable: {{count, num}} PR was merged in these weeks and could not be traced to {{prod}}.",
          toProd_other:
            "Not measurable: {{count, num}} PRs were merged in these weeks, none could be traced to {{prod}}.",
          toProdRolled_one:
            "Not measurable: {{count, num}} PR of a repo that is rolled out was merged in these weeks and could not be traced to {{prod}}.",
          toProdRolled_other:
            "Not measurable: of {{count, num}} merged PRs of repos that are rolled out, none could be traced to {{prod}}.",
        },
      },
      librariesOnly:
        "This product holds only libraries; they are not rolled out.",
      librariesOnlyTeam:
        "Every merged PR is in a library; libraries are not rolled out.",
      quiet: "Nothing stands out in these weeks.",
      source: {
        bitbucket: "Bitbucket",
        pipeline: "CI",
        argocd: "Argo CD",
        noergler: "noergler",
      },
      focus: "In focus",
      focusBy: "by weight of the findings",
      scopeAll: "Team, all products",
      scopeProduct: "Product {{product}}",
      col: {
        good: "Running well",
        bad: "Stuck",
        done: "Resolved",
        hint: "Hints",
        limit: "Cannot be judged",
      },
      colSub: {
        hint: "no finding, for your information",
        limit: "the data says nothing about it",
      },
      state: {
        // A span in a state's words: {{n}} is the backend's value.
        span: {
          minutes_one: "{{n}} minute",
          minutes_other: "{{n}} minutes",
          wd_one: "{{n}} working day",
          wd_other: "{{n}} working days",
          wdDat_one: "{{n}} working day",
          wdDat_other: "{{n}} working days",
          days_one: "{{n}} day",
          days_other: "{{n}} days",
          daysDat_one: "{{n}} day",
          daysDat_other: "{{n}} days",
        },
        unitWord: {
          project_one: "project",
          project_other: "projects",
          repo_one: "repo",
          repo_other: "repos",
          app_one: "App",
          app_other: "Apps",
          branch_one: "branch",
          branch_other: "branches",
          botBranch_one: "bot branch",
          botBranch_other: "bot branches",
        },
        of: {
          project_one: "{{k}} of {{n}} project",
          project_other: "{{k}} of {{n}} projects",
          repo_one: "{{k}} of {{n}} repo",
          repo_other: "{{k}} of {{n}} repos",
          app_one: "{{k}} of {{n}} App",
          app_other: "{{k}} of {{n}} Apps",
          branch_one: "{{k}} of {{n}} branch",
          branch_other: "{{k}} of {{n}} branches",
          botBranch_one: "{{k}} of {{n}} bot branch",
          botBranch_other: "{{k}} of {{n}} bot branches",
        },
        more_one: "show {{count}} more",
        more_other: "show {{count}} more",
        less: "show fewer",
        since: "since {{date}}",
        sinceAtLeast: "since {{date}} at the latest",
        asOf: "as of {{date}}",
        unjudged_one: "{{n}} {{unit}} with too little data",
        unjudged_other: "{{n}} {{unit}} with too little data",
        held: "(just under the limit, so still named)",
        twoSprints: "(for two sprints or longer)",
        folded:
          "{{label}}: {{value}}, notably worse than in the previous period",
        unitFacts: "See {{name}} in today's data",
        facts: "See today's “{{term}}” in the data →",
        no_release: {
          title: "No release to {{prod}} for long",
          unit_one:
            "last release {{n}} days ago ({{names}}), {{count}} PR merged since",
          unit_other:
            "last release {{n}} days ago ({{names}}), {{count}} PRs merged since",
          threshold_one:
            "Agreement: at least one release to {{prod}} per sprint. Reported when no release came for more than {{n}} day.",
          threshold_other:
            "Agreement: at least one release to {{prod}} per sprint. Reported when no release came for more than {{n}} days.",
          rule: "Counted are projects that are rolled out and worked on. An App's first known deploy counts as a release.",
        },
        release_due: {
          title: "Release to {{prod}} due soon",
          unit: "last release {{n}} days ago ({{names}})",
          threshold_one:
            "Agreement: at least one release to {{prod}} per sprint. A hint when no release came for more than {{n}} day.",
          threshold_other:
            "Agreement: at least one release to {{prod}} per sprint. A hint when no release came for more than {{n}} days.",
          rule: "Still within the agreement: a hint before it is broken.",
        },
        not_on_prod: {
          title: "Merged PRs wait long for {{prod}}",
          unit_one: "{{count}} merged PR not on {{prod}} yet, for {{n}} days",
          unit_other:
            "{{count}} merged PRs not on {{prod}} yet, the oldest for {{n}} days",
          threshold_one: "Agreement: at most {{n}} day from merge to {{prod}}.",
          threshold_other:
            "Agreement: at most {{n}} days from merge to {{prod}}.",
          rule: "Counted are PRs of repos that are rolled out, from their arrival on the main branch; libraries are not.",
        },
        slow_to_prod: {
          title: "Long wait from merge to {{prod}}",
          unit_one:
            "typically {{n}} days from merge to {{prod}}, from {{count}} PR",
          unit_other:
            "typically {{n}} days from merge to {{prod}}, from {{count}} PRs",
          threshold_one:
            "Agreement: at most {{n}} day from merge to {{prod}}. Judged from {{minN}}.",
          threshold_other:
            "Agreement: at most {{n}} days from merge to {{prod}}. Judged from {{minN}}.",
          min_one: "{{n}} PR",
          min_other: "{{n}} PRs",
          rule: "The typical value over the PRs merged in the period that reached {{prod}} by its end.",
        },
        merge_pause: {
          title: "Hardly merged while approved PRs waited",
          unit_one:
            "week {{week}}: {{count}} PR merged instead of typically {{typical}}, while {{n}} approved PRs were open at the week's end ({{names}})",
          unit_other:
            "week {{week}}: {{count}} PRs merged instead of typically {{typical}}, while {{n}} approved PRs were open at the week's end ({{names}})",
          unitLost_one:
            "week {{week}}: {{count}} PR merged instead of typically {{typical}} (leaving out the days Bitbucket delivered no data), while {{n}} approved PRs were open at the week's end ({{names}})",
          unitLost_other:
            "week {{week}}: {{count}} PRs merged instead of typically {{typical}} (leaving out the days Bitbucket delivered no data), while {{n}} approved PRs were open at the week's end ({{names}})",
          threshold:
            "Reported when a week has at most {{n}} of the typical merges while {{minN}} or more were open at the week's end.",
          min_one: "{{n}} approved PR",
          min_other: "{{n}} approved PRs",
          rule: "Against the project's typical week. Approved means a person other than the author approved it, the approval still stands at the week's end, and nobody asks for changes. Why nothing was merged is not in the data.",
        },
        approved_waiting: {
          title: "Approved, but not merged",
          unit_one:
            "{{count}} approved PR waits for its merge, for {{n}} working days",
          unit_other:
            "{{count}} approved PRs wait for their merge, the oldest for {{n}} working days",
          threshold_one:
            "Reported from {{minN}} per repo waiting longer than {{n}} working day.",
          threshold_other:
            "Reported from {{minN}} per repo waiting longer than {{n}} working days.",
          min_one: "{{n}} PR",
          min_other: "{{n}} PRs",
          rule: "Approved means a person other than the author approved the PR, and the approval still stands. A PR someone asks changes for does not wait for its merge.",
        },
        bot_loop: {
          title: "Bot branch burning CI time on failed builds",
          unit: "{{n}} failed builds in a row, {{ci}} of CI time",
          threshold_one: "Reported from {{n}} failed build in a row.",
          threshold_other: "Reported from {{n}} failed builds in a row.",
          rule: "CI time used up, no finding about the team's code.",
        },
        untraced: {
          title: "Not measurable whether merged PRs are on {{prod}}",
          unit_one: "{{n}} of {{of}} merged PRs not placeable: {{reason}}",
          unit_other:
            "{{n}} of {{of}} merged PRs not placeable, mostly: {{reason}}",
          rule: "For these PRs riptide cannot tell whether and when they reached {{prod}}. The agreement on the time to {{prod}} is not measurable there.",
        },
        unattributed: {
          title: "Releases to {{prod}} without a project",
          unit_one: "{{n}} release to {{prod}} belongs to no project",
          unit_other: "{{n}} releases to {{prod}} belong to no project",
          rule: "Their GitOps commit is unknown to Bitbucket. For their projects the agreement of one release per sprint is not measurable.",
        },
        no_prod_stage: {
          title: "Argo CD reports nothing to {{prod}}",
          unit: "Argo CD reports deploys to {{names}}, none to {{prod}}",
          rule: "Check the stage configuration or Argo CD's notification.",
        },
        deploy_failing: {
          title: "Last deploy to {{prod}} failed",
          unit_one: "no successful deploy for {{daysDat}}, {{count}} attempt",
          unit_other:
            "no successful deploy for {{daysDat}}, {{count}} attempts",
          threshold_one:
            "Reported from {{n}} working day without a successful deploy.",
          threshold_other:
            "Reported from {{n}} working days without a successful deploy.",
          rule: "The App's last deploy to {{prod}} failed, and none succeeded since.",
        },
        deploy_failures: {
          title: "Deploys to {{prod}} fail again and again",
          unit: "{{n}} times deploys failed in a row, each time until one succeeded again; {{count}} deploys in the period",
          threshold: "Reported from {{n}} times in the period.",
          rule: "Several failed tries in a row count once.",
        },
        first_review_wait: {
          title: "PRs wait for a first review",
          unitOldest_one:
            "{{count}} PR waits for a first review, for {{n}} working days",
          unitOldest_other:
            "{{count}} PRs wait for a first review, the oldest for {{n}} working days",
          unitTypical_one:
            "first review typically after {{n}} working days, from {{count}} PR",
          unitTypical_other:
            "first review typically after {{n}} working days, from {{count}} PRs",
          threshold_one:
            "Reported from {{minN}} waiting longer than {{n}} working day, or when the first review typically takes that long.",
          threshold_other:
            "Reported from {{minN}} waiting longer than {{n}} working days, or when the first review typically takes that long.",
          min_one: "{{n}} PR",
          min_other: "{{n}} PRs",
          rule: "PRs that are no drafts any more and have no reaction from a person yet. Bots' PRs not counted.",
        },
        trunk_failing: {
          title: "Builds on the main branch failing for long",
          unit_one: "failed for {{days}}, {{count}} failed build",
          unit_other: "failed for {{days}}, {{count}} failed builds",
          threshold_one:
            "Reported from {{minN}} over more than {{n}} working day.",
          threshold_other:
            "Reported from {{minN}} over more than {{n}} working days.",
          min_one: "{{n}} failed build",
          min_other: "{{n}} failed builds",
          rule: "From the first failed build to the next successful one, per main branch. Release builds not counted.",
        },
        branch_failing: {
          title: "A branch's builds fail again and again",
          unit: "{{n}} failed builds in a row",
          threshold_one:
            "Reported from {{n}} failed build in a row over at least {{within}}.",
          threshold_other:
            "Reported from {{n}} failed builds in a row over at least {{within}}.",
          rule: "A person's feature or bugfix branch. Bots' branches not counted.",
        },
        flaky_builds: {
          title: "Unreliable builds",
          unit: "{{kind}}: {{n}} of {{of}} failed builds passed on the next try without a change",
          threshold:
            "Reported from {{minN}} when such make up at least {{n}} of all failed builds.",
          min_one: "{{n}} such build",
          min_other: "{{n}} such builds",
          rule: "Failed, then successful on the same commit. A lower bound: retries inside a build riptide does not see.",
        },
        long_builds: {
          title: "Builds take long",
          unit_one:
            "{{kind}}: typically {{n}} minutes, from {{count}} successful build",
          unit_other:
            "{{kind}}: typically {{n}} minutes, from {{count}} successful builds",
          threshold_one:
            "Reported when a successful build typically takes longer than {{n}} minute, from at least {{minN}}.",
          threshold_other:
            "Reported when a successful build typically takes longer than {{n}} minutes, from at least {{minN}}.",
          min_one: "{{n}} build",
          min_other: "{{n}} builds",
          rule: "The typical duration of successful builds, per repo and build kind.",
        },
        release_failing: {
          title: "Release builds fail",
          unit: "{{count}} of {{of}} release builds failed ({{n}})",
          threshold:
            "Reported from {{minN}} when failed ones make up at least {{n}} of all release builds.",
          min_one: "{{n}} failed release build",
          min_other: "{{n}} failed release builds",
          rule: "Failed release builds, per repo.",
        },
        builds_silent: {
          title: "CI reports no builds for a repo any more",
          unit_one:
            "{{n}} PRs merged, no build reported; in the {{withinDat}} before, {{count}} build",
          unit_other:
            "{{n}} PRs merged, no build reported; in the {{withinDat}} before, {{count}} builds",
          threshold_one:
            "Reported from {{minN}} in the last {{n}} working day without a reported build.",
          threshold_other:
            "Reported from {{minN}} in the last {{n}} working days without a reported build.",
          min_one: "{{n}} merged PR",
          min_other: "{{n}} merged PRs",
          rule: "Whether it builds the data does not show: maybe the CI only stopped reporting.",
        },
        big_prs: {
          title: "Many big PRs",
          unit: "{{count}} of {{of}} PRs change more than {{lines}} lines ({{n}})",
          threshold:
            "Reported when at least {{n}} of the PRs change more than {{minN}}.",
          min_one: "{{n}} line",
          min_other: "{{n}} lines",
          rule: "Share of the merged PRs with a known size.",
        },
        fast_approval: {
          title: "Big PRs approved fast",
          unit_one:
            "{{n}} big PR approved within {{within}}, without a comment before",
          unit_other:
            "{{n}} big PRs approved within {{within}}, without a comment before",
          threshold_one:
            "Reported from {{n}} PR over {{minN}} approved within {{within}}.",
          threshold_other:
            "Reported from {{n}} PRs over {{minN}} approved within {{within}}.",
          min_one: "{{n}} line",
          min_other: "{{n}} lines",
          rule: "Without a person's comment before. The data shows the time, not how they were reviewed.",
        },
        unapproved: {
          title: "Merged without approval",
          unit: "{{count}} of {{of}} merged PRs without a person's approval ({{n}})",
          threshold:
            "Reported from {{minN}} per repo when at least {{n}} lack an approval.",
          min_one: "{{n}} merged PR",
          min_other: "{{n}} merged PRs",
          rule: "Housekeeping PRs and bots' PRs not counted.",
        },
        late_decline: {
          title: "PRs declined after long work",
          unit_one:
            "{{n}} PR declined after more than {{withinDat}} ({{names}})",
          unit_other:
            "{{n}} PRs declined after more than {{withinDat}} ({{names}})",
          threshold_one: "Reported from {{n}} such PR in the team.",
          threshold_other: "Reported from {{n}} such PRs in the team.",
          rule: "Work that did not land. Not counted when a later PR from the same branch carries it on.",
        },
        branch_life: {
          title: "Branches live long before the PR is ready",
          unit_one:
            "typically {{n}} working days from the first commit until the PR is ready, from {{count}} PR",
          unit_other:
            "typically {{n}} working days from the first commit until the PR is ready, from {{count}} PRs",
          threshold_one:
            "Reported from typically more than {{n}} working day, from at least {{minN}}.",
          threshold_other:
            "Reported from typically more than {{n}} working days, from at least {{minN}}.",
          min_one: "{{n}} PR",
          min_other: "{{n}} PRs",
          rule: "A lower bound: a rebase resets the first commit.",
        },
        branch_open: {
          title: "Branches without a PR, worked on for long",
          unit_one:
            "{{count}} branch without a PR worked on, for {{n}} days ({{names}})",
          unit_other:
            "{{count}} branches without a PR worked on, the oldest for {{n}} days ({{names}})",
          threshold_one:
            "Reported from one branch without a PR worked on for more than {{n}} day, last pushed at most {{withinDat}} ago.",
          threshold_other:
            "Reported from one branch without a PR worked on for more than {{n}} days, last pushed at most {{withinDat}} ago.",
          rule: "Branches that PRs target and bots' branches not counted. A branch silent as long as “Branches without a PR, silent for long” reports counts its work anew from the next push.",
        },
        branch_silent: {
          title: "Branches without a PR, silent for long",
          unit_one:
            "{{count}} branch without a PR not pushed any more ({{names}})",
          unit_other:
            "{{count}} branches without a PR not pushed any more ({{names}})",
          threshold_one:
            "Reported from {{minN}} per repo, not pushed for more than {{n}} day.",
          threshold_other:
            "Reported from {{minN}} per repo, not pushed for more than {{n}} days.",
          min_one: "{{n}} branch without a PR",
          min_other: "{{n}} branches without a PR",
          rule: "Whether they still exist Bitbucket does not report.",
        },
        unmeasurable: {
          title: "{{was}}: no longer measurable",
          unit: "",
          rule: "A week ago this still held here; now the data does not suffice to judge it. That does not mean it is solved.",
        },
      },
      tallyLabel: "Tally",
      sparkAria: "Per week, previous period: {{prev}}; now: {{cur}}",
      tally: {
        good: "running well",
        done: "resolved",
        bad: "stuck",
        hint: "hints",
        limit: "cannot be judged",
        talk: "to talk about",
        ofRepos: "of {{repos}}",
        calm_one: "active repo without a finding",
        calm_other: "active repos without a finding",
        unjudged_one: "repo with too little data",
        unjudged_other: "repos with too little data",
      },
      calmLine_one: "Of {{count}} active repo: {{parts}}.",
      calmLine_other: "Of {{count}} active repos: {{parts}}.",
      calmPart: "{{count}} without a finding",
      unjudgedPart: "{{count}} with too little data for a verdict",
      idleLine_one: "{{n}} repo idle that was active in the previous period.",
      idleLine_other:
        "{{n}} repos idle that were active in the previous period.",
      noGain: "No gain that counts as evidence.",
      stuckBelow: "No card of its own: the findings are further down.",
      waitingAll_one: "{{count}} waits for {{what}}",
      waitingAll_other: "all {{count}} wait for {{what}}",
      waitingOn: "waits for {{what}}: {{count}}",
      allDraft_one: "{{count}} is a draft",
      allDraft_other: "all {{count}} are drafts",
      seen_one: "seen {{count}} time before",
      seen_other: "seen {{count}} times before",
      kind: {
        data_gap: "data gap",
        pr_jam: "PR jam",
        slower_build: "got slower",
        failing_more: "fails more often",
        change: "notable change",
        agent_wait: "agent wait",
        resolved: "resolved",
        question: "question",
        state: "finding",
      },
      title: {
        data_gap: "{{source}} silent",
        pr_jam: "{{repo}}: PRs pile up",
        slower_build: "{{repo}}: {{kind}} slower than last period",
        failing_more: "{{repo}}: {{kind}} fail more often",
        changeGood: "{{label}}: notably better",
        changeBad: "{{label}}: notably worse",
        agent_wait: "{{kind}} wait for build agents",
        resolved: "{{what}}: resolved",
        question: "{{label}}",
      },
      rule: {
        data_gap:
          "The source stopped reporting before the period's end; nothing about it says anything about the work.",
        pr_jam:
          "Four or more PRs open a week or longer, drafts not counted. Why they wait is not in the data: a freeze, a dependency, holidays or nobody feeling responsible.",
        slower_build:
          "The typical succeeded build takes one and a half times as long as last period or longer: slower than the repo itself, not than others. What inside the build got slower is not in the data.",
        failing_more:
          "Fails far more often than in the previous period. Whether tests, infrastructure or real breakage is not in the data.",
        changeGood:
          "Moved past the notable threshold against the previous period.",
        changeBad:
          "Moved past the notable threshold against the previous period. Why is not in the data.",
        agent_wait:
          "{{share}} of the typical build time or more, and at least {{minutes}} minutes, is waiting for an agent: the platform's capacity, not the build.",
        resolved:
          "The latest assessment raised it; now the value is back where it was before, or better.",
        resolvedState:
          "A week ago this still held here. Not any more: clearly below the limit again, or over, like a merge pause or failed builds a card may still show with their date.",
      },
      factsOf: "View the facts of {{repo}} →",
      driver: "Driver: {{repo}} carries {{share}} of the rise",
      retro: "Retro",
      retroBox: "For the next retro",
      copy: "Copy as Markdown",
      copied: "copied",
      questionFixed: {
        feature_share: "Is the share of feature work where we want it?",
        untracked: "PRs without a Jira key: on purpose or forgotten?",
        wasted_share: "Why do we review PRs we then drop?",
        cost_per_kloc: "Is the review spend worth what it finds?",
        open_prs: "More work under way, or more left lying?",
        first_touch: "Does that fit how we review?",
      },
      also: "Also spotted:",
      weeksUnit: "{{n}} weeks",
      lever: {
        walk_prs:
          "walk through the PRs one by one: review, merge, close or park them on purpose",
        ask_why_waiting:
          "find out what they wait on: a freeze, a dependency, or nobody feeling responsible",
        inspect_build_log:
          "look in the build log of the slowest builds which step takes the time",
        inspect_failures:
          "look at the failed builds: the same failure each time, or a different one",
        platform_agents: "raise the agent capacity with the platform team",
        find_what_changed:
          "first find out what changed, then agree on something",
        name_what_helped: "name what helped, so it stays",
        check_source: "check the source's connection",
        plan_release:
          "plan the next release to {{prod}}, or name what holds it",
        inspect_deploys:
          "look at the failed deploys in Argo CD: the same error each time, or a different one?",
        stop_bot_loop:
          "look at the bot branch: fix what its builds trip over, or close it",
        walk_branches:
          "go through the branches: delete the finished ones, open a PR or park the rest on purpose",
      },
      label: {
        silent_days: "silent",
        open_long: "PRs open ≥ 7 days",
        open: "open PRs",
        age_p50: "typical age",
        failure_rate: "builds failed",
        wasted: "CI time in failed builds",
        runs: "builds",
        build_p50: "typical succeeded build",
        build_wait_p50: "waiting for an agent",
        build_work_p50: "building",
        deploy_per_week: "releases per week",
        deploy_success: "deploys succeeded",
        lead_time_p50: "lead time",
        review_p50: "ready to approval",
        within_day: "merged within a day",
        jammed_repos: "jammed repos",
        build_success: "builds succeeded",
        merged_per_week: "PRs merged per week",
        batch_size: "changed lines per PR",
        wip_weeks: "of work standing open",
        feature_share: "feature PRs",
        untracked: "PRs without a Jira key",
        wasted_share: "review spend on dropped PRs",
        cost_per_kloc: "review spend per 1000 lines",
        open_prs: "open PRs",
        first_touch: "approvals without a prior comment",
        state_units: "named",
        state_judged: "judged",
        state_threshold: "the team's threshold",
        state_no_release: "days since the last release",
        state_release_due: "days since the last release",
        state_not_on_prod: "days the oldest PR waits",
        state_slow_to_prod: "typical days to {{prod}}",
        state_merge_pause: "approved PRs open at the week's end",
        state_approved_waiting: "working days the oldest PR waits",
        state_bot_loop: "failed builds in a row",
        state_untraced: "PRs not placeable",
        state_unattributed: "releases without a project",
        state_no_prod_stage: "stages reported",
        state_deploy_failing: "working days without a successful deploy",
        state_deploy_failures: "times deploys failed in a row",
        state_first_review_wait: "working days without a first review",
        state_trunk_failing: "working days failing",
        state_branch_failing: "failed builds in a row",
        state_flaky_builds: "flaky builds",
        state_long_builds: "minutes per successful build",
        state_release_failing: "release builds failed",
        state_builds_silent: "PRs merged without a build reported",
        state_big_prs: "PRs above the line threshold",
        state_fast_approval: "big PRs approved fast",
        state_unapproved: "PRs without approval",
        state_late_decline: "PRs declined after long work",
        state_branch_life: "working days until the PR is ready",
        state_branch_open: "days without a PR",
        state_branch_silent: "branches without a push",
      },
      waiting: {
        draft: "draft",
        first_review: "first review",
        review: "review",
        changes: "changes",
        merge: "merge",
      },
    },
    sinceNote:
      "Argo CD only reports since {{date}}: the weeks before are empty, the rate applies from then on.",
    sinceLow: " Less than a week of data: not very reliable.",
    throughput: "Throughput and instability",
    freq: {
      label: "Deployment frequency",
      dora: "DORA · Deployment frequency · Throughput",
      question: "How often does new code go to {{prod}}?",
      unit: "releases per week, on average",
      note_one: "{{count}} release in the period",
      note_other: "{{count}} releases in the period",
      noDeploys: "no deploys in the period",
      notReporting: "Argo CD does not report for the team yet",
      direction: "more often is better",
      directionWhy: "smaller releases, less risk per release",
      firstSeen_one:
        "{{count, num}} first deploy in the first {{days}} days of Argo CD data: whether it brought a new version is unknown; not counted",
      firstSeen_other:
        "{{count, num}} first deploys in the first {{days}} days of Argo CD data: whether they brought a new version is unknown; not counted",
      unattributed_one: "{{count, num}} deploy not attributable to a product",
      unattributed_other:
        "{{count, num}} deploys not attributable to a product",
      infra_one: "{{count, num}} deploy of infrastructure apps not counted",
      infra_other: "{{count, num}} deploys of infrastructure apps not counted",
      means:
        "How often new code reaches the users. Frequent small releases carry little risk per release and bring feedback early.",
      watch:
        "the weeks swing widely or releases stop: then changes pile up into big releases.",
      lever:
        "smaller changes, automated deploys, no fixed release date for PROD.",
      def: "Successful deploys to {{prod}} with a new version: the version differs from the previous deploy of the same Argo app, or it is the first deploy of an app going live. Counted once per GitOps revision, divided by the weeks in the period in which Argo CD reported. Deploys of infrastructure apps, whose deploys never belonged to a product of the team, do not count.",
      basis:
        "{{releases}}, {{resyncs}} and {{firstSeen}} from {{syncs}}. A release that updates several apps through the same GitOps revision counts once.",
      basisFirst_one: " Of them, {{count}} release as an app's first deploy.",
      basisFirst_other:
        " Of them, {{count}} releases as an app's first deploy.",
      filterImages: "images ≠ previous deploy",
      filterRevision: "per revision",
      coverageFirst:
        "In the first {{days}} days of Argo CD data an app's first deploy has no predecessor: whether it brought a new version is unknown, so it does not count as a release. After that an app's first deploy counts as a release: the app goes live.",
      coverageAll:
        "Every deploy is comparable: an app's first deploy after the first {{days}} days of Argo CD data counts as a release.",
      coverageInfra_one:
        " Not counted: {{count, num}} deploy of infrastructure apps that belong to no product of the team ({{apps}}).",
      coverageInfra_other:
        " Not counted: {{count, num}} deploys of infrastructure apps that belong to no product of the team ({{apps}}).",
      coverageUnattributed_one:
        " {{count, num}} deploy belongs to no product: neither its GitOps commit nor another deploy of its app names one. It counts in the team view, in no product view.",
      coverageUnattributed_other:
        " {{count, num}} deploys belong to no product: neither their GitOps commit nor another deploy of their app names one. They count in the team view, in no product view.",
    },
    lead: {
      label: "Lead time to {{prod}}",
      dora: "DORA · Change lead time · Throughput",
      question: "How long does a PR take from its first commit to {{prod}}?",
      noHuman: "no merged PR traced to {{prod}}",
      slowest_one: "the slowest 10 %: over {{p90}} · from {{count}} PR",
      slowest_other: "the slowest 10 %: over {{p90}} · from {{count}} PRs",
      directionWhy: "faster feedback from real users",
      lowSample: "Fewer than 10 PRs: not very reliable",
      means:
        "The time in which a finished change helps no user yet. Long means: feedback comes late, failures are hard to pin to a change, releases grow big.",
      watch:
        "the slowest 10 % sit far above the typical value: then single changes get stuck, often behind a release date.",
      lever:
        "Where the time goes is shown by the flow below; that is where the lever is, not in the short stages.",
      def: "Per PR, from its first commit to the first deploy to {{prod}} that contains it. The PRs merged in the period that were on {{prod}} by its end. Bots' PRs and libraries' PRs do not count.",
      basis_one:
        "From {{count}} PR, merged in the period and on {{prod}} by its end.",
      basis_other:
        "From {{count}} PRs, merged in the period and on {{prod}} by its end.",
      filterBots: "no bots",
      coverage_one:
        "{{traced, num}} of {{count, num}} merged PR traced to {{prod}}: through the compare link in the release commit or the version a build built. A release with neither is missing here.",
      coverage_other:
        "{{traced, num}} of {{count, num}} merged PRs traced to {{prod}}: through the compare link in the release commit or the version a build built. A release with neither is missing here.",
      traced_one:
        "{{traced, num}} of {{count, num}} merged PR traced to {{prod}}",
      traced_other:
        "{{traced, num}} of {{count, num}} merged PRs traced to {{prod}}",
      notYet_one: "{{count, num}} not on {{prod}} yet",
      notYet_other: "{{count, num}} not on {{prod}} yet",
      unmeasured_one:
        "{{count, num}} of them with no known start, not counted in the time",
      unmeasured_other:
        "{{count, num}} of them with no known start, not counted in the time",
    },
    dsucc: {
      label: "Deploy success",
      dora: "not a DORA metric",
      question: "Does the rollout itself work?",
      unit: "of deploys",
      note_one: "{{failed}} of {{count}} deploy failed",
      note_other: "{{failed}} of {{count}} deploys failed",
      direction: "higher is better",
      means:
        "Whether the rollout itself works: an aborted or failed deploy means someone has to step in.",
      watch:
        "the value drops: then deploys fail on configuration, resources or dependencies.",
      lever:
        "look at the failed deploys under “Data”; it is mostly the same app or the same cause.",
      def: "Share of deploys to {{prod}} with operation_phase Succeeded among all finished deploys; Failed and Error count as failed. Not a DORA metric: the change fail rate counts deploys after which someone has to step in, even when the deploy succeeded.",
      basis:
        "{{finished}} finished deploys, {{failed}} of them failed (Failed or Error).",
      coverage:
        "Only deploys Argo CD reported; deploys of infrastructure apps do not count.",
    },
    instability: {
      label: "Instability",
      dora: "DORA · Change fail rate · Deployment rework rate · Failed deployment recovery time",
      question:
        "How often does a deployment cause trouble, and how fast is it fixed?",
      value: "not measured",
      text: "Needs an incident source, which is missing.",
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
      toProdPre: "From {{pre}} to {{prod}}",
      toProd: "From merge to {{prod}}",
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
      "Per merged PR in repos that are rolled out, typical duration per stage; the lead time above counts the same PRs to {{prod}}.",
    leadTo: "Lead time to {{env}}:",
    typicalSlowest: "typical value · the slowest 10 %: over {{p90}}",
    shareAria: "Share of the time to {{prod}}: {{parts}}",
    ofTime: "· {{share}} of the time",
    hot: "The lever is at “{{name}}”",
    hotRest: ": {{share}} of the time to {{prod}}.",
    noProd:
      "No change traced to {{prod}} in this period: shares exist only over the whole way.",
    chainNote_one:
      "Only PRs mapped to a deploy reach the last two stages: {{traced, num}} of {{count, num}} merged PR traced to {{prod}}.",
    chainNote_other:
      "Only PRs mapped to a deploy reach the last two stages: {{traced, num}} of {{count, num}} merged PRs traced to {{prod}}.",
    notYet_one:
      "{{count, num}} merged PR not on {{prod}} yet, merged on {{date}}.",
    notYet_other:
      "{{count, num}} merged PRs not on {{prod}} yet, the oldest merged on {{date}}.",
    notPlaceable_one: "{{count, num}} merged PR not placeable: {{reasons}}.",
    notPlaceable_other: "{{count, num}} merged PRs not placeable: {{reasons}}.",
    reasonPart: "{{reason}} ({{count, num}})",
    lowerBound_one:
      "{{count, num}} traced PR may have reached {{prod}} with an earlier deploy that cannot be tied to a PR: its time to {{prod}} is an upper bound.",
    lowerBound_other:
      "{{count, num}} traced PRs may have reached {{prod}} with an earlier deploy that cannot be tied to a PR: their time to {{prod}} is an upper bound.",
    library_one:
      "{{count, num}} PR in libraries, never rolled out: in no count to {{prod}}.",
    library_other:
      "{{count, num}} PRs in libraries, never rolled out: in no count to {{prod}}.",
    perRepo_one: "Per repo ({{count}} repo)",
    perRepo_other: "Per repo ({{count}} repos)",
    colRepo: "Repo",
    colMerged: "Merged",
    colOnProd: "On {{prod}}",
    colNotYet: "Not on {{prod}} yet",
    colNotPlaceable: "Not placeable",
    colLibrary: "Library",
    oldestSince: "oldest merged on {{date}}",
    ridge: {
      title: "How the time spreads within each stage",
      sub: "The same PRs as above, time on a log scale. Line: typical value, dashed: from there the slowest 10 %.",
      caption: "Wait per stage",
      colStage: "Stage",
      few: "under {{min}} PRs: single PRs instead of a curve",
      none: "no PRs",
      tipN: "PRs",
      tipTypical: "typical value",
      tipSlowest: "the slowest 10 %: over",
      tick: {
        min: "{{n}} min",
        h: "{{n}} h",
        d: "{{n}} d",
        w: "{{n}} wk",
      },
      note: "The far left also counts shorter waits, the far right longer ones.",
    },
    people: "People",
    eyebrow: "From commit to {{prod}} · app repos only",
    title: "Where does a change wait?",
    lead: "The time to the user, split into stages.",
    fwLeadTime: "Change lead time, broken down",
    reviewTitle: "Review: fast, but how thorough?",
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
        "PRs opened before the data began have no pr:opened and are missing. Seen opened: {{seen, num}} of {{total, num}} merged.",
    },
    histHead: "PRs by time from opening to merge",
    histLine: " · the line marks 24 h",
    histAria: "PR cycle time: {{parts}}",
  },
  throughput: {
    head: "Flow: how much gets done, in what portions",
    cardTitle: "PR throughput and batch size per week",
    scope: "People's PRs only: no stage promotions, no bots.",
    merged: {
      label: "PR throughput",
      framework: "Flow · Flow velocity",
      question: "How many PRs does the team merge per week?",
      unit: "PRs per week",
      note_one: "{{count, num}} PR merged in the period",
      note_other: "{{count, num}} PRs merged in the period",
      housekeeping_one: "{{count, num}} housekeeping PR not counted",
      housekeeping_other: "{{count, num}} housekeeping PRs not counted",
      housekeepingTitle:
        "The same title opened in {{repos}} or more repos within {{minutes}} minutes, without a person's approval and without a ticket key: upkeep across all repos with a tool, no work on the product.",
      umbrella_one: "{{count, num}} umbrella-branch merge not counted",
      umbrella_other: "{{count, num}} umbrella-branch merges not counted",
      umbrellaTitle:
        "The PR only carried other PRs' work to the main branch: they were merged into its branch or along with it. Those PRs count, it does not.",
      twice_one: "{{count, num}} PR counted twice, left out",
      twice_other: "{{count, num}} PRs counted twice, left out",
      twiceTitle:
        "The same branch already went into an umbrella branch with another PR and counts there. When the umbrella branch was merged, this PR was marked merged too: the same work, counted once.",
      since:
        "Bitbucket has only sent data since {{date}}: the weeks before are empty, the rate applies from then on.",
      direction: "more is better",
      chart: "Merged PRs per week, by branch prefix",
    },
    batch: {
      label: "Batch size",
      framework: "DORA capability · Working in small batches",
      question: "How large is a typical PR?",
      unit: "changed lines per PR, typical",
      note: "largest 10 % from {{p90}} lines · typically {{files}} files · size known for {{n}} of {{total}}",
      none: "No PR sizes in the period.",
      direction: "smaller is better",
      chart: "Typical PR size per week, in changed lines",
      series: "Changed lines, typical",
    },
  },
  open: {
    title: "Open PRs: where do they pile up?",
    sub: "As of {{at}}, whenever opened. A PR open {{days}} days or more counts as long open; {{min}} long-open ones in the same repo are a jam; a draft never, it may wait. GitOps repos and bots left out.",
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
    unknown_one:
      "{{count, num}} PR in an unknown state: last seen before an outage in which Bitbucket sent no data; whether it was merged or closed since is unknown. In no number above.",
    unknown_other:
      "{{count, num}} PRs in an unknown state: last seen before an outage in which Bitbucket sent no data; whether they were merged or closed since is unknown. In no number above.",
    viewInactive: "View →",
    jam: "Jam",
    colRepo: "Repo",
    colOpen: "Open",
    colLong: "≥ {{days}} d",
    colMedian: "Median age",
    colWaiting: "Waiting on",
    colInactive: "Inactive",
    viewPRs: "View PRs →",
    none: "No PR is open right now.",
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
      def: "PRs open now, those opened this week included; compared with those open at the previous period's end: opened before, neither merged, declined nor deleted. Age from its last turn ready for review, else from its creation: time as a draft does not count, a draft has no age. Long open from {{days}} days, a jam from {{min}} long-open ones in the same repo. A draft is never long open and never forgotten: it waits on its author. Per repo the median age, never the oldest: one alone decides that.",
      basis_one: "{{count}} open PR, as of {{at}}.",
      basis_other: "{{count}} open PRs, as of {{at}}.",
      coverage:
        "A PR whose merge or closing Bitbucket never reported stays open until, after {{inactive}} days without an event, it counts as inactive; alone it never makes a jam. If its last event came before an outage in which Bitbucket sent no data while builds ran, and none since, its state is unknown: it does not count as open. A PR without any event since data began is missing.",
    },
  },
  deploys: {
    eyebrow: "Rollout",
    title: "How evenly does code reach {{prod}}?",
    fwSpeed: "Speed, secondary",
    cardTitle: "Releases to {{prod}} per week",
    cardSub:
      "Only releases, deploys with a new version; the line is the team's typical week",
    release: "Release",
    releases: "Releases",
    typical: "typical week: {{n}}",
    aria: "Releases to {{prod}} per week: {{parts}}",
    note: "{{resyncs}} do not count. {{firstSeen}} in the first {{days}} days of Argo CD data do not count either: whether they brought a new version is unknown. The typical week: the middle value of the whole weeks in which Argo CD reported.",
  },
  calendar: {
    cardTitle: "Releases to {{prod}} per day",
    cardSub:
      "Each release on the day of its first deployment; on the right, each weekday's releases per whole day with data",
    none: "no release",
    four: "4 and more",
    noData: "no data",
    weekdayCell_one: "{{perDay}}: {{releases}} on {{count}} day",
    weekdayCell_other: "{{perDay}}: {{releases}} on {{count}} days",
    weekdayTitle_one:
      "{{day}}: {{releases}} releases on {{count}} whole day with data",
    weekdayTitle_other:
      "{{day}}: {{releases}} releases on {{count}} whole days with data",
    noDataDays_one: "{{count}} day without data",
    noDataDays_other: "{{count}} days without data",
    perDay: "per whole day with data",
    tableCaption: "Releases to {{prod}} per day; {{noData}}",
    week: "Week",
    days: {
      mon: "Mon",
      tue: "Tue",
      wed: "Wed",
      thu: "Thu",
      fri: "Fri",
      sat: "Sat",
      sun: "Sun",
    },
  },
  mix: {
    feature: "Feature",
    bugfix: "Bugfix",
    hotfix: "Hotfix",
    other: "Other / no prefix",
    eyebrow: "Work mix",
    title: "What is the team working on?",
    lead: "By branch prefix, as a share of PRs, not of time.",
    fwImpact: "Impact, proxy for “time on new work”",
    cardTitle: "Merged PRs by type",
    cardSub_one: "{{count}} PR in the period, without bots",
    cardSub_other: "{{count}} PRs in the period, without bots",
    aria: "Work mix: {{parts}}",
    barTitle_one: "{{label}}: {{count}} PR, {{share}}",
    barTitle_other: "{{label}}: {{count}} PRs, {{share}}",
    untracked: "{{rate}} without a Jira key",
    untrackedText: "({{n}} of {{total}}): key forgotten, or unplanned work?",
  },
  heat: {
    title: "Failure rate per repo and week",
    sub: "A red row means: this repo. A red column means: everyone at once, more likely the infrastructure.",
    aria: "Failure rate per repo and week, {{kind}}",
    failed_one: "{{failed}} of {{count}} build failed",
    failed_other: "{{failed}} of {{count}} builds failed",
    noVerdict: "under {{min}} builds: no verdict",
    none: "no builds",
    zero: "0 %",
    below: "under {{v}}",
    from: "{{v}} or more",
    other: "other repos",
    colRepo: "Repo",
  },
  build: {
    // Searched by the backend too: terms.json.
    kind: terms.en.runKind,
    kindNote: {
      release: "build_type release or hotfix",
      main: "master or main, no release",
      feature:
        "feature/, feat/, bugfix/, fix/, hotfix/, chore/, refactor/ and PR builds",
      other:
        "docs/ and doc/ (often without a build), release/… without a release, nightlies, other naming schemes",
      unknown: "before the first build_type report",
    },
    prefixes: "Most frequent prefixes",
    noBranch: "no branch in the pipeline name",
    featureMissingTitle: "Feature and bugfix builds: no build recognised",
    featureMissing_one:
      "Recognised are branches starting feature/, feat/, bugfix/, fix/, hotfix/, chore/, refactor/ and PR builds. {{count}} build on a branch without a known prefix is under “Other branches” (release/, docs/, doc/, nightlies and bots not counted), likely a feature build of the team.",
    featureMissing_other:
      "Recognised are branches starting feature/, feat/, bugfix/, fix/, hotfix/, chore/, refactor/ and PR builds. {{count}} builds on branches without a known prefix are under “Other branches” (release/, docs/, doc/, nightlies and bots not counted), likely the team's feature builds.",
    sparkAria: "Success rate per week, {{kind}}: {{parts}}",
    successful: "successful",
    typicalDuration: "typical successful build, all repos",
    repos:
      "Of {{compared}} repos, {{slower}} got slower and {{failing}} fail more often than in the previous period",
    reposNone: "Repos: no comparison with the previous period",
    redRuns: "in failed builds, of {{h}} CI time",
    waitingTitle:
      "Typical value over {{n}} of the {{total}} builds that report when they got an agent",
    waiting:
      "of which typically {{wait}} waiting for an agent, {{build}} building",
    scale: "CW {{kw}} · success, scale {{low}}–100 %",
    runs_one: "{{count}} build",
    runs_other: "{{count}} builds",
    noVerdict: " + {{n}} without a result",
    outliers: " · {{n}} outliers ({{h}}) not in duration and CI time",
    flaky_one:
      "{{count}} failed build flaky: succeeded afterwards on the same commit ({{share}} of the failed, {{h}})",
    flaky_other:
      "{{count}} failed builds flaky: succeeded afterwards on the same commit ({{share}} of the failed, {{h}})",
    flakyNone: "No failed build succeeded afterwards on the same commit",
    flakyNoRed: "No failed build, so none flaky",
    unjudged_one: " · {{count}} build without a commit not judged",
    unjudged_other: " · {{count}} builds without a commit not judged",
    flakyRate: "Flaky rate {{rate}} of builds with a commit",
    viewFlaky: "View flaky builds →",
    flakyTitle:
      "Flaky: a build fails, a later build of the same pipeline on the same commit succeeds. A lower bound: retries inside a build, commits built once and reruns after the period's end are missing. Whether tests or infrastructure failed is not in the data.",
    repoFlaky: "{{n}} flaky",
    repoFlakyTitle:
      "{{h}} of it in builds a later build on the same commit made succeed",
    failingMore: "Fails more often",
    failingMoreText: "previous period {{prev}} → now {{rate}} failed",
    slow: "Got slower",
    slowText: "previous period {{prev}} → now {{p50}} ({{factor}}×)",
    reliable: "Got more reliable",
    faster: "Got faster",
    fasterText: "previous period {{prev}} → now {{p50}}",
    fine: "Unremarkable",
    noCompare: "No comparison",
    noCompareNoValue:
      "no comparable builds of this kind in the previous period",
    noCompareIncomplete: "build data of the previous period incomplete",
    noCompareLowSample: "under {{min}} builds in one of the two periods",
    fineText: "{{p50}}, {{rate}} failed",
    fineWait: " · {{wait}} waiting, {{build}} building",
    repoWait: "Typically {{wait}} waiting for an agent, {{build}} building",
    group_one: "{{count}} build from {{repos}} repos started {{when}}",
    group_other: "{{count}} builds from {{repos}} repos started {{when}}",
    groupSameDay: "on {{day}} between {{from}} and {{to}}",
    groupSpan: "between {{from}} and {{to}}",
    groupRest:
      ": that points to the infrastructure (agents gone, a waiting input step), not to the repos.",
    colPipeline: "Pipeline",
    colKind: "Kind",
    colStart: "Start",
    colDuration: "Duration",
    colWaiting: "Waiting",
    colFactor: "× typical",
    noAgent: "The build does not report when it got an agent",
    eyebrow: "CI · Jenkins · Tekton · each kind of build on its own",
    title: "How fast and reliable is the feedback?",
    fwEffectiveness: "Effectiveness, driver",
    dataFrom: "Build data from {{date}}; the weeks before are empty.",
    unattributed_one:
      "{{count, num}} build without a repo: neither its commit nor its pipeline name names one. Counted in the team view, in no product view.",
    unattributed_other:
      "{{count, num}} builds without a repo: neither their commit nor their pipeline name names one. Counted in the team view, in no product view.",
    botRuns_one: "Bot builds: {{count, num}} build, {{h}} of CI time",
    botRuns_other: "Bot builds: {{count, num}} builds, {{h}} of CI time",
    botRunsFailed_one: ", {{count, num}} of them failed ({{h}})",
    botRunsFailed_other: ", {{count, num}} of them failed ({{h}})",
    botRunsRest: ". A cost, no finding: in no other number of this section.",
    noRuns: "No finished builds in the period.",
    legend:
      "Successful: SUCCESS (Tekton: Succeeded, Completed). Failed: FAILURE, UNSTABLE or Failed. Without a result (ABORTED, NOT_BUILT): no build that counts, only CI time; a Jenkins timeout() also ends ABORTED.",
    hotspotsTitle: "Repos that slow down the feedback",
    hotspotsSub:
      "Each repo against its own builds of the same kind in the previous period",
    kindGroup: "Kind of build",
    colRepo: "Repo",
    colWhy: "Why",
    colRed: "Failed CI time",
    viewRuns: "View builds →",
    noneOfKind: "No builds of this kind with a result.",
    slowestTitle: "Longest builds",
    slowestSub:
      "By the typical duration of successful builds, outliers aside, from {{min}} of them.",
    colMedian: "Typical",
    colSlowest: "Slowest 10 %",
    colWaitBuild: "Waiting / building",
    colRuns: "Builds",
    noAgentRepo: "None of the builds reports when it got an agent",
    hotspotsRule:
      "Fails more often: rate at least {{failing}}× the previous period's and {{points}} points above it, at most halfway between it and 100 %, with {{failures}} failed builds or more. Got slower: typical duration at least {{slow}}× the previous period's. Both from {{min}} builds in both periods.",
    stuckTitle: "Stuck builds",
    stuckSub:
      "Builds over {{factor}}× the typical duration of their kind and over {{min}}",
    stuck_one: "1 build got stuck",
    stuck_other: "{{count}} builds got stuck",
    stuckRest:
      " in the period. Their time counts in neither duration nor CI time.",
    showLongest: "Show the {{n}} longest",
    showAll: "Show builds",
    noOutliers: "No outliers in the period.",
    stuckRule:
      "A repo with at least {{min}} successful builds of its own must also exceed its own typical duration {{factor}}×. Aborted builds still appear.",
    flow: {
      title: "Where the CI hours go",
      sub: "Each kind's CI time by outcome, outliers included{{sep}}band width = hours",
      outcome: {
        succeeded: "Succeeded",
        flaky: "Flaky (succeeded afterwards)",
        failed: "Failed",
        no_verdict: "Without a result",
        outlier: "Outliers",
      },
      tipHours: "CI time",
      tipOfKind: "Share of {{kind}}",
      tipOfAll: "Share of all CI time",
      aria: "CI time from kind of build to outcome: {{parts}}",
      withOutliers: "{{h}} incl. outliers",
      repos: "By repo, across build kinds",
      reposMeta: "hatched: failed (flaky included), outliers aside",
      repoFailed: "{{repo}}: {{h}}, {{failed}} of it failed",
      repoFailedShort: " · {{h}} failed",
      reposOther_one: "{{count}} more",
      reposOther_other: "{{count}} more",
      reposAria: "CI time by repo: {{parts}}",
      rule: "Failed: FAILURE, UNSTABLE, Failed. Flaky: failed as well, but a later build of the same pipeline succeeded on the same commit. Without a result: ABORTED, NOT_BUILT, Tekton's None, Jenkins timeouts included. Outliers: {{outliers}} (a repo with enough builds of its own also over its own typical duration). An outlier counts only as an outlier, whatever its result, so the hours here include the outliers the cards above leave out. Each kind's bands show its own split; a node's height only the amount, no comparison between kinds.",
    },
  },
  quiver: {
    aria: "{{kind}}: each repo from the previous period to now, typical duration against failure rate",
    xAxis: "Typical duration (log) →",
    yAxis: "↑ Failure rate",
    slower: "▲ ×{{factor}} duration",
    failingMore: "▲ failure rate {{prev}} → {{rate}}",
    median: "Typical",
    rate: "Failure rate",
    runs: "Builds",
    ci: "CI time",
    legend:
      "Arrow: previous period → now; a dot in or at the circle: barely moved · filled circle without a dot: only the typical duration or only the failure rate compares · circle area: share of this kind's CI time · red: got slower or fails more often, as below · dashed: no comparison.",
    unplotted_one:
      "{{count}} repo of this kind is not drawn: past the list, without a build with a result, or without a typical duration.",
    unplotted_other:
      "{{count}} repos of this kind are not drawn: past the list, without a build with a result, or without a typical duration.",
  },
  costs: {
    eyebrow: "FinOps · noergler",
    title: "What do the AI reviews cost?",
    lead: "Spend on the automatic reviews in the period.",
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
    searchLabel: "Search by repo, app, PR or Jira key",
    placeholder: "Repo, app, PR, Jira key …",
    failed: "The single facts could not be loaded; the message is above.",
    tabs: "Facts table",
    tabPRs: "PRs",
    tabDeploys: "Deploys",
    tabRuns: "Builds",
    filtered: "filtered by “{{q}}”",
    newest: "Newest first",
    openFirst:
      "Open ones first, oldest on top, inactive after; then the newest",
    openFor: "open for {{age}}",
    idleFor: "no event for {{idle}}",
    firstOf: " · the first {{limit}} of {{count}}",
    runningNote: " · rows marked “This week” count in no number",
    noHits: "No hits for “{{q}}”.",
    empty: "No entries in the period.",
    colPR: "PR",
    openPR: "Open the PR in Bitbucket",
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
    colRun: "Build",
    colKind: "Kind",
    colCommit: "Commit",
    colDuration: "Duration",
    colImages: "Images",
    onProdAt: "On {{prod}} since {{date}},",
    route: {
      compare: "found through the compare link in the release commit.",
      image: "found through the version a build built.",
    },
    atMostTitle:
      "At most: the PR may have reached {{prod}} with an earlier deploy that cannot be tied to PRs.",
    ...terms.en,
  },
  frameworks: {
    eyebrow: "Classification",
    title: "Which frameworks Riptide covers",
    lead: "Measurable without a survey: two of the five DORA metrics, DX Core 4 in part.",
    row: {
      freq: "Deployment frequency",
      lead: "Lead time to PROD",
      stages: "Flow by stage",
      dsucc: "Deploy success",
      review: "Review time, merged within 24 h",
      touch: "Approval without comment",
      merged: "PR throughput",
      batch: "Batch size",
      mix: "Work mix, without a Jira key",
      build: "Build: success, duration, failed builds",
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
      diffsTeam:
        "Speed measures “diffs per engineer”: Riptide counts per team, never per person",
      activity: "Activity",
      flowVelocity: "Flow velocity",
      smallBatches: "Capability “Working in small batches”",
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
  },
};

export type Catalog = typeof en;
