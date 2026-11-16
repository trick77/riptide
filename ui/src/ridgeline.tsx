// The lead-time stages as a ridgeline: each stage's waits as a mountain over
// one log time axis, rows overlapping, so the dominant stage reads as a wide
// mountain far to the right before any number does. The backend counts the
// waits into its buckets; this file only scales them and smooths the curve
// through the counts. Below the low-sample threshold the PRs show one by one:
// a curve through a handful of values would be invented.

import { useCallback, useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { LeadChain, Stage, StageKey } from "./api";
import { Tip } from "./charts";
import { dur, num } from "./format";
import { Card, CardHead } from "./ui";

// Drawn in rendered px: the plot measures its width, so dots and labels
// keep their size on a phone as on a wide screen.
const ROW = 46;
// A mountain rises past its row into the one above: the ridgeline overlap.
const PEAK = 60;
const TOP = 22;
const DOT_R = 4;
// Centre to centre: a dot and its panel ring never touch the next one.
const DOT_STEP = 2 * DOT_R + 2;
// Dots stack at most this high; the row's note sits above them.
const DOT_LEVELS = 3;
// Until measured (and in jsdom, which lays nothing out).
const FALLBACK_W = 640;
// Below this the minor ticks' labels would overprint.
const NARROW = 480;
const SEP = `${String.fromCharCode(0x2002)}·${String.fromCharCode(0x2002)}`;

// Axis ticks in seconds; the ones outside the backend's bounds drop out,
// the minor ones on a narrow plot.
const TICKS: [number, "min" | "h" | "d" | "w", number, boolean][] = [
  [600, "min", 10, false],
  [3600, "h", 1, false],
  [4 * 3600, "h", 4, true],
  [86400, "d", 1, false],
  [3 * 86400, "d", 3, false],
  [7 * 86400, "w", 1, true],
  [28 * 86400, "w", 4, false],
];

type Pt = [number, number];
// A cubic Bézier: start, two control points, end.
type Seg = [Pt, Pt, Pt, Pt];

// A Catmull-Rom spline through the points as cubic Béziers, the control
// points held between baseline and peak: a Bézier stays inside its control
// points' hull, so the curve never dips under the baseline next to a spike.
function spline(pts: Pt[], base: number): Seg[] {
  const clampY = (y: number) => Math.min(base, Math.max(base - PEAK, y));
  const out: Seg[] = [];
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i - 1] ?? pts[i];
    const p1 = pts[i];
    const p2 = pts[i + 1];
    const p3 = pts[i + 2] ?? p2;
    out.push([
      p1,
      [p1[0] + (p2[0] - p0[0]) / 6, clampY(p1[1] + (p2[1] - p0[1]) / 6)],
      [p2[0] - (p3[0] - p1[0]) / 6, clampY(p2[1] - (p3[1] - p1[1]) / 6)],
      p2,
    ]);
  }
  return out;
}

const f = (p: Pt) => `${p[0].toFixed(1)},${p[1].toFixed(1)}`;

function pathOf(segs: Seg[]): string {
  return (
    `M${f(segs[0][0])}` +
    segs.map(([, c1, c2, p]) => `C${f(c1)} ${f(c2)} ${f(p)}`).join("")
  );
}

function bezier(s: Seg, t: number, k: 0 | 1): number {
  const u = 1 - t;
  return (
    u * u * u * s[0][k] +
    3 * u * u * t * s[1][k] +
    3 * u * t * t * s[2][k] +
    t * t * t * s[3][k]
  );
}

// The drawn curve's y at x: the segment spanning x, its t found by
// bisection (x grows along a segment).
function curveY(segs: Seg[], x: number, base: number): number {
  const s = segs.find((g) => x >= g[0][0] && x <= g[3][0]);
  if (!s) return base;
  let a = 0;
  let b = 1;
  for (let i = 0; i < 30; i++) {
    const m = (a + b) / 2;
    if (bezier(s, m, 0) < x) a = m;
    else b = m;
  }
  return bezier(s, (a + b) / 2, 1);
}

type Colors = { fill: string; stroke: string };

function colors(s: Stage, i: number): Colors {
  // As the chain's bar above: the hot stage in the accent, the others in
  // its two alternating greys.
  if (s.hot)
    return {
      fill: "var(--color-accent-fill)",
      stroke: "var(--color-accent-strong)",
    };
  return {
    fill: i % 2 ? "var(--color-ink-3)" : "var(--color-other)",
    stroke: "var(--color-muted)",
  };
}

// The hot stage drawn as dots carries its numbers on lines of their own
// above the dots, one line on a wide plot, two on a narrow one; its band
// grows by them, so nothing overprints.
const LINE = 14;
function noteLines(s: Stage, w: number): number {
  return s.hot && s.low_sample && s.n > 0 ? (w < NARROW ? 2 : 1) : 0;
}

type Row = {
  s: Stage;
  base: number;
  // The row's height above its baseline.
  band: number;
  c: Colors;
  // The curve, or null for a low-sample row's dots.
  segs: Seg[] | null;
  line: string | null;
  area: string | null;
  dots: Pt[] | null;
  p50: number | null;
  p90: number | null;
  // Where the ticks end, y.
  p50Top: number;
  p90Top: number;
};

// One dot per PR on the baseline: at its wait if free, else stacked up to
// DOT_LEVELS high, else at the nearest free slot sideways, right and left
// alternately within the plot. Deterministic, and every PR stays a dot of
// its own; a plot too narrow for all of them stacks higher.
function placeDots(xs: number[], base: number, w: number): Pt[] {
  const placed: { x: number; level: number }[] = [];
  const lo = DOT_R;
  const hi = Math.max(lo, w - DOT_R);
  const free = (x: number, level: number) =>
    x >= lo &&
    x <= hi &&
    !placed.some(
      (p) => p.level === level && Math.abs(p.x - x) < DOT_STEP - 0.01,
    );
  const reach = Math.ceil(w / DOT_STEP);
  for (const raw of xs) {
    const x0 = Math.min(hi, Math.max(lo, raw));
    let spot: { x: number; level: number } | null = null;
    for (let levels = DOT_LEVELS; !spot; levels++) {
      for (let k = 0; k <= reach && !spot; k++) {
        for (const x of k === 0
          ? [x0]
          : [x0 + k * DOT_STEP, x0 - k * DOT_STEP]) {
          const level = [...Array(levels).keys()].find((l) => free(x, l));
          if (level != null) {
            spot = { x, level };
            break;
          }
        }
      }
    }
    placed.push(spot!);
  }
  return placed.map((p) => [p.x, base - DOT_R - 1 - p.level * DOT_STEP]);
}

// Every row's marks in px over a plot w wide: computed once per data and
// width, never on hover.
function layout(chain: LeadChain, w: number) {
  const bounds = chain.bucket_bounds_s;
  const lo = Math.log(bounds[0]);
  const hi = Math.log(bounds[bounds.length - 1]);
  const x = (s: number) =>
    ((Math.log(Math.min(Math.max(s, bounds[0]), bounds[bounds.length - 1])) -
      lo) /
      (hi - lo)) *
    w;
  const withData = chain.stages.filter((s) => s.p50 != null);
  let bottom = TOP;
  const rows: Row[] = chain.stages.map((s) => {
    const band = ROW + noteLines(s, w) * LINE;
    bottom += band;
    const base = bottom - 4;
    const c = colors(s, withData.indexOf(s));
    const p50 = s.p50 == null ? null : x(s.p50);
    const p90 = s.p90 == null ? null : x(s.p90);
    const h = s.histogram;
    if (s.low_sample || !h?.some((n) => n > 0)) {
      const placed = placeDots((s.durations_s ?? []).map(x), base, w);
      const tick = 18;
      return {
        s,
        base,
        band,
        c,
        segs: null,
        line: null,
        area: null,
        dots: placed,
        p50,
        p90,
        p50Top: base - tick,
        p90Top: base - tick,
      };
    }
    const max = Math.max(1, ...h);
    const mid = (j: number) => x(Math.sqrt(bounds[j] * bounds[j + 1]));
    // The mountain from the empty bucket before the first PR to the one
    // after the last, or the axis end: no line along an empty baseline.
    const first = h.findIndex((n) => n > 0);
    const last = h.length - 1 - [...h].reverse().findIndex((n) => n > 0);
    const pts: Pt[] = h
      .slice(first, last + 1)
      .map((n, j) => [mid(first + j), base - (n / max) * PEAK]);
    pts.unshift([first > 0 ? mid(first - 1) : 0, base]);
    pts.push([last < h.length - 1 ? mid(last + 1) : w, base]);
    const segs = spline(pts, base);
    const line = pathOf(segs);
    // Ticks end on the drawn curve, at least a stub tall.
    const top = (v: number | null) =>
      v == null ? base : Math.min(base - 10, curveY(segs, v, base));
    return {
      s,
      base,
      band,
      c,
      segs,
      line,
      area: `${line}Z`,
      dots: null,
      p50,
      p90,
      p50Top: top(p50),
      p90Top: top(p90),
    };
  });
  const ticks = TICKS.filter(
    ([s, , , minor]) =>
      s >= bounds[0] &&
      s <= bounds[bounds.length - 1] &&
      !(minor && w < NARROW),
  ).map(([s, unit, n]) => ({ s, unit, n, x: x(s) }));
  return { rows, ticks, H: bottom + 4 };
}

// The plot's rendered width, kept current: a callback ref, so the observer
// follows the node whenever it mounts, also after renders without one.
function useWidth() {
  const [w, setW] = useState(0);
  const ro = useRef<ResizeObserver | null>(null);
  const ref = useCallback((el: HTMLDivElement | null) => {
    ro.current?.disconnect();
    ro.current = null;
    if (!el) return;
    setW(el.clientWidth);
    if (typeof ResizeObserver === "undefined") return;
    ro.current = new ResizeObserver(() => setW(el.clientWidth));
    ro.current.observe(el);
  }, []);
  return [ref, w || FALLBACK_W] as const;
}

// A label's rendered width by key, measured once it is laid out (0 before
// and in jsdom): the side it goes on follows its real text, not a guess.
function useLabelWidths() {
  const [widths, setWidths] = useState<Record<string, number>>({});
  const measure = useCallback(
    (key: string) => (el: HTMLElement | null) => {
      if (!el) return;
      const v = el.offsetWidth;
      setWidths((m) => (m[key] === v ? m : { ...m, [key]: v }));
    },
    [],
  );
  return [widths, measure] as const;
}

export function StageRidge({
  chain,
  names,
}: {
  chain: LeadChain;
  names: Record<StageKey, [string, string]>;
}) {
  const { t } = useTranslation();
  const uid = useId().replace(/[^a-zA-Z0-9_-]/g, "");
  // The hovered row of this chain: new data drops a hover on the old one.
  const [hovered, setHovered] = useState<{
    chain: LeadChain;
    i: number;
  } | null>(null);
  const [ref, w] = useWidth();
  const [labelW, measure] = useLabelWidths();
  const usable =
    chain.stages.some((s) => s.p50 != null) &&
    chain.bucket_bounds_s.length >= 2;
  const geo = useMemo(
    () => (usable ? layout(chain, w) : null),
    [usable, chain, w],
  );
  if (!geo) return null;
  const { rows, ticks, H } = geo;
  const hover =
    hovered && hovered.chain === chain && hovered.i < rows.length
      ? rows[hovered.i]
      : null;
  const setHover = (i: number | null) =>
    setHovered(i == null ? null : { chain, i });

  const typical = (s: Stage) => t("common.typical", { unit: dur(s.p50) });
  const slowest = (s: Stage) => t("common.slowest", { p90: dur(s.p90) });
  const hot = rows.find(({ s }) => s.hot && s.p50 != null);
  // A label right of its tick, or left of it where its measured text
  // would run off the plot.
  const labelAt = (v: number, key: string) =>
    v + 4 + (labelW[key] ?? 0) > w
      ? { left: v - 4, transform: "translateX(-100%)" }
      : { left: v + 4 };
  // Text on the panel without a box: legible over its own curve, never
  // hiding another row's marks.
  const halo = {
    textShadow:
      "0 0 3px var(--color-panel), 0 0 3px var(--color-panel), 0 0 2px var(--color-panel)",
  };

  return (
    <Card className="mt-3">
      <CardHead title={t("flow.ridge.title")} sub={t("flow.ridge.sub")} />
      {/* For assistive tech the numbers once, as a table; the drawing and
          its tooltip are hidden from it. */}
      <table className="sr-only">
        <caption>{t("flow.ridge.caption")}</caption>
        <thead>
          <tr>
            <th scope="col">{t("flow.ridge.colStage")}</th>
            <th scope="col">{t("flow.ridge.tipN")}</th>
            <th scope="col">{t("flow.ridge.tipTypical")}</th>
            <th scope="col">{t("flow.ridge.tipSlowest")}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(({ s }) => (
            <tr key={s.key}>
              <th scope="row">{names[s.key][0]}</th>
              <td>{num(s.n)}</td>
              {s.n === 0 ? (
                <td colSpan={2}>{t("flow.ridge.none")}</td>
              ) : (
                <>
                  <td>{dur(s.p50)}</td>
                  <td>{dur(s.p90)}</td>
                </>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      <div className="mt-4 flex gap-3" aria-hidden="true">
        <div
          className="relative w-[112px] flex-none sm:w-[150px]"
          style={{ height: H }}
        >
          {rows.map(({ s, base }) => (
            <div
              key={s.key}
              className="absolute inset-x-0 leading-tight"
              style={{ top: base - 30 }}
            >
              <div
                className={`truncate text-[12.5px] ${s.hot ? "font-medium text-ink" : "text-ink-dim"}`}
              >
                {names[s.key][0]}
              </div>
              <div
                className={`text-[11px] tabular-nums ${s.low_sample ? "text-ochre" : "text-muted"}`}
              >
                {t("common.nPRs", { count: s.n })}
              </div>
            </div>
          ))}
        </div>
        <div className="min-w-0 flex-1">
          <div ref={ref} className="relative" style={{ height: H }}>
            <svg
              viewBox={`0 0 ${w} ${H}`}
              width={w}
              height={H}
              className="absolute inset-0 block overflow-visible"
              onMouseLeave={() => setHover(null)}
            >
              <defs>
                {/* Print drops the glow; the gradients print as drawn. */}
                <style>{`@media print { .ridge-glow { filter: none; } }`}</style>
                <filter
                  id={`${uid}glow`}
                  x="-10%"
                  y="-40%"
                  width="120%"
                  height="180%"
                >
                  <feGaussianBlur stdDeviation="2.6" result="b" />
                  <feMerge>
                    <feMergeNode in="b" />
                    <feMergeNode in="SourceGraphic" />
                  </feMerge>
                </filter>
                {rows.map(({ s, c }) => (
                  <linearGradient
                    key={s.key}
                    id={`${uid}g${s.key}`}
                    x1="0"
                    y1="0"
                    x2="0"
                    y2="1"
                  >
                    <stop
                      offset="0"
                      stopColor={c.fill}
                      stopOpacity={s.hot ? 0.75 : 0.6}
                    />
                    <stop offset="1" stopColor={c.fill} stopOpacity="0.05" />
                  </linearGradient>
                ))}
              </defs>
              {ticks.map((k) => (
                <line
                  key={k.s}
                  x1={k.x}
                  x2={k.x}
                  y1={0}
                  y2={H}
                  stroke="var(--color-grid-minor)"
                />
              ))}
              {/* Mountains and baselines first, row by row; ticks and dots
                  after all of them, so a later row's underlay never hides
                  them. */}
              {rows.map((r) => (
                <g key={r.s.key}>
                  {r.area && r.line && (
                    <>
                      {/* Opaque underlay: the row in front hides the one
                          behind instead of mixing with it. */}
                      <path d={r.area} fill="var(--color-panel)" />
                      <path d={r.area} fill={`url(#${uid}g${r.s.key})`} />
                      <path
                        d={r.line}
                        fill="none"
                        stroke={r.c.stroke}
                        strokeWidth={r.s.hot ? 2 : 1.3}
                        strokeLinejoin="round"
                        className={r.s.hot ? "ridge-glow" : undefined}
                        filter={r.s.hot ? `url(#${uid}glow)` : undefined}
                      />
                    </>
                  )}
                  <line
                    x1={0}
                    x2={w}
                    y1={r.base}
                    y2={r.base}
                    stroke="var(--color-axis)"
                  />
                </g>
              ))}
              {rows.map((r) => (
                <g key={r.s.key}>
                  {r.p50 != null && (
                    <line
                      x1={r.p50}
                      x2={r.p50}
                      y1={r.base}
                      y2={r.p50Top}
                      stroke="var(--color-ink)"
                      strokeOpacity={r.s.hot ? 0.9 : 0.5}
                      strokeWidth="1.5"
                    />
                  )}
                  {r.p90 != null && (
                    <line
                      x1={r.p90}
                      x2={r.p90}
                      y1={r.base}
                      y2={r.p90Top}
                      stroke="var(--color-ink)"
                      strokeOpacity={r.s.hot ? 0.7 : 0.4}
                      strokeDasharray="3 2"
                      strokeWidth="1.2"
                    />
                  )}
                </g>
              ))}
              {rows.map((r) => (
                <g key={r.s.key}>
                  {r.dots?.map(([cx, cy], k) => (
                    <circle
                      key={k}
                      data-testid="ridge-dot"
                      cx={cx}
                      cy={cy}
                      r={DOT_R}
                      fill={r.c.stroke}
                      stroke="var(--color-panel)"
                      strokeWidth="2"
                    />
                  ))}
                </g>
              ))}
              {/* Hit targets, later rows on top as drawn: a row's band
                  and its mountain where it rises into the row above. */}
              {rows.map((r, i) => (
                <g
                  key={r.s.key}
                  data-testid="ridge-row"
                  onMouseEnter={() => setHover(i)}
                >
                  <rect
                    x={0}
                    y={r.base - r.band}
                    width={w}
                    height={r.band}
                    fill="transparent"
                  />
                  {r.area && <path d={r.area} fill="transparent" />}
                </g>
              ))}
            </svg>
            {rows.map(({ s, base, band, dots }) => {
              if (!dots) return null;
              // The hot stage's numbers on lines above the dots: beside
              // them they would cover them.
              const extra =
                noteLines(s, w) === 2
                  ? [typical(s), slowest(s)]
                  : noteLines(s, w) === 1
                    ? [`${typical(s)}${SEP}${slowest(s)}`]
                    : [];
              return (
                <div
                  key={s.key}
                  className="pointer-events-none absolute left-0 text-[11px] whitespace-nowrap text-muted"
                  style={{ top: base - band + 2, lineHeight: `${LINE}px` }}
                >
                  <div style={halo}>
                    {s.n === 0
                      ? t("flow.ridge.none")
                      : t("flow.ridge.few", { min: chain.min_prs })}
                  </div>
                  {extra.map((l) => (
                    <div key={l} className="text-ink-dim" style={halo}>
                      {l}
                    </div>
                  ))}
                </div>
              );
            })}
            {hot && !hot.dots && (
              <>
                {/* Two lines at the top of the hot row's own band: below
                    the row above, above the row below's mountains. */}
                <span
                  ref={measure("typical")}
                  className="pointer-events-none absolute text-[11px] whitespace-nowrap text-ink tabular-nums"
                  style={{
                    top: hot.base - ROW + 1,
                    ...labelAt(hot.p50!, "typical"),
                    ...halo,
                  }}
                >
                  {typical(hot.s)}
                </span>
                {hot.p90 != null && (
                  <span
                    ref={measure("slowest")}
                    className="pointer-events-none absolute text-[11px] whitespace-nowrap text-ink-dim tabular-nums"
                    style={{
                      top: hot.base - ROW + 16,
                      ...labelAt(hot.p90, "slowest"),
                      ...halo,
                    }}
                  >
                    {slowest(hot.s)}
                  </span>
                )}
              </>
            )}
            {hover && (
              <div
                className="pointer-events-none absolute inset-x-0"
                style={{ top: hover.base - hover.band, height: hover.band }}
              >
                <Tip
                  title={names[hover.s.key][0]}
                  rows={[
                    [t("flow.ridge.tipN"), num(hover.s.n)],
                    [t("flow.ridge.tipTypical"), dur(hover.s.p50)],
                    [t("flow.ridge.tipSlowest"), dur(hover.s.p90)],
                  ]}
                />
              </div>
            )}
          </div>
          <div className="relative mt-1.5 h-4">
            {ticks.map((k, i) => (
              <span
                key={k.s}
                className={`absolute text-[11px] whitespace-nowrap text-muted ${
                  i === ticks.length - 1 && k.x >= w
                    ? "-translate-x-full"
                    : "-translate-x-1/2"
                }`}
                style={{ left: k.x }}
              >
                {t(`flow.ridge.tick.${k.unit}`, { n: k.n })}
              </span>
            ))}
          </div>
        </div>
      </div>
      <p className="mt-2.5 mb-0 text-xs text-muted">{t("flow.ridge.note")}</p>
    </Card>
  );
}
