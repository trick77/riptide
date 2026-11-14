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
import { LeadChain } from "./LeadChain";
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
});

describe("App", () => {
  it("loads the first team and shows every section", async () => {
    const calls = mockFetch();
    render(<App />);
    // The facts come last: they ask for the product the metrics resolved.
    await screen.findByRole("heading", { name: "Daten" });
    for (const h of [
      "Delivery",
      "Code-Review",
      "Build",
      "Kosten der KI-Reviews",
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
    expect(screen.getByText("1 Releases aus 3 Syncs")).toBeInTheDocument();
    expect(
      screen.getByText("1 fehlgeschlagen von 4 Syncs"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/1 Erstdeploys ohne Vergleichswert/),
    ).toBeInTheDocument();
    expect(screen.getByText("Lead Time intg")).toBeInTheDocument();
    expect(screen.getAllByText(/wenig belastbar/).length).toBeGreaterThan(0);
  });

  it("explains a number on demand, one at a time", async () => {
    mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Delivery" });
    const whys = screen.getAllByRole("button", { name: "Woher?" });
    fireEvent.click(whys[0]);
    expect(
      screen.getByRole("region", { name: "Woher · Deploy-Frequenz" }),
    ).toBeInTheDocument();
    expect(screen.getByText("pro revision")).toBeInTheDocument();
    for (const [i, title] of [
      [1, "Lead Time prod"],
      [2, "Lead Time intg"],
      [3, "Deploy-Erfolg"],
      [4, "PR-Cycle-Time"],
      [5, "Review-Zeit"],
      [6, "Review-Tiefe"],
      [7, "Ohne Jira-Key"],
    ] as const) {
      fireEvent.click(whys[i]);
      expect(
        screen.getByRole("region", { name: `Woher · ${title}` }),
      ).toBeInTheDocument();
      expect(screen.getAllByRole("region", { name: /^Woher/ })).toHaveLength(1);
    }
    fireEvent.click(whys[7]);
    expect(screen.queryByRole("region", { name: /^Woher/ })).toBeNull();
  });

  it("asks for a product and bots, keeps them in the URL", async () => {
    const calls = mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Delivery" });
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
    expect(screen.getByText("Kosten nach Repo")).toBeInTheDocument();

    fireEvent.click(
      within(products).getByRole("button", { name: /Alle Produkte/ }),
    );
    await screen.findByText(/Team team-a, alle Produkte/);
    expect(screen.getByText("0 von 1 gemergten PRs")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Bots einbeziehen" }));
    expect(window.location.search).toBe("?bots=1");
    expect(
      await screen.findByText("1 von 2 gemergten PRs"),
    ).toBeInTheDocument();
    expect(metricsCalls(calls).at(-1)).toMatch(/&bots=1$/);
    expect(screen.getAllByText("Bots").length).toBeGreaterThan(0);
  });

  it("switches the window and the team, refetching", async () => {
    const calls = mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Delivery" });
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
    await screen.findByRole("heading", { name: "Daten" });
    const tabs = screen.getByRole("tablist", { name: "Faktentabelle" });
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
    expect(screen.getByText("UNSTABLE")).toBeInTheDocument();
    expect(screen.getAllByText("Releases").length).toBeGreaterThan(0);
  });

  it("shows tooltips on hover and picks the build kind for hotspots", async () => {
    mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Build" });
    const cols = screen.getAllByTestId("week-col");
    fireEvent.mouseEnter(cols[3]);
    expect(screen.getByRole("tooltip")).toHaveTextContent("KW 46");
    fireEvent.mouseLeave(cols[3]);
    expect(screen.queryByRole("tooltip")).toBeNull();
    const kinds = screen.getByRole("group", { name: "Lauf-Art" });
    // master builds first: one failed run of kasse-api
    expect(screen.getByText("1 / 1")).toBeInTheDocument();
    fireEvent.click(within(kinds).getByRole("button", { name: "Releases" }));
    expect(
      within(kinds).getByRole("button", { name: "Releases" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(
      screen.getByText("Keine fehlgeschlagenen Läufe dieser Art."),
    ).toBeInTheDocument();
    fireEvent.mouseEnter(
      screen.getByRole("img", { name: /Erfolgsquote/ }).parentElement!
        .lastElementChild!.lastElementChild!,
    );
    expect(screen.getByRole("tooltip")).toHaveTextContent("KW 46");
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
      const h = m.build.hotspots;
      h.kinds[1] = {
        ...h.kinds[1],
        durations: [
          {
            repo: "kasse/slow-api",
            runs: 12,
            duration: spread(12, 1800, 2400),
            factor: 3,
            slow: true,
          },
        ],
        failing: [
          {
            repo: "kasse/flaky-api",
            runs: 20,
            failures: 8,
            failure_rate: 0.4,
            wasted_seconds: 7200,
            flaky: true,
          },
        ],
      };
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
        },
      ];
      return m;
    });
    render(<App />);
    await screen.findByRole("heading", { name: "Hotspots" });
    expect(screen.getByText("kasse/slow-api · langsam")).toBeInTheDocument();
    expect(screen.getByText("auffällig")).toBeInTheDocument();
    expect(screen.getByText("8 / 20")).toBeInTheDocument();
    expect(screen.getByText("2.0 h")).toBeInTheDocument();
    expect(screen.getByText("160×")).toBeInTheDocument();
    expect(screen.getByText("#7")).toBeInTheDocument();
  });

  it("asks for the facts of the product the metrics resolved", async () => {
    window.history.replaceState(null, "", "/?product=gone");
    const calls = mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Daten" });
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
    await screen.findByRole("heading", { name: "Code-Review" });
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
      screen.getByRole("heading", { name: "Delivery" }),
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
          sum_p50: null,
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
      await screen.findByText(/Nur Läufe unbekannter Art/),
    ).toBeInTheDocument();
    expect(screen.getByText(/Build-Daten ab 09\.11\.2026/)).toBeInTheDocument();
    expect(
      screen.getByText("Keine gemergten PRs im Zeitraum."),
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
    await screen.findByRole("heading", { name: "Delivery" });
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
    await screen.findByRole("heading", { name: "Daten" });
    const row = screen.getByText("KAS-1: Pay by card").closest("tr")!;
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
    await screen.findByRole("heading", { name: "Build" });
    expect(screen.getByText("+ 3 ohne Ergebnis")).toBeInTheDocument();
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

describe("lead chain", () => {
  it("draws the shares the backend computed, no stage it left out", () => {
    const zero = { n: 1, p50: 0, p90: 0, low_sample: true, hot: false };
    render(
      <LeadChain
        prod="prod"
        chain={{
          stages: (["coding", "review", "merge", "to_prod"] as const).map(
            (key) => ({ key, ...zero, share: 0.25 }),
          ),
          pre_environment: null,
          sum_p50: 0,
          before_prod: 0,
        }}
      />,
    );
    expect(document.body.textContent).not.toMatch(/NaN/);
    expect(screen.getByText(/25 %/)).toBeInTheDocument();
    expect(screen.queryByText(/Bis null/)).toBeNull();
    expect(screen.getByText("gemergt → auf prod")).toBeInTheDocument();
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
