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
