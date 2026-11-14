// Number, duration and date formatting, Swiss German conventions (de-CH:
// 1’234.5, 24.11.).

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

export function money(x: number): string {
  return `$${num(x, 2)}`;
}

// A duration in seconds, in the largest unit that keeps it readable:
// "45 s", "12 min", "3,4 h", "8,1 Tage".
export function dur(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return "–";
  const s = Math.max(0, seconds);
  if (s < 60) return `${num(Math.round(s))} s`;
  if (s < 3600) return `${num(Math.round(s / 60))} min`;
  if (s < 86400) return `${num(s / 3600, 1)} h`;
  return `${num(s / 86400, 1)} Tage`;
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

export function dayMonth(d: Date): string {
  return d.toLocaleDateString(LOCALE, { day: "2-digit", month: "2-digit" });
}

export function dateTime(iso: string): string {
  const d = new Date(iso);
  return `${dayMonth(d)} ${d.toLocaleTimeString(LOCALE, { hour: "2-digit", minute: "2-digit" })}`;
}

export function fullDate(d: Date): string {
  return d.toLocaleDateString(LOCALE, {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  });
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
