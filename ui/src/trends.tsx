// Numbers week by week as lines: the previous period on a recessive band,
// the period, then the week running now as a hollow dot on a dashed end (it
// is not over). A week the backend sent as a gap breaks the line and is
// hatched, never drawn as zero; a week alone between gaps is a dot. The
// values and gaps are the backend's; the UI scales and draws.

import type { TFunction } from "i18next";
import { useId } from "react";
import { useTranslation } from "react-i18next";
import type { Metrics, TrendGap, TrendSeries, TrendWeek } from "./api";
import { labelEvery } from "./charts";
import { num } from "./format";
import { Card, CardHead, Legend } from "./ui";

// The top of a scale: a round value at or above v.
function niceTop(v: number): number {
  if (v <= 0) return 1;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10])
    if (m * p >= v) return m * p;
  return 10 * p;
}

type Pt = { i: number; x: number; y: number; v: number };

// The series' closed weeks as runs of consecutive values; the running week
// apart.
function runs(
  values: (number | null)[],
  weeks: TrendWeek[],
  pt: (i: number, v: number) => Pt,
) {
  const segs: Pt[][] = [];
  let cur: Pt[] = [];
  let running: Pt | null = null;
  values.forEach((v, i) => {
    if (weeks[i]?.running) {
      if (v != null) running = pt(i, v);
      return;
    }
    if (v == null) {
      if (cur.length) segs.push(cur);
      cur = [];
    } else cur.push(pt(i, v));
  });
  if (cur.length) segs.push(cur);
  return { segs, running: running as Pt | null };
}

function gapTitle(t: TFunction, g: TrendGap): string {
  return g ? t(`trends.gap.${g}`) : "";
}

export function TrendChart({
  weeks,
  split,
  series,
  format,
  label,
  stroke = "var(--color-s1)",
  small = false,
  max,
  className,
}: {
  weeks: TrendWeek[];
  split: number;
  series: TrendSeries;
  format: (v: number) => string;
  label: string;
  stroke?: string;
  small?: boolean;
  // A scale shared with other charts; the series' own otherwise.
  max?: number;
  className?: string;
}) {
  const { t } = useTranslation();
  // One hatch per chart: an id shared across charts breaks once the first
  // one leaves the page.
  const hatch = `hatch${useId().replace(/[^a-zA-Z0-9]/g, "")}`;
  const W = small ? 140 : 560;
  const H = small ? 34 : 190;
  const padL = small ? 3 : 34;
  const padR = small ? 5 : 40;
  const padT = small ? 4 : 22;
  const padB = small ? 4 : 24;
  const n = weeks.length;
  const iw = W - padL - padR;
  const ih = H - padT - padB;
  const step = n < 2 ? 0 : iw / (n - 1);
  const x = (i: number) => padL + (n < 2 ? iw / 2 : i * step);
  const known = series.values.filter((v): v is number => v != null);
  const top = niceTop(max ?? Math.max(0, ...known));
  const y = (v: number) => padT + ih - (v / top) * ih;
  const pt = (i: number, v: number): Pt => ({ i, x: x(i), y: y(v), v });
  const { segs, running } = runs(series.values, weeks, pt);
  const band = split > 0 && split < n ? (x(split - 1) + x(split)) / 2 : 0;
  const last = segs.length ? segs[segs.length - 1].at(-1)! : null;
  const prevOfRunning =
    running && last && last.i === running.i - 1 ? last : null;
  const every = labelEvery(n);
  const aria = `${label}: ${weeks
    .map(
      (w, i) =>
        `${t("common.cw", { kw: w.kw })} ${
          series.values[i] == null
            ? gapTitle(t, series.gaps[i]) || "–"
            : format(series.values[i]!)
        }${w.running ? ` (${t("trends.running")})` : ""}`,
    )
    .join(", ")}`;
  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      className={
        className ??
        (small
          ? "block h-[34px] w-[140px] flex-none"
          : "chart mt-2 h-auto w-full")
      }
      role="img"
      aria-label={aria}
      data-testid={small ? "trend-spark" : "trend-chart"}
    >
      {band > 0 && (
        <rect
          x="0"
          y="0"
          width={band}
          height={H}
          fill="var(--color-active)"
          opacity="0.55"
        />
      )}
      {!small && band > 0 && (
        <>
          <text x={padL + 2} y="12" fontSize="10.5" fill="var(--color-muted)">
            {t("trends.previous")}
          </text>
          <text x={band + 6} y="12" fontSize="10.5" fill="var(--color-ink-dim)">
            {t("trends.period")}
          </text>
        </>
      )}
      {weeks.map((_, i) =>
        series.gaps[i] && series.gaps[i] !== "none" ? (
          <rect
            key={`g${i}`}
            data-testid="trend-gap"
            x={Math.max(0, x(i) - step / 2)}
            y={padT}
            width={Math.max(step, 4)}
            height={ih}
            fill={`url(#${hatch})`}
            opacity="0.8"
          >
            <title>{gapTitle(t, series.gaps[i])}</title>
          </rect>
        ) : null,
      )}
      <defs>
        <pattern
          id={hatch}
          width="6"
          height="6"
          patternUnits="userSpaceOnUse"
          patternTransform="rotate(45)"
        >
          <rect width="2" height="6" fill="var(--color-grid)" />
        </pattern>
      </defs>
      {small ? (
        <line
          x1="0"
          x2={W}
          y1={y(0)}
          y2={y(0)}
          stroke="var(--color-grid-minor)"
        />
      ) : (
        <>
          {[0, top / 2, top].map((v) => (
            <g key={v}>
              <line
                x1={padL}
                x2={W - padR}
                y1={y(v)}
                y2={y(v)}
                stroke={
                  v === 0 ? "var(--color-grid)" : "var(--color-grid-minor)"
                }
              />
              <text
                x={padL - 6}
                y={y(v) + 3.5}
                textAnchor="end"
                fontSize="10"
                fill="var(--color-muted)"
              >
                {/* 12.5 stays 12.5: a rounded label sits off its line. */}
                {num(v, v % 1 ? 1 : 0)}
              </text>
            </g>
          ))}
          {weeks.map((w, i) =>
            i % every === 0 || w.running ? (
              <text
                key={`l${i}`}
                x={x(i)}
                y={H - 6}
                textAnchor="middle"
                fontSize="10"
                fill={w.running ? "var(--color-faint)" : "var(--color-muted)"}
              >
                {t("common.cw", { kw: w.kw })}
              </text>
            ) : null,
          )}
        </>
      )}
      {segs.map((sg) =>
        sg.length > 1 ? (
          <path
            key={`s${sg[0].i}`}
            d={sg
              .map(
                (p, j) => `${j ? "L" : "M"}${p.x.toFixed(1)} ${p.y.toFixed(1)}`,
              )
              .join(" ")}
            fill="none"
            stroke={stroke}
            strokeWidth={small ? 1.8 : 2}
            strokeLinejoin="round"
          />
        ) : null,
      )}
      {prevOfRunning && running && (
        <line
          x1={prevOfRunning.x}
          y1={prevOfRunning.y}
          x2={running.x}
          y2={running.y}
          stroke={stroke}
          strokeWidth={small ? 1.4 : 1.6}
          strokeDasharray="3 3"
        />
      )}
      {segs.flat().map((p) =>
        // Every point on the big chart; on the small one only a week alone.
        !small || segs.some((sg) => sg.length === 1 && sg[0] === p) ? (
          <circle
            key={`p${p.i}`}
            cx={p.x}
            cy={p.y}
            r={small ? 2.2 : 3}
            fill={stroke}
            stroke="var(--color-panel)"
            strokeWidth="1.5"
          >
            <title>{`${t("common.cw", { kw: weeks[p.i].kw })}: ${format(p.v)}`}</title>
          </circle>
        ) : null,
      )}
      {running && (
        <circle
          data-testid="trend-running"
          cx={running.x}
          cy={running.y}
          r={small ? 2.6 : 3.5}
          fill="var(--color-panel)"
          stroke={stroke}
          strokeWidth="1.6"
        >
          <title>{`${t("common.cw", { kw: weeks[running.i].kw })} (${t("trends.running")}): ${format(running.v)}`}</title>
        </circle>
      )}
      {!small && last && (
        <text
          x={last.x + 7}
          y={last.y + 4}
          fontSize="11.5"
          fill="var(--color-ink)"
        >
          {format(last.v)}
        </text>
      )}
    </svg>
  );
}

// A tile's small trend line of one of the backend's series.
export function TileSpark({
  m,
  series,
  label,
}: {
  m: Metrics;
  series: "merged" | "batch" | "releases";
  label: string;
}) {
  const tr = m.trends;
  if (!tr.weeks.length) return null;
  return (
    <TrendChart
      weeks={tr.weeks}
      split={tr.split}
      series={tr[series]}
      format={(v) => num(v)}
      label={label}
      small
    />
  );
}

// The flow section's trend card: merged PRs and their typical size per
// week, the previous period beside the period.
export function TrendCard({ m }: { m: Metrics }) {
  const { t } = useTranslation();
  const tr = m.trends;
  if (!tr.weeks.length) return null;
  const prev = tr.weeks.slice(0, tr.split);
  const th = m.throughput;
  const c = m.comparison;
  const head = (
    title: string,
    now: number | null,
    before: number | null,
    unit: string,
  ) => (
    <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
      <span className="text-[13.5px] text-ink-dim">{title}</span>
      <span className="font-serif text-[24px] leading-none text-ink tabular-nums">
        {now == null ? "–" : num(now, now < 10 ? 1 : 0)}
        <small className="ml-1.5 font-sans text-[12.5px] text-muted">
          {before == null
            ? unit
            : t("trends.before", { unit, v: num(before, before < 10 ? 1 : 0) })}
        </small>
      </span>
    </div>
  );
  const gaps = new Set(
    [...tr.merged.gaps, ...tr.batch.gaps].filter((g) => g && g !== "none"),
  );
  return (
    <Card className="mt-3">
      <CardHead
        title={t("trends.title")}
        sub={t("trends.sub", {
          from: tr.weeks[0].kw,
          to: tr.weeks[tr.weeks.length - 1].kw,
        })}
      />
      <div className="mt-3.5 grid grid-cols-2 gap-x-8 gap-y-6 max-md:grid-cols-1">
        <div className="min-w-0">
          {head(
            t("trends.merged"),
            th.merged_per_week,
            c.merged_per_week.prev,
            t("trends.mergedUnit"),
          )}
          <TrendChart
            weeks={tr.weeks}
            split={tr.split}
            series={tr.merged}
            format={(v) => num(v)}
            label={t("trends.merged")}
          />
        </div>
        <div className="min-w-0">
          {head(
            t("trends.batch"),
            th.batch_size.p50,
            c.batch_size.prev,
            t("trends.batchUnit"),
          )}
          <TrendChart
            weeks={tr.weeks}
            split={tr.split}
            series={tr.batch}
            format={(v) => num(v)}
            label={t("trends.batch")}
          />
        </div>
      </div>
      <div className="mt-2.5">
        <Legend>
          {prev.length > 0 && (
            <span className="inline-flex items-center gap-1.5">
              <span
                aria-hidden="true"
                className="size-2.5 rounded-[3px] bg-active"
              />
              {t("trends.legendPrevious", {
                from: prev[0].kw,
                to: prev[prev.length - 1].kw,
              })}
            </span>
          )}
          <span className="inline-flex items-center gap-1.5">
            <span aria-hidden="true" className="size-2.5 rounded-[3px] bg-s1" />
            {t("trends.legendWeek")}
          </span>
          {tr.weeks.some((w) => w.running) && (
            <span className="inline-flex items-center gap-1.5">
              <span
                aria-hidden="true"
                className="size-2.5 rounded-full border-[1.5px] border-s1"
              />
              {t("trends.legendRunning")}
            </span>
          )}
          {gaps.size > 0 && (
            <span className="inline-flex items-center gap-1.5">
              <span
                aria-hidden="true"
                className="texture-gap size-2.5 rounded-[3px] border border-grid"
              />
              {[...gaps].map((g) => gapTitle(t, g)).join(" · ")}
            </span>
          )}
        </Legend>
      </div>
    </Card>
  );
}
