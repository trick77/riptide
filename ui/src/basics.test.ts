import { describe, expect, it } from "vitest";
import {
  dateTime,
  dayMonth,
  dur,
  durParts,
  fullDate,
  money,
  num,
  pct,
  shortImage,
  shortSha,
} from "./format";
import { isoWeek, period, weekIndex, weekStart, inPeriod } from "./period";
import { between, median, percentile, sum } from "./stats";

describe("percentile", () => {
  // Postgres: SELECT percentile_cont(p) WITHIN GROUP (ORDER BY x) FROM
  // unnest('{1,2,3,4,10}'::float8[]) x;  0.5 -> 3, 0.9 -> 7.6, 0.25 -> 2
  it("interpolates like percentile_cont", () => {
    const v = [10, 1, 4, 2, 3];
    expect(percentile(v, 0.5)).toBe(3);
    expect(percentile(v, 0.9)).toBeCloseTo(7.6);
    expect(percentile(v, 0.25)).toBe(2);
    expect(median([1, 2])).toBe(1.5);
    expect(percentile([5], 0.9)).toBe(5);
    expect(percentile([], 0.5)).toBeNull();
  });
  it("leaves its input unsorted", () => {
    const v = [3, 1, 2];
    percentile(v, 0.5);
    expect(v).toEqual([3, 1, 2]);
  });
  it("sums and measures intervals", () => {
    expect(sum([1, 2, 3])).toBe(6);
    expect(between("2026-01-01T00:00:00Z", "2026-01-01T01:00:00Z")).toBe(3600);
    expect(between(null, "2026-01-01T01:00:00Z")).toBeNull();
  });
});

describe("format", () => {
  it("uses Swiss conventions", () => {
    expect(num(1234.5, 1)).toBe("1'234.5");
    expect(pct(0.834)).toBe("83 %");
    expect(pct(null)).toBe("–");
    expect(money(12.345)).toBe("$12.35");
  });
  it("picks a readable unit", () => {
    expect(dur(42)).toBe("42 s");
    expect(dur(319)).toBe("5 min");
    expect(dur(2.5 * 3600)).toBe("2.5 h");
    expect(dur(8.1 * 86400)).toBe("8.1 Tage");
    expect(dur(null)).toBe("–");
    expect(dur(-5)).toBe("0 s");
    expect(durParts(8.1 * 86400)).toEqual({ value: "8.1", unit: "Tage" });
    expect(durParts(null)).toEqual({ value: "–", unit: "" });
  });
  it("shortens", () => {
    expect(shortSha("0123456789abcdef")).toBe("01234567");
    expect(shortSha(null)).toBe("–");
    expect(shortImage("reg.example/team/kasse-api:1.2")).toBe("kasse-api:1.2");
    expect(shortImage("kasse-api:1.2")).toBe("kasse-api:1.2");
  });
  it("formats dates locally", () => {
    expect(dayMonth(new Date("2026-11-12T12:00:00+01:00"))).toBe("12.11.");
    expect(dateTime("2026-11-12T15:04:00Z")).toBe("12.11. 16:04");
    expect(fullDate(new Date("2026-11-13T12:00:00+01:00"))).toBe("13.11.2026");
  });
});

describe("period", () => {
  const now = new Date("2026-11-12T12:00:00+01:00"); // Thursday, KW 46
  it("is whole weeks, the current one to now", () => {
    const p = period(4, now);
    expect(p.weeks.map((w) => w.kw)).toEqual([43, 44, 45, 46]);
    expect(p.from).toEqual(new Date("2026-10-19T00:00:00+02:00")); // summer time still
    expect(p.to).toBe(now);
    // 3.5 weeks, plus the hour the switch back from summer time added
    expect(p.span).toBeCloseTo(3.5 + 1 / (7 * 24), 6);
    expect(weekStart(now)).toEqual(new Date("2026-11-09T00:00:00+01:00"));
  });
  it("buckets instants", () => {
    const p = period(4, now);
    expect(weekIndex(p, "2026-10-18T21:59:59Z")).toBe(-1); // Sunday 23:59 local
    expect(weekIndex(p, "2026-10-18T22:00:00Z")).toBe(0);
    expect(weekIndex(p, "2026-11-09T00:30:00+01:00")).toBe(3);
    expect(weekIndex(p, new Date("2026-11-12T12:00:01+01:00"))).toBe(-1);
    expect(inPeriod(p, null)).toBe(false);
    expect(inPeriod(p, "2026-11-01T00:00:00Z")).toBe(true);
  });
  it("numbers weeks per ISO 8601", () => {
    expect(isoWeek(new Date("2026-01-01T12:00:00"))).toBe(1);
    expect(isoWeek(new Date("2027-01-01T12:00:00"))).toBe(53);
  });
});
