// The repos of the view in one table, what stands out first: grouped by how
// often they roll out (cut by the team's own thresholds), each row with its
// releases per week, the wait from merge to production, the merged PRs per
// week and the typical PR size. Order, groups, flags and how many rows open
// are the backend's (Metrics.repos); a row opens the repo's facts.

import { type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Metrics, RepoGroupKey, RepoRow, TrendSeries } from "./api";
import { num, stage } from "./format";
import { TABLE_HEAD } from "./shared";
import { TrendChart } from "./trends";
import { Pill, type Tone } from "./ui";

const GROUP_TONE: Record<RepoGroupKey, string> = {
  none: "bg-danger",
  rare: "border-[1.5px] border-muted",
  unjudged: "texture-unknown",
  regular: "bg-online",
  library: "bg-ink-3",
  idle: "bg-faint",
};

// The cut values the group names and the order line write, all the
// backend's: the team's thresholds, the rare group's first day, the stage.
function cuts(m: Metrics) {
  const r = m.repos;
  return {
    hint: r.hint_days,
    breach: r.breach_days,
    from: r.rare_from,
    prod: stage(m.delivery.production_stage),
  };
}

function GroupHead({
  g,
  count,
  m,
}: {
  g: RepoGroupKey;
  count: number;
  m: Metrics;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
      <span
        aria-hidden="true"
        className={`inline-block size-2 rounded-full ${GROUP_TONE[g]}`}
      />
      <span className="text-[11px] font-medium tracking-[.12em] text-ink-dim uppercase">
        {t(`repos.group.${g}`, cuts(m))}
      </span>
      <span className="font-mono text-[12.5px] text-muted">{count}</span>
      <span className="text-xs text-faint">
        {t(`repos.groupSub.${g}`, cuts(m))}
      </span>
    </div>
  );
}

// Releases per week as tiny bars on a scale shared by every row; a week
// before Argo CD reported is a dashed outline, none a flat stub.
function Bars({
  weeks,
  max,
  split,
}: {
  weeks: (number | null)[];
  max: number;
  split: number;
}) {
  const bw = 6;
  const gap = 2;
  const H = 22;
  const W = weeks.length * (bw + gap);
  return (
    <svg
      width={Math.min(W, 104)}
      height={H}
      viewBox={`0 0 ${W} ${H}`}
      aria-hidden="true"
      className="flex-none"
      preserveAspectRatio="none"
    >
      {split > 0 && (
        <rect
          x="0"
          y="0"
          width={split * (bw + gap) - gap / 2}
          height={H}
          fill="var(--color-active)"
          opacity="0.6"
        />
      )}
      {weeks.map((v, i) => {
        const x = i * (bw + gap) + 1;
        if (v == null)
          return (
            <rect
              key={i}
              x={x}
              y={H - 6}
              width={bw}
              height="5"
              fill="none"
              stroke="var(--color-grid)"
              strokeDasharray="1.5 1.5"
            />
          );
        if (v === 0)
          return (
            <rect
              key={i}
              x={x}
              y={H - 1.5}
              width={bw}
              height="1.5"
              fill="var(--color-grid)"
            />
          );
        const h = Math.max(2.5, Math.sqrt(v / max) * (H - 2));
        return (
          <rect
            key={i}
            x={x}
            y={H - h}
            width={bw}
            height={h}
            rx="1"
            fill="var(--color-s1)"
          />
        );
      })}
    </svg>
  );
}

function Cell({ v, sub, tone }: { v: string; sub?: string; tone?: string }) {
  return (
    <span className="block min-w-0 text-[13px] whitespace-nowrap text-ink-dim">
      <span className={tone}>{v}</span>
      {sub && (
        <small className="block text-[11.5px] whitespace-normal text-muted">
          {sub}
        </small>
      )}
    </span>
  );
}

function StandPills({ r, prod }: { r: RepoRow; prod: string }) {
  const { t } = useTranslation();
  const pills: [Tone, string][] = r.flags.map((f) => [
    f.tone === "breach" ? "fail" : "none",
    t(`repos.flag.${f.key}`, {
      count: Math.round(f.value),
      days: num(f.value, 1),
      prod,
      lower:
        (f.key === "no_release" || f.key === "release_due") &&
        r.since_lower_bound
          ? t("repos.flag.lowerBound")
          : "",
    }),
  ]);
  if (r.group === "unjudged" && r.reason)
    pills.push(["warn", t(`repos.reason.${r.reason}`, { prod })]);
  if (r.group === "library") pills.push(["none", t("repos.pill.library")]);
  if (r.group === "idle") pills.push(["none", t("repos.pill.idle")]);
  if (!pills.length) return <span className="text-faint">–</span>;
  return (
    <span className="flex flex-col items-start gap-1">
      {pills.map(([tone, text]) => (
        <Pill key={text} tone={tone}>
          {text}
        </Pill>
      ))}
    </span>
  );
}

export function Ranking({
  m,
  onRepo,
}: {
  m: Metrics;
  onRepo: (repo: string) => void;
}) {
  const { t } = useTranslation();
  const [all, setAll] = useState(false);
  const v = m.repos;
  const prod = stage(m.delivery.production_stage);
  if (!v.rows.length) return null;
  const tr = m.trends;
  // The closed weeks the rows' week lists run over.
  const weeks = tr.weeks.filter((w) => !w.running);
  const relMax = Math.max(
    1,
    ...v.rows.flatMap((r) => r.release_weeks.map((x) => x ?? 0)),
  );
  const mergedMax = Math.max(
    1,
    ...v.rows.flatMap((r) => r.merged_weeks.map((x) => x ?? 0)),
  );
  const open = all ? v.rows.length : v.open;
  const shown = v.rows.slice(0, open);
  const before = (x: number | null, f: (n: number) => string) =>
    x == null ? undefined : t("compare.before", { v: f(x) });
  const days = (x: number) => t("units.days", { n: num(x, 1) });
  // As the strips write it: once a month is 0.25, not 0.3.
  const perWeek = (x: number) => num(x, x < 1 ? 2 : x < 10 ? 1 : 0);
  const body: ReactNode[] = [];
  let group: RepoGroupKey | null = null;
  shown.forEach((r) => {
    if (r.group !== group) {
      group = r.group;
      body.push(
        <tr key={`g-${r.group}`}>
          <td colSpan={7} className="border-b border-border px-3 pt-4 pb-1.5">
            <GroupHead
              g={r.group}
              count={v.groups.find((g) => g.key === r.group)?.count ?? 0}
              m={m}
            />
          </td>
        </tr>,
      );
    }
    // A week without a value names the trend's own reason: the same
    // source's gap (the rows' weeks are the trend's closed ones).
    const mergedSeries: TrendSeries = {
      values: r.merged_weeks,
      n: [],
      gaps: r.merged_weeks.map((x, i) =>
        x == null ? tr.merged.gaps[i] || "no_data" : "",
      ),
    };
    // How the row writes its releases is the backend's: null writes none.
    const form = r.releases_form;
    const relText =
      form == null
        ? "–"
        : form === "none"
          ? t("repos.rel.none")
          : form === "per_week" && r.releases_per_week != null
            ? t("repos.rel.perWeek", { n: perWeek(r.releases_per_week) })
            : t("repos.rel.inWeeks", {
                count: r.releases,
                weeks: r.releases_base,
              });
    const relSub =
      r.days_since != null
        ? t(r.since_lower_bound ? "repos.rel.lastLower" : "repos.rel.last", {
            count: Math.round(r.days_since),
          })
        : undefined;
    let prodCell: { v: string; sub?: string; tone?: string };
    if (r.to_prod != null)
      prodCell = {
        v: days(r.to_prod),
        sub: before(r.to_prod_prev, days),
        tone: r.flags.some((f) => f.key === "slow_to_prod")
          ? "text-danger-ink"
          : undefined,
      };
    else if (r.to_prod_n > 0)
      prodCell = { v: t("repos.thinCell", { count: r.to_prod_n }) };
    else if (r.group === "none" && r.merged > 0)
      prodCell = { v: t("repos.lying", { prod }), tone: "text-danger-ink" };
    else prodCell = { v: "–" };
    if (r.not_yet > 0)
      prodCell.sub = [
        prodCell.sub,
        t("repos.notYet", { count: r.not_yet, prod }),
      ]
        .filter(Boolean)
        .join(" · ");
    const openRow = () => onRepo(r.repo);
    body.push(
      <tr
        key={r.repo}
        data-testid="rank-row"
        tabIndex={0}
        title={t("repos.rowTitle", { repo: r.repo })}
        onClick={openRow}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            openRow();
          }
        }}
        className="cursor-pointer outline-none hover:bg-active focus-visible:bg-active focus-visible:ring-2 focus-visible:ring-accent-strong focus-visible:ring-inset"
      >
        <td className="border-b border-border-soft px-3 py-2 align-middle">
          <span className="block font-mono text-[13px] whitespace-nowrap text-ink">
            {r.repo.slice(r.repo.indexOf("/") + 1)}
          </span>
          <span className="block text-[11px] tracking-[.08em] text-faint uppercase">
            {r.project}
          </span>
        </td>
        <td className="border-b border-border-soft px-3 py-2 align-middle">
          <StandPills r={r} prod={prod} />
        </td>
        <td className="border-b border-border-soft px-3 py-2 align-middle">
          <span className="flex items-center gap-2.5">
            {form != null && (
              <Bars weeks={r.release_weeks} max={relMax} split={tr.split} />
            )}
            <Cell v={relText} sub={relSub} />
          </span>
        </td>
        <td className="border-b border-border-soft px-3 py-2 align-middle">
          <Cell {...prodCell} />
        </td>
        <td className="border-b border-border-soft px-3 py-2 align-middle">
          <span className="flex items-center gap-2.5">
            <TrendChart
              weeks={weeks}
              split={tr.split}
              series={mergedSeries}
              format={(x) => num(x)}
              label={t("repos.mergedAria", { repo: r.repo })}
              small
              max={mergedMax}
              className="block h-[22px] w-[72px] flex-none"
            />
            <Cell
              v={r.merged_per_week == null ? "–" : perWeek(r.merged_per_week)}
              sub={before(r.merged_prev, perWeek)}
            />
          </span>
        </td>
        <td className="border-b border-border-soft px-3 py-2 align-middle">
          <Cell
            v={
              r.batch != null
                ? t("repos.unit.lines", { n: num(r.batch) })
                : r.sized > 0
                  ? t("repos.thinCell", { count: r.sized })
                  : "–"
            }
            sub={before(r.batch_prev, (x) => num(x))}
          />
        </td>
        <td
          aria-hidden="true"
          className="w-7 border-b border-border-soft pr-3 text-right text-lg text-faint"
        >
          ›
        </td>
      </tr>,
    );
  });
  if (!all)
    v.groups
      .filter((g) => g.hidden.length)
      .forEach((g) => {
        // A group with rows above folds its rest into one line that reads
        // as that group's rest; a group with none shown gets its head.
        const partly = shown.some((r) => r.group === g.key);
        const names = g.hidden.map((x) => x.slice(x.indexOf("/") + 1));
        body.push(
          <tr key={`more-${g.key}`} data-testid="rank-more">
            <td
              colSpan={7}
              className="border-b border-border-soft px-3 py-2 text-[13px] text-muted"
            >
              {partly ? (
                `${t("repos.restOf", { count: g.hidden_count })} `
              ) : (
                <span className="mr-2.5 inline-flex">
                  <GroupHead g={g.key} count={g.hidden_count} m={m} />
                </span>
              )}
              {names.join(", ")}
              {g.more > 0 && ` ${t("repos.andMore", { n: g.more })}`}
            </td>
          </tr>,
        );
      });
  const hiddenAny = v.open < v.rows.length;
  return (
    <div
      id="repos"
      className="mt-3 rounded-ui-lg border border-border bg-panel"
      data-testid="ranking"
    >
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1.5 px-5 pt-4 pb-2.5">
        <span className="text-[15px] font-medium text-ink">
          {t("repos.title")}
        </span>
        <span className="text-[12.5px] text-muted">
          {t("repos.sub", { from: weeks[0]?.kw, to: weeks.at(-1)?.kw })}
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[980px] border-collapse tabular-nums">
          <thead>
            <tr>
              {[
                t("repos.col.repo"),
                t("repos.col.stand"),
                t("repos.col.releases", { prod }),
                t("repos.col.toProd", { prod }),
                t("repos.col.merged"),
                t("repos.col.batch"),
              ].map((h) => (
                <th
                  key={h}
                  className={`${TABLE_HEAD} border-b border-border px-3 py-2 text-left align-bottom`}
                >
                  {h}
                </th>
              ))}
              <th className="border-b border-border" />
            </tr>
          </thead>
          <tbody>{body}</tbody>
        </table>
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-5 pt-3 pb-3.5 text-[12.5px] text-muted">
        {hiddenAny && (
          <button
            type="button"
            aria-expanded={all}
            onClick={() => setAll((a) => !a)}
            className="no-print rounded-ui-sm border border-elevated-border bg-bg px-3.5 py-1 text-[13px] text-ink-dim hover:bg-active hover:text-ink"
          >
            {all
              ? t("repos.less", { n: v.open })
              : t("repos.all", { n: v.rows.length })}
          </button>
        )}
        <span>
          {all || !hiddenAny
            ? t("repos.countAll", { count: v.rows.length })
            : t("repos.countOpen", { n: v.open, total: v.rows.length })}{" "}
          {t("repos.order", cuts(m))}
        </span>
      </div>
    </div>
  );
}
