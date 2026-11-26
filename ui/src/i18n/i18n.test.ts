import { afterEach, describe, expect, it, vi } from "vitest";
import type { Signal, StateCard, StateUnit } from "../api";
import { signalTitle, stateUnitText, thresholdText } from "../Assessment";
import { dur, num } from "../format";
import { de } from "./de";
import { en } from "./en";
import i18n, {
  browserLanguage,
  initialLanguage,
  setLanguage,
  storedLanguage,
} from "./index";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("language", () => {
  it("reads German for every de-* browser locale", () => {
    for (const l of ["de", "de-CH", "de-DE", "de-AT", "DE-ch"]) {
      expect(browserLanguage(l)).toBe("de");
    }
  });
  it("reads English for everything else", () => {
    for (const l of ["en-US", "fr-CH", "it-CH", "rm", ""]) {
      expect(browserLanguage(l)).toBe("en");
    }
  });

  it("lets a stored choice win over the browser", () => {
    vi.spyOn(navigator, "language", "get").mockReturnValue("de-CH");
    expect(initialLanguage()).toBe("de");
    localStorage.setItem("lang", "en");
    expect(initialLanguage()).toBe("en");
  });

  it("ignores junk and blocked storage", () => {
    localStorage.setItem("lang", "fr");
    expect(storedLanguage()).toBeNull();
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(storedLanguage()).toBeNull();
  });

  it("switches, keeps the choice and tags the page", () => {
    setLanguage("en");
    expect(i18n.language).toBe("en");
    expect(localStorage.getItem("lang")).toBe("en");
    expect(document.documentElement.lang).toBe("en");
    setLanguage("de");
    expect(localStorage.getItem("lang")).toBe("de");
    expect(document.documentElement.lang).toBe("de");
  });

  it("still switches when storage is blocked", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    setLanguage("en");
    expect(i18n.language).toBe("en");
  });

  it("follows a switch made in another tab", () => {
    setLanguage("en");
    localStorage.setItem("lang", "de");
    window.dispatchEvent(new StorageEvent("storage", { key: "lang" }));
    expect(i18n.language).toBe("de");
    localStorage.setItem("lang", "en");
    window.dispatchEvent(new StorageEvent("storage", { key: "other" }));
    expect(i18n.language).toBe("de");
  });

  it("translates the unit words, never the number format", () => {
    expect(dur(8.1 * 86400)).toBe("8.1 Tage");
    setLanguage("en");
    expect(dur(8.1 * 86400)).toBe("8.1 days");
    expect(dur(2.5 * 3600)).toBe("2.5 h");
  });

  it("writes a count in the text Swiss in both languages", () => {
    const swiss = `${num(1234)} PRs `;
    expect(swiss).not.toBe("1234 PRs ");
    setLanguage("en");
    expect(i18n.t("open.unknown", { count: 1234 })).toMatch(
      new RegExp(`^${swiss}`),
    );
    setLanguage("de");
    expect(i18n.t("open.unknown", { count: 1234 })).toMatch(
      new RegExp(`^${swiss}`),
    );
  });
});

// Every state's card in both languages: its title, each unit's line (both
// of first_review_wait's measures, held, a build kind), its threshold. A
// placeholder the page does not fill would show raw, and only on the one
// state that fires in production.
describe("state cards", () => {
  const keys = Object.entries(en.overview.assess.state)
    .filter(([, v]) => typeof v === "object" && "title" in v)
    .map(([k]) => k as StateCard["key"]);
  const unit = (o: Partial<StateUnit> = {}): StateUnit => ({
    name: "kasse/kasse-api",
    project: "kasse",
    value: 3,
    count: 2,
    of: 9,
    typical: 7.5,
    since: "2026-10-02T09:00:00Z",
    lower_bound: false,
    week: {
      kw: 45,
      start: "2026-11-02T00:00:00Z",
      end: "2026-11-09T00:00:00Z",
    },
    names: ["feature"],
    held: false,
    reason: "in_silence",
    ci_seconds: 3600,
    query: "kasse/kasse-api",
    measure: null,
    ...o,
  });
  const card = (key: StateCard["key"]): StateCard => ({
    key,
    unit: "repo",
    count: 1,
    judged: 4,
    unjudged: 0,
    idle: 0,
    worst: [unit()],
    more: 0,
    units: [unit()],
    since: null,
    since_lower_bound: false,
    threshold: {
      key: key === "untraced" ? "" : "some_key",
      value: 2,
      unit: "days",
      min_key: "some_min",
      min: 5,
    },
    at: "2026-11-09T00:00:00Z",
    was: key === "unmeasurable" ? "no_release" : null,
    facts: "prs",
  });
  const honest = ["untraced", "unattributed", "no_prod_stage", "unmeasurable"];
  it.each(["de", "en"] as const)("fill every placeholder in %s", (lng) => {
    const t = i18n.getFixedT(lng);
    expect(keys.length).toBeGreaterThan(20);
    for (const key of keys) {
      const c = card(key);
      const signal = {
        kind: "state",
        state: c,
        facts: [],
      } as unknown as Signal;
      const texts = [
        signalTitle(t, signal, "PROD"),
        ...[
          unit(),
          unit({ held: true }),
          unit({ measure: "oldest" }),
          unit({ measure: "typical" }),
        ].map((u) => stateUnitText(t, c, u, "PROD")),
      ];
      const threshold = thresholdText(t, c, "PROD");
      if (!honest.includes(key)) texts.push(threshold);
      for (const s of texts) {
        expect([key, s.includes("{{")]).toEqual([key, false]);
        if (key !== "unmeasurable")
          expect([key, s.trim()]).not.toEqual([key, ""]);
      }
    }
  });
});

// leaves flattens a catalog to dotted key -> string.
function leaves(o: object, prefix = ""): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(o)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (typeof v === "string") out[key] = v;
    else Object.assign(out, leaves(v as object, key));
  }
  return out;
}

// A placeholder with its format ({{count, num}}): a count formatted in one
// language only would read 1234 in the other.
const placeholders = (s: string) =>
  [...s.matchAll(/\{\{(\w+(?:, *\w+)?)\}\}/g)].map((m) => m[1]).sort();

describe("catalogs", () => {
  const enLeaves = leaves(en);
  const deLeaves = leaves(de);

  it("have the same keys", () => {
    expect(Object.keys(deLeaves).sort()).toEqual(Object.keys(enLeaves).sort());
  });

  // A dropped {{prod}} renders the raw placeholder, and only in the one
  // language nobody looked at.
  it("carry the same placeholders per key", () => {
    for (const [k, v] of Object.entries(enLeaves)) {
      expect([k, placeholders(deLeaves[k])]).toEqual([k, placeholders(v)]);
    }
  });

  it("write Swiss German: double s, never eszett", () => {
    for (const [k, v] of Object.entries(deLeaves)) {
      expect([k, v.includes("ß")]).toEqual([k, false]);
    }
  });

  // A Denglish participle ends in -ed, never -t: gemerged, as in
  // ../noergler (user, 2026-11-15). The adjective before a noun is the
  // page's word for the PR counts: "gemergte PRs" (owner's decision,
  // 2026-11).
  it("write gemerged, never gemergt", () => {
    for (const [k, v] of Object.entries(deLeaves)) {
      expect([k, /gemergt\b/i.exec(v)?.[0] ?? null]).toEqual([k, null]);
    }
  });
});
