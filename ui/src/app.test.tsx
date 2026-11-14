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
import type { Dataset } from "./api";
import { jiraPrefix } from "./Facts";
import { LeadChain } from "./LeadChain";
import { parseState, stateQuery } from "./state";
import {
  NOW,
  ago,
  dataset,
  deploy,
  leadTime,
  pr,
  review,
  run,
} from "./test/fixtures";

function sample(): Dataset {
  return dataset({
    prs: [
      pr({
        pr_id: 1,
        title: "KAS-1: Pay by card",
        environments: [
          { environment: "intg", first_deployed_at: ago(1.7) },
          { environment: "prod", first_deployed_at: ago(1) },
        ],
      }),
      pr({
        pr_id: 2,
        title: "Bump lib",
        bot: true,
        change_type: "other",
        jira_keys: [],
      }),
      pr({
        pr_id: 3,
        repo: "kasse/kasse-gitops",
        gitops: true,
        title: "kasse PROD 1.1",
        jira_keys: ["KAS-1", "KAS-2", "KAS-3", "KAS-4"],
      }),
      pr({
        pr_id: 4,
        project: "portal",
        repo: "portal/portal-web",
        title: null,
        change_type: "bugfix",
        jira_keys: [],
        state: "open",
        merged_at: null,
      }),
    ],
    deploys: [
      deploy({ revision: "r1", at: ago(1) }),
      deploy({
        revision: "r1",
        app: "kasse-intg",
        environment: "intg",
        at: ago(1.7),
      }),
      deploy({ revision: "r2", kind: "sync", new_images: [], at: ago(8) }),
      deploy({
        revision: "r3",
        app: "kasse-new",
        kind: "first_seen",
        new_images: [],
        at: ago(15),
      }),
      deploy({
        revision: "r4",
        phase: "Failed",
        kind: null,
        new_images: [],
        at: ago(2),
      }),
    ],
    lead_times: [
      leadTime({ lead_time_s: 2 * 86400 }),
      leadTime({ lead_time_s: 3 * 3600, environment: "intg" }),
      leadTime({ lead_time_s: 600, bot: true }),
    ],
    runs: [
      run({
        build_type: "release",
        run_id: "10",
        image_refs: ["reg/kasse-api:1.1"],
      }),
      run({ run_id: "11", status: "FAILURE", duration_s: 900 }),
      run({
        run_id: "12",
        branch_kind: "feature",
        pipeline_name: "kasse-api/feature%2Fx",
        status: "UNSTABLE",
      }),
    ],
    reviews: [
      review(),
      review({
        pr_key: "kasse/kasse-api#9",
        outcome: "declined",
        cost_usd: "0.25",
      }),
    ],
  });
}

function mockFetch(
  ds: Dataset | (() => Promise<Response>),
  teams = ["team-a", "team-b"],
) {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      calls.push(url);
      if (url.startsWith("/api/teams"))
        return new Response(JSON.stringify({ teams }));
      if (typeof ds === "function") return ds();
      return new Response(JSON.stringify(ds));
    }),
  );
  return calls;
}

beforeEach(() => {
  window.history.replaceState(null, "", "/");
});
afterEach(() => {
  vi.unstubAllGlobals();
});

const now = () => NOW;

describe("App", () => {
  it("loads the first team and shows every section", async () => {
    const calls = mockFetch(sample());
    render(<App now={now} />);
    await screen.findByRole("heading", { name: "Delivery" });
    for (const h of [
      "Code-Review",
      "Build",
      "Kosten der KI-Reviews",
      "Daten",
    ]) {
      expect(screen.getByRole("heading", { name: h })).toBeInTheDocument();
    }
    expect(calls[1]).toMatch(/^\/api\/dataset\?team=team-a&from=2026-10-1/);
    expect(
      screen.getByText(/Team team-a, alle Produkte · KW 43–46/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Datenstand: Bitbucket 12\.11\./),
    ).toBeInTheDocument();
    // one release, one sync, one first deploy; the failed sync apart
    expect(screen.getByText("1 Releases aus 3 Syncs")).toBeInTheDocument();
    expect(
      screen.getByText("1 fehlgeschlagen von 4 Syncs"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/1 Erstdeploys ohne Vergleichswert/),
    ).toBeInTheDocument();
    expect(screen.getByText("Lead Time intg")).toBeInTheDocument();
  });

  it("explains a number on demand, one at a time", async () => {
    mockFetch(sample());
    render(<App now={now} />);
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

  it("filters by product, counts bots in on request, keeps it in the URL", async () => {
    mockFetch(sample());
    render(<App now={now} />);
    await screen.findByRole("heading", { name: "Delivery" });
    const products = screen.getByRole("group", { name: "Produkt" });
    fireEvent.click(within(products).getByRole("button", { name: /portal/ }));
    expect(window.location.search).toBe("?product=portal");
    expect(screen.getByText(/Team team-a, Produkt portal/)).toBeInTheDocument();
    expect(screen.getByText("Argo-Apps")).toBeInTheDocument();
    expect(screen.getByText("keine")).toBeInTheDocument();
    expect(screen.getByText("keine Deploys im Zeitraum")).toBeInTheDocument();
    expect(
      screen.getByText("Keine abgeschlossenen Läufe im Zeitraum."),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Keine noergler-Reviews im Zeitraum."),
    ).toBeInTheDocument();

    fireEvent.click(
      within(products).getByRole("button", { name: /Alle Produkte/ }),
    );
    expect(screen.getByText("0 von 1 gemergten PRs")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Bots einbeziehen" }));
    expect(window.location.search).toBe("?bots=1");
    // the bot's PR carries no Jira key
    expect(screen.getByText("1 von 2 gemergten PRs")).toBeInTheDocument();
    expect(screen.getAllByText("Bots").length).toBeGreaterThan(0);
  });

  it("switches the window and the team, refetching", async () => {
    const calls = mockFetch(sample());
    render(<App now={now} />);
    await screen.findByRole("heading", { name: "Delivery" });
    fireEvent.click(screen.getByRole("button", { name: "12 W" }));
    await waitFor(() => expect(calls).toHaveLength(3));
    expect(calls[2]).toMatch(/from=2026-08-2/);
    await screen.findByText(/KW 35–46/);
    fireEvent.change(screen.getByLabelText("Team"), {
      target: { value: "team-b" },
    });
    await waitFor(() => expect(calls[3]).toMatch(/team=team-b/));
    expect(window.location.search).toBe("?team=team-b&w=12");
  });

  it("searches the facts and switches tables", async () => {
    mockFetch(sample());
    render(<App now={now} />);
    await screen.findByRole("heading", { name: "Daten" });
    const tabs = screen.getByRole("tablist", { name: "Faktentabelle" });
    expect(within(tabs).getByRole("tab", { name: /PRs 3/ })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    expect(
      screen.getByText("Pay by card", { exact: false }),
    ).toBeInTheDocument();
    expect(screen.getByText("Promotion")).toBeInTheDocument();
    expect(screen.getByText("#4")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/Suche nach/), {
      target: { value: "portal" },
    });
    expect(
      within(tabs).getByRole("tab", { name: /PRs 1/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("gefiltert nach «portal»")).toBeInTheDocument();
    fireEvent.click(within(tabs).getByRole("tab", { name: /Deploys/ }));
    expect(screen.getByText("Keine Treffer für «portal».")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/Suche nach/), {
      target: { value: "" },
    });
    expect(screen.getByText("Erstdeploy")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getAllByText("kasse-api:1.1").length).toBeGreaterThan(0);
    fireEvent.click(within(tabs).getByRole("tab", { name: /Läufe/ }));
    expect(window.location.search).toBe("?tab=runs");
    expect(screen.getByText("UNSTABLE")).toBeInTheDocument();
    expect(screen.getAllByText("Releases").length).toBeGreaterThan(0);
  });

  it("shows tooltips on hover and picks the build kind for durations", async () => {
    mockFetch(sample());
    render(<App now={now} />);
    await screen.findByRole("heading", { name: "Build" });
    const cols = screen.getAllByTestId("week-col");
    fireEvent.mouseEnter(cols[3]);
    expect(screen.getByRole("tooltip")).toHaveTextContent("KW 46");
    fireEvent.mouseLeave(cols[3]);
    expect(screen.queryByRole("tooltip")).toBeNull();
    const kinds = screen.getByRole("group", { name: "Lauf-Art" });
    fireEvent.click(within(kinds).getByRole("button", { name: "Releases" }));
    expect(
      within(kinds).getByRole("button", { name: "Releases" }),
    ).toHaveAttribute("aria-pressed", "true");
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

  it("reports a failed load with the server's detail", async () => {
    mockFetch(
      async () =>
        new Response(JSON.stringify({ detail: "unknown team" }), {
          status: 404,
        }),
    );
    render(<App now={now} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "404: unknown team",
    );
  });

  it("keeps a non-JSON error's status text", async () => {
    mockFetch(
      async () =>
        new Response("<html>", { status: 502, statusText: "Bad Gateway" }),
    );
    render(<App now={now} />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "502: Bad Gateway",
    );
  });

  it("says so when the config has no team", async () => {
    mockFetch(sample(), []);
    render(<App now={now} />);
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
    render(<App now={now} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("offline");
  });

  it("prints", async () => {
    mockFetch(sample());
    const print = vi.fn();
    vi.stubGlobal("print", print);
    render(<App now={now} />);
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /Drucken/ }));
    });
    expect(print).toHaveBeenCalled();
  });

  it("explains a build section with only unknown runs", async () => {
    mockFetch(
      dataset({
        runs: [run({ build_type: null })],
        meta: {
          ...dataset().meta,
          build_type_from: null,
          build_data_from: ago(3),
        },
      }),
    );
    render(<App now={now} />);
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
    mockFetch(async () => {
      n++;
      return n === 1
        ? new Response(JSON.stringify({ detail: "boom" }), { status: 500 })
        : new Response(JSON.stringify(sample()));
    });
    render(<App now={now} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("boom");
    fireEvent.click(screen.getByRole("button", { name: "12 W" }));
    await screen.findByRole("heading", { name: "Delivery" });
    fireEvent.click(screen.getByRole("button", { name: "4 W" }));
    await screen.findByText(/KW 43–46/);
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("finds a deploy by any image it ran, shows no negative wait", async () => {
    const ds = sample();
    ds.prs[0].approved_at = ago(2.5); // before ready_at: not a wait
    mockFetch(ds);
    render(<App now={now} />);
    await screen.findByRole("heading", { name: "Daten" });
    const row = screen.getByText("KAS-1: Pay by card").closest("tr")!;
    expect(within(row).getAllByText("–").length).toBeGreaterThan(0);
    expect(within(row).queryByText("0 s")).toBeNull();
    fireEvent.change(screen.getByLabelText(/Suche nach/), {
      target: { value: "kasse-api:1.1" },
    });
    const tabs = screen.getByRole("tablist", { name: "Faktentabelle" });
    // the release, the sync and the failed sync ran it; only one brought it
    expect(
      within(tabs).getByRole("tab", { name: /Deploys 5/ }),
    ).toBeInTheDocument();
  });

  it("tells a team Argo CD does not report for from one without deploys", async () => {
    const ds = sample();
    ds.deploys = [];
    ds.meta.data_start.argocd = null;
    mockFetch(ds);
    render(<App now={now} />);
    expect(
      await screen.findByText("Argo CD meldet für das Team noch nicht"),
    ).toBeInTheDocument();
  });

  it("notes when Argo CD started inside the window", async () => {
    const ds = sample();
    ds.meta.data_start.argocd = ago(5);
    mockFetch(ds);
    render(<App now={now} />);
    expect(
      await screen.findByText(/Argo CD meldet erst seit 07\.11\.2026/),
    ).toBeInTheDocument();
  });

  it("treats a blank search as none", async () => {
    mockFetch(sample());
    render(<App now={now} />);
    await screen.findByRole("heading", { name: "Daten" });
    fireEvent.change(screen.getByLabelText(/Suche nach/), {
      target: { value: "   " },
    });
    expect(screen.queryByText(/gefiltert nach/)).toBeNull();
  });
});

describe("lead chain", () => {
  const zero = { n: 1, p50: 0, p90: 0 };
  it("splits evenly when every median is zero, never NaN", () => {
    render(
      <LeadChain
        prod="prod"
        chain={{
          stages: (
            ["coding", "review", "merge", "toPre", "toProd"] as const
          ).map((key) => ({ key, spread: zero })),
          preEnvironment: null,
          approved: 1,
          approvedOnFirstTouch: 1,
        }}
      />,
    );
    expect(document.body.textContent).not.toMatch(/NaN/);
    expect(screen.getByText(/25 %/)).toBeInTheDocument();
    // no stage before production: no row for it, prod counted from the merge
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

describe("jira prefix", () => {
  it("leaves out keys the title shows, caps the rest", () => {
    expect(
      jiraPrefix(pr({ title: "KAS-1: x", jira_keys: ["KAS-1", "KAS-2"] })),
    ).toBe("KAS-2");
    expect(
      jiraPrefix(
        pr({ title: null, jira_keys: ["A-1", "A-2", "A-3", "A-4", "A-5"] }),
      ),
    ).toBe("A-1 A-2 A-3 +2");
  });
});
