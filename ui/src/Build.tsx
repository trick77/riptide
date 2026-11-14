import { useState } from "react";
import type { Dataset } from "./api";
import { BarList, RateLines } from "./charts";
import { dur, durParts, fullDate, num, pct } from "./format";
import {
  builds,
  repoDurations,
  type BuildStats,
  type Filter,
  type RunKind,
} from "./metrics";
import type { Period } from "./period";
import { Card, CardHead, Legend, Note, SectionHead } from "./ui";

export const KIND_LABEL: Record<RunKind, string> = {
  release: "Releases",
  main: "master-Builds",
  feature: "Feature- und PR-Builds",
  other: "Andere Branches",
  unknown: "master, Art unbekannt",
};

const KIND_NOTE: Record<RunKind, string> = {
  release: "build_type release oder hotfix",
  main: "Hauptbranch, kein Release",
  feature: "feature/…, bugfix/…, PR-…",
  other: "release/… ohne Release, Nightlies",
  unknown: "vor der ersten build_type-Meldung",
};

// Line colours in the series ramp's fixed order, by kind, so a kind keeps
// its colour whichever kinds the filter leaves.
const KIND_STROKE: Record<RunKind, string> = {
  release: "var(--color-s1)",
  main: "var(--color-s2)",
  feature: "var(--color-s3)",
  other: "var(--color-s4)",
  unknown: "var(--color-axis)",
};

function KindRow({ b }: { b: BuildStats }) {
  const d = durParts(b.duration.p50);
  const rate = b.runs ? b.succeeded / b.runs : null;
  return (
    <div className="contents">
      <div className="border-b border-border-soft py-2">
        <div className="flex items-center gap-2 text-[13.5px] text-ink-dim">
          <span
            aria-hidden="true"
            className="size-2.5 rounded-[3px]"
            style={{ background: KIND_STROKE[b.kind] }}
          />
          {KIND_LABEL[b.kind]}
        </div>
        <div className="text-[11.5px] text-muted">{KIND_NOTE[b.kind]}</div>
      </div>
      <div className="border-b border-border-soft py-2 text-right text-[13.5px] text-ink-dim tabular-nums">
        {b.runs}
      </div>
      <div className="border-b border-border-soft py-2 text-right text-[13.5px] text-ink tabular-nums">
        {pct(rate)}
      </div>
      <div className="border-b border-border-soft py-2 text-right text-[13.5px] text-ink tabular-nums">
        {d.value} <span className="text-muted">{d.unit}</span>
      </div>
      <div className="border-b border-border-soft py-2 text-right text-[13.5px] text-ink-dim tabular-nums">
        {num(b.ciSeconds / 3600, 1)} h
      </div>
      <div
        className="border-b border-border-soft py-2 text-right text-[13.5px] text-ink-dim tabular-nums"
        title={`${b.failed} Läufe ohne SUCCESS`}
      >
        {num(b.wastedSeconds / 3600, 1)} h
      </div>
    </div>
  );
}

export function Build({
  ds,
  period,
  filter,
}: {
  ds: Dataset;
  period: Period;
  filter: Filter;
}) {
  const stats = builds(ds, period, filter);
  const [kind, setKind] = useState<RunKind>("main");
  const shown = stats.some((s) => s.kind === kind)
    ? kind
    : (stats[0]?.kind ?? "main");
  const repos = repoDurations(ds, filter, shown).slice(0, 8);
  const from = ds.meta.build_data_from;
  const gapUntil = from
    ? period.weeks.filter((w) => w.end.getTime() <= Date.parse(from)).length
    : period.weeks.length;
  const lines = stats.filter((s) => s.kind !== "unknown");

  return (
    <section id="build" aria-labelledby="h-build" className="mt-10">
      <SectionHead
        id="h-build"
        eyebrow="Jenkins · Tekton · nie über Lauf-Arten gemittelt"
        title="Build"
      />
      {from && Date.parse(from) > period.from.getTime() && (
        <Note>
          Build-Daten ab {fullDate(new Date(from))}: der erste Lauf, den riptide
          für das Team erhalten hat. Ältere Wochen bleiben leer statt falsch.
        </Note>
      )}
      {stats.length === 0 ? (
        <Card className="mt-3">
          <p className="m-0 text-[13.5px] text-muted">
            Keine abgeschlossenen Läufe im Zeitraum.
          </p>
        </Card>
      ) : (
        <>
          <Card className="mt-3 overflow-x-auto">
            <CardHead
              title="Läufe nach Art"
              sub="Releases überspringen Tests, Feature-Builds scheitern öfter: jede Art für sich"
            />
            <div className="mt-3.5 grid min-w-[620px] grid-cols-[minmax(180px,2fr)_64px_72px_96px_80px_96px] items-center gap-x-3.5">
              {[
                "Art",
                "Läufe",
                "Erfolg",
                "Dauer p50",
                "CI-Zeit",
                "verschwendet",
              ].map((h, i) => (
                <div
                  key={h}
                  className={`border-b border-border pb-2 text-[11px] font-medium tracking-[.12em] text-muted uppercase ${i ? "text-right" : ""}`}
                >
                  {h}
                </div>
              ))}
              {stats.map((b) => (
                <KindRow key={b.kind} b={b} />
              ))}
            </div>
            <p className="mt-2.5 mb-0 text-xs text-muted">
              Erfolg: COMPLETED-Läufe mit Status SUCCESS. Verschwendet: Dauer
              der Läufe mit FAILURE, UNSTABLE oder ABORTED.
            </p>
          </Card>
          <div className="mt-3 flex flex-wrap gap-3">
            <Card className="flex-[2_1_520px]">
              <CardHead
                title="Erfolgsquote pro Woche"
                sub="Je Lauf-Art, Skala 0–100 %"
                right={
                  <Legend>
                    {lines.map((s) => (
                      <span
                        key={s.kind}
                        className="inline-flex items-center gap-1.5"
                      >
                        <span
                          aria-hidden="true"
                          className="h-0.5 w-3 rounded"
                          style={{ background: KIND_STROKE[s.kind] }}
                        />
                        {KIND_LABEL[s.kind]}
                      </span>
                    ))}
                  </Legend>
                }
              />
              {lines.length ? (
                <RateLines
                  labels={period.weeks.map((w) => `KW ${w.kw}`)}
                  lines={lines.map((s) => ({
                    key: s.kind,
                    label: KIND_LABEL[s.kind],
                    stroke: KIND_STROKE[s.kind],
                    values: s.weekly,
                  }))}
                  gapUntil={gapUntil}
                  format={(v) => pct(v)}
                  ariaLabel={`Erfolgsquote pro Woche: ${lines
                    .map(
                      (s) =>
                        `${KIND_LABEL[s.kind]} ${pct(s.runs ? s.succeeded / s.runs : null)}`,
                    )
                    .join(", ")}`}
                />
              ) : (
                <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
                  Nur Läufe unbekannter Art im Zeitraum: ohne build_type lässt
                  sich ein Release nicht von einem master-Build trennen, darum
                  keine Wochenquote.
                </p>
              )}
            </Card>
            <Card className="flex-[1_1_320px]">
              <CardHead
                title="Build-Dauer nach Repo"
                sub="Median, längste zuerst"
              />
              <div
                role="group"
                aria-label="Lauf-Art"
                className="no-print mt-3 flex flex-wrap gap-1"
              >
                {stats.map((s) => (
                  <button
                    key={s.kind}
                    type="button"
                    aria-pressed={s.kind === shown}
                    onClick={() => setKind(s.kind)}
                    className={`rounded-md border-0 px-2.5 py-1 text-xs ${
                      s.kind === shown
                        ? "bg-active text-ink shadow-[inset_0_0_0_1px] shadow-elevated-border"
                        : "bg-transparent text-muted"
                    }`}
                  >
                    {KIND_LABEL[s.kind]}
                  </button>
                ))}
              </div>
              <BarList
                rows={repos.map((r) => ({
                  label: r.repo,
                  value: r.duration,
                  note: `${r.runs} Läufe`,
                }))}
                format={dur}
              />
            </Card>
          </div>
        </>
      )}
    </section>
  );
}
