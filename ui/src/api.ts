// The read API's wire types, as backend/api/openapi.yaml describes them.
// Every number is computed by the backend; the UI renders and formats.
// Timestamps are ISO strings, durations seconds.

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

export type Week = { start: string; end: string; kw: number };

export type Period = { from: string; to: string; weeks: Week[] };

export type Spread = { n: number; p50: number | null; p90: number | null };

export type DeployWeek = { release: number; sync: number; first_seen: number };

export type DeployFrequency = {
  releases: number;
  syncs: number;
  first_seen: number;
  resyncs: number;
  per_week: number;
  low_sample: boolean;
  since: string | null;
  weeks: DeployWeek[];
  succeeded: number;
  failed: number;
  finished: number;
  success_rate: number | null;
  argo_reporting: boolean;
};

export type LeadTimes = {
  environment: string;
  human: Spread;
  bot: Spread;
  low_sample: boolean;
};

export type Delivery = {
  production_stage: string;
  pre_environment: string | null;
  frequency: DeployFrequency;
  lead_time: LeadTimes;
  lead_time_pre: LeadTimes | null;
};

export type StageKey = "coding" | "review" | "merge" | "to_pre" | "to_prod";

export type Stage = Spread & {
  key: StageKey;
  low_sample: boolean;
  share: number | null;
  hot: boolean;
};

export type LeadChain = {
  stages: Stage[];
  pre_environment: string | null;
  sum_p50: number | null;
  before_prod: number;
};

export type MixWeek = {
  feature: number;
  bugfix: number;
  hotfix: number;
  other: number;
};

export type Review = {
  cycle: Spread;
  review: Spread;
  approved: number;
  approved_on_first_touch: number;
  first_touch_rate: number | null;
  merged: number;
  untracked: number;
  untracked_rate: number | null;
  cycle_histogram: {
    upper_bounds_s: number[];
    human: number[];
    bot: number[];
  };
  change_mix: MixWeek[];
};

export type RunKind = "release" | "main" | "feature" | "other" | "unknown";

export type BuildStats = {
  kind: RunKind;
  runs: number;
  succeeded: number;
  failed: number;
  no_verdict: number;
  success_rate: number | null;
  duration: Spread;
  ci_seconds: number;
  wasted_seconds: number;
  weekly: (number | null)[];
};

export type RepoDuration = {
  repo: string;
  runs: number;
  duration: Spread;
  factor: number | null;
  slow: boolean;
};

export type RepoFailures = {
  repo: string;
  runs: number;
  failures: number;
  failure_rate: number | null;
  wasted_seconds: number;
  flaky: boolean;
};

export type KindHotspots = {
  kind: RunKind;
  duration: Spread;
  failure_rate: number | null;
  durations: RepoDuration[];
  failing: RepoFailures[];
  outlier_threshold_s: number | null;
};

export type Outlier = {
  kind: RunKind;
  pipeline_name: string;
  repo: string;
  run_id: string;
  started_at: string | null;
  duration_s: number;
  status: string | null;
  factor: number;
};

export type Hotspots = {
  min_runs: number;
  slow_factor: number;
  flaky_factor: number;
  outlier_factor: number;
  kinds: KindHotspots[];
  outliers: Outlier[];
};

export type Build = {
  data_from: string | null;
  starts_inside: boolean;
  gap_weeks: number;
  kinds: BuildStats[];
  hotspots: Hotspots;
};

export type Costs = {
  total: number;
  merged: number;
  merged_reviews: number;
  per_kloc: number | null;
  wasted: number;
  rows: { label: string; value: number }[];
  reviewed_repos: number;
};

export type Metrics = {
  meta: Meta;
  period: Period;
  products: Product[];
  product: string | null;
  bots: boolean;
  delivery: Delivery;
  lead_chain: LeadChain;
  review: Review;
  build: Build;
  costs: Costs;
};

export type PRFact = {
  repo: string;
  pr_id: number;
  title: string | null;
  jira_prefix: string;
  gitops: boolean;
  bot: boolean;
  change_type: string | null;
  state: "open" | "merged" | "declined" | "deleted";
  review_s: number | null;
  cycle_s: number | null;
  merged_at: string | null;
  prod_at: string | null;
  to_prod_s: number | null;
};

export type DeployFact = {
  at: string;
  app: string;
  environment: string | null;
  revision: string;
  new_images: string[];
  class: "failed" | "release" | "first_seen" | "sync";
  phase: string;
};

export type RunFact = {
  pipeline_name: string;
  run_id: string;
  kind: RunKind;
  commit_sha: string | null;
  duration_s: number | null;
  image_refs: string[];
  status: string | null;
  verdict: "success" | "failure" | "none";
};

export type Table<T> = { count: number; rows: T[] };

export type Facts = {
  production_stage: string;
  limit: number;
  prs: Table<PRFact>;
  deploys: Table<DeployFact>;
  runs: Table<RunFact>;
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

// What the page shows: whose data, how many calendar weeks, which product,
// bots or not.
export type Query = {
  team: string;
  weeks: number;
  product: string | null;
  bots: boolean;
};

// The viewer's time zone: the backend cuts weeks at Monday 00:00 there.
export function viewerZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

function params(v: Query): URLSearchParams {
  const q = new URLSearchParams({
    team: v.team,
    weeks: String(v.weeks),
    tz: viewerZone(),
  });
  if (v.product) q.set("product", v.product);
  if (v.bots) q.set("bots", "1");
  return q;
}

export function fetchMetrics(v: Query, signal?: AbortSignal): Promise<Metrics> {
  return getJSON<Metrics>(`/api/metrics?${params(v)}`, signal);
}

export function fetchFacts(
  v: Query,
  search: string,
  signal?: AbortSignal,
): Promise<Facts> {
  const q = params(v);
  if (search.trim()) q.set("q", search.trim());
  return getJSON<Facts>(`/api/facts?${q}`, signal);
}
