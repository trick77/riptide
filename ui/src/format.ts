// Number, duration and date formatting, Swiss conventions (de-CH: 1’234.5,
// 24.11.) in both UI languages (user, 2026-11-15): only the unit words
// follow the language.

import i18n from "./i18n";

const LOCALE = "de-CH";

export function num(x: number, digits = 0): string {
  return x.toLocaleString(LOCALE, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  });
}

export function pct(x: number | null): string {
  return x == null ? "–" : `${num(x * 100, 0)} %`;
}

// A part of a whole: as pct, but a part that is there never reads 0 %.
export function partPct(x: number): string {
  return x > 0 && x < 0.005 ? "< 1 %" : pct(x);
}

// CI time in hours as the build section shows it, to a tenth; time that is
// there never reads 0.0 h.
export function ciHours(seconds: number): string {
  return seconds > 0 && seconds < 180
    ? i18n.t("common.hours", { n: `< ${num(0.1, 1)}` })
    : i18n.t("common.hours", { n: num(seconds / 3600, 1) });
}

export function money(x: number): string {
  return `$${num(x, 2)}`;
}

// A duration in seconds, in the largest unit that keeps it readable:
// "45 s", "12 min", "3.4 h", "8.1 Tage" / "8.1 days".
export function dur(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return "–";
  const s = Math.max(0, seconds);
  if (s < 60) return i18n.t("units.s", { n: num(Math.round(s)) });
  if (s < 3600) return i18n.t("units.min", { n: num(Math.round(s / 60)) });
  if (s < 86400) return i18n.t("units.h", { n: num(s / 3600, 1) });
  return i18n.t("units.days", { n: num(s / 86400, 1) });
}

// dur split into value and unit, for a tile's big number.
export function durParts(seconds: number | null): {
  value: string;
  unit: string;
} {
  const t = dur(seconds);
  const i = t.lastIndexOf(" ");
  return i < 0
    ? { value: t, unit: "" }
    : { value: t.slice(0, i), unit: t.slice(i + 1) };
}

export function dayMonth(d: Date, timeZone?: string): string {
  return d.toLocaleDateString(LOCALE, {
    day: "2-digit",
    month: "2-digit",
    timeZone,
  });
}

export function clock(iso: string): string {
  return new Date(iso).toLocaleTimeString(LOCALE, {
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function dateTime(iso: string): string {
  return `${dayMonth(new Date(iso))} ${clock(iso)}`;
}

export function fullDate(d: Date): string {
  return d.toLocaleDateString(LOCALE, {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  });
}

// A CI's status word as a pill shows it: Jenkins' shouting "UNSTABLE" ->
// "Unstable", "NOT_BUILT" -> "Not built". Any other word (Tekton's
// "PipelineRunTimeout") stays as the CI wrote it.
export function ciWord(s: string): string {
  if (!/^[A-Z_]+$/.test(s)) return s;
  const w = s.replace(/_/g, " ").toLowerCase();
  return w.charAt(0).toUpperCase() + w.slice(1);
}

// A stage as the team says it: INTG, SYST, PROD, whatever case Argo CD
// reports it in.
export function stage(name: string): string {
  return name.toUpperCase();
}

export function shortSha(sha: string | null): string {
  return sha ? sha.slice(0, 8) : "–";
}

// An image reference without its registry: "reg.example/team/app:1.2" ->
// "app:1.2". The tag is what tells releases apart.
export function shortImage(ref: string): string {
  const i = ref.lastIndexOf("/");
  return i < 0 ? ref : ref.slice(i + 1);
}
