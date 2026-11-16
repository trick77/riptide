// A kind's repos as arrows from the previous period to now: median duration
// (log) against failure rate, the circle's area the repo's share of the
// kind's CI time. Red only where the backend flagged slow or flaky, an
// arrow only where it sent both previous values; the browser draws no
// thresholds and classifies nothing, it places, scales and labels.

import { useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { KindHotspots, RepoHotspot } from "./api";
import { Tip } from "./charts";
import { dur, num, pct } from "./format";

// Chart units. Below lg the chart keeps MIN_W px and scrolls, so the
// smallest text (12.5 units) renders at 10px or more.
const W = 800;
const H = 360;
const M = { l: 52, r: 16, t: 26, b: 48 };
const PW = W - M.l - M.r;
const PH = H - M.t - M.b;
// Circle radius of a repo with all of the kind's CI time; area scales with
// the share.
const RFULL = 64;
// The smallest circle still drawn as one.
const RMIN = 4.5;
// Candidate gridlines on the duration axis, in seconds.
const X_TICKS = [
  5, 10, 30, 60, 120, 300, 600, 1200, 1800, 3600, 7200, 14400, 28800, 86400,
];
// Duration ticks closer than this are dropped, so labels never touch.
const TICK_GAP = 56;
const TICK = 12.5;
const LABEL = 13;
// Mono: about 0.6 em a character, to keep a label inside.
const CHAR_W = LABEL * 0.6;
const LINE = 17;

const DANGER = "var(--color-danger)";
const GREY = "var(--color-faint)";

type Placed = {
  h: RepoHotspot;
  nx: number;
  ny: number;
  // Previous position, when the backend sent both previous values.
  prev: { x: number; y: number } | null;
  r: number;
  bad: boolean;
};

// The duration axis: the values' span with some room, at least a decade.
function xDomain(values: number[]): [number, number] {
  let lo = Math.max(1, Math.min(...values) / 1.5);
  let hi = Math.max(lo * 1.5, Math.max(...values) * 1.5);
  if (hi / lo < 10) {
    const mid = Math.sqrt(lo * hi);
    lo = Math.max(1, mid / Math.sqrt(10));
    hi = lo * 10;
  }
  return [lo, hi];
}

// The rate axis: from 0 to the next tick above the highest rate, ticks that
// stay few, the top one always on a tick.
function yDomain(values: number[]): { max: number; ticks: number[] } {
  const top = Math.min(1, Math.max(0, ...values) + 0.02);
  const step = top <= 0.4 ? 0.1 : top <= 0.8 ? 0.2 : 0.25;
  const max =
    Math.round(
      Math.min(1, Math.max(step, Math.ceil(top / step) * step)) * 100,
    ) / 100;
  const ticks: number[] = [];
  for (let v = 0; v <= max + 1e-9; v += step)
    ticks.push(Math.round(v * 100) / 100);
  return { max, ticks };
}

export function Quiver({
  k,
  kindCiSeconds,
  kindLabel,
  stroke,
  noCompare,
}: {
  k: KindHotspots;
  // The kind's CI time, all repos: the base of every circle's area.
  kindCiSeconds: number;
  kindLabel: string;
  // The kind's series colour, for the unflagged repos.
  stroke: string;
  // Why a repo has no comparison, as the table words it; null when it has.
  noCompare: (h: RepoHotspot) => string | null;
}) {
  const { t } = useTranslation();
  const uid = useId().replace(/[^a-zA-Z0-9_-]/g, "");
  // The tooltip's anchor in viewport coordinates: it renders outside the
  // chart's scroll box, which would clip it.
  const [hover, setHover] = useState<{
    repo: string;
    left: number;
    top: number;
  } | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);

  const drawable = k.repos.filter((h) => h.plotted);
  const unplotted =
    k.unplotted > 0 && t("quiver.unplotted", { count: k.unplotted });
  if (!drawable.length)
    return unplotted ? (
      <p data-testid="quiver" className="mt-3 mb-0 text-xs text-muted">
        {unplotted}
      </p>
    ) : null;

  const durations = drawable.flatMap((h) =>
    h.prev_complete
      ? [h.duration.p50!, h.duration_change.prev!]
      : [h.duration.p50!],
  );
  const rates = drawable.flatMap((h) =>
    h.prev_complete
      ? [h.failure_rate!, h.failure_change.prev!]
      : [h.failure_rate!],
  );
  const [lo, hi] = xDomain(durations);
  const llo = Math.log10(lo);
  const lspan = Math.log10(hi) - llo;
  const x = (v: number) =>
    M.l + ((Math.log10(Math.max(v, lo)) - llo) / lspan) * PW;
  const yd = yDomain(rates);
  const y = (v: number) => M.t + PH - (Math.min(v, yd.max) / yd.max) * PH;
  const radius = (ci: number) =>
    kindCiSeconds > 0
      ? Math.max(RMIN, RFULL * Math.sqrt(Math.max(0, ci) / kindCiSeconds))
      : RMIN;

  const placed: Placed[] = drawable.map((h) => ({
    h,
    nx: x(h.duration.p50!),
    ny: y(h.failure_rate!),
    prev: h.prev_complete
      ? { x: x(h.duration_change.prev!), y: y(h.failure_change.prev!) }
      : null,
    r: radius(h.ci_seconds),
    bad: h.slow || h.flaky,
  }));
  // Painted in three layers, each in the backend's order: the flagged on
  // top, so no grey circle hides a red arrow, nor takes its hover.
  const layers = [
    placed.filter((p) => !p.h.compared),
    placed.filter((p) => p.h.compared && !p.bad),
    placed.filter((p) => p.h.compared && p.bad),
  ];
  const painted = layers.flat();
  // Hover targets reach past small circles: the largest first, so every
  // smaller one on top stays reachable; flagged on top among equals.
  const hits = [...painted].sort(
    (a, b) => b.r - a.r || Number(a.bad) - Number(b.bad),
  );
  const show = (p: Placed) => {
    const box = svgRef.current?.getBoundingClientRect();
    if (!box) return;
    setHover({
      repo: p.h.repo,
      left: box.left + (p.nx / W) * box.width,
      top: box.top + ((p.ny - p.r) / H) * box.height,
    });
  };

  const xTicks: number[] = [];
  for (const v of X_TICKS) {
    if (v < lo || v > hi) continue;
    const last = xTicks[xTicks.length - 1];
    if (last == null || x(v) - x(last) >= TICK_GAP) xTicks.push(v);
  }
  const shown = hover && placed.find((p) => p.h.repo === hover.repo);

  const label = (p: Placed) => {
    const parts = [p.h.repo];
    if (p.h.slow && p.h.factor != null)
      parts.push(t("quiver.slower", { factor: num(p.h.factor, 1) }));
    if (p.h.flaky)
      parts.push(
        t("quiver.flakier", {
          prev: pct(p.h.failure_change.prev),
          rate: pct(p.h.failure_rate),
        }),
      );
    return parts.join(" ");
  };

  // Flagged labels beside their circle, flipped or centred to stay inside,
  // then moved a line down or up, nearest first, while they overlap one
  // placed before; within the plot area, or none and the tooltip tells.
  // Top to bottom, so a label is pushed towards its own circle's side.
  const boxes: { x0: number; x1: number; y: number }[] = [];
  const labelTop = M.t + LABEL;
  const labelBottom = M.t + PH - 4;
  const labels = [...layers[2]]
    .sort((a, b) => a.ny - b.ny)
    .flatMap((p) => {
      const text = label(p);
      const width = text.length * CHAR_W;
      let x0 = p.nx + p.r + 6;
      if (x0 + width > W - 2) x0 = p.nx - p.r - 6 - width;
      if (x0 < 2) x0 = Math.min(Math.max(2, p.nx - width / 2), W - 2 - width);
      const clash = (ly: number) =>
        boxes.some(
          (b) =>
            b.x0 < x0 + width && x0 < b.x1 && Math.abs(b.y - ly) < LINE - 1,
        );
      const start = Math.min(labelBottom, Math.max(labelTop, p.ny - p.r - 4));
      let ly: number | null = null;
      for (let d = 0; ly == null; d++) {
        const down = start + d * LINE;
        const up = start - d * LINE;
        if (down > labelBottom && up < labelTop) break;
        if (down <= labelBottom && !clash(down)) ly = down;
        else if (up >= labelTop && !clash(up)) ly = up;
      }
      if (ly == null) return [];
      boxes.push({ x0, x1: x0 + width, y: ly });
      return [{ p, text, x0, ly }];
    });

  const tipRows = (h: RepoHotspot): [string, string][] => {
    const dp = h.duration_change.prev;
    const fp = h.failure_change.prev;
    const rows: [string, string][] = [
      [
        t("quiver.median"),
        dp != null
          ? `${dur(dp)} → ${dur(h.duration.p50)}`
          : dur(h.duration.p50),
      ],
      [
        t("quiver.rate"),
        fp != null
          ? `${pct(fp)} → ${pct(h.failure_rate)}`
          : pct(h.failure_rate),
      ],
      [t("quiver.runs"), num(h.runs)],
      [t("quiver.ci"), t("common.hours", { n: num(h.ci_seconds / 3600, 1) })],
    ];
    const none = noCompare(h);
    if (none) rows.push([t("build.noCompare"), none]);
    return rows;
  };

  return (
    <div className="mt-3" data-testid="quiver">
      <div className="overflow-x-auto lg:overflow-visible">
        <div className="relative max-w-[880px] min-w-[640px] lg:min-w-0">
          <svg
            ref={svgRef}
            viewBox={`0 0 ${W} ${H}`}
            className="block h-auto w-full"
            role="group"
            aria-label={t("quiver.aria", { kind: kindLabel })}
          >
            <defs>
              {/* In chart units: a vertical or horizontal arrow has a box of
                width or height 0, and a filter region relative to it would
                erase the arrow. */}
              <filter
                id={`${uid}-glow`}
                filterUnits="userSpaceOnUse"
                x="0"
                y="0"
                width={W}
                height={H}
              >
                <feGaussianBlur stdDeviation="2.6" result="b" />
                <feMerge>
                  <feMergeNode in="b" />
                  <feMergeNode in="SourceGraphic" />
                </feMerge>
              </filter>
              {(
                [
                  ["d", DANGER, 10],
                  ["g", GREY, 8],
                ] as const
              ).map(([id, fill, size]) => (
                <marker
                  key={id}
                  id={`${uid}-arr${id}`}
                  viewBox="0 0 10 10"
                  refX="9"
                  refY="5"
                  markerUnits="userSpaceOnUse"
                  markerWidth={size}
                  markerHeight={size}
                  orient="auto"
                >
                  <path d="M0,1 L9,5 L0,9 Z" fill={fill} />
                </marker>
              ))}
            </defs>
            <g aria-hidden="true">
              {xTicks.map((v) => (
                <g key={v}>
                  <line
                    x1={x(v)}
                    x2={x(v)}
                    y1={M.t}
                    y2={M.t + PH}
                    stroke="var(--color-grid-minor)"
                  />
                  <text
                    x={x(v)}
                    y={H - 26}
                    textAnchor="middle"
                    fontSize={TICK}
                    fill="var(--color-muted)"
                  >
                    {dur(v)}
                  </text>
                </g>
              ))}
              {yd.ticks.map((v) => (
                <g key={v}>
                  <line
                    x1={M.l}
                    x2={W - M.r}
                    y1={y(v)}
                    y2={y(v)}
                    stroke={
                      v === 0 ? "var(--color-axis)" : "var(--color-grid-minor)"
                    }
                  />
                  <text
                    x={M.l - 8}
                    y={y(v) + 4}
                    textAnchor="end"
                    fontSize={TICK}
                    fill="var(--color-muted)"
                  >
                    {pct(v)}
                  </text>
                </g>
              ))}
              <text
                x={W - M.r}
                y={H - 4}
                textAnchor="end"
                fontSize={TICK}
                fill="var(--color-faint)"
              >
                {t("quiver.xAxis")}
              </text>
              <text
                x={M.l + 6}
                y={M.t - 10}
                fontSize={TICK}
                fill="var(--color-faint)"
              >
                {t("quiver.yAxis")}
              </text>
              {painted.map((p) => {
                const color = p.bad ? DANGER : stroke;
                let arrow = null;
                if (p.prev) {
                  const dx = p.nx - p.prev.x;
                  const dy = p.ny - p.prev.y;
                  const len = Math.hypot(dx, dy);
                  // Too short to point anywhere: the previous dot alone,
                  // inside or beside the circle.
                  if (len > p.r + 6) {
                    const ex = p.nx - (dx / len) * (p.r + 2);
                    const ey = p.ny - (dy / len) * (p.r + 2);
                    arrow = (
                      <line
                        x1={p.prev.x}
                        y1={p.prev.y}
                        x2={ex}
                        y2={ey}
                        stroke={p.bad ? DANGER : GREY}
                        strokeWidth={p.bad ? 2.5 : 1.25}
                        strokeLinecap="round"
                        markerEnd={`url(#${uid}-arr${p.bad ? "d" : "g"})`}
                        filter={p.bad ? `url(#${uid}-glow)` : undefined}
                        className="print:filter-none"
                      />
                    );
                  }
                }
                return (
                  <g
                    key={p.h.repo}
                    data-testid="quiver-repo"
                    data-repo={p.h.repo}
                    data-flagged={p.bad || undefined}
                    data-arrow={arrow ? true : undefined}
                  >
                    {p.h.compared ? (
                      <circle
                        cx={p.nx}
                        cy={p.ny}
                        r={p.r}
                        fill={color}
                        fillOpacity={p.bad ? 0.3 : 0.18}
                        stroke={color}
                        strokeWidth="1.5"
                      />
                    ) : (
                      <circle
                        cx={p.nx}
                        cy={p.ny}
                        r={p.r}
                        fill="none"
                        stroke={GREY}
                        strokeWidth="1.25"
                        strokeDasharray="3 2"
                      />
                    )}
                    {p.prev && (
                      <circle
                        data-testid="quiver-prev"
                        cx={p.prev.x}
                        cy={p.prev.y}
                        r="2.5"
                        fill={p.bad ? DANGER : "var(--color-muted)"}
                      />
                    )}
                    {arrow}
                  </g>
                );
              })}
              {/* Labels above every mark, so no circle covers one. */}
              {labels.map(({ p, text, x0, ly }) => (
                <text
                  key={p.h.repo}
                  x={x0}
                  y={ly}
                  fontSize={LABEL}
                  className="font-mono"
                  fill="var(--color-ink)"
                  stroke="var(--color-panel)"
                  strokeWidth="4"
                  strokeLinejoin="round"
                  paintOrder="stroke"
                >
                  {text}
                </text>
              ))}
            </g>
            {hits.map((p) => (
              <circle
                key={p.h.repo}
                data-testid="quiver-hit"
                data-repo={p.h.repo}
                role="img"
                tabIndex={0}
                aria-label={[
                  p.h.repo,
                  ...tipRows(p.h).map(([k, v]) => `${k}: ${v}`),
                ].join("; ")}
                cx={p.nx}
                cy={p.ny}
                r={Math.max(p.r, 12)}
                fill="transparent"
                strokeWidth="2"
                className="outline-none focus-visible:stroke-accent-strong"
                onMouseEnter={() => show(p)}
                onMouseLeave={() => setHover(null)}
                onFocus={() => show(p)}
                onBlur={() => setHover(null)}
              />
            ))}
          </svg>
          {hover &&
            shown &&
            createPortal(
              <span
                className="pointer-events-none fixed z-50"
                style={{ left: hover.left, top: hover.top }}
              >
                <Tip title={shown.h.repo} rows={tipRows(shown.h)} />
              </span>,
              document.body,
            )}
        </div>
      </div>
      <p className="mt-1.5 mb-0 text-xs text-muted">
        {t("quiver.legend")}
        {unplotted && ` ${unplotted}`}
      </p>
    </div>
  );
}
