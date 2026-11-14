import { memo, useCallback, useEffect, useMemo, useState } from "react";
import { ApiError, type Dataset, fetchDataset, fetchTeams } from "./api";
import { Build as BuildSection } from "./Build";
import { Costs as CostsSection } from "./Costs";
import { Delivery as DeliverySection } from "./Delivery";
import { Facts } from "./Facts";
import { dateTime, fullDate } from "./format";
import { chain, type Filter } from "./metrics";
import { WINDOWS, period, type Period } from "./period";
import { Review as ReviewSection } from "./Review";

// Every section but the facts recomputes its metrics on render; memoised,
// a search keystroke leaves them alone.
const Delivery = memo(DeliverySection);
const Review = memo(ReviewSection);
const Build = memo(BuildSection);
const Costs = memo(CostsSection);
import { parseState, stateQuery, type ViewState } from "./state";
import { Eyebrow, Mono } from "./ui";

const NAV: [string, string][] = [
  ["delivery", "Delivery"],
  ["review", "Code-Review"],
  ["build", "Build"],
  ["kosten", "Kosten"],
  ["daten", "Daten"],
];

const SOURCES: [keyof Dataset["meta"]["data_end"], string][] = [
  ["bitbucket", "Bitbucket"],
  ["pipeline", "CI"],
  ["argocd", "Argo CD"],
  ["noergler", "noergler"],
];

type Loaded = { key: string; ds: Dataset; period: Period };

export default function App({ now = () => new Date() }: { now?: () => Date }) {
  const [state, setState] = useState<ViewState>(() =>
    parseState(window.location.search),
  );
  const [teams, setTeams] = useState<string[] | null>(null);
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  // A failure belongs to what was being loaded: switching the team or the
  // window hides it without an effect resetting it.
  const [failure, setFailure] = useState<{ key: string; msg: string } | null>(
    null,
  );
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
          setFailure({
            key: "teams",
            msg: e instanceof Error ? e.message : String(e),
          });
      });
    return () => ctl.abort();
  }, []);

  // The team: the one asked for when the config has it, else the first.
  const team =
    teams &&
    (state.team && teams.includes(state.team)
      ? state.team
      : (teams[0] ?? null));
  const key = `${team}|${state.weeks}`;

  useEffect(() => {
    if (!team) return;
    const ctl = new AbortController();
    const p = period(state.weeks, now());
    const k = `${team}|${state.weeks}`;
    fetchDataset(team, p.from, p.to, ctl.signal)
      .then((ds) => {
        setLoaded({ key: k, ds, period: p });
        // An earlier failure of the same load is over once it succeeds.
        setFailure((f) => (f?.key === k ? null : f));
      })
      .catch((e: unknown) => {
        if (ctl.signal.aborted) return;
        setFailure({
          key: k,
          msg: e instanceof ApiError ? `${e.status}: ${e.message}` : String(e),
        });
      });
    return () => ctl.abort();
    // now is a clock, not state
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [team, state.weeks]);

  const current = loaded && loaded.key === key ? loaded : null;
  const error =
    failure && (failure.key === key || failure.key === "teams")
      ? failure.msg
      : null;
  const ds = current?.ds ?? null;
  const products = useMemo(() => ds?.products ?? [], [ds]);
  const product =
    state.product && products.some((p) => p.key === state.product)
      ? state.product
      : null;
  // Stable across renders, so the memoised sections skip a search keystroke:
  // only the facts table depends on the search.
  const filter: Filter = useMemo(
    () => ({ product, bots: state.bots }),
    [product, state.bots],
  );
  const onWhy = useCallback(
    (k: string) => setWhy((w) => (w === k ? null : k)),
    [],
  );
  const sel = products.find((p) => p.key === product);
  // Delivery and Review both show the lead-time breakdown: computed once.
  const ch = useMemo(
    () => (current ? chain(current.ds, current.period, filter) : null),
    [current, filter],
  );

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

      <main className="mx-auto box-border w-full max-w-[1400px] px-4 pt-7 pb-16 sm:px-10">
        <h2 className="m-0 font-serif text-[28px] leading-tight font-medium tracking-[-0.01em] text-ink">
          Engineering-Metriken
        </h2>
        {current && (
          <>
            <p className="mt-1.5 mb-0 max-w-[75ch] text-[14.5px] text-muted">
              {`Team ${current.ds.meta.team}, ${product ? `Produkt ${product}` : "alle Produkte"} · KW ${current.period.weeks[0].kw}–${
                current.period.weeks[current.period.weeks.length - 1].kw
              } · ${fullDate(current.period.from)} bis ${fullDate(current.period.to)} · Rohdaten aus Bitbucket, CI, Argo CD und noergler`}
            </p>
            <p className="mt-1 mb-0 text-[12.5px] text-faint">
              Datenstand:{" "}
              {SOURCES.map(([k, label]) => {
                const t = current.ds.meta.data_end[k];
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

        {error && (
          <div
            role="alert"
            className="mt-6 rounded-ui border border-danger/40 bg-danger/10 px-4 py-3 text-[13.5px] text-danger-ink"
          >
            Daten konnten nicht geladen werden: {error}
          </div>
        )}
        {teams && teams.length === 0 && (
          <p className="mt-6 text-[13.5px] text-muted">
            In der Konfiguration ist kein Team eingetragen.
          </p>
        )}
        {!current && !error && teams?.length !== 0 && (
          <div aria-busy="true" aria-label="Lädt" className="mt-9 grid gap-3">
            <div className="skeleton h-24 rounded-ui" />
            <div className="skeleton h-72 rounded-ui-lg" />
          </div>
        )}
        {current && (
          <>
            <Delivery
              ds={current.ds}
              period={current.period}
              filter={filter}
              ch={ch!}
              why={why}
              onWhy={onWhy}
            />
            <Review
              ds={current.ds}
              period={current.period}
              filter={filter}
              ch={ch!}
              why={why}
              onWhy={onWhy}
            />
            <Build ds={current.ds} period={current.period} filter={filter} />
            <Costs ds={current.ds} filter={filter} />
            <Facts
              ds={current.ds}
              filter={filter}
              q={state.q}
              tab={state.tab}
              onTab={(tab) => set({ tab })}
            />
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
