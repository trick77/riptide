// How evenly code reaches production: releases per week against the
// team's own median week, and the mix of work merged.

import type { Metrics } from "./api";
import { WeekStack } from "./charts";
import { num, pct } from "./format";
import { Card, CardHead, Compare, Legend, SectionHead, Swatch } from "./ui";

export function Deploys({ m }: { m: Metrics }) {
  const prod = m.delivery.production_stage;
  const freq = m.delivery.frequency;
  const weeks = m.period.weeks;
  return (
    <section id="deploys" aria-labelledby="h-deploys" className="mt-10">
      <SectionHead
        id="h-deploys"
        eyebrow="Ausrollen"
        title={`Wie gleichmässig kommt Code nach ${prod}?`}
        lead="Gleichmässig ist gut: Lücken und Spitzen deuten auf Sammelreleases, die viele Änderungen auf einmal ausliefern."
        frameworks={[
          ["DORA", "Deployment frequency"],
          ["Core 4", "Speed, sekundär"],
          ["Flow", "Flow velocity"],
        ]}
      />
      <Card>
        <CardHead
          title={`Releases auf ${prod} pro Woche`}
          sub="Nur Releases mit neuen Images; die Linie ist die typische Woche des Teams"
          right={
            <Legend>
              <Swatch className="bg-s1" label="Release" />
            </Legend>
          }
        />
        <WeekStack
          labels={weeks.map((w) => `KW ${w.kw}`)}
          series={[{ key: "release", label: "Releases", className: "bg-s1" }]}
          values={freq.weeks}
          reference={
            freq.median_week == null
              ? undefined
              : {
                  value: freq.median_week,
                  label: `typisch ${num(freq.median_week, 1)} pro Woche`,
                }
          }
          ariaLabel={`Releases auf ${prod} pro Woche: ${freq.weeks
            .map((w, i) => `KW ${weeks[i].kw} ${w.release}`)
            .join(", ")}`}
        />
        <p className="mt-2.5 mb-0 text-xs text-muted">
          {freq.resyncs} Syncs ohne neue Images und {freq.first_seen}{" "}
          Erstdeploys zählen nicht; was sie sind, steht bei der Deploy-Frequenz
          unter «Was heisst das?». Die typische Woche ist der Median über die
          ganzen Wochen, in denen Argo CD meldete.
        </p>
      </Card>
    </section>
  );
}

const MIX: [keyof Metrics["review"]["mix_total"], string, string][] = [
  ["feature", "feature", "bg-s1"],
  ["bugfix", "bugfix", "bg-s2"],
  ["hotfix", "hotfix", "bg-s3"],
  ["other", "anderes / ohne Präfix", "bg-other"],
];

export function Mix({ m }: { m: Metrics }) {
  const r = m.review;
  const shown = MIX.filter(([k]) => (r.mix_share[k] ?? 0) > 0);
  return (
    <section id="arbeitsmix" aria-labelledby="h-arbeitsmix" className="mt-10">
      <SectionHead
        id="h-arbeitsmix"
        eyebrow="Arbeitsmix"
        title="Woran arbeitet das Team?"
        lead="Anteil der gemergten PRs nach Branch-Präfix, nicht der Zeit. Kein Zielwert: wichtig ist, ob der Mix zum Plan des Teams passt."
        frameworks={[
          ["Core 4", "Impact, Näherung für «Zeit für Neues»"],
          ["Flow", "Flow distribution"],
        ]}
      />
      <Card>
        <CardHead
          title="Gemergte PRs nach Art"
          sub={`${r.merged} PRs im Zeitraum, ${m.bots ? "Bots eingeschlossen" : "ohne Bots"}`}
        />
        {shown.length ? (
          <>
            <div
              role="img"
              aria-label={`Arbeitsmix: ${shown.map(([k, l]) => `${l} ${pct(r.mix_share[k])}`).join(", ")}`}
              className="mt-3.5 flex h-7 gap-0.5"
            >
              {shown.map(([k, l, cls]) => (
                <div
                  key={k}
                  title={`${l}: ${r.mix_total[k]} PRs, ${pct(r.mix_share[k])}`}
                  className={`flex h-7 min-w-[4px] items-center overflow-hidden px-2 text-xs whitespace-nowrap text-ink first:rounded-l-md last:rounded-r-md ${cls}`}
                  style={{ flex: `${r.mix_share[k]} 1 0` }}
                >
                  {pct(r.mix_share[k])}
                </div>
              ))}
            </div>
            <div className="mt-2">
              <Legend>
                {MIX.map(([k, l, cls]) => (
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
            Keine gemergten PRs im Zeitraum.
          </p>
        )}
        <div className="mt-4 text-[13.5px] text-ink-dim">
          <span className="font-medium text-ink">
            {pct(r.untracked_rate)} ohne Jira-Key
          </span>{" "}
          ({r.untracked} von {r.merged}). Diese Arbeit taucht in keiner Planung
          auf. Frage fürs Team: Key vergessen oder ungeplante Arbeit?
        </div>
        <Compare
          change={m.comparison.untracked}
          now={pct(r.untracked_rate)}
          format={pct}
        />
      </Card>
    </section>
  );
}
