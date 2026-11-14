// Builders for datasets in tests: a row with sensible defaults, overridden
// field by field. Synthetic names only.

import type { Dataset, Deploy, LeadTime, PR, Review, Run } from "../api";

export const NOW = new Date("2026-11-12T12:00:00+01:00"); // a Thursday

// An ISO instant `days` days (fractions allowed) before NOW.
export function ago(days: number): string {
  return new Date(NOW.getTime() - days * 86_400_000).toISOString();
}

export function pr(o: Partial<PR> = {}): PR {
  return {
    repo: "kasse/kasse-api",
    project: "kasse",
    pr_id: 1,
    title: "Pay by card",
    gitops: false,
    bot: false,
    change_type: "feature",
    jira_keys: ["KAS-1"],
    state: "merged",
    opened_at: ago(2),
    ready_at: ago(2),
    first_review_at: ago(1.9),
    approved_at: ago(1.9),
    approved_on_first_touch: true,
    merged_at: ago(1.8),
    first_commit_at: ago(3),
    merge_commit: "a".repeat(40),
    environments: [],
    ...o,
  };
}

export function deploy(o: Partial<Deploy> = {}): Deploy {
  return {
    app: "kasse-prod",
    project: "kasse",
    environment: "prod",
    revision: "r1",
    at: ago(1),
    phase: "Succeeded",
    duration_s: 40,
    images: ["reg/kasse-api:1.1"],
    kind: "release",
    new_images: ["reg/kasse-api:1.1"],
    ...o,
  };
}

export function leadTime(o: Partial<LeadTime> = {}): LeadTime {
  return {
    repo: "kasse/kasse-api",
    project: "kasse",
    environment: "prod",
    commit_sha: "b".repeat(40),
    authored_at: ago(3),
    first_deployed_at: ago(1),
    lead_time_s: 2 * 86400,
    is_merge: false,
    service_account: false,
    bot: false,
    ...o,
  };
}

export function run(o: Partial<Run> = {}): Run {
  return {
    source: "jenkins",
    pipeline_name: "kasse-api/master",
    branch: "master",
    branch_kind: "main",
    build_type: "build",
    run_id: "1",
    commit_sha: "c".repeat(40),
    status: "SUCCESS",
    started_at: ago(1),
    finished_at: ago(0.99),
    duration_s: 600,
    repo: "kasse/kasse-api",
    project: "kasse",
    image_refs: [],
    ...o,
  };
}

export function review(o: Partial<Review> = {}): Review {
  return {
    pr_key: "kasse/kasse-api#1",
    repo: "kasse/kasse-api",
    project: "kasse",
    outcome: "merged",
    cost_usd: "0.500000",
    lines_added: 400,
    lines_removed: 100,
    files_changed: 5,
    findings_count: 2,
    total_runs: 1,
    closed_at: ago(1),
    ...o,
  };
}

export function dataset(o: Partial<Dataset> = {}): Dataset {
  return {
    meta: {
      team: "team-a",
      from: ago(25),
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
      build_data_from: ago(10),
      build_type_from: ago(10),
    },
    products: [
      {
        key: "kasse",
        repos: ["kasse/kasse-api", "kasse/kasse-gitops"],
        apps: ["kasse-prod"],
      },
      { key: "portal", repos: ["portal/portal-web"], apps: [] },
    ],
    prs: [],
    deploys: [],
    lead_times: [],
    runs: [],
    reviews: [],
    ...o,
  };
}
