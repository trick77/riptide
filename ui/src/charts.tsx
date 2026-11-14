// The charts, without a chart library: stacked week bars, a grouped
// histogram, weekly rate lines and horizontal bar lists. Thin marks, 4px
// rounded data ends on the baseline side away from it, a 2px panel gap
// between stacked segments, recessive grid, a tooltip on every mark.

import { type ReactNode, useState } from "react";
import { num } from "./format";

export type Series = { key: string; label: string; className: string };

function Tip({ title, rows }: { title: string; rows: [string, ReactNode][] }) {
  return (
    <div
      role="tooltip"
      className="pointer-events-none absolute bottom-[calc(100%+8px)] left-1/2 z-10 min-w-[150px] -translate-x-1/2 rounded-ui-sm border border-elevated-border bg-elevated px-2.5 py-2 text-[12.5px] leading-normal whitespace-nowrap text-ink-dim shadow-menu"
    >
      <div className="font-medium text-elevated-ink">{title}</div>
      {rows.map(([k, v]) => (
        <div key={k} className="flex justify-between gap-3.5">
          <span>{k}</span>
          <span className="tabular-nums">{v}</span>
        </div>
      ))}
    </div>
  );
}

// Every n-th week label, so 26 weeks do not overprint each other.
function labelEvery(n: number): number {
  return n <= 6 ? 1 : n <= 13 ? 2 : 4;
}

function niceMax(v: number): number {
  if (v <= 3) return 3;
  if (v <= 10) return Math.ceil(v / 2) * 2;
  const step = Math.pow(10, Math.floor(Math.log10(v)));
  return Math.ceil(v / step) * step;
}

const H = 170;

export function WeekStack({
  labels,
  series,
  values,
  ariaLabel,
}: {
  labels: string[];
  // Bottom to top.
  series: Series[];
  values: Record<string, number>[];
  ariaLabel: string;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const totals = values.map((v) =>
    series.reduce((a, s) => a + (v[s.key] ?? 0), 0),
  );
  const max = niceMax(Math.max(0, ...totals));
  const ticks = [0, Math.round(max / 2), max];
  const every = labelEvery(labels.length);
  const gap = labels.length > 13 ? 3 : labels.length > 6 ? 6 : 14;
  return (
    <div className="mt-3.5 flex gap-2" role="img" aria-label={ariaLabel}>
      <div className="relative w-[26px] flex-none" style={{ height: H }}>
        {ticks.map((t) => (
          <span
            key={t}
            className="absolute right-0 text-[11px] text-muted"
            style={{ top: H - (t / max) * H - 7 }}
          >
            {num(t)}
          </span>
        ))}
      </div>
      <div className="min-w-0 flex-1">
        <div className="relative" style={{ height: H }}>
          {ticks.map((t) => (
            <span
              key={t}
              aria-hidden="true"
              className={`absolute inset-x-0 border-t ${t === 0 ? "border-axis" : "border-grid-minor"}`}
              style={{ top: H - (t / max) * H }}
            />
          ))}
          <div className="absolute inset-0 flex items-end" style={{ gap }}>
            {values.map((v, i) => {
              const present = series.filter((s) => (v[s.key] ?? 0) > 0);
              const top = present[present.length - 1]?.key;
              return (
                <div
                  key={i}
                  data-testid="week-col"
                  onMouseEnter={() => setHover(i)}
                  onMouseLeave={() => setHover(null)}
                  className="relative flex h-full min-w-0 flex-1 flex-col-reverse"
                >
                  {series.map((s) => {
                    const n = v[s.key] ?? 0;
                    if (n === 0) return null;
                    return (
                      <div
                        key={s.key}
                        className={`box-border flex-none ${s.className} ${s.key === top ? "rounded-t" : ""} ${
                          s.key !== present[0].key
                            ? "border-b-2 border-panel"
                            : ""
                        }`}
                        style={{ height: (n / max) * H }}
                      />
                    );
                  })}
                  {hover === i && (
                    <Tip
                      title={labels[i]}
                      rows={[...series]
                        .reverse()
                        .map((s) => [s.label, num(v[s.key] ?? 0)])}
                    />
                  )}
                </div>
              );
            })}
          </div>
        </div>
        <div className="mt-1.5 flex" style={{ gap }}>
          {labels.map((l, i) => (
            <span
              key={i}
              className="min-w-0 flex-1 overflow-hidden text-center text-[11px] whitespace-nowrap text-muted"
            >
              {i % every === 0 ? l : ""}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

// Side-by-side bars per bucket, one per series; the first series carries
// its value above the bar.
export function GroupedBars({
  labels,
  series,
  values,
  ariaLabel,
}: {
  labels: string[];
  series: Series[];
  // values[series][bucket]
  values: number[][];
  ariaLabel: string;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const max = Math.max(1, ...values.flat());
  return (
    <div role="img" aria-label={ariaLabel}>
      <div className="relative mt-5 flex h-[170px] items-end gap-3.5 border-b border-axis">
        {labels.map((l, i) => (
          <div
            key={l}
            onMouseEnter={() => setHover(i)}
            onMouseLeave={() => setHover(null)}
            className="relative flex h-full min-w-0 flex-1 items-end justify-center gap-[3px]"
          >
            {series.map((s, j) => (
              <div
                key={s.key}
                className={`relative max-w-[46px] flex-1 rounded-t ${s.className}`}
                style={{ height: (values[j][i] / max) * 150 }}
              >
                {j === 0 && (
                  <span className="absolute -top-[18px] left-1/2 -translate-x-1/2 text-[11.5px] text-ink-dim tabular-nums">
                    {values[j][i]}
                  </span>
                )}
              </div>
            ))}
            {hover === i && (
              <Tip
                title={l}
                rows={series.map((s, j) => [s.label, num(values[j][i])])}
              />
            )}
          </div>
        ))}
      </div>
      <div className="mt-1.5 flex gap-3.5">
        {labels.map((l) => (
          <span
            key={l}
            className="min-w-0 flex-1 text-center text-[11.5px] text-muted"
          >
            {l}
          </span>
        ))}
      </div>
    </div>
  );
}

export type Line = {
  key: string;
  label: string;
  stroke: string;
  values: (number | null)[];
};

// Weekly rates, 0–100 %, one line per series. A week without data is a
// gap in its line, never a zero. `gapUntil` hatches the weeks before the
// data is reliable.
export function RateLines({
  labels,
  lines,
  gapUntil,
  format,
  ariaLabel,
}: {
  labels: string[];
  lines: Line[];
  gapUntil: number;
  format: (v: number) => string;
  ariaLabel: string;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const n = labels.length;
  const LH = 160;
  const x = (i: number) => ((i + 0.5) / n) * 1000;
  const y = (v: number) => LH - v * LH;
  const every = labelEvery(n);
  return (
    <div className="mt-3.5 flex gap-2">
      <div className="relative w-[38px] flex-none" style={{ height: LH }}>
        {[1, 0.5, 0].map((t) => (
          <span
            key={t}
            className="absolute right-0 text-[11px] text-muted"
            style={{ top: y(t) - 7 }}
          >
            {format(t)}
          </span>
        ))}
      </div>
      <div className="min-w-0 flex-1">
        <div className="relative" style={{ height: LH }}>
          {[1, 0.5, 0].map((t) => (
            <span
              key={t}
              aria-hidden="true"
              className={`absolute inset-x-0 border-t ${t === 0 ? "border-axis" : t === 1 ? "border-grid" : "border-grid-minor"}`}
              style={{ top: y(t) }}
            />
          ))}
          {gapUntil > 0 && (
            <div
              className="texture-gap absolute top-0 bottom-px left-0 flex items-center justify-center"
              style={{ width: `${(gapUntil / n) * 100}%` }}
            >
              <span className="rounded bg-panel px-1.5 py-px text-[11.5px] text-muted">
                keine verlässlichen Daten
              </span>
            </div>
          )}
          <svg
            role="img"
            aria-label={ariaLabel}
            viewBox={`0 0 1000 ${LH}`}
            preserveAspectRatio="none"
            className="absolute inset-0 h-full w-full overflow-visible"
          >
            {lines.map((l) => {
              // A path per run of consecutive weeks with data.
              const segs: string[] = [];
              let cur = "";
              l.values.forEach((v, i) => {
                if (v == null) {
                  if (cur) segs.push(cur);
                  cur = "";
                } else
                  cur += `${cur ? "L" : "M"}${x(i).toFixed(1)} ${y(v).toFixed(1)} `;
              });
              if (cur) segs.push(cur);
              return segs.map((d, k) => (
                <path
                  key={`${l.key}${k}`}
                  d={d}
                  fill="none"
                  stroke={l.stroke}
                  strokeWidth="2"
                  strokeLinejoin="round"
                  strokeLinecap="round"
                  vectorEffect="non-scaling-stroke"
                />
              ));
            })}
          </svg>
          {lines.map((l) =>
            l.values.map((v, i) =>
              v == null ? null : (
                <span
                  key={`${l.key}-${i}`}
                  aria-hidden="true"
                  className="absolute size-2 -translate-1/2 rounded-full ring-2 ring-panel"
                  style={{
                    left: `${x(i) / 10}%`,
                    top: y(v),
                    background: l.stroke,
                  }}
                />
              ),
            ),
          )}
          <div className="absolute inset-0 flex">
            {labels.map((lab, i) => (
              <div
                key={i}
                className="relative h-full flex-1"
                onMouseEnter={() => setHover(i)}
                onMouseLeave={() => setHover(null)}
              >
                {hover === i && (
                  <>
                    <span
                      aria-hidden="true"
                      className="absolute inset-y-0 left-1/2 border-l border-grid"
                    />
                    <Tip
                      title={lab}
                      rows={lines.map((l) => [
                        l.label,
                        l.values[i] == null ? "–" : format(l.values[i]!),
                      ])}
                    />
                  </>
                )}
              </div>
            ))}
          </div>
        </div>
        <div className="mt-1.5 flex">
          {labels.map((l, i) => (
            <span
              key={i}
              className="min-w-0 flex-1 overflow-hidden text-center text-[11px] whitespace-nowrap text-muted"
            >
              {i % every === 0 ? l : ""}
            </span>
          ))}
        </div>
      </div>
    </div>
  );
}

// A ranked list: label, a bar to the largest value, the value.
export function BarList({
  rows,
  format,
  barClass = "bg-s1",
}: {
  rows: { label: string; value: number; note?: string }[];
  format: (v: number) => string;
  barClass?: string;
}) {
  const max = Math.max(...rows.map((r) => r.value), 0) || 1;
  return (
    <div className="mt-3.5 flex flex-col gap-2.5">
      {rows.map((r) => (
        <div
          key={r.label}
          title={`${r.label}: ${format(r.value)}${r.note ? ` · ${r.note}` : ""}`}
        >
          <div className="flex justify-between gap-2.5 text-[12.5px]">
            <span className="overflow-hidden font-mono text-[11.5px] text-ellipsis whitespace-nowrap text-ink-dim">
              {r.label}
            </span>
            <span className="whitespace-nowrap text-ink tabular-nums">
              {format(r.value)}
              {r.note && <span className="text-muted"> · {r.note}</span>}
            </span>
          </div>
          <div className="mt-1 h-1.5 overflow-hidden rounded-[3px] bg-border-soft">
            <div
              className={`h-full rounded-[3px] ${barClass}`}
              style={{ width: `${(r.value / max) * 100}%` }}
            />
          </div>
        </div>
      ))}
    </div>
  );
}
