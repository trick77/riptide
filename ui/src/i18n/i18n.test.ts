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
  const honest = ["untraced", "unattributed", "no_prod_stage", "unmeasurable"];
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
    threshold: honest.includes(key)
      ? null
      : {
          key: "some_key",
          value: 2,
          unit: "days",
          min_key: "some_min",
          min: 5,
          within_key: "some_span",
          within: 10,
        },
    at: "2026-11-09T00:00:00Z",
    was: key === "unmeasurable" ? "no_release" : null,
    facts: "prs",
    value_unit: "count",
    names_kind: "",
  });
  // Every nullable field null, as the backend sends a unit that rests on
  // less: nothing raw shows.
  const sparse = unit({
    count: null,
    of: null,
    typical: null,
    since: null,
    week: null,
    names: [],
    reason: null,
    ci_seconds: null,
    query: null,
  });
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
      for (const s of [
        stateUnitText(t, c, sparse, "PROD"),
        stateUnitText(t, { ...c, threshold: null }, sparse, "PROD"),
      ])
        expect([key, /\{\{|undefined|null|NaN/.test(s)]).toEqual([key, false]);
    }
  });
});

// The configured spans print, not words standing for them; one case or
// one day reads singular; an unknown code shows as sent, never as a key.
describe("state wording", () => {
  const t = i18n.getFixedT("de");
  const unit = (o: Partial<StateUnit> = {}): StateUnit => ({
    name: "kasse/kasse-api",
    project: "kasse",
    value: 3,
    count: 3,
    of: null,
    typical: null,
    since: null,
    lower_bound: false,
    week: null,
    names: [],
    held: false,
    reason: null,
    ci_seconds: null,
    query: null,
    measure: null,
    ...o,
  });
  const card = (
    key: StateCard["key"],
    o: Partial<NonNullable<StateCard["threshold"]>> = {},
    c: Partial<StateCard> = {},
  ): StateCard => ({
    key,
    unit: "repo",
    count: 1,
    judged: 2,
    unjudged: 0,
    idle: 0,
    worst: [],
    more: 0,
    units: [],
    since: null,
    since_lower_bound: false,
    threshold: {
      key: "k",
      value: 3,
      unit: "count",
      min_key: null,
      min: 400,
      within_key: null,
      within: null,
      ...o,
    },
    at: "2026-11-09T00:00:00Z",
    was: null,
    facts: "prs",
    value_unit: "count",
    names_kind: "",
    ...c,
  });
  it("prints the team's spans", () => {
    const fast = card("fast_approval", { within: 7 });
    expect(stateUnitText(t, fast, unit(), "PROD")).toBe(
      "3 grosse PRs innert 7 Minuten freigegeben, ohne Kommentar davor",
    );
    expect(thresholdText(t, fast, "PROD")).toBe(
      "Gemeldet ab 3 PRs über 400 Zeilen, innert 7 Minuten freigegeben.",
    );
    expect(
      stateUnitText(t, card("late_decline", { within: 12 }), unit(), "PROD"),
    ).toBe("3 PRs nach mehr als 12 Arbeitstagen abgelehnt ()");
    expect(
      thresholdText(t, card("branch_open", { value: 14, within: 1 }), "PROD"),
    ).toBe(
      "Gemeldet ab einem Branch ohne PR, zuerst vor mehr als 14 Tagen gepusht und zuletzt vor höchstens 1 Arbeitstag.",
    );
  });
  it("counts one in the singular", () => {
    const failing = card("deploy_failing", {}, { unit: "app" });
    expect(
      stateUnitText(t, failing, unit({ value: 1, count: 1 }), "PROD"),
    ).toBe("seit 1 Arbeitstag kein erfolgreiches Deploy, 1 Versuch");
    expect(
      stateUnitText(t, failing, unit({ value: 2.5, count: 4 }), "PROD"),
    ).toBe("seit 2.5 Arbeitstagen kein erfolgreiches Deploy, 4 Versuche");
    expect(
      stateUnitText(
        t,
        card("unattributed", {}, { unit: "team" }),
        unit({ value: 1, count: 1 }),
        "PROD",
      ),
    ).toBe("1 Release nach PROD keinem Projekt zuordenbar");
  });
  it("names a resolved state by its title", () => {
    const title = signalTitle(
      t,
      {
        kind: "resolved",
        metric: "state:unapproved",
        repo: null,
        run_kind: null,
        source: "bitbucket",
        facts: [{ label: "state_units", unit: "count", value: 1 }],
      } as unknown as Signal,
      "PROD",
    );
    expect(title).toContain("Ohne Freigabe gemerged");
    expect(title).not.toContain("genannt");
  });
  it("shows an unknown code as sent", () => {
    const untraced = card("untraced", {}, { threshold: null });
    expect(
      stateUnitText(
        t,
        untraced,
        unit({ value: 1, count: 1, of: 3, reason: "future_reason" as never }),
        "PROD",
      ),
    ).toBe("1 von 3 gemergten PRs nicht zuordenbar: future_reason");
    expect(thresholdText(t, untraced, "PROD")).toBe("");
    const title = signalTitle(
      t,
      {
        kind: "state",
        state: card("future_state" as never, {}, { unit: "pipeline" as never }),
        facts: [],
      } as unknown as Signal,
      "PROD",
    );
    expect(title.includes("overview.")).toBe(false);
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
