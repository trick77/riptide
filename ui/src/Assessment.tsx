// The overview's assessment of the closed weeks, as the backend computed
// and judged it (/api/analysis), worded by an LLM where a model is set, by
// the rules' own texts where not. Every number, verdict, focus repo and
// column comes from the backend; the browser formats them and scales the
// bars. The page never says who worded it (user, 2026-11-16: "slop").

import type { TFunction } from "i18next";
import { type ReactNode, useEffect, useState } from "react";
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
import { clock, dayMonth, dur, money, num, pct, stage } from "./format";
import type { Tab } from "./state";
import { Delta, Eyebrow, Pill } from "./ui";

const POLL_MS = 3000;
// Small meta text: plain spaces around the middot are fine there.
const SEP = ` ${String.fromCharCode(0x00b7)} `;
// In a heading an en space each side: plain spaces read cramped (user).
const EN = String.fromCharCode(0x2002);
const HEAD_SEP = `${EN}${String.fromCharCode(0x00b7)}${EN}`;

// The column, lever and facts tab of an item are the backend's (layout):
// the page only shows them.
type Column = LayoutCard["column"];

// What a jam waits on, in the order of the wait chain: one shade of the
// validated sequential ramp per step, the draft grey.
const WAIT_ORDER: PRWaiting[] = [
  "first_review",
  "review",
  "changes",
  "merge",
  "draft",
];
const WAIT_COLOR: Record<PRWaiting, string> = {
  first_review: "bg-seq-4",
  review: "bg-seq-3",
  changes: "bg-seq-2",
  merge: "bg-seq-1",
  draft: "bg-other",
};

// A card in its column's colour: the left rule, a wash fading down from
// the top, the tag. Green runs, red is stuck, blue is resolved.
const COLUMN_STYLE: Record<
  Column,
  { border: string; wash: string; line: string; ink: string; dot: string }
> = {
  good: {
    border: "border-online/35 border-l-online",
    wash: "from-online/15",
    line: "border-online/45",
    ink: "text-online",
    dot: "bg-online",
  },
  bad: {
    border: "border-danger/45 border-l-danger",
    wash: "from-danger/15",
    line: "border-danger-ink/45",
    ink: "text-danger-ink",
    dot: "bg-danger",
  },
  done: {
    border: "border-s1/35 border-l-s1",
    wash: "from-s1/15",
    line: "border-s1/45",
    ink: "text-s1",
    dot: "bg-s1",
  },
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

// The period's calendar days: its first Monday to its last Sunday, in the
// zone the weeks were cut in (a viewer west of it would see a Sunday
// start). A week's end is the next Monday 00:00, so the day shown is the
// instant before it.
function daysLabel(a: AssessmentData, zone?: string): string {
  const w = a.period.weeks;
  if (!w.length) return "";
  const last = new Date(new Date(w[w.length - 1].end).getTime() - 1);
  return `${dayMonth(new Date(w[0].start), zone)}–${dayMonth(last, zone)}`;
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
    return t(`overview.assess.title.${changeKey(s)}`, vars);
  if (s.kind === "resolved") {
    const was = (s.metric ?? "").split(":")[0];
    const kindWord = t(`overview.assess.kind.${was}`, { defaultValue: was });
    vars.what = s.repo ? `${s.repo} (${kindWord})` : label || kindWord;
  }
  return t(`overview.assess.title.${s.kind}`, vars);
}

// A change's words by the backend's tone and magnitude: a small gain is
// never "notably" better.
function changeKey(s: Signal): "changeBad" | "changeGood" | "changeGoodMinor" {
  if (s.tone !== "good") return "changeBad";
  return s.facts[0]?.magnitude === "minor" ? "changeGoodMinor" : "changeGood";
}

function ruleText(t: TFunction, s: Signal): string {
  if (s.kind === "change") return t(`overview.assess.rule.${changeKey(s)}`);
  return t(`overview.assess.rule.${s.kind}`, { defaultValue: "" });
}

function Prev({ f }: { f: Fact }) {
  const { t } = useTranslation();
  return <Delta change={f} format={(v) => fmtFact(t, f.unit, v)} />;
}

function Seen({ n }: { n: number }) {
  const { t } = useTranslation();
  if (n <= 0) return null;
  return (
    <span className="inline-block rounded-full border border-ochre/40 px-1.5 font-mono text-[10.5px] text-ochre">
      {t("overview.assess.seen", { count: n })}
    </span>
  );
}

// LowSample says how few cases the signal rests on, where the backend
// counted them: ochre, read with care.
function LowSample({ s }: { s: Signal }) {
  const { t } = useTranslation();
  if (!s.low_sample) return null;
  return (
    <Pill tone="warn">
      {s.sample != null
        ? t("overview.assess.lowSample", { count: s.sample })
        : t("common.lowSample")}
    </Pill>
  );
}

// WaitingBar is what a jam waits on: one line when all wait on the same,
// a bar with its legend when they differ.
function WaitingBar({ w }: { w: WaitingCounts }) {
  const { t } = useTranslation();
  const parts = WAIT_ORDER.filter((k) => w[k] > 0);
  const total = parts.reduce((s, k) => s + w[k], 0);
  if (!total) return null;
  if (parts.length === 1)
    return (
      <p className="my-1.5 text-[13px] text-muted">
        {parts[0] === "draft"
          ? t("overview.assess.allDraft", { count: total })
          : t("overview.assess.waitingAll", {
              count: total,
              what: t(`overview.assess.waiting.${parts[0]}`),
            })}
      </p>
    );
  return (
    <div className="my-1.5">
      <div className="flex h-2 gap-0.5">
        {parts.map((k) => (
          <span
            key={k}
            className={`rounded-sm ${WAIT_COLOR[k]}`}
            style={{ flex: w[k] }}
          />
        ))}
      </div>
      <div className="mt-1 flex flex-wrap gap-x-3 font-mono text-[11px] text-muted">
        {parts.map((k) => (
          <span key={k}>
            <i
              className={`mr-1 inline-block size-2 rounded-sm ${WAIT_COLOR[k]}`}
            />
            {t("overview.assess.waitingOn", {
              what: t(`overview.assess.waiting.${k}`),
              count: w[k],
            })}
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
};

function FocusBlock({
  f,
  n,
  wide,
  onFacts,
}: {
  f: FocusView;
  // n: its place in the backend's ranking, from 1.
  n: number;
  // wide: the only repo in focus, its numbers beside its words.
  wide: boolean;
  onFacts: OnFacts;
}) {
  const { t } = useTranslation();
  const first = f.r.facts[0];
  return (
    <article
      className={`min-w-0 rounded-ui-lg border border-border bg-linear-to-b from-danger/10 to-transparent to-60% px-4 pt-4 pb-3 ${
        wide ? "md:grid md:grid-cols-[minmax(220px,1fr)_2fr] md:gap-x-7" : ""
      }`}
    >
      <div className="min-w-0">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <span className="font-serif text-[24px] leading-none text-danger-ink tabular-nums">
            {n}
          </span>
          <h4 className="m-0 font-mono text-[16px] font-semibold break-all text-ink">
            {f.r.repo}
          </h4>
          <span className="rounded-full border border-danger-ink/40 px-2 font-mono text-[10.5px] tracking-[.06em] text-danger-ink uppercase">
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
                {t(`overview.assess.label.${x.label}`, {
                  defaultValue: x.label,
                })}
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
      </div>
      <div className="min-w-0">
        <p className="my-2 text-sm text-ink-dim">{f.take}</p>
        <Retro q={f.retro} />
        <Lever k={f.lever} />
        <button
          type="button"
          onClick={() => onFacts(f.tab, f.r.repo)}
          className="no-print mt-1.5 border-0 bg-transparent p-0 text-left text-[13px] text-accent-strong hover:underline"
        >
          {t("overview.assess.factsOf", { repo: f.r.repo })}
        </button>
      </div>
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
};

function SignalCard({ c }: { c: CardView }) {
  const { t } = useTranslation();
  const f = c.s.facts[0];
  const d = c.s.drivers?.[0];
  const style = COLUMN_STYLE[c.column];
  // The column says "runs" or "stuck"; only a resolution, which shares the
  // left with the gains, names its own.
  const tags = c.column === "done" || c.s.low_sample || c.s.seen > 0;
  return (
    <article
      className={`min-w-0 rounded-ui border border-l-[3px] bg-linear-to-b ${style.wash} to-transparent to-75% px-3.5 pt-3 pb-3 ${style.border}`}
    >
      {tags && (
        <div className="mb-2 flex flex-wrap items-center gap-1.5">
          {c.column === "done" && (
            <span
              className={`rounded-full border px-1.5 font-mono text-[10.5px] tracking-[.06em] uppercase ${style.line} ${style.ink}`}
            >
              {t("overview.assess.col.done")}
            </span>
          )}
          <LowSample s={c.s} />
          <Seen n={c.s.seen} />
        </div>
      )}
      <p className="m-0 mb-2.5 text-[15px] leading-snug font-semibold text-ink">
        {c.title}
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
    </article>
  );
}

// A count of the tally, in its column's colour; the calm repos grey.
function TallyChip({
  col,
  n,
  children,
}: {
  col?: Column;
  n: string;
  children: ReactNode;
}) {
  const style = col ? COLUMN_STYLE[col] : null;
  return (
    <li
      className={`inline-flex items-baseline gap-[7px] rounded-full border py-[5px] pr-[13px] pl-[11px] text-[13px] text-muted ${
        style ? style.line : "border-border"
      }`}
    >
      <span
        aria-hidden="true"
        className={`size-2 self-center rounded-full ${style ? style.dot : "bg-faint"}`}
      />
      <strong
        className={`font-serif text-[17px] font-medium ${style ? style.ink : "text-ink"}`}
      >
        {n}
      </strong>
      {children}
    </li>
  );
}

// One side of the assessment: what runs (or, without evidence, what is
// unremarkable) and what is stuck.
function Side({ col, children }: { col?: Column; children: ReactNode }) {
  const { t } = useTranslation();
  const style = col ? COLUMN_STYLE[col] : null;
  return (
    <section className="flex min-w-0 flex-col gap-2.5">
      <h4
        className={`m-0 flex items-center gap-2 text-[11px] font-medium tracking-[.12em] uppercase ${
          style ? style.ink : "text-muted"
        }`}
      >
        <span
          aria-hidden="true"
          className={`size-2 rounded-full ${style ? style.dot : "bg-faint"}`}
        />
        {t(`overview.assess.col.${col ?? "calm"}`)}
      </h4>
      {children}
    </section>
  );
}

function Quiet({ children }: { children: ReactNode }) {
  return (
    <p className="m-0 rounded-ui border border-dashed border-border px-3.5 py-3 text-[13px] text-faint">
      {children}
    </p>
  );
}

// The view's words: the LLM's where an analysis is shown, the rules'
// otherwise. Both from the same assessment.
type Words = {
  headline: string | null;
  trend: string | null;
  focus: FocusView[];
  cards: CardView[];
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
      },
    ];
  });
  return {
    headline: text ? fill(t, text.headline, facts) : null,
    trend: text?.trend ? fill(t, text.trend, facts) : null,
    focus,
    cards,
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
    // Team and product in the head: copies of several tabs end up on one
    // board.
    const scope = m.product ?? t("page.allProducts");
    const lines = [
      `## Retro-Input riptide${HEAD_SEP}${m.meta.team}${HEAD_SEP}${scope}${HEAD_SEP}${weeks}`,
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

  const tally = st.layout.tally;
  // Each side in the backend's order: gains and resolutions as it ranked
  // them, the worries as worded.
  const left = w.cards.filter((c) => c.column !== "bad");
  const stuck = w.cards.filter((c) => c.column === "bad");
  const calmOf = t("overview.assess.tally.calmOf", {
    calm: num(a.calm),
    active: num(a.active),
  });

  const button =
    st.enabled &&
    (st.running ? (
      <button
        type="button"
        disabled
        className="no-print inline-flex items-center gap-2 rounded-ui-sm border border-border-soft bg-transparent px-2.5 py-1 text-[12.5px] text-faint"
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
        className="no-print rounded-ui-sm border border-border-soft bg-transparent px-2.5 py-1 text-[12.5px] text-faint hover:border-border hover:text-muted disabled:opacity-60"
      >
        {shown ? t("overview.assess.ask") : t("overview.assess.askFirst")}
      </button>
    ));

  return (
    <div
      data-testid="assessment"
      className="rounded-ui-lg border border-border bg-panel px-6 pt-5 pb-5"
    >
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5">
        <p className="m-0 text-[12.5px] text-muted">
          <span
            title={t("overview.assess.withoutCurrent")}
            className="cursor-help border-b border-dotted border-faint"
          >
            {weeks}
            {SEP}
            {daysLabel(a, st.zone || undefined)}
          </span>
          {shown && (
            <>
              {SEP}
              {t("overview.assess.madeAt", {
                date: stamp(shown.finished_at, lang),
              })}
              , {t(`overview.assess.trigger.${shown.trigger}`)}
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
          {st.next_run && (
            <>
              {SEP}
              {t("overview.assess.next", { date: stamp(st.next_run, lang) })}
            </>
          )}
        </p>
        {button}
      </div>
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
        <h3 className="mt-4 mb-1.5 max-w-[36ch] font-serif text-[clamp(22px,2.4vw,30px)] leading-[1.2] font-normal tracking-[-.01em] text-ink">
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
        <p className="mt-2 mb-0 max-w-[70ch] text-[15px] text-ink-dim">
          {w.trend}
        </p>
      )}

      <div className="mt-4 flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <ul
          aria-label={t("overview.assess.tallyLabel")}
          className="m-0 flex list-none flex-wrap gap-2 p-0"
        >
          {tally.good > 0 && (
            <TallyChip col="good" n={num(tally.good)}>
              {t("overview.assess.tally.good")}
            </TallyChip>
          )}
          {tally.done > 0 && (
            <TallyChip col="done" n={num(tally.done)}>
              {t("overview.assess.tally.done")}
            </TallyChip>
          )}
          <TallyChip col="bad" n={num(tally.bad)}>
            {t("overview.assess.tally.bad")}
          </TallyChip>
          {a.calm > 0 && (
            <TallyChip n={calmOf}>
              {t("overview.assess.tally.calm", { count: a.active })}
            </TallyChip>
          )}
        </ul>
        {/* Whose the counts and cards are: the team or one product. */}
        <span className="text-[12px] text-faint">
          {m.product
            ? t("overview.assess.scopeProduct", { product: m.product })
            : t("overview.assess.scopeAll")}
        </span>
      </div>

      <div className="mt-4 grid gap-[18px] md:grid-cols-2">
        {tally.good > 0 || tally.done > 0 ? (
          <Side col="good">
            {left.map((c) => (
              <SignalCard key={c.s.id} c={c} />
            ))}
            {a.calm > 0 && (
              <Quiet>
                {t("overview.assess.calmLine", {
                  count: a.active,
                  calm: num(a.calm),
                  active: num(a.active),
                })}
              </Quiet>
            )}
          </Side>
        ) : (
          <Side>
            {a.calm > 0 && (
              <article className="min-w-0 rounded-ui border border-l-[3px] border-border border-l-faint px-3.5 pt-3 pb-3">
                <div className="flex flex-wrap items-baseline gap-2">
                  <span className="font-serif text-[32px] leading-none text-ink tabular-nums">
                    {calmOf}
                  </span>
                  <span className="text-[13px] text-muted">
                    {t("overview.assess.calmLabel", { count: a.active })}
                  </span>
                </div>
              </article>
            )}
            <Quiet>
              {tally.baseline
                ? t("overview.assess.noGain")
                : t("overview.assess.noBaseline")}
            </Quiet>
          </Side>
        )}
        <Side col="bad">
          {stuck.map((c) => (
            <SignalCard key={c.s.id} c={c} />
          ))}
          {stuck.length === 0 && (
            <Quiet>
              {tally.bad > 0
                ? t("overview.assess.stuckBelow")
                : t("overview.assess.nothingStuck")}
            </Quiet>
          )}
        </Side>
      </div>

      {w.focus.length > 0 && (
        <>
          <div className="mt-7 mb-2.5 flex flex-wrap items-baseline justify-between gap-3">
            <Eyebrow className="text-ink-dim">
              {t("overview.assess.focus")}
            </Eyebrow>
            <span className="text-[12px] text-faint">
              {t("overview.assess.focusBy")}
            </span>
          </div>
          <div
            className={`grid gap-3 ${w.focus.length > 1 ? "md:grid-cols-[repeat(auto-fit,minmax(260px,1fr))]" : ""}`}
          >
            {w.focus.map((f, i) => (
              <FocusBlock
                key={f.r.repo}
                f={f}
                n={i + 1}
                wide={w.focus.length === 1}
                onFacts={onFacts}
              />
            ))}
          </div>
        </>
      )}

      {(retros.length > 0 || w.question) && (
        <div className="mt-6 rounded-ui border border-border bg-bg/40 px-4 py-3">
          <div className="mb-2 flex flex-wrap items-center gap-2.5">
            <b className="flex-1 text-[13px] font-medium text-ink">
              {t("overview.assess.retroBox")}
            </b>
            {copied && (
              <span className="font-mono text-xs text-online">
                {t("overview.assess.copied")}
              </span>
            )}
            <button
              type="button"
              onClick={copy}
              className="no-print rounded-ui-sm border border-border bg-active px-3 py-1 text-[13px] text-ink-dim hover:border-accent"
            >
              {t("overview.assess.copy")}
            </button>
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
        <div className="mt-3 flex flex-wrap items-center gap-1.5 text-[13px] text-muted">
          {t("overview.assess.also")}
          {also.map(({ s, column }) => (
            <span
              key={s.id}
              className={`rounded-full border px-2.5 py-0.5 text-[12.5px] ${COLUMN_STYLE[column].line} ${COLUMN_STYLE[column].ink}`}
            >
              {signalTitle(t, s, m)}
            </span>
          ))}
        </div>
      )}
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
  const { team, weeks, product } = query;
  const key = `${team}|${weeks}|${product ?? ""}`;
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
    const k = `${team}|${weeks}|${product ?? ""}`;
    fetchAnalysis({ team, weeks, product }, ctl.signal)
      .then((data) => setSt({ key: k, data }))
      .catch((e: unknown) => {
        if (!ctl.signal.aborted)
          setFailure({
            key: k,
            msg: e instanceof Error ? e.message : String(e),
          });
      });
    return () => ctl.abort();
  }, [team, weeks, product, tick]);

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
    askAnalysis({ team, weeks, product })
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
