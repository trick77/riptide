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
  // Spans of two working days or more overlapping the window in which one
  // source stored nothing while another did, oldest first. Only `lost`
  // tells lost events from a pause.
  silences: Silence[];
  // The silences whose events were lost, the ones the page names: the
  // backend's choice.
  lost: Silence[];
  // The AI reviewer's first event per repository; empty for the facts read.
  repo_starts: { repo: string; noergler: string | null }[];
};

export type Source = "bitbucket" | "pipeline" | "argocd" | "noergler";

export type Silence = {
  source: Source;
  from: string;
  to: string;
  unseen_runs: number;
  // Bitbucket's events were lost: the backend's word, the page names it.
  lost: boolean;
  // Still silent at the read: `to` is the read's now, no end.
  ongoing: boolean;
};

export type Product = { key: string; repos: string[]; apps: string[] };

export type Week = { start: string; end: string; kw: number };

// last_day: the last calendar day before `to`, YYYY-MM-DD, the backend's.
export type Period = {
  from: string;
  to: string;
  last_day: string;
  weeks: Week[];
};

export type Spread = { n: number; p50: number | null; p90: number | null };

export type DeployWeek = {
  release: number;
  // Of release, an App's first deploy.
  first_release: number;
  sync: number;
  first_seen: number;
};

// Infrastructure Apps are in none of the numbers but infra and infra_apps.
export type DeployFrequency = {
  releases: number;
  // Of releases, an App's first deploy past the onboarding's grace.
  first_releases: number;
  // Of releases, those no PR could be traced to: what they shipped no one
  // can name.
  releases_unresolved: number;
  syncs: number;
  // First deploys within the onboarding's grace: release or re-sync unknown.
  first_seen: number;
  // The onboarding's grace in days, for the words.
  grace_days: number;
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
  // Team-wide in every view: successful deploys of Apps without a known
  // product, infrastructure Apps aside; and those of infrastructure Apps.
  unattributed: number;
  // A product view without deploys of its own beside unattributed ones:
  // the page says they may be its, not "no deploys".
  maybe_unattributed: boolean;
  infra: number;
  infra_apps: string[];
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

// Per PR: the people's PRs merged in the period on the environment by its
// end, from the first commit; human.n is how many.
export type LeadTimes = {
  environment: string;
  human: Spread;
  // Of human, placed through a lower bound: they may have gone out earlier.
  lower_bound: number;
  low_sample: boolean;
};

// Why tracing cannot place a merged PR, the first reason that applies.
export type TraceReason =
  "no_merge_commit" | "release_unresolved" | "repo_untraced" | "in_silence";

// How merged PRs stood on production at the period's end:
// merged = on_prod + not_yet + not_placeable; library PRs are outside merged.
export type TraceCounts = {
  merged: number;
  on_prod: number;
  lower_bound: number;
  not_yet: number;
  oldest_not_yet: string | null;
  not_placeable: number;
  reasons: Record<TraceReason, number>;
  library: number;
};

export type Coverage = TraceCounts & {
  repos: (TraceCounts & { repo: string; project: string })[];
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
  min_prs: number;
  before_prod: number;
  // Some PR has a production wait; false: no shares, no hot stage.
  reaches_prod: boolean;
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
  // Failed runs a rerun passed on the same commit; their rate over the
  // runs with a commit, their share of the failed ones, their part of
  // wasted_seconds. Unjudged runs have no commit to tell.
  flaky_runs: number;
  flaky_rate: number | null;
  flaky_of_failed: number | null;
  flaky_seconds: number;
  unjudged: number;
  outlier_runs: number;
  outlier_seconds: number;
  weekly: (number | null)[];
  // Kind other only: its most frequent branch schemes.
  prefixes: BranchPrefix[];
};

// A branch scheme and its runs; prefix "" for a pipeline name without a
// branch.
export type BranchPrefix = { prefix: string; runs: number };

export type FlowOutcome =
  "succeeded" | "flaky" | "failed" | "no_verdict" | "outlier";

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
  // The top repos across kinds, outliers included, the rest folded: with
  // it they sum to total_seconds, their failed to the failed outcome.
  repos: { repo: string; seconds: number; failed: number }[];
  other_repos: number;
  other_seconds: number;
  other_failed: number;
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
  failing_more: boolean;
  flaky_runs: number;
  flaky_seconds: number;
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
  failing_more_factor: number;
  failing_more_min_points: number;
  failing_more_min_failures: number;
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
  // Team-wide: runs with a status and no repository, in no product's numbers.
  unattributed: number;
  // Runs of bots' branches: in no other build number, their count and CI
  // time apart.
  bot_runs: {
    runs: number;
    ci_seconds: number;
    failed: number;
    failed_seconds: number;
  };
  // The agent-wait hint's rule: the share of the typical build spent
  // waiting, and the least wait in seconds.
  agent_wait_share: number;
  agent_wait_min_s: number;
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

// The PRs open now, whenever opened: the page's one number of now.
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
  // PRs whose close may have been lost with Bitbucket's events: in no other
  // count.
  unknown: number;
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
  // How far it moved, better and worse alike: notable past 25 % (5 points
  // for a share; a count damped and moved by at least 2; few cases or
  // releases gated as the backend's caseGate and releaseGate).
  magnitude: "same" | "minor" | "notable" | null;
};

// A kind of run against the previous period: its success rate, and how
// many of its repos got slower or fail more against their own.
export type KindChange = {
  kind: RunKind;
  success: Change;
  // The flaky rate against the previous period's, lower better.
  flaky: Change;
  slower: number;
  failing_more: number;
  compared: number;
};

export type Comparison = {
  from: string;
  to: string;
  last_day: string;
  deploy_frequency: Change;
  deploy_success: Change;
  lead_time: Change;
  lead_time_pre: Change | null;
  review_time: Change;
  within_day: Change;
  first_touch: Change;
  untracked: Change;
  merged_per_week: Change;
  batch_size: Change;
  cost_per_kloc: Change;
  wasted_share: Change;
  build: KindChange[];
  open_prs: Change;
  open_jammed: Change;
  // Some number has a previous value; false: nothing was compared, the
  // page names no comparison.
  any: boolean;
};

// The people's merged PRs per week and their size (changed lines, added
// plus removed); stage promotions and bots are in neither.
export type Throughput = {
  merged: number;
  merged_per_week: number | null;
  // When Bitbucket's data starts inside the period; per week is since then,
  // null when that is under a week.
  since: string | null;
  // People's merged PRs left out of every PR number, by first reason.
  umbrella: number;
  housekeeping: number;
  twice: number;
  // The housekeeping rule's numbers, for the words.
  housekeeping_repos: number;
  housekeeping_window_s: number;
  // Of merged, the library repos' PRs: never rolled out, out of the base
  // of coverage.to_prod.
  library: number;
  coverage: PRCoverage;
  batch_size: Spread;
  batch_files: Spread;
  weeks: { merged: number; sized: number; batch_p50: number | null }[];
};

// Of the people's merged PRs (n each), those a number can read: traced to
// production, with a size, seen opened.
export type PRCoverage = { to_prod: Share; sized: Share; opened: Share };

export type Metrics = {
  meta: Meta;
  period: Period;
  products: Product[];
  product: string | null;
  delivery: Delivery;
  lead_chain: LeadChain;
  // How the period's merged PRs reached production by its end, per repo too.
  coverage: Coverage;
  review: Review;
  throughput: Throughput;
  open_prs: OpenPRs;
  build: Build;
  costs: Costs;
  comparison: Comparison;
  running_week: RunningWeek;
};

// The week running now so far, counted in no other number of the page.
export type RunningWeek = {
  week: Week;
  // Now, where the counts end.
  to: string;
  releases: number;
  merged: number;
  runs: number;
};

// A Jira key; url is null when the config sets no jira.base_url.
export type JiraRef = { key: string; url: string | null };

// A piece of a PR title: plain text, or a Jira key.
export type TitlePart = { text: string; jira: JiraRef | null };

export type PRFact = {
  repo: string;
  pr_id: number;
  // The PR in Bitbucket; null for a deleted PR or none sent.
  url: string | null;
  title: string | null;
  // The title split at its Jira keys; empty without a title.
  title_parts: TitlePart[];
  // The keys the title does not show, three at most, and how many more.
  jira_prefix: JiraRef[];
  jira_more: number;
  gitops: boolean;
  change_type: string | null;
  // unknown: open at the end, its close maybe lost with Bitbucket's events.
  state: "open" | "merged" | "declined" | "deleted" | "unknown";
  // Why a merged PR is in no PR number; null when it counts.
  not_counted: "umbrella" | "housekeeping" | "twice" | null;
  // A counted merged PR that went into a feature or umbrella branch, or
  // that an umbrella's merge brought in.
  via_umbrella: boolean;
  // A merged PR of a library repo: never rolled out.
  library: boolean;
  review_s: number | null;
  cycle_s: number | null;
  merged_at: string | null;
  prod_at: string | null;
  to_prod_s: number | null;
  // Where a merged PR stands on production now; null for every other PR, a
  // library's included. The reason when not placeable; the route of its
  // first production deploy, and whether that was a lower bound.
  prod_state: "on_prod" | "not_yet" | "not_placeable" | null;
  prod_reason: TraceReason | null;
  prod_route: "compare" | "image" | null;
  prod_lower_bound: boolean;
  // A PR open now: its age, what it waits on.
  age_s: number | null;
  waiting: PRFactWaiting | null;
  idle_s: number | null;
  // Merged or closed, else opened, in the running week: counted in no number.
  running: boolean;
};

export type DeployFact = {
  at: string;
  app: string;
  environment: string | null;
  revision: string;
  new_images: string[];
  class: "failed" | "release" | "first_seen" | "sync";
  phase: string;
  // The App's first known deploy, whether a release or first_seen.
  first_deploy: boolean;
  // An infrastructure App's deploy: listed, counted nowhere.
  infra: boolean;
  // Synced in the running week: counted in no number.
  running: boolean;
};

export type RunFact = {
  pipeline_name: string;
  run_id: string;
  kind: RunKind;
  commit_sha: string | null;
  // The commit to show, the backend's pick: a PR build's own head where
  // commit_sha is a merge the CI made of it, else commit_sha.
  shown_sha: string | null;
  duration_s: number | null;
  image_refs: string[];
  status: string | null;
  verdict: "success" | "failure" | "none";
  // The status when it plainly says so, worded by the UI; null shows the
  // CI's own word.
  outcome: "succeeded" | "failed" | null;
  // A stage promotion's check: listed faded, counted in no build number.
  gitops: boolean;
  // Failed, and a rerun passed on the same commit.
  flaky: boolean;
  // Run in the running week: counted in no number.
  running: boolean;
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

// readJSON reads a response's body, or throws its {"detail": ...} message.
// A 2xx that is not JSON (a sign-in proxy's HTML page) is an ApiError too,
// not a raw SyntaxError; an aborted read stays an AbortError.
async function readJSON<T>(res: Response): Promise<T> {
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
  try {
    return (await res.json()) as T;
  } catch (e) {
    if (e instanceof SyntaxError)
      throw new ApiError(res.status, "answer is not JSON (a sign-in page?)");
    throw e;
  }
}

async function getJSON<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, {
    signal,
    headers: { Accept: "application/json" },
  });
  return readJSON<T>(res);
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
  | "slower_build"
  | "failing_more"
  | "change"
  | "agent_wait"
  | "resolved"
  | "question"
  | "state";

// limit: a data-honesty state, what the data cannot judge; no finding.
export type SignalTone =
  "gate" | "attention" | "hint" | "limit" | "good" | "talk";

// A state's key; one the page does not know yet still renders, untitled.
export type StateKey =
  | "no_release"
  | "release_due"
  | "not_on_prod"
  | "slow_to_prod"
  | "merge_pause"
  | "approved_waiting"
  | "bot_loop"
  | "untraced"
  | "unattributed"
  | "no_prod_stage"
  | "unmeasurable"
  | "deploy_failing"
  | "deploy_failures"
  | "first_review_wait"
  | "trunk_failing"
  | "branch_failing"
  | "flaky_builds"
  | "long_builds"
  | "release_failing"
  | "builds_silent"
  | "big_prs"
  | "fast_approval"
  | "unapproved"
  | "late_decline"
  | "branch_life"
  | "branch_open"
  | "branch_silent";

// One unit a state names: its number in the threshold's unit, the cases it
// rests on, since when.
export type StateUnit = {
  name: string;
  project: string | null;
  value: number;
  count: number | null;
  of: number | null;
  typical: number | null;
  since: string | null;
  lower_bound: boolean;
  week: Week | null;
  names: string[];
  held: boolean;
  reason: TraceReason | null;
  ci_seconds: number | null;
  // The facts search listing the unit's rows in the card's facts table;
  // null where none finds them.
  query: string | null;
  // What value measured where a state judges two ways (first_review_wait).
  measure: "oldest" | "typical" | null;
};

// A state's card: k of n units, the worst three, since when, the team's
// threshold. Every count, the ranking and the worst three are the
// backend's.
export type StateCard = {
  key: StateKey;
  unit: "project" | "repo" | "app" | "branch" | "team";
  count: number;
  judged: number;
  unjudged: number;
  idle: number;
  worst: StateUnit[];
  more: number;
  units: StateUnit[];
  since: string | null;
  since_lower_bound: boolean;
  threshold: {
    key: string;
    value: number;
    unit: "days" | "working_days" | "count" | "share" | "minutes";
    min_key: string | null;
    min: number | null;
  };
  at: string;
  was: string | null;
  facts: "prs" | "deploys" | "runs" | "";
};

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
  weight: number;
  seen: number;
  facts: Fact[];
  waiting: WaitingCounts | null;
  drivers: Driver[] | null;
  // The hero number week by week, where the backend counts it anyway
  // (deploy frequency, build success); absent otherwise and in an analysis
  // stored before the field.
  series?: { prev: (number | null)[]; cur: (number | null)[] };
  // A state's card (kind state); null otherwise, absent in older rows.
  state?: StateCard | null;
};

// A team-wide signal as the page shows it: its column and lever are the
// backend's. bad is red, hint a neutral group, limit what could not be
// judged.
export type LayoutCard = {
  signal: string;
  column: "good" | "bad" | "done" | "hint" | "limit";
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

export type Assessment = {
  period: Period;
  previous: Period;
  signals: Signal[];
  focus: FocusRepo[];
  // Active repos: calm (judged, no signal), unjudged (too little data) and
  // those with a signal; idle were active in the previous period only;
  // repos = active + idle.
  active: number;
  calm: number;
  unjudged: number;
  idle: number;
  repos: number;
  team: Fact[];
  // Null unless an undirected number moved notably.
  question: Signal | null;
  // An assessment of the view for an earlier week was there.
  compared: boolean;
  // Nothing stands out and the period could be measured and compared: the
  // one case the page says so. Else the reasons, most important first.
  quiet: boolean;
  quiet_reasons: QuietReason[];
  // Why not measurable, the backend's; null unless quiet_reasons holds
  // not_measurable.
  not_measurable: NotMeasurable | null;
  // A reason other than findings: no calm word, no 0 for what is stuck.
  limited: boolean;
  // No number had a previous value: no comparison at all.
  nothing_compared: boolean;
  // Every merged PR is a library's: nothing to roll out. No limit.
  libraries_only: boolean;
  // The period's merged PRs in library repos, out of coverage.to_prod.
  library: number;
  limits: Limit[];
  coverage: PRCoverage;
};

export type QuietReason =
  "no_baseline" | "source_gap" | "not_measurable" | "unjudged" | "findings";

export type NotMeasurable =
  "bitbucket" | "argocd" | "no_merged" | "no_release" | "to_prod";

// A hole in a source's data the comparison crosses. from: the last event
// before it (the first ever for starts); to: the first after a silence, the
// period's end for silent_end, null for starts and for a silence still
// running at the read.
export type Limit = {
  source: Source;
  kind: "silent_end" | "silence" | "starts";
  from: string;
  to: string | null;
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
  // The newest analysis was assessed under older rules: not shown, the
  // assessment is the current rules' view; the page offers to assess again.
  outdated: boolean;
  assessment: Assessment;
  // What the page shows where, chosen by the backend.
  layout: {
    focus: { repo: string; lever: string; tab: "prs" | "runs" }[];
    cards: LayoutCard[];
    also: LayoutCard[];
    // The signals by column, gates aside, and the question (talk, 0 or 1).
    tally: {
      good: number;
      done: number;
      bad: number;
      hint: number;
      limit: number;
      talk: number;
    };
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
  return readJSON<AnalysisStatus>(res);
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
