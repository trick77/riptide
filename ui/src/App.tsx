import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
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
import { Deploys, Mix } from "./Deploys";
import { Facts } from "./Facts";
import { Flow } from "./Flow";
import { dateTime, fullDate, fullDay, stage } from "./format";
import { FrameworkMap } from "./FrameworkMap";
import { PAGE_SECTIONS, SectionNav, sectionOf, useActiveSection } from "./nav";
import { Overview } from "./Overview";
import {
  WINDOWS,
  parseState,
  stateQuery,
  type Tab,
  type ViewState,
} from "./state";
import { Eyebrow, LanguageSwitch, Mono } from "./ui";

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
  const { t } = useTranslation();
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
    // The section a link points at stays: a filter change keeps the hash.
    if (q !== window.location.search)
      window.history.replaceState(
        null,
        "",
        `${window.location.pathname}${q}${window.location.hash}`,
      );
  }, [state]);

  // The sticky header's height, for the anchors' scroll margin and the
  // section marking: it wraps on a narrow window.
  const head = useRef<HTMLElement>(null);
  const headHeight = useCallback(() => head.current?.offsetHeight ?? 0, []);
  useEffect(() => {
    const el = head.current;
    if (!el) return;
    const set = () =>
      document.documentElement.style.setProperty(
        "--head-h",
        `${el.offsetHeight}px`,
      );
    set();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(set);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

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
      }
    : null;
  const key = query
    ? `${query.team}|${query.weeks}|${query.product ?? ""}`
    : null;
  const search = useSettled(state.q.trim(), SEARCH_DELAY_MS);

  const metrics = useLoad(key, (signal) => fetchMetrics(query!, signal));
  // The facts ask once the metrics for the same view are in, for the
  // product the metrics resolved: the facts cannot tell every product.
  const current = metrics.loaded?.key === key ? metrics.loaded.data : null;
  const factsQuery: Query | null =
    query && current ? { ...query, product: current.product } : null;
  const factsKey = factsQuery
    ? `${factsQuery.team}|${factsQuery.weeks}|${factsQuery.product ?? ""}|${search}`
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
  // A link from a finding or a hotspot: the facts behind it, searched.
  const onFacts = useCallback((tab: Tab, q: string) => {
    setState((s) => ({ ...s, tab, q }));
    document.getElementById("daten")?.scrollIntoView?.({ behavior: "smooth" });
  }, []);
  const weeks = m?.period.weeks ?? [];
  const active = useActiveSection(PAGE_SECTIONS, m != null, headHeight);

  // A link to a section: the sections render once the numbers are in, so
  // the browser's own jump on load found nothing to land on. Once, then the
  // reader scrolls.
  const landed = useRef(false);
  useEffect(() => {
    if (!m || landed.current) return;
    landed.current = true;
    const id = sectionOf(window.location.hash);
    if (id)
      requestAnimationFrame(() =>
        document
          .getElementById(id)
          ?.scrollIntoView?.({ behavior: "instant" as ScrollBehavior }),
      );
  }, [m]);

  return (
    <div className="flex min-h-screen flex-col bg-bg text-ink">
      <header
        ref={head}
        className="sticky-head sticky top-0 z-30 flex min-h-14 flex-wrap items-center gap-x-[22px] gap-y-1.5 border-b border-border bg-panel px-6 py-2.5"
      >
        {/* On one baseline: centred boxes leave the 21px serif mark, its
            descender included, 3px below the 13.5px links' text. */}
        <div className="flex flex-wrap items-baseline gap-x-[22px] gap-y-1.5">
          <h1 className="m-0 font-serif text-[21px] leading-7 font-medium whitespace-nowrap text-wordmark">
            riptide
          </h1>
          <SectionNav active={active} />
        </div>
        <div className="ml-auto flex items-center gap-3 text-[13px] text-muted">
          {/* The view the numbers are for, always in sight; a click goes
              to where it is changed. */}
          {m && weeks.length > 0 && (
            <a
              href="#ansicht"
              data-testid="head-view"
              className="no-print rounded-ui-sm px-1.5 py-1 text-[12.5px] whitespace-nowrap text-muted no-underline hover:bg-active hover:text-ink"
            >
              {t("nav.view", {
                product: m.product ?? t("page.allProducts"),
                from: weeks[0].kw,
                to: weeks[weeks.length - 1].kw,
                weeks: weeks.length,
              })}
            </a>
          )}
          <label htmlFor="team" className="sr-only">
            {t("page.team")}
          </label>
          <select
            id="team"
            value={team ?? ""}
            onChange={(e) => set({ team: e.target.value, product: null })}
            disabled={!teams?.length}
            className="h-8 rounded-ui-sm border border-border bg-panel px-2 text-[13px] text-ink-dim"
          >
            {(teams ?? []).map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </select>
          <LanguageSwitch />
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
            {t("page.print")}
          </button>
        </div>
      </header>

      <main
        aria-busy={loading && m != null}
        className="mx-auto box-border w-full max-w-[1400px] px-4 pt-7 pb-16 sm:px-10"
      >
        <h2 className="m-0 font-serif text-[28px] leading-tight font-medium tracking-[-0.01em] text-ink">
          {t("page.title")}
        </h2>
        {m && (
          <>
            <p className="mt-1.5 mb-0 max-w-[75ch] text-[14.5px] text-muted">
              {/* Nothing compared (the backend's `any`): the head claims
                  no comparison. */}
              {t(m.comparison.any ? "page.scope" : "page.scopeAlone", {
                team: m.meta.team,
                product: m.product
                  ? t("page.oneProduct", { product: m.product })
                  : t("page.allProductsLower"),
                from: weeks[0].kw,
                to: weeks[weeks.length - 1].kw,
                start: fullDate(new Date(m.period.from)),
                end: fullDay(m.period.last_day),
                weeks: weeks.length,
                prevStart: fullDate(new Date(m.comparison.from)),
                prevEnd: fullDay(m.comparison.last_day),
              })}
            </p>
            <p className="mt-1 mb-0 text-[13px] text-muted">
              {t("page.running", {
                kw: m.running_week.week.kw,
                releases: t("page.runningReleases", {
                  count: m.running_week.releases,
                }),
                merged: t("page.runningMerged", {
                  count: m.running_week.merged,
                }),
                runs: t("page.runningRuns", { count: m.running_week.runs }),
              })}{" "}
              <button
                type="button"
                onClick={() => onFacts(state.tab, t("facts.running"))}
                className="no-print border-0 bg-transparent p-0 text-[13px] text-accent-strong hover:underline"
              >
                {t("page.runningView")}
              </button>
            </p>
            {/* Per source since when and until when it reports; the
                reviewer's start per repo on hover (it is set up repo by
                repo). Then the silences in which events were lost (the
                backend's `lost`): a silence without that evidence is a team
                that did not push, build or deploy for two days, no gap in
                the data, so not named. */}
            <p className="mt-1 mb-0 text-[12.5px] text-faint">
              {t("page.dataAsOf")}{" "}
              {SOURCES.map(([k, label], i) => {
                const since = m.meta.data_start[k];
                const at = m.meta.data_end[k];
                const text =
                  since && at
                    ? t("page.source", {
                        source: label,
                        since: fullDate(new Date(since)),
                        at: dateTime(at),
                      })
                    : t("page.sourceNone", { source: label });
                const starts =
                  k === "noergler" && m.meta.repo_starts.length
                    ? m.meta.repo_starts
                        .map((r) =>
                          r.noergler
                            ? t("page.repoStart", {
                                repo: r.repo,
                                date: fullDate(new Date(r.noergler)),
                              })
                            : t("page.repoNever", { repo: r.repo }),
                        )
                        .join("\n")
                    : undefined;
                return (
                  <span key={k}>
                    {i > 0 && " · "}
                    <span
                      title={starts}
                      className={
                        starts ? "underline decoration-dotted" : undefined
                      }
                    >
                      {text}
                    </span>
                  </span>
                );
              })}
            </p>
            {m.meta.lost.map((s) => (
              <p
                key={`${s.source}${s.from}`}
                className="mt-1 mb-0 text-[12.5px] text-ochre"
              >
                {/* Still silent: no end to name. */}
                {t(s.ongoing ? "page.lostOngoing" : "page.lost", {
                  source: SOURCES.find(([k]) => k === s.source)?.[1],
                  from: dateTime(s.from),
                  to: dateTime(s.to),
                })}
              </p>
            ))}
          </>
        )}

        <section
          id="ansicht"
          aria-label={t("page.filter")}
          className="no-print mt-5 flex flex-col gap-3 rounded-ui-lg border border-border bg-panel px-4 py-3.5"
        >
          {/* The product is the frame of the whole page: tabs, not one
              chip among the filters (user, 2026-11-16). */}
          <div
            role="group"
            aria-label={t("page.product")}
            className="-mx-4 -mt-3.5 flex flex-wrap gap-x-1 border-b border-border px-2"
          >
            {[
              {
                key: null as string | null,
                label: t("page.allProducts"),
                count: `${products.length}`,
              },
            ]
              .concat(
                products.map((p) => ({
                  key: p.key,
                  label: p.key,
                  count: t("page.repos", { count: p.repos.length }),
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
                    className={`-mb-px inline-flex items-baseline gap-1.5 border-0 border-b-2 bg-transparent px-3.5 pt-3 pb-2.5 text-[14px] ${
                      on
                        ? "border-accent text-ink"
                        : "border-transparent text-muted hover:text-ink"
                    }`}
                  >
                    <span>{c.label}</span>
                    <span className="font-mono text-[11px] text-faint">
                      {c.count}
                    </span>
                  </button>
                );
              })}
          </div>
          <div className="flex flex-wrap items-center gap-x-[18px] gap-y-2.5">
            <div
              role="group"
              aria-label={t("page.period")}
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
                  {t("page.weeks", { n: w })}
                </button>
              ))}
            </div>
          </div>
          {sel && (
            <div className="flex flex-wrap items-baseline gap-x-2.5 gap-y-1.5 border-t border-border-soft pt-2.5 text-[12.5px] text-muted">
              <Eyebrow>{t("page.reposHead")}</Eyebrow>
              {sel.repos.map((r) => (
                <Mono
                  key={r}
                  className="rounded-md border border-elevated-border px-[7px] text-ink-dim"
                >
                  {r}
                </Mono>
              ))}
              <Eyebrow className="ml-2">{t("page.appsHead")}</Eyebrow>
              {(sel.apps.length ? sel.apps : [t("page.none")]).map((a) => (
                <Mono
                  key={a}
                  className="rounded-md border border-elevated-border px-[7px] text-ink-dim"
                >
                  {a}
                </Mono>
              ))}
            </div>
          )}
        </section>

        {failure && (
          <div
            role="alert"
            className="mt-6 rounded-ui border border-danger/40 bg-danger/10 px-4 py-3 text-[13.5px] text-danger-ink"
          >
            {t("page.loadFailed", { msg: failure })}
          </div>
        )}
        {teams && teams.length === 0 && (
          <p className="mt-6 text-[13.5px] text-muted">{t("page.noTeam")}</p>
        )}
        {!m && !failure && teams?.length !== 0 && (
          <div
            aria-busy="true"
            aria-label={t("page.loading")}
            className="mt-9 grid gap-3"
          >
            <div className="skeleton h-24 rounded-ui" />
            <div className="skeleton h-72 rounded-ui-lg" />
          </div>
        )}
        {m && (
          <>
            <Overview
              m={m}
              query={query!}
              why={why}
              onWhy={onWhy}
              onFacts={onFacts}
            />
            <Flow m={m} why={why} onWhy={onWhy} onFacts={onFacts} />
            <Build m={m} onFacts={onFacts} />
            <Deploys m={m} />
            <Mix m={m} />
            <Costs m={m} />
            <Facts
              facts={shownFacts}
              failed={facts.failure != null && facts.failure.key === factsKey}
              q={search}
              input={state.q}
              onInput={(q) => set({ q })}
              tab={state.tab}
              onTab={(tab) => set({ tab })}
            />
            <FrameworkMap prod={stage(m.delivery.production_stage)} />
          </>
        )}
      </main>
    </div>
  );
}
