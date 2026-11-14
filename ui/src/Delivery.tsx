import type { LeadTimes, Metrics } from "./api";
import { WeekStack } from "./charts";
import { dur, durParts, fullDate, num, pct } from "./format";
import { LeadChain } from "./LeadChain";
import {
  Card,
  CardHead,
  Grid,
  Legend,
  SectionHead,
  Swatch,
  Tile,
  type Why,
  WhyPanel,
} from "./ui";

type Props = {
  m: Metrics;
  why: string | null;
  onWhy: (key: string) => void;
};

function leadTile(l: LeadTimes, showBots: boolean) {
  const p = durParts(l.human.p50);
  return {
    value: p.value,
    unit: l.human.p50 == null ? "" : `${p.unit} p50`,
    note:
      (l.human.n === 0
        ? "keine menschlichen Commits zugeordnet"
        : `p90 ${dur(l.human.p90)} · ${l.human.n} menschliche Commits`) +
      (showBots && l.bot.n > 0
        ? ` · Bots p50 ${dur(l.bot.p50)} (${l.bot.n})`
        : ""),
    warn: l.low_sample ? "Weniger als 10 Commits: wenig belastbar" : undefined,
  };
}

export function Delivery({ m, why, onWhy }: Props) {
  // As the numbers were computed, not as the toggle stands while they load.
  const bots = m.bots;
  const { production_stage: prod, pre_environment: pre } = m.delivery;
  const freq = m.delivery.frequency;
  const lt = m.delivery.lead_time;
  const ltPre = m.delivery.lead_time_pre;
  const sinceNote = freq.since
    ? `Argo CD meldet erst seit ${fullDate(new Date(freq.since))}: die Wochen davor sind leer, die Rate gilt ab dann.`
    : null;

  const whys: Record<string, Why> = {
    freq: {
      title: "Deploy-Frequenz",
      def: `Erfolgreiche Deploys auf ${prod}, deren Images sich vom vorherigen Deploy derselben Argo-App unterscheiden, einmal pro GitOps-Revision gezählt, geteilt durch die Wochen im Zeitraum, in denen Argo CD meldete.`,
      basis: `${freq.releases} Releases, ${freq.resyncs} Syncs ohne neue Images und ${freq.first_seen} Erstdeploys aus ${freq.syncs} Revisionen.`,
      filters: [
        "operation_phase = Succeeded",
        `environment = ${prod}`,
        "images ≠ vorheriger Deploy",
        "pro revision",
      ],
      coverage:
        (sinceNote ? `${sinceNote} ` : "") +
        (freq.first_seen > 0
          ? "Der erste Deploy einer App seit Datenbeginn hat keinen Vorgänger: Release oder Re-Sync ist unbekannt, er zählt nicht als Release."
          : "Jede App hat einen Vorgänger in den Daten, alle Deploys sind vergleichbar."),
      query: "README · Deployment-frequency query",
    },
    lead: {
      title: `Lead Time ${prod}`,
      def: `Pro Commit vom Autorenzeitpunkt bis zum ersten Deploy auf ${prod}, der ihn enthält. Merge-Commits und Release-Tooling ausgenommen, Bots separat.`,
      basis: `${lt.human.n} menschliche Commits, zuerst deployt im Zeitraum${lt.bot.n ? `, dazu ${lt.bot.n} von Bots` : ""}.`,
      filters: [
        "NOT is_merge",
        "NOT service account",
        "Bots separat",
        `environment = ${prod}`,
      ],
      coverage:
        "Nur Commits, die riptide einem Deploy zuordnen kann (Image-Referenz des Builds oder Release-Notes); ein Deploy ohne Zuordnung fehlt hier.",
      query: "lead_time_changes · docs/dora-lead-time.md",
    },
    leadPre: {
      title: `Lead Time ${pre ?? ""}`,
      def: `Wie Lead Time ${prod}, bis zum ersten Deploy auf ${pre}.`,
      basis: `${ltPre?.human.n ?? 0} menschliche Commits, zuerst auf ${pre} deployt im Zeitraum.`,
      filters: [
        "NOT is_merge",
        "NOT service account",
        "Bots separat",
        `environment = ${pre}`,
      ],
      coverage: "Wie bei der Lead Time auf Prod.",
      query: "lead_time_changes",
    },
    dsucc: {
      title: "Deploy-Erfolg",
      def: `Anteil der Syncs auf ${prod} mit operation_phase Succeeded an allen abgeschlossenen Syncs.`,
      basis: `${freq.finished} abgeschlossene Syncs, davon ${freq.failed} Failed.`,
      filters: [
        "operation_phase IN (Succeeded, Failed)",
        `environment = ${prod}`,
      ],
      coverage: "Vollständig: Argo CD meldet jeden abgeschlossenen Sync.",
      query: "argocd_events",
    },
  };
  const open = why && whys[why] ? whys[why] : null;
  const lead = leadTile(lt, bots);
  const leadPre = ltPre ? leadTile(ltPre, bots) : null;
  const weeks = m.period.weeks;

  return (
    <section id="delivery" aria-labelledby="h-delivery" className="mt-9">
      <SectionHead id="h-delivery" eyebrow="DORA" title="Delivery" />
      <Grid>
        <Tile
          label={`Deploy-Frequenz ${prod}`}
          value={freq.syncs ? num(freq.per_week, 1) : "–"}
          unit={freq.syncs ? "Releases / Woche" : ""}
          note={
            freq.syncs
              ? `${freq.releases} Releases aus ${freq.syncs} Syncs`
              : freq.argo_reporting
                ? "keine Deploys im Zeitraum"
                : "Argo CD meldet für das Team noch nicht"
          }
          warn={
            sinceNote ??
            (freq.first_seen > 0
              ? `${freq.first_seen} Erstdeploys ohne Vergleichswert, separat gezeigt`
              : undefined)
          }
          open={why === "freq"}
          onWhy={() => onWhy("freq")}
        />
        <Tile
          label={`Lead Time ${prod}`}
          {...lead}
          open={why === "lead"}
          onWhy={() => onWhy("lead")}
        />
        {leadPre && (
          <Tile
            label={`Lead Time ${pre}`}
            {...leadPre}
            open={why === "leadPre"}
            onWhy={() => onWhy("leadPre")}
          />
        )}
        <Tile
          label={`Deploy-Erfolg ${prod}`}
          value={pct(freq.success_rate)}
          note={`${freq.failed} fehlgeschlagen von ${freq.finished} Syncs`}
          open={why === "dsucc"}
          onWhy={() => onWhy("dsucc")}
        />
      </Grid>
      {open && <WhyPanel why={open} />}

      <LeadChain chain={m.lead_chain} prod={prod} />

      <div className="mt-3 flex flex-wrap gap-3">
        <Card className="flex-[2_1_560px]">
          <CardHead
            title={`Deploys auf ${prod} pro Woche`}
            sub="Nur Releases zählen als Deploy-Frequenz"
            right={
              <Legend>
                <Swatch className="bg-s1" label="Release (neue Images)" />
                <Swatch className="bg-other" label="Sync ohne neue Images" />
                <Swatch
                  className="texture-unknown"
                  label="Erstdeploy, ohne Vergleich"
                />
              </Legend>
            }
          />
          <WeekStack
            labels={weeks.map((w) => `KW ${w.kw}`)}
            series={[
              { key: "release", label: "Releases", className: "bg-s1" },
              {
                key: "sync",
                label: "Syncs ohne neue Images",
                className: "bg-other",
              },
              {
                key: "first_seen",
                label: "Erstdeploys",
                className: "texture-unknown",
              },
            ]}
            values={freq.weeks}
            ariaLabel={`Deploys auf ${prod} pro Woche: ${freq.weeks
              .map((w, i) => `KW ${weeks[i].kw} ${w.release} Releases`)
              .join(", ")}`}
          />
        </Card>
        <Card className="flex-[1_1_300px]">
          <CardHead
            title="Woraus die Deploy-Frequenz besteht"
            sub={`Erfolgreiche Syncs auf ${prod} im Zeitraum, pro Revision`}
          />
          <div className="mt-4 flex flex-col gap-3.5">
            {(
              [
                [
                  "Releases mit neuen Images",
                  freq.releases,
                  "bg-s1",
                  "zählen als Deploy-Frequenz",
                ],
                [
                  "Syncs ohne neue Images",
                  freq.resyncs,
                  "bg-other",
                  "Config-Änderungen, manuelle Re-Syncs",
                ],
                [
                  "Erstdeploys ohne Vergleich",
                  freq.first_seen,
                  "texture-unknown",
                  freq.first_seen
                    ? "erster Deploy einer App seit Datenbeginn"
                    : "keine im Zeitraum",
                ],
              ] as const
            ).map(([label, v, cls, note]) => (
              <div key={label}>
                <div className="flex justify-between text-[13.5px] text-ink-dim">
                  <span>{label}</span>
                  <span className="text-ink tabular-nums">{v}</span>
                </div>
                <div className="mt-1 h-2 overflow-hidden rounded bg-border-soft">
                  <div
                    className={`h-full rounded ${cls}`}
                    style={{
                      width: `${freq.syncs ? (v / freq.syncs) * 100 : 0}%`,
                    }}
                  />
                </div>
                <div className="mt-1 text-xs text-muted">{note}</div>
              </div>
            ))}
          </div>
          <div className="mt-4 border-t border-border-soft pt-3 text-[12.5px] text-muted">
            Aktualisiert ein Release mehrere Apps über dieselbe GitOps-Revision,
            zählt es einmal.
          </div>
        </Card>
      </div>
    </section>
  );
}
