// Every number the page shows, computed from the dataset in the browser.
// Each function reapplies its README query's filters: the API serves
// supersets with flags (GitOps PRs, bots, PRs opened before the window) and
// leaves the dropping to the reader. Pure, so every rule has a test.

import type { Dataset, Deploy, LeadTime, PR, Review, Run } from "./api";
import { type Period, inPeriod, weekIndex } from "./period";
import { between, median, percentile, sum } from "./stats";

export type Filter = {
  // A Bitbucket project key, or null for all.
  product: string | null;
  // Bots' PRs and commits counted in (on their own series where a chart
  // has one).
  bots: boolean;
};

export function inProduct(project: string | null, f: Filter): boolean {
  return f.product == null || project === f.product;
}

export type Spread = { n: number; p50: number | null; p90: number | null };

export function spread(values: number[]): Spread {
  return {
    n: values.length,
    p50: median(values),
    p90: percentile(values, 0.9),
  };
}

// ---- Deploys ---------------------------------------------------------------

export type DeployWeek = { release: number; sync: number; firstSeen: number };

export type DeployFrequency = {
  environment: string;
  // Distinct GitOps revisions over the window: a release reconciles every
  // App sharing its revision (intranet + extranet), so it counts once.
  releases: number;
  syncs: number;
  firstSeen: number;
  // Releases per week over the window's length, or over the part of it
  // since Argo CD started reporting.
  perWeek: number;
  // When Argo CD's data starts inside the window; null when it covers it.
  since: string | null;
  weeks: DeployWeek[];
  succeeded: number;
  failed: number;
};

// A revision's class, once per revision: a release if it brought new
// images to any App, else a first deploy if it was some App's first, else
// a re-sync. The stack of the three adds up to the syncs.
type RevisionClass = keyof DeployWeek;

function classify(deploys: Deploy[]): Map<string, RevisionClass> {
  const out = new Map<string, RevisionClass>();
  const rank: Record<RevisionClass, number> = {
    release: 3,
    firstSeen: 2,
    sync: 1,
  };
  for (const d of deploys) {
    const c: RevisionClass =
      d.kind === "release"
        ? "release"
        : d.kind === "first_seen"
          ? "firstSeen"
          : "sync";
    const prev = out.get(d.revision);
    if (!prev || rank[c] > rank[prev]) out.set(d.revision, c);
  }
  return out;
}

function count(classes: Map<string, RevisionClass>, c: RevisionClass): number {
  let n = 0;
  for (const v of classes.values()) if (v === c) n++;
  return n;
}

export function deployFrequency(
  ds: Dataset,
  p: Period,
  f: Filter,
  environment: string,
): DeployFrequency {
  const env = ds.deploys.filter(
    (d) =>
      d.environment === environment &&
      inProduct(d.project, f) &&
      inPeriod(p, d.at),
  );
  const ok = env.filter((d) => d.phase === "Succeeded");
  const all = classify(ok);
  // Each revision in the week of its first deploy in the window, with its
  // class over the window: the bars add up to the totals, a release rolled
  // out across a week boundary counts once.
  const firstAt = new Map<string, string>();
  for (const d of ok) {
    const t = firstAt.get(d.revision);
    if (t == null || Date.parse(d.at) < Date.parse(t))
      firstAt.set(d.revision, d.at);
  }
  const weeks: DeployWeek[] = p.weeks.map(() => ({
    release: 0,
    sync: 0,
    firstSeen: 0,
  }));
  for (const [rev, at] of firstAt) weeks[weekIndex(p, at)][all.get(rev)!]++;
  const releases = count(all, "release");
  // Argo CD started reporting inside the window: the rate is over the weeks
  // it reported, not the empty ones before.
  const start = ds.meta.data_start.argocd;
  const since =
    start != null && Date.parse(start) > p.from.getTime() ? start : null;
  const span = since
    ? (p.to.getTime() - Date.parse(since)) / (7 * 86_400_000)
    : p.span;
  return {
    environment,
    releases,
    syncs: all.size,
    firstSeen: count(all, "firstSeen"),
    perWeek: span > 0 ? releases / span : 0,
    since,
    weeks,
    succeeded: ok.length,
    failed: env.filter((d) => d.phase === "Failed").length,
  };
}

// Rows per week of the period, in one pass; rows outside it are dropped.
function bucket<T>(p: Period, rows: T[], at: (r: T) => string | null): T[][] {
  const out: T[][] = p.weeks.map(() => []);
  for (const r of rows) {
    const t = at(r);
    const i = t == null ? -1 : weekIndex(p, t);
    if (i >= 0) out[i].push(r);
  }
  return out;
}

// The environments the product's deploys went to, the production stage
// last, the rest by how often they were deployed to.
export function environments(ds: Dataset, f: Filter): string[] {
  const n = new Map<string, number>();
  for (const d of ds.deploys)
    if (d.environment && inProduct(d.project, f))
      n.set(d.environment, (n.get(d.environment) ?? 0) + 1);
  const prod = ds.meta.production_stage;
  return [...n.keys()]
    .filter((e) => e !== prod)
    .sort((a, b) => (n.get(b) ?? 0) - (n.get(a) ?? 0) || a.localeCompare(b))
    .concat(n.has(prod) ? [prod] : []);
}

// ---- Lead time -------------------------------------------------------------

export type LeadTimes = { environment: string; human: Spread; bot: Spread };

// Per commit, first deploy to the environment in the window: merge commits
// and release tooling left out, bots on their own line (docs/dora-lead-time.md).
export function leadTime(
  ds: Dataset,
  f: Filter,
  environment: string,
): LeadTimes {
  const rows = ds.lead_times.filter(
    (l: LeadTime) =>
      l.environment === environment &&
      inProduct(l.project, f) &&
      !l.is_merge &&
      !l.service_account,
  );
  return {
    environment,
    human: spread(rows.filter((l) => !l.bot).map((l) => l.lead_time_s)),
    bot: spread(rows.filter((l) => l.bot).map((l) => l.lead_time_s)),
  };
}

// ---- PRs -------------------------------------------------------------------

// PRs merged in the window, GitOps promotions left out, bots per the filter.
export function mergedPRs(
  ds: Dataset,
  p: Period,
  f: Filter,
  bots = f.bots,
): PR[] {
  return ds.prs.filter(
    (pr) =>
      !pr.gitops &&
      (bots || !pr.bot) &&
      pr.state === "merged" &&
      inProduct(pr.project, f) &&
      inPeriod(p, pr.merged_at),
  );
}

export type Stage = {
  key: "coding" | "review" | "merge" | "toPre" | "toProd";
  spread: Spread;
};

export type Chain = {
  stages: Stage[];
  // The stage before production the PRs' merge commits reached first; null
  // when none deployed anywhere else first.
  preEnvironment: string | null;
  approved: number;
  approvedOnFirstTouch: number;
};

// The lead-time breakdown query, per human PR merged in the window.
export function chain(ds: Dataset, p: Period, f: Filter): Chain {
  const prod = ds.meta.production_stage;
  const prs = mergedPRs(ds, p, f, false).filter((pr) => pr.ready_at != null);
  const pre = new Map<string, number>();
  const firstPre = (pr: PR) =>
    pr.environments.find((e) => e.environment !== prod) ?? null;
  for (const pr of prs) {
    const e = firstPre(pr);
    if (e) pre.set(e.environment, (pre.get(e.environment) ?? 0) + 1);
  }
  const preEnvironment =
    [...pre.entries()].sort(
      (a, b) => b[1] - a[1] || a[0].localeCompare(b[0]),
    )[0]?.[0] ?? null;

  const waits: Record<Stage["key"], number[]> = {
    coding: [],
    review: [],
    merge: [],
    toPre: [],
    toProd: [],
  };
  const add = (k: Stage["key"], w: number | null) => {
    if (w != null && w >= 0) waits[k].push(w);
  };
  for (const pr of prs) {
    add("coding", between(pr.first_commit_at, pr.ready_at));
    add("review", between(pr.ready_at, pr.approved_at));
    add("merge", between(pr.approved_at, pr.merged_at));
    const preAt = preEnvironment
      ? (pr.environments.find((e) => e.environment === preEnvironment)
          ?.first_deployed_at ?? null)
      : null;
    const prodAt =
      pr.environments.find((e) => e.environment === prod)?.first_deployed_at ??
      null;
    // The stage before production counts only when it came before
    // production: a hotfix deployed to production first and carried to the
    // earlier stage by a later release went past it.
    const viaPre =
      preAt != null &&
      (prodAt == null || Date.parse(preAt) <= Date.parse(prodAt));
    if (viaPre) add("toPre", between(pr.merged_at, preAt));
    // From the stage before production; a PR that went to production
    // without it (no such stage, or a hotfix past it) from its merge, so
    // its deploy time is not lost.
    add("toProd", between(viaPre ? preAt : pr.merged_at, prodAt));
  }
  const approved = prs.filter((pr) => pr.approved_at != null);
  return {
    stages: (Object.keys(waits) as Stage["key"][]).map((key) => ({
      key,
      spread: spread(waits[key]),
    })),
    preEnvironment,
    approved: approved.length,
    approvedOnFirstTouch: approved.filter((pr) => pr.approved_on_first_touch)
      .length,
  };
}

// Opened to merged, per PR merged in the window with a known opening.
export function cycleTimes(prs: PR[]): number[] {
  return prs
    .map((pr) => between(pr.opened_at, pr.merged_at))
    .filter((s): s is number => s != null && s >= 0);
}

export const CYCLE_BUCKETS: { label: string; upTo: number }[] = [
  { label: "< 15 min", upTo: 15 * 60 },
  { label: "15–60 min", upTo: 3600 },
  { label: "1–4 h", upTo: 4 * 3600 },
  { label: "4–24 h", upTo: 86400 },
  { label: "1–3 Tage", upTo: 3 * 86400 },
  { label: "> 3 Tage", upTo: Infinity },
];

export function histogram(seconds: number[]): number[] {
  const out = CYCLE_BUCKETS.map(() => 0);
  for (const s of seconds) out[CYCLE_BUCKETS.findIndex((b) => s < b.upTo)]++;
  return out;
}

export const MIX_TYPES = ["feature", "bugfix", "hotfix"] as const;
export type MixWeek = {
  feature: number;
  bugfix: number;
  hotfix: number;
  other: number;
};

// Merged PRs per week by branch prefix; every other prefix and none fold
// into "other".
export function changeMix(prs: PR[], p: Period): MixWeek[] {
  const weeks = p.weeks.map(() => ({
    feature: 0,
    bugfix: 0,
    hotfix: 0,
    other: 0,
  }));
  for (const pr of prs) {
    const i = weekIndex(p, pr.merged_at!);
    if (i < 0) continue;
    const t = pr.change_type as (typeof MIX_TYPES)[number];
    weeks[i][MIX_TYPES.includes(t) ? t : "other"]++;
  }
  return weeks;
}

// ---- Builds ----------------------------------------------------------------

// What a run was, so durations and success rates are never averaged across
// kinds: a release skips tests, a feature build fails more often by nature.
export type RunKind = "release" | "main" | "feature" | "other" | "unknown";

export const RUN_KINDS: RunKind[] = [
  "release",
  "main",
  "feature",
  "other",
  "unknown",
];

export function runKind(r: Run, buildTypeFrom: string | null): RunKind {
  const t = r.build_type?.toLowerCase();
  if (t === "release" || t === "hotfix") return "release";
  // Before the pipelines reported a build_type, a master run may have been
  // a release: unknown, not a master build.
  const at = r.finished_at ?? r.started_at;
  if (
    t == null &&
    r.branch_kind === "main" &&
    (buildTypeFrom == null ||
      (at != null && Date.parse(at) < Date.parse(buildTypeFrom)))
  )
    return "unknown";
  if (r.branch_kind === "main") return "main";
  if (r.branch_kind === "feature" || r.branch_kind === "pr") return "feature";
  return "other";
}

export type BuildStats = {
  kind: RunKind;
  runs: number;
  succeeded: number;
  // Runs without SUCCESS: FAILURE, UNSTABLE, ABORTED, whatever else came.
  failed: number;
  duration: Spread;
  ciSeconds: number;
  wastedSeconds: number;
  // Success rate per week, null for a week without runs.
  weekly: (number | null)[];
};

export function builds(ds: Dataset, p: Period, f: Filter): BuildStats[] {
  const runs = ds.runs.filter(
    (r) => inProduct(r.project, f) && r.status != null,
  );
  return RUN_KINDS.map((kind) => {
    const rs = runs.filter((r) => runKind(r, ds.meta.build_type_from) === kind);
    const ok = (r: Run) => r.status?.toUpperCase() === "SUCCESS";
    const secs = (r: Run) => r.duration_s ?? 0;
    const weekly = bucket(p, rs, (r) => r.finished_at ?? r.started_at).map(
      (w) => (w.length ? w.filter(ok).length / w.length : null),
    );
    return {
      kind,
      runs: rs.length,
      succeeded: rs.filter(ok).length,
      failed: rs.filter((r) => !ok(r)).length,
      duration: spread(
        rs.map((r) => r.duration_s).filter((s): s is number => s != null),
      ),
      ciSeconds: sum(rs.map(secs)),
      wastedSeconds: sum(rs.filter((r) => !ok(r)).map(secs)),
      weekly,
    };
  }).filter((b) => b.runs > 0);
}

export type RepoDuration = { repo: string; duration: number; runs: number };

// Median duration per repo for one kind of run, longest first.
export function repoDurations(
  ds: Dataset,
  f: Filter,
  kind: RunKind,
): RepoDuration[] {
  const by = new Map<string, number[]>();
  for (const r of ds.runs) {
    if (
      !inProduct(r.project, f) ||
      r.status == null ||
      r.duration_s == null ||
      runKind(r, ds.meta.build_type_from) !== kind
    )
      continue;
    const key = r.repo ?? r.pipeline_name.split("/")[0];
    const list = by.get(key);
    if (list) list.push(r.duration_s);
    else by.set(key, [r.duration_s]);
  }
  return [...by.entries()]
    .map(([repo, d]) => ({ repo, duration: median(d) ?? 0, runs: d.length }))
    .sort((a, b) => b.duration - a.duration || a.repo.localeCompare(b.repo));
}

// ---- Costs -----------------------------------------------------------------

export type Costs = {
  // All rollups closed in the window, whatever the outcome.
  total: number;
  merged: number;
  mergedReviews: number;
  // Per 1000 changed lines of merged PRs; null without line counts.
  perKloc: number | null;
  // Spent on PRs declined or deleted.
  wasted: number;
  // By product (all products) or by repo (one product), largest first.
  rows: { label: string; value: number }[];
  reviewedRepos: number;
};

const usd = (r: Review) => (r.cost_usd == null ? 0 : Number(r.cost_usd));

export function costs(ds: Dataset, f: Filter): Costs {
  const rs = ds.reviews.filter((r) => inProduct(r.project, f));
  const merged = rs.filter((r) => r.outcome === "merged");
  const lines = sum(
    merged.map((r) => (r.lines_added ?? 0) + (r.lines_removed ?? 0)),
  );
  const by = new Map<string, number>();
  for (const r of rs) {
    const key = (f.product == null ? r.project : r.repo) ?? "?";
    by.set(key, (by.get(key) ?? 0) + usd(r));
  }
  return {
    total: sum(rs.map(usd)),
    merged: sum(merged.map(usd)),
    mergedReviews: merged.length,
    perKloc: lines > 0 ? (sum(merged.map(usd)) / lines) * 1000 : null,
    wasted: sum(
      rs
        .filter((r) => r.outcome === "declined" || r.outcome === "deleted")
        .map(usd),
    ),
    rows: [...by.entries()]
      .map(([label, value]) => ({ label, value }))
      .sort((a, b) => b.value - a.value || a.label.localeCompare(b.label)),
    reviewedRepos: new Set(rs.map((r) => r.repo).filter(Boolean)).size,
  };
}
