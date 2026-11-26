// How evenly code reaches production: releases per week against the
// team's own median week, per day as a calendar, and the mix of work
// merged.

import { useTranslation } from "react-i18next";
import type { Change, Metrics } from "./api";
import { DayCalendar } from "./calendar";
import { WeekStack } from "./charts";
import { fullDate, num, pct, stage } from "./format";
import {
  Card,
  CardHead,
  Compare,
  Delta,
  Eyebrow,
  Legend,
  SectionHead,
  Swatch,
  WarnLine,
} from "./ui";

// A number of the section against the previous period, in its head, in the
// tiles' form: the previous value under it, the verdict below. The bars
// stay blue, they are a count. The first deploys are in the note under the
// chart; Argo CD reporting only since a day is said under the head, as on
// the overview's tile.
function HeadNumber({
  label,
  value,
  unit,
  change,
  format,
}: {
  label: string;
  value: string;
  unit?: string;
  change: Change;
  format: (v: number) => string;
}) {
  return (
    <div className="text-right">
      <Eyebrow>{label}</Eyebrow>
      <div className="text-[20px] font-medium text-ink tabular-nums">
        {value}
        {unit && (
          <span className="ml-1.5 text-[12px] font-normal text-muted">
            {unit}
          </span>
        )}
      </div>
      <Delta change={change} format={format} />
      <Compare change={change} />
    </div>
  );
}

export function Deploys({ m }: { m: Metrics }) {
  const { t } = useTranslation();
  const prod = stage(m.delivery.production_stage);
  const freq = m.delivery.frequency;
  const weeks = m.period.weeks;
  return (
    <section id="deploys" aria-labelledby="h-deploys" className="mt-10">
      <SectionHead
        id="h-deploys"
        eyebrow={t("deploys.eyebrow")}
        title={t("deploys.title", { prod })}
        frameworks={[
          ["DORA", "Deployment frequency"],
          ["Core 4", t("deploys.fwSpeed")],
          ["Flow", "Flow velocity"],
        ]}
      />
      <Card>
        <CardHead
          title={t("deploys.cardTitle", { prod })}
          sub={t("deploys.cardSub")}
          right={
            <div className="flex flex-wrap items-start gap-x-6 gap-y-2">
              {freq.argo_reporting && (
                <>
                  <HeadNumber
                    label={t("overview.freq.label", { prod })}
                    value={freq.syncs ? num(freq.per_week, 1) : "–"}
                    unit={freq.syncs ? t("overview.freq.unit") : undefined}
                    change={m.comparison.deploy_frequency}
                    format={(v) => num(v, 1)}
                  />
                  <HeadNumber
                    label={t("overview.dsucc.label")}
                    value={pct(freq.success_rate)}
                    change={m.comparison.deploy_success}
                    format={pct}
                  />
                </>
              )}
              <Legend>
                <Swatch className="bg-s1" label={t("deploys.release")} />
              </Legend>
            </div>
          }
        />
        {freq.argo_reporting && freq.since && (
          <WarnLine>
            {t("overview.sinceNote", { date: fullDate(new Date(freq.since)) }) +
              (freq.low_sample ? t("overview.sinceLow") : "")}
          </WarnLine>
        )}
        <WeekStack
          labels={weeks.map((w) => t("common.cw", { kw: w.kw }))}
          series={[
            {
              key: "release",
              label: t("deploys.releases"),
              className: "bg-s1",
            },
          ]}
          values={freq.weeks}
          reference={
            freq.median_week == null
              ? undefined
              : {
                  value: freq.median_week,
                  label: t("deploys.typical", {
                    n: num(freq.median_week, 1),
                  }),
                }
          }
          ariaLabel={t("deploys.aria", {
            prod,
            parts: freq.weeks
              .map(
                (w, i) => `${t("common.cw", { kw: weeks[i].kw })} ${w.release}`,
              )
              .join(", "),
          })}
        />
        <p className="mt-2.5 mb-0 text-xs text-muted">
          {t("deploys.note", {
            resyncs: t("common.resyncs", { count: freq.resyncs }),
            firstSeen: t("common.firstDeploys", { count: freq.first_seen }),
            days: freq.grace_days,
          })}
        </p>
      </Card>
      <Card className="mt-3">
        <CardHead
          title={t("calendar.cardTitle", { prod })}
          sub={t("calendar.cardSub")}
          right={
            freq.argo_reporting && (
              <Legend>
                <Swatch className="bg-cell-empty" label={t("calendar.none")} />
                <Swatch className="bg-seq-1" label="1" />
                <Swatch className="bg-seq-2" label="2" />
                <Swatch className="bg-seq-3" label="3" />
                <Swatch className="bg-seq-4" label={t("calendar.four")} />
                <Swatch className="texture-gap" label={t("calendar.noData")} />
              </Legend>
            )
          }
        />
        {freq.argo_reporting ? (
          <DayCalendar
            weeks={weeks}
            days={freq.days}
            weekdays={freq.weekdays}
            noDataDays={freq.no_data_days}
            prod={prod}
          />
        ) : (
          <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
            {t("overview.freq.notReporting")}
          </p>
        )}
      </Card>
    </section>
  );
}

const MIX: [keyof Metrics["review"]["mix_total"], string][] = [
  ["feature", "bg-s1"],
  ["bugfix", "bg-s2"],
  ["hotfix", "bg-s3"],
  ["other", "bg-other"],
];

export function Mix({ m }: { m: Metrics }) {
  const { t } = useTranslation();
  const r = m.review;
  const mix = MIX.map(([k, cls]) => [k, t(`mix.${k}`), cls] as const);
  const shown = mix.filter(([k]) => (r.mix_share[k] ?? 0) > 0);
  return (
    <section id="arbeitsmix" aria-labelledby="h-arbeitsmix" className="mt-10">
      <SectionHead
        id="h-arbeitsmix"
        eyebrow={t("mix.eyebrow")}
        title={t("mix.title")}
        lead={t("mix.lead")}
        frameworks={[
          ["Core 4", t("mix.fwImpact")],
          ["Flow", "Flow distribution"],
        ]}
      />
      <Card>
        <CardHead
          title={t("mix.cardTitle")}
          sub={t("mix.cardSub", { count: r.merged })}
        />
        {shown.length ? (
          <>
            <div
              role="img"
              aria-label={t("mix.aria", {
                parts: shown
                  .map(([k, l]) => `${l} ${pct(r.mix_share[k])}`)
                  .join(", "),
              })}
              className="mt-3.5 flex h-7 gap-0.5"
            >
              {shown.map(([k, l, cls]) => (
                <div
                  key={k}
                  title={t("mix.barTitle", {
                    label: l,
                    count: r.mix_total[k],
                    share: pct(r.mix_share[k]),
                  })}
                  className={`flex h-7 min-w-[4px] items-center overflow-hidden px-2 text-xs whitespace-nowrap text-ink first:rounded-l-md last:rounded-r-md ${cls}`}
                  style={{ flex: `${r.mix_share[k]} 1 0` }}
                >
                  {pct(r.mix_share[k])}
                </div>
              ))}
            </div>
            <div className="mt-2">
              <Legend>
                {mix.map(([k, l, cls]) => (
                  <Swatch
                    key={k}
                    className={cls}
                    label={`${l} ${r.mix_total[k]}`}
                  />
                ))}
              </Legend>
            </div>
          </>
        ) : (
          <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
            {t("common.noMergedPRs")}
          </p>
        )}
        <div className="mt-4 text-[13.5px] text-ink-dim">
          <span className="font-medium text-ink">
            {t("mix.untracked", { rate: pct(r.untracked_rate) })}
          </span>{" "}
          {t("mix.untrackedText", { n: r.untracked, total: r.merged })}
        </div>
        <Delta change={m.comparison.untracked} format={pct} />
        <Compare change={m.comparison.untracked} />
      </Card>
    </section>
  );
}
