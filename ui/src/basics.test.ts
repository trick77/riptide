import { describe, expect, it } from "vitest";
import {
  ciHours,
  ciWord,
  dateTime,
  dayMonth,
  dur,
  durParts,
  fullDate,
  money,
  num,
  partPct,
  pct,
  shortImage,
  shortSha,
} from "./format";
import { bandHeights, H, labelYs, layout, MIN_BAND } from "./sankey-layout";

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
  it("writes a CI's word with a capital", () => {
    expect(ciWord("UNSTABLE")).toBe("Unstable");
    expect(ciWord("NOT_BUILT")).toBe("Not built");
    expect(ciWord("None")).toBe("None");
    expect(ciWord("PipelineRunTimeout")).toBe("PipelineRunTimeout");
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

  it("never prints a part that is there as 0", () => {
    expect(ciHours(36)).toBe("< 0.1 h");
    expect(ciHours(0)).toBe("0.0 h");
    expect(ciHours(5400)).toBe("1.5 h");
    expect(partPct(0.001)).toBe("< 1 %");
    expect(partPct(0)).toBe("0 %");
    expect(partPct(0.36)).toBe("36 %");
  });
});

describe("sankey layout", () => {
  const sum = (xs: number[]) => xs.reduce((a, x) => a + x, 0);

  it("keeps thin bands visible and the rest in proportion", () => {
    const hs = bandHeights([1, 1000, 3000], 200);
    expect(hs[0]).toBe(MIN_BAND);
    expect(hs[2] / hs[1]).toBeCloseTo(3);
    expect(sum(hs)).toBeCloseTo(200);
  });

  it("shrinks the minimum when thin bands would not fit", () => {
    const values = [...Array.from({ length: 150 }, () => 1), 1e6];
    const hs = bandHeights(values, 200);
    expect(hs.every((h) => h > 0)).toBe(true);
    expect(hs[150]).toBeGreaterThan(hs[0]);
    expect(sum(hs)).toBeLessThanOrEqual(200 + 1e-9);
  });

  it("keeps labels inside the chart, spread apart", () => {
    const ys = labelYs([0, 1, 2, 3, 400, 401, 402, 403, 404, 405, 406, 407]);
    expect(Math.min(...ys)).toBeGreaterThanOrEqual(12);
    expect(Math.max(...ys)).toBeLessThanOrEqual(H - 22);
    ys.slice(1).forEach((y, i) => expect(y - ys[i]).toBeGreaterThan(20));
  });

  it("stacks the bands within their nodes", () => {
    const g = layout(
      ["a", "b"],
      ["x"],
      [
        { from: "a", to: "x", seconds: 10 },
        { from: "b", to: "x", seconds: 30 },
        { from: "a", to: "gone", seconds: 5 },
      ],
    );
    expect(g.bands).toHaveLength(2);
    const [ax, bx] = g.bands;
    expect(bx.ty).toBeCloseTo(ax.ty + ax.h);
    expect(g.right.get("x")!.h).toBeCloseTo(ax.h + bx.h);
  });
});
