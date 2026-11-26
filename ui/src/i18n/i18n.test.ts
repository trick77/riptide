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
    typical: 6.5,
    since: "2026-10-02T09:00:00Z",
    lower_bound: false,
    week: {
      kw: 45,
      start: "2026-11-02T00:00:00Z",
      end: "2026-11-09T00:00:00Z",
    },
    lost: false,
    names: ["feature"],
    held: false,
    two_sprints: false,
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
          within_unit: "working_days",
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
          unit({ two_sprints: true }),
          unit({ lost: true }),
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
    lost: false,
    names: [],
    held: false,
    two_sprints: false,
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
      within_unit: null,
      ...o,
    },
    at: "2026-11-09T00:00:00Z",
    was: null,
    facts: "prs",
    value_unit: "count",
    names_kind: "",
    ...c,
  });
  // The span's unit is the backend's (within_unit): the page words it, in
  // the case the sentence needs.
  it("prints the team's spans", () => {
    const fast = card("fast_approval", { within: 7, within_unit: "minutes" });
    expect(stateUnitText(t, fast, unit(), "PROD")).toBe(
      "3 grosse PRs innert 7 Minuten freigegeben, ohne Kommentar davor",
    );
    expect(thresholdText(t, fast, "PROD")).toBe(
      "Gemeldet ab 3 PRs über 400 Zeilen, innert 7 Minuten freigegeben.",
    );
    const late = card("late_decline", {
      within: 12,
      within_unit: "working_days",
    });
    expect(stateUnitText(t, late, unit(), "PROD")).toBe(
      "3 PRs nach mehr als 12 Arbeitstagen abgelehnt ()",
    );
    expect(
      thresholdText(
        t,
        card("branch_open", {
          value: 14,
          within: 1,
          within_unit: "working_days",
        }),
        "PROD",
      ),
    ).toBe(
      "Gemeldet ab einem Branch ohne PR, an dem seit mehr als 14 Tagen gearbeitet wird, zuletzt vor höchstens 1 Arbeitstag gepusht.",
    );
    expect(
      thresholdText(
        t,
        card("branch_failing", {
          value: 5,
          within: 2,
          within_unit: "working_days",
        }),
        "PROD",
      ),
    ).toBe(
      "Gemeldet ab 5 fehlgeschlagenen Builds in Folge über mindestens 2 Arbeitstage.",
    );
    // A span in calendar days, as the backend says.
    const silent = card("builds_silent", {
      value: 10,
      min: 3,
      within: 28,
      within_unit: "days",
    });
    expect(stateUnitText(t, silent, unit({ count: 12 }), "PROD")).toBe(
      "3 PRs gemerged, kein Build gemeldet; in den 28 Tagen davor 12 Builds",
    );
  });
  // A configured one is singular, in the second threshold too.
  it("counts a threshold of one in the singular", () => {
    expect(
      thresholdText(
        t,
        card("branch_silent", { value: 14, min: 1, unit: "days" }),
        "PROD",
      ),
    ).toBe(
      "Gemeldet ab 1 Branch ohne PR pro Repo, seit mehr als 14 Tagen nicht gepusht.",
    );
    expect(
      thresholdText(t, card("bot_loop", { value: 1, min: null }), "PROD"),
    ).toBe("Gemeldet ab 1 fehlgeschlagenen Build in Folge.");
    expect(
      thresholdText(
        t,
        card("approved_waiting", { value: 1, min: 1, unit: "working_days" }),
        "PROD",
      ),
    ).toBe(
      "Gemeldet ab 1 PR pro Repo mit mehr als 1 Arbeitstag Wartezeit auf den Merge.",
    );
    expect(
      thresholdText(
        t,
        card("merge_pause", { value: 0.2, min: 3, unit: "share" }),
        "PROD",
      ),
    ).toBe(
      "Gemeldet, wenn eine Woche höchstens 20 % der typischen Merges hat, während am Ende der Woche 3 freigegebene PRs oder mehr offen waren.",
    );
  });
  // The unit that held a lasting state two sprints says so: one such unit
  // turns the whole card red.
  it("marks the unit held two sprints", () => {
    const c = card("approved_waiting", { value: 1, min: 2 });
    expect(
      stateUnitText(
        t,
        c,
        unit({ value: 12, count: 2, two_sprints: true }),
        "PROD",
      ),
    ).toBe(
      "2 freigegebene PRs warten auf den Merge, der älteste seit 12 Arbeitstagen (seit zwei Sprints oder länger)",
    );
  });
  // A week Bitbucket lost part of: the typical value (the backend's,
  // scaled) says it leaves those days out.
  it("names the days a merge pause's week lost", () => {
    const c = card("merge_pause", { value: 0.2, min: 3, unit: "share" });
    const week = {
      kw: 45,
      start: "2026-11-02T00:00:00Z",
      end: "2026-11-09T00:00:00Z",
    };
    const u = unit({
      value: 4,
      count: 0,
      typical: 4.8,
      week,
      names: ["kasse/kasse-api"],
    });
    expect(stateUnitText(t, c, { ...u, lost: true }, "PROD")).toBe(
      "KW 45: 0 PRs gemerged statt typisch 4.8 (ohne die Tage, an denen Bitbucket keine Daten lieferte), während am Ende der Woche 4 freigegebene PRs offen waren (kasse/kasse-api)",
    );
    expect(stateUnitText(t, c, u, "PROD")).toBe(
      "KW 45: 0 PRs gemerged statt typisch 4.8, während am Ende der Woche 4 freigegebene PRs offen waren (kasse/kasse-api)",
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
    ).toBe("1 Release nach PROD keinem Produkt zuordenbar");
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
