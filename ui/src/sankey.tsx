// A two-column Sankey, hand-rolled: nodes on the left flow into nodes on
// the right, band width proportional to the value. Every value, label and
// share arrives from the backend; sankey-layout.ts only lays the bands out.

import { type ReactNode, useId, useMemo, useState } from "react";
import { Tip } from "./charts";
import { H, layout, NW, W, X0, X1 } from "./sankey-layout";

export type SankeyNode = {
  key: string;
  label: string;
  // The line under the label: the node's value, formatted.
  value: string;
  color: string;
  // Opacity of a band's gradient at this node.
  linkOpacity: number;
  // Hatched rather than filled: time without a verdict.
  hatch?: boolean;
  danger?: boolean;
  // Bands into this node wear its colour along their whole length, no
  // gradient from the source: a failed stream reads red at a glance.
  solid?: boolean;
};

export type SankeyLink = {
  from: string;
  to: string;
  seconds: number;
  title: string;
  rows: [string, ReactNode][];
};

// The height a band answers the mouse over, however thin it is drawn.
const HIT = 6;
const MID = (X0 + NW + X1) / 2;

const path = ({ h, sy, ty }: { h: number; sy: number; ty: number }) =>
  `M${X0 + NW},${sy}C${MID},${sy} ${MID},${ty} ${X1},${ty}L${X1},${ty + h}C${MID},${ty + h} ${MID},${sy + h} ${X0 + NW},${sy + h}Z`;

// The caller decides whether there is anything to draw.
export function Sankey({
  left,
  right,
  links,
  ariaLabel,
}: {
  left: SankeyNode[];
  right: SankeyNode[];
  links: SankeyLink[];
  ariaLabel: string;
}) {
  // By link, not by index: a new period's links keep no stale hover.
  const [hover, setHover] = useState<string | null>(null);
  // url(#…) breaks on the colons React puts in an id.
  const uid = useId().replace(/[^a-zA-Z0-9_-]/g, "");
  // The geometry follows the data only; a hover changes opacity alone.
  const geo = useMemo(() => {
    const nodes = new Map([...left, ...right].map((n) => [n.key, n]));
    const g = layout(
      left.map((n) => n.key),
      right.map((n) => n.key),
      links,
    );
    const bands = g.bands.map((b) => ({
      ...b,
      id: `${b.from}>${b.to}`,
      link: links[b.index],
      a: nodes.get(b.from)!,
      z: nodes.get(b.to)!,
      d: path(b),
    }));
    const slots = (side: SankeyNode[], m: typeof g.left) =>
      side.map((node) => ({ node, ...m.get(node.key)! }));
    return { bands, L: slots(left, g.left), R: slots(right, g.right) };
  }, [left, right, links]);
  const shown = geo.bands.find((d) => d.id === hover);
  return (
    <div className="relative mt-3.5 max-w-[860px]">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        className="chart h-auto w-full overflow-visible"
        role="img"
        aria-label={ariaLabel}
      >
        <defs>
          <pattern
            id={`${uid}-hatch`}
            width="5"
            height="5"
            patternUnits="userSpaceOnUse"
            patternTransform="rotate(45)"
          >
            <rect width="5" height="5" fill="var(--color-panel)" />
            <line
              x1="0"
              y1="0"
              x2="0"
              y2="5"
              stroke="var(--color-faint)"
              strokeWidth="2"
            />
          </pattern>
          {geo.bands.map(({ id, a, z }, i) => {
            const up = hover === id ? 0.3 : 0;
            return (
              <linearGradient
                key={id}
                id={`${uid}-g${i}`}
                gradientUnits="userSpaceOnUse"
                x1={X0 + NW}
                x2={X1}
                y1="0"
                y2="0"
              >
                <stop
                  offset="0"
                  stopColor={a.color}
                  stopOpacity={Math.min(1, a.linkOpacity + up)}
                />
                <stop
                  offset="1"
                  stopColor={z.color}
                  stopOpacity={Math.min(1, z.linkOpacity + up)}
                />
              </linearGradient>
            );
          })}
        </defs>
        {geo.bands.map(({ id, z, d }, i) => (
          <path
            key={id}
            data-testid="sankey-link"
            d={d}
            fill={z.solid ? z.color : `url(#${uid}-g${i})`}
            fillOpacity={
              z.solid
                ? Math.min(1, z.linkOpacity + (hover === id ? 0.15 : 0))
                : undefined
            }
            stroke="var(--color-panel)"
            strokeWidth="0.8"
            pointerEvents="none"
          />
        ))}
        {/* Thin bands answer the mouse over HIT px, the drawn ones none. */}
        {geo.bands.map(({ id, d, h }) => (
          <path
            key={id}
            data-testid="sankey-hit"
            d={d}
            fill="transparent"
            stroke="transparent"
            strokeWidth={Math.max(0, HIT - h)}
            onMouseEnter={() => setHover(id)}
            onMouseLeave={() => setHover(null)}
          />
        ))}
        {geo.L.map((p) => (
          <g key={p.node.key}>
            <rect
              x={X0}
              y={p.y}
              width={NW}
              height={Math.max(2, p.h)}
              rx="3"
              fill={p.node.color}
            />
            <text
              x={X0 - 8}
              y={p.labelY}
              textAnchor="end"
              fontSize="12"
              fill="var(--color-ink-dim)"
            >
              {p.node.label}
            </text>
            <text
              x={X0 - 8}
              y={p.labelY + 14}
              textAnchor="end"
              fontSize="10.5"
              fill="var(--color-muted)"
            >
              {p.node.value}
            </text>
          </g>
        ))}
        {geo.R.map((p) => (
          <g key={p.node.key}>
            <rect
              x={X1}
              y={p.y}
              width={NW}
              height={Math.max(2, p.h)}
              rx="3"
              fill={p.node.hatch ? `url(#${uid}-hatch)` : p.node.color}
              stroke={p.node.hatch ? p.node.color : undefined}
            />
            <text
              x={X1 + NW + 8}
              y={p.labelY}
              fontSize="12"
              fill={
                p.node.danger
                  ? "var(--color-danger-ink)"
                  : "var(--color-ink-dim)"
              }
            >
              {p.node.label}
            </text>
            <text
              x={X1 + NW + 8}
              y={p.labelY + 14}
              fontSize="10.5"
              fill="var(--color-muted)"
            >
              {p.node.value}
            </text>
          </g>
        ))}
      </svg>
      {shown && (
        <div
          className="absolute size-0"
          style={{
            left: `${(MID / W) * 100}%`,
            top: `${((shown.sy + shown.ty + shown.h) / 2 / H) * 100}%`,
          }}
        >
          <Tip title={shown.link.title} rows={shown.link.rows} />
        </div>
      )}
    </div>
  );
}
