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
import type { Facts, Metrics } from "./api";
import { Costs } from "./Costs";
import { Flow } from "./Flow";
import { initialLanguage } from "./i18n";
import { parseState, stateQuery } from "./state";
import {
  ago,
  deployFact,
  facts,
  metrics,
  prFact,
  spread,
} from "./test/fixtures";

type Answer = Metrics | Facts | Response;

// The sample team's answers: per product, bots and search as the backend
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
  if (url.searchParams.get("bots") === "1")
    return {
      ...m,
      bots: true,
      review: { ...m.review, merged: 2, untracked: 1, untracked_rate: 0.5 },
    };
  return m;
}

function mockFetch(
  answer: (url: URL) => Answer = sample,
  teams = ["team-a", "team-b"],
) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (raw: string) => {
      calls.push(raw);
      const url = new URL(raw, "http://riptide.test");
      if (url.pathname === "/api/teams")
        return new Response(JSON.stringify({ teams }));
      const a = answer(url);
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
      "Was jetzt?",
      "Throughput und Instability",
      "Wo wartet eine Änderung?",
      "Wie gleichmässig kommt Code nach prod?",
      "Woran arbeitet das Team?",
      "Wie schnell und verlässlich ist das Feedback?",
      "Was kosten die KI-Reviews?",
      "Welche Frameworks Riptide abdeckt",
    ]) {
      expect(screen.getByRole("heading", { name: h })).toBeInTheDocument();
    }
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
    expect(screen.getByText(/Lead Time bis intg/)).toBeInTheDocument();
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
    await screen.findByRole("heading", { name: "Was jetzt?" });
    const whys = screen.getAllByRole("button", { name: "Was heisst das?" });
    fireEvent.click(whys[0]);
    expect(
      screen.getByRole("region", { name: "Was heisst das? · Deploy-Frequenz" }),
    ).toBeInTheDocument();
    expect(screen.getByText("pro revision")).toBeInTheDocument();
    expect(screen.getByText("Genauer hinschauen, wenn")).toBeInTheDocument();
    for (const [i, title] of [
      [1, "Lead Time bis prod"],
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
    await screen.findByRole("heading", { name: "Was jetzt?" });
    expect(screen.getByText("Besser")).toBeInTheDocument();
    expect(screen.getByText("Schlechter")).toBeInTheDocument();
    expect(screen.getAllByText("Unverändert").length).toBeGreaterThan(0);
    expect(screen.getByText("ohne Richtung")).toBeInTheDocument();
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

  it("words the findings and links them to the facts", async () => {
    const calls = mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      m.lead_chain.stages[0].share = 0.6;
      m.build.hotspots.kinds[2].repos = [
        {
          repo: "kasse/flaky-api",
          runs: 20,
          failures: 8,
          failure_rate: 0.4,
          wasted_seconds: 7200,
          duration: spread(20, 600),
          factor: 1,
          slow: false,
          flaky: true,
        },
        {
          repo: "kasse/slow-api",
          runs: 12,
          failures: 0,
          failure_rate: 0,
          wasted_seconds: 0,
          duration: spread(12, 1800),
          factor: 3,
          slow: true,
          flaky: false,
        },
      ];
      m.findings = [
        {
          kind: "stage",
          tone: "attention",
          stage: "coding",
          run_kind: null,
          repo: null,
        },
        {
          kind: "flaky",
          tone: "attention",
          stage: null,
          run_kind: "feature",
          repo: "kasse/flaky-api",
        },
        {
          kind: "slow",
          tone: "attention",
          stage: null,
          run_kind: "feature",
          repo: "kasse/slow-api",
        },
        // a repo the hotspots no longer name: left out, not wrong
        {
          kind: "slow",
          tone: "attention",
          stage: null,
          run_kind: "main",
          repo: "gone",
        },
      ];
      return m;
    });
    render(<App />);
    await screen.findByRole("heading", { name: "Was jetzt?" });
    expect(
      screen.getByText("60 % der Zeit bis prod liegt in der Etappe «Coding»."),
    ).toBeInTheDocument();
    // The thresholds as the backend sent them.
    expect(
      screen.getByText(/ab 50 % der Zeit und 10 PRs \(Startwert\)/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "kasse/flaky-api: 40 % der Feature- und PR-Builds scheitern.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "kasse/slow-api liefert Feedback 3.0× langsamer als das Team.",
      ),
    ).toBeInTheDocument();
    expect(screen.getAllByText("Achtung · Build")).toHaveLength(2);
    expect(screen.queryByText(/gone/)).toBeNull();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Läufe von kasse/flaky-api ansehen →",
      }),
    );
    expect(window.location.search).toBe("?q=kasse%2Fflaky-api&tab=runs");
    await waitFor(() =>
      expect(factsCalls(calls).at(-1)).toMatch(/&q=kasse%2Fflaky-api$/),
    );
    fireEvent.click(screen.getByRole("button", { name: "Zum Fluss →" }));
  });

  it("switches to English and keeps the choice", async () => {
    mockFetch();
    const { unmount } = render(<App />);
    await screen.findByRole("heading", { name: "Was jetzt?" });
    await screen.findByText("KAS-1: Pay by card");
    // A placeholder the call site names differently stays raw on screen.
    const raw = /\{\{|\}\}/;
    expect(document.body.textContent).not.toMatch(raw);
    fireEvent.click(
      screen.getByRole("button", {
        name: "Sprache: Deutsch. Auf Englisch wechseln",
      }),
    );
    expect(
      await screen.findByRole("heading", { name: "What now?" }),
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
      await screen.findByRole("heading", { name: "What now?" }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", {
        name: "Language: English. Switch to German",
      }),
    );
    expect(
      await screen.findByRole("heading", { name: "Was jetzt?" }),
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

  it("says so when no rule fires", async () => {
    mockFetch((url) =>
      url.pathname === "/api/facts" ? facts() : metrics(4, { findings: [] }),
    );
    render(<App />);
    expect(
      await screen.findByText(
        "Keine Auffälligkeiten nach den Regeln, die Riptide anwendet.",
      ),
    ).toBeInTheDocument();
  });

  it("asks for a product and bots, keeps them in the URL", async () => {
    const calls = mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Was jetzt?" });
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
    fireEvent.click(screen.getByRole("button", { name: "Bots einbeziehen" }));
    expect(window.location.search).toBe("?bots=1");
    expect(await screen.findByText(/^\(1 von 2\)/)).toBeInTheDocument();
    expect(metricsCalls(calls).at(-1)).toMatch(/&bots=1$/);
    expect(screen.getAllByText("Bots").length).toBeGreaterThan(0);
  });

  it("switches the window and the team, refetching", async () => {
    const calls = mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Was jetzt?" });
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
    expect(screen.getByText("KAS-1: Pay by card")).toBeInTheDocument();
    expect(screen.getByText("KAS-2 KAS-3 KAS-4")).toBeInTheDocument();
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
    expect(screen.getByText("Erfolgreich")).toBeInTheDocument();
    expect(screen.getByText("Fehlgeschlagen")).toBeInTheDocument();
    expect(screen.getByText("Unstable")).toBeInTheDocument();
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
    // feature builds first: kasse-api broke one run, nothing flagged
    expect(
      within(kinds).getByRole("button", { name: "Feature- und PR-Builds" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("Unauffällig")).toBeInTheDocument();
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
      const h = m.build.hotspots;
      h.kinds[2] = {
        ...h.kinds[2],
        failure_rate: 0.1,
        repos: [
          {
            repo: "kasse/flaky-api",
            runs: 20,
            failures: 8,
            failure_rate: 0.4,
            wasted_seconds: 7200,
            duration: spread(20, 600),
            factor: 1,
            slow: false,
            flaky: true,
          },
          {
            repo: "kasse/slow-api",
            runs: 12,
            failures: 0,
            failure_rate: 0,
            wasted_seconds: 0,
            duration: spread(12, 1800, 2400),
            factor: 3,
            slow: true,
            flaky: false,
          },
        ],
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
    expect(screen.getByText("Fehleranfällig")).toBeInTheDocument();
    expect(screen.getByText(/40 % rot, Team 10 %/)).toBeInTheDocument();
    expect(screen.getByText("Langsam")).toBeInTheDocument();
    expect(screen.getByText(/Team 10 min \(3\.0×\)/)).toBeInTheDocument();
    expect(screen.getByText("2.0 h")).toBeInTheDocument();
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
            bot: [0, 0],
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
      screen.getByRole("heading", { name: "Was jetzt?" }),
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
    await screen.findByRole("heading", { name: "Was jetzt?" });
    fireEvent.click(screen.getByRole("button", { name: "4 W" }));
    await screen.findByText(/KW 43–46/);
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
    const row = (await screen.findByText("KAS-1: Pay by card")).closest("tr")!;
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
    expect(
      await screen.findByText("Argo CD meldet für das Team noch nicht"),
    ).toBeInTheDocument();
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

describe("flow", () => {
  it("draws the shares the backend computed, no stage it left out", () => {
    const zero = { n: 1, p50: 0, p90: 0, low_sample: true, hot: false };
    const m = metrics();
    render(
      <Flow
        why={null}
        onWhy={() => {}}
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
          },
        }}
      />,
    );
    expect(document.body.textContent).not.toMatch(/NaN/);
    expect(screen.getAllByText(/25 % der Zeit/)).toHaveLength(4);
    expect(screen.queryByText(/Bis null/)).toBeNull();
    expect(screen.getByText("gemerged → auf prod")).toBeInTheDocument();
    // No stage is hot: no lever named.
    expect(screen.queryByText(/Der Hebel liegt/)).toBeNull();
  });
});

describe("state", () => {
  it("round-trips through the query string", () => {
    const s = parseState("?team=a&product=kasse&w=26&bots=1&q=x&tab=deploys");
    expect(s).toEqual({
      team: "a",
      product: "kasse",
      weeks: 26,
      bots: true,
      q: "x",
      tab: "deploys",
    });
    expect(stateQuery(s)).toBe(
      "?team=a&product=kasse&w=26&bots=1&q=x&tab=deploys",
    );
    expect(parseState("?w=5&tab=nope")).toMatchObject({
      weeks: 4,
      tab: "prs",
      team: null,
    });
    expect(stateQuery(parseState(""))).toBe("");
  });
});
