// Responses of the read API for tests: computed numbers as the backend
// sends them, overridden field by field. The UI only renders them; the
// computation's tests are the backend's. Synthetic names only.

import type {
  BuildStats,
  Change,
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

function stage(
  key: Stage["key"],
  s: Spread,
  share: number,
  hot = false,
): Stage {
  return { key, ...s, low_sample: s.n < 10, share, hot };
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
    outlier_runs: 0,
    outlier_seconds: 0,
    weekly: Array.from({ length: n }, (_, i) =>
      i === n - 1 ? succeeded / runs : null,
    ),
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
        flaky: false,
      },
    ],
    // One run: too few for the slowest list.
    slowest: [],
    outlier_threshold_s: null,
  };
}

// A comparison verdict as the backend sends it.
export function change(
  verdict: Change["verdict"],
  prev: number | null = null,
  reason: Change["reason"] = null,
): Change {
  return { prev, verdict, reason };
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
    period: { from: w[0].start, to: NOW.toISOString(), weeks: w },
    products: [
      {
        key: "kasse",
        repos: ["kasse/kasse-api", "kasse/kasse-gitops"],
        apps: ["kasse-prod"],
      },
      { key: "portal", repos: ["portal/portal-web"], apps: [] },
    ],
    product: null,
    bots: false,
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
        bot: [0, 0, 0, 1, 0, 0],
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
      kinds: [
        kindStats("release", 1, 1, n),
        kindStats("main", 1, 0, n),
        kindStats("feature", 1, 0, n),
      ],
      hotspots: {
        min_runs: 10,
        slow_factor: 1.5,
        flaky_factor: 2,
        flaky_min_points: 0.05,
        flaky_min_failures: 2,
        outlier_factor: 6,
        outlier_min_s: 3600,
        outlier_repo_min_runs: 3,
        slowest_min_runs: 3,
        kinds: [
          kindHotspots("release", 0),
          kindHotspots("main", 1),
          kindHotspots("feature", 1),
        ],
        outliers: [],
        outlier_count: 0,
        outlier_groups: [],
      },
    },
    open_prs: {
      at: w[w.length - 1].end,
      long_days: 7,
      jam_min: 3,
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
    findings: [
      {
        kind: "stage",
        tone: "hint",
        stage: "to_prod",
        run_kind: null,
        repo: null,
      },
      {
        kind: "first_touch",
        tone: "talk",
        stage: null,
        run_kind: null,
        repo: null,
      },
    ],
    comparison: {
      from: weeks(2 * n)[0].start,
      to: w[0].start,
      deploy_frequency: change("better", 0.1),
      deploy_success: change("same", 0.75),
      lead_time: change("worse", 86400),
      lead_time_pre: lowSample,
      review_time: lowSample,
      within_day: lowSample,
      first_touch: change("none", 0.5, "undirected"),
      untracked: lowSample,
      cost_per_kloc: change("none", null, "incomplete"),
      wasted_share: lowSample,
      open_prs: change("same", 3),
      open_jammed: change("same", 1),
      build: [
        {
          kind: "release",
          success: lowSample,
          slower: 0,
          flakier: 0,
          compared: 0,
        },
        {
          kind: "main",
          success: lowSample,
          slower: 0,
          flakier: 0,
          compared: 0,
        },
        {
          kind: "feature",
          success: lowSample,
          slower: 0,
          flakier: 0,
          compared: 0,
        },
      ],
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
    title,
    title_parts: titleParts(title),
    jira_prefix: [],
    jira_more: 0,
    gitops: false,
    bot: false,
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
    ...o,
  };
}

export function runFact(o: Partial<RunFact> = {}): RunFact {
  return {
    pipeline_name: "kasse-api/master",
    run_id: "1",
    kind: "main",
    commit_sha: "c".repeat(40),
    duration_s: 600,
    image_refs: [],
    status: "SUCCESS",
    verdict: "success",
    outcome: "succeeded",
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
