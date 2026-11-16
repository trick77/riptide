// A kind's failure rate per repo and week: a flaky repo shows as a red
// row, a shared outage as a red column. Rows, cells, classes and the
// legend's bounds come from the backend; this only paints them.

import {
  type CSSProperties,
  type MouseEvent,
  type ReactNode,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import type { HeatCell, HeatClass, Hotspots, KindHotspots, Week } from "./api";
import { labelEvery } from "./charts";
import { num, pct } from "./format";
import { SrTable } from "./ui";

type Look = { className: string; style?: CSSProperties };

// Static class names per step, so Tailwind generates them: 0 without a
// failure, then one per class of heat_steps.
const STEP_BG = [
  "bg-cell-empty",
  "bg-bad-1",
  "bg-bad-2",
  "bg-bad-3",
  "bg-bad-4",
];

// A faint top highlight on the filled cells.
const SHINE: CSSProperties = {
  backgroundImage:
    "linear-gradient(to bottom, rgba(255, 255, 255, 0.1), rgba(255, 255, 255, 0) 50%)",
};

const SHAPE = "box-border rounded-[2.5px]";

// Runs, but too few for a verdict: hatched, never a hue.
const NO_VERDICT: Look = {
  className: SHAPE,
  style: {
    background:
      "repeating-linear-gradient(135deg, var(--color-grid) 0 1.5px, var(--color-border-soft) 1.5px 4px)",
  },
};
// A week without runs: an outline only, unlike the filled 0 % cell.
const NO_RUNS: Look = {
  className: `${SHAPE} border border-dashed border-grid`,
};
// Before the run data starts: the faint hatch the other week charts use.
const NO_DATA: Look = { className: `${SHAPE} texture-gap` };

let warned = false;

// The look of a class. A step past the tokens (the backend sent more cut
// points than there are colours) takes the top colour and warns once,
// rather than taking the page down.
export function stepLook(step: number): Look {
  let bg = STEP_BG[step];
  if (!bg) {
    if (!warned) console.warn(`heatmap: no colour for step ${step}`);
    warned = true;
    bg = STEP_BG[STEP_BG.length - 1];
  }
  return { className: `${SHAPE} ${bg}`, style: step > 0 ? SHINE : undefined };
}

function cellLook(c: HeatCell): Look {
  if (c.no_data) return NO_DATA;
  if (c.runs === 0) return NO_RUNS;
  if (c.step == null) return NO_VERDICT;
  return stepLook(c.step);
}

// A title separator: the middot with an en space each side.
const EN = String.fromCharCode(0x2002);
const SEP = `${EN}·${EN}`;

const TIP_W = 290;

// Where the tooltip's left edge sits, from its cell's: centred over the
// cell, but held inside the screen, so it never widens the page on a phone.
function tipLeft(e: MouseEvent<HTMLElement>): number {
  const r = e.currentTarget.getBoundingClientRect();
  const want = r.left + r.width / 2 - TIP_W / 2;
  const left = Math.max(8, Math.min(want, window.innerWidth - 8 - TIP_W));
  return left - r.left;
}

function HeatTip({
  title,
  lines,
  left,
}: {
  title: string;
  lines: string[];
  left: number;
}) {
  return (
    <div
      role="tooltip"
      className="pointer-events-none absolute bottom-[calc(100%+8px)] z-10 rounded-ui-sm border border-elevated-border bg-elevated px-2.5 py-2 text-[12.5px] leading-normal text-ink-dim shadow-menu"
      style={{ left, width: TIP_W }}
    >
      <div className="font-medium text-elevated-ink">{title}</div>
      {lines.map((l) => (
        <div key={l} className="tabular-nums">
          {l}
        </div>
      ))}
    </div>
  );
}

// A legend entry, drawn exactly as the cells are, only smaller.
function Swatch({ look, children }: { look: Look; children: ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <i
        aria-hidden="true"
        data-testid="heat-swatch"
        className={`inline-block size-2.5 ${look.className}`}
        style={look.style}
      />
      {children}
    </span>
  );
}

export function FailureHeatmap({
  k,
  h,
  weeks,
}: {
  k: KindHotspots;
  h: Hotspots;
  weeks: Week[];
}) {
  const { t } = useTranslation();
  const [hover, setHover] = useState<[number, number, number] | null>(null);
  const rows = k.heatmap;
  if (!rows.length) return null;
  const n = weeks.length;
  const every = labelEvery(n);
  const kind = t(`build.kind.${k.kind}`);
  const label = (r: (typeof rows)[number]) =>
    r.other ? t("heat.other") : r.repo;
  const cw = (wi: number) => t("common.cw", { kw: weeks[wi]?.kw });

  const lines = (cell: HeatCell): string[] => {
    if (cell.no_data) return [t("chart.noData")];
    if (cell.runs === 0) return [t("heat.none")];
    const failed = t("heat.failed", { failed: cell.failed, count: cell.runs });
    return cell.step == null
      ? [failed, t("heat.noVerdict", { min: h.heat_min_runs })]
      : [`${failed} (${pct(cell.rate)})`];
  };

  const classLabel = (c: HeatClass) => {
    if (c.step === 0) return t("heat.zero");
    if (c.from == null) return t("heat.below", { v: pct(c.to) });
    if (c.to == null) return t("heat.from", { v: pct(c.from) });
    return `${num(c.from * 100)}–${pct(c.to)}`;
  };

  return (
    <div className="mt-5">
      <div className="text-[13.5px] font-medium text-ink">
        {t("heat.title")}
      </div>
      <div className="text-[12.5px] text-muted">{t("heat.sub")}</div>
      {/* The numbers for a screen reader: the grid below only draws them. */}
      <SrTable caption={t("heat.aria", { kind })}>
        <thead>
          <tr>
            <th scope="col">{t("heat.colRepo")}</th>
            {weeks.map((w, wi) => (
              <th key={w.start} scope="col">
                {cw(wi)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.other ? "" : r.repo}>
              <th scope="row">{label(r)}</th>
              {r.weeks.map((cell, wi) => (
                <td key={wi}>{lines(cell).join(", ")}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </SrTable>
      <div
        aria-hidden="true"
        data-testid="heat-map"
        className="mt-3 grid gap-[2px]"
        style={{
          // Cells stay squarish over 4 weeks too, not bars across the card.
          gridTemplateColumns: `minmax(80px, max-content) repeat(${n}, minmax(0, 48px))`,
        }}
      >
        {rows.map((r, ri) => (
          <div
            key={r.other ? "" : r.repo}
            className="contents"
            data-testid="heat-row"
          >
            <div
              className={`max-w-[120px] truncate pr-2 text-right text-[11px] leading-[18px] sm:max-w-[240px] ${r.other ? "text-muted" : "font-mono text-ink-dim"}`}
              title={label(r)}
            >
              {label(r)}
            </div>
            {r.weeks.map((cell, wi) => {
              const look = cellLook(cell);
              const on = hover?.[0] === ri && hover?.[1] === wi;
              return (
                <div
                  key={wi}
                  data-testid="heat-cell"
                  className="relative min-w-0"
                  onMouseEnter={(e) => setHover([ri, wi, tipLeft(e)])}
                  onMouseLeave={() => setHover(null)}
                >
                  <div
                    className={`h-[18px] ${look.className}`}
                    style={look.style}
                  />
                  {on && (
                    <HeatTip
                      title={[label(r), cw(wi), kind].join(SEP)}
                      lines={lines(cell)}
                      left={hover[2]}
                    />
                  )}
                </div>
              );
            })}
          </div>
        ))}
        <div />
        {weeks.map((w, i) => (
          <span
            key={w.start}
            // Half the labels on a phone, where the columns are narrow.
            className={`mt-1 min-w-0 text-center text-[11px] whitespace-nowrap text-muted ${n > 13 && i % (2 * every) ? "max-sm:invisible" : ""}`}
          >
            {i % every === 0 ? cw(i) : ""}
          </span>
        ))}
      </div>
      <div className="mt-2.5 flex flex-wrap gap-x-3.5 gap-y-1.5 text-[11.5px] text-muted">
        {h.heat_classes.map((c) => (
          <Swatch key={c.step} look={stepLook(c.step)}>
            {classLabel(c)}
          </Swatch>
        ))}
        <Swatch look={NO_VERDICT}>
          {t("heat.noVerdict", { min: h.heat_min_runs })}
        </Swatch>
        <Swatch look={NO_RUNS}>{t("heat.none")}</Swatch>
        {rows.some((r) => r.weeks.some((c) => c.no_data)) && (
          <Swatch look={NO_DATA}>{t("chart.noData")}</Swatch>
        )}
      </div>
    </div>
  );
}
