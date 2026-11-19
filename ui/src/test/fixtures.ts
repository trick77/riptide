// Responses of the read API for tests: computed numbers as the backend
// sends them, overridden field by field. The UI only renders them; the
// computation's tests are the backend's. Synthetic names only.

import type {
  AnalysisStatus,
  AnalysisText,
  Assessment,
  BuildStats,
  Change,
  Fact,
  Signal,
  DeployDay,
  DeployFact,
  Facts,
  KindHotspots,
  Metrics,
  PRFact,
  RunFact,
  Spread,
  Stage,
  Week,
} from "../api";

export const NOW = new Date("2026-11-12T12:00:00+01:00"); // a Thursday, KW 46

// An ISO instant `days` days (fractions allowed) before NOW.
export function ago(days: number): string {
  return new Date(NOW.getTime() - days * 86_400_000).toISOString();
}

const DAY = 86_400_000;
// Monday 00:00 of KW 46, Swiss winter time.
const KW46 = Date.parse("2026-11-09T00:00:00+01:00");

// The period of n weeks ending with KW 46, as the backend numbers it.
export function weeks(n: number): Week[] {
  return Array.from({ length: n }, (_, i) => {
    const start = KW46 - (n - 1 - i) * 7 * DAY;
    return {
      start: new Date(start).toISOString(),
      end: new Date(start + 7 * DAY).toISOString(),
      kw: 46 - (n - 1 - i),
    };
  });
}

export const spread = (n: number, p50: number, p90 = p50): Spread => ({
  n,
  p50,
  p90,
});

// No run reports its agent time: n 0, nulls rather than 0.
export const noSpread = (): Spread => ({ n: 0, p50: null, p90: null });

const none: Spread = { n: 0, p50: null, p90: null };

// The backend's stage histogram edges: 5 min to 28 days, log-spaced.
export const STAGE_BOUNDS = [
  300, 452, 680, 1023, 1540, 2317, 3488, 5249, 7901, 11892, 17899, 26940, 40548,
  61029, 91856, 138254, 208089, 313198, 471400, 709513, 1067901, 1607316,
  2419200,
];

// A stage's histogram with every PR in the bucket holding p50.
export function stageHistogram(s: Spread): number[] {
  const h = STAGE_BOUNDS.slice(1).map(() => 0);
  if (s.p50 != null) {
    const i = STAGE_BOUNDS.findIndex((b) => b > s.p50!) - 1;
    h[Math.min(Math.max(i, 0), h.length - 1)] = s.n;
  }
  return h;
}

function stage(
  key: Stage["key"],
  s: Spread,
  share: number,
  hot = false,
): Stage {
  const low = s.n < 10;
  return {
    key,
    ...s,
    low_sample: low,
    share,
    hot,
    histogram: low ? null : stageHistogram(s),
    durations_s: low ? Array.from({ length: s.n }, () => s.p50 ?? 0) : null,
  };
}

function kindStats(
  kind: BuildStats["kind"],
  runs: number,
  succeeded: number,
  n: number,
): BuildStats {
  return {
    kind,
    runs,
    succeeded,
    failed: runs - succeeded,
    no_verdict: 0,
    success_rate: succeeded / runs,
    duration: spread(runs, 600),
    // No run reports its agent time: the split stays hidden.
    waiting: { n: 0, p50: null, p90: null },
    building: { n: 0, p50: null, p90: null },
    ci_seconds: runs * 600,
    wasted_seconds: (runs - succeeded) * 600,
    flaky_runs: 0,
    flaky_rate: runs ? 0 : null,
    flaky_of_failed: runs - succeeded ? 0 : null,
    flaky_seconds: 0,
    unjudged: 0,
    outlier_runs: 0,
    outlier_seconds: 0,
    weekly: Array.from({ length: n }, (_, i) =>
      i === n - 1 ? succeeded / runs : null,
    ),
    prefixes: [],
  };
}

function kindHotspots(
  kind: KindHotspots["kind"],
  failures: number,
): KindHotspots {
  return {
    kind,
    duration: spread(1, 600),
    failure_rate: failures,
    repos: [
      {
        repo: "kasse/kasse-api",
        runs: 1,
        duration: spread(1, 600),
        waiting: noSpread(),
        building: noSpread(),
        duration_change: change("none", null, "low_sample"),
        factor: null,
        slow: false,
        failures,
        failure_rate: failures,
        failure_change: change("none", null, "low_sample"),
        wasted_seconds: failures * 600,
        failing_more: false,
        flaky_runs: 0,
        flaky_seconds: 0,
        ci_seconds: 600,
        compared: false,
        prev_complete: false,
        plotted: true,
      },
    ],
    // One run: too few for the slowest list.
    slowest: [],
    outlier_threshold_s: null,
    unplotted: 0,
    // Set per test: its weeks follow the period.
    heatmap: [],
  };
}

// A comparison verdict as the backend sends it, for a number with a
// better direction; undirected() for one without.
export function change(
  verdict: Change["verdict"],
  prev: number | null = null,
  reason: Change["reason"] = null,
  score: number | null = verdict === "none" ? null : 0,
  directed = true,
  magnitude: Change["magnitude"] = verdict === "none"
    ? null
    : verdict === "same"
      ? "same"
      : "minor",
): Change {
  return { prev, verdict, reason, directed, score, magnitude };
}

// A number without a better direction, as the backend sends it: no
// verdict, `undirected` once both periods compare, else why not.
export function undirected(
  prev: number | null,
  reason: Exclude<Change["reason"], null> = "undirected",
): Change {
  return change("none", prev, reason, null, false);
}

const lowSample = change("none", null, "low_sample");

// The sample team: one release, a re-sync and a first deploy on prod, a
// failed sync; one human PR through intg to prod, a bot's PR; three runs.
export function metrics(n = 4, o: Partial<Metrics> = {}): Metrics {
  const w = weeks(n);
  const zeroWeeks = <T>(v: T) => w.map(() => ({ ...v }));
  const deployWeeks = zeroWeeks({ release: 0, sync: 0, first_seen: 0 });
  if (n >= 3) {
    deployWeeks[n - 3].first_seen = 1;
    deployWeeks[n - 2].sync = 1;
  }
  deployWeeks[n - 1].release = 1;
  // The release on Wednesday of KW 46; Friday on is after NOW.
  const monday = Date.UTC(2026, 10, 9) - (n - 1) * 7 * DAY;
  const days = Array.from({ length: 7 * n }, (_, i) => ({
    date: new Date(monday + i * DAY).toISOString().slice(0, 10),
    releases: 0,
    step: 0 as DeployDay["step"],
    no_data: i > 7 * (n - 1) + 3,
  }));
  days[7 * (n - 1) + 2] = { ...days[7 * (n - 1) + 2], releases: 1, step: 1 };
  // Thursday (today, not over) to Sunday have one whole day with data less.
  const weekdays = Array.from({ length: 7 }, (_, d) => {
    const releases = d === 2 ? 1 : 0;
    const count = d < 3 ? n : n - 1;
    return {
      releases,
      days: count,
      per_day: count ? releases / count : null,
    };
  });
  const mix = zeroWeeks({ feature: 0, bugfix: 0, hotfix: 0, other: 0 });
  mix[n - 1].feature = 1;
  return {
    meta: {
      team: "team-a",
      from: w[0].start,
      to: NOW.toISOString(),
      generated_at: NOW.toISOString(),
      production_stage: "prod",
      data_start: {
        bitbucket: ago(60),
        pipeline: ago(10),
        argocd: ago(60),
        noergler: ago(60),
      },
      data_end: {
        bitbucket: ago(0.1),
        pipeline: ago(0.2),
        argocd: ago(0.3),
        noergler: ago(0.4),
      },
      build_data_from: ago(60),
      build_type_from: ago(10),
    },
    period: {
      from: w[0].start,
      to: NOW.toISOString(),
      last_day: "2026-11-15",
      weeks: w,
    },
    products: [
      {
        key: "kasse",
        repos: ["kasse/kasse-api", "kasse/kasse-gitops"],
        apps: ["kasse-prod"],
      },
      { key: "portal", repos: ["portal/portal-web"], apps: [] },
    ],
    product: null,
    delivery: {
      production_stage: "prod",
      pre_environment: "intg",
      frequency: {
        releases: 1,
        syncs: 3,
        first_seen: 1,
        resyncs: 1,
        per_week: 0.29,
        low_sample: false,
        since: null,
        weeks: deployWeeks,
        succeeded: 3,
        failed: 1,
        finished: 4,
        success_rate: 0.75,
        argo_reporting: true,
        median_week: 1,
        days,
        no_data_days: 3,
        weekdays,
      },
      lead_time: {
        environment: "prod",
        human: spread(1, 2 * 86400),
        bot: spread(1, 600),
        low_sample: true,
      },
      lead_time_pre: {
        environment: "intg",
        human: spread(1, 3 * 3600),
        bot: none,
        low_sample: true,
      },
    },
    lead_chain: {
      stages: [
        stage("coding", spread(1, 86400), 0.4, true),
        stage("review", spread(1, 0.1 * 86400), 0.04),
        stage("merge", spread(1, 0.1 * 86400), 0.04),
        stage("to_pre", spread(1, 0.1 * 86400), 0.04),
        stage("to_prod", spread(1, 0.7 * 86400), 0.48),
      ],
      pre_environment: "intg",
      attention_share: 0.5,
      min_prs: 10,
      before_prod: 1.3 * 86400,
      reaches_prod: true,
      bucket_bounds_s: STAGE_BOUNDS,
    },
    review: {
      cycle: spread(1, 0.2 * 86400),
      review: spread(1, 0.1 * 86400),
      approved: 1,
      approved_on_first_touch: 1,
      first_touch_rate: 1,
      merged: 1,
      untracked: 0,
      untracked_rate: 0,
      cycle_histogram: {
        upper_bounds_s: [900, 3600, 14400, 86400, 259200],
        human: [0, 0, 0, 1, 0, 0],
      },
      within_day: { count: 1, n: 1, rate: 1 },
      change_mix: mix,
      mix_total: { feature: 1, bugfix: 0, hotfix: 0, other: 0 },
      mix_share: { feature: 1, bugfix: 0, hotfix: 0, other: 0 },
    },
    build: {
      data_from: ago(60),
      starts_inside: false,
      gap_weeks: 0,
      // the backend's order: feature first
      kinds: [
        kindStats("feature", 1, 0, n),
        kindStats("main", 1, 0, n),
        kindStats("release", 1, 1, n),
      ],
      ci_flow: {
        total_seconds: 1800,
        kinds: [
          { kind: "feature", seconds: 600, share: 1 / 3 },
          { kind: "main", seconds: 600, share: 1 / 3 },
          { kind: "release", seconds: 600, share: 1 / 3 },
        ],
        outcomes: [
          { outcome: "succeeded", seconds: 600, share: 1 / 3 },
          { outcome: "failed", seconds: 1200, share: 2 / 3 },
        ],
        links: [
          {
            kind: "feature",
            outcome: "failed",
            seconds: 600,
            share_of_kind: 1,
            share: 1 / 3,
          },
          {
            kind: "main",
            outcome: "failed",
            seconds: 600,
            share_of_kind: 1,
            share: 1 / 3,
          },
          {
            kind: "release",
            outcome: "succeeded",
            seconds: 600,
            share_of_kind: 1,
            share: 1 / 3,
          },
        ],
        repos: [
          { repo: "kasse/kasse-web", seconds: 900, failed: 600 },
          { repo: "kasse/kasse-api", seconds: 500, failed: 400 },
          { repo: "kasse/policy", seconds: 200, failed: 0 },
        ],
        other_repos: 2,
        other_seconds: 200,
        other_failed: 200,
      },
      feature_hint: null,
      hotspots: {
        min_runs: 10,
        slow_factor: 1.5,
        failing_more_factor: 2,
        failing_more_min_points: 0.05,
        failing_more_min_failures: 2,
        outlier_factor: 6,
        outlier_min_s: 3600,
        outlier_repo_min_runs: 3,
        slowest_min_runs: 3,
        heat_min_runs: 3,
        heat_steps: [0.1, 0.2, 0.4],
        heat_classes: [
          { step: 0, from: null, to: null },
          { step: 1, from: null, to: 0.1 },
          { step: 2, from: 0.1, to: 0.2 },
          { step: 3, from: 0.2, to: 0.4 },
          { step: 4, from: 0.4, to: null },
        ],
        kinds: [
          kindHotspots("feature", 1),
          kindHotspots("main", 1),
          kindHotspots("release", 0),
        ],
        outliers: [],
        outlier_count: 0,
        outlier_groups: [],
      },
    },
    open_prs: {
      at: w[w.length - 1].end,
      long_days: 7,
      jam_min: 4,
      inactive_days: 30,
      open: 4,
      long: 3,
      jammed: 1,
      jammed_long: 3,
      inactive: 1,
      age: { n: 3, p50: 9 * 86400, p90: 28 * 86400 },
      waiting: { draft: 0, first_review: 3, review: 0, changes: 0, merge: 1 },
      long_waiting: {
        draft: 0,
        first_review: 2,
        review: 0,
        changes: 0,
        merge: 1,
      },
      jammed_waiting: {
        draft: 0,
        first_review: 2,
        review: 0,
        changes: 0,
        merge: 1,
      },
      repos: [
        {
          repo: "kasse/kasse-api",
          open: 3,
          long: 3,
          jammed: true,
          age: { n: 3, p50: 12 * 86400, p90: 28 * 86400 },
          waiting: {
            draft: 0,
            first_review: 2,
            review: 0,
            changes: 0,
            merge: 1,
          },
          long_waiting: {
            draft: 0,
            first_review: 2,
            review: 0,
            changes: 0,
            merge: 1,
          },
          inactive: 1,
        },
        {
          repo: "portal/portal-web",
          open: 1,
          long: 0,
          jammed: false,
          age: { n: 1, p50: 2 * 86400, p90: 2 * 86400 },
          waiting: {
            draft: 0,
            first_review: 1,
            review: 0,
            changes: 0,
            merge: 0,
          },
          long_waiting: {
            draft: 0,
            first_review: 0,
            review: 0,
            changes: 0,
            merge: 0,
          },
          inactive: 0,
        },
      ],
    },
    costs: {
      total: 0.75,
      merged: 0.5,
      merged_reviews: 1,
      per_kloc: 1,
      wasted: 0.25,
      wasted_share: 1 / 3,
      promotion: 0,
      promotion_reviews: 0,
      rows: [{ label: "kasse", value: 0.75 }],
      reviewed_repos: 1,
    },
    comparison: {
      from: weeks(2 * n)[0].start,
      to: w[0].start,
      last_day: "2026-10-18",
      deploy_frequency: change("better", 0.1, null, 1),
      deploy_success: change("same", 0.75),
      lead_time: change("worse", 86400, null, -0.6),
      lead_time_pre: lowSample,
      review_time: lowSample,
      within_day: lowSample,
      first_touch: undirected(0.5),
      untracked: undirected(null, "low_sample"),
      cost_per_kloc: undirected(null, "incomplete"),
      wasted_share: undirected(null, "low_sample"),
      open_prs: undirected(3),
      open_jammed: change("same", 1),
      build: [
        {
          kind: "release",
          success: lowSample,
          flaky: lowSample,
          slower: 0,
          failing_more: 0,
          compared: 0,
        },
        {
          kind: "main",
          success: lowSample,
          flaky: lowSample,
          slower: 0,
          failing_more: 0,
          compared: 0,
        },
        {
          kind: "feature",
          success: lowSample,
          flaky: lowSample,
          slower: 0,
          failing_more: 0,
          compared: 0,
        },
      ],
    },
    running_week: {
      week: { start: w[n - 1].end, end: w[n - 1].end, kw: w[n - 1].kw + 1 },
      to: w[n - 1].end,
      releases: 1,
      merged: 2,
      runs: 1,
    },
    ...o,
  };
}

// The parts the backend splits a fixture title into: a plain title is one
// part, the default one starts with its linked key.
function titleParts(title: string | null): PRFact["title_parts"] {
  if (title === null) return [];
  if (title !== "KAS-1: Pay by card") return [{ text: title, jira: null }];
  return [
    {
      text: "KAS-1",
      jira: { key: "KAS-1", url: "https://jira.example.com/browse/KAS-1" },
    },
    { text: ": Pay by card", jira: null },
  ];
}

export function prFact(o: Partial<PRFact> = {}): PRFact {
  const title = o.title === undefined ? "KAS-1: Pay by card" : o.title;
  return {
    repo: "kasse/kasse-api",
    pr_id: 1,
    url: "https://bitbucket.example.com/projects/KASSE/repos/kasse-api/pull-requests/1",
    title,
    title_parts: titleParts(title),
    jira_prefix: [],
    jira_more: 0,
    gitops: false,
    change_type: "feature",
    state: "merged",
    review_s: 0.1 * 86400,
    cycle_s: 0.2 * 86400,
    merged_at: ago(1.8),
    prod_at: ago(1),
    to_prod_s: 0.8 * 86400,
    age_s: null,
    waiting: null,
    idle_s: null,
    running: false,
    ...o,
  };
}

export function deployFact(o: Partial<DeployFact> = {}): DeployFact {
  return {
    at: ago(1),
    app: "kasse-prod",
    environment: "prod",
    revision: "r1",
    new_images: ["reg/kasse-api:1.1"],
    class: "release",
    phase: "Succeeded",
    running: false,
    ...o,
  };
}

export function runFact(o: Partial<RunFact> = {}): RunFact {
  return {
    pipeline_name: "kasse-api/master",
    run_id: "1",
    kind: "main",
    commit_sha: "c".repeat(40),
    shown_sha: "c".repeat(40),
    duration_s: 600,
    image_refs: [],
    status: "SUCCESS",
    verdict: "success",
    outcome: "succeeded",
    gitops: false,
    flaky: false,
    running: false,
    ...o,
  };
}

export function facts(o: Partial<Facts> = {}): Facts {
  const prs = [
    prFact(),
    prFact({
      pr_id: 3,
      repo: "kasse/kasse-gitops",
      gitops: true,
      title: "kasse PROD 1.1",
      jira_prefix: [
        { key: "KAS-2", url: null },
        { key: "KAS-3", url: null },
        { key: "KAS-4", url: null },
      ],
      jira_more: 2,
    }),
    prFact({
      pr_id: 4,
      repo: "portal/portal-web",
      url: null,
      title: null,
      change_type: "bugfix",
      state: "open",
      merged_at: null,
      prod_at: null,
      to_prod_s: null,
      age_s: 2 * 86400,
      waiting: "first_review",
      idle_s: 86400,
    }),
  ];
  const deploys = [
    deployFact(),
    deployFact({
      revision: "r4",
      at: ago(2),
      new_images: [],
      class: "failed",
      phase: "Failed",
    }),
    deployFact({
      revision: "r2",
      at: ago(8),
      new_images: [],
      class: "sync",
    }),
    deployFact({
      revision: "r3",
      app: "kasse-new",
      at: ago(15),
      new_images: [],
      class: "first_seen",
    }),
  ];
  const runs = [
    runFact({
      run_id: "10",
      kind: "release",
      image_refs: ["reg/kasse-api:1.1"],
    }),
    runFact({
      run_id: "11",
      status: "FAILURE",
      verdict: "failure",
      outcome: "failed",
      duration_s: 900,
    }),
    runFact({
      run_id: "12",
      kind: "feature",
      pipeline_name: "kasse-api/feature%2Fx",
      status: "UNSTABLE",
      verdict: "failure",
      outcome: null,
    }),
    runFact({
      run_id: "13",
      pipeline_name: "kasse-gitops/master",
      gitops: true,
    }),
  ];
  return {
    production_stage: "prod",
    limit: 300,
    prs: { count: prs.length, rows: prs },
    deploys: { count: deploys.length, rows: deploys },
    runs: { count: runs.length, rows: runs },
    ...o,
  };
}

function fact(
  id: string,
  label: string,
  unit: Fact["unit"],
  value: number | null,
  prev: number | null = null,
  verdict: Fact["verdict"] = "none",
): Fact {
  return {
    id,
    label,
    unit,
    value,
    prev,
    verdict,
    magnitude: verdict === "none" ? null : "notable",
  };
}

function signal(o: Partial<Signal> & Pick<Signal, "id" | "kind">): Signal {
  return {
    tone: "hint",
    source: "bitbucket",
    repo: null,
    run_kind: null,
    metric: null,
    stage: null,
    prev_stage: null,
    low_sample: false,
    sample: null,
    weight: 1,
    seen: 0,
    facts: [],
    waiting: null,
    drivers: null,
    ...o,
  };
}

// The assessment of the sample team's closed weeks: a jam that was there
// before, a steady slow feature build, a review time that doubled through
// one repo, deploys notably more often, a build failing more resolved, forgotten
// PRs no wording covers, and Argo CD silent.
export function assessment(): Assessment {
  const w = weeks(4);
  const jam = signal({
    id: "pr_jam:kasse/kasse-api",
    kind: "pr_jam",
    tone: "attention",
    repo: "kasse/kasse-api",
    weight: 4,
    seen: 2,
    facts: [
      fact("pr_jam:kasse/kasse-api.long", "open_long", "count", 5, 3, "worse"),
      fact("pr_jam:kasse/kasse-api.open", "open", "count", 7),
      fact("pr_jam:kasse/kasse-api.age", "age_p50", "seconds", 12 * 86400),
    ],
    waiting: { draft: 0, first_review: 4, review: 0, changes: 1, merge: 0 },
  });
  const slow = signal({
    id: "steady_slow:feature:kasse/kasse-web",
    kind: "steady_slow",
    source: "pipeline",
    repo: "kasse/kasse-web",
    run_kind: "feature",
    weight: 3,
    facts: [
      fact(
        "steady_slow:feature:kasse/kasse-web.p50",
        "build_p50",
        "seconds",
        1440,
        1380,
        "same",
      ),
      fact(
        "steady_slow:feature:kasse/kasse-web.kind_p50",
        "kind_p50",
        "seconds",
        480,
      ),
    ],
  });
  return {
    period: { from: w[0].start, to: w[3].end, last_day: "", weeks: w },
    previous: { from: w[0].start, to: w[0].start, last_day: "", weeks: [] },
    gaps: ["argocd"],
    signals: [
      signal({
        id: "data_gap:argocd",
        kind: "data_gap",
        tone: "gate",
        source: "argocd",
        weight: 10,
        facts: [fact("data_gap_argocd.silent", "silent_days", "days", 6)],
      }),
      jam,
      signal({
        id: "change:review_time",
        kind: "change",
        tone: "attention",
        metric: "review_time",
        weight: 2,
        facts: [
          fact(
            "change:review_time.value",
            "review_p50",
            "seconds",
            9.4 * 3600,
            4.6 * 3600,
            "worse",
          ),
        ],
        drivers: [
          { repo: "kasse/kasse-api", share: 0.7, value: 50400, prev: 18000 },
        ],
      }),
      slow,
      signal({
        id: "change:merged_per_week",
        kind: "change",
        tone: "good",
        metric: "merged_per_week",
        weight: 2,
        facts: [
          fact(
            "change:merged_per_week.value",
            "merged_per_week",
            "per_week",
            11,
            8.4,
            "better",
          ),
        ],
      }),
      signal({
        id: "resolved:failing_more:feature:kasse/policy",
        kind: "resolved",
        tone: "good",
        source: "pipeline",
        repo: "kasse/policy",
        run_kind: "feature",
        metric: "failing_more:feature:kasse/policy",
        weight: 2,
        facts: [
          fact(
            "resolved:failing_more:feature:kasse/policy.failure_rate",
            "failure_rate",
            "share",
            0.02,
            0.14,
            "better",
          ),
        ],
      }),
      signal({
        id: "inactive_prs",
        kind: "inactive_prs",
        facts: [fact("inactive_prs.count", "inactive", "count", 4)],
      }),
    ],
    focus: [
      {
        repo: "kasse/kasse-api",
        weight: 7,
        seen: 2,
        signals: [jam.id, "change:review_time"],
        facts: [jam.facts[0]],
        waiting: jam.waiting,
      },
      {
        repo: "kasse/kasse-web",
        weight: 3,
        seen: 0,
        signals: [slow.id],
        facts: slow.facts,
        waiting: null,
      },
    ],
    active: 11,
    calm: 9,
    team: [],
    question: signal({
      id: "question:first_touch",
      kind: "question",
      tone: "talk",
      metric: "first_touch",
      facts: [fact("question:first_touch.value", "first_touch", "share", 0.64)],
    }),
    compared: true,
  };
}

// The LLM's wording of assessment(), in both languages.
export function analysisText(): AnalysisText {
  return {
    headline: "Ihr liefert öfter – aber Reviews bleiben liegen.",
    trend: "Über alle aktiven Repos brauchen Reviews länger.",
    focus: [
      {
        repo: "kasse/kasse-api",
        take: "{{pr_jam:kasse/kasse-api.long}} PRs liegen, und Reviews dauern.",
        retro: "Warum liegen diese PRs?",
        lever: "walk_prs",
      },
      {
        repo: "kasse/kasse-web",
        take: "Konstant der langsamste Build mit {{steady_slow:feature:kasse/kasse-web.p50}}.",
        retro: null,
        lever: "inspect_build_log",
      },
    ],
    cards: [
      {
        signal: "change:merged_per_week",
        take: "Mehr PRs kommen durch.",
        retro: null,
        lever: "name_what_helped",
      },
      {
        signal: "change:review_time",
        take: "Seit kurzem bleibt mehr liegen.",
        retro: "Was war seit dem letzten Mal anders?",
        lever: "find_what_changed",
      },
      {
        signal: "resolved:failing_more:feature:kasse/policy",
        take: "Die Fehlerquote ist zurück.",
        retro: null,
        lever: "name_what_helped",
      },
    ],
    question: "Passt das zu unserer Art zu reviewen?",
  };
}

// The status /api/analysis answers: no model, the assessment alone, unless
// overridden.
export function analysisStatus(
  o: Partial<AnalysisStatus> = {},
): AnalysisStatus {
  return {
    state: "off",
    enabled: false,
    running: false,
    assessment: assessment(),
    // As the backend lays out assessment() without an analysis: both focus
    // repos (their signals covered), the team-wide signals left as cards.
    layout: {
      focus: [
        { repo: "kasse/kasse-api", lever: "walk_prs", tab: "prs" },
        { repo: "kasse/kasse-web", lever: "inspect_build_log", tab: "runs" },
      ],
      cards: [
        {
          signal: "change:merged_per_week",
          column: "good",
          lever: "name_what_helped",
        },
        {
          signal: "resolved:failing_more:feature:kasse/policy",
          column: "done",
          lever: "name_what_helped",
        },
        { signal: "inactive_prs", column: "bad", lever: "tidy_up" },
      ],
      also: [],
      tally: { good: 1, done: 1, bad: 4, baseline: true },
    },
    analysis: null,
    failure: null,
    next_run: null,
    cooldown_until: null,
    zone: "Europe/Zurich",
    ...o,
  };
}

// A status with a fresh analysis shown, the model set.
export function analysedStatus(
  o: Partial<AnalysisStatus> = {},
): AnalysisStatus {
  const de = analysisText();
  return analysisStatus({
    state: "fresh",
    enabled: true,
    next_run: "2026-11-16T05:00:00Z",
    // As the backend lays out analysisText(): what it words, the forgotten
    // PRs spotted without words.
    layout: {
      focus: [
        { repo: "kasse/kasse-api", lever: "walk_prs", tab: "prs" },
        { repo: "kasse/kasse-web", lever: "inspect_build_log", tab: "runs" },
      ],
      cards: [
        {
          signal: "change:merged_per_week",
          column: "good",
          lever: "name_what_helped",
        },
        {
          signal: "change:review_time",
          column: "bad",
          lever: "find_what_changed",
        },
        {
          signal: "resolved:failing_more:feature:kasse/policy",
          column: "done",
          lever: "name_what_helped",
        },
      ],
      also: [{ signal: "inactive_prs", column: "bad", lever: "tidy_up" }],
      tally: { good: 1, done: 1, bad: 4, baseline: true },
    },
    analysis: {
      id: 7,
      created_at: "2026-11-09T05:01:00Z",
      finished_at: "2026-11-09T05:02:00Z",
      trigger: "scheduled",
      model: "mimo-v2.6-pro",
      cost_usd: "0.007",
      weeks_old: 0,
      text: {
        de,
        en: { ...de, headline: "You ship more often, but reviews pile up." },
      },
      report: { dropped: [], missed: [] },
    },
    ...o,
  });
}
