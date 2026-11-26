// The team against its own previous period, one row per number with a
// better direction: the dashed centre line is the previous period, a bar
// to the left got worse, to the right better, its length the backend's
// score (-1..1, halving / doubling). Worse bars are red, better ones green,
// as on every page; the same is a grey tick. Numbers without a direction
// are no rows at all.

import type { TFunction } from "i18next";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Change, Metrics } from "./api";
import { Tip } from "./charts";
import { dur, num, pct, stage } from "./format";
import { SubHead } from "./ui";

type Row = {
  key: string;
  label: string;
  change: Change;
  // The current value and the previous one's format, as the page's
  // Compare tiles show them.
  now: string;
  format: (v: number) => string;
};

// The numbers the backend judges with a direction; it says which in
// `directed`, so a number it stops directing drops out by itself.
function rows(t: TFunction, m: Metrics): Row[] {
  const c = m.comparison;
  const freq = m.delivery.frequency;
  const lt = m.delivery.lead_time;
  const pre = m.delivery.lead_time_pre;
  const r = m.review;
  const out: Row[] = [
    {
      key: "deploy_frequency",
      label: t("trends.releases", {
        prod: stage(m.delivery.production_stage),
      }),
      change: c.deploy_frequency,
      now: freq.syncs ? num(freq.per_week, 1) : "–",
      format: (v) => num(v, 1),
    },
    {
      key: "lead_time",
      label: t("overview.lead.label", {
        prod: stage(m.delivery.production_stage),
      }),
      change: c.lead_time,
      now: dur(lt.human.p50),
      format: dur,
    },
  ];
  if (c.lead_time_pre && pre)
    out.push({
      key: "lead_time_pre",
      label: t("overview.lead.label", { prod: stage(pre.environment) }),
      change: c.lead_time_pre,
      now: dur(pre.human.p50),
      format: dur,
    });
  out.push(
    {
      key: "deploy_success",
      label: t("overview.dsucc.label"),
      change: c.deploy_success,
      now: pct(freq.success_rate),
      format: pct,
    },
    {
      key: "review_time",
      label: t("flow.review.label"),
      change: c.review_time,
      now: dur(r.review.p50),
      format: dur,
    },
    {
      key: "within_day",
      label: t("flow.cycle.label"),
      change: c.within_day,
      now: pct(r.within_day.rate),
      format: pct,
    },
    {
      key: "merged_per_week",
      label: t("throughput.merged.label"),
      change: c.merged_per_week,
      now:
        m.throughput.merged_per_week == null
          ? "–"
          : num(m.throughput.merged_per_week, 1),
      format: (v) => num(v, 1),
    },
    {
      key: "batch_size",
      label: t("throughput.batch.label"),
      change: c.batch_size,
      now:
        m.throughput.batch_size.p50 == null
          ? "–"
          : num(m.throughput.batch_size.p50),
      format: (v) => num(v),
    },
    {
      key: "open_jammed",
      label: t("open.jammed.label"),
      change: c.open_jammed,
      now: num(m.open_prs.jammed),
      format: (v) => num(v),
    },
    ...c.build.map((k) => ({
      key: `build_${k.kind}`,
      label: t("strip.buildSuccess", { kind: t(`build.kind.${k.kind}`) }),
      change: k.success,
      now: pct(
        m.build.kinds.find((b) => b.kind === k.kind)?.success_rate ?? null,
      ),
      format: pct,
    })),
  );
  return out.filter((x) => x.change.directed);
}

// Room at the outer end for the value label, the longest ("12.3 Tage")
// with its gap: a full bar stops short of it, so the label never runs into
// the row's name.
const ROOM = "4.5rem";

// Phones: the name above its bar, so the bar gets the whole width. The bar
// column never gets narrower than 15rem: each side keeps a 3rem track past
// the value's room, so a bar's length still reads.
const GRID =
  "grid grid-cols-[minmax(15rem,1fr)] sm:grid-cols-[clamp(112px,36%,260px)_minmax(15rem,1fr)]";

function Centre() {
  return (
    <span
      aria-hidden="true"
      className="absolute inset-y-0 left-1/2 border-l border-dashed border-ink-dim/60"
    />
  );
}

export function Strip({ m }: { m: Metrics }) {
  const { t } = useTranslation();
  // One row's tooltip at a time: the last hover or focus wins.
  const [active, setActive] = useState<string | null>(null);
  const list = rows(t, m);
  return (
    <>
      <SubHead title={t("strip.title")} text={t("strip.text")} />
      <div
        role="group"
        aria-label={t("strip.aria")}
        className="mt-3 rounded-ui-lg border border-border bg-panel px-3 py-3 sm:px-4"
      >
        <div className={`${GRID} text-[11px]`}>
          <span className="max-sm:hidden" />
          <div className="relative h-5">
            <Centre />
            <span className="absolute top-0 right-[calc(50%+8px)] whitespace-nowrap text-danger-ink">
              ◀ {t("compare.worse")}
            </span>
            <span className="absolute top-0 left-[calc(50%+8px)] whitespace-nowrap text-online">
              {t("compare.better")} ▶
            </span>
          </div>
        </div>
        <ul className="m-0 list-none p-0">
          {list.map((row, i) => {
            const { change: c } = row;
            const stripe = i % 2 === 0 ? "bg-bg" : "bg-panel";
            const v = c.verdict === "none" ? null : c.verdict;
            const s = v && c.score;
            return (
              <li
                key={row.key}
                data-testid="strip-row"
                className={`${GRID} rounded-[4px] ${stripe}`}
              >
                {/* A long label wraps rather than being cut: "Erfolgsquote,
                    Default-Branch, Art unbekannt" read as cut off. */}
                <span
                  className={`min-w-0 self-center px-2 pt-1.5 text-[12.5px] leading-tight break-words sm:py-1 sm:pr-0 ${s == null ? "text-faint" : "text-ink-dim"}`}
                >
                  {row.label}
                </span>
                {v == null || s == null ? (
                  <div className="relative min-h-7">
                    <Centre />
                    <span
                      className={`absolute top-1/2 left-1/2 -translate-1/2 px-1.5 text-[11px] whitespace-nowrap text-faint ${stripe}`}
                    >
                      {/* The source that does not report, the backend's:
                          noergler, Bitbucket or the CI as well as Argo CD. */}
                      {c.reason === "not_reporting"
                        ? t("strip.reason.not_reporting", {
                            source: t(
                              `overview.assess.source.${c.source ?? "argocd"}`,
                            ),
                          })
                        : c.reason && c.reason !== "undirected"
                          ? t(`strip.reason.${c.reason}`)
                          : t("compare.none")}
                    </span>
                  </div>
                ) : (
                  <Bar
                    row={row}
                    verdict={v}
                    score={s}
                    open={active === row.key}
                    onOpen={(on) =>
                      setActive((a) =>
                        on ? row.key : a === row.key ? null : a,
                      )
                    }
                  />
                )}
              </li>
            );
          })}
        </ul>
        <div className={`${GRID} text-[11px]`}>
          <span className="max-sm:hidden" />
          <div className="relative h-5">
            <Centre />
            <span className="absolute bottom-0 left-1/2 -translate-x-1/2 bg-panel px-1.5 whitespace-nowrap text-muted">
              {t("compare.previous")}
            </span>
          </div>
        </div>
      </div>
    </>
  );
}

function Bar({
  row,
  verdict,
  score,
  open,
  onOpen,
}: {
  row: Row;
  verdict: Exclude<Change["verdict"], "none">;
  score: number;
  open: boolean;
  onOpen: (on: boolean) => void;
}) {
  const { t } = useTranslation();
  const tip = useId();
  const prev = row.change.prev;
  const worse = verdict === "worse";
  // The backend scores a same verdict 0: a tick on the centre line, no
  // side.
  const same = verdict === "same";
  const left = !same && score < 0;
  const word = t(`compare.${verdict}`);
  const width = `max(3px, calc((50% - ${ROOM}) * ${Math.abs(score)}))`;
  const at = same ? "calc(50% + 6px)" : `calc(50% + ${width} + 6px)`;
  return (
    <div
      role="img"
      aria-label={`${row.label}: ${row.now}, ${word}`}
      aria-describedby={open ? tip : undefined}
      tabIndex={0}
      className="relative min-h-7 rounded-[4px] outline-none focus-visible:ring-2 focus-visible:ring-accent-strong"
      onMouseEnter={() => onOpen(true)}
      onMouseLeave={() => onOpen(false)}
      onFocus={() => onOpen(true)}
      onBlur={() => onOpen(false)}
    >
      <Centre />
      {same ? (
        <span
          aria-hidden="true"
          data-testid="strip-tick"
          className="absolute top-1/2 left-1/2 h-3.5 w-0.5 -translate-1/2 rounded-full bg-ink-3"
        />
      ) : (
        <span
          aria-hidden="true"
          data-testid="strip-bar"
          className={`absolute top-1/2 h-2.5 -translate-y-1/2 rounded-[3px] ${
            worse
              ? left
                ? "bg-linear-to-r from-danger to-danger/45"
                : "bg-linear-to-l from-danger to-danger/45"
              : left
                ? "bg-linear-to-r from-online to-online/45"
                : "bg-linear-to-l from-online to-online/45"
          }`}
          style={left ? { right: "50%", width } : { left: "50%", width }}
        />
      )}
      <span
        aria-hidden="true"
        className={`absolute top-1/2 -translate-y-1/2 text-[11px] whitespace-nowrap tabular-nums ${
          worse ? "text-danger-ink" : same ? "text-muted" : "text-online"
        }`}
        style={left ? { right: at } : { left: at }}
      >
        {row.now}
      </span>
      {open && (
        <div id={tip}>
          <Tip
            title={row.label}
            rows={[
              [t("compare.now"), row.now],
              [t("compare.previous"), prev == null ? "–" : row.format(prev)],
              [word, ""],
            ]}
          />
        </div>
      )}
    </div>
  );
}
