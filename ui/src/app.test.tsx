import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import type {
  AnalysisStatus,
  Facts,
  FlowOutcome,
  Metrics,
  RunKind,
} from "./api";
import { Build } from "./Build";
import { DayCalendar } from "./calendar";
import { Costs } from "./Costs";
import { Flow } from "./Flow";
import { stepLook } from "./heatmap";
import { initialLanguage } from "./i18n";
import { keepLabels } from "./marimekko";
import { parseState, stateQuery } from "./state";
import { Strip } from "./strip";
import {
  ago,
  analysedStatus,
  analysisStatus,
  assessment,
  change,
  deployFact,
  facts,
  metrics,
  noSpread,
  prFact,
  STAGE_BOUNDS,
  spread,
  undirected,
} from "./test/fixtures";

type Answer = Metrics | Facts | AnalysisStatus | Response;

// The sample team's answers: per product and search as the backend
// would compute them.
function sample(url: URL): Answer {
  const weeks = Number(url.searchParams.get("weeks") ?? 4);
  const product = url.searchParams.get("product");
  if (url.pathname === "/api/facts") {
    const q = url.searchParams.get("q");
    if (q === "portal")
      return facts({
        prs: { count: 1, rows: [facts().prs.rows[2]] },
        deploys: { count: 0, rows: [] },
        runs: { count: 0, rows: [] },
      });
    if (q === "kasse-api:1.1")
      return facts({
        deploys: { count: 5, rows: facts().deploys.rows },
      });
    return facts();
  }
  const m = metrics(weeks);
  if (product === "portal") {
    const empty = metrics(weeks);
    return {
      ...m,
      product: "portal",
      delivery: {
        ...m.delivery,
        frequency: {
          ...m.delivery.frequency,
          releases: 0,
          syncs: 0,
          first_seen: 0,
          resyncs: 0,
          per_week: 0,
          succeeded: 0,
          failed: 0,
          finished: 0,
          success_rate: null,
        },
      },
      build: { ...empty.build, kinds: [] },
      costs: { ...empty.costs, rows: [] },
    };
  }
  return m;
}

function mockFetch(
  answer: (url: URL, init?: RequestInit) => Answer = sample,
  teams = ["team-a", "team-b"],
) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (raw: string, init?: RequestInit) => {
      calls.push(init?.method === "POST" ? `POST ${raw}` : raw);
      const url = new URL(raw, "http://riptide.test");
      if (url.pathname === "/api/teams")
        return new Response(JSON.stringify({ teams }));
      let a = answer(url, init);
      // The assessment answers on its own unless the test says otherwise:
      // most answers send the metrics for every path but the facts.
      if (
        url.pathname === "/api/analysis" &&
        !(a instanceof Response) &&
        !("assessment" in a)
      )
        a = analysisStatus();
      return a instanceof Response ? a : new Response(JSON.stringify(a));
    }),
  );
  return calls;
}

const metricsCalls = (calls: string[]) =>
  calls.filter((c) => c.startsWith("/api/metrics"));
const factsCalls = (calls: string[]) =>
  calls.filter((c) => c.startsWith("/api/facts"));

beforeEach(() => {
  window.history.replaceState(null, "", "/");
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("App", () => {
  it("loads the first team and shows every section", async () => {
    const calls = mockFetch();
    render(<App />);
    // The facts come last: they ask for the product the metrics resolved.
    await screen.findByRole("tablist", { name: "Faktentabelle" });
    for (const h of [
      "Einschätzung",
      "Gegen die Vorperiode",
      "Throughput und Instability",
      "Wo wartet eine Änderung?",
      "Wie schnell und verlässlich ist das Feedback?",
      "Wie gleichmässig kommt Code nach PROD?",
      "Woran arbeitet das Team?",
      "Was kosten die KI-Reviews?",
      "Welche Frameworks Riptide abdeckt",
    ]) {
      expect(screen.getByRole("heading", { name: h })).toBeInTheDocument();
    }
    // The sections in a change's order, builds before rollout, and the nav
    // in the same order as the page.
    const sections = [...document.querySelectorAll("section[id]")].map(
      (s) => s.id,
    );
    expect(sections).toEqual([
      "ueberblick",
      "fluss",
      "build",
      "deploys",
      "arbeitsmix",
      "kosten",
      "daten",
      "frameworks",
    ]);
    const nav = screen.getByRole("navigation", { name: "Bereiche" });
    expect(
      within(nav)
        .getAllByRole("link")
        .map((a) => a.getAttribute("href")),
    ).toEqual(sections.map((id) => `#${id}`));
    // The viewer's zone goes along: the backend cuts the weeks there.
    expect(metricsCalls(calls)[0]).toBe(
      "/api/metrics?team=team-a&weeks=4&tz=Europe%2FZurich",
    );
    expect(factsCalls(calls)[0]).toBe(
      "/api/facts?team=team-a&weeks=4&tz=Europe%2FZurich",
    );
    expect(
      screen.getByText(/Team team-a, alle Produkte · KW 43–46/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Datenstand: Bitbucket 12\.11\./),
    ).toBeInTheDocument();
    expect(screen.getByText("1 Release im Zeitraum")).toBeInTheDocument();
    expect(
      screen.getByText("1 von 4 Syncs fehlgeschlagen"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/1 Erstdeploy ohne Vergleichswert/),
    ).toBeInTheDocument();
    expect(screen.getByText(/Lead Time bis INTG:/)).toBeInTheDocument();
    expect(screen.getAllByText(/wenig belastbar/).length).toBeGreaterThan(0);
    // The DORA names, and the side Riptide cannot measure.
    expect(
      screen.getByText("DORA · Change lead time · Throughput"),
    ).toBeInTheDocument();
    expect(screen.getByText("nicht gemessen")).toBeInTheDocument();
    expect(screen.getByText("typisch 1.0 pro Woche")).toBeInTheDocument();
  });

  it("explains a number on demand, one at a time", async () => {
    mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Einschätzung" });
    const whys = screen.getAllByRole("button", { name: "Was heisst das?" });
    fireEvent.click(whys[0]);
    expect(
      screen.getByRole("region", { name: "Was heisst das? · Deploy-Frequenz" }),
    ).toBeInTheDocument();
    expect(screen.getByText("pro revision")).toBeInTheDocument();
    expect(screen.getByText("Genauer hinschauen, wenn")).toBeInTheDocument();
    for (const [i, title] of [
      [1, "Lead Time bis PROD"],
      [2, "Deploy-Erfolg"],
      [3, "Review-Zeit"],
      [4, "Approval ohne Kommentar"],
      [5, "Innert 24 h gemerged"],
    ] as const) {
      fireEvent.click(whys[i]);
      expect(
        screen.getByRole("region", { name: `Was heisst das? · ${title}` }),
      ).toBeInTheDocument();
      expect(
        screen.getAllByRole("region", { name: /^Was heisst das\?/ }),
      ).toHaveLength(1);
    }
    fireEvent.click(whys[5]);
    expect(
      screen.queryByRole("region", { name: /^Was heisst das\?/ }),
    ).toBeNull();
  });

  it("shows each number against the previous period", async () => {
    mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Einschätzung" });
    expect(screen.getByText("Besser")).toBeInTheDocument();
    expect(screen.getByText("Schlechter")).toBeInTheDocument();
    expect(screen.getAllByText("Unverändert").length).toBeGreaterThan(0);
    expect(screen.getAllByText("ohne Richtung").length).toBeGreaterThan(0);
    expect(
      screen.getAllByText(
        "kein Vergleich: zu wenig Daten in einer der beiden Perioden",
      ).length,
    ).toBeGreaterThan(0);
    expect(
      screen.getByText(
        "kein Vergleich: die Daten der Vorperiode sind nicht vollständig, oder Erstdeploys überwiegen",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/verglichen mit den 4 Wochen davor/),
    ).toBeInTheDocument();
  });

  it("words the assessment with the model's text", async () => {
    const calls = mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") return analysedStatus();
      return metrics();
    });
    render(<App />);
    const panel = await screen.findByTestId("assessment");
    const p = within(panel);
    expect(
      p.getByRole("heading", {
        name: "Ihr liefert öfter – aber Reviews bleiben liegen.",
      }),
    ).toBeInTheDocument();
    // Who worded it is nobody's business (user): no model, no LLM badge.
    expect(panel.textContent).not.toMatch(/LLM|mimo|USD/);
    // The weeks, the stamp and the next scheduled run, in one line.
    const weeks = p.getByTitle("ohne die laufende Woche");
    expect(weeks.textContent).toMatch(
      /^KW \d+–\d+ · \d\d\.\d\d\.–\d\d\.\d\d\.$/,
    );
    expect(weeks.parentElement?.textContent).toMatch(
      /· Einschätzung vom .*, automatisch · nächste: /,
    );
    expect(
      p.getByText(
        "Argo CD meldet seit 6 Tagen nichts: Aussagen dazu ausgelassen.",
      ),
    ).toBeInTheDocument();
    // The focus, the placeholders filled with the backend's numbers.
    expect(p.getByText("Im Fokus")).toBeInTheDocument();
    expect(
      p.getByText("9 weitere aktive Repos ohne Auffälligkeiten"),
    ).toBeInTheDocument();
    expect(p.getByText("Team, alle Produkte")).toBeInTheDocument();
    expect(
      p.getByRole("heading", { name: "kasse/kasse-api" }),
    ).toBeInTheDocument();
    expect(
      p.getByText("5 PRs liegen, und Reviews dauern."),
    ).toBeInTheDocument();
    expect(
      p.getByText("Konstant der langsamste Build mit 24 min."),
    ).toBeInTheDocument();
    expect(p.getByText("schon 2 Mal davor")).toBeInTheDocument();
    expect(p.getByText(/die PRs einzeln durchgehen/)).toBeInTheDocument();
    // Team-wide, each card in its column.
    expect(
      p.getByText("PRs gemerged pro Woche: deutlich besser"),
    ).toBeInTheDocument();
    expect(
      p.getByText("Treiber: kasse/kasse-api trägt 70 % der Zunahme"),
    ).toBeInTheDocument();
    expect(p.getByText("kasse/policy (flaky): erledigt")).toBeInTheDocument();
    // What no wording covers is still spotted.
    expect(p.getByText("Vergessene PRs")).toBeInTheDocument();
    expect(p.getByText("Für die nächste Retro")).toBeInTheDocument();
    // On the focus block and gathered in the retro box.
    expect(p.getAllByText("Warum liegen diese PRs?")).toHaveLength(2);
    expect(
      p.getByRole("button", { name: "Als Markdown kopieren" }),
    ).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(/\{\{|\}\}/);
    fireEvent.click(
      p.getByRole("button", { name: "Fakten zu kasse/kasse-web ansehen →" }),
    );
    expect(window.location.search).toBe("?q=kasse%2Fkasse-web&tab=runs");
    await waitFor(() =>
      expect(factsCalls(calls).at(-1)).toMatch(/&q=kasse%2Fkasse-web$/),
    );
  });

  it("shows the rules' own words without a model", async () => {
    mockFetch();
    render(<App />);
    const panel = await screen.findByTestId("assessment");
    const p = within(panel);
    expect(panel.textContent).not.toMatch(/LLM|Regeln/);
    expect(p.queryByRole("button", { name: /einschätzen/ })).toBeNull();
    // Without a model nothing is scheduled, and nothing ever compared.
    expect(p.queryByText(/nächste: /)).toBeNull();
    expect(panel.textContent).not.toMatch(/Vergleich|erledigt\./);
    expect(
      p.getByText(
        "kasse/kasse-api: PRs stauen sich; bereit bis Approval: deutlich schlechter",
      ),
    ).toBeInTheDocument();
    expect(
      p.getByText("PRs gemerged pro Woche: deutlich besser"),
    ).toBeInTheDocument();
    // Where the CI time goes is the build section's, not an assessment.
    expect(panel.textContent).not.toMatch(/CI-Zeit/);
    // The question to talk about, no retro questions of a model.
    expect(
      p.getByText(
        "64 % Approvals ohne vorherigen Kommentar: Besprechen wir grosse Änderungen ausserhalb von Bitbucket?",
      ),
    ).toBeInTheDocument();
  });

  it("asks for an assessment and shows it running", async () => {
    let asked = false;
    const calls = mockFetch((url, init) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        if (init?.method === "POST") asked = true;
        return analysisStatus({
          state: "none",
          enabled: true,
          running: asked,
        });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    expect(await p.findByRole("button", { name: "läuft …" })).toBeDisabled();
    expect(p.getByText(/läuft, meist unter einer Minute/)).toBeInTheDocument();
    expect(calls.filter((c) => c.startsWith("POST /api/analysis"))).toEqual([
      "POST /api/analysis?team=team-a&weeks=4&tz=Europe%2FZurich",
    ]);
  });

  it("keeps polling a run after a failed poll", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      let polls = 0;
      mockFetch((url) => {
        if (url.pathname === "/api/facts") return facts();
        if (url.pathname === "/api/analysis") {
          polls++;
          if (polls === 1)
            return analysisStatus({
              state: "none",
              enabled: true,
              running: true,
            });
          // A gateway hiccup mid-run, then the finished analysis.
          if (polls === 2) return new Response("bad gateway", { status: 502 });
          return analysedStatus();
        }
        return metrics();
      });
      render(<App />);
      const p = within(await screen.findByTestId("assessment"));
      await p.findByRole("button", { name: "läuft …" });
      // The failed poll plans the next one only once it has failed, an
      // interval later: advance until the third poll came.
      for (let i = 0; i < 8 && polls < 3; i++)
        await vi.advanceTimersByTimeAsync(3100);
      expect(
        await p.findByRole("heading", {
          name: "Ihr liefert öfter – aber Reviews bleiben liegen.",
        }),
      ).toBeInTheDocument();
      expect(polls).toBeGreaterThanOrEqual(3);
    } finally {
      vi.useRealTimers();
    }
  });

  it("switches to English and keeps the choice", async () => {
    mockFetch();
    const { unmount } = render(<App />);
    await screen.findByRole("heading", { name: "Einschätzung" });
    await screen.findByText(": Pay by card");
    // A placeholder the call site names differently stays raw on screen.
    const raw = /\{\{|\}\}/;
    expect(document.body.textContent).not.toMatch(raw);
    fireEvent.click(
      screen.getByRole("button", {
        name: "Sprache: Deutsch. Auf Englisch wechseln",
      }),
    );
    expect(
      await screen.findByRole("heading", { name: "Assessment" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Print/ })).toBeInTheDocument();
    expect(document.body.textContent).not.toMatch(raw);
    expect(localStorage.getItem("lang")).toBe("en");
    // The facts table's words come from the shared search terms.
    expect(screen.getAllByText("Merged").length).toBeGreaterThan(0);
    expect(screen.getByText("Promotion")).toBeInTheDocument();
    unmount();

    // A new page load reads the stored choice, not the browser (en-US
    // here, so the stored "en" is checked against a German browser).
    vi.spyOn(navigator, "language", "get").mockReturnValue("de-CH");
    expect(initialLanguage()).toBe("en");
    render(<App />);
    expect(
      await screen.findByRole("heading", { name: "Assessment" }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Language: English. Switch to German",
      }),
    );
    expect(
      await screen.findByRole("heading", { name: "Einschätzung" }),
    ).toBeInTheDocument();
    expect(localStorage.getItem("lang")).toBe("de");
  });

  it("spells a PR's type and its way to prod", async () => {
    const rows = [
      prFact({ pr_id: 11, title: "a", prod_at: null, to_prod_s: null }),
      prFact({ pr_id: 12, title: "b", change_type: "other" }),
      prFact({ pr_id: 13, title: "c", change_type: null }),
      prFact({ pr_id: 14, title: "d", change_type: "spike" }),
    ];
    mockFetch((url) =>
      url.pathname === "/api/facts"
        ? facts({ prs: { count: rows.length, rows } })
        : metrics(4),
    );
    render(<App />);
    const row = async (title: string) =>
      within((await screen.findByText(title)).closest("tr")!);
    expect((await row("a")).getByText("Feature")).toBeInTheDocument();
    expect((await row("a")).getByText("Nein")).toBeInTheDocument();
    expect((await row("b")).getByText("Anderes")).toBeInTheDocument();
    expect((await row("c")).getAllByText("–").length).toBeGreaterThan(0);
    // a prefix the UI does not know yet shows as the backend sent it
    expect((await row("d")).getByText("spike")).toBeInTheDocument();
  });

  it("says how few, what all wait on, and that a first one has no past", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const a = assessment();
        const l = analysisStatus().layout;
        return analysisStatus({
          enabled: true,
          // Without an earlier assessment nothing can be resolved.
          layout: { ...l, cards: l.cards.filter((c) => c.column !== "done") },
          assessment: {
            ...a,
            compared: false,
            signals: a.signals.map((s) =>
              s.id === "change:merged_per_week"
                ? { ...s, low_sample: true, sample: 7 }
                : s,
            ),
            focus: a.focus.map((f) =>
              f.waiting
                ? {
                    ...f,
                    waiting: { ...f.waiting, first_review: 0, changes: 3 },
                  }
                : f,
            ),
          },
        });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    expect(p.getByText("wenig belastbar · 7 PRs")).toBeInTheDocument();
    expect(p.getByText("alle 3 warten auf Änderungen")).toBeInTheDocument();
    expect(
      p.getByText(/keine frühere Woche eingeschätzt, noch kein Vergleich/),
    ).toBeInTheDocument();
  });

  it("says drafts are drafts, and copies with team and product", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const a = assessment();
        return analysedStatus({
          assessment: {
            ...a,
            focus: a.focus.map((f) =>
              f.waiting
                ? {
                    ...f,
                    waiting: {
                      draft: 2,
                      first_review: 0,
                      review: 0,
                      changes: 0,
                      merge: 0,
                    },
                  }
                : f,
            ),
          },
        });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    expect(p.getByText("alle 2 sind Entwürfe")).toBeInTheDocument();
    fireEvent.click(p.getByRole("button", { name: "Als Markdown kopieren" }));
    expect(writeText).toHaveBeenCalledWith(
      expect.stringMatching(
        /^## Retro-Input riptide\u2002·\u2002team-a\u2002·\u2002Alle Produkte\u2002·\u2002KW /,
      ),
    );
  });

  it("says so when nothing stands out", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const a = assessment();
        return analysisStatus({
          enabled: true,
          assessment: {
            ...a,
            gaps: [],
            signals: [],
            focus: [],
            question: null,
          },
        });
      }
      return metrics(4);
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    // The empty columns in one quiet line, no empty boxes.
    expect(
      p.getByText(
        "nichts bemerkenswert besser · nichts hakt · seit der letzten Einschätzung nichts erledigt.",
      ),
    ).toBeInTheDocument();
    expect(p.queryByText("Im Fokus")).toBeNull();
    expect(p.queryByText("Für die nächste Retro")).toBeNull();
  });

  it("asks for a product, keeps it in the URL", async () => {
    const calls = mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Einschätzung" });
    const products = screen.getByRole("group", { name: "Produkt" });
    fireEvent.click(within(products).getByRole("button", { name: /portal/ }));
    expect(window.location.search).toBe("?product=portal");
    await screen.findByText(/Team team-a, Produkt portal/);
    expect(metricsCalls(calls).at(-1)).toMatch(/&product=portal$/);
    expect(screen.getByText("Argo-Apps")).toBeInTheDocument();
    expect(screen.getByText("keine")).toBeInTheDocument();
    expect(screen.getByText("keine Deploys im Zeitraum")).toBeInTheDocument();
    expect(
      screen.getByText("Keine abgeschlossenen Läufe im Zeitraum."),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Keine noergler-Reviews im Zeitraum."),
    ).toBeInTheDocument();
    expect(screen.getByText("Nach Repo")).toBeInTheDocument();

    fireEvent.click(
      within(products).getByRole("button", { name: /Alle Produkte/ }),
    );
    await screen.findByText(/Team team-a, alle Produkte/);
    expect(screen.getByText(/^\(0 von 1\)/)).toBeInTheDocument();
    // Bots are never counted in: no switch for them.
    expect(screen.queryByRole("button", { name: /Bots/ })).toBeNull();
  });

  it("switches the window and the team, refetching", async () => {
    const calls = mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Einschätzung" });
    fireEvent.click(screen.getByRole("button", { name: "12 W" }));
    await screen.findByText(/KW 35–46/);
    expect(metricsCalls(calls).at(-1)).toMatch(/weeks=12/);
    fireEvent.change(screen.getByLabelText("Team"), {
      target: { value: "team-b" },
    });
    await waitFor(() =>
      expect(metricsCalls(calls).at(-1)).toMatch(/team=team-b/),
    );
    expect(window.location.search).toBe("?team=team-b&w=12");
  });

  it("asks the backend to search the facts, switches tables", async () => {
    const calls = mockFetch();
    render(<App />);
    const tabs = await screen.findByRole("tablist", { name: "Faktentabelle" });
    expect(within(tabs).getByRole("tab", { name: /PRs 3/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    // A key in the title is orange and links to its ticket; one the title
    // does not show is orange too, unlinked without a URL.
    const key = screen.getByRole("link", { name: "KAS-1" });
    expect(key).toHaveAttribute(
      "href",
      "https://jira.example.com/browse/KAS-1",
    );
    expect(key).toHaveAttribute("target", "_blank");
    expect(key.parentElement).toHaveClass("text-accent-strong");
    expect(key.closest("td")).toHaveTextContent("KAS-1: Pay by card");
    for (const k of ["KAS-2", "KAS-3", "KAS-4"]) {
      expect(screen.getByText(k)).toHaveClass("text-accent-strong");
      expect(screen.queryByRole("link", { name: k })).toBeNull();
    }
    expect(screen.getByText("+2")).toBeInTheDocument();
    expect(screen.getByText("Promotion")).toBeInTheDocument();
    expect(screen.getByText("#4")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/Suche nach/), {
      target: { value: "portal" },
    });
    expect(
      await within(tabs).findByRole("tab", { name: /PRs 1/ }),
    ).toBeInTheDocument();
    expect(factsCalls(calls).at(-1)).toMatch(/&q=portal$/);
    expect(screen.getByText("gefiltert nach «portal»")).toBeInTheDocument();
    fireEvent.click(within(tabs).getByRole("tab", { name: /Deploys/ }));
    expect(screen.getByText("Keine Treffer für «portal».")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/Suche nach/), {
      target: { value: "kasse-api:1.1" },
    });
    expect(
      await within(tabs).findByRole("tab", { name: /Deploys 5/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("Erstdeploy")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getAllByText("kasse-api:1.1").length).toBeGreaterThan(0);
    fireEvent.click(within(tabs).getByRole("tab", { name: /Läufe/ }));
    expect(window.location.search).toBe("?q=kasse-api%3A1.1&tab=runs");
    // The UI's word where the status plainly says it, else the CI's.
    const runs = screen.getByText("Unstable").closest("table")!;
    expect(within(runs).getByText("Erfolgreich")).toBeInTheDocument();
    expect(within(runs).getByText("Fehlgeschlagen")).toBeInTheDocument();
    expect(screen.getAllByText("Releases").length).toBeGreaterThan(0);
  });

  it("shows tooltips on hover and picks the build kind for hotspots", async () => {
    mockFetch();
    render(<App />);
    await screen.findByRole("heading", {
      name: "Wie schnell und verlässlich ist das Feedback?",
    });
    const cols = screen.getAllByTestId("week-col");
    fireEvent.mouseEnter(cols[3]);
    expect(screen.getByRole("tooltip")).toHaveTextContent("KW 46");
    fireEvent.mouseLeave(cols[3]);
    expect(screen.queryByRole("tooltip")).toBeNull();
    const kinds = screen.getByRole("group", { name: "Lauf-Art" });
    // feature builds first: kasse-api broke one run, too few to compare
    expect(
      within(kinds).getByRole("button", { name: "Feature- und Bugfix-Builds" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("Kein Vergleich")).toBeInTheDocument();
    expect(
      screen.getByText(/unter 10 Läufen in einer der beiden Perioden;/),
    ).toBeInTheDocument();
    expect(
      screen.getAllByText("Repos: kein Vergleich mit der Vorperiode"),
    ).toHaveLength(3);
    fireEvent.click(within(kinds).getByRole("button", { name: "Releases" }));
    expect(
      within(kinds).getByRole("button", { name: "Releases" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(
      screen.getAllByRole("img", { name: /^Erfolgsquote pro Woche/ }),
    ).toHaveLength(3);
    fireEvent.mouseEnter(
      screen
        .getByText("< 15 min")
        .closest("[role=img]")!
        .querySelector(".gap-\\[3px\\]")!,
    );
    expect(screen.getAllByRole("tooltip").length).toBeGreaterThan(0);
  });

  it("flags the hotspots the backend found", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      m.build.kinds[1] = {
        ...m.build.kinds[1],
        outlier_runs: 1,
        outlier_seconds: 32 * 3600,
        waiting: spread(4, 7 * 60),
        building: spread(4, 3 * 60),
      };
      // The circles' base: the rows hold most of the kind's CI time.
      m.build.kinds[0] = { ...m.build.kinds[0], ci_seconds: 60_000 };
      const h = m.build.hotspots;
      h.kinds[0] = {
        ...h.kinds[0],
        failure_rate: 0.1,
        unplotted: 2,
        repos: [
          {
            repo: "kasse/flaky-api",
            runs: 20,
            failures: 8,
            failure_rate: 0.4,
            failure_change: change("worse", 0.1),
            wasted_seconds: 7200,
            duration: spread(20, 600),
            waiting: noSpread(),
            building: noSpread(),
            duration_change: change("same", 600),
            factor: 1,
            slow: false,
            flaky: true,
            ci_seconds: 12_000,
            compared: true,
            prev_complete: true,
            plotted: true,
          },
          {
            repo: "kasse/slow-api",
            runs: 12,
            failures: 0,
            failure_rate: 0,
            failure_change: change("same", 0),
            wasted_seconds: 0,
            duration: spread(12, 1800, 2400),
            waiting: noSpread(),
            building: noSpread(),
            duration_change: change("worse", 600),
            factor: 3,
            slow: true,
            flaky: false,
            ci_seconds: 21_600,
            compared: true,
            prev_complete: true,
            plotted: true,
          },
          {
            repo: "kasse/new-api",
            runs: 12,
            failures: 0,
            failure_rate: 0,
            failure_change: change("none", null, "no_value"),
            wasted_seconds: 0,
            duration: spread(12, 300),
            waiting: noSpread(),
            building: noSpread(),
            duration_change: change("none", null, "no_value"),
            factor: null,
            slow: false,
            flaky: false,
            ci_seconds: 3600,
            compared: false,
            prev_complete: false,
            plotted: true,
          },
        ],
      };
      m.comparison.build[2] = {
        ...m.comparison.build[2],
        slower: 1,
        flakier: 1,
        compared: 2,
      };
      h.outlier_groups = [
        {
          from: ago(3),
          to: ago(2.99),
          runs: 2,
          repos: ["kasse/kasse-api", "kasse/kasse-web"],
          shared: true,
        },
        // one repo stuck twice: no sentence about the agents
        { from: ago(5), to: ago(5), runs: 2, repos: ["a"], shared: false },
        // across midnight: both days
        {
          from: "2026-11-05T22:50:00Z",
          to: "2026-11-05T23:15:00Z",
          runs: 3,
          repos: ["b", "c"],
          shared: true,
        },
      ];
      // More stuck than the 10 longest the backend sends.
      h.outlier_count = 25;
      h.outliers = [
        {
          kind: "main",
          pipeline_name: "kasse-api/master",
          repo: "kasse/kasse-api",
          run_id: "7",
          started_at: ago(3),
          duration_s: 32 * 3600,
          status: "SUCCESS",
          factor: 160,
          wait_s: 20 * 3600,
        },
        {
          kind: "main",
          pipeline_name: "kasse-api/master",
          repo: "kasse/kasse-api",
          run_id: "8",
          started_at: ago(3),
          duration_s: 5 * 3600,
          status: "SUCCESS",
          factor: null,
          wait_s: null,
        },
      ];
      return m;
    });
    render(<App />);
    await screen.findByText("Repos, die das Feedback bremsen");
    // Each repo against its own previous period, never the team.
    expect(screen.getByText("Fehleranfälliger geworden")).toBeInTheDocument();
    expect(
      screen.getByText(/Vorperiode 10 % → jetzt 40 % rot/),
    ).toBeInTheDocument();
    expect(screen.getByText("Langsamer geworden")).toBeInTheDocument();
    expect(
      screen.getByText(/Vorperiode 10 min → jetzt 30 min \(3\.0×\)/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Team 10/)).toBeNull();
    expect(
      screen.getByText(
        /keine vergleichbaren Läufe dieser Art in der Vorperiode;/,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/fehleranfälliger geworden als in der Vorperiode/),
    ).toHaveTextContent(
      "Von 2 Repos sind 1 langsamer, 1 fehleranfälliger geworden als in der Vorperiode",
    );
    expect(screen.getByText("2.0 h")).toBeInTheDocument();
    // Above the table: red arrows only where the backend flagged, labelled
    // with what changed; no comparison, no arrow.
    const quiver = screen.getByTestId("quiver");
    expect(
      within(quiver).getByRole("group", {
        name: /^Feature- und Bugfix-Builds: jedes Repo von der Vorperiode/,
      }),
    ).toBeInTheDocument();
    expect(quiver).toHaveTextContent(
      "2 Repos dieser Art sind nicht eingezeichnet",
    );
    const marks = within(quiver).getAllByTestId("quiver-repo");
    const mark = (repo: string) => marks.find((g) => g.dataset.repo === repo)!;
    expect(mark("kasse/flaky-api").dataset).toMatchObject({
      flagged: "true",
      arrow: "true",
    });
    expect(mark("kasse/slow-api").dataset).toMatchObject({
      flagged: "true",
      arrow: "true",
    });
    expect(mark("kasse/new-api").dataset.flagged).toBeUndefined();
    expect(mark("kasse/new-api").dataset.arrow).toBeUndefined();
    expect(mark("kasse/new-api").querySelector("circle")).toHaveAttribute(
      "stroke-dasharray",
    );
    expect(
      within(quiver).getByText("kasse/flaky-api ▲ Fehlerquote 10 % → 40 %"),
    ).toBeInTheDocument();
    expect(
      within(quiver).getByText("kasse/slow-api ▲ ×3.0 Dauer"),
    ).toBeInTheDocument();
    expect(within(quiver).queryByText(/^kasse\/new-api/)).toBeNull();
    const hit = (repo: string) =>
      within(quiver)
        .getAllByTestId("quiver-hit")
        .find((c) => c.dataset.repo === repo)!;
    // The tooltip lives outside the chart's scroll box, which would clip it.
    fireEvent.mouseEnter(hit("kasse/slow-api"));
    expect(within(quiver).queryByRole("tooltip")).toBeNull();
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      /kasse\/slow-apiMedian10 min → 30 minFehlerquote0 % → 0 %Läufe12CI-Zeit6.0 h$/,
    );
    fireEvent.mouseLeave(hit("kasse/slow-api"));
    // By keyboard too, the tooltip's content as the target's name.
    fireEvent.focus(hit("kasse/new-api"));
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      "Kein Vergleichkeine vergleichbaren Läufe dieser Art in der Vorperiode",
    );
    fireEvent.blur(hit("kasse/new-api"));
    expect(screen.queryByRole("tooltip")).toBeNull();
    expect(hit("kasse/new-api")).toHaveAttribute("tabindex", "0");
    expect(
      within(quiver).getByRole("img", { name: /^kasse\/slow-api; Median: / }),
    ).toBeInTheDocument();
    // Flagged ones painted last; hover targets the largest first, so a
    // small circle is never under a big one's target.
    expect(marks.map((g) => g.dataset.repo)).toEqual([
      "kasse/new-api",
      "kasse/flaky-api",
      "kasse/slow-api",
    ]);
    expect(
      within(quiver)
        .getAllByTestId("quiver-hit")
        .map((c) => c.dataset.repo),
    ).toEqual(["kasse/slow-api", "kasse/flaky-api", "kasse/new-api"]);
    fireEvent.click(
      screen.getAllByRole("button", { name: "Läufe ansehen →" })[1],
    );
    expect(window.location.search).toBe("?q=kasse%2Fslow-api&tab=runs");
    // Stuck together across repos: the agents, said once.
    expect(
      screen.getByText(/2 Läufe aus 2 Repos starteten am/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "3 Läufe aus 2 Repos starteten zwischen 05.11. 23:50 und 06.11. 00:15",
      ),
    ).toBeInTheDocument();
    expect(screen.getAllByText(/Repos starteten/)).toHaveLength(2);
    expect(
      screen.getByText(/^25 Läufe hingen im Zeitraum/),
    ).toBeInTheDocument();
    expect(screen.getByText("Die längsten 2 anzeigen")).toBeInTheDocument();
    expect(screen.getByText("160×")).toBeInTheDocument();
    // A team median of 0 gives no factor.
    expect(
      screen.getByText("#8").parentElement?.parentElement,
    ).toHaveTextContent(/–$/);
    expect(
      screen.getByText(/1 Ausreisser \(32\.0 h\) nicht in Dauer und CI-Zeit/),
    ).toBeInTheDocument();
    expect(screen.getByText("#7")).toBeInTheDocument();
    // The agent wait, beside the duration, only where runs report it.
    expect(screen.getByTitle(/^Median über 4 der /)).toHaveTextContent(
      "davon typischerweise 7 min Warten auf einen Agent, 3 min Bauen",
    );
    expect(screen.getAllByText(/Warten auf einen Agent/)).toHaveLength(1);
    expect(
      screen.getByText("#7").parentElement?.parentElement,
    ).toHaveTextContent("20.0 h");
    expect(
      screen.getByTitle("Der Lauf meldet nicht, wann er einen Agent bekam"),
    ).toHaveTextContent("–");
  });

  it("says how many repos the build chart cannot draw, even all", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      const k = m.build.hotspots.kinds[0];
      k.repos = [{ ...k.repos[0], plotted: false }];
      k.unplotted = 3;
      return m;
    });
    render(<App />);
    const quiver = await screen.findByTestId("quiver");
    expect(quiver).toHaveTextContent(
      "3 Repos dieser Art sind nicht eingezeichnet",
    );
    expect(quiver.querySelector("svg")).toBeNull();
  });

  it("lists the longest builds as the backend picked them", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      const k = m.build.hotspots.kinds[0];
      const row = k.repos[0];
      k.repos = [{ ...row, waiting: spread(1, 60), building: spread(1, 540) }];
      k.slowest = [
        {
          ...row,
          repo: "kasse/steady-web",
          runs: 12,
          duration: spread(12, 1500, 1800),
          waiting: spread(6, 120),
          building: spread(6, 1380),
        },
        { ...row, repo: "kasse/mono", runs: 4, duration: spread(4, 900) },
      ];
      return m;
    });
    render(<App />);
    await screen.findByText("Längste Builds");
    expect(
      screen.getByText(/ab 3 Läufen mit Ergebnis und Dauer/),
    ).toBeInTheDocument();
    // The wait beside the build, in the hotspots row too.
    expect(screen.getByText(/· 1 min Warten, 9 min Bauen/)).toBeInTheDocument();
    const steady = screen.getByText("kasse/steady-web").closest(".contents");
    expect(steady).toHaveTextContent(/25 min30 min2 min \/ 23 min12/);
    expect(
      screen.getByTitle(
        "Median über 6 der 12 Läufe, die melden, wann sie einen Agent bekamen",
      ),
    ).toBeInTheDocument();
    const mono = screen.getByText("kasse/mono").closest(".contents");
    expect(
      within(mono as HTMLElement).getByTitle(
        "Keiner der Läufe meldet, wann er einen Agent bekam",
      ),
    ).toHaveTextContent("–");
    // In the backend's order, its link last.
    const links = screen.getAllByRole("button", { name: "Läufe ansehen →" });
    fireEvent.click(links[links.length - 1]);
    expect(window.location.search).toBe("?q=kasse%2Fmono&tab=runs");
  });

  it("paints the failure heatmap as the backend classed it", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics(5);
      const cell = (
        runs: number,
        failed: number,
        step: number | null,
        no_data = false,
      ) => ({ runs, failed, rate: runs ? failed / runs : null, step, no_data });
      m.build.hotspots.kinds[2].heatmap = [
        {
          repo: "kasse/kasse-api",
          other: false,
          weeks: [
            // before the run data
            cell(0, 0, null, true),
            cell(0, 0, null),
            cell(2, 1, null),
            cell(3, 0, 0),
            cell(5, 2, 4),
          ],
        },
        {
          repo: "",
          other: true,
          weeks: [
            cell(0, 0, null, true),
            cell(0, 0, null),
            cell(0, 0, null),
            cell(0, 0, null),
            cell(4, 4, 4),
          ],
        },
      ];
      m.build.hotspots.kinds[0].heatmap = m.build.hotspots.kinds[2].heatmap;
      return m;
    });
    render(<App />);
    await screen.findByText("Fehlerquote pro Repo und Woche");
    // The numbers as a table for assistive tech, the grid hidden from it.
    const table = screen.getByRole("table", {
      name: "Fehlerquote pro Repo und Woche, Feature- und Bugfix-Builds",
    });
    expect(
      within(table).getByRole("row", { name: /^übrige Repos/ }),
    ).toHaveTextContent("4 von 4 Läufen fehlgeschlagen (100 %)");
    const map = screen.getByTestId("heat-map");
    expect(map).toHaveAttribute("aria-hidden", "true");
    expect(within(map).getAllByTestId("heat-row")).toHaveLength(2);
    expect(within(map).getByText("übrige Repos")).toBeInTheDocument();
    const cells = within(map).getAllByTestId("heat-cell").slice(0, 5);
    // The backend's class, never one the browser derived.
    expect(cells[4].firstElementChild).toHaveClass("bg-bad-4");
    expect(cells[3].firstElementChild).toHaveClass("bg-cell-empty");
    expect(cells[1].firstElementChild).toHaveClass("border-dashed");
    expect(cells[0].firstElementChild).toHaveClass("texture-gap");
    fireEvent.mouseEnter(cells[4]);
    // The tooltip sits in the grid, hidden from assistive tech.
    const tipNow = () => screen.getByRole("tooltip", { hidden: true });
    const tip = tipNow();
    expect(tip.firstElementChild?.textContent).toBe(
      "kasse/kasse-api · KW 46 · Feature- und Bugfix-Builds",
    );
    expect(tip).toHaveTextContent("2 von 5 Läufen fehlgeschlagen (40 %)");
    fireEvent.mouseLeave(cells[4]);
    fireEvent.mouseEnter(cells[2]);
    expect(tipNow()).toHaveTextContent(
      "1 von 2 Läufen fehlgeschlagenunter 3 Läufen: kein Urteil",
    );
    fireEvent.mouseLeave(cells[2]);
    fireEvent.mouseEnter(cells[1]);
    expect(tipNow()).toHaveTextContent("keine Läufe");
    fireEvent.mouseLeave(cells[1]);
    fireEvent.mouseEnter(cells[0]);
    expect(tipNow()).toHaveTextContent("keine verlässlichen Daten");
    // The legend from the backend's cut points, drawn as the cells are.
    expect(screen.getByText("10–20 %")).toBeInTheDocument();
    expect(screen.getByText("ab 40 %")).toBeInTheDocument();
    const swatches = screen.getAllByTestId("heat-swatch");
    expect(swatches).toHaveLength(8);
    expect(swatches[0].className).toBe(
      `inline-block size-2.5 ${cells[3].firstElementChild?.className.replace("h-[18px] ", "")}`,
    );
    // A hovered cell does not carry over to another kind.
    fireEvent.mouseEnter(cells[4]);
    expect(tipNow()).toBeInTheDocument();
    const kinds = screen.getByRole("group", { name: "Lauf-Art" });
    fireEvent.click(within(kinds).getByRole("button", { name: "Releases" }));
    expect(
      screen.getByRole("table", { name: /Releases$/ }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("tooltip", { hidden: true })).toBeNull();
    // A class past the colours takes the top one and warns once, never
    // takes the page down.
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    expect(stepLook(5).className).toContain("bg-bad-4");
    expect(stepLook(6).className).toContain("bg-bad-4");
    expect(warn).toHaveBeenCalledTimes(1);
  });

  it("asks for the facts of the product the metrics resolved", async () => {
    window.history.replaceState(null, "", "/?product=gone");
    const calls = mockFetch();
    render(<App />);
    await screen.findByRole("tablist", { name: "Faktentabelle" });
    expect(metricsCalls(calls)[0]).toMatch(/&product=gone$/);
    expect(factsCalls(calls)[0]).not.toMatch(/product=/);
  });

  it("labels cycle-time buckets it has no words for by their bounds", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      return {
        ...m,
        review: {
          ...m.review,
          cycle_histogram: {
            upper_bounds_s: [600],
            human: [1, 2],
          },
        },
      };
    });
    render(<App />);
    await screen.findByRole("heading", { name: "Wo wartet eine Änderung?" });
    expect(screen.getByText("bis 10 min")).toBeInTheDocument();
    expect(screen.getByText("ab 10 min")).toBeInTheDocument();
  });

  it("reports a failed load with the server's detail", async () => {
    mockFetch(
      () =>
        new Response(JSON.stringify({ detail: "unknown team" }), {
          status: 404,
        }),
    );
    render(<App />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "404: unknown team",
    );
  });

  it("keeps a non-JSON error's status text", async () => {
    mockFetch(
      () => new Response("<html>", { status: 502, statusText: "Bad Gateway" }),
    );
    render(<App />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "502: Bad Gateway",
    );
  });

  it("reports a failed facts load", async () => {
    mockFetch((url) =>
      url.pathname === "/api/facts"
        ? new Response(JSON.stringify({ detail: "boom" }), { status: 500 })
        : metrics(),
    );
    render(<App />);
    expect(await screen.findByRole("alert")).toHaveTextContent("500: boom");
    expect(
      screen.getByRole("heading", { name: "Einschätzung" }),
    ).toBeInTheDocument();
    // The data section says so instead of loading forever.
    expect(
      screen.getByText(/Die Einzelfakten konnten nicht geladen werden/),
    ).toBeInTheDocument();
  });

  it("says so when the config has no team", async () => {
    mockFetch(sample, []);
    render(<App />);
    expect(
      await screen.findByText(
        "In der Konfiguration ist kein Team eingetragen.",
      ),
    ).toBeInTheDocument();
  });

  it("reports a failed team list", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("offline");
      }),
    );
    render(<App />);
    expect(await screen.findByRole("alert")).toHaveTextContent("offline");
  });

  it("prints", async () => {
    mockFetch();
    const print = vi.fn();
    vi.stubGlobal("print", print);
    render(<App />);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Drucken/ }));
    });
    expect(print).toHaveBeenCalled();
  });

  it("says where the feature builds hide when none is recognised", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      const other = {
        ...m.build.kinds[1],
        kind: "other" as const,
        runs: 70,
        prefixes: [
          { prefix: "feat/", runs: 31 },
          { prefix: "KAS-…", runs: 13 },
          { prefix: "", runs: 2 },
        ],
      };
      return {
        ...m,
        build: {
          ...m.build,
          kinds: [m.build.kinds[1], other],
          feature_hint: { runs: 44 },
        },
      };
    });
    render(<App />);
    expect(
      await screen.findByText("Feature- und Bugfix-Builds: kein Lauf erkannt"),
    ).toBeInTheDocument();
    // the backend's count, not the other card's 70 runs
    expect(
      screen.getByText(
        /44 Läufe auf Branches ohne bekanntes Präfix stehen unter «Übrige Branches»/,
      ),
    ).toBeInTheDocument();
    // the chips in the hint only, not again on the other card
    expect(screen.getAllByText("feat/")).toHaveLength(1);
    expect(screen.getAllByText(/ohne Branch im Pipeline-Namen/)).toHaveLength(
      1,
    );
    expect(screen.queryByText(/^Häufigste Präfixe:/)).toBeNull();
    // no feature card: its success sparkline is the card's own mark
    expect(
      screen.queryByLabelText(
        /^Erfolgsquote pro Woche, Feature- und Bugfix-Builds/,
      ),
    ).toBeNull();
    // the repos that slow the feedback down: where the feature builds hide,
    // never the default branch
    const kinds = screen.getByRole("group", { name: "Lauf-Art" });
    expect(
      within(kinds).getByRole("button", { name: "Übrige Branches" }),
    ).toHaveAttribute("aria-pressed", "true");
  });

  it("shows no hint the backend did not send", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      const other = {
        ...m.build.kinds[1],
        kind: "other" as const,
        prefixes: [{ prefix: "nightly", runs: 3 }],
      };
      return { ...m, build: { ...m.build, kinds: [m.build.kinds[1], other] } };
    });
    render(<App />);
    await screen.findAllByText("Übrige Branches");
    expect(screen.queryByText(/kein Lauf erkannt/)).toBeNull();
    // the prefixes on the other card instead
    expect(screen.getByText(/^Häufigste Präfixe:/)).toBeInTheDocument();
    // no feature builds, none hiding: the first kind
    const kinds = screen.getByRole("group", { name: "Lauf-Art" });
    expect(
      within(kinds).getByRole("button", { name: "Default-Branch-Builds" }),
    ).toHaveAttribute("aria-pressed", "true");
  });

  it("puts the feature card first", async () => {
    mockFetch();
    render(<App />);
    // the card itself, not the hotspots' button of the same name, and
    // twice as wide as the others
    const spark = await screen.findByLabelText(
      /^Erfolgsquote pro Woche, Feature- und Bugfix-Builds/,
    );
    expect(spark.closest(".md\\:col-span-2")).not.toBeNull();
    expect(screen.queryByText(/kein Lauf erkannt/)).toBeNull();
    // the cards in the backend's order, before the hotspots' kind buttons
    const names = screen
      .getAllByText(
        /^(Feature- und Bugfix-Builds|Default-Branch-Builds|Releases)$/,
      )
      .slice(0, 3)
      .map((e) => e.textContent);
    expect(names).toEqual([
      "Feature- und Bugfix-Builds",
      "Default-Branch-Builds",
      "Releases",
    ]);
  });

  it("explains a build section with only unknown runs", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      return {
        ...m,
        lead_chain: {
          ...m.lead_chain,
          stages: m.lead_chain.stages.map((s) => ({
            ...s,
            n: 0,
            p50: null,
            p90: null,
            share: null,
            hot: false,
          })),
        },
        build: {
          ...m.build,
          data_from: ago(3),
          starts_inside: true,
          gap_weeks: 3,
          kinds: [{ ...m.build.kinds[1], kind: "unknown" }],
          hotspots: {
            ...m.build.hotspots,
            kinds: [{ ...m.build.hotspots.kinds[1], kind: "unknown" }],
          },
        },
      };
    });
    render(<App />);
    expect(
      await screen.findByText("vor der ersten build_type-Meldung"),
    ).toBeInTheDocument();
    expect(screen.getByText(/Build-Daten ab 09\.11\.2026/)).toBeInTheDocument();
    expect(
      screen.getByText("Im Zeitraum wurde kein PR gemerged."),
    ).toBeInTheDocument();
  });

  it("drops a failure once the same load succeeds", async () => {
    let n = 0;
    mockFetch((url) => {
      if (url.pathname === "/api/metrics" && ++n === 1)
        return new Response(JSON.stringify({ detail: "boom" }), {
          status: 500,
        });
      return sample(url);
    });
    render(<App />);
    expect(await screen.findByRole("alert")).toHaveTextContent("boom");
    fireEvent.click(screen.getByRole("button", { name: "12 W" }));
    await screen.findByRole("heading", { name: "Einschätzung" });
    fireEvent.click(screen.getByRole("button", { name: "4 W" }));
    // The page's scope and the assessment's closed weeks both name it.
    await screen.findAllByText(/KW 43–46/);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("shows an unknown wait as a dash, never as 0 s", async () => {
    mockFetch((url) =>
      url.pathname === "/api/facts"
        ? facts({
            prs: { count: 1, rows: [prFact({ review_s: null })] },
            deploys: { count: 1, rows: [deployFact()] },
          })
        : metrics(),
    );
    render(<App />);
    const row = (await screen.findByText(": Pay by card")).closest("tr")!;
    expect(within(row).getAllByText("–").length).toBeGreaterThan(0);
    expect(within(row).queryByText("0 s")).toBeNull();
  });

  it("tells a team Argo CD does not report for from one without deploys", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      return {
        ...m,
        delivery: {
          ...m.delivery,
          frequency: {
            ...m.delivery.frequency,
            syncs: 0,
            argo_reporting: false,
          },
        },
      };
    });
    render(<App />);
    // The tile and the calendar, which draws no day as one without a release.
    expect(
      await screen.findAllByText("Argo CD meldet für das Team noch nicht"),
    ).toHaveLength(2);
    expect(screen.queryAllByTestId("day-cell")).toHaveLength(0);
  });

  it("notes when Argo CD started inside the window", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      return {
        ...m,
        delivery: {
          ...m.delivery,
          frequency: { ...m.delivery.frequency, since: ago(5) },
        },
      };
    });
    render(<App />);
    expect(
      await screen.findByText(/Argo CD meldet erst seit 07\.11\.2026/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/wenig belastbar\.$/)).toBeNull();
  });

  it("flags a deploy rate on less than a week of data", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      return {
        ...m,
        delivery: {
          ...m.delivery,
          frequency: {
            ...m.delivery.frequency,
            since: ago(2),
            low_sample: true,
          },
        },
      };
    });
    render(<App />);
    expect(
      await screen.findByText(/Weniger als eine Woche Daten: wenig belastbar/),
    ).toBeInTheDocument();
  });

  it("shows the runs without a verdict a kind leaves out", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      return {
        ...m,
        build: {
          ...m.build,
          kinds: [{ ...m.build.kinds[0], no_verdict: 3 }],
        },
      };
    });
    render(<App />);
    expect(await screen.findByText(/\+ 3 ohne Ergebnis/)).toBeInTheDocument();
  });

  it("treats a blank search as none", async () => {
    const calls = mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Daten" });
    fireEvent.change(screen.getByLabelText(/Suche nach/), {
      target: { value: "   " },
    });
    await new Promise((r) => setTimeout(r, 300));
    expect(screen.queryByText(/gefiltert nach/)).toBeNull();
    expect(factsCalls(calls).every((c) => !c.includes("q="))).toBe(true);
  });
});

describe("costs", () => {
  it("names the promotion spend only when there is some", () => {
    const m = metrics();
    const { unmount } = render(<Costs m={m} />);
    expect(
      screen.getByText("davon $0.50 für 1 PR, der gemerged wurde"),
    ).toBeInTheDocument();
    unmount();
    render(
      <Costs
        m={{
          ...m,
          costs: { ...m.costs, promotion: 0.2, promotion_reviews: 2 },
        }}
      />,
    );
    expect(
      screen.getByText(
        "davon $0.50 für 1 PR, der gemerged wurde · $0.20 für 2 Promotion-Reviews",
      ),
    ).toBeInTheDocument();
  });
});

describe("open PRs", () => {
  it("renders the repos the backend sent, with what they wait on", () => {
    const onFacts = vi.fn();
    render(
      <Flow why={null} onWhy={() => {}} onFacts={onFacts} m={metrics()} />,
    );
    expect(
      screen.getByText("Offene PRs: wo staut es sich?"),
    ).toBeInTheDocument();
    expect(screen.getByText("Repos im Stau")).toBeInTheDocument();
    expect(screen.getByText("mit 3 lang offenen PRs")).toBeInTheDocument();
    expect(screen.getByText("insgesamt 3 PRs lang offen")).toBeInTheDocument();
    expect(screen.getByText("typischerweise 9.0 Tage alt")).toBeInTheDocument();
    // The repo's median age, never its oldest PR's.
    expect(screen.getByText("12.0 Tage")).toBeInTheDocument();
    // In the backend's order, chart and table, zero counts left out; the
    // jam marked.
    expect(
      screen
        .getAllByTestId("mekko-col")
        .map((c) => c.getAttribute("aria-label")?.split(":")[0]),
    ).toEqual(["kasse/kasse-api", "portal/portal-web"]);
    const rows = screen.getAllByText(/^(kasse\/kasse-api|portal\/portal-web)$/);
    expect(rows.map((r) => r.textContent)).toEqual([
      "kasse/kasse-api",
      "portal/portal-web",
    ]);
    expect(screen.getAllByText("Stau")).toHaveLength(1);
    expect(screen.getAllByText("Bereit zum Merge 1")).toHaveLength(2);
    expect(screen.queryByText(/^Entwurf \d/)).toBeNull();
    fireEvent.click(
      screen.getAllByRole("button", { name: "PRs ansehen →" })[0],
    );
    expect(onFacts).toHaveBeenCalledWith("prs", "kasse/kasse-api");
    // The inactive ones apart, their link a search for their word.
    expect(
      screen.getByText(/^1 PR inaktiv: seit 30 Tagen kein Event/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Ansehen →" }));
    expect(onFacts).toHaveBeenCalledWith("prs", "Inaktiv");
  });

  it("draws a column per repo, as wide as its open PRs, split by wait", () => {
    render(
      <Flow why={null} onWhy={() => {}} onFacts={() => {}} m={metrics()} />,
    );
    const chart = screen.getByRole("group", {
      name: "Offene PRs pro Repo, nach dem, worauf sie warten",
    });
    const cols = within(chart).getAllByTestId("mekko-col");
    expect(cols.map((c) => c.style.flex)).toEqual(["3 1 0px", "1 1 0px"]);
    // At least 22px per open PR: past that it scrolls.
    expect(chart.style.minWidth).toBe(`${42 + 4 * 22 + 3}px`);
    // kasse-api: first review at the foot, the merge on top; its 3
    // long-open PRs marked.
    const api = within(cols[0]).getAllByTestId("mekko-seg");
    expect(api.map((s) => s.style.height)).toEqual(["113.333px", "56.6667px"]);
    expect(api[0].className).toContain("from-s1");
    expect(api[1].className).toContain("from-s2");
    expect(api[1].className).toContain("rounded-t");
    expect(api[1].className).toContain("border-b-2");
    expect(
      within(cols[0]).getByText("3", { selector: ".text-ochre" }),
    ).toBeInTheDocument();
    expect(
      within(cols[1]).queryByText("1", { selector: ".text-ochre" }),
    ).toBeNull();
    // The whole column is the target, its tooltip every segment, top
    // first; formatted counts, en spaces round the middot.
    fireEvent.mouseEnter(cols[0]);
    const sep = `${String.fromCharCode(0x2002)}·${String.fromCharCode(0x2002)}`;
    const tip = within(chart).getByRole("tooltip");
    expect(
      within(tip)
        .getAllByText(/./, { selector: "span" })
        .map((s) => s.textContent),
    ).toEqual([
      "Bereit zum Merge",
      `1 von 3 offenen PRs${sep}1 davon ≥ 7 Tage offen`,
      "Wartet auf Review",
      `2 von 3 offenen PRs${sep}2 davon ≥ 7 Tage offen`,
    ]);
    fireEvent.mouseLeave(cols[0]);
    expect(within(chart).queryByRole("tooltip")).toBeNull();
    // Keyboard: focusable, the tooltip on focus, the same words as label.
    expect(cols[1]).toHaveAttribute("tabindex", "0");
    expect(cols[1]).toHaveAccessibleName(
      "portal/portal-web: Wartet auf Review 1 von 1 offenen PR",
    );
    fireEvent.focus(cols[1]);
    expect(within(chart).getByText("1 von 1 offenen PR")).toBeInTheDocument();
    // Every reason in the legend, the draft as not the team's.
    expect(
      screen.getByText("Entwurf (wartet nicht aufs Team)"),
    ).toBeInTheDocument();
  });

  // Two long names of one project, the end telling them apart, and a
  // one-PR repo beside them.
  function longNames(): Metrics {
    const m = metrics();
    const [, one] = m.open_prs.repos;
    const repo = (name: string, open: number) => ({
      ...one,
      repo: name,
      open,
      waiting: { ...one.waiting, first_review: open },
    });
    return {
      ...m,
      open_prs: {
        ...m.open_prs,
        repos: [
          repo("fleetops/fleet-telematics-ingest-service", 9),
          repo("fleetops/fleet-telematics-ingest-ui", 8),
          repo("portal/portal-web", 1),
        ],
      },
    };
  }

  // The columns' row as wide as a laid-out page would render it.
  function rowWidth(w: number) {
    return vi
      .spyOn(HTMLElement.prototype, "clientWidth", "get")
      .mockReturnValue(w);
  }

  it("names every column under it, wrapped whole, never rotated", () => {
    const spy = rowWidth(900);
    render(
      <Flow why={null} onWhy={() => {}} onFacts={() => {}} m={longNames()} />,
    );
    spy.mockRestore();
    const chart = screen.getByRole("group", {
      name: "Offene PRs pro Repo, nach dem, worauf sie warten",
    });
    const cols = within(chart).getAllByTestId("mekko-col");
    // The project on its own line, the name whole with a break after every
    // "-": the ends that tell the two apart are there.
    const names = cols.map((c) => {
      const label = within(c).getByTestId("mekko-label");
      const [project, name] = Array.from(label.children);
      return [
        project.textContent,
        name.textContent,
        name.querySelectorAll("wbr").length,
      ];
    });
    expect(names).toEqual([
      ["fleetops", "fleet-telematics-ingest-service", 3],
      ["fleetops", "fleet-telematics-ingest-ui", 3],
      ["portal", "portal-web", 1],
    ]);
    // Horizontal, in the flow, the full name on hover.
    expect(chart.querySelector("[class*='rotate']")).toBeNull();
    expect(
      within(cols[1]).getByTitle("fleetops/fleet-telematics-ingest-ui"),
    ).toBeInTheDocument();
    // Every name fits: no numbers, not in the table either.
    expect(screen.queryAllByTestId("repo-no")).toEqual([]);
  });

  it("numbers the columns once one is too narrow, the table rows the same", () => {
    // Unmeasured: the narrowest the chart gets, 22px for the one-PR repo.
    render(
      <Flow why={null} onWhy={() => {}} onFacts={() => {}} m={longNames()} />,
    );
    const chart = screen.getByRole("group", {
      name: "Offene PRs pro Repo, nach dem, worauf sie warten",
    });
    const cols = within(chart).getAllByTestId("mekko-col");
    expect(
      cols.map((c) => within(c).getByTestId("repo-no").textContent),
    ).toEqual(["1", "2", "3"]);
    // The narrow column only its number, the wide ones their names too.
    expect(
      cols.map((c) => within(c).queryByTestId("mekko-label") != null),
    ).toEqual([true, true, false]);
    // The number before the repo in its table row.
    const row = screen.getByText("portal/portal-web").parentElement!;
    expect(within(row).getByTestId("repo-no")).toHaveTextContent("3");
    expect(
      within(
        screen.getByText("fleetops/fleet-telematics-ingest-service")
          .parentElement!,
      ).getByTestId("repo-no"),
    ).toHaveTextContent("1");
  });

  // What the browser measured in a column's probe: `cut` says which part
  // ("name" clamped, "project" ellipsised) overflowed for which repo.
  function measured(cut: (repo: string, part: string) => boolean) {
    const over = (el: HTMLElement) => {
      const repo =
        el
          .closest("[data-testid=mekko-col]")
          ?.getAttribute("aria-label")
          ?.split(":")[0] ?? "";
      return el.dataset.fit && cut(repo, el.dataset.fit) ? 10_000 : 0;
    };
    return [
      rowWidth(900),
      vi
        .spyOn(HTMLElement.prototype, "scrollHeight", "get")
        .mockImplementation(function (this: HTMLElement) {
          return over(this);
        }),
      vi
        .spyOn(HTMLElement.prototype, "scrollWidth", "get")
        .mockImplementation(function (this: HTMLElement) {
          return over(this);
        }),
    ];
  }

  function shown() {
    return screen
      .getAllByTestId("mekko-col")
      .map((c) =>
        within(c).queryByTestId("mekko-label")
          ? "name"
          : within(c).getByTestId("repo-no").textContent,
      );
  }

  it("numbers a column whose name the clamp would cut short", () => {
    const spies = measured(
      (repo, part) =>
        repo === "fleetops/fleet-telematics-ingest-ui" && part === "name",
    );
    render(
      <Flow why={null} onWhy={() => {}} onFacts={() => {}} m={longNames()} />,
    );
    spies.forEach((s) => s.mockRestore());
    // Its "-ui" would be gone: a number, matched in the table.
    expect(shown()).toEqual(["name", "2", "name"]);
    expect(
      within(
        screen.getByText("fleetops/fleet-telematics-ingest-ui").parentElement!,
      ).getByTestId("repo-no"),
    ).toHaveTextContent("2");
  });

  it("numbers columns whose cut project keys could be the same", () => {
    const m = longNames();
    const [a, b] = m.open_prs.repos;
    const two = (x: string, y: string): Metrics => ({
      ...m,
      open_prs: {
        ...m.open_prs,
        repos: [
          { ...a, repo: x },
          { ...b, repo: y },
        ],
      },
    });
    const spies = measured((_, part) => part === "project");
    const { unmount } = render(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={two("fleetops-north/ingest-api", "fleetops-south/ingest-api")}
      />,
    );
    expect(shown()).toEqual(["1", "2"]);
    unmount();
    // One project for all: its key cut hides nothing.
    render(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={two("fleetops/ingest-api", "fleetops/ingest-ui")}
      />,
    );
    spies.forEach((s) => s.mockRestore());
    expect(shown()).toEqual(["name", "name"]);
  });

  it("gives a lost name back only well wider, so a scrollbar cannot flip it", () => {
    const lost = new Map<string, number>();
    const at = (width: number, fits: boolean) =>
      keepLabels([{ repo: "a/b", width, fits }], lost).label[0];
    expect(at(100, false)).toBe(false);
    // Fits again 15px wider, as a page scrollbar gone would make it.
    expect(at(115, true)).toBe(false);
    expect(at(124, true)).toBe(true);
    expect(at(100, true)).toBe(true);
  });

  it("keeps a sliver segment without the gap, so the column stays 100 %", () => {
    const m = metrics();
    const [api] = m.open_prs.repos;
    render(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={{
          ...m,
          open_prs: {
            ...m.open_prs,
            repos: [
              {
                ...api,
                open: 100,
                waiting: { ...api.waiting, first_review: 99, merge: 1 },
              },
            ],
          },
        }}
      />,
    );
    const [, merge] = screen.getAllByTestId("mekko-seg");
    expect(merge.style.height).toBe("1.7px");
    expect(merge.className).not.toContain("border-b-2");
  });

  it("says when nothing was open", () => {
    const m = metrics();
    render(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={{
          ...m,
          open_prs: {
            ...m.open_prs,
            open: 0,
            long: 0,
            jammed: 0,
            jammed_long: 0,
            inactive: 0,
            age: { n: 0, p50: null, p90: null },
            repos: [],
          },
        }}
      />,
    );
    expect(
      screen.getByText("Am Ende des Zeitraums war kein PR offen."),
    ).toBeInTheDocument();
    expect(screen.queryByText("Die lang offenen warten auf:")).toBeNull();
    expect(screen.queryByText(/inaktiv:/)).toBeNull();
  });

  it("links a jammed focus repo to its open PRs", async () => {
    const calls = mockFetch();
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    expect(p.getByText("wartet auf erstes Review: 4")).toBeInTheDocument();
    fireEvent.click(
      p.getByRole("button", { name: "Fakten zu kasse/kasse-api ansehen →" }),
    );
    await waitFor(() =>
      expect(factsCalls(calls).at(-1)).toMatch(/&q=kasse%2Fkasse-api$/),
    );
    expect(window.location.search).toBe("?q=kasse%2Fkasse-api");
  });

  it("shows an open PR's wait and age in the facts", async () => {
    mockFetch();
    render(<App />);
    expect(
      await screen.findByText("Wartet auf Review · 2.0 Tage offen"),
    ).toBeInTheDocument();
    // The first row is no open one: plain newest first.
    expect(screen.getByText("Neueste zuerst")).toBeInTheDocument();
  });

  it("shows an inactive PR's time without an event", async () => {
    const rows = facts().prs.rows;
    mockFetch((url) =>
      url.pathname === "/api/facts"
        ? facts({
            prs: {
              count: 1,
              rows: [
                {
                  ...rows[2],
                  waiting: "inactive",
                  age_s: 90 * 86400,
                  idle_s: 60 * 86400,
                },
              ],
            },
          })
        : metrics(),
    );
    render(<App />);
    expect(
      await screen.findByText("Inaktiv · 60.0 Tage ohne Event"),
    ).toBeInTheDocument();
  });

  it("says the open PRs come first when they lead the table", async () => {
    const rows = facts().prs.rows;
    mockFetch((url) =>
      url.pathname === "/api/facts"
        ? facts({ prs: { count: 3, rows: [rows[2], rows[0], rows[1]] } })
        : metrics(),
    );
    render(<App />);
    expect(
      await screen.findByText(
        "Offene zuerst, die ältesten oben, inaktive danach; dann die neuesten",
      ),
    ).toBeInTheDocument();
  });
});

describe("flow", () => {
  it("draws the shares the backend computed, no stage it left out", () => {
    const zero = {
      n: 1,
      p50: 0,
      p90: 0,
      low_sample: true,
      hot: false,
      histogram: null,
      durations_s: [0],
    };
    const m = metrics();
    render(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={{
          ...m,
          delivery: { ...m.delivery, lead_time_pre: null },
          comparison: { ...m.comparison, lead_time_pre: null },
          lead_chain: {
            stages: (["coding", "review", "merge", "to_prod"] as const).map(
              (key) => ({ key, ...zero, share: 0.25 }),
            ),
            pre_environment: null,
            attention_share: 0.5,
            min_prs: 10,
            before_prod: 0,
            bucket_bounds_s: STAGE_BOUNDS,
          },
        }}
      />,
    );
    expect(document.body.textContent).not.toMatch(/NaN/);
    expect(screen.getAllByText(/25 % der Zeit/)).toHaveLength(4);
    expect(screen.queryByText(/Bis null/)).toBeNull();
    expect(screen.getByText("gemerged → auf PROD")).toBeInTheDocument();
    // No stage is hot: no lever named.
    expect(screen.queryByText(/Der Hebel liegt/)).toBeNull();
  });

  it("draws a stage's spread as a curve, a few PRs one by one", () => {
    const m = metrics();
    const coding = m.lead_chain.stages[0];
    const many = {
      ...coding,
      n: 30,
      low_sample: false,
      durations_s: null,
      histogram: STAGE_BOUNDS.slice(1).map((_, i) =>
        i > 12 && i < 18 ? 6 : 0,
      ),
    };
    const few = {
      ...m.lead_chain.stages[4],
      n: 3,
      durations_s: [3600, 3700, 86400],
    };
    const none = {
      ...m.lead_chain.stages[2],
      n: 0,
      p50: null,
      p90: null,
      share: null,
      durations_s: [],
    };
    const { rerender } = render(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={{
          ...m,
          lead_chain: {
            ...m.lead_chain,
            stages: [
              many,
              m.lead_chain.stages[1],
              none,
              m.lead_chain.stages[3],
              few,
            ],
          },
        }}
      />,
    );
    const rows = screen.getAllByTestId("ridge-row");
    expect(rows).toHaveLength(5);
    // The curve's row has no dots; the others one per PR, none without PRs.
    expect(screen.getAllByTestId("ridge-dot")).toHaveLength(1 + 1 + 3);
    expect(
      screen.getAllByText("unter 10 PRs: einzelne PRs statt Kurve"),
    ).toHaveLength(3);
    // In the drawing and in the screen-reader table.
    expect(screen.getAllByText("keine PRs")).toHaveLength(2);
    // Typical value and slowest 10 % labelled on the hot stage only, in
    // the app's words, never "Median" / "p90".
    expect(screen.getByText("1.0 Tage, typischer Wert")).toBeInTheDocument();
    expect(screen.getAllByText(/^[\d.]+ \S+, typischer Wert$/)).toHaveLength(1);
    expect(document.body.textContent).not.toMatch(/Median 1|p90/);
    expect(screen.getByText("4 W")).toBeInTheDocument();
    fireEvent.mouseEnter(rows[4]);
    // The drawing is hidden from assistive tech, its tooltip with it.
    const tip = screen.getByRole("tooltip", { hidden: true });
    expect(tip).toHaveTextContent("Warten auf PROD");
    expect(tip).toHaveTextContent("PRs3");
    expect(tip).toHaveTextContent("typischer Wert16.8 h");
    expect(tip).toHaveTextContent("die langsamsten 10 %: über16.8 h");
    expect(document.body.textContent).not.toMatch(/NaN/);
    // Assistive tech gets the numbers once, as a table; a stage without
    // PRs reads as such, not as dashes.
    const table = screen.getByRole("table", { name: "Wartezeit je Etappe" });
    const merge = within(table).getByRole("row", { name: /^Merge/ });
    expect(merge).toHaveTextContent("Merge0keine PRs");
    expect(
      within(table).getByRole("row", { name: /^Warten auf PROD/ }),
    ).toHaveTextContent("Warten auf PROD316.8 h16.8 h");
    // New data drops the hover on the old rows.
    rerender(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={{
          ...m,
          lead_chain: { ...m.lead_chain, stages: [many, none] },
        }}
      />,
    );
    expect(screen.queryByRole("tooltip", { hidden: true })).toBeNull();
  });

  it("finds every PR a free spot, also at the plot's right end", () => {
    const m = metrics();
    const stages = [...m.lead_chain.stages];
    // Nine waits past the axis' end: they all pile on its right edge.
    stages[4] = {
      ...stages[4],
      n: 9,
      durations_s: Array.from({ length: 9 }, (_, i) => 5e6 + i),
    };
    render(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={{ ...m, lead_chain: { ...m.lead_chain, stages } }}
      />,
    );
    const xy = screen
      .getAllByTestId("ridge-dot")
      .slice(-9)
      .map((d) => [Number(d.getAttribute("cx")), Number(d.getAttribute("cy"))]);
    expect(new Set(xy.map((p) => p.join())).size).toBe(9);
    // Inside the plot (jsdom lays out nothing: 640 px wide).
    for (const [x] of xy) expect(x).toBeLessThanOrEqual(640);
  });

  it("puts a hot dot stage's numbers above its dots, every PR a dot", () => {
    const m = metrics();
    const stages = m.lead_chain.stages.map((s) => ({ ...s, hot: false }));
    // Seven PRs at nearly the same wait: no two dots on one spot.
    stages[4] = {
      ...stages[4],
      hot: true,
      n: 7,
      durations_s: [3600, 3600, 3600, 3600, 3610, 3620, 3630],
    };
    render(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={{ ...m, lead_chain: { ...m.lead_chain, stages } }}
      />,
    );
    const dots = screen
      .getAllByTestId("ridge-dot")
      .slice(-7)
      .map((d) => `${d.getAttribute("cx")},${d.getAttribute("cy")}`);
    expect(new Set(dots).size).toBe(7);
    expect(
      screen.getByText(
        `16.8 h, typischer Wert${String.fromCharCode(0x2002)}·${String.fromCharCode(0x2002)}die langsamsten 10 %: über 16.8 h`,
        { normalizer: (s) => s },
      ),
    ).toBeInTheDocument();
  });
});

describe("strip", () => {
  it("draws the backend's scores, worse to the left and red, no undirected row", () => {
    const m = metrics();
    render(
      <Strip
        m={{
          ...m,
          comparison: {
            ...m.comparison,
            lead_time_pre: change("none", null, "incomplete"),
            review_time: change("better", 7200, null, 0.4),
          },
        }}
      />,
    );
    const rows = screen.getAllByTestId("strip-row");
    // freq, lead, lead to intg, deploy success, review, within a day,
    // jammed, three kinds; first touch and open PRs have no direction.
    expect(rows).toHaveLength(10);
    expect(screen.queryByText("Approval ohne Kommentar")).toBeNull();
    expect(screen.queryByText("Offene PRs")).toBeNull();
    expect(screen.getByText("◀ Schlechter")).toBeInTheDocument();

    const lead = rows[1];
    expect(within(lead).getByText("Lead Time bis PROD")).toBeInTheDocument();
    const now = within(lead).getByText("2.0 Tage");
    expect(now).toHaveClass("text-danger-ink");
    expect(now.style.right).toContain("50%");
    const bar = within(lead).getByRole("img", {
      name: "Lead Time bis PROD: 2.0 Tage, Schlechter",
    });
    expect(bar).toHaveAttribute("tabindex", "0");

    const review = rows[4];
    const better = within(review).getByText("2.4 h");
    expect(better).toHaveClass("text-muted");
    expect(better.style.left).toContain("50%");

    expect(within(rows[2]).getByText("Vorperiode unvollständig")).toBeVisible();
    expect(within(rows[5]).getByText("zu wenig Daten")).toBeInTheDocument();
    expect(within(rows[7]).getByText("Erfolgsquote, Releases")).toBeVisible();

    fireEvent.mouseEnter(bar);
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent("Lead Time bis PROD");
    expect(tip).toHaveTextContent("jetzt2.0 Tage");
    expect(tip).toHaveTextContent("Vorperiode1.0 Tage");
    expect(tip).toHaveTextContent("Schlechter");
    expect(bar).toHaveAccessibleDescription(/Vorperiode1\.0 Tage/);
    fireEvent.mouseLeave(bar);
    expect(screen.queryByRole("tooltip")).toBeNull();
    expect(bar).not.toHaveAttribute("aria-describedby");

    // One tooltip at a time: the last hover or focus wins, and only that
    // row points at it.
    const reviewBar = within(rows[4]).getByRole("img");
    fireEvent.focus(bar);
    fireEvent.mouseEnter(reviewBar);
    expect(screen.getAllByRole("tooltip")).toHaveLength(1);
    expect(screen.getByRole("tooltip")).toHaveTextContent("Review-Zeit");
    expect(reviewBar).toHaveAttribute("aria-describedby");
    expect(bar).not.toHaveAttribute("aria-describedby");
    fireEvent.mouseLeave(reviewBar);
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.focus(bar);
    fireEvent.blur(bar);
    expect(screen.queryByRole("tooltip")).toBeNull();

    // A same verdict: a tick on the centre line, no bar on either side.
    const success = rows[3];
    expect(within(success).getByTestId("strip-tick")).toBeInTheDocument();
    expect(within(success).queryByTestId("strip-bar")).toBeNull();
    expect(within(success).getByText("75 %").style.left).toBe(
      "calc(50% + 6px)",
    );
  });

  it("takes its rows from the backend's direction, not the verdict", () => {
    const m = metrics();
    render(
      <Strip
        m={{
          ...m,
          comparison: {
            ...m.comparison,
            deploy_frequency: undirected(0.1),
            within_day: undirected(null, "low_sample"),
            review_time: change("none", null, "low_sample"),
          },
        }}
      />,
    );
    const rows = screen.getAllByTestId("strip-row");
    expect(rows).toHaveLength(8);
    expect(screen.queryByText("Deploy-Frequenz")).toBeNull();
    expect(screen.queryByText("Innert 24 h gemerged")).toBeNull();
    expect(within(rows[3]).getByText("zu wenig Daten")).toBeVisible();
  });

  it("says when Argo CD does not report yet, not too few data", () => {
    const m = metrics();
    render(
      <Strip
        m={{
          ...m,
          comparison: {
            ...m.comparison,
            deploy_frequency: change("none", null, "not_reporting"),
          },
        }}
      />,
    );
    const freq = screen.getAllByTestId("strip-row")[0];
    expect(
      within(freq).getByText("Argo CD meldet für das Team noch nicht"),
    ).toBeVisible();
  });
});

describe("CI hours", () => {
  it("draws each kind's split by outcome as the backend computed it", () => {
    const h = 3600;
    const m = metrics();
    const [release, , feature] = m.build.kinds;
    const link = (
      kind: RunKind,
      outcome: FlowOutcome,
      hours: number,
      ofKind: number,
    ) => ({
      kind,
      outcome,
      seconds: hours * h,
      share_of_kind: ofKind,
      share: hours / 460,
    });
    render(
      <Build
        onFacts={() => {}}
        m={{
          ...m,
          build: {
            ...m.build,
            kinds: [release, feature],
            ci_flow: {
              total_seconds: 460 * h,
              kinds: [
                { kind: "release", seconds: 40 * h, share: 40 / 460 },
                { kind: "feature", seconds: 420 * h, share: 420 / 460 },
              ],
              outcomes: [
                { outcome: "succeeded", seconds: 245 * h, share: 245 / 460 },
                { outcome: "failed", seconds: 150 * h, share: 150 / 460 },
                { outcome: "no_verdict", seconds: 40 * h, share: 40 / 460 },
                { outcome: "outlier", seconds: 25 * h, share: 25 / 460 },
              ],
              links: [
                link("release", "succeeded", 40, 1),
                link("feature", "succeeded", 205, 205 / 420),
                link("feature", "failed", 150, 150 / 420),
                link("feature", "no_verdict", 40, 40 / 420),
                link("feature", "outlier", 25, 25 / 420),
              ],
              repos: [
                { repo: "kasse/kasse-web", seconds: 200 * h, failed: 100 * h },
                { repo: "kasse/kasse-api", seconds: 120 * h, failed: 30 * h },
                { repo: "kasse/policy", seconds: 80 * h, failed: 0 },
              ],
              other_repos: 4,
              other_seconds: 60 * h,
              other_failed: 20 * h,
            },
          },
        }}
      />,
    );
    // The same hours by repo, outliers in: the top three and the rest.
    const byRepo = screen.getByRole("img", { name: /^CI-Zeit nach Repo/ });
    expect(byRepo.getAttribute("aria-label")).toBe(
      "CI-Zeit nach Repo: kasse/kasse-web: 200.0 h, davon 100.0 h fehlgeschlagen; kasse/kasse-api: 120.0 h, davon 30.0 h fehlgeschlagen; kasse/policy: 80.0 h; 4 weitere: 60.0 h, davon 20.0 h fehlgeschlagen",
    );
    // The failed part as a number, not by hatching alone.
    expect(
      screen.getByText(/kasse\/kasse-web 200\.0 h · 100\.0 h rot/),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Nach Repo, über alle Lauf-Arten"),
    ).toBeInTheDocument();
    const chart = screen.getByRole("img", {
      name: /^CI-Zeit von der Lauf-Art zum Ausgang/,
    });
    expect(document.body.textContent).not.toMatch(/NaN/);
    // Failed never by colour alone.
    expect(within(chart).getByText("▼ Fehlgeschlagen")).toBeInTheDocument();
    expect(within(chart).getByText("150.0 h · 33 %")).toBeInTheDocument();
    expect(within(chart).getByText("Ausreisser")).toBeInTheDocument();
    expect(within(chart).getByText("Ohne Ergebnis")).toBeInTheDocument();
    // Outliers included, unlike the card's CI time: said, not silent.
    expect(
      within(chart).getByText("420.0 h inkl. Ausreisser"),
    ).toBeInTheDocument();
    expect(within(chart).getByText("40.0 h")).toBeInTheDocument();
    // The outlier cut as the stuck runs card words it.
    expect(
      screen.getByText(/Ausreisser: Läufe über 6× dem Median ihrer Art/),
    ).toBeInTheDocument();
    const links = screen.getAllByTestId("sankey-link");
    expect(links).toHaveLength(5);
    // The failed stream is red all the way, the others a gradient.
    expect(links[2]).toHaveAttribute("fill", "var(--color-danger)");
    expect(links[1].getAttribute("fill")).toMatch(/^url\(#/);
    const hits = screen.getAllByTestId("sankey-hit");
    fireEvent.mouseEnter(hits[2]);
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent(
      "Feature- und Bugfix-Builds → Fehlgeschlagen",
    );
    expect(tip).toHaveTextContent("Anteil an Feature- und Bugfix-Builds36 %");
    expect(tip).toHaveTextContent("Anteil an aller CI-Zeit33 %");
    fireEvent.mouseLeave(hits[2]);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("keeps a band beside a long outage visible and hoverable", () => {
    const m = metrics();
    const total = 10001 * 3600;
    render(
      <Build
        onFacts={() => {}}
        m={{
          ...m,
          build: {
            ...m.build,
            ci_flow: {
              total_seconds: total,
              kinds: [{ kind: "main", seconds: total, share: 1 }],
              outcomes: [
                { outcome: "failed", seconds: 36, share: 36 / total },
                {
                  outcome: "outlier",
                  seconds: total - 36,
                  share: 1 - 36 / total,
                },
              ],
              links: [
                {
                  kind: "main",
                  outcome: "failed",
                  seconds: 36,
                  share_of_kind: 36 / total,
                  share: 36 / total,
                },
                {
                  kind: "main",
                  outcome: "outlier",
                  seconds: total - 36,
                  share_of_kind: 1 - 36 / total,
                  share: 1 - 36 / total,
                },
              ],
              repos: [],
              other_repos: 0,
              other_seconds: 0,
              other_failed: 0,
            },
          },
        }}
      />,
    );
    // Drawn MIN_BAND (2px) high, answering over 6px.
    const [failed, outlier] = screen.getAllByTestId("sankey-hit");
    expect(failed).toHaveAttribute("stroke-width", "4");
    expect(outlier).toHaveAttribute("stroke-width", "0");
  });

  it("draws nothing without CI time", () => {
    const m = metrics();
    render(
      <Build
        onFacts={() => {}}
        m={{
          ...m,
          build: {
            ...m.build,
            ci_flow: {
              total_seconds: 0,
              kinds: [],
              outcomes: [],
              links: [],
              repos: [],
              other_repos: 0,
              other_seconds: 0,
              other_failed: 0,
            },
          },
        }}
      />,
    );
    expect(screen.queryByText("Wohin die CI-Stunden fliessen")).toBeNull();
  });
});

describe("calendar", () => {
  it("shades the days by the backend's step, weekday by weekday", async () => {
    mockFetch();
    render(<App />);
    // The drawing is for the eye only; the table is what a screen reader
    // gets, saying how many days have no data.
    const cal = await screen.findByTestId("calendar");
    expect(cal).toHaveAttribute("aria-hidden", "true");
    const table = screen.getByRole("table", {
      name: "Releases auf PROD pro Tag; 3 Tage ohne Daten",
    });
    // Rows are weekdays, columns the 4 weeks: Wednesday of KW 46 is the
    // third row's last cell.
    const cells = within(cal).getAllByTestId("day-cell");
    expect(cells).toHaveLength(28);
    expect(cells[2 * 4 + 3]).toHaveClass("bg-seq-1");
    expect(cells[0]).toHaveClass("bg-cell-empty");
    // The tooltip is for the eye too, hidden with the drawing.
    const tip = () => screen.getByRole("tooltip", { hidden: true });
    fireEvent.mouseEnter(cells[2 * 4 + 3]);
    expect(tip()).toHaveTextContent("Mi 11.11.2026 · KW 46Releases1");
    fireEvent.mouseLeave(cells[2 * 4 + 3]);
    // Friday is after now: no data, not no release.
    expect(cells[4 * 4 + 3]).toHaveClass("texture-gap");
    fireEvent.mouseEnter(cells[4 * 4 + 3]);
    expect(tip()).toHaveTextContent("keine Daten");
    expect(within(cal).getAllByTestId("weekday-bar")).toHaveLength(1);
    expect(within(cal).getByText("0.25")).toBeInTheDocument();
    const kw46 = within(table).getByRole("row", { name: /^KW 46/ });
    expect(kw46).toHaveTextContent(
      "KW 460010keine Datenkeine Datenkeine Daten",
    );
    // Per weekday over the whole days with data: today is not over.
    const rate = within(table).getByRole("row", { name: /^pro ganzen Tag/ });
    expect(rate).toHaveTextContent("0.25: 1 an 4 Tagen");
    expect(rate).toHaveTextContent("0.00: 0 an 3 Tagen");
  });

  it("caps the shade at four and more", () => {
    const w = metrics(1).period.weeks;
    const day = (date: string, releases: number, step: 0 | 1 | 2 | 3 | 4) => ({
      date,
      releases,
      step,
      no_data: false,
    });
    render(
      <DayCalendar
        weeks={w}
        days={[
          day("2026-11-09", 7, 4),
          day("2026-11-10", 3, 3),
          day("2026-11-11", 2, 2),
          day("2026-11-12", 0, 0),
          day("2026-11-13", 0, 0),
          day("2026-11-14", 0, 0),
          day("2026-11-15", 0, 0),
        ]}
        weekdays={[7, 3, 2, 0, 0, 0, 0].map((releases) => ({
          releases,
          days: 1,
          per_day: releases,
        }))}
        noDataDays={0}
        prod="PROD"
      />,
    );
    const cells = screen.getAllByTestId("day-cell");
    expect(cells[0]).toHaveClass("bg-seq-4");
    expect(cells[1]).toHaveClass("bg-seq-3");
    fireEvent.mouseEnter(cells[0]);
    const tip = screen.getByRole("tooltip", { hidden: true });
    expect(tip).toHaveTextContent("Mo 09.11.2026");
    expect(tip).toHaveTextContent("Releases7");
    expect(screen.getAllByTestId("weekday-bar")).toHaveLength(3);
    expect(document.body.textContent).not.toMatch(/NaN/);
  });
});

describe("state", () => {
  it("round-trips through the query string", () => {
    // An old link's bots=1 is no part of the view any more.
    const s = parseState("?team=a&product=kasse&w=26&bots=1&q=x&tab=deploys");
    expect(s).toEqual({
      team: "a",
      product: "kasse",
      weeks: 26,
      q: "x",
      tab: "deploys",
    });
    expect(stateQuery(s)).toBe("?team=a&product=kasse&w=26&q=x&tab=deploys");
    expect(parseState("?w=5&tab=nope")).toMatchObject({
      weeks: 4,
      tab: "prs",
      team: null,
    });
    expect(stateQuery(parseState(""))).toBe("");
  });
});
