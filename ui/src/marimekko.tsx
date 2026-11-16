// The open PRs as a Marimekko: a column per repo, as wide as its open PRs,
// split by what they wait on. The widest column nearly all first review
// shows at a glance. Repos and counts as the backend sent them; the shares
// are geometry only, never shown as numbers.

import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { OpenPRs, PRWaiting } from "./api";
import { Tip } from "./charts";
import { num, pct } from "./format";
import { Legend, Swatch } from "./ui";

const H = 170;
// The count row above the bars.
const TOP = 20;
// Px per open PR at least: a one-PR column still holds its count and the
// long-open mark; past that the chart scrolls sideways.
const UNIT = 22;
const GAP = 3;
// Y axis 34px + 8px gap.
const AXIS = 42;
// Room left of the axis for the first column's name: 140px at 40° reach
// ~115px left of its column's centre.
const PAD = 64;
// The 2px gap between segments only on segments taller than it.
const SEG_GAP_MIN = 4;
const SEP = `${String.fromCharCode(0x2002)}·${String.fromCharCode(0x2002)}`;

// A segment fades from the series' token at its top to 55 % at its foot;
// a draft is grey, its author's, not the team's.
const FILL: Record<PRWaiting, { bar: string; swatch: string }> = {
  first_review: { bar: "from-s1 to-s1/55", swatch: "bg-s1" },
  review: { bar: "from-s3 to-s3/55", swatch: "bg-s3" },
  changes: { bar: "from-s4 to-s4/55", swatch: "bg-s4" },
  merge: { bar: "from-s2 to-s2/55", swatch: "bg-s2" },
  draft: { bar: "from-other to-other/55", swatch: "bg-other" },
};

export function OpenMekko({
  o,
  waits,
}: {
  o: OpenPRs;
  // Bottom to top.
  waits: PRWaiting[];
}) {
  const { t } = useTranslation();
  const [hover, setHover] = useState<number | null>(null);
  // Every repo the backend sends has an open PR.
  const repos = o.repos;
  if (!repos.length) return null;
  const word = (k: PRWaiting) => t(`facts.prWaiting.${k}`);
  // Layout only: the width the columns need at UNIT px per PR.
  const units = repos.reduce((a, r) => a + r.open, 0);
  const minWidth = PAD + AXIS + units * UNIT + (repos.length - 1) * GAP;
  return (
    <div className="mt-4">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1.5">
        <div className="text-[12.5px] text-muted">{t("open.mekko.head")}</div>
        <Legend>
          {waits.map((k) => (
            <Swatch
              key={k}
              className={FILL[k].swatch}
              label={
                k === "draft"
                  ? `${word(k)} (${t("open.mekko.draft")})`
                  : word(k)
              }
            />
          ))}
          <span className="inline-flex items-center gap-1.5">
            <span
              aria-hidden="true"
              className="size-[7px] rounded-full bg-ochre"
            />
            {t("open.mekko.longMark", { days: num(o.long_days) })}
          </span>
        </Legend>
      </div>
      <div className="mt-2 overflow-x-auto">
        <div
          className="flex gap-2"
          style={{ minWidth, paddingLeft: PAD }}
          role="group"
          aria-label={t("open.mekko.aria")}
        >
          <div
            className="relative w-[34px] flex-none"
            style={{ height: TOP + H }}
          >
            {[0, 0.5, 1].map((s) => (
              <span
                key={s}
                className="absolute right-0 text-[11px] whitespace-nowrap text-muted"
                style={{ top: TOP + H - s * H - 7 }}
              >
                {pct(s)}
              </span>
            ))}
          </div>
          <div className="relative min-w-0 flex-1">
            {[0, 0.5, 1].map((s) => (
              <span
                key={s}
                aria-hidden="true"
                className={`absolute inset-x-0 border-t ${s === 0 ? "border-axis" : "border-grid-minor"}`}
                style={{ top: TOP + H - s * H }}
              />
            ))}
            <div className="relative flex" style={{ gap: GAP }}>
              {repos.map((r, i) => {
                const present = waits.filter((k) => r.waiting[k] > 0);
                const top = present[present.length - 1];
                // Top to bottom, as the column reads.
                const rows: [string, string][] = [...present]
                  .reverse()
                  .map((k) => {
                    const long = r.long_waiting[k];
                    const of = t("open.mekko.of", {
                      n: num(r.waiting[k]),
                      open: num(r.open),
                      count: r.open,
                    });
                    return [
                      word(k),
                      long > 0
                        ? `${of}${SEP}${t("open.mekko.long", {
                            long: num(long),
                            days: num(o.long_days),
                          })}`
                        : of,
                    ];
                  });
                return (
                  <div
                    key={r.repo}
                    data-testid="mekko-col"
                    tabIndex={0}
                    aria-label={`${r.repo}: ${rows.map(([k, v]) => `${k} ${v}`).join(", ")}`}
                    onMouseEnter={() => setHover(i)}
                    onMouseLeave={() => setHover(null)}
                    onFocus={() => setHover(i)}
                    onBlur={() => setHover(null)}
                    className="relative min-w-0 rounded-[3px] outline-none focus-visible:outline-2 focus-visible:outline-accent-strong"
                    // As wide as its open PRs.
                    style={{ flex: `${r.open} 1 0` }}
                  >
                    <div
                      className="text-center text-[11.5px] whitespace-nowrap text-ink tabular-nums"
                      style={{ height: TOP }}
                    >
                      {num(r.open)}
                    </div>
                    <div
                      className="flex flex-col-reverse"
                      style={{ height: H }}
                    >
                      {present.map((k) => {
                        const h = (r.waiting[k] / r.open) * H;
                        return (
                          <div
                            key={k}
                            data-testid="mekko-seg"
                            className={`box-border flex-none bg-linear-to-b ${FILL[k].bar} ${k === top ? "rounded-t" : ""} ${
                              k !== present[0] && h >= SEG_GAP_MIN
                                ? "border-b-2 border-panel"
                                : ""
                            }`}
                            style={{ height: h }}
                          />
                        );
                      })}
                    </div>
                    {hover === i && (
                      // Anchored at the bars' foot: inside the scroll box,
                      // which clips anything above it.
                      <div
                        className="absolute left-1/2 h-0 w-0"
                        style={{ top: TOP + H }}
                      >
                        <Tip title={r.repo} rows={rows} />
                      </div>
                    )}
                    <div className="flex h-5 items-center justify-center">
                      {r.long > 0 && (
                        <span className="inline-flex items-center gap-1 text-[11px] whitespace-nowrap text-ochre tabular-nums">
                          <span
                            aria-hidden="true"
                            className="size-[7px] rounded-full bg-ochre"
                          />
                          {num(r.long)}
                        </span>
                      )}
                    </div>
                    <div className="relative h-[100px]">
                      <span
                        title={r.repo}
                        className="absolute top-0 right-1/2 max-w-[140px] origin-top-right -rotate-40 truncate font-mono text-[10.5px] whitespace-nowrap text-ink-dim"
                      >
                        {r.repo}
                      </span>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
