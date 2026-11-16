// The overview's "what now": the assessment of the closed weeks, as the
// backend computed and judged it (/api/analysis), worded by an LLM where a
// model is set, by the rules' own texts where not. Every number, verdict,
// focus repo and column comes from the backend; the browser formats them
// and scales the bars.

import type { TFunction } from "i18next";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  type AnalysisStatus,
  type AnalysisText,
  askAnalysis,
  type Assessment as AssessmentData,
  type Fact,
  fetchAnalysis,
  type FocusRepo,
  type LayoutCard,
  type Metrics,
  type PRWaiting,
  type Query,
  type Signal,
  type WaitingCounts,
} from "./api";
import { stageNames } from "./Flow";
import {
  ciHours,
  clock,
  dayMonth,
  dur,
  money,
  num,
  pct,
  stage,
} from "./format";
import type { Tab } from "./state";

const POLL_MS = 3000;
// Small meta text: plain spaces around the middot are fine there.
const SEP = ` ${String.fromCharCode(0x00b7)} `;

// The column, lever and facts tab of an item are the backend's (layout):
// the page only shows them.
type Column = LayoutCard["column"];

const WAIT_ORDER: PRWaiting[] = [
  "first_review",
  "review",
  "changes",
  "merge",
  "draft",
];
const WAIT_COLOR: Record<PRWaiting, string> = {
  first_review: "bg-s4",
  review: "bg-s3",
  changes: "bg-s1",
  merge: "bg-s2",
  draft: "bg-other",
};
const CI_COLOR = ["bg-s4", "bg-s1", "bg-s2"];

const VERDICT_TEXT: Record<Fact["verdict"], string> = {
  better: "text-online",
  worse: "text-danger-ink",
  same: "text-muted",
  none: "text-muted",
};

const COLUMN_STYLE: Record<Column, { border: string; dot: string }> = {
  good: { border: "border-l-online", dot: "bg-online" },
  bad: { border: "border-l-danger-ink", dot: "bg-danger-ink" },
  done: { border: "border-l-s1", dot: "bg-s1" },
};

export function fmtFact(
  t: TFunction,
  unit: Fact["unit"],
  v: number | null,
): string {
  if (v == null) return "–";
  switch (unit) {
    case "seconds":
      return dur(v);
    case "share":
      return pct(v);
    case "per_week":
      return num(v, 1);
    case "weeks":
      return t("overview.assess.weeksUnit", { n: num(v, 1) });
    case "usd":
      return money(v);
    case "days":
      return t("units.days", { n: num(v) });
    default:
      return num(v);
  }
}

// Every fact of the assessment by id, for the placeholders.
function factIndex(a: AssessmentData): Map<string, Fact> {
  const out = new Map<string, Fact>();
  const add = (fs: Fact[] | null | undefined) =>
    fs?.forEach((f) => out.set(f.id, f));
  add(a.team);
  a.signals.forEach((s) => add(s.facts));
  a.focus.forEach((f) => add(f.facts));
  add(a.question?.facts);
  return out;
}

// fill replaces {{fact}} placeholders with the formatted value; an unknown
// one is left out rather than shown raw.
export function fill(
  t: TFunction,
  text: string,
  facts: Map<string, Fact>,
): string {
  return text
    .replace(/\{\{([^{}]+)\}\}/g, (_, id: string) => {
      const f = facts.get(id.trim());
      return f ? fmtFact(t, f.unit, f.value) : "";
    })
    .replace(/ {2,}/g, " ");
}

function weekday(iso: string, lang: string): string {
  return new Date(iso).toLocaleDateString(lang === "en" ? "en-GB" : "de-CH", {
    weekday: "short",
  });
}

function stamp(iso: string, lang: string): string {
  return `${weekday(iso, lang)} ${dayMonth(new Date(iso))}, ${clock(iso)}`;
}

function weeksLabel(t: TFunction, a: AssessmentData): string {
  const w = a.period.weeks;
  if (!w.length) return "";
  return t("overview.assess.weeks", { from: w[0].kw, to: w[w.length - 1].kw });
}

// The rule title of a signal, its words filled from the signal itself.
function signalTitle(t: TFunction, s: Signal, m: Metrics): string {
  const prod = stage(m.delivery.production_stage);
  const label = s.facts[0]
    ? t(`overview.assess.label.${s.facts[0].label}`, {
        defaultValue: s.facts[0].label,
      })
    : "";
  const vars = {
    repo: s.repo ?? "",
    kind: s.run_kind
      ? t(`build.kind.${s.run_kind}`, { defaultValue: s.run_kind })
      : "",
    source: t(`overview.assess.source.${s.source}`, {
      defaultValue: s.source,
    }),
    label,
    stage: s.stage ? stageNames(t, m.lead_chain, prod)[s.stage][0] : "",
    what: "",
  };
  if (s.kind === "change")
    return t(
      s.tone === "good"
        ? "overview.assess.title.changeGood"
        : "overview.assess.title.changeBad",
      vars,
    );
  if (s.kind === "resolved") {
    const was = (s.metric ?? "").split(":")[0];
    const kindWord = t(`overview.assess.kind.${was}`, { defaultValue: was });
    vars.what = s.repo ? `${s.repo} (${kindWord})` : label || kindWord;
  }
  return t(`overview.assess.title.${s.kind}`, vars);
}

function ruleText(t: TFunction, s: Signal): string {
  if (s.kind === "change")
    return t(
      s.tone === "good"
        ? "overview.assess.rule.changeGood"
        : "overview.assess.rule.changeBad",
    );
  return t(`overview.assess.rule.${s.kind}`, { defaultValue: "" });
}

function Prev({ f }: { f: Fact }) {
  const { t } = useTranslation();
  if (f.prev == null) return null;
  return (
    <span className={`font-mono text-[11.5px] ${VERDICT_TEXT[f.verdict]}`}>
      {t("overview.assess.prev", { v: fmtFact(t, f.unit, f.prev) })}
    </span>
  );
}

function Seen({ n }: { n: number }) {
  const { t } = useTranslation();
  if (n <= 0) return null;
  return (
    <span className="ml-1.5 inline-block rounded-full border border-ochre/40 px-1.5 align-[2px] font-mono text-[10.5px] text-ochre">
      {t("overview.assess.seen", { count: n })}
    </span>
  );
}

function WaitingBar({ w }: { w: WaitingCounts }) {
  const { t } = useTranslation();
  const parts = WAIT_ORDER.filter((k) => w[k] > 0);
  const total = parts.reduce((s, k) => s + w[k], 0);
  if (!total) return null;
  return (
    <div className="my-1.5">
      <div className="flex h-2 overflow-hidden rounded bg-track">
        {parts.map((k) => (
          <span
            key={k}
            className={WAIT_COLOR[k]}
            style={{ width: `${(w[k] / total) * 100}%` }}
          />
        ))}
      </div>
      <div className="mt-1 flex flex-wrap gap-x-2.5 font-mono text-[11px] text-muted">
        {parts.map((k) => (
          <span key={k}>
            <i
              className={`mr-1 inline-block size-2 rounded-sm ${WAIT_COLOR[k]}`}
            />
            {t(`overview.assess.waiting.${k}`)} {w[k]}
          </span>
        ))}
      </div>
    </div>
  );
}

// The first number now against before, two bars scaled to the larger.
function CompareBars({ f }: { f: Fact }) {
  const { t } = useTranslation();
  if (f.value == null || f.prev == null) return null;
  const max = Math.max(f.value, f.prev);
  if (max <= 0) return null;
  const rows: [string, number, string][] = [
    [t("compare.now"), f.value, "bg-danger-ink/80"],
    [t("compare.previous"), f.prev, "bg-axis"],
  ];
  return (
    <div className="my-1.5 grid grid-cols-[auto_1fr_auto] items-center gap-x-2 gap-y-0.5 font-mono text-[11px] text-muted">
      {rows.map(([label, v, color]) => (
        <div key={label} className="contents">
          <span>{label}</span>
          <div>
            <div
              className={`h-[7px] rounded-sm ${color}`}
              style={{ width: `${(v / max) * 100}%` }}
            />
          </div>
          <span>{fmtFact(t, f.unit, v)}</span>
        </div>
      ))}
    </div>
  );
}

function Lever({ k }: { k: string }) {
  const { t } = useTranslation();
  if (!k) return null;
  return (
    <div className="text-[13.5px] text-ink">
      <span className="text-accent-strong">→</span>{" "}
      {t(`overview.assess.lever.${k}`, { defaultValue: k })}
    </div>
  );
}

function Retro({ q }: { q: string | null }) {
  const { t } = useTranslation();
  if (!q) return null;
  return (
    <div className="my-1.5 flex items-baseline gap-2 font-serif text-[15px] text-ink">
      <span className="rounded-full border border-s3/45 px-1.5 font-mono text-[10.5px] tracking-[.06em] whitespace-nowrap text-s3 uppercase">
        {t("overview.assess.retro")}
      </span>
      <span>{q}</span>
    </div>
  );
}

function Why({ text }: { text: string }) {
  const { t } = useTranslation();
  if (!text) return null;
  return (
    <details className="mt-1.5 text-[12.5px] text-faint">
      <summary className="cursor-pointer text-muted">
        {t("overview.assess.why")}
      </summary>
      <p className="mt-1 mb-0">{text}</p>
    </details>
  );
}

type OnFacts = (tab: Tab, q: string) => void;

type FocusView = {
  r: FocusRepo;
  // The facts tab its first signal points at: runs for a build, PRs else.
  tab: Tab;
  rank: string;
  seen: number;
  take: string;
  retro: string | null;
  lever: string;
  why: string;
};

function FocusBlock({
  f,
  i,
  onFacts,
}: {
  f: FocusView;
  i: number;
  onFacts: OnFacts;
}) {
  const { t } = useTranslation();
  const first = f.r.facts[0];
  return (
    <article className="min-w-0 rounded-ui-lg border border-border bg-[linear-gradient(180deg,rgba(236,126,126,.06),transparent_40%)] px-4 pt-4 pb-3">
      <div className="mb-3 flex flex-wrap items-baseline gap-2.5">
        <span className="font-serif text-[26px] leading-none text-danger-ink">
          {i + 1}
        </span>
        <h4 className="m-0 font-mono text-[18px] font-semibold break-all text-ink">
          {f.r.repo}
        </h4>
        <span className="rounded-full border border-danger-ink/40 px-2 font-mono text-[11px] tracking-[.06em] text-danger-ink uppercase">
          {f.rank}
        </span>
        <Seen n={f.seen} />
      </div>
      <div className="mb-2 grid grid-cols-[repeat(auto-fit,minmax(110px,1fr))] gap-2.5">
        {f.r.facts.slice(0, 3).map((x) => (
          <div key={x.id} className="flex min-w-0 flex-col">
            <span className="font-serif text-[26px] leading-tight text-ink tabular-nums">
              {fmtFact(t, x.unit, x.value)}
            </span>
            <span className="text-xs leading-snug text-muted">
              {t(`overview.assess.label.${x.label}`, { defaultValue: x.label })}
            </span>
            <Prev f={x} />
          </div>
        ))}
      </div>
      {f.r.waiting ? (
        <WaitingBar w={f.r.waiting} />
      ) : (
        first && <CompareBars f={first} />
      )}
      <p className="my-2 text-sm text-ink-dim">{f.take}</p>
      <Retro q={f.retro} />
      <Lever k={f.lever} />
      <Why text={f.why} />
      <button
        type="button"
        onClick={() => onFacts(f.tab, f.r.repo)}
        className="no-print mt-1.5 border-0 bg-transparent p-0 text-left text-[13px] text-accent-strong hover:underline"
      >
        {t("overview.assess.factsOf", { repo: f.r.repo })}
      </button>
    </article>
  );
}

type CardView = {
  s: Signal;
  column: Column;
  title: string;
  take: string;
  retro: string | null;
  lever: string;
  why: string;
};

function Card({ c }: { c: CardView }) {
  const { t } = useTranslation();
  const f = c.s.facts[0];
  const d = c.s.drivers?.[0];
  return (
    <article
      className={`mb-3 rounded-ui border border-l-[3px] border-border bg-panel px-3.5 pt-3.5 pb-3 ${COLUMN_STYLE[c.column].border}`}
    >
      <p className="m-0 mb-2.5 text-[15px] leading-snug font-semibold text-ink">
        {c.title}
        <Seen n={c.s.seen} />
        {c.s.low_sample && (
          <span className="ml-1.5 inline-block rounded-full border border-border px-1.5 align-[2px] font-mono text-[10.5px] text-muted">
            {t("common.lowSample")}
          </span>
        )}
      </p>
      {f && (
        <div className="mb-2 flex flex-wrap items-baseline gap-2">
          <span className="font-serif text-[32px] leading-none text-ink tabular-nums">
            {fmtFact(t, f.unit, f.value)}
          </span>
          <span className="text-[13px] text-muted">
            {t(`overview.assess.label.${f.label}`, { defaultValue: f.label })}
          </span>
          <Prev f={f} />
        </div>
      )}
      {c.s.waiting && <WaitingBar w={c.s.waiting} />}
      {d && (
        <div className="mt-1.5 font-mono text-xs leading-relaxed text-ochre">
          {t("overview.assess.driver", { repo: d.repo, share: pct(d.share) })}
        </div>
      )}
      <p className="my-2 text-sm text-ink-dim">{c.take}</p>
      <Retro q={c.retro} />
      <Lever k={c.lever} />
      <Why text={c.why} />
    </article>
  );
}

function CIBlock({
  a,
  sentence,
}: {
  a: AssessmentData;
  sentence: string | null;
}) {
  const { t } = useTranslation();
  const ci = a.ci;
  if (ci.seconds <= 0) return null;
  const rows = ci.repos.map((r, i) => ({ ...r, color: CI_COLOR[i] }));
  if (ci.other_repos > 0)
    rows.push({
      repo: t("overview.assess.ciOther", { count: ci.other_repos }),
      seconds: ci.other_seconds,
      wasted: ci.other_wasted,
      color: "bg-other",
    });
  const top = ci.repos[0];
  const rule = top
    ? t("overview.assess.ciRule", {
        repo: top.repo,
        time: ciHours(top.seconds),
        total: ciHours(ci.seconds),
        wasted: ciHours(top.wasted),
      })
    : "";
  return (
    <div className="mt-6">
      <div className="mb-2 flex flex-wrap items-baseline justify-between gap-3">
        <span className="font-mono text-xs tracking-[.08em] text-muted uppercase">
          {t("overview.assess.ci")}
        </span>
        <span className="text-[12.5px] text-faint">
          {t("overview.assess.ciMeta", {
            total: ciHours(ci.seconds),
            weeks: a.period.weeks.length,
            wasted: ciHours(ci.wasted),
          })}
        </span>
      </div>
      <div className="flex h-3.5 gap-0.5 overflow-hidden rounded">
        {rows.map((r) => (
          <span
            key={r.repo}
            title={`${r.repo}: ${ciHours(r.seconds)}`}
            className={`relative block h-full ${r.color}`}
            style={{ flex: r.seconds }}
          >
            {r.wasted > 0 && (
              <i
                className="texture-wasted absolute inset-y-0 right-0"
                style={{ width: `${(r.wasted / r.seconds) * 100}%` }}
              />
            )}
          </span>
        ))}
      </div>
      <div className="mt-1.5 flex flex-wrap gap-x-2.5 font-mono text-[11px] text-muted">
        {rows.map((r) => (
          <span key={r.repo}>
            <i className={`mr-1 inline-block size-2 rounded-sm ${r.color}`} />
            {r.repo} {ciHours(r.seconds)}
          </span>
        ))}
      </div>
      <p className="mt-2 mb-0 text-sm text-ink-dim">{sentence || rule}</p>
    </div>
  );
}

// The view's words: the LLM's where an analysis is shown, the rules'
// otherwise. Both from the same assessment.
type Words = {
  headline: string | null;
  trend: string | null;
  focus: FocusView[];
  cards: CardView[];
  ci: string | null;
  question: string | null;
};

// words puts the view's words on the backend's layout: which repos and
// signals show where is decided there, never here.
function words(
  t: TFunction,
  a: AssessmentData,
  layout: AnalysisStatus["layout"],
  m: Metrics,
  text: AnalysisText | null,
): Words {
  const facts = factIndex(a);
  const byId = new Map(a.signals.map((s) => [s.id, s]));
  const repos = new Map(a.focus.map((r) => [r.repo, r]));
  const rank = (r: FocusRepo) => {
    const s = byId.get(r.signals[0]);
    return s ? t(`overview.assess.kind.${s.kind}`) : "";
  };
  const whyOf = (r: FocusRepo) =>
    r.signals
      .map((id) => byId.get(id))
      .filter((s): s is Signal => s != null)
      .map((s) => ruleText(t, s))
      .filter(Boolean)
      .join(" ");
  const question = a.question
    ? `${fmtFact(t, a.question.facts[0]?.unit ?? "count", a.question.facts[0]?.value ?? null)} ${t(
        `overview.assess.label.${a.question.facts[0]?.label ?? ""}`,
        { defaultValue: "" },
      )}: ${t(`overview.assess.questionFixed.${a.question.metric ?? ""}`, {
        defaultValue: "",
      })}`
    : null;

  const focusTexts = new Map(text?.focus.map((f) => [f.repo, f]) ?? []);
  const cardTexts = new Map(text?.cards.map((c) => [c.signal, c]) ?? []);
  const focus: FocusView[] = layout.focus.flatMap((lf) => {
    const r = repos.get(lf.repo);
    if (!r) return [];
    const ft = focusTexts.get(lf.repo);
    const sigs = r.signals
      .map((id) => byId.get(id))
      .filter((s): s is Signal => s != null);
    return [
      {
        r,
        tab: lf.tab,
        rank: rank(r),
        seen: r.seen,
        take: ft
          ? fill(t, ft.take, facts)
          : sigs.map((s) => signalTitle(t, s, m)).join("; "),
        retro: ft?.retro ? fill(t, ft.retro, facts) : null,
        lever: lf.lever,
        why: whyOf(r),
      },
    ];
  });
  const cards: CardView[] = layout.cards.flatMap((lc) => {
    const s = byId.get(lc.signal);
    if (!s) return [];
    const ct = cardTexts.get(lc.signal);
    return [
      {
        s,
        column: lc.column,
        title: signalTitle(t, s, m),
        take: ct ? fill(t, ct.take, facts) : ruleText(t, s),
        retro: ct?.retro ? fill(t, ct.retro, facts) : null,
        lever: lc.lever,
        why: ct ? ruleText(t, s) : "",
      },
    ];
  });
  return {
    headline: text ? fill(t, text.headline, facts) : null,
    trend: text?.trend ? fill(t, text.trend, facts) : null,
    focus,
    cards,
    ci: text?.ci ? fill(t, text.ci, facts) : null,
    question: text?.question ? fill(t, text.question, facts) : question,
  };
}

function Panel({
  st,
  m,
  onAsk,
  onFacts,
  askFailure,
}: {
  st: AnalysisStatus;
  m: Metrics;
  onAsk: () => void;
  onFacts: OnFacts;
  askFailure: string | null;
}) {
  const { t, i18n } = useTranslation();
  const [copied, setCopied] = useState(false);
  const a = st.assessment;
  const shown = st.analysis;
  const lang = i18n.language === "en" ? "en" : "de";
  const text = shown ? shown.text[lang] : null;
  const w = words(t, a, st.layout, m, text);
  const weeks = weeksLabel(t, a);
  // The backend sends cooldown_until only while the view cools down.
  const cooling = st.cooldown_until != null;

  const gates = a.signals.filter((s) => s.tone === "gate");
  const byId = new Map(a.signals.map((s) => [s.id, s]));
  const also = st.layout.also.flatMap((lc) => {
    const s = byId.get(lc.signal);
    return s ? [{ s, column: lc.column }] : [];
  });
  const retros = [
    ...w.focus.map((f) => [f.retro, f.r.repo] as const),
    ...w.cards.map((c) => [c.retro, c.title] as const),
  ].filter((x): x is readonly [string, string] => x[0] != null);

  const copy = () => {
    const lines = [
      `## Retro-Input riptide${SEP}${weeks}`,
      "",
      ...retros.map(([q, ctx]) => `- ${q} (${ctx})`),
      ...(w.question ? [`- ${w.question}`] : []),
    ];
    try {
      void navigator.clipboard?.writeText(lines.join("\n"));
      setCopied(true);
    } catch {
      // no clipboard (an insecure origin): nothing to copy to
    }
  };

  const columns: Column[] = ["good", "bad", "done"];
  const button =
    st.enabled &&
    (st.running ? (
      <button
        type="button"
        disabled
        className="no-print inline-flex items-center gap-2 rounded-ui-sm border border-border bg-active px-3 py-1 text-[13px] text-ink-dim opacity-70"
      >
        <span className="spinner" aria-hidden="true" />
        {t("overview.assess.running")}
      </button>
    ) : (
      <button
        type="button"
        onClick={onAsk}
        disabled={cooling}
        title={
          cooling
            ? t("overview.assess.cooldown", { time: clock(st.cooldown_until!) })
            : undefined
        }
        className={`no-print rounded-ui-sm border px-3 py-1 text-[13px] disabled:opacity-60 ${
          shown
            ? "border-border bg-active text-ink-dim hover:border-accent"
            : "border-accent bg-accent-dim text-ink"
        }`}
      >
        {shown ? t("overview.assess.ask") : t("overview.assess.askFirst")}
      </button>
    ));

  return (
    <div
      data-testid="assessment"
      className="relative overflow-hidden rounded-ui-lg border border-border bg-panel px-6 pt-5 pb-4 before:absolute before:inset-x-0 before:top-0 before:h-0.5 before:bg-[linear-gradient(90deg,var(--color-accent-strong),transparent_70%)]"
    >
      <div className="flex flex-wrap items-center gap-2.5">
        <span className="flex-1 font-mono text-xs tracking-[.08em] text-muted uppercase">
          {weeks}
        </span>
        <span className="rounded-full border border-border px-2.5 py-px font-mono text-[11px] text-muted">
          {shown
            ? t("overview.assess.badgeLLM")
            : t("overview.assess.badgeRules")}
        </span>
        {button}
      </div>
      {(shown || st.next_run) && (
        <p className="mt-1.5 mb-0 text-[12.5px] text-muted">
          {shown && (
            <>
              {t("overview.assess.madeAt", {
                date: stamp(shown.finished_at, lang),
              })}
              {SEP}
              {t(`overview.assess.trigger.${shown.trigger}`)}
              {shown.weeks_old > 0 && (
                <>
                  {SEP}
                  <span className="text-ochre">
                    {t("overview.assess.older", { count: shown.weeks_old })}
                  </span>
                </>
              )}
            </>
          )}
          {shown && st.next_run && SEP}
          {st.next_run &&
            t("overview.assess.next", { date: stamp(st.next_run, lang) })}
        </p>
      )}
      {st.running && (
        <p className="mt-3 mb-0 flex items-center gap-2 rounded-ui-sm border border-border bg-active px-3 py-2 text-[13.5px] text-ink-dim">
          <span className="spinner" aria-hidden="true" />
          {t("overview.assess.runningNote", { weeks })}
        </p>
      )}
      {(st.failure || askFailure) && (
        <p className="mt-3 mb-0 rounded-ui-sm border border-danger/50 bg-danger/10 px-3 py-2 text-[13.5px] text-ink-dim">
          {askFailure
            ? t("overview.assess.askFailed", { detail: askFailure })
            : t("overview.assess.failed", { detail: st.failure!.detail })}
        </p>
      )}

      {w.headline && (
        <h3 className="mt-3.5 mb-1.5 max-w-[36ch] font-serif text-[clamp(22px,2.4vw,30px)] leading-[1.2] font-normal tracking-[-.01em] text-ink">
          {w.headline}
        </h3>
      )}
      {gates.map((g) => (
        <p
          key={g.id}
          className="mt-3 mb-1 rounded-ui-sm border border-ochre/35 bg-ochre-wash px-2.5 py-1.5 text-[13px] text-ochre"
        >
          {t("overview.assess.gap", {
            source: t(`overview.assess.source.${g.source}`, {
              defaultValue: g.source,
            }),
            days: num(g.facts[0]?.value ?? 0),
          })}
        </p>
      ))}
      {w.trend && (
        <p className="mt-2.5 mb-0 max-w-[70ch] text-[15.5px] text-ink-dim">
          {w.trend}
        </p>
      )}

      {w.focus.length > 0 && (
        <div className="mt-3.5">
          <div className="mb-2.5 flex flex-wrap items-baseline justify-between gap-3">
            <span className="font-mono text-xs tracking-[.08em] text-muted uppercase">
              {t("overview.assess.focus", {
                shown: w.focus.length,
                active: a.active,
              })}
            </span>
            <span className="text-[12.5px] text-faint">
              {t(
                shown ? "overview.assess.calmLLM" : "overview.assess.calmRules",
                { calm: a.calm },
              )}
            </span>
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            {w.focus.map((f, i) => (
              <FocusBlock key={f.r.repo} f={f} i={i} onFacts={onFacts} />
            ))}
          </div>
        </div>
      )}

      <div className="mt-5 font-mono text-xs tracking-[.08em] text-muted uppercase">
        {t("overview.assess.teamWide")}
      </div>
      <div className="mt-2.5 grid gap-4 md:grid-cols-3">
        {columns.map((col) => {
          const cs = w.cards.filter((c) => c.column === col);
          return (
            <div key={col} className="min-w-0">
              <h4 className="m-0 mb-2.5 flex items-center gap-2 font-mono text-xs font-normal tracking-[.08em] text-muted uppercase">
                <i
                  className={`inline-block size-2 rounded-full ${COLUMN_STYLE[col].dot}`}
                />
                {t(`overview.assess.col.${col}`)}
              </h4>
              {cs.length ? (
                cs.map((c) => <Card key={c.s.id} c={c} />)
              ) : (
                <div className="rounded-ui border border-l-[3px] border-border border-l-axis bg-panel px-3.5 py-3 text-[13.5px] text-muted">
                  {t(`overview.assess.empty.${col}`)}
                </div>
              )}
            </div>
          );
        })}
      </div>

      <CIBlock a={a} sentence={w.ci} />

      <div className="mt-4 flex flex-col gap-2.5 border-t border-border-soft pt-3">
        {(retros.length > 0 || w.question) && (
          <div className="rounded-ui border border-s3/35 bg-s3/5 px-3.5 py-3">
            <div className="mb-2 flex flex-wrap items-center gap-2.5">
              <b className="flex-1 font-semibold text-ink">
                {t("overview.assess.retroBox")}
              </b>
              <button
                type="button"
                onClick={copy}
                className="no-print rounded-ui-sm border border-border bg-active px-3 py-1 text-[13px] text-ink-dim hover:border-accent"
              >
                {t("overview.assess.copy")}
              </button>
              {copied && (
                <span className="font-mono text-xs text-online">
                  {t("overview.assess.copied")}
                </span>
              )}
            </div>
            <ol className="m-0 grid gap-1 pl-5 font-serif text-base text-ink-dim">
              {retros.map(([q, ctx]) => (
                <li key={q}>
                  {q}
                  <small className="ml-1.5 font-mono text-[11px] text-faint">
                    {ctx}
                  </small>
                </li>
              ))}
              {w.question && <li>{w.question}</li>}
            </ol>
          </div>
        )}
        {also.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5 text-[13px] text-muted">
            {t("overview.assess.also")}
            {also.map(({ s, column }) => (
              <span
                key={s.id}
                className="rounded-full border border-border bg-panel px-2.5 py-0.5 text-[12.5px] text-ink-dim"
              >
                <i
                  className={`mr-1.5 inline-block size-1.5 rounded-full align-[1px] ${COLUMN_STYLE[column].dot}`}
                />
                {signalTitle(t, s, m)}
              </span>
            ))}
          </div>
        )}
        <div className="text-xs text-faint">
          {shown
            ? t("overview.assess.meta", {
                weeks,
                model: shown.model,
                cost: shown.cost_usd
                  ? `${shown.cost_usd} USD`
                  : t("overview.assess.metaUnpriced"),
              })
            : t("overview.assess.metaRules", { weeks })}
        </div>
      </div>
    </div>
  );
}

// Assessment loads the view's assessment, polls while a run is under way,
// and starts one on request.
export function Assessment({
  query,
  m,
  onFacts,
}: {
  query: Query;
  m: Metrics;
  onFacts: OnFacts;
}) {
  const { team, weeks, product, bots } = query;
  const key = `${team}|${weeks}|${product ?? ""}|${bots}`;
  const [st, setSt] = useState<{ key: string; data: AnalysisStatus } | null>(
    null,
  );
  const [failure, setFailure] = useState<{ key: string; msg: string } | null>(
    null,
  );
  const [askFailure, setAskFailure] = useState<string | null>(null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    const ctl = new AbortController();
    fetchAnalysis({ team, weeks, product, bots }, ctl.signal)
      .then((data) =>
        setSt({ key: `${team}|${weeks}|${product ?? ""}|${bots}`, data }),
      )
      .catch((e: unknown) => {
        if (!ctl.signal.aborted)
          setFailure({
            key: `${team}|${weeks}|${product ?? ""}|${bots}`,
            msg: e instanceof Error ? e.message : String(e),
          });
      });
    return () => ctl.abort();
  }, [team, weeks, product, bots, tick]);

  const current = st?.key === key ? st.data : null;
  const running = current?.running ?? false;
  // Every answer, or a failed poll (a 502 mid-run), schedules the next
  // look while a run is under way: one lost poll must not stop the page.
  useEffect(() => {
    if (!running) return;
    const id = setTimeout(() => setTick((n) => n + 1), POLL_MS);
    return () => clearTimeout(id);
  }, [running, st, failure]);

  const ask = () => {
    setAskFailure(null);
    askAnalysis({ team, weeks, product, bots })
      .then((data) => setSt({ key, data }))
      .catch((e: unknown) =>
        setAskFailure(e instanceof Error ? e.message : String(e)),
      );
  };

  if (!current) {
    if (failure?.key === key)
      return <p className="m-0 text-[13.5px] text-muted">{failure.msg}</p>;
    return (
      <div
        aria-busy="true"
        className="skeleton h-40 rounded-ui-lg"
        data-testid="assessment-loading"
      />
    );
  }
  return (
    <Panel
      st={current}
      m={m}
      onAsk={ask}
      onFacts={onFacts}
      askFailure={askFailure}
    />
  );
}
