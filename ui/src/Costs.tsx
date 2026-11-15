import type { Metrics } from "./api";
import { BarList } from "./charts";
import { money, pct } from "./format";
import { Card, CardHead, Compare, Direction, SectionHead, Tile } from "./ui";

export function Costs({ m }: { m: Metrics }) {
  const c = m.costs;
  const cmp = m.comparison;
  return (
    <section id="kosten" aria-labelledby="h-kosten" className="mt-10">
      <SectionHead
        id="h-kosten"
        eyebrow="FinOps · noergler"
        title="Was kosten die KI-Reviews?"
        lead="Ausgaben der automatischen Reviews im Zeitraum. Kein Framework misst das; die Zahlen helfen, Kosten und Nutzen der Reviews abzuwägen."
      />
      <div className="grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
        <Tile
          label="Ausgaben"
          value={money(c.total)}
          note={`davon ${money(c.merged)} für ${c.merged_reviews} gemergte PRs`}
        />
        <Tile
          label="Davon verworfen"
          question="Wie viel ging an PRs, die nie gemergt wurden?"
          value={pct(c.wasted_share)}
          unit={money(c.wasted)}
          direction={
            <Direction
              label="keine Richtung"
              why="hoch heisst: viele Entwürfe werden reviewt und dann verworfen"
            />
          }
          compare={
            <Compare
              change={cmp.wasted_share}
              now={pct(c.wasted_share)}
              format={pct}
            />
          }
        />
        <Tile
          label="Pro 1000 geänderte Zeilen"
          question="Was kostet das Review einer gemergten Änderung?"
          value={c.per_kloc == null ? "–" : money(c.per_kloc)}
          direction={
            <Direction
              label="nur im Verlauf lesen"
              why="steigt der Wert, werden Reviews pro Änderung teurer"
            />
          }
          compare={
            <Compare
              change={cmp.cost_per_kloc}
              now={c.per_kloc == null ? "–" : money(c.per_kloc)}
              format={money}
            />
          }
        />
      </div>
      <Card className="mt-3">
        <CardHead
          title={m.product == null ? "Nach Produkt" : "Nach Repo"}
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
          Nur Repos, die noergler reviewt: {c.reviewed_repos} im Zeitraum.
        </div>
      </Card>
    </section>
  );
}
