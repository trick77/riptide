// Where a change waits on its way to production: the lead-time breakdown
// per PR as labelled stages, then the review as a pair of numbers that only
// read together, then how many PRs are merged within a day.

import type { LeadChain, Metrics, StageKey } from "./api";
import { GroupedBars } from "./charts";
import { dur, durParts, pct } from "./format";
import {
  Card,
  CardHead,
  Compare,
  Direction,
  Legend,
  SectionHead,
  Swatch,
  Tile,
  type Why,
  WhyPanel,
} from "./ui";

export function stageNames(
  c: LeadChain,
  prod: string,
): Record<StageKey, [string, string]> {
  const pre = c.pre_environment;
  return {
    coding: ["Coding", "erster Commit → PR bereit"],
    review: ["Review", "PR bereit → Approval"],
    merge: ["Merge", "Approval → gemergt"],
    to_pre: [`Bis ${pre}`, `gemergt → auf ${pre}`],
    to_prod: [
      `Warten auf ${prod}`,
      pre
        ? `auf ${pre} (ohne: gemergt) → auf ${prod}`
        : `gemergt → auf ${prod}`,
    ],
  };
}

// Wording of the backend's cycle-time buckets by upper bound in seconds;
// a bound without one says so plainly rather than mislabel a count.
const BUCKET_LABEL: Record<number, string> = {
  900: "< 15 min",
  3600: "15–60 min",
  14400: "1–4 h",
  86400: "4–24 h",
  259200: "1–3 Tage",
};
const OPEN_LABEL: Record<number, string> = { 259200: "> 3 Tage" };

function bucketLabels(bounds: number[]): string[] {
  const last = bounds[bounds.length - 1];
  return bounds
    .map((b) => BUCKET_LABEL[b] ?? `bis ${dur(b)}`)
    .concat(OPEN_LABEL[last] ?? `ab ${dur(last)}`);
}

type Props = {
  m: Metrics;
  why: string | null;
  onWhy: (key: string) => void;
};

function Chain({ m }: { m: Metrics }) {
  const prod = m.delivery.production_stage;
  const chain = m.lead_chain;
  const names = stageNames(chain, prod);
  const withData = chain.stages.filter((s) => s.p50 != null);
  const hot = chain.stages.find((s) => s.hot);
  const pre = m.delivery.lead_time_pre;
  const preChange = m.comparison.lead_time_pre;
  return (
    <Card>
      <CardHead
        title={`Anteil jeder Etappe an der Zeit bis ${prod}`}
        sub={`Pro PR in App-Repos, typische Dauer je Etappe. Die Lead Time oben zählt pro Commit; die Etappen zeigen, wo die Zeit liegt.`}
      />
      {pre && (
        <div className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
          <div className="rounded-ui border border-border-soft px-3 py-2.5">
            <div className="text-[13.5px] text-ink-dim">
              Lead Time bis {pre.environment}:{" "}
              <span className="font-medium text-ink tabular-nums">
                {dur(pre.human.p50)}
              </span>{" "}
              <span className="text-xs text-muted">
                typischer Wert · die langsamsten 10 %: über {dur(pre.human.p90)}
              </span>
            </div>
            {preChange && (
              <Compare
                change={preChange}
                now={dur(pre.human.p50)}
                format={dur}
              />
            )}
          </div>
          <div className="rounded-ui border border-border-soft px-3 py-2.5 text-[13.5px] text-ink-dim">
            Lead Time bis {prod}:{" "}
            <span className="font-medium text-ink tabular-nums">
              {dur(m.delivery.lead_time.human.p50)}
            </span>
            <div className="mt-1 text-xs text-muted">
              Der Abstand zwischen beiden ist die Zeit vor {prod}: liegt sie
              dort, ist die Pipeline schnell und das Release der Engpass.
            </div>
          </div>
        </div>
      )}
      {withData.length > 0 ? (
        <>
          <div
            role="img"
            aria-label={`Anteil an der Zeit bis ${prod}: ${withData.map((s) => `${names[s.key][0]} ${pct(s.share)}`).join(", ")}`}
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
                    {s.share == null ? "" : `· ${pct(s.share)} der Zeit`}
                  </span>
                </div>
                <div className="text-xs text-muted">
                  die langsamsten 10 %: über {dur(s.p90)}
                </div>
                <div
                  className={`text-xs ${s.low_sample ? "text-ochre" : "text-muted"}`}
                >
                  {s.n} PRs{s.low_sample ? " · wenig belastbar" : ""}
                </div>
              </li>
            ))}
          </ol>
          {hot && (
            <div className="mt-3 rounded-ui border border-accent-dim bg-accent/10 px-3.5 py-2.5 text-[13.5px] text-ink-dim">
              <span className="font-medium text-accent-strong">
                Der Hebel liegt bei «{names[hot.key][0]}»
              </span>
              : {pct(hot.share)} der Zeit bis {prod}. Die kurzen Etappen zu
              beschleunigen, bringt wenig.
            </div>
          )}
        </>
      ) : (
        <p className="mt-4 mb-0 text-[13.5px] text-muted">
          Keine gemergten PRs im Zeitraum.
        </p>
      )}
      <p className="mt-2.5 mb-0 text-xs text-muted">
        Review ist eine eigene Etappe, weil das Approval meist der erste Kontakt
        des Reviewers ist. Die letzten zwei Etappen erreichen nur PRs, deren
        Merge-Commit einem Deploy zugeordnet ist.
      </p>
    </Card>
  );
}

export function Flow({ m, why, onWhy }: Props) {
  const prod = m.delivery.production_stage;
  const r = m.review;
  const c = m.comparison;
  const rv = durParts(r.review.p50);
  const labels = bucketLabels(r.cycle_histogram.upper_bounds_s);
  const day = r.cycle_histogram.upper_bounds_s.indexOf(86400);
  const series = [
    { key: "h", label: "Menschen", className: "bg-s1" },
    ...(m.bots ? [{ key: "b", label: "Bots", className: "bg-s2" }] : []),
  ];
  const hist = [r.cycle_histogram.human, r.cycle_histogram.bot];

  const whys: Record<string, Why> = {
    review: {
      title: "Review-Zeit",
      means:
        "Wie lange ein fertiger PR auf das erste Approval wartet. In dieser Zeit liegt die Arbeit still, und der Autor beginnt oft schon etwas Neues.",
      watch:
        "die langsamsten 10 % weit über dem typischen Wert liegen: einzelne PRs bleiben liegen, oft grosse oder solche ohne klaren Reviewer.",
      lever:
        "Review-Zeiten im Team vereinbaren, PRs klein halten, Reviewer gleich beim Öffnen nennen.",
      def: "Von PR bereit (opened, bei Drafts ready_for_review) bis zum ersten Approval eines menschlichen Reviewers.",
      basis: `${r.review.n} PRs mit Approval.`,
      filters: [
        "Reviewer ≠ PR-Ersteller",
        "non_human_identities",
        "ohne GitOps-Repos",
      ],
      coverage: "Vollständig für Bitbucket-Events.",
      query: "README · Lead-time breakdown query",
    },
    touch: {
      title: "Approval ohne Kommentar",
      means:
        "Wie oft das Approval die erste Reaktion des Reviewers ist. Hoch heisst: Review ohne sichtbare Diskussion in Bitbucket.",
      watch:
        "der Wert zusammen mit sehr kurzen Review-Zeiten steigt: dann wird vielleicht durchgewinkt.",
      lever:
        "keine Zielvorgabe. Im Team besprechen, ob grosse Änderungen anderswo diskutiert werden.",
      def: "Anteil der approvten PRs, deren erster Reviewer-Kontakt das Approval selbst ist.",
      basis: `${r.approved_on_first_touch} von ${r.approved} approvten PRs.`,
      filters: ["ohne Bot-Kommentare", "ohne Bots", "ohne GitOps-Repos"],
      coverage: "Mündliche Reviews und Pairing sieht Riptide nicht.",
      query: "README · Lead-time breakdown query",
    },
    cycle: {
      title: "Innert 24 h gemergt",
      means:
        "Wie viele PRs innert 24 Stunden nach dem Öffnen gemergt sind. Danach ist der Autor meist schon bei der nächsten Aufgabe; Rückfragen kosten dann doppelt.",
      watch:
        "viele PRs über drei Tage offen sind: das sind meist grosse PRs oder solche, die auf jemanden warten.",
      lever: "PRs kleiner schneiden, Review früh anfragen.",
      def: `pr:merged minus pr:opened pro PR, Schlüssel (repo_full_name, pr_id). Die PR-Cycle-Time ist typischerweise ${dur(r.cycle.p50)}, die langsamsten 10 % über ${dur(r.cycle.p90)}.`,
      basis: `${r.within_day.count} von ${r.within_day.n} menschlichen PRs, gemergt im Zeitraum.`,
      filters: ["ohne GitOps-Repos", "ohne Bots", "pr:merged im Zeitraum"],
      coverage:
        "PRs, die vor Datenbeginn geöffnet wurden, haben kein pr:opened und fehlen.",
      query: "README · PR cycle time",
    },
  };
  const open = why && whys[why] ? whys[why] : null;

  return (
    <section id="fluss" aria-labelledby="h-fluss" className="mt-10">
      <SectionHead
        id="h-fluss"
        eyebrow={`Vom Commit nach ${prod} · nur App-Repos`}
        title="Wo wartet eine Änderung?"
        lead="Die Zeit bis zum Nutzer, aufgeteilt in Etappen. Wo die meiste Zeit liegt, liegt der Hebel; die kurzen Etappen zu beschleunigen, bringt wenig."
        frameworks={[
          ["DORA", "Change lead time, aufgeschlüsselt"],
          ["Flow", "Flow efficiency"],
          ["DevEx", "Feedback loops"],
          ["SPACE", "Efficiency & flow"],
        ]}
      />
      <Chain m={m} />

      <Card className="mt-3">
        <CardHead
          title="Review: schnell, aber wie gründlich?"
          sub={`Beide Zahlen zusammen lesen: wer nur auf eine optimiert, winkt durch oder bremst. Nur PRs von Menschen${m.bots ? "; Bots im Histogramm separat" : ""}.`}
        />
        <div className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-3">
          <Tile
            label="Review-Zeit"
            question="Wie lange wartet ein PR auf das erste Approval?"
            value={rv.value}
            unit={r.review.p50 == null ? "" : `${rv.unit}, typischer Wert`}
            note={`die langsamsten 10 %: über ${dur(r.review.p90)} · ${r.review.n} PRs`}
            direction={
              <Direction
                arrow="down"
                label="kürzer ist besser"
                why="nur mit dem eigenen Verlauf vergleichen"
              />
            }
            compare={
              <Compare
                change={c.review_time}
                now={dur(r.review.p50)}
                format={dur}
              />
            }
            open={why === "review"}
            onWhy={() => onWhy("review")}
          />
          <Tile
            label="Approval ohne Kommentar"
            question="Wie oft ist das Approval die erste Reaktion des Reviewers?"
            value={pct(r.first_touch_rate)}
            unit={`${r.approved_on_first_touch} von ${r.approved}`}
            direction={
              <Direction
                label="keine Richtung"
                why="Gesprächsanlass, kein Ziel"
              />
            }
            compare={
              <Compare
                change={c.first_touch}
                now={pct(r.first_touch_rate)}
                format={pct}
              />
            }
            open={why === "touch"}
            onWhy={() => onWhy("touch")}
          />
        </div>
        <div className="mt-5 grid grid-cols-[minmax(240px,1fr)_2fr] gap-5 max-md:grid-cols-1">
          <Tile
            label="Innert 24 h gemergt"
            question="Wie viele PRs sind innert 24 h nach dem Öffnen gemergt?"
            value={pct(r.within_day.rate)}
            unit={`${r.within_day.count} von ${r.within_day.n} PRs`}
            direction={
              <Direction
                arrow="up"
                label="mehr ist besser"
                why="kleine PRs, schnelles Review"
              />
            }
            compare={
              <Compare
                change={c.within_day}
                now={pct(r.within_day.rate)}
                format={pct}
              />
            }
            open={why === "cycle"}
            onWhy={() => onWhy("cycle")}
          />
          <div className="min-w-0">
            <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
              <div className="text-[12.5px] text-muted">
                PRs nach Dauer vom Öffnen bis zum Merge
                {day >= 0 && " · die Linie markiert 24 h"}
              </div>
              {m.bots && (
                <Legend>
                  {series.map((s) => (
                    <Swatch
                      key={s.key}
                      className={s.className}
                      label={s.label}
                    />
                  ))}
                </Legend>
              )}
            </div>
            <GroupedBars
              labels={labels}
              series={series}
              values={hist.slice(0, series.length)}
              markAfter={day >= 0 ? day : undefined}
              markLabel="24 h"
              ariaLabel={`PR-Cycle-Time: ${labels.map((l, i) => `${l} ${hist[0][i]}`).join(", ")}`}
            />
          </div>
        </div>
      </Card>
      {open && <WhyPanel why={open} />}
    </section>
  );
}
