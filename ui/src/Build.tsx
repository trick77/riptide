// How fast and how reliable the CI's feedback is: a card per kind of run,
// never a number across kinds; one list of the repos that slow it down;
// runs stuck together as one sentence.

import type { TFunction } from "i18next";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  BranchPrefix,
  BuildStats,
  CIFlow,
  Hotspots,
  FlowOutcome,
  KindChange,
  Metrics,
  Outlier,
  OutlierGroup,
  RepoHotspot,
  RunKind,
} from "./api";
import {
  ciHours,
  clock,
  dateTime,
  dayMonth,
  dur,
  fullDate,
  num,
  partPct,
  pct,
} from "./format";
import { shareLow, Spark } from "./charts";
import { FailureHeatmap } from "./heatmap";
import { Quiver } from "./quiver";
import { Sankey, type SankeyLink, type SankeyNode } from "./sankey";
import { SEP, TABLE_CELL as CELL, TABLE_HEAD as HEAD } from "./shared";
import type { Tab } from "./state";
import {
  Card,
  CardHead,
  Compare,
  Delta,
  Mono,
  Note,
  Pill,
  SectionHead,
  type Tone,
} from "./ui";

// Line colours in the series ramp's fixed order, by kind, so a kind keeps
// its colour whichever kinds the filter leaves, on the cards, in the flow
// and the scatter alike. The remainder kinds grey: a pink one would read
// as a second red next to the failed runs.
const KIND_STROKE: Record<RunKind, string> = {
  release: "var(--color-s1)",
  main: "var(--color-s2)",
  feature: "var(--color-s3)",
  other: "var(--color-other)",
  unknown: "var(--color-axis)",
};

// A kind's weekly success rate on the scale all cards share.
function KindSpark({ b, low }: { b: BuildStats; low: number }) {
  const { t } = useTranslation();
  return (
    <Spark
      values={b.weekly}
      low={low}
      high={1}
      stroke={KIND_STROKE[b.kind]}
      className="mt-2 block h-12 w-full"
      label={t("build.sparkAria", {
        kind: t(`build.kind.${b.kind}`),
        parts: b.weekly.map((v) => (v == null ? "–" : pct(v))).join(", "),
      })}
    />
  );
}

function KindCard({
  b,
  low,
  m,
  onFacts,
}: {
  b: BuildStats;
  low: number;
  m: Metrics;
  onFacts: (tab: Tab, q: string) => void;
}) {
  const { t } = useTranslation();
  const c = m.comparison.build.find((x) => x.kind === b.kind);
  const weeks = m.period.weeks;
  // Feature builds are what developers wait on: first, twice as wide.
  const lead = b.kind === "feature";
  const big = lead ? "text-[26px]" : "text-[20px]";
  return (
    <div
      className={`flex min-w-0 flex-col gap-1 rounded-ui border bg-panel px-4 py-3.5 ${
        lead ? "border-accent-dim md:col-span-2" : "border-border"
      }`}
    >
      <div
        className={`flex items-center gap-2 ${
          lead
            ? "text-[15px] font-medium text-ink"
            : "text-[13.5px] text-ink-dim"
        }`}
      >
        <span
          aria-hidden="true"
          className="size-2.5 rounded-[3px]"
          style={{ background: KIND_STROKE[b.kind] }}
        />
        {t(`build.kind.${b.kind}`)}
      </div>
      <div className="text-[11.5px] text-muted">
        {t(`build.kindNote.${b.kind}`)}
      </div>
      <div className="mt-1 flex flex-wrap gap-x-5 gap-y-1">
        <div className="text-xs text-muted">
          <div className={`${big} font-medium text-ink tabular-nums`}>
            {pct(b.success_rate)}
          </div>
          {c && (
            <div>
              <Delta change={c.success} format={pct} />
            </div>
          )}
          {t("build.successful")}
          {/* Its verdict under it: the flaky rate below has its own. */}
          {c && <Compare change={c.success} />}
        </div>
        <div className="text-xs text-muted">
          <div className={`${big} font-medium text-ink tabular-nums`}>
            {dur(b.duration.p50)}
          </div>
          {t("build.typicalDuration")}
        </div>
        <div className="text-xs text-muted">
          <div
            className={`${big} font-medium tabular-nums ${b.wasted_seconds > 0 ? "text-danger-ink" : "text-ink"}`}
          >
            {ciHours(b.wasted_seconds)}
          </div>
          {t("build.redRuns", { h: ciHours(b.ci_seconds) })}
        </div>
      </div>
      {/* With the feature hint, the hint card lists them. */}
      {b.prefixes.length > 0 && !m.build.feature_hint && (
        <div className="text-xs text-muted">
          {t("build.prefixes")}:{" "}
          {b.prefixes.map((p, i) => (
            <span key={p.prefix}>
              {i > 0 && ", "}
              <Prefix p={p} />
            </span>
          ))}
        </div>
      )}
      {b.waiting.n > 0 && (
        <div
          className="text-xs text-muted"
          title={t("build.waitingTitle", {
            n: b.waiting.n,
            total: b.duration.n,
          })}
        >
          {t("build.waiting", {
            wait: dur(b.waiting.p50),
            build: dur(b.building.p50),
          })}
        </div>
      )}
      {/* A normal card's width on the wide one: the line keeps its shape. */}
      <div className={lead ? "max-w-[320px]" : ""}>
        <KindSpark b={b} low={low} />
        <div className="flex justify-between text-[10.5px] text-faint">
          <span>{t("build.scale", { kw: weeks[0]?.kw, low: pct(low) })}</span>
          <span>{t("common.cw", { kw: weeks[weeks.length - 1]?.kw })}</span>
        </div>
      </div>
      <div className="text-xs text-muted">
        {t("build.runs", { count: b.runs })}
        {b.no_verdict > 0 && t("build.noVerdict", { n: b.no_verdict })}
        {b.outlier_runs > 0 &&
          t("build.outliers", {
            n: b.outlier_runs,
            h: ciHours(b.outlier_seconds),
          })}
      </div>
      <Flaky b={b} c={c} onFacts={onFacts} />
      {c && <RepoChanges c={c} />}
    </div>
  );
}

// The kind's red runs a rerun passed on the same commit: how many, their
// share of the red ones and their time, the rate against the previous
// period as the backend judged it, and the way to the runs.
function Flaky({
  b,
  c,
  onFacts,
}: {
  b: BuildStats;
  c: KindChange | undefined;
  onFacts: (tab: Tab, q: string) => void;
}) {
  const { t } = useTranslation();
  // Nothing red now and nothing flaky before: nothing to say. Flaky
  // before and nothing red now is the improvement the backend judged.
  if (b.failed === 0 && !c?.flaky.prev) return null;
  const compared = c?.flaky.prev != null ? c.flaky : null;
  return (
    <div
      className="flex flex-col gap-0.5 text-xs text-muted"
      title={t("build.flakyTitle")}
    >
      <span>
        {b.flaky_runs > 0
          ? t("build.flaky", {
              count: b.flaky_runs,
              share: pct(b.flaky_of_failed),
              h: ciHours(b.flaky_seconds),
            })
          : b.failed > 0
            ? t("build.flakyNone")
            : t("build.flakyNoRed")}
        {b.unjudged > 0 && t("build.unjudged", { count: b.unjudged })}
      </span>
      {(b.flaky_runs > 0 || compared) && (
        <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span>{t("build.flakyRate", { rate: pct(b.flaky_rate) })}</span>
          {compared && <Delta change={compared} format={pct} />}
          {b.flaky_runs > 0 && (
            <button
              type="button"
              onClick={() => onFacts("runs", "flaky")}
              className="no-print border-0 bg-transparent p-0 text-xs text-accent-strong hover:underline"
            >
              {t("build.viewFlaky")}
            </button>
          )}
        </span>
      )}
      {compared && <Compare change={compared} />}
    </div>
  );
}

// A branch scheme and its runs: "nightly (6)", so a number in the scheme
// ("feat-1") never reads as the count.
function Prefix({ p }: { p: BranchPrefix }) {
  const { t } = useTranslation();
  return (
    <>
      {p.prefix ? <Mono>{p.prefix}</Mono> : t("build.noBranch")}{" "}
      <span className="text-muted tabular-nums">({p.runs})</span>
    </>
  );
}

// The backend's feature hint: no feature build recognised, while other
// branches that look like work in progress ran. In the feature card's place,
// so the empty slot says where to look.
function FeatureMissing({
  hint,
  stats,
}: {
  hint: Metrics["build"]["feature_hint"];
  stats: BuildStats[];
}) {
  const { t } = useTranslation();
  const other = stats.find((s) => s.kind === "other");
  if (!hint) return null;
  return (
    <div className="flex min-w-0 flex-col gap-1.5 self-start rounded-ui border border-dashed border-ochre bg-ochre-wash px-4 py-3.5 md:col-span-2">
      <div className="flex items-center gap-2 text-[15px] font-medium text-ochre">
        <span aria-hidden="true" className="size-2.5 rounded-[3px] bg-ochre" />
        {t("build.featureMissingTitle")}
      </div>
      <p className="m-0 text-[13px] text-ink-dim">
        {t("build.featureMissing", { count: hint.runs })}
      </p>
      {other && other.prefixes.length > 0 && (
        <>
          <div className="text-xs text-muted">{t("build.prefixes")}</div>
          <ul className="m-0 flex list-none flex-wrap gap-1.5 p-0">
            {other.prefixes.map((p) => (
              <li
                key={p.prefix}
                className="rounded-md border border-border px-2 py-0.5 text-xs text-ink-dim tabular-nums"
              >
                <Prefix p={p} />
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

// The kind's repos against their own previous period: no median of the
// kind, whose mix of toolchains shifts from one period to the next.
function RepoChanges({ c }: { c: KindChange }) {
  const { t } = useTranslation();
  if (!c.compared)
    return <div className="text-xs text-muted">{t("build.reposNone")}</div>;
  return (
    <div className="text-[12.5px] text-ink-dim">
      {t("build.repos", {
        compared: c.compared,
        slower: c.slower,
        failing: c.failing_more,
      })}
    </div>
  );
}

// Each repo against its own previous period, never the team: a kind mixes
// Gradle, npm with Playwright and the rest. What the backend flagged, red.
function reason(t: TFunction, h: RepoHotspot) {
  const out: [string, Tone, string][] = [];
  if (h.failing_more)
    out.push([
      t("build.failingMore"),
      "fail",
      t("build.failingMoreText", {
        prev: pct(h.failure_change.prev),
        rate: pct(h.failure_rate),
      }),
    ]);
  if (h.slow)
    out.push([
      t("build.slow"),
      "fail",
      t("build.slowText", {
        prev: dur(h.duration_change.prev),
        p50: dur(h.duration.p50),
        factor: num(h.factor ?? 0, 1),
      }),
    ]);
  return out;
}

// What the backend judged better than the repo's previous period, green:
// a listed repo that improved says so instead of "unremarkable". Better is
// the verdict past the tolerance; red waits for the flags above, as a
// small worsening is noise there (the assessment counts the same way).
function gains(t: TFunction, h: RepoHotspot) {
  const out: [string, Tone, string][] = [];
  if (h.failure_change.verdict === "better")
    out.push([
      t("build.reliable"),
      "ok",
      t("build.failingMoreText", {
        prev: pct(h.failure_change.prev),
        rate: pct(h.failure_rate),
      }),
    ]);
  if (h.duration_change.verdict === "better")
    out.push([
      t("build.faster"),
      "ok",
      t("build.fasterText", {
        prev: dur(h.duration_change.prev),
        p50: dur(h.duration.p50),
      }),
    ]);
  return out;
}

// Why a repo has no comparison, in the repo's terms.
function noCompare(
  t: TFunction,
  h: RepoHotspot,
  minRuns: number,
): string | null {
  if (h.compared) return null;
  switch (h.duration_change.reason ?? h.failure_change.reason) {
    case "no_value":
      return t("build.noCompareNoValue");
    case "incomplete":
      return t("build.noCompareIncomplete");
    default:
      return t("build.noCompareLowSample", { min: minRuns });
  }
}

function Groups({ groups }: { groups: OutlierGroup[] }) {
  const { t } = useTranslation();
  const shared = groups.filter((g) => g.shared);
  if (!shared.length) return null;
  return (
    <ul className="m-0 mt-2 flex list-none flex-col gap-1.5 p-0 text-[13.5px] text-ink-dim">
      {shared.map((g) => (
        <li key={g.from}>
          <span className="font-medium text-ink">
            {t("build.group", {
              count: g.runs,
              repos: g.repos.length,
              when:
                dayMonth(new Date(g.from)) === dayMonth(new Date(g.to))
                  ? t("build.groupSameDay", {
                      day: dayMonth(new Date(g.from)),
                      from: clock(g.from),
                      to: clock(g.to),
                    })
                  : t("build.groupSpan", {
                      from: dateTime(g.from),
                      to: dateTime(g.to),
                    }),
            })}
          </span>
          {t("build.groupRest")}
        </li>
      ))}
    </ul>
  );
}

// A repo row's way to its runs in the facts.
function RunsLink({
  repo,
  onFacts,
}: {
  repo: string;
  onFacts: (tab: Tab, q: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <button
      type="button"
      onClick={() => onFacts("runs", repo)}
      className="no-print border-0 bg-transparent p-0 text-[13px] text-accent-strong hover:underline"
    >
      {t("build.viewRuns")}
    </button>
  );
}

// The kind's longest repos by median, flagged or not: a build steady at 25
// minutes is never slow against itself. Picked and ordered by the backend.
function SlowestTable({
  rows,
  minRuns,
  onFacts,
}: {
  rows: RepoHotspot[];
  minRuns: number;
  onFacts: (tab: Tab, q: string) => void;
}) {
  const { t } = useTranslation();
  if (!rows.length) return null;
  return (
    <div className="mt-5">
      <div className="text-[13.5px] font-medium text-ink">
        {t("build.slowestTitle")}
      </div>
      <div className="text-[12.5px] text-muted">
        {t("build.slowestSub", { min: minRuns })}
      </div>
      <div className="mt-2 overflow-x-auto">
        <div className="grid min-w-[820px] grid-cols-[minmax(150px,1.3fr)_80px_150px_150px_60px_150px] items-center gap-x-3.5">
          {[
            t("build.colRepo"),
            t("build.colMedian"),
            t("build.colSlowest"),
            t("build.colWaitBuild"),
            t("build.colRuns"),
            "",
          ].map((head, i) => (
            <div
              key={head || i}
              className={`${HEAD} whitespace-nowrap ${i > 0 && i < 5 ? "text-right" : ""}`}
            >
              {head}
            </div>
          ))}
          {rows.map((r) => (
            <div key={r.repo} className="contents">
              <div className={`${CELL} overflow-hidden`}>
                <Mono className="text-[12px] whitespace-nowrap text-ink-dim">
                  {r.repo}
                </Mono>
              </div>
              <div className={`${CELL} text-right text-ink`}>
                {dur(r.duration.p50)}
              </div>
              <div className={`${CELL} text-right text-ink-dim`}>
                {dur(r.duration.p90)}
              </div>
              <div
                className={`${CELL} text-right text-ink-dim`}
                title={
                  r.waiting.n > 0
                    ? t("build.waitingTitle", {
                        n: r.waiting.n,
                        total: r.duration.n,
                      })
                    : t("build.noAgentRepo")
                }
              >
                {r.waiting.n > 0
                  ? `${dur(r.waiting.p50)} / ${dur(r.building.p50)}`
                  : "–"}
              </div>
              {/* the runs the median is over */}
              <div className={`${CELL} text-right text-ink-dim`}>
                {num(r.duration.n)}
              </div>
              <div className={`${CELL} text-right`}>
                <RunsLink repo={r.repo} onFacts={onFacts} />
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function OutlierTable({ rows }: { rows: Outlier[] }) {
  const { t } = useTranslation();
  return (
    <div className="mt-3 overflow-x-auto">
      <div className="grid min-w-[700px] grid-cols-[minmax(180px,2fr)_minmax(130px,1.2fr)_110px_80px_80px_80px] items-center gap-x-3.5">
        {[
          t("build.colPipeline"),
          t("build.colKind"),
          t("build.colStart"),
          t("build.colDuration"),
          t("build.colWaiting"),
          t("build.colFactor"),
        ].map((h, i) => (
          <div key={h} className={`${HEAD} ${i > 1 ? "text-right" : ""}`}>
            {h}
          </div>
        ))}
        {rows.map((o) => (
          <div key={`${o.pipeline_name}#${o.run_id}`} className="contents">
            <div className={`${CELL} overflow-hidden`}>
              <Mono className="text-[12px] whitespace-nowrap text-ink-dim">
                {o.pipeline_name}
              </Mono>{" "}
              <span className="text-muted">#{o.run_id}</span>
            </div>
            <div className={`${CELL} text-muted`}>
              {t(`build.kind.${o.kind}`)}
            </div>
            <div className={`${CELL} text-right text-ink-dim`}>
              {o.started_at ? dateTime(o.started_at) : "–"}
            </div>
            <div className={`${CELL} text-right text-ink`}>
              {dur(o.duration_s)}
            </div>
            <div
              className={`${CELL} text-right text-ink-dim`}
              title={o.wait_s == null ? t("build.noAgent") : undefined}
            >
              {o.wait_s != null ? dur(o.wait_s) : "–"}
            </div>
            <div className={`${CELL} text-right text-ink-dim`}>
              {o.factor != null ? `${num(o.factor, 0)}×` : "–"}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// Where each kind's CI hours went: a failed stream stands out without
// setting kinds against each other, each share is of its own kind's time.
const OUTCOME_LOOK: Record<
  FlowOutcome,
  Pick<SankeyNode, "color" | "linkOpacity" | "hatch" | "danger" | "solid">
> = {
  succeeded: { color: "var(--color-ink-3)", linkOpacity: 0.45 },
  // Red time a rerun turned green: red on purpose, the failed time's part
  // hatched apart (the kinds keep no red; this is an outcome, red time).
  flaky: { color: "var(--color-danger)", linkOpacity: 0.45, hatch: true },
  failed: {
    color: "var(--color-danger)",
    linkOpacity: 0.85,
    danger: true,
    solid: true,
  },
  no_verdict: { color: "var(--color-faint)", linkOpacity: 0.45, hatch: true },
  outlier: { color: "var(--color-ochre)", linkOpacity: 0.45 },
};

// The repos ranked by CI time: the sequential ramp, the largest brightest;
// the series ramp's fixed order would put no rank in the colours. More
// repos named than shades keep the dimmest.
const REPO_COLOR = ["bg-seq-4", "bg-seq-3", "bg-seq-2"];

// FlowRepos is the flow's total once more, split by repo: the same hours,
// outliers included, the red part hatched as the flow's flaky and failed
// outcomes count it, so without the outliers.
function FlowRepos({ f }: { f: CIFlow }) {
  const { t } = useTranslation();
  if (!f.repos.length) return null;
  const rows = f.repos.map((r, i) => ({
    ...r,
    color: REPO_COLOR[Math.min(i, REPO_COLOR.length - 1)],
  }));
  if (f.other_repos > 0)
    rows.push({
      repo: t("build.flow.reposOther", { count: f.other_repos }),
      seconds: f.other_seconds,
      failed: f.other_failed,
      color: "bg-other",
    });
  // Hours, and the failed part as a number too, never by hatching alone.
  const label = (r: (typeof rows)[number]) =>
    r.failed > 0
      ? t("build.flow.repoFailed", {
          repo: r.repo,
          h: ciHours(r.seconds),
          failed: ciHours(r.failed),
        })
      : `${r.repo}: ${ciHours(r.seconds)}`;
  return (
    <div className="mt-3">
      <div className="mb-1.5 flex flex-wrap items-baseline gap-x-3 text-xs">
        <span className="font-medium text-ink-dim">
          {t("build.flow.repos")}
        </span>
        <span className="text-muted">{t("build.flow.reposMeta")}</span>
      </div>
      <div
        role="img"
        aria-label={t("build.flow.reposAria", {
          parts: rows.map(label).join("; "),
        })}
        className="flex h-3 gap-0.5"
      >
        {rows.map((r) => (
          <span
            key={r.repo}
            title={label(r)}
            className={`relative block h-full overflow-hidden rounded-sm ${r.color}`}
            style={{ flex: r.seconds }}
          >
            {r.failed > 0 && (
              <i
                className="texture-wasted absolute inset-y-0 right-0"
                style={{ width: `${(r.failed / r.seconds) * 100}%` }}
              />
            )}
          </span>
        ))}
      </div>
      <div className="mt-1.5 flex flex-wrap gap-x-3 font-mono text-[11px] text-muted">
        {rows.map((r) => (
          <span key={r.repo}>
            <i className={`mr-1 inline-block size-2 rounded-sm ${r.color}`} />
            {r.repo} {ciHours(r.seconds)}
            {r.failed > 0 && (
              <span className="text-danger-ink">
                {t("build.flow.repoFailedShort", { h: ciHours(r.failed) })}
              </span>
            )}
          </span>
        ))}
      </div>
    </div>
  );
}

function FlowCard({ f, h }: { f: CIFlow; h: Hotspots }) {
  const { t } = useTranslation();
  // The one place that decides: nothing to draw, no card.
  if (!f.links.length) return null;
  const kindLabel = (k: RunKind) => t(`build.kind.${k}`);
  const outcomeLabel = (o: FlowOutcome) => t(`build.flow.outcome.${o}`);
  // The cards above count CI time without outliers; a kind's node here
  // says so when it holds some.
  const stuck = new Set(
    f.links.filter((l) => l.outcome === "outlier").map((l) => l.kind),
  );
  const left: SankeyNode[] = f.kinds.map((k) => ({
    key: k.kind,
    label: kindLabel(k.kind),
    value: stuck.has(k.kind)
      ? t("build.flow.withOutliers", { h: ciHours(k.seconds) })
      : ciHours(k.seconds),
    color: KIND_STROKE[k.kind],
    linkOpacity: 0.5,
  }));
  const right: SankeyNode[] = f.outcomes.map((o) => ({
    key: o.outcome,
    label: `${o.outcome === "failed" ? "▼ " : ""}${outcomeLabel(o.outcome)}`,
    value: `${ciHours(o.seconds)} · ${partPct(o.share)}`,
    ...OUTCOME_LOOK[o.outcome],
  }));
  const links: SankeyLink[] = f.links.map((l) => ({
    from: l.kind,
    to: l.outcome,
    seconds: l.seconds,
    title: `${kindLabel(l.kind)} → ${outcomeLabel(l.outcome)}`,
    rows: [
      [t("build.flow.tipHours"), ciHours(l.seconds)],
      [
        t("build.flow.tipOfKind", { kind: kindLabel(l.kind) }),
        partPct(l.share_of_kind),
      ],
      [t("build.flow.tipOfAll"), partPct(l.share)],
    ],
  }));
  return (
    <Card className="mt-3">
      <CardHead
        title={t("build.flow.title")}
        sub={t("build.flow.sub", { sep: SEP })}
      />
      <Sankey
        left={left}
        right={right}
        links={links}
        ariaLabel={t("build.flow.aria", {
          parts: links
            .map((l) => `${l.title}: ${ciHours(l.seconds)}`)
            .join(", "),
        })}
      />
      <FlowRepos f={f} />
      <p className="mt-2.5 mb-0 text-xs text-muted">
        {t("build.flow.rule", {
          outliers: t("build.stuckSub", {
            factor: num(h.outlier_factor, 0),
            min: dur(h.outlier_min_s),
          }),
        })}
      </p>
    </Card>
  );
}

export function Build({
  m,
  onFacts,
}: {
  m: Metrics;
  onFacts: (tab: Tab, q: string) => void;
}) {
  const { t } = useTranslation();
  const b = m.build;
  const h = b.hotspots;
  const stats = b.kinds;
  const [kind, setKind] = useState<RunKind>("feature");
  // Feature builds by default: developers wait on them, the default branch
  // only gets what they merged. Without one recognised, the other branches
  // when the backend hints that the feature builds hide there.
  const fallback =
    b.feature_hint && stats.some((s) => s.kind === "other")
      ? "other"
      : stats[0]?.kind;
  const shown = stats.some((s) => s.kind === kind)
    ? kind
    : (fallback ?? "feature");
  const k = h.kinds.find((x) => x.kind === shown);
  // One scale for every card, so a dip reads the same in each: from the
  // lowest week, rounded down to 10 %, to 100 %.
  const low = shareLow(stats.flatMap((s) => s.weekly));

  return (
    <section id="build" aria-labelledby="h-build" className="mt-10">
      <SectionHead
        id="h-build"
        eyebrow={t("build.eyebrow")}
        title={t("build.title")}
        frameworks={[
          ["DevEx", "Feedback loops"],
          ["Core 4", t("build.fwEffectiveness")],
          ["SPACE", "Efficiency & flow"],
        ]}
      />
      {b.starts_inside && b.data_from && (
        <Note>
          {t("build.dataFrom", { date: fullDate(new Date(b.data_from)) })}
        </Note>
      )}
      {/* Team-wide: in the team view's numbers, in no product's. A
          product view may lack its builds for exactly this. */}
      {b.unattributed > 0 && (
        <p className="mt-2 mb-0 text-[13px] text-muted">
          {t("build.unattributed", { count: b.unattributed })}
        </p>
      )}
      {/* Bots' branches: in no number of this section, said once, as
          what they cost: CI time, never a finding (owner, 2026-11). */}
      {b.bot_runs.runs > 0 && (
        <p className="mt-2 mb-0 text-[13px] text-muted">
          {t("build.botRuns", {
            count: b.bot_runs.runs,
            h: ciHours(b.bot_runs.ci_seconds),
          })}
          {b.bot_runs.failed > 0 &&
            t("build.botRunsFailed", {
              count: b.bot_runs.failed,
              h: ciHours(b.bot_runs.failed_seconds),
            })}
          {t("build.botRunsRest")}
        </p>
      )}
      {stats.length === 0 ? (
        <Card className="mt-3">
          <p className="m-0 text-[13.5px] text-muted">{t("build.noRuns")}</p>
        </Card>
      ) : (
        <>
          <div className="mt-3 grid grid-cols-[repeat(auto-fill,minmax(250px,1fr))] gap-3">
            <FeatureMissing hint={b.feature_hint} stats={stats} />
            {stats.map((s) => (
              <KindCard key={s.kind} b={s} low={low} m={m} onFacts={onFacts} />
            ))}
          </div>
          <p className="mt-2 mb-0 text-xs text-muted">{t("build.legend")}</p>

          <FlowCard f={b.ci_flow} h={h} />

          <Card className="mt-3">
            <CardHead
              title={t("build.hotspotsTitle")}
              sub={t("build.hotspotsSub")}
              right={
                <div
                  role="group"
                  aria-label={t("build.kindGroup")}
                  className="no-print flex flex-wrap gap-1"
                >
                  {stats.map((s) => (
                    <button
                      key={s.kind}
                      type="button"
                      aria-pressed={s.kind === shown}
                      onClick={() => setKind(s.kind)}
                      className={`rounded-md border-0 px-2.5 py-1 text-xs ${
                        s.kind === shown
                          ? "bg-active text-ink shadow-[inset_0_0_0_1px] shadow-elevated-border"
                          : "bg-transparent text-muted"
                      }`}
                    >
                      {t(`build.kind.${s.kind}`)}
                    </button>
                  ))}
                </div>
              }
            />
            {k && (k.repos.length > 0 || k.unplotted > 0) && (
              <Quiver
                k={k}
                kindCiSeconds={
                  stats.find((s) => s.kind === k.kind)?.ci_seconds ?? 0
                }
                kindLabel={t(`build.kind.${k.kind}`)}
                stroke={KIND_STROKE[k.kind]}
                noCompare={(r) => noCompare(t, r, h.min_runs)}
              />
            )}
            {k && k.repos.length ? (
              <div className="mt-3 overflow-x-auto">
                <div className="grid min-w-[640px] grid-cols-[minmax(150px,1.3fr)_minmax(220px,2fr)_96px_150px] items-center gap-x-3.5">
                  {[
                    t("build.colRepo"),
                    t("build.colWhy"),
                    t("build.colRed"),
                    "",
                  ].map((head, i) => (
                    <div
                      key={head || i}
                      className={`${HEAD} ${i === 2 ? "text-right" : ""}`}
                    >
                      {head}
                    </div>
                  ))}
                  {k.repos.map((r) => {
                    const why = reason(t, r);
                    const better = gains(t, r);
                    const none = noCompare(t, r, h.min_runs);
                    return (
                      <div key={r.repo} className="contents">
                        <div className={`${CELL} overflow-hidden`}>
                          <Mono className="text-[12px] whitespace-nowrap text-ink-dim">
                            {r.repo}
                          </Mono>
                        </div>
                        <div className={`${CELL} flex flex-col gap-1`}>
                          {[...why, ...better].map(([label, tone, text]) => (
                            <span
                              key={label}
                              className="text-[12.5px] text-muted"
                            >
                              <Pill tone={tone}>{label}</Pill> {text}
                            </span>
                          ))}
                          {why.length === 0 && (
                            <span className="text-[12.5px] text-muted">
                              {better.length === 0 && (
                                <>
                                  <Pill tone="none">
                                    {none
                                      ? t("build.noCompare")
                                      : t("build.fine")}
                                  </Pill>{" "}
                                </>
                              )}
                              {none ? `${none}; ` : ""}
                              {t("build.fineText", {
                                p50: dur(r.duration.p50),
                                rate: pct(r.failure_rate),
                              })}
                              {r.waiting.n > 0 &&
                                t("build.fineWait", {
                                  wait: dur(r.waiting.p50),
                                  build: dur(r.building.p50),
                                })}
                            </span>
                          )}
                          {/* a slow repo waiting for agents is no slower build */}
                          {why.length > 0 && r.waiting.n > 0 && (
                            <span className="text-[12.5px] text-muted">
                              {t("build.repoWait", {
                                wait: dur(r.waiting.p50),
                                build: dur(r.building.p50),
                              })}
                            </span>
                          )}
                        </div>
                        <div
                          className={`${CELL} text-right ${r.wasted_seconds > 0 ? "text-danger-ink" : "text-ink-dim"}`}
                        >
                          {ciHours(r.wasted_seconds)}
                          {r.flaky_runs > 0 && (
                            <div
                              className="text-[11.5px] text-muted"
                              title={t("build.repoFlakyTitle", {
                                h: ciHours(r.flaky_seconds),
                              })}
                            >
                              {t("build.repoFlaky", { n: r.flaky_runs })}
                            </div>
                          )}
                        </div>
                        <div className={`${CELL} text-right`}>
                          <RunsLink repo={r.repo} onFacts={onFacts} />
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            ) : (
              <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
                {t("build.noneOfKind")}
              </p>
            )}
            <p className="mt-2.5 mb-0 text-xs text-muted">
              {t("build.hotspotsRule", {
                failing: num(h.failing_more_factor, 1),
                points: num(h.failing_more_min_points * 100, 0),
                failures: h.failing_more_min_failures,
                slow: num(h.slow_factor, 1),
                min: h.min_runs,
              })}
            </p>
            {k && (
              // Keyed by kind: a hovered cell never carries over to another.
              <FailureHeatmap key={k.kind} k={k} h={h} weeks={m.period.weeks} />
            )}
            {k && (
              <SlowestTable
                rows={k.slowest}
                minRuns={h.slowest_min_runs}
                onFacts={onFacts}
              />
            )}
          </Card>

          <Card className="mt-3">
            <CardHead
              title={t("build.stuckTitle")}
              sub={t("build.stuckSub", {
                factor: num(h.outlier_factor, 0),
                min: dur(h.outlier_min_s),
              })}
            />
            {h.outliers.length ? (
              <>
                <p className="mt-2 mb-0 text-[13.5px] text-ink-dim">
                  {t("build.stuck", { count: h.outlier_count })}
                  {t("build.stuckRest")}
                </p>
                <Groups groups={h.outlier_groups} />
                <details className="mt-2">
                  <summary className="cursor-pointer text-[13px] text-muted">
                    {h.outlier_count > h.outliers.length
                      ? t("build.showLongest", { n: h.outliers.length })
                      : t("build.showAll")}
                  </summary>
                  <OutlierTable rows={h.outliers} />
                </details>
              </>
            ) : (
              <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
                {t("build.noOutliers")}
              </p>
            )}
            <p className="mt-2.5 mb-0 text-xs text-muted">
              {t("build.stuckRule", {
                min: h.outlier_repo_min_runs,
                factor: num(h.outlier_factor, 0),
              })}
            </p>
          </Card>
        </>
      )}
    </section>
  );
}
