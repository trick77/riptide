// Where the time to production goes: the five stages of the lead-time
// breakdown as a proportional bar, then per stage its PR count, median and
// p90 on a log scale (minutes to a month do not fit a linear one).

import type { Chain, Stage } from "./metrics";
import { dur, pct } from "./format";
import { Card, CardHead } from "./ui";

const MONTH_MIN = 43_200;
// Position on the log axis, 0..100, of a duration in seconds; past a month
// pinned to the axis end, the number beside it tells the rest.
const L = (s: number) =>
  Math.min(
    100,
    (Math.log10(Math.max(1, s / 60)) / Math.log10(MONTH_MIN)) * 100,
  );

const TICKS: [number, string][] = [
  [60, "1 min"],
  [3600, "1 h"],
  [86400, "1 Tag"],
  [7 * 86400, "1 Woche"],
  [30 * 86400, "30 Tage"],
];

export function stageNames(
  c: Chain,
  prod: string,
): Record<Stage["key"], [string, string]> {
  const pre = c.preEnvironment;
  return {
    coding: ["Coding", "erster Commit → PR bereit"],
    review: ["Review", "PR bereit → Approval"],
    merge: ["Merge", "Approval → gemergt"],
    toPre: [`Bis ${pre}`, `gemergt → auf ${pre}`],
    toProd: [
      `Bis ${prod}`,
      pre
        ? `auf ${pre} (ohne: gemergt) → auf ${prod}`
        : `gemergt → auf ${prod}`,
    ],
  };
}

export function LeadChain({
  chain: full,
  prod,
}: {
  chain: Chain;
  prod: string;
}) {
  // Without a stage before production there is no "to pre" stage to show.
  const chain = {
    ...full,
    stages: full.stages.filter(
      (s) => s.key !== "toPre" || full.preEnvironment != null,
    ),
  };
  const names = stageNames(chain, prod);
  const withData = chain.stages.filter((s) => s.spread.p50 != null);
  const sum = withData.reduce((a, s) => a + s.spread.p50!, 0);
  // All medians zero (one PR through in a second): equal shares, not NaN.
  const total = sum > 0 ? sum : withData.length;
  const share = (s: Stage) => (sum > 0 ? s.spread.p50! / sum : 1 / total);
  const hot = withData.reduce<Stage | null>(
    (a, s) => (!a || s.spread.p50! > a.spread.p50! ? s : a),
    null,
  );
  const last = chain.stages[chain.stages.length - 1];
  const beforeProd = withData
    .filter((s) => s !== last)
    .reduce((a, s) => a + s.spread.p50!, 0);
  return (
    <Card className="mt-3">
      <CardHead
        title={`Wo die Zeit bis ${prod} liegt`}
        sub="Lead Time pro menschlichem PR in App-Repos, Median je Etappe"
        right={
          <div className="text-[12.5px] text-muted">
            Summe der Mediane{" "}
            <span className="text-ink tabular-nums">
              {dur(withData.length ? sum : null)}
            </span>
          </div>
        }
      />
      {withData.length > 0 ? (
        <>
          <div
            role="img"
            aria-label={`Lead Time nach Etappe: ${chain.stages.map((s) => `${names[s.key][0]} ${dur(s.spread.p50)}`).join(", ")}`}
            className="mt-4 flex h-7 gap-0.5"
          >
            {withData.map((s, i) => (
              <div
                key={s.key}
                title={`${names[s.key][0]}: ${dur(s.spread.p50)}`}
                className={`h-7 min-w-[3px] first:rounded-l-md last:rounded-r-md ${
                  s === hot
                    ? "bg-accent-strong"
                    : i % 2
                      ? "bg-muted"
                      : "bg-ink-3"
                }`}
                style={{
                  flex: `${Math.max(share(s), 0.004)} 1 0`,
                }}
              />
            ))}
          </div>
          <div className="mt-1.5 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
            <span>
              Bis vor {prod}:{" "}
              <span className="text-ink">{dur(beforeProd)}</span>
            </span>
            {last.spread.p50 != null && (
              <span>
                Warten auf {prod}:{" "}
                <span className="text-accent-strong">
                  {dur(last.spread.p50)}
                </span>{" "}
                ({pct(share(last))} der Zeit)
              </span>
            )}
          </div>
        </>
      ) : (
        <p className="mt-4 mb-0 text-[13.5px] text-muted">
          Keine gemergten PRs im Zeitraum.
        </p>
      )}

      <div className="mt-5 grid grid-cols-[minmax(130px,1.1fr)_48px_72px_minmax(200px,4fr)] items-center gap-x-3.5">
        {["Etappe", "PRs", "Median"].map((h, i) => (
          <div
            key={h}
            className={`border-b border-border pb-2 text-[11px] font-medium tracking-[.12em] text-muted uppercase ${i ? "text-right" : ""}`}
          >
            {h}
          </div>
        ))}
        <div className="relative h-4 border-b border-border pb-2">
          {TICKS.map(([s, label], i) => (
            <span
              key={label}
              className="absolute top-0 text-[11px] whitespace-nowrap text-muted"
              style={{
                left: `${L(s)}%`,
                transform: `translateX(${i === 0 ? "0" : i === TICKS.length - 1 ? "-100%" : "-50%"})`,
              }}
            >
              {label}
            </span>
          ))}
        </div>
        {chain.stages.map((s) => {
          const { n, p50, p90 } = s.spread;
          const isHot = s === hot;
          const color = isHot
            ? "var(--color-accent-strong)"
            : "var(--color-s1)";
          const a = p50 == null ? 0 : L(p50);
          const b = p90 == null ? a : L(p90);
          return (
            <div key={s.key} className="contents">
              <div className="border-b border-border-soft py-2 text-[13.5px] text-ink-dim">
                <div>{names[s.key][0]}</div>
                <div className="text-[11.5px] text-muted">
                  {names[s.key][1]}
                </div>
              </div>
              <div
                className={`border-b border-border-soft py-2 text-right text-[13.5px] tabular-nums ${n < 10 ? "text-ochre" : "text-ink-dim"}`}
                title={
                  n < 10 ? "Weniger als 10 PRs: wenig belastbar" : undefined
                }
              >
                {n}
              </div>
              <div className="border-b border-border-soft py-2 text-right text-[13.5px] text-ink tabular-nums">
                {dur(p50)}
              </div>
              <div
                className="relative box-border h-[42px] border-b border-border-soft py-2"
                title={
                  p50 == null
                    ? "keine Daten"
                    : `${names[s.key][0]}: Median ${dur(p50)}, p90 ${dur(p90)}, ${n} PRs`
                }
              >
                {TICKS.map(([t]) => (
                  <span
                    key={t}
                    aria-hidden="true"
                    className="absolute top-1 bottom-1 border-l border-grid-minor"
                    style={{ left: `${L(t)}%` }}
                  />
                ))}
                {p50 != null && (
                  <>
                    <span
                      aria-hidden="true"
                      className="absolute top-[20px] h-0.5 rounded-sm opacity-55"
                      style={{
                        left: `${a}%`,
                        width: `${Math.max(0.5, b - a)}%`,
                        background: color,
                      }}
                    />
                    <span
                      aria-hidden="true"
                      className="absolute top-[15px] -ml-1.5 size-3 rounded-full ring-2 ring-panel"
                      style={{ left: `${a}%`, background: color }}
                    />
                    <span
                      className="absolute top-[13px] text-[11px] whitespace-nowrap text-muted tabular-nums"
                      // Right of the line; left of the dot when the line
                      // ends too close to the edge for the label to fit.
                      style={
                        b > 86
                          ? { right: `calc(${100 - a}% + 10px)` }
                          : { left: `calc(${b}% + 8px)` }
                      }
                    >
                      p90 {dur(p90)}
                    </span>
                  </>
                )}
              </div>
            </div>
          );
        })}
      </div>
      <p className="mt-2.5 mb-0 text-xs text-muted">
        Log-Skala: Punkt = Median, Linie bis p90. Review ist eine Etappe, weil
        das Approval meist der erste Kontakt des Reviewers ist. Die letzten zwei
        Etappen erreichen nur PRs, deren Merge-Commit einem Deploy zugeordnet
        ist.
      </p>
    </Card>
  );
}
