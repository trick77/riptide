// The open PRs as a Marimekko: a column per repo, as wide as its open PRs,
// split by what they wait on. The widest column nearly all first review
// shows at a glance. Repos and counts as the backend sent them; the shares
// are geometry only, never shown as numbers.

import { Fragment, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { OpenPRs, OpenRepo, PRWaiting } from "./api";
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
// The 2px gap between segments only on segments taller than it.
const SEG_GAP_MIN = 4;
const SEP = `${String.fromCharCode(0x2002)}·${String.fromCharCode(0x2002)}`;
// Narrower than this many characters a name is no longer read, only
// guessed at.
const MIN_CHARS = 7;
// Until measured (and in jsdom): MIN_CHARS of 10.5px mono, ~6.3px each,
// and 2px padding either side.
const GUESS_MIN = MIN_CHARS * 6.4 + 4;
// A column that lost its name gets it back only this much wider: its name
// and its number differ in height, and the page scrollbar that difference
// brings or takes changes the width again.
const HYSTERESIS = 24;
// The tooltip's least distance to the scroll box's sides.
const TIP_INSET = 4;

// The pieces a repo name wraps between: a break after "-", "_" and "/".
function pieces(name: string): string[] {
  return name.split(/(?<=[-_/])/);
}

// A name that wraps only after "-", "_" and "/", unless a piece alone is
// wider than the line.
export function Breakable({ text }: { text: string }) {
  return pieces(text).map((p, k) => (
    <Fragment key={k}>
      {k > 0 && <wbr />}
      {p}
    </Fragment>
  ));
}

function split(repo: string): [string, string] {
  const cut = repo.indexOf("/");
  return cut < 0 ? ["", repo] : [repo.slice(0, cut), repo.slice(cut + 1)];
}

function units(repos: OpenRepo[]): number {
  return repos.reduce((a, r) => a + r.open, 0);
}

// Which columns show their repo's name; the others a number. Once one
// column needs a number every column and every table row gets one: 1 to n
// without gaps, column and row matched by the same number.
export type MekkoLabels = { label: boolean[]; numbered: boolean };

function labelsOf(label: boolean[]): MekkoLabels {
  return { label, numbered: label.includes(false) };
}

// Until measured: the narrowest the chart gets, UNIT px per PR.
export function guessLabels(repos: OpenRepo[]): MekkoLabels {
  return labelsOf(repos.map((r) => r.open * UNIT >= GUESS_MIN));
}

// Measured columns, `fits` as rendered at `width`. `lost` keeps the width a
// column lost its name at: the name comes back only HYSTERESIS px wider.
export function keepLabels(
  cols: { repo: string; width: number; fits: boolean }[],
  lost: Map<string, number>,
): MekkoLabels {
  return labelsOf(
    cols.map(({ repo, width, fits }) => {
      const at = lost.get(repo);
      if (fits && (at === undefined || width >= at + HYSTERESIS)) {
        lost.delete(repo);
        return true;
      }
      if (at === undefined) lost.set(repo, width);
      return false;
    }),
  );
}

// Each column's name as the browser renders it, in the column's invisible
// probe (its font, fallback and zoom included). It fits when MIN_CHARS fit
// the column, the 3-line clamp cut nothing: the end ("-api" vs "-web") is
// what tells two repos of one project apart, and the project key is whole,
// or the same for every repo.
function measureLabels(
  row: HTMLElement,
  repos: OpenRepo[],
  lost: Map<string, number>,
): MekkoLabels {
  if (!row.clientWidth) return guessLabels(repos);
  const oneProject = new Set(repos.map((r) => split(r.repo)[0])).size === 1;
  const probes = row.querySelectorAll<HTMLElement>("[data-probe]");
  return keepLabels(
    repos.map((r, i) => {
      const probe = probes[i];
      const part = (k: string) =>
        probe.querySelector<HTMLElement>(`[data-fit="${k}"]`);
      const name = part("name");
      const project = part("project");
      const min = part("min");
      const fits =
        !!name &&
        !!min &&
        probe.clientWidth >= min.offsetWidth &&
        name.scrollHeight <= name.clientHeight + 1 &&
        (oneProject ||
          !project ||
          project.scrollWidth <= project.clientWidth + 1);
      return { repo: r.repo, width: probe.clientWidth, fits };
    }),
    lost,
  );
}

// A column's number, the same under the column and before its row in the
// table. A visual key only: column and row carry the full name.
export function RepoNo({ n }: { n: number }) {
  return (
    <span
      aria-hidden="true"
      data-testid="repo-no"
      className="inline-grid h-4 min-w-4 flex-none place-items-center rounded-full border border-border px-1 text-[10px] leading-none text-muted tabular-nums"
    >
      {num(n)}
    </span>
  );
}

// The name under a column: the project key, then the repo in at most 3
// lines. The probe renders the same, invisible, for measuring.
function RepoLabel({ repo, probe }: { repo: string; probe?: boolean }) {
  const [project, name] = split(repo);
  return (
    <div
      data-testid={probe ? undefined : "mekko-label"}
      className="w-full font-mono leading-tight"
    >
      {project && (
        <div data-fit="project" className="truncate text-[9.5px] text-muted">
          {project}
        </div>
      )}
      <div
        data-fit="name"
        className="line-clamp-3 text-[10.5px] wrap-anywhere text-ink-dim"
      >
        <Breakable text={name} />
      </div>
    </div>
  );
}

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
  labels,
  onLabels,
}: {
  o: OpenPRs;
  // Bottom to top.
  waits: PRWaiting[];
  labels: MekkoLabels;
  // Called only when the measured labels change: the table numbers its
  // rows from them.
  onLabels: (l: MekkoLabels) => void;
}) {
  const { t } = useTranslation();
  const [hover, setHover] = useState<number | null>(null);
  // Every repo the backend sends has an open PR.
  const repos = o.repos;
  const boxRef = useRef<HTMLDivElement>(null);
  const rowRef = useRef<HTMLDivElement>(null);
  const tipRef = useRef<HTMLDivElement>(null);
  const lost = useRef(new Map<string, number>());
  const sent = useRef("");

  // Measured before paint, again when the row's width changes (its
  // height follows the labels: ignored) and once the fonts are in.
  useLayoutEffect(() => {
    const row = rowRef.current;
    if (!row) return;
    let width = -1;
    let live = true;
    const run = (always: boolean) => {
      if (!live || (!always && row.clientWidth === width)) return;
      width = row.clientWidth;
      const next = measureLabels(row, repos, lost.current);
      const key = JSON.stringify(next.label);
      if (key === sent.current) return;
      sent.current = key;
      onLabels(next);
    };
    run(true);
    void document.fonts?.ready.then(() => run(true));
    const ro =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(() => run(false));
    ro?.observe(row);
    return () => {
      live = false;
      ro?.disconnect();
    };
  }, [repos, onLabels]);

  // The tooltip centred on its column, then pushed inside the scroll box's
  // visible part, which clips anything past its sides; wider than that
  // part (a long name on a phone), it wraps to its width.
  useLayoutEffect(() => {
    const anchor = tipRef.current;
    const tip = anchor?.firstElementChild;
    const box = boxRef.current;
    if (!anchor || !(tip instanceof HTMLElement) || !box) return;
    const b = box.getBoundingClientRect();
    const room = b.width - 2 * TIP_INSET;
    if (tip.offsetWidth > room) {
      tip.style.width = `${room}px`;
      tip.style.whiteSpace = "normal";
      tip.style.overflowWrap = "anywhere";
    }
    const r = tip.getBoundingClientRect();
    const shift = Math.max(
      b.left + TIP_INSET - r.left,
      Math.min(0, b.right - TIP_INSET - r.right),
    );
    anchor.style.transform = shift ? `translateX(${shift}px)` : "";
  }, [hover]);

  if (!repos.length) return null;
  const word = (k: PRWaiting) => t(`facts.prWaiting.${k}`);
  // Layout only: the width the columns need at UNIT px per PR.
  const minWidth = AXIS + units(repos) * UNIT + (repos.length - 1) * GAP;
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
      <div ref={boxRef} className="mt-2 overflow-x-auto">
        <div
          className="flex gap-2"
          style={{ minWidth }}
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
            <div ref={rowRef} className="relative flex" style={{ gap: GAP }}>
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
                        ref={tipRef}
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
                    {/* In the flow, so the chart grows with its names. */}
                    <div
                      title={r.repo}
                      className="relative flex flex-col items-center gap-0.5 px-0.5 text-center"
                    >
                      {/* Measured, never seen: no height, nothing to scroll. */}
                      <div
                        aria-hidden="true"
                        data-probe
                        className="invisible absolute inset-x-0.5 top-0 h-0 overflow-hidden"
                      >
                        <RepoLabel repo={r.repo} probe />
                        <span
                          data-fit="min"
                          className="font-mono text-[10.5px] whitespace-nowrap"
                        >
                          {"0".repeat(MIN_CHARS)}
                        </span>
                      </div>
                      {labels.numbered && <RepoNo n={i + 1} />}
                      {labels.label[i] && <RepoLabel repo={r.repo} />}
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
