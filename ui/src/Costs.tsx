import type { Dataset } from "./api";
import { BarList } from "./charts";
import { money } from "./format";
import { costs, type Filter } from "./metrics";
import { Card, CardHead, SectionHead } from "./ui";

export function Costs({ ds, filter }: { ds: Dataset; filter: Filter }) {
  const c = costs(ds, filter);
  const tiles: [string, string, string][] = [
    [
      "Ausgaben gemergt",
      money(c.merged),
      `${c.mergedReviews} gemergte PRs reviewt`,
    ],
    [
      "Pro KLOC",
      c.perKloc == null ? "–" : money(c.perKloc),
      "Kosten pro 1000 geänderte Zeilen, gemergte PRs",
    ],
    ["Verworfen", money(c.wasted), "Reviews für abgelehnte oder gelöschte PRs"],
  ];
  return (
    <section id="kosten" aria-labelledby="h-kosten" className="mt-10">
      <SectionHead
        id="h-kosten"
        eyebrow="FinOps · noergler"
        title="Kosten der KI-Reviews"
      />
      <div className="flex flex-wrap gap-3">
        <div className="grid flex-[1_1_300px] grid-cols-1 gap-3">
          {tiles.map(([label, value, note]) => (
            <div
              key={label}
              className="rounded-ui border border-border bg-panel px-3.5 py-3"
            >
              <span className="text-[11px] font-medium tracking-[.12em] text-muted uppercase">
                {label}
              </span>
              <div className="mt-0.5 text-2xl font-medium text-ink tabular-nums">
                {value}
              </div>
              <div className="text-xs text-muted">{note}</div>
            </div>
          ))}
        </div>
        <Card className="flex-[2_1_520px]">
          <CardHead
            title={
              filter.product == null
                ? "Kosten nach Produkt"
                : "Kosten nach Repo"
            }
            sub="Alle Reviews im Zeitraum, neuestes Rollup pro PR"
          />
          {c.rows.length ? (
            <BarList rows={c.rows} format={money} />
          ) : (
            <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
              Keine noergler-Reviews im Zeitraum.
            </p>
          )}
          <div className="mt-3.5 text-xs text-muted">
            Nur Repos, die noergler reviewt: {c.reviewedRepos} im Zeitraum.
            Gesamt {money(c.total)}.
          </div>
        </Card>
      </div>
    </section>
  );
}
