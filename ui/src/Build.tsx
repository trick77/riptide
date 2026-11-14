import { useState } from "react";
import type {
  BuildStats,
  KindHotspots,
  Metrics,
  Outlier,
  RepoFailures,
  RunKind,
} from "./api";
import { BarList, RateLines } from "./charts";
import { dateTime, dur, durParts, fullDate, num, pct } from "./format";
import { Card, CardHead, Legend, Mono, Note, Pill, SectionHead } from "./ui";

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

const HEAD =
  "border-b border-border pb-2 text-[11px] font-medium tracking-[.12em] text-muted uppercase";
const CELL = "border-b border-border-soft py-2 text-[13.5px] tabular-nums";

function KindRow({ b }: { b: BuildStats }) {
  const d = durParts(b.duration.p50);
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
      <div className={`${CELL} text-right text-ink-dim`}>{b.runs}</div>
      <div className={`${CELL} text-right text-ink`}>{pct(b.success_rate)}</div>
      <div className={`${CELL} text-right text-ink`}>
        {d.value} <span className="text-muted">{d.unit}</span>
      </div>
      <div className={`${CELL} text-right text-ink-dim`}>
        {num(b.ci_seconds / 3600, 1)} h
      </div>
      <div
        className={`${CELL} text-right text-ink-dim`}
        title={`${b.failed} Läufe ohne SUCCESS`}
      >
        {num(b.wasted_seconds / 3600, 1)} h
      </div>
    </div>
  );
}

function FailingRow({ f }: { f: RepoFailures }) {
  return (
    <div className="contents">
      <div className={`${CELL} overflow-hidden`}>
        <Mono className="text-[12px] text-ellipsis whitespace-nowrap text-ink-dim">
          {f.repo}
        </Mono>
      </div>
      <div className={`${CELL} text-right text-ink-dim`}>
        {f.failures} / {f.runs}
      </div>
      <div className={`${CELL} text-right text-ink`}>{pct(f.failure_rate)}</div>
      <div className={`${CELL} text-right text-ink`}>
        {num(f.wasted_seconds / 3600, 1)} h
      </div>
      <div className={`${CELL} text-right`}>
        {f.flaky ? <Pill tone="fail">auffällig</Pill> : null}
      </div>
    </div>
  );
}

function Hotspots({
  k,
  minRuns,
  flaky,
}: {
  k: KindHotspots;
  minRuns: number;
  flaky: number;
}) {
  return (
    <Card className="flex-[1_1_420px] overflow-x-auto">
      <CardHead
        title="Fehler-Hotspots"
        sub={`Nach verschwendeter CI-Zeit · Team-Quote ${pct(k.failure_rate)}`}
      />
      {k.failing.length ? (
        <div className="mt-3.5 grid min-w-[420px] grid-cols-[minmax(140px,2fr)_64px_56px_64px_84px] items-center gap-x-3">
          {["Repo", "Fehler", "Quote", "verschw.", ""].map((h, i) => (
            <div key={h || i} className={`${HEAD} ${i ? "text-right" : ""}`}>
              {h}
            </div>
          ))}
          {k.failing.map((f) => (
            <FailingRow key={f.repo} f={f} />
          ))}
        </div>
      ) : (
        <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
          Keine fehlgeschlagenen Läufe dieser Art.
        </p>
      )}
      <p className="mt-2.5 mb-0 text-xs text-muted">
        Fehler: FAILURE und UNSTABLE. ABORTED zählt weder als Fehler noch als
        Lauf. Auffällig: Quote mindestens {num(flaky, 1)}× die des Teams,
        höchstens halbwegs zwischen ihr und 100 %, ab {minRuns} Läufen.
      </p>
    </Card>
  );
}

function Outliers({
  rows,
  factor,
  minRuns,
}: {
  rows: Outlier[];
  factor: number;
  minRuns: number;
}) {
  return (
    <Card className="mt-3 overflow-x-auto">
      <CardHead
        title="Ausreisser"
        sub={`Läufe über ${num(factor, 0)}× dem p90 ihrer Art, längste zuerst`}
      />
      {rows.length ? (
        <div className="mt-3.5 grid min-w-[620px] grid-cols-[minmax(180px,2fr)_minmax(130px,1.2fr)_110px_80px_80px] items-center gap-x-3.5">
          {["Pipeline", "Art", "Start", "Dauer", "× p90"].map((h, i) => (
            <div key={h} className={`${HEAD} ${i > 1 ? "text-right" : ""}`}>
              {h}
            </div>
          ))}
          {rows.map((o) => (
            <div key={`${o.pipeline_name}#${o.run_id}`} className="contents">
              <div className={`${CELL} overflow-hidden`}>
                <Mono className="text-[12px] whitespace-nowrap text-ink-dim">
                  {o.pipeline_name}
                </Mono>{" "}
                <span className="text-muted">#{o.run_id}</span>
              </div>
              <div className={`${CELL} text-muted`}>{KIND_LABEL[o.kind]}</div>
              <div className={`${CELL} text-right text-ink-dim`}>
                {o.started_at ? dateTime(o.started_at) : "–"}
              </div>
              <div className={`${CELL} text-right text-ink`}>
                {dur(o.duration_s)}
              </div>
              <div className={`${CELL} text-right text-ink-dim`}>
                {num(o.factor, 0)}×
              </div>
            </div>
          ))}
        </div>
      ) : (
        <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
          Keine Ausreisser im Zeitraum.
        </p>
      )}
      <p className="mt-2.5 mb-0 text-xs text-muted">
        Hängen mehrere Pipelines zur selben Zeit, liegt es an der Infrastruktur
        (Agent weg, wartender Input-Schritt), nicht am Repo. Eine Art braucht
        dafür mindestens {minRuns} Läufe.
      </p>
    </Card>
  );
}

export function Build({ m }: { m: Metrics }) {
  const b = m.build;
  const h = b.hotspots;
  const stats = b.kinds;
  const [kind, setKind] = useState<RunKind>("main");
  const shown = stats.some((s) => s.kind === kind)
    ? kind
    : (stats[0]?.kind ?? "main");
  const k = h.kinds.find((x) => x.kind === shown);
  const lines = stats.filter((s) => s.kind !== "unknown");
  const weeks = m.period.weeks;

  return (
    <section id="build" aria-labelledby="h-build" className="mt-10">
      <SectionHead
        id="h-build"
        eyebrow="Jenkins · Tekton · nie über Lauf-Arten gemittelt"
        title="Build"
      />
      {b.starts_inside && b.data_from && (
        <Note>
          Build-Daten ab {fullDate(new Date(b.data_from))}: der erste Lauf, den
          riptide für das Team erhalten hat. Ältere Wochen bleiben leer statt
          falsch.
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
              ].map((t, i) => (
                <div key={t} className={`${HEAD} ${i ? "text-right" : ""}`}>
                  {t}
                </div>
              ))}
              {stats.map((s) => (
                <KindRow key={s.kind} b={s} />
              ))}
            </div>
            <p className="mt-2.5 mb-0 text-xs text-muted">
              Erfolg: COMPLETED-Läufe mit Status SUCCESS. Verschwendet: Dauer
              der Läufe mit FAILURE, UNSTABLE oder ABORTED.
            </p>
          </Card>
          <Card className="mt-3">
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
                labels={weeks.map((w) => `KW ${w.kw}`)}
                lines={lines.map((s) => ({
                  key: s.kind,
                  label: KIND_LABEL[s.kind],
                  stroke: KIND_STROKE[s.kind],
                  values: s.weekly,
                }))}
                gapUntil={b.gap_weeks}
                format={(v) => pct(v)}
                ariaLabel={`Erfolgsquote pro Woche: ${lines
                  .map((s) => `${KIND_LABEL[s.kind]} ${pct(s.success_rate)}`)
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

          <div className="mt-6 flex flex-wrap items-baseline justify-between gap-2">
            <h3 className="m-0 font-serif text-[19px] font-medium text-ink">
              Hotspots
            </h3>
            <div
              role="group"
              aria-label="Lauf-Art"
              className="no-print flex flex-wrap gap-1"
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
          </div>
          <p className="mt-1 mb-0 max-w-[90ch] text-[12.5px] text-muted">
            Jedes Repo gegen die Läufe des Teams derselben Art. Die Schwellen
            sind Startwerte, nicht gemessen.
          </p>
          {k && (
            <div className="mt-3 flex flex-wrap gap-3">
              <Card className="flex-[1_1_320px]">
                <CardHead
                  title="Build-Dauer nach Repo"
                  sub={`Median, längste zuerst · Team-Median ${dur(k.duration.p50)}`}
                />
                <BarList
                  rows={k.durations.map((r) => ({
                    label: r.slow ? `${r.repo} · langsam` : r.repo,
                    value: r.duration.p50 ?? 0,
                    note: `p90 ${dur(r.duration.p90)} · ${r.runs} Läufe${
                      r.factor != null ? ` · ${num(r.factor, 1)}×` : ""
                    }`,
                  }))}
                  format={dur}
                />
                <p className="mt-2.5 mb-0 text-xs text-muted">
                  Langsam: Median mindestens {num(h.slow_factor, 1)}× der des
                  Teams, ab {h.min_runs} Läufen.
                </p>
              </Card>
              <Hotspots k={k} minRuns={h.min_runs} flaky={h.flaky_factor} />
            </div>
          )}
          <Outliers
            rows={h.outliers}
            factor={h.outlier_factor}
            minRuns={h.min_runs}
          />
        </>
      )}
    </section>
  );
}
