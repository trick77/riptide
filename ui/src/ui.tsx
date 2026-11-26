// The page's primitives, in the family's look (almanach, noergler): tokens
// only, through the generated utilities.

import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { Change } from "./api";
import { type UiLanguage, setLanguage } from "./i18n";

export function Eyebrow({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={`text-[11px] font-medium tracking-[.12em] text-muted uppercase ${className}`}
    >
      {children}
    </div>
  );
}

// A framework a section maps to, and the metric of it.
export type Framework = [name: string, metric: string];

export function SectionHead({
  id,
  eyebrow,
  title,
  lead,
  frameworks,
}: {
  id: string;
  eyebrow: ReactNode;
  title: string;
  // One sentence on what the section answers and how to read it.
  lead?: ReactNode;
  frameworks?: Framework[];
}) {
  const { t } = useTranslation();
  return (
    <>
      <Eyebrow>{eyebrow}</Eyebrow>
      <h3
        id={id}
        className="mt-0.5 mb-3.5 font-serif text-[21px] font-medium text-ink"
      >
        {title}
      </h3>
      {lead && (
        <p className="-mt-1.5 mb-3 max-w-[80ch] text-[14px] text-ink-dim">
          {lead}
        </p>
      )}
      {frameworks && (
        <ul
          aria-label={t("frameworksLabel")}
          className="m-0 mb-3.5 flex list-none flex-wrap gap-1.5 p-0"
        >
          {frameworks.map(([name, metric]) => (
            <li key={name + metric}>
              <a
                href="#frameworks"
                className="inline-block rounded-md border border-elevated-border px-2 py-px text-[11.5px] text-ink-dim no-underline hover:bg-active"
              >
                <span className="mr-1 font-medium text-muted">{name}</span>
                {metric}
              </a>
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

// A block's head inside a section: its title, and a line of legend where
// the block needs one.
export function SubHead({ title, text }: { title: string; text?: string }) {
  return (
    <div className="mt-7 flex flex-wrap items-baseline gap-x-3.5 gap-y-1">
      <h4 className="m-0 font-serif text-[19px] font-medium text-ink">
        {title}
      </h4>
      {text && <span className="text-[13px] text-muted">{text}</span>}
    </div>
  );
}

// One meaning per colour on every page: green better, red worse (also
// failed, red CI time, a jam), ochre only "read with care" (few cases, a
// data gap, missing).
export const VERDICT_TONE: Record<Exclude<Change["verdict"], "none">, Tone> = {
  better: "ok",
  worse: "fail",
  same: "none",
};

const VERDICT_INK: Record<Change["verdict"], string> = {
  better: "text-online",
  worse: "text-danger-ink",
  same: "text-muted",
  none: "text-muted",
};

// A number against the previous period, one form on every page: the
// previous value under the number (Delta), the verdict at the foot
// (Compare).
export type CompareProps = {
  change: Change;
  format: (v: number) => string;
};

// The previous value right under a number, in the backend's verdict's
// colour: the colour sits on the change, the number stays ink. A Change or
// an assessment's Fact, both carry prev and verdict.
export function Delta({
  change,
  format,
}: {
  change: Pick<Change, "prev" | "verdict">;
  format: (v: number) => string;
}) {
  const { t } = useTranslation();
  if (change.prev == null) return null;
  return (
    <span
      className={`font-mono text-[12px] tabular-nums ${VERDICT_INK[change.verdict]}`}
    >
      {t("compare.before", { v: format(change.prev) })}
    </span>
  );
}

// The foot of a number against the previous period: the backend's verdict,
// or why there is none. The previous value stands under the number
// (Delta), never here again.
export function Compare({ change }: { change: Change }) {
  const { t } = useTranslation();
  if (change.prev == null)
    return (
      <div className="mt-1 border-t border-border-soft pt-2 text-xs text-muted">
        {change.reason === "undirected"
          ? ""
          : change.reason
            ? t(`compare.${change.reason}`)
            : t("compare.none")}
      </div>
    );
  const v = change.verdict;
  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-2.5 gap-y-1 border-t border-border-soft pt-2 text-[12.5px] text-ink-dim">
      {v !== "none" && <Pill tone={VERDICT_TONE[v]}>{t(`compare.${v}`)}</Pill>}
      {change.reason === "undirected" && (
        <span className="text-muted">{t("compare.undirected")}</span>
      )}
    </div>
  );
}

// The header's language switch: the active language's code behind a
// globe, a click switches to the other. Same markup as ../noergler's.
export function LanguageSwitch() {
  const { t, i18n } = useTranslation();
  const next: UiLanguage = i18n.language === "de" ? "en" : "de";
  return (
    <button
      type="button"
      onClick={() => setLanguage(next)}
      aria-label={t("lang.switch")}
      title={t("lang.switch")}
      className="no-print inline-flex h-8 items-center gap-1.5 rounded-ui-sm border border-border bg-panel px-2.5 text-[12.5px] tracking-[.06em] text-ink-dim hover:bg-active"
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
        className="text-muted"
      >
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18" />
        <path d="M12 3a14 14 0 0 1 0 18a14 14 0 0 1 0-18z" />
      </svg>
      {t("lang.code")}
    </button>
  );
}

export function Card({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={`min-w-0 rounded-ui-lg border border-border bg-panel px-5 pt-[18px] pb-4 ${className}`}
    >
      {children}
    </div>
  );
}

// A chart's numbers for a screen reader only. A table sizes itself to its
// content whatever sr-only's 1px box says and widens the page; the block
// wrapper clips it. Never put sr-only on the table itself.
export function SrTable({
  caption,
  children,
}: {
  caption: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="sr-only">
      <table>
        <caption>{caption}</caption>
        {children}
      </table>
    </div>
  );
}

export function CardHead({
  title,
  sub,
  right,
}: {
  title: string;
  sub?: ReactNode;
  right?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1.5">
      <div>
        <div className="text-[15px] font-medium text-ink">{title}</div>
        {sub && <div className="text-[12.5px] text-muted">{sub}</div>}
      </div>
      {right}
    </div>
  );
}

export function WarnLine({ children }: { children: ReactNode }) {
  return (
    <div className="mt-2 flex items-start gap-1.5 text-xs text-ochre">
      <svg
        aria-hidden="true"
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        className="mt-0.5 flex-none"
      >
        <path d="M12 3 2 21h20L12 3z" />
        <path d="M12 10v5" />
        <path d="M12 18h.01" />
      </svg>
      <span>{children}</span>
    </div>
  );
}

export type TileProps = {
  label: string;
  value: string;
  unit?: string;
  note?: ReactNode;
  warn?: ReactNode;
  // The DORA name, or that it is none, in small print under the label.
  dora?: string;
  // The question the number answers, in plain words.
  question?: string;
  // Which way is better, and why; or that there is no better way.
  direction?: ReactNode;
  // The number against the previous period: its change under the number,
  // the verdict at the foot.
  compare?: CompareProps;
  // When set, the tile has a "Was heisst das?" toggle.
  open?: boolean;
  onWhy?: () => void;
  // A small trend line beside the number, and what follows the note: the
  // number per repo.
  spark?: ReactNode;
  extra?: ReactNode;
};

export function Tile({
  label,
  value,
  unit,
  note,
  warn,
  dora,
  question,
  direction,
  compare,
  open,
  onWhy,
  spark,
  extra,
}: TileProps) {
  const { t } = useTranslation();
  return (
    <div
      className={`flex flex-col gap-1 rounded-ui border px-3.5 py-3 ${open ? "border-accent-fill bg-active" : "border-border bg-panel"}`}
    >
      <div className="flex items-start justify-between gap-2">
        <span className="text-[11px] font-medium tracking-[.12em] text-muted uppercase">
          {label}
        </span>
        {onWhy && (
          <button
            type="button"
            aria-expanded={open}
            onClick={onWhy}
            className="no-print rounded-md border-0 bg-transparent px-[7px] py-px text-xs whitespace-nowrap text-muted hover:bg-active hover:text-ink"
          >
            {t("why.button")}
          </button>
        )}
      </div>
      {dora && <div className="-mt-1 text-[11px] text-faint">{dora}</div>}
      {question && <div className="text-[13px] text-ink-dim">{question}</div>}
      <div className="flex flex-wrap items-baseline gap-x-1.5">
        <span className="text-[26px] font-medium text-ink tabular-nums">
          {value}
        </span>
        {unit && <span className="text-[13px] text-muted">{unit}</span>}
        {spark && <span className="ml-auto self-center">{spark}</span>}
      </div>
      {compare?.change.prev != null && (
        <div>
          <Delta change={compare.change} format={compare.format} />
        </div>
      )}
      {note && <div className="text-xs text-muted">{note}</div>}
      {extra}
      {direction && <div className="text-xs text-muted">{direction}</div>}
      {warn && <WarnLine>{warn}</WarnLine>}
      {compare && <Compare change={compare.change} />}
    </div>
  );
}

// Direction is a tile's line on which way is better.
export function Direction({
  arrow,
  label,
  why,
}: {
  arrow?: "up" | "down";
  label: string;
  why: string;
}) {
  return (
    <>
      <span className="font-medium text-ink-dim">
        {arrow === "up" ? "↑ " : arrow === "down" ? "↓ " : ""}
        {label}
      </span>
      {why && <>: {why}</>}
    </>
  );
}

// Why is what one number means and where it comes from: how to read it,
// when to look closer, what moves it; then what it counts, from how much,
// with which filters, and how complete the data behind it is.
export type Why = {
  title: string;
  means?: ReactNode;
  watch?: ReactNode;
  lever?: ReactNode;
  def: ReactNode;
  basis: ReactNode;
  filters: string[];
  coverage: ReactNode;
  query: string;
};

export function WhyPanel({ why }: { why: Why }) {
  const { t } = useTranslation();
  const title = t("why.means", { title: why.title });
  const reading: [string, ReactNode][] = (
    [
      [title, why.means],
      [t("why.watch"), why.watch],
      [t("common.lever"), why.lever],
    ] as [string, ReactNode][]
  ).filter(([, v]) => v != null);
  return (
    <div
      role="region"
      aria-label={title}
      className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-x-7 gap-y-4 rounded-ui-lg border border-elevated-border bg-active px-[18px] py-4"
    >
      {reading.map(([head, text]) => (
        <div key={head}>
          <Eyebrow>{head}</Eyebrow>
          <p className="mt-1.5 mb-0 text-[13.5px] text-ink-dim">{text}</p>
        </div>
      ))}
      <div>
        <Eyebrow>{t("why.def")}</Eyebrow>
        <p className="mt-1.5 mb-0 text-[13.5px] text-ink-dim">{why.def}</p>
      </div>
      <div>
        <Eyebrow>{t("why.basis")}</Eyebrow>
        <p className="mt-1.5 mb-0 text-[13.5px] text-ink-dim">{why.basis}</p>
        <div className="mt-2 flex flex-wrap gap-1.5">
          {why.filters.map((f) => (
            <span
              key={f}
              className="rounded-md border border-accent-dim bg-accent/10 px-[7px] py-px font-mono text-[11px] text-accent-strong"
            >
              {f}
            </span>
          ))}
        </div>
      </div>
      <div>
        <Eyebrow>{t("why.coverage")}</Eyebrow>
        <p className="mt-1.5 mb-0 text-[13.5px] text-ink-dim">{why.coverage}</p>
        <p className="mt-1.5 mb-0 text-[12.5px] text-muted">
          {t("why.query")}{" "}
          <span className="font-mono text-xs text-accent-strong">
            {why.query}
          </span>
        </p>
      </div>
    </div>
  );
}

export type Tone = "ok" | "warn" | "fail" | "none";

const PILL: Record<Tone, string> = {
  ok: "bg-online/15 text-[#82c491]",
  warn: "bg-ochre-wash text-ochre",
  fail: "bg-danger/15 text-danger-ink",
  none: "bg-elevated text-ink-dim",
};
const DOT: Record<Tone, string> = {
  ok: "bg-online",
  warn: "bg-ochre",
  fail: "bg-danger",
  none: "bg-muted",
};

// dot overrides the tone's dot: a texture for what is counted without a
// verdict (texture-unknown).
export function Pill({
  tone,
  dot,
  children,
}: {
  tone: Tone;
  dot?: string;
  children: ReactNode;
}) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-px text-xs font-medium whitespace-nowrap ${PILL[tone]}`}
    >
      <span
        aria-hidden="true"
        className={`size-1.5 rounded-full ${dot ?? DOT[tone]}`}
      />
      {children}
    </span>
  );
}

// A legend entry: the swatch carries identity, the text stays in ink.
export function Swatch({
  className,
  label,
}: {
  className: string;
  label: string;
}) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        aria-hidden="true"
        className={`size-2.5 rounded-[3px] ${className}`}
      />
      {label}
    </span>
  );
}

export function Legend({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-wrap gap-x-3.5 gap-y-1 text-xs text-ink-dim">
      {children}
    </div>
  );
}

export function Note({
  children,
  icon = true,
}: {
  children: ReactNode;
  icon?: boolean;
}) {
  return (
    <div
      role="note"
      className="flex items-start gap-2.5 rounded-ui bg-ochre-wash px-3.5 py-2.5 text-[13px] text-[#e9c48c]"
    >
      {icon && (
        <svg
          aria-hidden="true"
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="mt-0.5 flex-none"
        >
          <circle cx="12" cy="12" r="9" />
          <path d="M12 8v5" />
          <path d="M12 16h.01" />
        </svg>
      )}
      <span>{children}</span>
    </div>
  );
}

export function Mono({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return <span className={`font-mono text-xs ${className}`}>{children}</span>;
}
