// The PRs open at the period's end: every other PR number counts a PR once
// merged, so a PR left open for weeks shows only here. Per repo, with what
// the PRs wait on; the backend counts, sorts and cuts, this renders.

import { useTranslation } from "react-i18next";
import type { Metrics, PRWaiting, WaitingCounts } from "./api";
import { dateTime, dur, num } from "./format";
import type { Tab } from "./state";
import { Card, CardHead, Compare, Direction, Mono, Pill, Tile } from "./ui";

const HEAD =
  "border-b border-border pb-2 text-[11px] font-medium tracking-[.12em] text-muted uppercase";
const CELL = "border-b border-border-soft py-2 text-[13.5px] tabular-nums";

// The order the waits read in: the author's, then the reviewers', then
// the merge.
export const WAITS: PRWaiting[] = [
  "first_review",
  "review",
  "changes",
  "merge",
  "draft",
];

function Waits({ w }: { w: WaitingCounts }) {
  const { t } = useTranslation();
  const parts = WAITS.filter((k) => w[k] > 0);
  if (!parts.length) return <span className="text-muted">–</span>;
  return (
    <span className="flex flex-wrap gap-1.5">
      {parts.map((k) => (
        <Pill key={k} tone={k === "first_review" ? "warn" : "none"}>
          {t(`facts.prWaiting.${k}`)} {num(w[k])}
        </Pill>
      ))}
    </span>
  );
}

export function OpenPRs({
  m,
  open,
  onWhy,
  onFacts,
}: {
  m: Metrics;
  open: boolean;
  onWhy: () => void;
  onFacts: (tab: Tab, q: string) => void;
}) {
  const { t } = useTranslation();
  const o = m.open_prs;
  const c = m.comparison;
  return (
    <Card className="mt-3">
      <CardHead
        title={t("open.title")}
        sub={t("open.sub", {
          at: dateTime(o.at),
          days: o.long_days,
          bots: m.bots ? t("open.subBots") : "",
        })}
      />
      <div className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-3">
        <Tile
          label={t("open.long.label")}
          question={t("open.long.question", { days: o.long_days })}
          value={num(o.long)}
          unit={t("open.long.unit", { count: o.open })}
          direction={
            <Direction
              arrow="down"
              label={t("open.long.direction")}
              why={t("open.long.directionWhy")}
            />
          }
          compare={
            <Compare
              change={c.open_long}
              now={num(o.long)}
              format={(v) => num(v)}
            />
          }
          open={open}
          onWhy={onWhy}
        />
        <Tile
          label={t("open.all.label")}
          question={t("open.all.question")}
          value={num(o.open)}
          unit={
            o.age.p50 == null ? "" : t("open.all.unit", { p50: dur(o.age.p50) })
          }
          note={
            o.age.p90 == null
              ? undefined
              : t("open.all.note", { p90: dur(o.age.p90) })
          }
          direction={
            <Direction
              label={t("common.noDirection")}
              why={t("open.all.directionWhy")}
            />
          }
          compare={
            <Compare
              change={c.open_prs}
              now={num(o.open)}
              format={(v) => num(v)}
            />
          }
        />
      </div>
      {o.long > 0 && (
        <div className="mt-3.5 flex flex-wrap items-center gap-x-2.5 gap-y-1.5 text-[13px] text-ink-dim">
          {t("open.longWaitOn", { days: o.long_days })}{" "}
          <Waits w={o.long_waiting} />
        </div>
      )}
      {o.repos.length ? (
        <div className="mt-3 overflow-x-auto">
          <div className="grid min-w-[640px] grid-cols-[minmax(150px,1.3fr)_64px_64px_96px_minmax(200px,2fr)_120px] items-center gap-x-3.5">
            {[
              [t("open.colRepo"), false],
              [t("open.colOpen"), true],
              [t("open.colLong", { days: o.long_days }), true],
              [t("open.colOldest"), true],
              [t("open.colWaiting"), false],
              ["", false],
            ].map(([head, right], i) => (
              <div
                key={(head as string) || i}
                className={`${HEAD} ${right ? "text-right" : ""}`}
              >
                {head}
              </div>
            ))}
            {o.repos.map((r) => (
              <div key={r.repo} className="contents">
                <div className={`${CELL} overflow-hidden`}>
                  <Mono className="text-[12px] whitespace-nowrap text-ink-dim">
                    {r.repo}
                  </Mono>
                </div>
                <div className={`${CELL} text-right`}>{num(r.open)}</div>
                <div
                  className={`${CELL} text-right ${r.long > 0 ? "text-ochre" : "text-muted"}`}
                >
                  {num(r.long)}
                </div>
                <div className={`${CELL} text-right text-ink-dim`}>
                  {dur(r.oldest_s)}
                </div>
                <div className={CELL}>
                  <Waits w={r.waiting} />
                </div>
                <div className={`${CELL} text-right`}>
                  <button
                    type="button"
                    onClick={() => onFacts("prs", r.repo)}
                    className="no-print border-0 bg-transparent p-0 text-[13px] text-accent-strong hover:underline"
                  >
                    {t("open.viewPRs")}
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : (
        <p className="mt-3.5 mb-0 text-[13.5px] text-muted">{t("open.none")}</p>
      )}
    </Card>
  );
}
