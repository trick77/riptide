// Responses of the read API for tests: computed numbers as the backend
// sends them, overridden field by field. The UI only renders them; the
// computation's tests are the backend's. Synthetic names only.

import type {
  BuildStats,
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
    success_rate: succeeded / runs,
    duration: spread(runs, 600),
    ci_seconds: runs * 600,
    wasted_seconds: (runs - succeeded) * 600,
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
    durations: [
      {
        repo: "kasse/kasse-api",
        runs: 1,
        duration: spread(1, 600),
        factor: 1,
        slow: false,
      },
    ],
    failing: failures
      ? [
          {
            repo: "kasse/kasse-api",
            runs: 1,
            failures: 1,
            failure_rate: 1,
            wasted_seconds: 600,
            flaky: false,
          },
        ]
      : [],
    outlier_threshold_s: null,
  };
}

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
        since: null,
        weeks: deployWeeks,
        succeeded: 3,
        failed: 1,
        finished: 4,
        success_rate: 0.75,
        argo_reporting: true,
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
      sum_p50: 2 * 86400,
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
      change_mix: mix,
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
        slow_factor: 2,
        flaky_factor: 2,
        outlier_factor: 3,
        kinds: [
          kindHotspots("release", 0),
          kindHotspots("main", 1),
          kindHotspots("feature", 1),
        ],
        outliers: [],
      },
    },
    costs: {
      total: 0.75,
      merged: 0.5,
      merged_reviews: 1,
      per_kloc: 1,
      wasted: 0.25,
      rows: [{ label: "kasse", value: 0.75 }],
      reviewed_repos: 1,
    },
    ...o,
  };
}

export function prFact(o: Partial<PRFact> = {}): PRFact {
  return {
    repo: "kasse/kasse-api",
    pr_id: 1,
    title: "KAS-1: Pay by card",
    jira_prefix: "",
    gitops: false,
    bot: false,
    change_type: "feature",
    state: "merged",
    review_s: 0.1 * 86400,
    cycle_s: 0.2 * 86400,
    merged_at: ago(1.8),
    prod_at: ago(1),
    to_prod_s: 0.8 * 86400,
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
      jira_prefix: "KAS-2 KAS-3 KAS-4",
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
    runFact({ run_id: "11", status: "FAILURE", duration_s: 900 }),
    runFact({
      run_id: "12",
      kind: "feature",
      pipeline_name: "kasse-api/feature%2Fx",
      status: "UNSTABLE",
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
