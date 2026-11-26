import { afterEach, describe, expect, it, vi } from "vitest";
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
