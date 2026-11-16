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
  median_week: number | null;
  // The period's weeks day by day, Monday first, 7 per week.
  days: DeployDay[];
  no_data_days: number;
  // Releases per weekday over the days with data, Monday first.
  weekdays: DeployWeekday[];
};

export type DeployWeekday = {
  releases: number;
  days: number;
  per_day: number | null;
};

// One local calendar day's releases; step is the shade, 0 to 4.
export type DeployDay = {
  date: string;
  releases: number;
  step: 0 | 1 | 2 | 3 | 4;
  no_data: boolean;
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
  // PRs per bucket_bounds_s bucket; out-of-range waits in the end buckets.
  // Null when low_sample (durations_s instead).
  histogram: number[] | null;
  // The waits in seconds, ascending, only when low_sample.
  durations_s: number[] | null;
};

export type LeadChain = {
  stages: Stage[];
  pre_environment: string | null;
  attention_share: number;
  min_prs: number;
  before_prod: number;
  // The stage histograms' edges in seconds, log-spaced, ascending.
  bucket_bounds_s: number[];
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
  };
  within_day: Share;
  change_mix: MixWeek[];
  mix_total: MixWeek;
  mix_share: Record<keyof MixWeek, number | null>;
};

export type Share = { count: number; n: number; rate: number | null };

export type RunKind = "release" | "main" | "feature" | "other" | "unknown";

export type BuildStats = {
  kind: RunKind;
  runs: number;
  succeeded: number;
  failed: number;
  no_verdict: number;
  success_rate: number | null;
  duration: Spread;
  waiting: Spread;
  building: Spread;
  ci_seconds: number;
  wasted_seconds: number;
  outlier_runs: number;
  outlier_seconds: number;
  weekly: (number | null)[];
  // Kind other only: its most frequent branch schemes.
  prefixes: BranchPrefix[];
};

// A branch scheme and its runs; prefix "" for a pipeline name without a
// branch.
export type BranchPrefix = { prefix: string; runs: number };

export type FlowOutcome = "succeeded" | "failed" | "no_verdict" | "outlier";

export type FlowKind = { kind: RunKind; seconds: number; share: number };

export type FlowOutcomeShare = {
  outcome: FlowOutcome;
  seconds: number;
  share: number;
};

export type FlowLink = {
  kind: RunKind;
  outcome: FlowOutcome;
  seconds: number;
  share_of_kind: number;
  share: number;
};

export type CIFlow = {
  total_seconds: number;
  kinds: FlowKind[];
  outcomes: FlowOutcomeShare[];
  links: FlowLink[];
};

export type KindHotspots = {
  kind: RunKind;
  duration: Spread;
  failure_rate: number | null;
  repos: RepoHotspot[];
  // The longest repos by median, flagged or not; cut and ordered by the
  // backend.
  slowest: RepoHotspot[];
  outlier_threshold_s: number | null;
  // The kind's repos the chart cannot draw: past the cut, without a run
  // with a verdict, or without a median.
  unplotted: number;
  // A row per repo of `repos`, in its order, a cell per week.
  heatmap: HeatRow[];
};

// One repo's runs of a kind in one week. step: null under heat_min_runs
// runs, 0 without a failure, 1..4 by heat_steps.
export type HeatCell = {
  runs: number;
  failed: number;
  rate: number | null;
  step: number | null;
  // No run in a week before the run data starts: unknown, not none.
  no_data: boolean;
};

// other: the kind's repos past the cut of `repos`, together; repo empty.
export type HeatRow = { repo: string; other: boolean; weeks: HeatCell[] };

// A legend class: a failure rate from `from` to `to`, null where open.
export type HeatClass = {
  step: number;
  from: number | null;
  to: number | null;
};

// One repo against its own runs of the kind in the previous period.
export type RepoHotspot = {
  repo: string;
  runs: number;
  duration: Spread;
  waiting: Spread;
  building: Spread;
  duration_change: Change;
  factor: number | null;
  slow: boolean;
  failures: number;
  failure_rate: number | null;
  failure_change: Change;
  wasted_seconds: number;
  flaky: boolean;
  // Every run's time of the kind, those without a verdict included,
  // outliers aside; the rows add up to at most the kind's.
  ci_seconds: number;
  // Either previous value is there.
  compared: boolean;
  // Both are: a previous position to draw the arrow from.
  prev_complete: boolean;
  // A median and a failure rate: a position to draw the repo at.
  plotted: boolean;
};

export type OutlierGroup = {
  from: string;
  to: string;
  runs: number;
  repos: string[];
  shared: boolean;
};

export type Outlier = {
  kind: RunKind;
  pipeline_name: string;
  repo: string;
  run_id: string;
  started_at: string | null;
  duration_s: number;
  status: string | null;
  factor: number | null;
  wait_s: number | null;
};

export type Hotspots = {
  min_runs: number;
  slow_factor: number;
  flaky_factor: number;
  flaky_min_points: number;
  flaky_min_failures: number;
  outlier_factor: number;
  outlier_min_s: number;
  outlier_repo_min_runs: number;
  slowest_min_runs: number;
  heat_min_runs: number;
  heat_steps: number[];
  heat_classes: HeatClass[];
  kinds: KindHotspots[];
  outliers: Outlier[];
  outlier_count: number;
  outlier_groups: OutlierGroup[];
};

export type Build = {
  data_from: string | null;
  starts_inside: boolean;
  gap_weeks: number;
  kinds: BuildStats[];
  ci_flow: CIFlow;
  // Set when feature builds look unrecognised: other runs on branches that
  // look like work in progress, no feature run.
  feature_hint: { runs: number } | null;
  hotspots: Hotspots;
};

export type Costs = {
  total: number;
  merged: number;
  merged_reviews: number;
  per_kloc: number | null;
  wasted: number;
  wasted_share: number | null;
  promotion: number;
  promotion_reviews: number;
  rows: { label: string; value: number }[];
  reviewed_repos: number;
};

export type PRWaiting =
  "draft" | "first_review" | "review" | "changes" | "merge";

// Inactive: no event for inactive_days, in the PR table only.
export type PRFactWaiting = PRWaiting | "inactive";

export type WaitingCounts = Record<PRWaiting, number>;

export type OpenRepo = {
  repo: string;
  open: number;
  long: number;
  jammed: boolean;
  age: Spread;
  waiting: WaitingCounts;
  // What the long-open ones wait on.
  long_waiting: WaitingCounts;
  inactive: number;
};

// The PRs open at the period's end, whenever opened.
export type OpenPRs = {
  at: string;
  long_days: number;
  jam_min: number;
  inactive_days: number;
  open: number;
  long: number;
  jammed: number;
  jammed_long: number;
  inactive: number;
  age: Spread;
  waiting: WaitingCounts;
  long_waiting: WaitingCounts;
  jammed_waiting: WaitingCounts;
  repos: OpenRepo[];
};

export type Change = {
  prev: number | null;
  verdict: "better" | "worse" | "same" | "none";
  reason:
    | "low_sample"
    | "incomplete"
    | "no_value"
    | "undirected"
    | "not_reporting"
    | null;
  // A better direction exists, whatever the verdict.
  directed: boolean;
  // -1..1, positive better: log2(now / previous), negated where lower is
  // better, clamped at halving / doubling; null without a verdict.
  score: number | null;
  // How far it moved: notable past 25 % (5 points for a share).
  magnitude: "same" | "minor" | "notable" | null;
};

// A kind of run against the previous period: its success rate, and how
// many of its repos got slower or flakier against their own.
export type KindChange = {
  kind: RunKind;
  success: Change;
  slower: number;
  flakier: number;
  compared: number;
};

export type Comparison = {
  from: string;
  to: string;
  deploy_frequency: Change;
  deploy_success: Change;
  lead_time: Change;
  lead_time_pre: Change | null;
  review_time: Change;
  within_day: Change;
  first_touch: Change;
  untracked: Change;
  cost_per_kloc: Change;
  wasted_share: Change;
  build: KindChange[];
  open_prs: Change;
  open_jammed: Change;
};

export type Metrics = {
  meta: Meta;
  period: Period;
  products: Product[];
  product: string | null;
  delivery: Delivery;
  lead_chain: LeadChain;
  review: Review;
  open_prs: OpenPRs;
  build: Build;
  costs: Costs;
  comparison: Comparison;
};

// A Jira key; url is null when the config sets no jira.base_url.
export type JiraRef = { key: string; url: string | null };

// A piece of a PR title: plain text, or a Jira key.
export type TitlePart = { text: string; jira: JiraRef | null };

export type PRFact = {
  repo: string;
  pr_id: number;
  title: string | null;
  // The title split at its Jira keys; empty without a title.
  title_parts: TitlePart[];
  // The keys the title does not show, three at most, and how many more.
  jira_prefix: JiraRef[];
  jira_more: number;
  gitops: boolean;
  change_type: string | null;
  state: "open" | "merged" | "declined" | "deleted";
  review_s: number | null;
  cycle_s: number | null;
  merged_at: string | null;
  prod_at: string | null;
  to_prod_s: number | null;
  // A PR open at the period's end: its age then, what it waits on.
  age_s: number | null;
  waiting: PRFactWaiting | null;
  idle_s: number | null;
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
  // The status when it plainly says so, worded by the UI; null shows the
  // CI's own word.
  outcome: "succeeded" | "failed" | null;
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

// What the page shows: whose data, how many calendar weeks, which product.
export type Query = {
  team: string;
  weeks: number;
  product: string | null;
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
  return q;
}

export function fetchMetrics(v: Query, signal?: AbortSignal): Promise<Metrics> {
  return getJSON<Metrics>(`/api/metrics?${params(v)}`, signal);
}

// ---- The overview's assessment and its LLM wording (/api/analysis) -------

export type FactUnit =
  "seconds" | "share" | "count" | "per_week" | "weeks" | "usd" | "days";

// One number of a signal, judged by the backend against the previous
// period; text refers to it as {{id}}.
export type Fact = {
  id: string;
  label: string;
  unit: FactUnit;
  value: number | null;
  prev: number | null;
  verdict: Change["verdict"];
  magnitude: Change["magnitude"];
};

export type SignalKind =
  | "data_gap"
  | "pr_jam"
  | "slow_stage"
  | "slower_build"
  | "steady_slow"
  | "flaky"
  | "change"
  | "bottleneck_moved"
  | "throughput_instability"
  | "agent_wait"
  | "shared_stall"
  | "inactive_prs"
  | "resync_share"
  | "resolved"
  | "question";

export type SignalTone = "gate" | "attention" | "hint" | "good" | "talk";

export type Driver = {
  repo: string;
  share: number;
  value: number | null;
  prev: number | null;
};

export type Signal = {
  id: string;
  kind: SignalKind;
  tone: SignalTone;
  source: string;
  repo: string | null;
  run_kind: RunKind | null;
  metric: string | null;
  stage: StageKey | null;
  prev_stage: StageKey | null;
  low_sample: boolean;
  // The cases it rests on, where counted (a stage's PRs).
  sample: number | null;
  weight: number;
  seen: number;
  facts: Fact[];
  waiting: WaitingCounts | null;
  drivers: Driver[] | null;
};

// A team-wide signal as the page shows it: its column and lever are the
// backend's.
export type LayoutCard = {
  signal: string;
  column: "good" | "bad" | "done";
  lever: string;
};

export type FocusRepo = {
  repo: string;
  weight: number;
  seen: number;
  signals: string[];
  facts: Fact[];
  waiting: WaitingCounts | null;
};

export type CISplit = {
  seconds: number;
  wasted: number;
  change: Change;
  repos: { repo: string; seconds: number; wasted: number }[];
  other_repos: number;
  other_seconds: number;
  other_wasted: number;
};

export type Assessment = {
  period: Period;
  previous: Period;
  gaps: string[];
  signals: Signal[];
  focus: FocusRepo[];
  active: number;
  calm: number;
  team: Fact[];
  question: Signal | null;
  ci: CISplit;
  // An assessment of the view for an earlier week was there.
  compared: boolean;
};

export type FocusText = {
  repo: string;
  take: string;
  retro: string | null;
  lever: string;
};

export type CardText = {
  signal: string;
  take: string;
  retro: string | null;
  lever: string;
};

export type AnalysisText = {
  headline: string;
  trend: string;
  focus: FocusText[];
  cards: CardText[];
  ci: string;
  question: string;
};

export type ShownAnalysis = {
  id: number;
  created_at: string;
  finished_at: string;
  trigger: "scheduled" | "manual";
  model: string;
  cost_usd: string | null;
  weeks_old: number;
  text: { de: AnalysisText; en: AnalysisText };
  report: { dropped: string[]; missed: string[] };
};

export type AnalysisStatus = {
  state: "fresh" | "older" | "none" | "off";
  enabled: boolean;
  running: boolean;
  assessment: Assessment;
  // What the page shows where, chosen by the backend.
  layout: {
    focus: { repo: string; lever: string; tab: "prs" | "runs" }[];
    cards: LayoutCard[];
    also: LayoutCard[];
  };
  analysis: ShownAnalysis | null;
  failure: { at: string; detail: string } | null;
  next_run: string | null;
  cooldown_until: string | null;
  // The zone the closed weeks are cut in, their days shown there; empty:
  // the viewer's.
  zone: string;
};

export function fetchAnalysis(
  v: Query,
  signal?: AbortSignal,
): Promise<AnalysisStatus> {
  return getJSON<AnalysisStatus>(`/api/analysis?${params(v)}`, signal);
}

// Starts an analysis of the view; the backend answers 202 with the status,
// 409 when one is under way, was just asked for, or no model is set.
export async function askAnalysis(v: Query): Promise<AnalysisStatus> {
  const res = await fetch(`/api/analysis?${params(v)}`, {
    method: "POST",
    // The backend refuses a POST without it: a cross-site form cannot set it.
    headers: { Accept: "application/json", "X-Riptide-Ask": "1" },
  });
  if (!res.ok) {
    let detail = res.statusText || `HTTP ${res.status}`;
    try {
      const body = (await res.json()) as { detail?: unknown };
      if (typeof body.detail === "string") detail = body.detail;
    } catch {
      // not JSON
    }
    throw new ApiError(res.status, detail);
  }
  return (await res.json()) as AnalysisStatus;
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
