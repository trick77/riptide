// The page's primitives, in the family's look (almanach, noergler): tokens
// only, through the generated utilities.

import type { ReactNode } from "react";

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

export function SectionHead({
  id,
  eyebrow,
  title,
}: {
  id: string;
  eyebrow: ReactNode;
  title: string;
}) {
  return (
    <>
      <Eyebrow>{eyebrow}</Eyebrow>
      <h3
        id={id}
        className="mt-0.5 mb-3.5 font-serif text-[21px] font-medium text-ink"
      >
        {title}
      </h3>
    </>
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

export function Grid({ children }: { children: ReactNode }) {
  return (
    <div className="grid grid-cols-[repeat(auto-fit,minmax(230px,1fr))] gap-3">
      {children}
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
  // When set, the tile has a "Woher?" toggle.
  open?: boolean;
  onWhy?: () => void;
};

export function Tile({
  label,
  value,
  unit,
  note,
  warn,
  open,
  onWhy,
}: TileProps) {
  return (
    <div
      className={`rounded-ui border px-3.5 py-3 ${open ? "border-accent-fill bg-active" : "border-border bg-panel"}`}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="text-[11px] font-medium tracking-[.12em] text-muted uppercase">
          {label}
        </span>
        {onWhy && (
          <button
            type="button"
            aria-expanded={open}
            onClick={onWhy}
            className="no-print rounded-md border-0 bg-transparent px-[7px] py-px text-xs text-muted hover:bg-active hover:text-ink"
          >
            Woher?
          </button>
        )}
      </div>
      <div className="mt-0.5 flex items-baseline gap-1.5">
        <span className="text-[26px] font-medium text-ink tabular-nums">
          {value}
        </span>
        {unit && <span className="text-[13px] text-muted">{unit}</span>}
      </div>
      {note && <div className="text-xs text-muted">{note}</div>}
      {warn && <WarnLine>{warn}</WarnLine>}
    </div>
  );
}

// Why is the provenance of one number: what it counts, from how much, with
// which filters, and how complete the data behind it is.
export type Why = {
  title: string;
  def: ReactNode;
  basis: ReactNode;
  filters: string[];
  coverage: ReactNode;
  query: string;
};

export function WhyPanel({ why }: { why: Why }) {
  return (
    <div
      role="region"
      aria-label={`Woher · ${why.title}`}
      className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-x-7 gap-y-4 rounded-ui-lg border border-elevated-border bg-active px-[18px] py-4"
    >
      <div>
        <Eyebrow>Definition · {why.title}</Eyebrow>
        <p className="mt-1.5 mb-0 text-[13.5px] text-ink-dim">{why.def}</p>
      </div>
      <div>
        <Eyebrow>Datenbasis</Eyebrow>
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
        <Eyebrow>Abdeckung</Eyebrow>
        <p className="mt-1.5 mb-0 text-[13.5px] text-ink-dim">{why.coverage}</p>
        <p className="mt-1.5 mb-0 text-[12.5px] text-muted">
          Query:{" "}
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

export function Pill({ tone, children }: { tone: Tone; children: ReactNode }) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-px text-xs font-medium whitespace-nowrap ${PILL[tone]}`}
    >
      <span
        aria-hidden="true"
        className={`size-1.5 rounded-full ${DOT[tone]}`}
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
