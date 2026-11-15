// How fast and how reliable the CI's feedback is: a card per kind of run,
// never a number across kinds; one list of the repos that slow it down;
// runs stuck together as one sentence.

import { useState } from "react";
import type {
  BuildStats,
  Metrics,
  Outlier,
  OutlierGroup,
  RepoHotspot,
  RunKind,
} from "./api";
import { clock, dateTime, dayMonth, dur, fullDate, num, pct } from "./format";
import type { Tab } from "./state";
import { Card, CardHead, Compare, Mono, Note, Pill, SectionHead } from "./ui";

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

const SW = 240;
const SH = 48;

// A kind's weekly success rate on the scale all cards share.
function Spark({ b, low }: { b: BuildStats; low: number }) {
  const n = b.weekly.length;
  const x = (i: number) => (n < 2 ? SW / 2 : 4 + (i / (n - 1)) * (SW - 8));
  const y = (v: number) => SH - 4 - ((v - low) / (1 - low)) * (SH - 8);
  const segs: string[] = [];
  let cur = "";
  b.weekly.forEach((v, i) => {
    if (v == null) {
      if (cur) segs.push(cur);
      cur = "";
    } else cur += `${cur ? "L" : "M"}${x(i).toFixed(1)} ${y(v).toFixed(1)} `;
  });
  if (cur) segs.push(cur);
  let last = -1;
  b.weekly.forEach((v, i) => {
    if (v != null) last = i;
  });
  return (
    <svg
      viewBox={`0 0 ${SW} ${SH}`}
      className="mt-2 block h-12 w-full"
      role="img"
      aria-label={`Erfolgsquote pro Woche, ${KIND_LABEL[b.kind]}: ${b.weekly.map((v) => (v == null ? "–" : pct(v))).join(", ")}`}
    >
      <line x1="0" x2={SW} y1="4" y2="4" stroke="var(--color-grid-minor)" />
      <line
        x1="0"
        x2={SW}
        y1={SH - 4}
        y2={SH - 4}
        stroke="var(--color-grid-minor)"
      />
      {segs.map((d, i) => (
        <path
          key={i}
          d={d}
          fill="none"
          stroke={KIND_STROKE[b.kind]}
          strokeWidth="2"
          strokeLinejoin="round"
        />
      ))}
      {last >= 0 && (
        <circle
          cx={x(last)}
          cy={y(b.weekly[last]!)}
          r="3.5"
          fill={KIND_STROKE[b.kind]}
          stroke="var(--color-panel)"
          strokeWidth="2"
        />
      )}
    </svg>
  );
}

function KindCard({ b, low, m }: { b: BuildStats; low: number; m: Metrics }) {
  const c = m.comparison.build.find((x) => x.kind === b.kind);
  const weeks = m.period.weeks;
  return (
    <div className="flex min-w-0 flex-col gap-1 rounded-ui border border-border bg-panel px-4 py-3.5">
      <div className="flex items-center gap-2 text-[13.5px] text-ink-dim">
        <span
          aria-hidden="true"
          className="size-2.5 rounded-[3px]"
          style={{ background: KIND_STROKE[b.kind] }}
        />
        {KIND_LABEL[b.kind]}
      </div>
      <div className="text-[11.5px] text-muted">{KIND_NOTE[b.kind]}</div>
      <div className="mt-1 flex flex-wrap gap-x-5 gap-y-1">
        <div className="text-xs text-muted">
          <div className="text-[20px] font-medium text-ink tabular-nums">
            {pct(b.success_rate)}
          </div>
          erfolgreich
        </div>
        <div className="text-xs text-muted">
          <div className="text-[20px] font-medium text-ink tabular-nums">
            {dur(b.duration.p50)}
          </div>
          typische Dauer
        </div>
        <div className="text-xs text-muted">
          <div className="text-[20px] font-medium text-ink tabular-nums">
            {num(b.wasted_seconds / 3600, 1)} h
          </div>
          rote Läufe, von {num(b.ci_seconds / 3600, 1)} h CI-Zeit
        </div>
      </div>
      {b.waiting.n > 0 && (
        <div
          className="text-xs text-muted"
          title={`Median über ${b.waiting.n} der ${b.duration.n} Läufe, die melden, wann sie einen Agent bekamen`}
        >
          davon typischerweise {dur(b.waiting.p50)} Warten auf einen Agent,{" "}
          {dur(b.building.p50)} Bauen
        </div>
      )}
      <Spark b={b} low={low} />
      <div className="flex justify-between text-[10.5px] text-faint">
        <span>
          KW {weeks[0]?.kw} · Erfolg, Skala {pct(low)}–100 %
        </span>
        <span>KW {weeks[weeks.length - 1]?.kw}</span>
      </div>
      <div className="text-xs text-muted">
        {b.runs} Läufe
        {b.no_verdict > 0 && ` + ${b.no_verdict} ohne Ergebnis`}
        {b.outlier_runs > 0 &&
          ` · ${b.outlier_runs} Ausreisser (${num(b.outlier_seconds / 3600, 1)} h) nicht in Dauer und CI-Zeit`}
      </div>
      {c &&
        // Neither compares, for the same reason: say it once.
        (c.success.prev == null &&
        c.duration.prev == null &&
        c.success.reason === c.duration.reason ? (
          <Compare change={c.success} now="" format={pct} />
        ) : (
          <>
            <Compare
              change={c.success}
              now={pct(b.success_rate)}
              format={pct}
            />
            <Compare
              change={c.duration}
              now={dur(b.duration.p50)}
              format={dur}
            />
          </>
        ))}
    </div>
  );
}

function reason(
  h: RepoHotspot,
  teamRate: number | null,
  teamP50: number | null,
) {
  const out: [string, "fail" | "warn", string][] = [];
  if (h.flaky)
    out.push([
      "fehleranfällig",
      "fail",
      `${pct(h.failure_rate)} rot, Team ${pct(teamRate)}`,
    ]);
  if (h.slow)
    out.push([
      "langsam",
      "warn",
      `typische Dauer ${dur(h.duration.p50)}, Team ${dur(teamP50)} (${num(h.factor ?? 0, 1)}×)`,
    ]);
  return out;
}

function Groups({ groups }: { groups: OutlierGroup[] }) {
  const shared = groups.filter((g) => g.shared);
  if (!shared.length) return null;
  return (
    <ul className="m-0 mt-2 flex list-none flex-col gap-1.5 p-0 text-[13.5px] text-ink-dim">
      {shared.map((g) => (
        <li key={g.from}>
          <span className="font-medium text-ink">
            {g.runs} Läufe aus {g.repos.length} Repos starteten{" "}
            {dayMonth(new Date(g.from)) === dayMonth(new Date(g.to))
              ? `am ${dayMonth(new Date(g.from))} zwischen ${clock(g.from)} und ${clock(g.to)}`
              : `zwischen ${dateTime(g.from)} und ${dateTime(g.to)}`}
          </span>
          : das spricht für die Infrastruktur (Agents weg, wartender
          Input-Schritt), nicht für die Repos.
        </li>
      ))}
    </ul>
  );
}

function OutlierTable({ rows }: { rows: Outlier[] }) {
  return (
    <div className="mt-3 overflow-x-auto">
      <div className="grid min-w-[700px] grid-cols-[minmax(180px,2fr)_minmax(130px,1.2fr)_110px_80px_80px_80px] items-center gap-x-3.5">
        {["Pipeline", "Art", "Start", "Dauer", "Warten", "× Median"].map(
          (h, i) => (
            <div key={h} className={`${HEAD} ${i > 1 ? "text-right" : ""}`}>
              {h}
            </div>
          ),
        )}
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
            <div
              className={`${CELL} text-right text-ink-dim`}
              title={
                o.wait_s == null
                  ? "Der Lauf meldet nicht, wann er einen Agent bekam"
                  : undefined
              }
            >
              {o.wait_s != null ? dur(o.wait_s) : "–"}
            </div>
            <div className={`${CELL} text-right text-ink-dim`}>
              {o.factor != null ? `${num(o.factor, 0)}×` : "–"}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export function Build({
  m,
  onFacts,
}: {
  m: Metrics;
  onFacts: (tab: Tab, q: string) => void;
}) {
  const b = m.build;
  const h = b.hotspots;
  const stats = b.kinds;
  const [kind, setKind] = useState<RunKind>("feature");
  const shown = stats.some((s) => s.kind === kind)
    ? kind
    : (stats[0]?.kind ?? "feature");
  const k = h.kinds.find((x) => x.kind === shown);
  // One scale for every card, so a dip reads the same in each: from the
  // lowest week, rounded down to 10 %, to 100 %.
  const lowest = Math.min(
    1,
    ...stats.flatMap((s) => s.weekly.filter((v): v is number => v != null)),
  );
  const low = Math.min(0.9, Math.floor(lowest * 10) / 10);

  return (
    <section id="build" aria-labelledby="h-build" className="mt-10">
      <SectionHead
        id="h-build"
        eyebrow="CI · Jenkins · Tekton · jede Lauf-Art für sich"
        title="Wie schnell und verlässlich ist das Feedback?"
        lead="Releases überspringen Tests, Feature-Builds scheitern öfter. Darum gibt es keine Summe über alle Arten: jede Karte steht für sich."
        frameworks={[
          ["DevEx", "Feedback loops"],
          ["Core 4", "Effectiveness, Treiber"],
          ["SPACE", "Efficiency & flow"],
        ]}
      />
      {b.starts_inside && b.data_from && (
        <Note>
          Build-Daten ab {fullDate(new Date(b.data_from))}: der erste Lauf, den
          Riptide für das Team erhalten hat. Ältere Wochen bleiben leer statt
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
          <div className="mt-3 grid grid-cols-[repeat(auto-fill,minmax(250px,1fr))] gap-3">
            {stats.map((s) => (
              <KindCard key={s.kind} b={s} low={low} m={m} />
            ))}
          </div>
          <p className="mt-2 mb-0 text-xs text-muted">
            Erfolgreich: SUCCESS (Tekton: Succeeded, Completed). Rot: FAILURE,
            UNSTABLE oder Failed. Ohne Ergebnis (ABORTED, NOT_BUILT): kein Lauf,
            nur CI-Zeit; ein Jenkins-timeout() endet ebenfalls ABORTED.
          </p>

          <Card className="mt-3">
            <CardHead
              title="Repos, die das Feedback bremsen"
              sub="Jedes Repo gegen alle Läufe derselben Art im Team · die Schwellen sind Startwerte, nicht gemessen"
              right={
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
              }
            />
            {k && k.repos.length ? (
              <div className="mt-3 overflow-x-auto">
                <div className="grid min-w-[640px] grid-cols-[minmax(150px,1.3fr)_minmax(220px,2fr)_96px_150px] items-center gap-x-3.5">
                  {["Repo", "Warum", "CI-Zeit rot", ""].map((t, i) => (
                    <div
                      key={t || i}
                      className={`${HEAD} ${i === 2 ? "text-right" : ""}`}
                    >
                      {t}
                    </div>
                  ))}
                  {k.repos.map((r) => {
                    const why = reason(r, k.failure_rate, k.duration.p50);
                    return (
                      <div key={r.repo} className="contents">
                        <div className={`${CELL} overflow-hidden`}>
                          <Mono className="text-[12px] whitespace-nowrap text-ink-dim">
                            {r.repo}
                          </Mono>
                        </div>
                        <div className={`${CELL} flex flex-col gap-1`}>
                          {why.length ? (
                            why.map(([label, tone, text]) => (
                              <span
                                key={label}
                                className="text-[12.5px] text-muted"
                              >
                                <Pill tone={tone}>{label}</Pill> {text}
                              </span>
                            ))
                          ) : (
                            <span className="text-[12.5px] text-muted">
                              <Pill tone="none">unauffällig</Pill>{" "}
                              {dur(r.duration.p50)}, {pct(r.failure_rate)} rot
                            </span>
                          )}
                        </div>
                        <div className={`${CELL} text-right text-ink-dim`}>
                          {num(r.wasted_seconds / 3600, 1)} h
                        </div>
                        <div className={`${CELL} text-right`}>
                          <button
                            type="button"
                            onClick={() => onFacts("runs", r.repo)}
                            className="no-print border-0 bg-transparent p-0 text-[13px] text-accent-strong hover:underline"
                          >
                            Läufe ansehen →
                          </button>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            ) : (
              <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
                Keine Läufe dieser Art mit Ergebnis.
              </p>
            )}
            <p className="mt-2.5 mb-0 text-xs text-muted">
              Fehleranfällig: Quote mindestens {num(h.flaky_factor, 1)}× die des
              Teams, höchstens halbwegs zwischen ihr und 100 %. Langsam: Median
              mindestens {num(h.slow_factor, 1)}× der des Teams. Beides ab{" "}
              {h.min_runs} Läufen.
            </p>
          </Card>

          <Card className="mt-3">
            <CardHead
              title="Hängende Läufe"
              sub={`Läufe über ${num(h.outlier_factor, 0)}× dem Median ihrer Art und über ${dur(h.outlier_min_s)}`}
            />
            {h.outliers.length ? (
              <>
                <p className="mt-2 mb-0 text-[13.5px] text-ink-dim">
                  {h.outlier_count === 1
                    ? "1 Lauf hing"
                    : `${h.outlier_count} Läufe hingen`}{" "}
                  im Zeitraum. Ihre Zeit zählt weder in Dauer noch CI-Zeit.
                </p>
                <Groups groups={h.outlier_groups} />
                <details className="mt-2">
                  <summary className="cursor-pointer text-[13px] text-muted">
                    {h.outlier_count > h.outliers.length
                      ? `Die längsten ${h.outliers.length} anzeigen`
                      : "Läufe anzeigen"}
                  </summary>
                  <OutlierTable rows={h.outliers} />
                </details>
              </>
            ) : (
              <p className="mt-3.5 mb-0 text-[13.5px] text-muted">
                Keine Ausreisser im Zeitraum.
              </p>
            )}
            <p className="mt-2.5 mb-0 text-xs text-muted">
              Ein Repo mit mindestens {h.outlier_repo_min_runs} eigenen Läufen
              mit Ergebnis und Dauer muss zudem {num(h.outlier_factor, 0)}×
              seinen eigenen Median überschreiten: ein langsames Repo ist kein
              Ausreisser. Abgebrochene Läufe erscheinen trotzdem.
            </p>
          </Card>
        </>
      )}
    </section>
  );
}
