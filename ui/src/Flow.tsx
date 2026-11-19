// Where a change waits on its way to production: the lead-time breakdown
// per PR as labelled stages, then the review as a pair of numbers that only
// read together, then how many PRs are merged within a day.

import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";
import type { LeadChain, Metrics, StageKey } from "./api";
import { GroupedBars } from "./charts";
import { dateTime, dur, durParts, pct, stage } from "./format";
import { OpenPRs } from "./OpenPRs";
import { StageRidge } from "./ridgeline";
import type { Tab } from "./state";
import {
  Card,
  CardHead,
  Compare,
  Delta,
  Direction,
  SectionHead,
  Tile,
  type Why,
  WhyPanel,
} from "./ui";

export function stageNames(
  t: TFunction,
  c: LeadChain,
  prod: string,
): Record<StageKey, [string, string]> {
  const pre = c.pre_environment && stage(c.pre_environment);
  return {
    coding: [t("flow.stage.coding"), t("flow.stage.codingDesc")],
    review: [t("flow.stage.review"), t("flow.stage.reviewDesc")],
    merge: [t("flow.stage.merge"), t("flow.stage.mergeDesc")],
    to_pre: [
      t("flow.stage.toPre", { pre }),
      t("flow.stage.toPreDesc", { pre }),
    ],
    to_prod: [
      t("flow.stage.toProd", { prod }),
      pre
        ? t("flow.stage.toProdDescPre", { pre, prod })
        : t("flow.stage.toProdDesc", { prod }),
    ],
  };
}

// Wording of the backend's cycle-time buckets by upper bound in seconds;
// a bound without one says so plainly rather than mislabel a count.
function bucketLabels(t: TFunction, bounds: number[]): string[] {
  const label: Record<number, string> = {
    900: "< 15 min",
    3600: "15–60 min",
    14400: "1–4 h",
    86400: "4–24 h",
    259200: t("flow.bucket.days"),
  };
  const open: Record<number, string> = { 259200: t("flow.bucket.overDays") };
  const last = bounds[bounds.length - 1];
  return bounds
    .map((b) => label[b] ?? t("flow.bucket.upTo", { d: dur(b) }))
    .concat(open[last] ?? t("flow.bucket.from", { d: dur(last) }));
}

type Props = {
  m: Metrics;
  why: string | null;
  onWhy: (key: string) => void;
  onFacts: (tab: Tab, q: string) => void;
};

function Chain({ m }: { m: Metrics }) {
  const { t } = useTranslation();
  const prod = stage(m.delivery.production_stage);
  const chain = m.lead_chain;
  const names = stageNames(t, chain, prod);
  const withData = chain.stages.filter((s) => s.p50 != null);
  const hot = chain.stages.find((s) => s.hot);
  const pre = m.delivery.lead_time_pre;
  const preChange = m.comparison.lead_time_pre;
  return (
    <Card>
      <CardHead
        title={t("flow.chainTitle", { prod })}
        sub={t("flow.chainSub")}
      />
      {pre && (
        <div className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
          <div className="rounded-ui border border-border-soft px-3 py-2.5">
            <div className="text-[13.5px] text-ink-dim">
              {t("flow.leadTo", { env: stage(pre.environment) })}{" "}
              <span className="font-medium text-ink tabular-nums">
                {dur(pre.human.p50)}
              </span>{" "}
              <span className="text-xs text-muted">
                {t("flow.typicalSlowest", { p90: dur(pre.human.p90) })}
              </span>
            </div>
            {preChange && (
              <>
                <Delta change={preChange} format={dur} />
                <Compare change={preChange} />
              </>
            )}
          </div>
          <div className="rounded-ui border border-border-soft px-3 py-2.5">
            <div className="text-[13.5px] text-ink-dim">
              {t("flow.leadTo", { env: prod })}{" "}
              <span className="font-medium text-ink tabular-nums">
                {dur(m.delivery.lead_time.human.p50)}
              </span>
            </div>
            <Delta change={m.comparison.lead_time} format={dur} />
            <Compare change={m.comparison.lead_time} />
          </div>
        </div>
      )}
      {withData.length > 0 ? (
        <>
          {chain.reaches_prod ? (
            <div
              role="img"
              aria-label={t("flow.shareAria", {
                prod,
                parts: withData
                  .map((s) => `${names[s.key][0]} ${pct(s.share)}`)
                  .join(", "),
              })}
              className="mt-4 flex h-8 gap-0.5"
            >
              {withData.map((s, i) => (
                <div
                  key={s.key}
                  title={`${names[s.key][0]}: ${dur(s.p50)}, ${pct(s.share)}`}
                  className={`flex h-8 min-w-[4px] items-center overflow-hidden px-2 text-xs whitespace-nowrap text-ink first:rounded-l-md last:rounded-r-md ${
                    s.hot ? "bg-accent-fill" : i % 2 ? "bg-ink-3" : "bg-other"
                  }`}
                  style={{ flex: `${Math.max(s.share ?? 0, 0.004)} 1 0` }}
                >
                  {s.hot && `${names[s.key][0]} · ${pct(s.share)}`}
                </div>
              ))}
            </div>
          ) : (
            <p className="mt-4 mb-0 rounded-ui border border-border-soft px-3.5 py-2.5 text-[13.5px] text-ink-dim">
              {t("flow.noProd", { prod })}
            </p>
          )}
          <ol className="m-0 mt-3.5 grid list-none grid-cols-[repeat(auto-fit,minmax(170px,1fr))] gap-2.5 p-0">
            {chain.stages.map((s) => (
              <li
                key={s.key}
                className={`min-w-0 rounded-ui border px-3 py-2.5 ${
                  s.hot
                    ? "border-accent-fill bg-accent/10"
                    : "border-border-soft"
                }`}
              >
                <div className="text-[13.5px] text-ink">{names[s.key][0]}</div>
                <div className="text-[11.5px] text-muted">
                  {names[s.key][1]}
                </div>
                <div className="mt-1 text-[20px] font-medium text-ink tabular-nums">
                  {dur(s.p50)}{" "}
                  <span className="text-xs font-normal text-muted">
                    {s.share == null
                      ? ""
                      : t("flow.ofTime", { share: pct(s.share) })}
                  </span>
                </div>
                <div className="text-xs text-muted">
                  {t("common.slowest", { p90: dur(s.p90) })}
                </div>
                <div
                  className={`text-xs ${s.low_sample ? "text-ochre" : "text-muted"}`}
                >
                  {t("common.nPRs", { count: s.n })}
                  {s.low_sample ? ` · ${t("common.lowSample")}` : ""}
                </div>
              </li>
            ))}
          </ol>
          {hot && (
            <div className="mt-3 rounded-ui border border-accent-dim bg-accent/10 px-3.5 py-2.5 text-[13.5px] text-ink-dim">
              <span className="font-medium text-accent-strong">
                {t("flow.hot", { name: names[hot.key][0] })}
              </span>
              {t("flow.hotRest", { share: pct(hot.share), prod })}
            </div>
          )}
        </>
      ) : (
        <p className="mt-4 mb-0 text-[13.5px] text-muted">
          {t("common.noMergedPRs")}
        </p>
      )}
      <p className="mt-2.5 mb-0 text-xs text-muted">{t("flow.chainNote")}</p>
    </Card>
  );
}

export function Flow({ m, why, onWhy, onFacts }: Props) {
  const { t } = useTranslation();
  const prod = stage(m.delivery.production_stage);
  const r = m.review;
  const c = m.comparison;
  const rv = durParts(r.review.p50);
  const labels = bucketLabels(t, r.cycle_histogram.upper_bounds_s);
  const day = r.cycle_histogram.upper_bounds_s.indexOf(86400);
  // People's PRs only: bots never count.
  const series = [{ key: "h", label: t("flow.people"), className: "bg-s1" }];

  const whys: Record<string, Why> = {
    review: {
      title: t("flow.review.label"),
      means: t("flow.review.means"),
      watch: t("flow.review.watch"),
      lever: t("flow.review.lever"),
      def: t("flow.review.def"),
      basis: t("flow.review.basis", { count: r.review.n }),
      filters: [
        t("flow.review.filterReviewer"),
        "non_human_identities",
        t("common.withoutGitOps"),
      ],
      coverage: t("flow.review.coverage"),
      query: "docs/metrics.md · Lead-time breakdown query",
    },
    touch: {
      title: t("flow.touch.label"),
      means: t("flow.touch.means"),
      watch: t("flow.touch.watch"),
      lever: t("flow.touch.lever"),
      def: t("flow.touch.def"),
      basis: t("flow.touch.basis", {
        n: r.approved_on_first_touch,
        total: r.approved,
      }),
      filters: [
        t("flow.touch.filterComments"),
        t("common.withoutBots"),
        t("common.withoutGitOps"),
      ],
      coverage: t("flow.touch.coverage"),
      query: "docs/metrics.md · Lead-time breakdown query",
    },
    cycle: {
      title: t("flow.cycle.label"),
      means: t("flow.cycle.means"),
      watch: t("flow.cycle.watch"),
      lever: t("flow.cycle.lever"),
      def: t("flow.cycle.def", {
        p50: dur(r.cycle.p50),
        p90: dur(r.cycle.p90),
      }),
      basis: t("flow.cycle.basis", {
        count: r.within_day.count,
        n: r.within_day.n,
      }),
      filters: [
        t("common.withoutGitOps"),
        t("common.withoutBots"),
        t("flow.cycle.filterMerged"),
      ],
      coverage: t("flow.cycle.coverage"),
      query: "docs/metrics.md · PR cycle time",
    },
    open: {
      title: t("open.jammed.label"),
      means: t("open.why.means"),
      watch: t("open.why.watch"),
      lever: t("open.why.lever"),
      def: t("open.why.def", {
        days: m.open_prs.long_days,
        min: m.open_prs.jam_min,
      }),
      basis: t("open.why.basis", {
        count: m.open_prs.open,
        at: dateTime(m.open_prs.at),
      }),
      filters: [
        t("common.withoutGitOps"),
        t("common.withoutBots"),
        "non_human_identities",
      ],
      coverage: t("open.why.coverage", {
        inactive: m.open_prs.inactive_days,
      }),
      query: "docs/metrics.md · Open PR age",
    },
  };
  const open = why && whys[why] ? whys[why] : null;

  return (
    <section id="fluss" aria-labelledby="h-fluss" className="mt-10">
      <SectionHead
        id="h-fluss"
        eyebrow={t("flow.eyebrow", { prod })}
        title={t("flow.title")}
        lead={t("flow.lead")}
        frameworks={[
          ["DORA", t("flow.fwLeadTime")],
          ["Flow", "Flow efficiency"],
          ["DevEx", "Feedback loops"],
          ["SPACE", "Efficiency & flow"],
        ]}
      />
      <Chain m={m} />
      <StageRidge
        chain={m.lead_chain}
        names={stageNames(t, m.lead_chain, prod)}
      />

      <Card className="mt-3">
        <CardHead title={t("flow.reviewTitle")} />
        <div className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-3">
          <Tile
            label={t("flow.review.label")}
            question={t("flow.review.question")}
            value={rv.value}
            unit={
              r.review.p50 == null ? "" : t("common.typical", { unit: rv.unit })
            }
            note={t("flow.review.note", {
              p90: dur(r.review.p90),
              count: r.review.n,
            })}
            direction={
              <Direction
                arrow="down"
                label={t("common.shorterBetter")}
                why={t("common.ownHistory")}
              />
            }
            compare={{
              change: c.review_time,
              format: dur,
            }}
            open={why === "review"}
            onWhy={() => onWhy("review")}
          />
          <Tile
            label={t("flow.touch.label")}
            question={t("flow.touch.question")}
            value={pct(r.first_touch_rate)}
            unit={t("common.nOf", {
              n: r.approved_on_first_touch,
              total: r.approved,
            })}
            direction={
              <Direction
                label={t("common.noDirection")}
                why={t("flow.touch.directionWhy")}
              />
            }
            compare={{
              change: c.first_touch,
              format: pct,
            }}
            open={why === "touch"}
            onWhy={() => onWhy("touch")}
          />
        </div>
        <div className="mt-5 grid grid-cols-[minmax(240px,1fr)_2fr] gap-5 max-md:grid-cols-1">
          <Tile
            label={t("flow.cycle.label")}
            question={t("flow.cycle.question")}
            value={pct(r.within_day.rate)}
            unit={t("flow.cycle.unit", {
              n: r.within_day.count,
              count: r.within_day.n,
            })}
            direction={
              <Direction
                arrow="up"
                label={t("flow.cycle.direction")}
                why={t("flow.cycle.directionWhy")}
              />
            }
            compare={{
              change: c.within_day,
              format: pct,
            }}
            open={why === "cycle"}
            onWhy={() => onWhy("cycle")}
          />
          <div className="min-w-0">
            <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
              <div className="text-[12.5px] text-muted">
                {t("flow.histHead")}
                {day >= 0 && t("flow.histLine")}
              </div>
            </div>
            <GroupedBars
              labels={labels}
              series={series}
              values={[r.cycle_histogram.human]}
              markAfter={day >= 0 ? day : undefined}
              markLabel="24 h"
              ariaLabel={t("flow.histAria", {
                parts: labels
                  .map((l, i) => `${l} ${r.cycle_histogram.human[i]}`)
                  .join(", "),
              })}
            />
          </div>
        </div>
      </Card>
      {open && why !== "open" && <WhyPanel why={open} />}

      <OpenPRs
        m={m}
        open={why === "open"}
        onWhy={() => onWhy("open")}
        onFacts={onFacts}
      />
      {open && why === "open" && <WhyPanel why={open} />}
    </section>
  );
}
