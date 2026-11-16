// Releases per day as a heat calendar: the period's calendar weeks as
// columns, the weekdays as rows, a cell shaded by the step the backend
// computed; to the right each weekday's releases per day with data. A team
// sliding into a release train or into gaps shows here while its weekly
// bars stay even.

import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { DeployDay, DeployWeekday, Week } from "./api";
import { labelEvery, Tip } from "./charts";
import { fullDate, num } from "./format";

const WEEKDAYS = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"] as const;

// Literal class names per step, so Tailwind generates them.
const SHADE = ["bg-cell-empty", "bg-seq-1", "bg-seq-2", "bg-seq-3", "bg-seq-4"];
// A filled cell's faint top highlight.
const HIGHLIGHT = "bg-linear-to-b from-white/10 to-white/0";

const EN = String.fromCharCode(0x2002);
const SEP = `${EN}·${EN}`;

// "2026-11-12" as a local date: new Date("2026-11-12") is UTC midnight,
// the day before west of UTC.
function localDate(iso: string): Date {
  const [y, m, d] = iso.split("-").map(Number);
  return new Date(y, m - 1, d);
}

const ROW = 16;

export function DayCalendar({
  weeks,
  days,
  weekdays,
  noDataDays,
  prod,
}: {
  weeks: Week[];
  // 7 per week, Monday first.
  days: DeployDay[];
  // Per weekday over the days with data, Monday first.
  weekdays: DeployWeekday[];
  noDataDays: number;
  prod: string;
}) {
  const { t } = useTranslation();
  const dayName = (d: number) => t(`calendar.days.${WEEKDAYS[d]}`);
  const perDay = (w: DeployWeekday | undefined) =>
    w?.per_day == null ? "–" : num(w.per_day, 2);
  const [hover, setHover] = useState<number | null>(null);
  const every = labelEvery(weeks.length);
  const max = Math.max(0, ...weekdays.map((w) => w.per_day ?? 0)) || 1;
  const grid = {
    // Cells shrink with a narrow card (a phone) rather than wrap the
    // weekday column away from its rows.
    gridTemplateColumns: `22px repeat(${weeks.length}, minmax(0, 40px))`,
    gridAutoRows: ROW,
  };
  return (
    <>
      {/* Drawn for the eye; a screen reader gets the table below. */}
      <div
        aria-hidden="true"
        data-testid="calendar"
        className="mt-3.5 flex items-start gap-4 sm:gap-6"
      >
        <div className="grid min-w-0 gap-[2px]" style={grid}>
          {WEEKDAYS.map((wd, d) => [
            <span key={wd} className="text-[11px] leading-4 text-muted">
              {dayName(d)}
            </span>,
            ...weeks.map((w, i) => {
              const k = 7 * i + d;
              const day = days[k];
              if (!day) return <span key={`${wd}${i}`} />;
              return (
                <div
                  key={day.date}
                  data-testid="day-cell"
                  onMouseEnter={() => setHover(k)}
                  onMouseLeave={() => setHover(null)}
                  className={`relative rounded-[3px] ${
                    day.no_data
                      ? "texture-gap"
                      : `${SHADE[day.step]} ${day.step > 0 ? HIGHLIGHT : ""}`
                  }`}
                >
                  {hover === k && (
                    <Tip
                      title={`${dayName(d)} ${fullDate(localDate(day.date))}${SEP}${t("common.cw", { kw: w.kw })}`}
                      rows={[
                        day.no_data
                          ? [t("calendar.noData"), ""]
                          : [t("deploys.releases"), num(day.releases)],
                      ]}
                    />
                  )}
                </div>
              );
            }),
          ])}
          <span />
          {weeks.map((w, i) => (
            <span
              key={w.start}
              // On a phone the cells shrink: half the labels.
              className={`overflow-visible text-[11px] leading-4 whitespace-nowrap text-muted ${i % (2 * every) ? "max-sm:invisible" : ""}`}
            >
              {i % every === 0 ? t("common.cw", { kw: w.kw }) : ""}
            </span>
          ))}
        </div>
        <div
          className="grid w-[88px] flex-none gap-[2px] sm:w-[260px]"
          style={{ gridAutoRows: ROW }}
        >
          {WEEKDAYS.map((wd, d) => {
            const w = weekdays[d];
            return (
              <div
                key={wd}
                className="flex items-center gap-1.5"
                title={t("calendar.weekdayTitle", {
                  day: dayName(d),
                  releases: w?.releases ?? 0,
                  count: w?.days ?? 0,
                })}
              >
                {(w?.per_day ?? 0) > 0 && (
                  <div
                    data-testid="weekday-bar"
                    className="h-2 min-w-[2px] rounded-r-[3px] bg-linear-to-r from-s1/35 to-s1"
                    style={{
                      width: `calc((100% - 36px) * ${w!.per_day! / max})`,
                    }}
                  />
                )}
                <span className="text-[11px] leading-4 text-ink-dim tabular-nums">
                  {perDay(w)}
                </span>
              </div>
            );
          })}
          {/* Too wide for a phone's column; the card's subtitle says it. */}
          <span className="text-[11px] leading-4 whitespace-nowrap text-muted max-sm:hidden">
            {t("calendar.perDay")}
          </span>
        </div>
      </div>
      {/* The days for a screen reader, the calendar's only accessible
          form; a table ignores sr-only's 1px box, the wrapper does not. */}
      <div className="sr-only">
        <table>
          <caption>
            {t("calendar.tableCaption", {
              prod,
              noData: t("calendar.noDataDays", { count: noDataDays }),
            })}
          </caption>
          <thead>
            <tr>
              <th scope="col">{t("calendar.week")}</th>
              {WEEKDAYS.map((wd, d) => (
                <th key={wd} scope="col">
                  {dayName(d)}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {weeks.map((w, i) => (
              <tr key={w.start}>
                <th scope="row">{t("common.cw", { kw: w.kw })}</th>
                {WEEKDAYS.map((wd, d) => {
                  const day = days[7 * i + d];
                  return (
                    <td key={wd}>
                      {!day || day.no_data
                        ? t("calendar.noData")
                        : num(day.releases)}
                    </td>
                  );
                })}
              </tr>
            ))}
            <tr>
              <th scope="row">{t("calendar.perDay")}</th>
              {WEEKDAYS.map((wd, d) => (
                <td key={wd}>
                  {t("calendar.weekdayCell", {
                    perDay: perDay(weekdays[d]),
                    releases: weekdays[d]?.releases ?? 0,
                    count: weekdays[d]?.days ?? 0,
                  })}
                </td>
              ))}
            </tr>
          </tbody>
        </table>
      </div>
    </>
  );
}
