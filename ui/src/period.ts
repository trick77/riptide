// The window the page shows: whole calendar weeks (Monday to Monday, local
// time), the current one running to now. Charts bucket per week.

export type Week = { start: Date; end: Date; kw: number };

export type Period = {
  from: Date;
  to: Date;
  weeks: Week[];
  // Length in weeks, the current one counted as far as it has run: the
  // divisor of a per-week rate.
  span: number;
};

export const WINDOWS = [4, 12, 26] as const;
export type WindowWeeks = (typeof WINDOWS)[number];

const DAY = 86_400_000;

function addDays(d: Date, n: number): Date {
  const r = new Date(d);
  r.setDate(r.getDate() + n);
  return r;
}

// The Monday 00:00 of d's week, local time.
export function weekStart(d: Date): Date {
  const r = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const back = (r.getDay() + 6) % 7;
  return addDays(r, -back);
}

// ISO 8601 week number.
export function isoWeek(d: Date): number {
  const t = new Date(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()));
  const day = t.getUTCDay() || 7;
  t.setUTCDate(t.getUTCDate() + 4 - day);
  const yearStart = Date.UTC(t.getUTCFullYear(), 0, 1);
  return Math.ceil(((t.getTime() - yearStart) / DAY + 1) / 7);
}

export function period(weeks: number, now: Date): Period {
  const first = addDays(weekStart(now), -7 * (weeks - 1));
  const list: Week[] = [];
  for (let i = 0; i < weeks; i++) {
    const start = addDays(first, 7 * i);
    list.push({ start, end: addDays(start, 7), kw: isoWeek(start) });
  }
  return {
    from: first,
    to: now,
    weeks: list,
    span: (now.getTime() - first.getTime()) / (7 * DAY),
  };
}

// The index of the week t falls in, or -1 outside the period.
export function weekIndex(p: Period, t: string | Date): number {
  const ms = typeof t === "string" ? Date.parse(t) : t.getTime();
  if (ms < p.from.getTime() || ms >= p.to.getTime()) return -1;
  for (let i = 0; i < p.weeks.length; i++) {
    if (ms < p.weeks[i].end.getTime()) return i;
  }
  return -1;
}

export function inPeriod(p: Period, t: string | null): boolean {
  return t != null && weekIndex(p, t) >= 0;
}
