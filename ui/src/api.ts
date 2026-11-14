// The read API's wire types, as backend/api/openapi.yaml describes them.
// Timestamps stay ISO strings here; the metrics parse what they use.

export type SourceTimes = {
  bitbucket: string | null;
  pipeline: string | null;
  argocd: string | null;
  noergler: string | null;
};

export type Meta = {
  team: string;
  from: string;
  to: string;
  generated_at: string;
  production_stage: string;
  data_start: SourceTimes;
  data_end: SourceTimes;
  build_data_from: string | null;
  build_type_from: string | null;
};

export type Product = { key: string; repos: string[]; apps: string[] };

export type PR = {
  repo: string;
  project: string;
  pr_id: number;
  title: string | null;
  gitops: boolean;
  bot: boolean;
  change_type: string | null;
  jira_keys: string[];
  state: "open" | "merged" | "declined" | "deleted";
  opened_at: string | null;
  ready_at: string | null;
  first_review_at: string | null;
  approved_at: string | null;
  approved_on_first_touch: boolean;
  merged_at: string | null;
  first_commit_at: string | null;
  merge_commit: string | null;
  environments: { environment: string; first_deployed_at: string }[];
};

export type DeployKind = "release" | "sync" | "first_seen";

export type Deploy = {
  app: string;
  project: string | null;
  environment: string | null;
  revision: string;
  at: string;
  phase: string;
  duration_s: number | null;
  images: string[];
  kind: DeployKind | null;
  new_images: string[];
};

export type LeadTime = {
  repo: string;
  project: string;
  environment: string;
  commit_sha: string;
  authored_at: string;
  first_deployed_at: string;
  lead_time_s: number;
  is_merge: boolean;
  service_account: boolean;
  bot: boolean;
};

export type Run = {
  source: string;
  pipeline_name: string;
  branch: string;
  branch_kind: "main" | "release" | "feature" | "pr" | "other";
  build_type: string | null;
  run_id: string;
  commit_sha: string | null;
  status: string | null;
  started_at: string | null;
  finished_at: string | null;
  duration_s: number | null;
  repo: string | null;
  project: string | null;
  image_refs: string[];
};

export type Review = {
  pr_key: string;
  repo: string | null;
  project: string | null;
  outcome: string | null;
  cost_usd: string | null;
  lines_added: number | null;
  lines_removed: number | null;
  files_changed: number | null;
  findings_count: number | null;
  total_runs: number | null;
  closed_at: string;
};

export type Dataset = {
  meta: Meta;
  products: Product[];
  prs: PR[];
  deploys: Deploy[];
  lead_times: LeadTime[];
  runs: Run[];
  reviews: Review[];
};

// ApiError carries the server's {"detail": ...} message.
export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, {
    signal,
    headers: { Accept: "application/json" },
  });
  if (!res.ok) {
    let detail = res.statusText || `HTTP ${res.status}`;
    try {
      const body = (await res.json()) as { detail?: unknown };
      if (typeof body.detail === "string") detail = body.detail;
    } catch {
      // not JSON: keep the status text (an auth proxy's HTML page, say)
    }
    throw new ApiError(res.status, detail);
  }
  return (await res.json()) as T;
}

export function fetchTeams(signal?: AbortSignal): Promise<string[]> {
  return getJSON<{ teams: string[] }>("/api/teams", signal).then(
    (b) => b.teams,
  );
}

export function fetchDataset(
  team: string,
  from: Date,
  to: Date,
  signal?: AbortSignal,
): Promise<Dataset> {
  const q = new URLSearchParams({
    team,
    from: from.toISOString(),
    to: to.toISOString(),
  });
  return getJSON<Dataset>(`/api/dataset?${q}`, signal);
}
