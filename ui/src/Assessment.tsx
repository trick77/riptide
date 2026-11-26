// The overview's assessment of the closed weeks, as the backend computed
// and judged it (/api/analysis), worded by an LLM where a model is set, by
// the rules' own texts where not. Every number, verdict, focus repo and
// column comes from the backend; the browser formats them and scales the
// bars. The page never says who worded it (user, 2026-11-16: "slop").

import type { TFunction } from "i18next";
import { type ReactNode, useEffect, useRef, useState } from "react";
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
  type Query,
  type Signal,
  type WaitingCounts,
} from "./api";
import { shareLow, Spark } from "./charts";
import {
  clock,
  dayMonth,
  dur,
  fullDate,
  fullDay,
  money,
  num,
  pct,
  stage,
  weekSpan,
} from "./format";
import { SEP, WAIT_FILL, WAITS } from "./shared";
import type { Tab } from "./state";
import { Delta, Eyebrow } from "./ui";

const POLL_MS = 3000;
// Small meta text: plain spaces around the middot are fine there.
const META_SEP = ` ${String.fromCharCode(0x00b7)} `;

// The column, lever and facts tab of an item are the backend's (layout):
// the page only shows them.
type Column = LayoutCard["column"];

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
  // A hint is neutral, never red (owner, 2026-11).
  hint: {
    border: "border-border border-l-muted",
    wash: "from-active",
    line: "border-border",
    ink: "text-muted",
    dot: "bg-muted",
  },
  // What the data cannot judge: the limits' ochre.
  limit: {
    border: "border-ochre/35 border-l-ochre",
    wash: "from-ochre/10",
    line: "border-ochre/45",
    ink: "text-ochre",
    dot: "bg-ochre",
  },
};

// A column the page does not know (a newer backend) is drawn as a hint.
function columnStyle(c: string) {
  return COLUMN_STYLE[c as Column] ?? COLUMN_STYLE.hint;
}

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
  return weekSpan(t, w[0], w[w.length - 1]);
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

// A number's words; a label the catalog does not know shows nothing, never
// its key.
function labelText(t: TFunction, label: string, prod = "PROD"): string {
  return t(`overview.assess.label.${label}`, { defaultValue: "", prod });
}

// The rule title of a signal, its words filled from the signal itself;
// prod is the production stage as the page shows it.
export function signalTitle(t: TFunction, s: Signal, prod = "PROD"): string {
  if (s.kind === "state" && s.state)
    return t(`overview.assess.state.${s.state.key}.title`, {
      defaultValue: "",
      prod,
      was: s.state.was
        ? t(`overview.assess.state.${s.state.was}.title`, {
            defaultValue: "",
            prod,
          })
        : "",
    });
  const label = s.facts[0] ? labelText(t, s.facts[0].label, prod) : "";
  const vars = {
    repo: s.repo ?? "",
    kind: s.run_kind
      ? t(`build.kind.${s.run_kind}`, { defaultValue: s.run_kind })
      : "",
    source: t(`overview.assess.source.${s.source}`, {
      defaultValue: s.source,
    }),
    label,
    what: "",
  };
  if (s.kind === "change")
    return upperFirst(t(`overview.assess.title.${changeKey(s)}`, vars));
  if (s.kind === "resolved") {
    const parts = (s.metric ?? "").split(":");
    const was = parts[0];
    const kindWord =
      was === "state"
        ? t(`overview.assess.state.${parts[1]}.title`, {
            defaultValue: "",
            prod,
          })
        : t(`overview.assess.kind.${was}`, { defaultValue: "" });
    // A state is named by its own title, never by its count's label.
    vars.what = s.repo
      ? kindWord
        ? `${s.repo} (${kindWord})`
        : s.repo
      : was === "state"
        ? kindWord || label
        : label || kindWord;
  }
  const title = t(`overview.assess.title.${s.kind}`, {
    ...vars,
    defaultValue: "",
  });
  // Only a title a label leads: a repo or a source keeps its own case.
  return s.kind === "question" || (s.kind === "resolved" && !s.repo)
    ? upperFirst(title)
    : title;
}

// A title led by a number's label ("gemergte PRs pro Woche: …") starts
// upper case, as every title does.
function upperFirst(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}

// A change's words by the backend's tone: a gain is raised only when
// notable, as a worsening.
function changeKey(s: Signal): "changeBad" | "changeGood" {
  return s.tone === "good" ? "changeGood" : "changeBad";
}

// The rule's numbers come from the backend, never the text: the agent
// wait's floor (Build.agent_wait_min_s) and share, the jam's count and days
// (OpenPRs.jam_min, long_days), a slower build's factor
// (Hotspots.slow_factor).
function ruleText(t: TFunction, s: Signal, m: Metrics): string {
  if (s.kind === "change") return t(`overview.assess.rule.${changeKey(s)}`);
  // A state resolves when its units are clear again, judged anew from the
  // data, not when a number is back where it was.
  if (s.kind === "resolved" && s.metric?.startsWith("state:"))
    return t("overview.assess.rule.resolvedState");
  if (s.kind === "state" && s.state)
    return t(`overview.assess.state.${s.state.key}.rule`, {
      defaultValue: "",
      prod: stage(m.delivery.production_stage),
    });
  return t(`overview.assess.rule.${s.kind}`, {
    defaultValue: "",
    minutes: num(m.build.agent_wait_min_s / 60),
    share: pct(m.build.agent_wait_share),
    min: m.open_prs.jam_min,
    days: m.open_prs.long_days,
    factor: num(m.build.hotspots.slow_factor, 1),
  });
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

// WaitingBar is what a jam waits on: one line when all wait on the same,
// a bar with its legend when they differ.
function WaitingBar({ w }: { w: WaitingCounts }) {
  const { t } = useTranslation();
  const parts = WAITS.filter((k) => w[k] > 0);
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
            className={`rounded-sm ${WAIT_FILL[k].swatch}`}
            style={{ flex: w[k] }}
          />
        ))}
      </div>
      <div className="mt-1 flex flex-wrap gap-x-3 font-mono text-[11px] text-muted">
        {parts.map((k) => (
          <span key={k}>
            <i
              className={`mr-1 inline-block size-2 rounded-sm ${WAIT_FILL[k].swatch}`}
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

// The next step, where the backend gives one: none for a resolution, and
// none the catalog does not know, never its key.
function Lever({ k, prod = "PROD" }: { k: string; prod?: string }) {
  const { t } = useTranslation();
  const text = k
    ? t(`overview.assess.lever.${k}`, { defaultValue: "", prod })
    : "";
  if (!text) return null;
  return (
    <div className="text-[13.5px] text-ink">
      <span className="text-accent-strong">→</span> {text}
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
  // prod is the production stage as the page shows it.
  prod: string;
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
                {labelText(t, x.label, f.prod)}
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
        <Lever k={f.lever} prod={f.prod} />
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
  // prod is the production stage as the page shows it.
  prod: string;
  // lastDay is the period's last day, what a state was judged as of.
  lastDay: string;
};

const SPARK_STROKE: Record<Column, string> = {
  good: "var(--color-online)",
  bad: "var(--color-danger-ink)",
  done: "var(--color-s1)",
  hint: "var(--color-muted)",
  limit: "var(--color-ochre)",
};

type StateData = NonNullable<Signal["state"]>;
type StateUnitData = StateData["units"][number];

function bare(v: number): string {
  return num(v, Number.isInteger(v) ? 0 : 1);
}

// A unit's value as the backend says it is: a share, else a count or days,
// one decimal where it has one (working days).
function unitValue(c: StateData, v: number): string {
  return c.value_unit === "share" ? pct(v) : bare(v);
}

// The grammar of a span in a state's words: minutes, working days or
// days, the dative where the sentence needs it ("nach mehr als 10
// Arbeitstagen"). Wording only; the span's value and unit are the
// backend's.
type Span = "minutes" | "wd" | "wdDat" | "days" | "daysDat";

function span(t: TFunction, kind: Span, v: number): string {
  return t(`overview.assess.state.span.${kind}`, { count: v, n: bare(v) });
}

// The threshold's span a case must lie in, in its unit (the backend's),
// as the subject or object of a sentence, or in the dative; "" where the
// state has none.
function withinText(t: TFunction, c: StateData, dative = false): string {
  const v = c.threshold?.within;
  let kind: Span | null = null;
  switch (c.threshold?.within_unit) {
    case "minutes":
      kind = "minutes";
      break;
    case "working_days":
      kind = dative ? "wdDat" : "wd";
      break;
    case "days":
      kind = dative ? "daysDat" : "days";
      break;
  }
  return kind && v != null ? span(t, kind, v) : "";
}

// The states whose rows a mark of the facts table lists across their
// units, in the viewer's words (terms.json: the backend searches the same
// words in both languages). The others link per unit only.
const STATE_TERM: Partial<
  Record<StateData["key"], (t: TFunction, prod: string) => string>
> = {
  not_on_prod: (t, prod) => t("facts.prodState.not_yet", { prod }),
  untraced: (t) => t("facts.prodState.not_placeable"),
  approved_waiting: (t) => t("facts.prWaiting.merge"),
  first_review_wait: (t) => t("facts.prWaiting.first_review"),
  flaky_builds: (t) => t("facts.flaky"),
};

// One unit of a state in the state's own words, after its name: every
// number, the ranking and the worst three are the backend's.
export function stateUnitText(
  t: TFunction,
  c: StateData,
  u: StateUnitData,
  prod: string,
): string {
  // A week Bitbucket lost part of (a merge pause) names the gap its typical
  // value leaves out.
  const key =
    c.key === "first_review_wait"
      ? u.measure === "typical"
        ? "unitTypical"
        : "unitOldest"
      : u.lost
        ? "unitLost"
        : "unit";
  const text = t(`overview.assess.state.${c.key}.${key}`, {
    defaultValue: "{{n}}",
    n: unitValue(c, u.value),
    count: u.count ?? 0,
    of: u.of != null ? num(u.of) : "",
    typical: u.typical != null ? num(u.typical, 1) : "",
    week: u.week ? u.week.kw : "",
    names: u.names
      .map((x) => (c.names_kind === "stage" ? stage(x) : x))
      .join(", "),
    kind:
      c.names_kind === "build_kind"
        ? u.names
            .map((k) => t(`build.kind.${k}`, { defaultValue: k }))
            .join(", ")
        : "",
    ci: u.ci_seconds != null ? dur(u.ci_seconds) : "",
    reason: u.reason
      ? t(`facts.prodReason.${u.reason}`, { defaultValue: u.reason, prod })
      : "",
    lines: c.threshold?.min != null ? num(c.threshold.min) : "",
    within: withinText(t, c),
    withinDat: withinText(t, c, true),
    // A unit's value as a span of working days, in the dative ("seit 3
    // Arbeitstagen") or not ("3 Arbeitstage lang").
    days: span(t, "wd", u.value),
    daysDat: span(t, "wdDat", u.value),
    prod,
  });
  // Held within the hysteresis, or held two sprints (what turned the card
  // red): the backend's, said after the unit.
  const marks = [
    u.held ? t("overview.assess.state.held") : "",
    u.two_sprints ? t("overview.assess.state.twoSprints") : "",
  ].filter((m) => m !== "");
  return [text, ...marks].join(" ");
}

// The threshold a state measured against, in the team's words: the
// agreement it breaks, or when it is reported. The values are the
// backend's (config thresholds), never written into a text; none for a
// state measured against no agreement.
export function thresholdText(
  t: TFunction,
  c: StateData,
  prod: string,
): string {
  const th = c.threshold;
  if (!th) return "";
  return t(`overview.assess.state.${c.key}.threshold`, {
    defaultValue: "",
    count: th.value,
    n: th.unit === "share" ? pct(th.value) : bare(th.value),
    min: th.min != null ? num(th.min) : "",
    // The second threshold with its noun, singular for one ("ab 1 PR").
    minN:
      th.min != null
        ? t(`overview.assess.state.${c.key}.min`, {
            count: th.min,
            n: num(th.min),
            defaultValue: num(th.min),
          })
        : "",
    within: withinText(t, c),
    withinDat: withinText(t, c, true),
    prod,
  });
}

// "k of n <units>", the backend's counts; the unit's word is the state's
// where its units are of one kind only (a bot's branches). None for a
// team-wide state or one that judged nothing.
function stateOf(t: TFunction, c: StateData): string | null {
  if (c.unit === "team" || c.judged === 0) return null;
  const unit = c.key === "bot_loop" ? "botBranch" : c.unit;
  return t(`overview.assess.state.of.${unit}`, {
    defaultValue: "{{k}} / {{n}}",
    count: c.judged,
    k: num(c.count),
    n: num(c.judged),
  });
}

// The word for n units of a state's kind ("Repos"); "" for a team-wide one.
function unitWord(t: TFunction, c: StateData, n: number): string {
  if (c.unit === "team") return "";
  const unit = c.key === "bot_loop" ? "botBranch" : c.unit;
  return t(`overview.assess.state.unitWord.${unit}`, {
    defaultValue: "",
    count: n,
  });
}

// A state's units: the worst three, the rest behind "+N weitere" (the
// backend's list, in its order). A unit whose rows a facts search lists
// links there by its name.
function StateUnits({
  c,
  prod,
  onFacts,
}: {
  c: StateData;
  prod: string;
  onFacts: OnFacts;
}) {
  const { t } = useTranslation();
  const [all, setAll] = useState(false);
  const shown = all ? c.units : c.worst;
  if (shown.length === 0) return null;
  const tab = c.facts === "" ? null : c.facts;
  return (
    <ul className="m-0 mb-1.5 grid list-none gap-1 p-0">
      {shown.map((u) => {
        const q = tab && u.query ? u.query : null;
        return (
          <li
            key={`${u.name}|${u.week?.kw ?? ""}`}
            className="leading-snug break-words"
          >
            {u.name &&
              (tab && q ? (
                <button
                  type="button"
                  onClick={() => onFacts(tab, q)}
                  title={t("overview.assess.state.unitFacts", { name: u.name })}
                  className="mr-1.5 border-0 bg-transparent p-0 text-left font-mono text-[12.5px] font-semibold break-all text-accent-strong hover:underline"
                >
                  {u.name}
                </button>
              ) : (
                <span className="mr-1.5 font-mono text-[12.5px] font-semibold break-all text-ink">
                  {u.name}
                </span>
              ))}
            <span>{stateUnitText(t, c, u, prod)}</span>
          </li>
        );
      })}
      {c.more > 0 && (
        <li>
          <button
            type="button"
            aria-expanded={all}
            onClick={() => setAll((v) => !v)}
            className="no-print border-0 bg-transparent p-0 text-[13px] text-accent-strong hover:underline"
          >
            {all
              ? t("overview.assess.state.less")
              : t("overview.assess.state.more", { count: c.more })}
          </button>
        </li>
      )}
    </ul>
  );
}

// A state's card body: k of n units, the units, a team number's worsening
// the state speaks for (the backend folds it onto the card: the only facts
// here with a previous value), since when and as of when, the team's
// threshold.
function StateBody({
  s,
  prod,
  lastDay,
  onFacts,
}: {
  s: Signal;
  prod: string;
  // lastDay is the period's last day (Period.last_day), what the card was
  // judged as of.
  lastDay: string;
  onFacts: OnFacts;
}) {
  const { t } = useTranslation();
  const c = s.state;
  if (!c) return null;
  const threshold = thresholdText(t, c, prod);
  const folded = s.facts.filter((f) => f.prev != null && f.verdict !== "none");
  const of = stateOf(t, c);
  return (
    <div className="mb-2 text-[13.5px] text-ink-dim">
      {of && (
        <p className="m-0 mb-2 font-serif text-[22px] leading-tight text-ink tabular-nums">
          {of}
        </p>
      )}
      <StateUnits c={c} prod={prod} onFacts={onFacts} />
      {folded.map((f) => (
        <p key={f.id} className="m-0 mb-1.5 text-[13px] text-ink-dim">
          {t("overview.assess.state.folded", {
            label: labelText(t, f.label, prod),
            value: fmtFact(t, f.unit, f.value),
          })}{" "}
          <Prev f={f} />
        </p>
      ))}
      <p className="m-0 mt-1.5 font-mono text-[11.5px] text-muted">
        {[
          c.since &&
            t(
              c.since_lower_bound
                ? "overview.assess.state.sinceAtLeast"
                : "overview.assess.state.since",
              { date: fullDate(new Date(c.since)) },
            ),
          lastDay &&
            t("overview.assess.state.asOf", { date: fullDay(lastDay) }),
          c.unjudged > 0 &&
            c.key !== "unmeasurable" &&
            t("overview.assess.state.unjudged", {
              count: c.unjudged,
              n: num(c.unjudged),
              unit: unitWord(t, c, c.unjudged),
            }),
        ]
          .filter(Boolean)
          .join(META_SEP)}
      </p>
      {threshold && (
        <p className="m-0 mt-1 text-[12.5px] text-muted">{threshold}</p>
      )}
    </div>
  );
}

// The link to the rows of a state across its units, by the mark the facts
// table shows them with; none for a state without one.
function StateFactsLink({
  c,
  prod,
  onFacts,
}: {
  c: StateData;
  prod: string;
  onFacts: OnFacts;
}) {
  const { t } = useTranslation();
  const term = STATE_TERM[c.key];
  const tab = c.facts;
  if (!term || tab === "") return null;
  const q = term(t, prod);
  return (
    <button
      type="button"
      onClick={() => onFacts(tab, q)}
      className="no-print mt-1.5 border-0 bg-transparent p-0 text-left text-[13px] text-accent-strong hover:underline"
    >
      {t("overview.assess.state.facts", { term: q })}
    </button>
  );
}

// A card's number week by week, the previous period on a band: a share
// scaled as the build section scales its rates (shareLow, from the card's
// own weeks), a count from 0. Scaling only; the weeks are the backend's.
// No line without a week of this period: its end dot would stand in the
// previous one's band.
function WeekSpark({
  f,
  series,
  column,
}: {
  f: Fact;
  series: NonNullable<Signal["series"]>;
  column: Column;
}) {
  const { t } = useTranslation();
  const values = [...series.prev, ...series.cur];
  const known = values.filter((v): v is number => v != null);
  if (known.length < 2 || series.cur.every((v) => v == null)) return null;
  const share = f.unit === "share";
  const low = share ? shareLow(values) : 0;
  const high = share ? 1 : Math.max(...known);
  // A week's releases are a whole count; per week is the period's mean.
  const unit = f.unit === "per_week" ? "count" : f.unit;
  return (
    <Spark
      values={values}
      low={low}
      high={high}
      split={series.prev.length}
      stroke={SPARK_STROKE[column]}
      className="block h-8 w-40 shrink-0"
      label={t("overview.assess.sparkAria", {
        prev: series.prev.map((v) => fmtFact(t, unit, v)).join(", "),
        cur: series.cur.map((v) => fmtFact(t, unit, v)).join(", "),
      })}
    />
  );
}

function SignalCard({ c, onFacts }: { c: CardView; onFacts: OnFacts }) {
  const { t } = useTranslation();
  // A state's card says it with its units, not with a hero number; nor does
  // a state's resolution (its number is the units it had named).
  const f =
    c.s.state || c.s.metric?.startsWith("state:") ? undefined : c.s.facts[0];
  const d = c.s.drivers?.[0];
  const style = columnStyle(c.column);
  // The column says "runs" or "stuck"; only a resolution, which shares the
  // left with the gains, names its own.
  const tags = c.column === "done" || c.s.seen > 0;
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
          <Seen n={c.s.seen} />
        </div>
      )}
      <p className="m-0 mb-2.5 text-[15px] leading-snug font-semibold text-ink">
        {c.title}
      </p>
      {f && (
        <div className="mb-2 flex flex-wrap items-end justify-between gap-x-3 gap-y-1">
          <div className="flex flex-wrap items-baseline gap-2">
            <span className="font-serif text-[32px] leading-none text-ink tabular-nums">
              {fmtFact(t, f.unit, f.value)}
            </span>
            <span className="text-[13px] text-muted">
              {labelText(t, f.label, c.prod)}
            </span>
            <Prev f={f} />
          </div>
          {c.s.series && (
            <WeekSpark f={f} series={c.s.series} column={c.column} />
          )}
        </div>
      )}
      {c.s.state && (
        <StateBody
          s={c.s}
          prod={c.prod}
          lastDay={c.lastDay}
          onFacts={onFacts}
        />
      )}
      {c.s.waiting && <WaitingBar w={c.s.waiting} />}
      {d && (
        <div className="mt-1.5 font-mono text-xs leading-relaxed text-ochre">
          {t("overview.assess.driver", { repo: d.repo, share: pct(d.share) })}
        </div>
      )}
      {c.take && <p className="my-2 text-sm text-ink-dim">{c.take}</p>}
      <Retro q={c.retro} />
      <Lever k={c.lever} prod={c.prod} />
      {c.s.state && (
        <StateFactsLink c={c.s.state} prod={c.prod} onFacts={onFacts} />
      )}
    </article>
  );
}

// The question's colour in the tally, the retro pill's.
const TALK_STYLE = { ink: "text-s3", dot: "bg-s3" };

// A count of the tally as a big number, in its column's colour; the repos
// grey.
function TallyCount({
  look,
  n,
  children,
}: {
  look?: { ink: string; dot: string };
  n: ReactNode;
  children: ReactNode;
}) {
  return (
    <li className="flex flex-col gap-1">
      <strong
        className={`font-serif text-[44px] leading-none font-normal tabular-nums ${look ? look.ink : "text-muted"}`}
      >
        {n}
      </strong>
      <span className="flex items-center gap-[7px] text-[12.5px] text-muted">
        <span
          aria-hidden="true"
          className={`size-2 rounded-full ${look ? look.dot : "bg-faint"}`}
        />
        {children}
      </span>
    </li>
  );
}

// The words for each reason the backend gives a period as not measurable.
const NOT_MEASURABLE = {
  bitbucket: "bitbucket",
  argocd: "argocd",
  no_merged: "noMerged",
  no_release: "noRelease",
  to_prod: "toProd",
} as const;

// What limits the assessment, one sentence each, in the backend's order of
// the reasons: no previous period to compare with, the holes in the sources'
// data with their dates, and why the period could not be measured, as the
// backend says. A finding is no limit: the cards show it. The holes' days are
// the viewer's, as the data line above the page names the same hole.
function limitLines(t: TFunction, a: AssessmentData, m: Metrics): string[] {
  const day = (iso: string) => fullDate(new Date(iso));
  const lines: string[] = [];
  // Nothing compared at all (the backend's nothing_compared), or only some
  // numbers without a previous value.
  if (a.quiet_reasons.includes("no_baseline"))
    lines.push(
      t(
        a.nothing_compared
          ? "overview.assess.reason.no_baseline_none"
          : "overview.assess.reason.no_baseline",
      ),
    );
  // In a product view Bitbucket's lost events leave the deploys and builds
  // uncompared too: they reach a product through its commits.
  const view = m.product ? "Product" : "";
  for (const l of a.limits) {
    const source = t(`overview.assess.source.${l.source}`, {
      defaultValue: l.source,
    });
    // A silent_end's `to` is the period's end, Monday 00:00: the day after
    // the period, so not named. A silence without `to` still runs.
    if (l.kind === "starts")
      lines.push(
        t("overview.assess.limit.starts", { source, date: day(l.from) }),
      );
    else if (l.kind === "silence")
      lines.push(
        l.to == null
          ? t(`overview.assess.limit.silenceOngoing${view}`, {
              source,
              from: day(l.from),
            })
          : t(`overview.assess.limit.silence${view}`, {
              source,
              from: day(l.from),
              to: day(l.to),
            }),
      );
    else
      lines.push(
        t("overview.assess.limit.silent_end", { source, from: day(l.from) }),
      );
  }
  if (a.not_measurable) {
    // The PRs traced are those of repos that are rolled out: with library
    // PRs left out, the sentence says so.
    const key = NOT_MEASURABLE[a.not_measurable];
    lines.push(
      t(
        `overview.assess.reason.notMeasurable.${key === "toProd" && a.library > 0 ? "toProdRolled" : key}`,
        {
          count: a.coverage.to_prod.n,
          prod: stage(m.delivery.production_stage),
        },
      ),
    );
  }
  return lines;
}

// A group of findings: what is stuck, what runs. Groups share a row by
// their card count, so one card never holds half the width beside three;
// a group without a card is not drawn.
function Group({
  col,
  n,
  children,
}: {
  col: "good" | "bad" | "hint" | "limit";
  n: number;
  children: ReactNode;
}) {
  const { t } = useTranslation();
  const style = COLUMN_STYLE[col];
  const share = Math.min(n, 4);
  // Hints and what cannot be judged say what they are not: no finding.
  const sub =
    col === "hint" || col === "limit"
      ? t(`overview.assess.colSub.${col}`)
      : null;
  return (
    <section
      className="min-w-0"
      style={{ flex: `${share} 1 ${share * 260}px` }}
    >
      <div className="mb-2.5 flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
        <h4
          className={`m-0 flex items-center gap-2 text-[11px] font-medium tracking-[.12em] uppercase ${style.ink}`}
        >
          <span
            aria-hidden="true"
            className={`size-2 rounded-full ${style.dot}`}
          />
          {t(`overview.assess.col.${col}`)}
        </h4>
        {sub && <span className="text-[12px] text-faint">{sub}</span>}
      </div>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(min(250px,100%),1fr))] gap-2.5">
        {children}
      </div>
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
  text: AnalysisText | null,
  m: Metrics,
): Words {
  const facts = factIndex(a);
  const prod = stage(m.delivery.production_stage);
  const byId = new Map(a.signals.map((s) => [s.id, s]));
  const repos = new Map(a.focus.map((r) => [r.repo, r]));
  const rank = (r: FocusRepo) => {
    const s = byId.get(r.signals[0]);
    return s ? t(`overview.assess.kind.${s.kind}`) : "";
  };
  // The rules' question, only where the backend raised one: a number that
  // moved notably. No question without it, whatever a model wrote.
  const question = a.question
    ? `${fmtFact(t, a.question.facts[0]?.unit ?? "count", a.question.facts[0]?.value ?? null)} ${labelText(
        t,
        a.question.facts[0]?.label ?? "",
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
          : sigs.map((s) => signalTitle(t, s, prod)).join("; "),
        retro: ft?.retro ? fill(t, ft.retro, facts) : null,
        lever: lf.lever,
        prod,
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
        title: signalTitle(t, s, prod),
        take: ct ? fill(t, ct.take, facts) : ruleText(t, s, m),
        retro: ct?.retro ? fill(t, ct.retro, facts) : null,
        lever: lc.lever,
        prod,
        lastDay: a.period.last_day,
      },
    ];
  });
  return {
    headline: text ? fill(t, text.headline, facts) : null,
    trend: text?.trend ? fill(t, text.trend, facts) : null,
    focus,
    cards,
    question:
      a.question && text?.question ? fill(t, text.question, facts) : question,
  };
}

function Panel({
  st,
  m,
  onAsk,
  asking,
  onFacts,
  askFailure,
}: {
  st: AnalysisStatus;
  m: Metrics;
  onAsk: () => void;
  // asking: the view's ask is on its way; its button already shows the run,
  // and takes no second click.
  asking: boolean;
  onFacts: OnFacts;
  askFailure: string | null;
}) {
  const { t, i18n } = useTranslation();
  const [copied, setCopied] = useState(false);
  const a = st.assessment;
  const shown = st.analysis;
  const lang = i18n.language === "en" ? "en" : "de";
  const text = shown ? shown.text[lang] : null;
  const w = words(t, a, st.layout, text, m);
  const weeks = weeksLabel(t, a);
  // The backend sends cooldown_until only while the view cools down.
  const cooling = st.cooldown_until != null;

  // The gates are among the limits (silent at the period's end), with their
  // dates: said once, there.
  const limits = limitLines(t, a, m);
  // Only a period the backend could measure and compare may be called
  // calm in any word: nothing stuck, no gain, nothing standing out. A big
  // 0 "stuck" beside the limits would read as such a word. The backend's
  // `limited` says when.
  const limited = a.limited;
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
      `## Retro-Input riptide${SEP}${m.meta.team}${SEP}${scope}${SEP}${weeks}`,
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
  // Each group in the backend's order: gains and resolutions as it ranked
  // them, the worries as worded; hints and what could not be judged in
  // groups of their own, never red.
  const left = w.cards.filter(
    (c) => c.column === "good" || c.column === "done",
  );
  const stuck = w.cards.filter((c) => c.column === "bad");
  const hints = w.cards.filter((c) => c.column === "hint");
  // A column the page does not know yet joins the hints' neutral group.
  const unknown = w.cards.filter(
    (c) => !(c.column in COLUMN_STYLE) && c.column !== "limit",
  );
  const unjudgedCards = w.cards.filter((c) => c.column === "limit");
  const neutral = [...hints, ...unknown];
  const prod = stage(m.delivery.production_stage);

  const button =
    st.enabled &&
    (st.running || asking ? (
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
        {shown || st.outdated
          ? t("overview.assess.ask")
          : t("overview.assess.askFirst")}
      </button>
    ));

  return (
    <div
      data-testid="assessment"
      className="rounded-ui-lg border border-border bg-panel px-6 pt-5 pb-5"
    >
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5">
        <p className="m-0 text-[12.5px] text-muted">
          {/* The whole page reads closed weeks: nothing to set apart. */}
          {weeks}
          {META_SEP}
          {daysLabel(a, st.zone || undefined)}
          {shown && (
            <>
              {META_SEP}
              {t("overview.assess.madeAt", {
                date: stamp(shown.finished_at, lang),
              })}
              , {t(`overview.assess.trigger.${shown.trigger}`)}
              {shown.weeks_old > 0 && (
                <>
                  {META_SEP}
                  <span className="text-ochre">
                    {t("overview.assess.older", { count: shown.weeks_old })}
                  </span>
                </>
              )}
            </>
          )}
          {st.next_run && (
            <>
              {META_SEP}
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
      {st.outdated && (
        <p className="mt-3 mb-0 rounded-ui-sm border border-border bg-active px-3 py-2 text-[13.5px] text-ink-dim">
          {t("overview.assess.outdated")}
          {st.enabled && t("overview.assess.outdatedAsk")}
        </p>
      )}
      {/* What limits the assessment first, before any count: the counts
          read only within it. */}
      {limits.length > 0 && (
        <div
          role="note"
          aria-label={t("overview.assess.limitsLabel")}
          className="mt-3 mb-1 grid gap-1 rounded-ui-sm border border-ochre/35 bg-ochre-wash px-2.5 py-1.5 text-[13px] text-ochre"
        >
          {limits.map((l) => (
            <p key={l} className="m-0">
              {l}
            </p>
          ))}
        </div>
      )}
      {/* Libraries only: nothing to roll out, no hole in the data, so no
          limit; said plainly where the delivery would be. */}
      {a.libraries_only && (
        <p className="mt-3 mb-1 text-[13.5px] text-ink-dim">
          {t(
            m.product
              ? "overview.assess.librariesOnly"
              : "overview.assess.librariesOnlyTeam",
          )}
        </p>
      )}

      {/* The headline and the tally side by side; without an analysis the
          tally stands alone. */}
      <div
        className={`mt-4 grid items-end gap-x-12 gap-y-4 border-b border-border-soft pb-5 ${
          w.headline ? "lg:grid-cols-[minmax(0,1.4fr)_auto]" : ""
        }`}
      >
        {w.headline && (
          <div className="min-w-0">
            <h3 className="m-0 max-w-[24ch] font-serif text-[clamp(26px,3.2vw,42px)] leading-[1.12] font-normal tracking-[-.01em] text-balance text-ink">
              {w.headline}
            </h3>
            {w.trend && (
              <p className="mt-2.5 mb-0 max-w-[70ch] text-[15px] text-ink-dim">
                {w.trend}
              </p>
            )}
          </div>
        )}
        <ul
          aria-label={t("overview.assess.tallyLabel")}
          className="m-0 flex list-none flex-wrap gap-x-[34px] gap-y-3.5 p-0"
        >
          {/* A zero is no alarm: grey, like the repos; and none at all
              where the limits leave it saying nothing (a hole in the data,
              no comparison, something not judgeable). */}
          {(tally.bad > 0 || !limited) && (
            <TallyCount
              look={tally.bad > 0 ? COLUMN_STYLE.bad : undefined}
              n={num(tally.bad)}
            >
              {t("overview.assess.tally.bad")}
            </TallyCount>
          )}
          {tally.good > 0 && (
            <TallyCount look={COLUMN_STYLE.good} n={num(tally.good)}>
              {t("overview.assess.tally.good")}
            </TallyCount>
          )}
          {tally.done > 0 && (
            <TallyCount look={COLUMN_STYLE.done} n={num(tally.done)}>
              {t("overview.assess.tally.done")}
            </TallyCount>
          )}
          {(tally.hint ?? 0) > 0 && (
            <TallyCount look={COLUMN_STYLE.hint} n={num(tally.hint)}>
              {t("overview.assess.tally.hint")}
            </TallyCount>
          )}
          {(tally.limit ?? 0) > 0 && (
            <TallyCount look={COLUMN_STYLE.limit} n={num(tally.limit)}>
              {t("overview.assess.tally.limit")}
            </TallyCount>
          )}
          {tally.talk > 0 && (
            <TallyCount look={TALK_STYLE} n={num(tally.talk)}>
              {t("overview.assess.tally.talk")}
            </TallyCount>
          )}
          {a.active > 0 && (
            <TallyCount
              n={
                <>
                  {num(a.calm)}
                  <small className="text-[22px]">
                    {" "}
                    {t("overview.assess.tally.ofRepos", {
                      repos: num(a.active),
                    })}
                  </small>
                </>
              }
            >
              {t("overview.assess.tally.calm", { count: a.active })}
            </TallyCount>
          )}
          {a.unjudged > 0 && (
            <TallyCount n={num(a.unjudged)}>
              {t("overview.assess.tally.unjudged", { count: a.unjudged })}
            </TallyCount>
          )}
        </ul>
      </div>

      {/* The one calm word, only where the backend says quiet: findings
          without anything stuck simply show their cards. */}
      {a.quiet && (
        <div className="mt-5">
          <Quiet>{t("overview.assess.quiet")}</Quiet>
        </div>
      )}
      {(tally.bad > 0 || left.length > 0) && (
        <div className="mt-5 flex flex-wrap gap-x-[18px] gap-y-5">
          {/* Stuck without a card of its own keeps its heading: the
              sentence says where the findings are. */}
          {tally.bad > 0 && (
            <Group col="bad" n={Math.max(stuck.length, 1)}>
              {stuck.map((c) => (
                <SignalCard key={c.s.id} c={c} onFacts={onFacts} />
              ))}
              {stuck.length === 0 && (
                <Quiet>{t("overview.assess.stuckBelow")}</Quiet>
              )}
            </Group>
          )}
          {left.length > 0 && (
            <Group col="good" n={left.length}>
              {left.map((c) => (
                <SignalCard key={c.s.id} c={c} onFacts={onFacts} />
              ))}
            </Group>
          )}
        </div>
      )}
      {(neutral.length > 0 || unjudgedCards.length > 0) && (
        <div className="mt-5 flex flex-wrap gap-x-[18px] gap-y-5">
          {neutral.length > 0 && (
            <Group col="hint" n={neutral.length}>
              {neutral.map((c) => (
                <SignalCard key={c.s.id} c={c} onFacts={onFacts} />
              ))}
            </Group>
          )}
          {unjudgedCards.length > 0 && (
            <Group col="limit" n={unjudgedCards.length}>
              {unjudgedCards.map((c) => (
                <SignalCard key={c.s.id} c={c} onFacts={onFacts} />
              ))}
            </Group>
          )}
        </div>
      )}

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
              className={`rounded-full border px-2.5 py-0.5 text-[12.5px] ${columnStyle(column).line} ${columnStyle(column).ink}`}
            >
              {signalTitle(t, s, prod)}
              {/* A state without a card of its own still says how wide. */}
              {s.state && stateOf(t, s.state) && (
                <span className="text-muted">
                  {META_SEP}
                  {stateOf(t, s.state)}
                </span>
              )}
            </span>
          ))}
        </div>
      )}
      {/* What the findings leave out: the repos without one, those too
          thin to judge and the idle ones, the backend's counts as sent;
          that nothing counts as a gain only where a gain could be told;
          and whose the counts and cards are. */}
      <p className="mt-5 mb-0 flex flex-wrap items-center gap-x-[18px] gap-y-1 border-t border-border-soft pt-3 text-[13px] text-muted">
        {a.repos > 0 && (
          <span className="inline-flex items-center gap-2">
            <span aria-hidden="true" className="size-2 rounded-full bg-faint" />
            {/* The active repos, as the panel counts them; the idle ones,
                active only before, apart. */}
            {[
              a.active > 0 &&
                t("overview.assess.calmLine", {
                  count: a.active,
                  parts: [
                    t("overview.assess.calmPart", { count: num(a.calm) }),
                    a.unjudged > 0 &&
                      t("overview.assess.unjudgedPart", {
                        count: num(a.unjudged),
                      }),
                  ]
                    .filter(Boolean)
                    .join(", "),
                }),
              a.idle > 0 &&
                t("overview.assess.idleLine", {
                  count: a.idle,
                  n: num(a.idle),
                }),
            ]
              .filter(Boolean)
              .join(" ")}
          </span>
        )}
        {tally.good === 0 && tally.done === 0 && !limited && (
          <span>{t("overview.assess.noGain")}</span>
        )}
        <span className="ml-auto text-[12px] text-faint">
          {m.product
            ? t("overview.assess.scopeProduct", { product: m.product })
            : t("overview.assess.scopeAll")}
        </span>
      </p>
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
  // Every status and ask failure carries its place in one running order: a
  // status asked for when its request was sent, an ask's answer or failure
  // when it arrived (the server's state after the POST). An older status
  // never replaces a newer one, nor drops a failure that came after it.
  const order = useRef(0);
  const [st, setSt] = useState<{
    key: string;
    data: AnalysisStatus;
    seq: number;
  } | null>(null);
  const [failure, setFailure] = useState<{ key: string; msg: string } | null>(
    null,
  );
  // Each view's failed ask, apart: one view's never hides another's.
  const [askFailures, setAskFailures] = useState<
    ReadonlyMap<string, { msg: string; seq: number }>
  >(new Map());
  // A failed ask reloads its view's status: the status says what the
  // failure means (a run already going, a cooldown). That one load keeps the
  // red box; it is the failure's, not newer than it. By view, the failure's
  // place in the order, until the view's next load takes it.
  const kept = useRef(new Map<string, number>());
  // The views whose ask is on its way: a double click asks once. The ref
  // holds before React commits, the state draws the button.
  const inFlight = useRef(new Set<string>());
  const [asking, setAsking] = useState<ReadonlySet<string>>(new Set());
  const [tick, setTick] = useState(0);
  // The view on the page, for an ask that fails after the page moved on.
  const shown = useRef(key);
  useEffect(() => {
    shown.current = key;
  }, [key]);

  useEffect(() => {
    const ctl = new AbortController();
    const k = `${team}|${weeks}|${product ?? ""}`;
    const sent = ++order.current;
    const keep = kept.current.get(k);
    kept.current.delete(k);
    fetchAnalysis({ team, weeks, product }, ctl.signal)
      .then((data) => {
        setSt((prev) =>
          prev?.key === k && prev.seq > sent
            ? prev
            : { key: k, data, seq: sent },
        );
        // A status asked for after the view's ask failed outdates the
        // failure: back on the view, the old red box must not stand next
        // to it. Other views' failures go too, but one their view has not
        // loaded since: the page lives long, the map must not grow with it.
        setAskFailures((prev) => {
          const next = new Map<string, { msg: string; seq: number }>();
          for (const [v, f] of prev)
            if (v === k ? f.seq > sent || f.seq === keep : kept.current.has(v))
              next.set(v, f);
          return next.size === prev.size ? prev : next;
        });
      })
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

  // The answer belongs to the view asked for: once the page shows another
  // view, it must not replace that one's status (nothing would reload it).
  const ask = () => {
    if (inFlight.current.has(key)) return;
    inFlight.current.add(key);
    setAskFailures((prev) => {
      if (!prev.has(key)) return prev;
      const next = new Map(prev);
      next.delete(key);
      return next;
    });
    setAsking((prev) => new Set(prev).add(key));
    askAnalysis({ team, weeks, product })
      .then((data) => {
        const seq = ++order.current;
        setSt((prev) => (prev && prev.key !== key ? prev : { key, data, seq }));
      })
      .catch((e: unknown) => {
        const seq = ++order.current;
        const msg = e instanceof Error ? e.message : String(e);
        setAskFailures((prev) => new Map(prev).set(key, { msg, seq }));
        kept.current.set(key, seq);
        if (shown.current === key) setTick((n) => n + 1);
      })
      .finally(() => {
        inFlight.current.delete(key);
        setAsking((prev) => {
          const next = new Set(prev);
          next.delete(key);
          return next;
        });
      });
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
      asking={asking.has(key)}
      onFacts={onFacts}
      askFailure={askFailures.get(key)?.msg ?? null}
    />
  );
}
