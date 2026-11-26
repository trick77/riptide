// One number per repo on a strip: a dot per repo at the position the
// backend computed (the better end left), coloured by the backend's
// verdict against the team's threshold, the typical repo marked, repos
// with nothing released set apart at the right end. Under it, in plain
// words, the typical repo and what the team's number hides. The UI lays
// the dots out and formats; every value, position and verdict is the
// backend's.

import type { TFunction } from "i18next";
import { useId, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
  DotVerdict,
  Metrics,
  RepoDot,
  RepoSpread,
  RunKind,
  SpreadKey,
  SpreadUnit,
} from "./api";
import { dur, num, pct } from "./format";

// A value of a spread's unit, as the page writes it.
export function unitValue(t: TFunction, unit: SpreadUnit, v: number): string {
  switch (unit) {
    case "per_week":
      // Once a month is 0.25 a week: a tenth would round it to 0.3.
      return t("repos.unit.perWeek", {
        n: num(v, v < 1 ? 2 : v < 10 ? 1 : 0),
      });
    // The backend rounds days to a tenth: 0 is under a tenth, not none.
    case "days":
      return t("units.days", {
        n: v === 0 ? `< ${num(0.1, 1)}` : num(v, Number.isInteger(v) ? 0 : 1),
      });
    case "working_days":
      return t("repos.unit.workingDays", {
        n: v === 0 ? `< ${num(0.1, 1)}` : num(v, Number.isInteger(v) ? 0 : 1),
      });
    case "lines":
      return t("repos.unit.lines", { n: num(v) });
    case "seconds":
      return dur(v);
    case "share":
      return pct(v);
  }
}

// A tick's label: the value without the unit's words where they repeat.
function tickLabel(t: TFunction, unit: SpreadUnit, v: number): string {
  if (v === 0) return "0";
  if (unit === "per_week" || unit === "lines") return num(v, v < 1 ? 2 : 0);
  return unitValue(t, unit, v);
}

export function spreadOf(
  m: Metrics,
  key: SpreadKey,
  kind: RunKind | null = null,
): RepoSpread | undefined {
  return m.repos.spreads.find((s) => s.key === key && s.kind === kind);
}

// Drawn at the column's own width, so dots and labels keep their size in a
// narrow tile and a wide one alike; this until it is measured.
const START_W = 320;
const R = 3.6;
// A dot's diameter plus a pixel: closer dots stack.
const D = 2 * R + 1;
const PAD = 8;
const APART_GAP = 18;

const FILL: Record<DotVerdict, string> = {
  breach: "var(--color-danger-ink)",
  hint: "var(--color-panel)",
  within: "var(--color-ink-dim)",
  neutral: "var(--color-ink-dim)",
};

type Placed = { dot: RepoDot; cx: number; cy: number };

// Dots stacked where they would overlap: each takes the lowest free row
// around the centre line, the order the backend sent them in.
function layout(
  dots: RepoDot[],
  x0: number,
  x1: number,
): { placed: Placed[]; rows: number } {
  const rowsOf = [0, -1, 1, -2, 2, -3, 3, -4, 4, -5, 5, -6, 6];
  const taken: { x: number; r: number }[] = [];
  const out: { dot: RepoDot; x: number; r: number }[] = [];
  for (const dot of dots) {
    if (dot.x == null) continue;
    const x = x0 + dot.x * (x1 - x0);
    let row = rowsOf[rowsOf.length - 1];
    for (const r of rowsOf) {
      if (!taken.some((p) => p.r === r && Math.abs(p.x - x) < D)) {
        row = r;
        break;
      }
    }
    taken.push({ x, r: row });
    out.push({ dot, x, r: row });
  }
  const rows = Math.max(1, ...out.map((p) => Math.abs(p.r)));
  return {
    placed: out.map((p) => ({ dot: p.dot, cx: p.x, cy: p.r * D })),
    rows,
  };
}

export function RepoStrip({
  s,
  label,
  onRepo,
}: {
  s: RepoSpread;
  // What the strip shows, for its heading and the screen reader.
  label: string;
  onRepo?: (repo: string) => void;
}) {
  const { t } = useTranslation();
  const tip = useId();
  const [active, setActive] = useState<number | null>(null);
  const wrap = useRef<HTMLDivElement>(null);
  const [W, setW] = useState(START_W);
  useLayoutEffect(() => {
    const el = wrap.current;
    if (!el) return;
    const set = () => {
      if (el.clientWidth > 0) setW(Math.max(200, Math.round(el.clientWidth)));
    };
    set();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(set);
    ro.observe(el);
    return () => ro.disconnect();
  }, [s.shown]);
  if (!s.shown) return null;
  const apart = s.dots.filter((d) => d.apart);
  const perRow = 5;
  // The set-apart dots' caption, a phrase that reads alone ("kein Release
  // im Zeitraum").
  const apartLabel = t(`repos.apart.${s.key}`, {
    defaultValue: t("repos.apart.releases"),
  });
  // Wide enough for its label too (about 5.2 px a character at 9.5 px), so
  // the axis' last tick stays clear of it and the label of the tile's edge.
  const apartW = apart.length
    ? Math.max(
        Math.min(apart.length, perRow) * 11 + 6,
        Math.ceil(apartLabel.length * 5.2),
      ) + APART_GAP
    : 0;
  const x0 = PAD;
  const x1 = W - PAD - apartW;
  const { placed, rows } = layout(s.dots, x0, x1);
  const apartRows = Math.ceil(apart.length / perRow);
  const top = 16;
  const band = Math.max(2 * rows + 1, apartRows * 1.4) * D + 4;
  const cy = top + band / 2;
  const axisY = top + band + 2;
  const H = axisY + 16;
  const at = (x: number) => x0 + x * (x1 - x0);
  const ax = x1 + APART_GAP;
  const all: Placed[] = placed.concat(
    apart.map((dot, i) => ({
      dot,
      cx: ax + (i % perRow) * 11 + 4,
      cy: (Math.floor(i / perRow) - (apartRows - 1) / 2) * 10,
    })),
  );
  const tipText = (d: RepoDot) =>
    d.apart || d.value == null
      ? apartLabel
      : `${unitValue(t, s.unit, d.value)} · ${t(`repos.n.${s.key}`, { count: d.n })}`;
  const shown = active != null ? all[active] : null;
  const typical = s.typical_x != null ? at(s.typical_x) : null;
  return (
    <div ref={wrap} className="relative mt-1.5">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        width={W}
        height={H}
        className="block max-w-full overflow-visible"
        role="group"
        aria-label={t("repos.stripAria", { label, n: all.length })}
      >
        <line
          x1={x0}
          x2={x1}
          y1={cy}
          y2={cy}
          stroke="var(--color-grid-minor)"
        />
        {s.mark && (
          <g>
            <line
              x1={at(s.mark.x)}
              x2={at(s.mark.x)}
              y1={top - 2}
              y2={axisY}
              stroke="var(--color-reference)"
              strokeDasharray="3 3"
            />
            <text
              x={at(s.mark.x) + (s.mark.x > 0.8 ? -3 : 3)}
              y={top + 1}
              textAnchor={s.mark.x > 0.8 ? "end" : "start"}
              fontSize="9.5"
              fill="var(--color-muted)"
            >
              {unitValue(t, s.unit, s.mark.value)}
            </text>
          </g>
        )}
        {typical != null && (
          <g aria-hidden="true">
            <line
              x1={typical}
              x2={typical}
              y1={top + 2}
              y2={axisY - 2}
              stroke="var(--color-accent-strong)"
              strokeWidth="1.5"
            />
            <text
              x={typical}
              y={top - 5}
              textAnchor={
                typical < 40 ? "start" : typical > x1 - 30 ? "end" : "middle"
              }
              fontSize="9.5"
              fill="var(--color-accent-strong)"
            >
              {t("repos.typicalMark")}
            </text>
          </g>
        )}
        {s.scale.ticks.map((tk) => (
          <g key={tk.value} aria-hidden="true">
            <line
              x1={at(tk.x)}
              x2={at(tk.x)}
              y1={axisY - 3}
              y2={axisY}
              stroke="var(--color-axis)"
            />
            <text
              x={at(tk.x)}
              y={axisY + 11}
              textAnchor={
                tk.x < 0.06 ? "start" : tk.x > 0.94 ? "end" : "middle"
              }
              fontSize="9.5"
              fill="var(--color-muted)"
            >
              {tickLabel(t, s.unit, tk.value)}
            </text>
          </g>
        ))}
        {apart.length > 0 && (
          <g aria-hidden="true">
            <line
              x1={x1 + APART_GAP / 2}
              x2={x1 + APART_GAP / 2}
              y1={top - 2}
              y2={axisY + 12}
              stroke="var(--color-grid)"
            />
            <text
              x={ax}
              y={axisY + 11}
              textAnchor="start"
              fontSize="9.5"
              fill="var(--color-danger-ink)"
            >
              {apartLabel}
            </text>
          </g>
        )}
        {all.map((p, i) => {
          const d = p.dot;
          const open = () => onRepo?.(d.repo);
          return (
            <circle
              key={d.repo}
              data-testid="strip-dot"
              data-verdict={d.apart ? "apart" : d.verdict}
              cx={p.cx.toFixed(1)}
              cy={(cy + p.cy).toFixed(1)}
              r={R}
              fill={d.apart ? "none" : FILL[d.verdict]}
              stroke={
                d.apart
                  ? "var(--color-danger-ink)"
                  : d.verdict === "hint"
                    ? "var(--color-ink-dim)"
                    : "var(--color-panel)"
              }
              strokeWidth={d.apart ? 1.6 : 1.2}
              // A hollow dot answers the pointer inside its ring too.
              pointerEvents="all"
              tabIndex={0}
              role="button"
              aria-label={`${d.repo}: ${tipText(d)}`}
              aria-describedby={active === i ? tip : undefined}
              className="cursor-pointer outline-none hover:stroke-ink focus-visible:stroke-accent-strong focus-visible:stroke-2"
              onMouseEnter={() => setActive(i)}
              onMouseLeave={() => setActive(null)}
              onFocus={() => setActive(i)}
              onBlur={() => setActive(null)}
              onClick={open}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  open();
                }
              }}
            />
          );
        })}
      </svg>
      {shown && (
        <div
          id={tip}
          role="tooltip"
          className="pointer-events-none absolute z-20 rounded-ui-sm border border-elevated-border bg-elevated px-2.5 py-1.5 text-[12.5px] leading-snug whitespace-nowrap text-ink-dim shadow-menu"
          style={{
            left: `${(shown.cx / W) * 100}%`,
            top: `calc(${((cy + shown.cy) / H) * 100}% - 8px)`,
            // Towards the strip's middle, so a long repo name stays over it.
            transform: `translate(${shown.cx / W > 0.5 ? "calc(-100% + 8px)" : "-8px"}, -100%)`,
          }}
        >
          <div className="font-mono text-[12px] text-elevated-ink">
            {shown.dot.repo}
          </div>
          <div>{tipText(shown.dot)}</div>
        </div>
      )}
      <SpreadLines s={s} />
    </div>
  );
}

// The typical repo and what the team's number hides, in plain words, and
// the repos too thin to place.
export function SpreadLines({ s }: { s: RepoSpread }) {
  const { t } = useTranslation();
  const lines: string[] = [];
  const cases = t(`repos.cases.${s.key}`);
  // Every placed repo at one value: that, no strip and no typical one.
  if (s.same != null)
    lines.push(
      t("repos.same", {
        count: s.judged,
        value: unitValue(t, s.unit, s.same),
      }),
    );
  if (s.apart > 0 && !s.shown)
    lines.push(t("repos.apartLine", { count: s.apart }));
  if (s.same == null && s.typical != null)
    lines.push(
      t("repos.typical", {
        value: unitValue(t, s.unit, s.typical),
        count: s.judged,
      }),
    );
  const n = s.note;
  if (n?.kind === "dominant" && n.repo)
    lines.push(t("repos.note.dominant", { repo: n.repo, share: pct(n.share) }));
  if (n?.kind === "far")
    lines.push(
      [
        t("repos.note.far", { count: n.near, of: n.of }),
        n.above > 0 && t("repos.note.above", { count: n.above }),
        n.below > 0 && t("repos.note.below", { count: n.below }),
      ]
        .filter(Boolean)
        .join(", ") + ".",
    );
  if (n?.kind === "pooled" && s.typical != null)
    lines.push(
      t(n.additive ? "repos.note.pooledAdded" : "repos.note.pooled", {
        cases,
        value: unitValue(t, s.unit, s.typical),
      }),
    );
  if (s.thin > 0) lines.push(t("repos.thin", { count: s.thin, cases }));
  if (!lines.length) return null;
  return (
    <div className="mt-0.5 flex flex-col gap-0.5 text-xs text-muted">
      {lines.map((l) => (
        <span key={l}>{l}</span>
      ))}
    </div>
  );
}

// A strip with its heading, as the tiles and cards carry it.
export function StripBlock({
  s,
  title,
  onRepo,
}: {
  s: RepoSpread | undefined;
  title: string;
  onRepo?: (repo: string) => void;
}) {
  if (!s || (!s.shown && s.same == null)) return null;
  return (
    <div className="mt-1.5 border-t border-border-soft pt-2">
      <div className="text-[11.5px] text-muted">{title}</div>
      {s.shown ? (
        <RepoStrip s={s} label={title} onRepo={onRepo} />
      ) : (
        <SpreadLines s={s} />
      )}
    </div>
  );
}
