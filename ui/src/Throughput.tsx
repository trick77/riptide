// How much the team finishes and in what portions: the people's merged PRs
// per week (flow velocity) and their size (batch size). The two tiles stand
// in the overview, the weeks in the flow section right below it.

import { useTranslation } from "react-i18next";
import type { Metrics } from "./api";
import { WeekStack } from "./charts";
import { fullDate, num } from "./format";
import { Card, CardHead, Direction, Legend, Swatch, Tile } from "./ui";

// The people's merged PRs the backend left out of every PR number, each
// under its first reason, with what the reason means on hover.
const LEFT_OUT = ["housekeeping", "umbrella", "twice"] as const;

export function MergedTile({ m }: { m: Metrics }) {
  const { t } = useTranslation();
  const th = m.throughput;
  // The housekeeping rule's numbers are the backend's.
  const rule = {
    repos: num(th.housekeeping_repos),
    minutes: num(th.housekeeping_window_s / 60),
  };
  const leftOut = LEFT_OUT.filter((k) => th[k] > 0).map((k) => (
    <span key={k} title={t(`throughput.merged.${k}Title`, rule)}>
      {" · "}
      {t(`throughput.merged.${k}`, { count: th[k] })}
    </span>
  ));
  return (
    <Tile
      label={t("throughput.merged.label")}
      dora={t("throughput.merged.framework")}
      question={t("throughput.merged.question")}
      value={th.merged_per_week == null ? "–" : num(th.merged_per_week, 1)}
      unit={th.merged_per_week == null ? "" : t("throughput.merged.unit")}
      note={
        <>
          {t("throughput.merged.note", { count: th.merged })}
          {leftOut}
        </>
      }
      warn={
        th.since
          ? t("throughput.merged.since", {
              date: fullDate(new Date(th.since)),
            })
          : undefined
      }
      direction={
        <Direction
          arrow="up"
          label={t("throughput.merged.direction")}
          why={t("common.ownHistory")}
        />
      }
      compare={{
        change: m.comparison.merged_per_week,
        format: (v) => num(v, 1),
      }}
    />
  );
}

export function BatchTile({ m }: { m: Metrics }) {
  const { t } = useTranslation();
  const th = m.throughput;
  const b = th.batch_size;
  return (
    <Tile
      label={t("throughput.batch.label")}
      dora={t("throughput.batch.framework")}
      question={t("throughput.batch.question")}
      value={b.p50 == null ? "–" : num(b.p50)}
      unit={b.p50 == null ? "" : t("throughput.batch.unit")}
      note={
        b.p50 == null || b.p90 == null
          ? t("throughput.batch.none")
          : t("throughput.batch.note", {
              p90: num(b.p90),
              files: num(th.batch_files.p50 ?? 0),
              n: num(th.coverage.sized.count),
              total: num(th.coverage.sized.n),
            })
      }
      direction={
        <Direction
          arrow="down"
          label={t("throughput.batch.direction")}
          why={t("common.ownHistory")}
        />
      }
      compare={{
        change: m.comparison.batch_size,
        format: (v) => num(v),
      }}
    />
  );
}

const MIX = [
  ["feature", "bg-s1"],
  ["bugfix", "bg-s2"],
  ["hotfix", "bg-s3"],
  ["other", "bg-other"],
] as const;

export function ThroughputCard({ m }: { m: Metrics }) {
  const { t } = useTranslation();
  const labels = m.period.weeks.map((w) => t("common.cw", { kw: w.kw }));
  const mix = MIX.map(([key, className]) => ({
    key,
    label: t(`mix.${key}`),
    className,
  }));
  return (
    <Card className="mt-3">
      <CardHead title={t("throughput.cardTitle")} sub={t("throughput.scope")} />
      <div className="mt-3 grid grid-cols-2 gap-x-6 gap-y-6 max-md:grid-cols-1">
        <div className="min-w-0">
          <div className="text-[12.5px] text-muted">
            {t("throughput.merged.chart")}
          </div>
          <WeekStack
            labels={labels}
            series={mix}
            values={m.review.change_mix}
            ariaLabel={t("throughput.merged.chart")}
          />
          <div className="mt-2">
            <Legend>
              {mix.map((s) => (
                <Swatch key={s.key} className={s.className} label={s.label} />
              ))}
            </Legend>
          </div>
        </div>
        <div className="min-w-0">
          <div className="text-[12.5px] text-muted">
            {t("throughput.batch.chart")}
          </div>
          <WeekStack
            labels={labels}
            series={[
              {
                key: "lines",
                label: t("throughput.batch.series"),
                className: "bg-s1",
              },
            ]}
            // A week without a sized PR draws no bar.
            values={m.throughput.weeks.map((w) => ({
              lines: w.batch_p50 ?? 0,
            }))}
            ariaLabel={t("throughput.batch.chart")}
          />
        </div>
      </div>
    </Card>
  );
}
