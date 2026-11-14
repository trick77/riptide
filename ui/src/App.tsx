import { useCallback, useEffect, useState } from "react";
import {
  ApiError,
  type Metrics,
  type Query,
  fetchFacts,
  fetchMetrics,
  fetchTeams,
} from "./api";
import { Build } from "./Build";
import { Costs } from "./Costs";
import { Delivery } from "./Delivery";
import { Facts } from "./Facts";
import { dateTime, fullDate } from "./format";
import { Review } from "./Review";
import { WINDOWS, parseState, stateQuery, type ViewState } from "./state";
import { Eyebrow, Mono } from "./ui";

const NAV: [string, string][] = [
  ["delivery", "Delivery"],
  ["review", "Code-Review"],
  ["build", "Build"],
  ["kosten", "Kosten"],
  ["daten", "Daten"],
];

const SOURCES: [keyof Metrics["meta"]["data_end"], string][] = [
  ["bitbucket", "Bitbucket"],
  ["pipeline", "CI"],
  ["argocd", "Argo CD"],
  ["noergler", "noergler"],
];

// A search keystroke waits this long for the next one before it asks.
const SEARCH_DELAY_MS = 200;

type Loaded<T> = { key: string; data: T };
type Failure = { key: string; msg: string };

function message(e: unknown): string {
  return e instanceof ApiError ? `${e.status}: ${e.message}` : String(e);
}

// The value once it has stopped changing for ms.
function useSettled<T>(value: T, ms: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setSettled(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return settled;
}

// load fetches whenever key changes; the last answer stays shown while the
// next one loads, a failure belongs to the key it happened for.
function useLoad<T>(
  key: string | null,
  load: (signal: AbortSignal) => Promise<T>,
): { loaded: Loaded<T> | null; failure: Failure | null } {
  const [loaded, setLoaded] = useState<Loaded<T> | null>(null);
  const [failure, setFailure] = useState<Failure | null>(null);
  useEffect(() => {
    if (key == null) return;
    const ctl = new AbortController();
    load(ctl.signal)
      .then((data) => {
        setLoaded({ key, data });
        // An earlier failure of the same load is over once it succeeds.
        setFailure((f) => (f?.key === key ? null : f));
      })
      .catch((e: unknown) => {
        if (!ctl.signal.aborted) setFailure({ key, msg: message(e) });
      });
    return () => ctl.abort();
    // key names everything load reads
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return { loaded, failure };
}

export default function App() {
  const [state, setState] = useState<ViewState>(() =>
    parseState(window.location.search),
  );
  const [teams, setTeams] = useState<string[] | null>(null);
  const [teamsFailure, setTeamsFailure] = useState<string | null>(null);
  const [why, setWhy] = useState<string | null>(null);

  const set = (patch: Partial<ViewState>) =>
    setState((s) => ({ ...s, ...patch }));

  useEffect(() => {
    const q = stateQuery(state);
    if (q !== window.location.search)
      window.history.replaceState(null, "", `${window.location.pathname}${q}`);
  }, [state]);

  useEffect(() => {
    const ctl = new AbortController();
    fetchTeams(ctl.signal)
      .then(setTeams)
      .catch((e: unknown) => {
        if (!ctl.signal.aborted)
          setTeamsFailure(e instanceof Error ? e.message : String(e));
      });
    return () => ctl.abort();
  }, []);

  // The team: the one asked for when the config has it, else the first.
  const team =
    teams &&
    (state.team && teams.includes(state.team)
      ? state.team
      : (teams[0] ?? null));
  const query: Query | null = team
    ? {
        team,
        weeks: state.weeks,
        product: state.product,
        bots: state.bots,
      }
    : null;
  const key = query
    ? `${query.team}|${query.weeks}|${query.product ?? ""}|${query.bots}`
    : null;
  const search = useSettled(state.q.trim(), SEARCH_DELAY_MS);

  const metrics = useLoad(key, (signal) => fetchMetrics(query!, signal));
  // The facts ask once the metrics for the same view are in, for the
  // product the metrics resolved: the facts cannot tell every product.
  const current = metrics.loaded?.key === key ? metrics.loaded.data : null;
  const factsQuery: Query | null =
    query && current ? { ...query, product: current.product } : null;
  const factsKey = factsQuery
    ? `${factsQuery.team}|${factsQuery.weeks}|${factsQuery.product ?? ""}|${factsQuery.bots}|${search}`
    : null;
  const facts = useLoad(factsKey, (signal) =>
    fetchFacts(factsQuery!, search, signal),
  );

  const failure =
    teamsFailure ??
    (metrics.failure?.key === key ? metrics.failure.msg : null) ??
    (facts.failure?.key === factsKey ? facts.failure.msg : null);
  // The shown metrics belong to the team on screen; a product or window
  // switch keeps them until the new ones arrive.
  const m =
    metrics.loaded && metrics.loaded.data.meta.team === team
      ? metrics.loaded.data
      : null;
  const loading = metrics.loaded?.key !== key;
  const products = m?.products ?? [];
  // The chip pressed at once; the numbers say which product they are for.
  const selected = products.find((p) => p.key === state.product) ?? null;
  const product = selected?.key ?? null;
  const sel = selected;
  const shownFacts =
    facts.loaded && team && facts.loaded.key.startsWith(`${team}|`)
      ? facts.loaded.data
      : null;
  const onWhy = useCallback(
    (k: string) => setWhy((w) => (w === k ? null : k)),
    [],
  );
  const weeks = m?.period.weeks ?? [];

  return (
    <div className="flex min-h-screen flex-col bg-bg text-ink">
      <header className="flex min-h-14 flex-wrap items-center gap-x-[22px] gap-y-1.5 border-b border-border bg-panel px-6 py-2.5">
        <h1 className="m-0 font-serif text-[21px] leading-7 font-medium whitespace-nowrap text-wordmark">
          riptide
        </h1>
        <nav aria-label="Bereiche" className="no-print flex flex-wrap gap-0.5">
          {NAV.map(([id, label]) => (
            <a
              key={id}
              href={`#${id}`}
              className="rounded-ui-sm px-2.5 py-1.5 text-[13.5px] text-muted no-underline hover:bg-active hover:text-ink"
            >
              {label}
            </a>
          ))}
        </nav>
        <div className="no-print max-w-[320px] min-w-[200px] flex-[1_1_240px]">
          <label htmlFor="search" className="sr-only">
            Suche nach Repo, App, PR oder Jira-Key
          </label>
          <input
            id="search"
            type="search"
            value={state.q}
            onChange={(e) => set({ q: e.target.value })}
            placeholder="Repo, App, PR, Jira-Key …"
            className="box-border h-8 w-full rounded-ui-sm border border-border bg-bg px-2.5 text-[13px] text-ink focus-visible:outline-2 focus-visible:outline-accent-strong"
          />
        </div>
        <div className="ml-auto flex items-center gap-3 text-[13px] text-muted">
          <label htmlFor="team" className="sr-only">
            Team
          </label>
          <select
            id="team"
            value={team ?? ""}
            onChange={(e) => set({ team: e.target.value, product: null })}
            disabled={!teams?.length}
            className="h-8 rounded-ui-sm border border-border bg-panel px-2 text-[13px] text-ink-dim"
          >
            {(teams ?? []).map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={() => window.print()}
            className="no-print inline-flex h-8 items-center gap-2 rounded-ui-sm border border-border bg-panel px-3 text-[13.5px] text-ink-dim hover:bg-active"
          >
            <svg
              aria-hidden="true"
              width="15"
              height="15"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M6 9V3h12v6" />
              <rect x="4" y="9" width="16" height="8" rx="2" />
              <path d="M7 14h10v7H7z" />
            </svg>
            Drucken / PDF
          </button>
        </div>
      </header>

      <main
        aria-busy={loading && m != null}
        className="mx-auto box-border w-full max-w-[1400px] px-4 pt-7 pb-16 sm:px-10"
      >
        <h2 className="m-0 font-serif text-[28px] leading-tight font-medium tracking-[-0.01em] text-ink">
          Engineering-Metriken
        </h2>
        {m && (
          <>
            <p className="mt-1.5 mb-0 max-w-[75ch] text-[14.5px] text-muted">
              {`Team ${m.meta.team}, ${m.product ? `Produkt ${m.product}` : "alle Produkte"} · KW ${weeks[0].kw}–${
                weeks[weeks.length - 1].kw
              } · ${fullDate(new Date(m.period.from))} bis ${fullDate(new Date(m.period.to))} · Rohdaten aus Bitbucket, CI, Argo CD und noergler`}
            </p>
            <p className="mt-1 mb-0 text-[12.5px] text-faint">
              Datenstand:{" "}
              {SOURCES.map(([k, label]) => {
                const t = m.meta.data_end[k];
                return `${label} ${t ? dateTime(t) : "keine Daten"}`;
              }).join(" · ")}
            </p>
          </>
        )}

        <section
          aria-label="Filter"
          className="no-print mt-5 flex flex-col gap-3 rounded-ui-lg border border-border bg-panel px-4 py-3.5"
        >
          <div className="flex flex-wrap items-center gap-x-[18px] gap-y-2.5">
            <div
              role="group"
              aria-label="Zeitraum"
              className="inline-flex rounded-ui-sm border border-border bg-bg p-0.5"
            >
              {WINDOWS.map((w) => (
                <button
                  key={w}
                  type="button"
                  aria-pressed={state.weeks === w}
                  onClick={() => set({ weeks: w })}
                  className={`rounded-md border-0 px-3 py-1 text-[13px] ${
                    state.weeks === w
                      ? "bg-active text-ink shadow-[inset_0_0_0_1px] shadow-elevated-border"
                      : "bg-transparent text-muted"
                  }`}
                >
                  {w} W
                </button>
              ))}
            </div>
            <div
              role="group"
              aria-label="Produkt"
              className="flex flex-wrap gap-1.5"
            >
              {[
                {
                  key: null as string | null,
                  label: "Alle Produkte",
                  count: `${products.length}`,
                },
              ]
                .concat(
                  products.map((p) => ({
                    key: p.key,
                    label: p.key,
                    count: `${p.repos.length} Repos`,
                  })),
                )
                .map((c) => {
                  const on = product === c.key;
                  return (
                    <button
                      key={c.key ?? "*"}
                      type="button"
                      aria-pressed={on}
                      onClick={() => set({ product: c.key })}
                      className={`inline-flex h-[30px] items-center gap-[7px] rounded-full border px-3 text-[13px] ${
                        on
                          ? "border-accent-fill bg-accent/15 text-ink"
                          : "border-border bg-bg text-ink-dim"
                      }`}
                    >
                      <span>{c.label}</span>
                      <span
                        className={`font-mono text-[11px] ${on ? "text-accent-strong" : "text-muted"}`}
                      >
                        {c.count}
                      </span>
                    </button>
                  );
                })}
            </div>
            <button
              type="button"
              aria-pressed={state.bots}
              onClick={() => set({ bots: !state.bots })}
              className={`inline-flex h-[30px] items-center gap-2 rounded-full border px-3 text-[13px] ${
                state.bots
                  ? "border-s3 bg-s3/15 text-ink"
                  : "border-border bg-bg text-muted"
              }`}
            >
              <span
                aria-hidden="true"
                className={`size-2 rounded-full ${state.bots ? "bg-s3" : "bg-transparent shadow-[inset_0_0_0_1.5px] shadow-faint"}`}
              />
              Bots einbeziehen
            </button>
          </div>
          {sel && (
            <div className="flex flex-wrap items-baseline gap-x-2.5 gap-y-1.5 border-t border-border-soft pt-2.5 text-[12.5px] text-muted">
              <Eyebrow>Repos</Eyebrow>
              {sel.repos.map((r) => (
                <Mono
                  key={r}
                  className="rounded-md border border-elevated-border px-[7px] text-ink-dim"
                >
                  {r}
                </Mono>
              ))}
              <Eyebrow className="ml-2">Argo-Apps</Eyebrow>
              {(sel.apps.length ? sel.apps : ["keine"]).map((a) => (
                <Mono
                  key={a}
                  className="rounded-md border border-elevated-border px-[7px] text-ink-dim"
                >
                  {a}
                </Mono>
              ))}
              <span className="basis-full text-faint">
                Zuordnung aus den Daten: Repos über den
                Bitbucket-Projektschlüssel, Apps über das GitOps-Repo, dessen
                Revision sie deployen. Keine Namensliste.
              </span>
            </div>
          )}
        </section>

        {failure && (
          <div
            role="alert"
            className="mt-6 rounded-ui border border-danger/40 bg-danger/10 px-4 py-3 text-[13.5px] text-danger-ink"
          >
            Daten konnten nicht geladen werden: {failure}
          </div>
        )}
        {teams && teams.length === 0 && (
          <p className="mt-6 text-[13.5px] text-muted">
            In der Konfiguration ist kein Team eingetragen.
          </p>
        )}
        {!m && !failure && teams?.length !== 0 && (
          <div aria-busy="true" aria-label="Lädt" className="mt-9 grid gap-3">
            <div className="skeleton h-24 rounded-ui" />
            <div className="skeleton h-72 rounded-ui-lg" />
          </div>
        )}
        {m && (
          <>
            <Delivery m={m} why={why} onWhy={onWhy} />
            <Review m={m} why={why} onWhy={onWhy} />
            <Build m={m} />
            <Costs m={m} />
            {shownFacts && (
              <Facts
                facts={shownFacts}
                q={search}
                tab={state.tab}
                onTab={(tab) => set({ tab })}
              />
            )}
          </>
        )}

        <footer className="mt-10 border-t border-border-soft pt-3.5 text-xs text-faint">
          riptide liest die Rohdaten nur. Jede Zahl lässt sich über «Woher?» bis
          zur Query zurückverfolgen.
        </footer>
      </main>
    </div>
  );
}
