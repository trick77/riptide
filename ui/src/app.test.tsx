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
  Signal,
} from "./api";
import { Build } from "./Build";
import { DayCalendar } from "./calendar";
import { Costs } from "./Costs";
import { ciHours, dur, fullDate, num } from "./format";
import { Flow } from "./Flow";
import { stepLook } from "./heatmap";
import { initialLanguage } from "./i18n";
import { keepLabels } from "./marimekko";
import { parseState, stateQuery } from "./state";
import { Strip } from "./strip";
import { BatchTile, MergedTile, ThroughputCard } from "./Throughput";
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
    // Per source since when and until when it reports, as data.
    const asOf = screen.getByText(
      (_, el) =>
        el?.tagName === "P" && /^Datenstand: /.test(el.textContent ?? ""),
    );
    expect(asOf.textContent).toMatch(
      /^Datenstand: Bitbucket seit 13\.09\.2026, zuletzt 12\.11\. \d\d:\d\d · CI seit 02\.11\.2026, zuletzt /,
    );
    expect(screen.queryByText(/Ereignisse gingen verloren/)).toBeNull();
    expect(screen.getByText("1 Release im Zeitraum")).toBeInTheDocument();
    expect(
      screen.getByText("1 von 4 Deploys fehlgeschlagen"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/^1 Erstdeploy aus den ersten 14 Tagen/),
    ).toBeInTheDocument();
    expect(screen.getByText(/Lead Time bis INTG:/)).toBeInTheDocument();
    expect(screen.getAllByText(/wenig belastbar/).length).toBeGreaterThan(0);
    // The DORA names, and the side Riptide cannot measure.
    expect(
      screen.getByText("DORA · Change lead time · Throughput"),
    ).toBeInTheDocument();
    expect(screen.getByText("nicht gemessen")).toBeInTheDocument();
    // The mean is said to be one; the line is the typical week.
    expect(screen.getByText("typische Woche: 1.0")).toBeInTheDocument();
    expect(
      screen.getAllByText("Releases pro Woche, im Schnitt").length,
    ).toBeGreaterThan(0);
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
    expect(screen.getAllByText("Besser").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Schlechter").length).toBeGreaterThan(0);
    // One form everywhere: the previous value under the number, the
    // verdict at the foot; never "Vorperiode X → jetzt Y" again.
    expect(document.body.textContent).not.toMatch(/Vorperiode [^→]* → jetzt/);
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

  it("counts the running week apart and finds its rows", async () => {
    const calls = mockFetch((url) => {
      if (url.pathname !== "/api/facts") return metrics();
      return facts({
        deploys: {
          count: 2,
          rows: [deployFact({ running: true }), deployFact({ revision: "r2" })],
        },
      });
    });
    render(<App />);
    await screen.findByRole("heading", { name: "Einschätzung" });
    // The period's days end on the last closed week's Sunday, the previous
    // period's too: a window's end is the next Monday 00:00.
    const m = metrics();
    const scope = screen.getByText(/verglichen mit den 4 Wochen davor/);
    expect(scope.textContent).toContain(
      `bis 15.11.2026 · verglichen mit den 4 Wochen davor, ${fullDate(new Date(m.comparison.from))} bis 18.10.2026`,
    );
    const line = screen.getByText(/^Laufende Woche KW/);
    expect(line.textContent).toBe(
      `Laufende Woche KW ${m.running_week.week.kw} bis jetzt: 1 Release, 2 PRs gemerged, 1 Build. Zählt in keiner Zahl dieser Seite mit; nur die offenen PRs sind Stand jetzt. Ansehen →`,
    );
    // Its link searches the facts for the mark the rows carry.
    fireEvent.click(within(line).getByRole("button", { name: "Ansehen →" }));
    await waitFor(() =>
      expect(factsCalls(calls).at(-1)).toMatch(
        /&q=Laufende\+Woche$|&q=Laufende%20Woche$/,
      ),
    );
    const data = within(document.getElementById("daten")!);
    fireEvent.click(data.getByRole("tab", { name: /Deploys/ }));
    expect(await data.findByText("Laufende Woche")).toBeInTheDocument();
    expect(
      data.getByText(/Zeilen mit «Laufende Woche» zählen in keiner Zahl mit/),
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
    // The weeks, the stamp and the next scheduled run, in one line; no
    // "without the running week" aside: the whole page reads closed weeks.
    const stampLine = p.getByText(/^KW \d+–\d+ · \d\d\.\d\d\.–\d\d\.\d\d\. · /);
    expect(stampLine.textContent).toMatch(
      /^KW \d+–\d+ · \d\d\.\d\d\.–\d\d\.\d\d\. · Einschätzung vom .*, automatisch · nächste: /,
    );
    expect(p.queryByTitle("ohne die laufende Woche")).toBeNull();
    // The gate once, as a limit with its date in the weeks' zone.
    const limits = p.getByRole("note", {
      name: "Was diese Einschätzung einschränkt",
    });
    expect(limits.textContent).toBe(
      "Argo CD lieferte seit dem 06.11.2026 bis zum Ende des Zeitraums keine Daten mehr: Aussagen dazu ausgelassen.",
    );
    // The tally beside the headline, the backend's counts, what is stuck
    // first, the question, then the repos: none called unremarkable.
    const tally = within(p.getByRole("list", { name: "Bilanz" }));
    expect(tally.getAllByRole("listitem").map((li) => li.textContent)).toEqual([
      "4hakt",
      "1läuft",
      "1erledigt",
      "1zum Besprechen",
      "8 von 11aktiven Repos ohne Befund",
      // Apart from the cards of what cannot be judged ("nicht
      // beurteilbar"): repos too thin to judge.
      "1Repo mit zu wenig Daten",
    ]);
    expect(panel.textContent).not.toMatch(
      /unauffällig|fällt nichts auf|Nichts hakt/,
    );
    // Whose they are: the team or one product.
    expect(p.getByText("Team, alle Produkte")).toBeInTheDocument();
    // Two groups: what is stuck, then what runs with its evidence.
    expect(
      p
        .getAllByRole("heading", { level: 4 })
        .slice(0, 2)
        .map((h) => h.textContent),
    ).toEqual(["Hakt", "Läuft"]);
    // The active repos, as the tally counts them; the idle one apart.
    expect(
      p.getByText(
        "Von 11 aktiven Repos: 8 ohne Befund, 1 mit zu wenig Daten für ein Urteil. 1 Repo ohne Aktivität, das in der Vorperiode aktiv war.",
      ),
    ).toBeInTheDocument();
    // The focus below, ranked, the placeholders filled with the backend's
    // numbers.
    expect(p.getByText("Im Fokus")).toBeInTheDocument();
    const api = p.getByRole("heading", { name: "kasse/kasse-api" });
    expect(api.previousElementSibling?.textContent).toBe("1");
    expect(
      p.getByRole("heading", { name: "kasse/kasse-web" }).previousElementSibling
        ?.textContent,
    ).toBe("2");
    expect(
      p.getByText("5 PRs liegen, und Reviews dauern."),
    ).toBeInTheDocument();
    expect(
      p.getByText("Der Feature-Build braucht jetzt 24 min."),
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
    expect(
      p.getByText("kasse/policy (scheitert öfter): erledigt"),
    ).toBeInTheDocument();
    // A resolution names no next step: only the gain says what helped.
    expect(p.getAllByText(/benennen, was geholfen hat/)).toHaveLength(1);
    // What no wording covers is still spotted.
    expect(
      p.getByText("Feature- und Bugfix-Builds warten auf Build-Agents"),
    ).toBeInTheDocument();
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
        "64 % Approvals ohne vorherigen Kommentar: Passt das zu unserer Art zu reviewen?",
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

  it("drops an ask's answer for a view left meanwhile", async () => {
    const calls = mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return analysisStatus({ state: "none", enabled: true });
      return metrics();
    });
    // The POST answers only when the test says so: after the switch.
    const inner = globalThis.fetch;
    let release: (r: Response) => void = () => {};
    vi.stubGlobal(
      "fetch",
      vi.fn((raw: string, init?: RequestInit) =>
        init?.method === "POST"
          ? new Promise<Response>((r) => {
              release = r;
            })
          : inner(raw, init),
      ),
    );
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    fireEvent.click(screen.getByRole("button", { name: "12 W" }));
    await waitFor(() =>
      expect(
        calls.some((c) => c.startsWith("/api/analysis") && /weeks=12/.test(c)),
      ).toBe(true),
    );
    await screen.findByTestId("assessment");
    await act(async () =>
      release(
        new Response(
          JSON.stringify(
            analysisStatus({ state: "none", enabled: true, running: true }),
          ),
        ),
      ),
    );
    // The 12-week view stays: no skeleton nothing would ever replace.
    expect(screen.queryByTestId("assessment-loading")).toBeNull();
    expect(
      within(screen.getByTestId("assessment")).getByRole("button", {
        name: "Jetzt einschätzen",
      }),
    ).toBeEnabled();
  });

  it("says an ask failed for its view only", async () => {
    const calls = mockFetch((url, init) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        if (init?.method === "POST")
          return new Response(JSON.stringify({ detail: "cooling down" }), {
            status: 429,
          });
        return analysisStatus({ state: "none", enabled: true });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    expect(
      await p.findByText(/Einschätzung nicht gestartet: .*cooling down/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "12 W" }));
    await waitFor(() =>
      expect(
        calls.some((c) => c.startsWith("/api/analysis") && /weeks=12/.test(c)),
      ).toBe(true),
    );
    // The 12-week panel itself, not the skeleton before it.
    await screen.findByTestId("assessment");
    expect(
      screen.queryByText(/Einschätzung nicht gestartet/),
    ).not.toBeInTheDocument();
  });

  it("keeps a view's ask failure past a late one of another view", async () => {
    const calls = mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return analysisStatus({ state: "none", enabled: true });
      return metrics();
    });
    // The 4-week POST hangs until released; the 12-week one fails at once.
    const inner = globalThis.fetch;
    let release: (r: Response) => void = () => {};
    vi.stubGlobal(
      "fetch",
      vi.fn((raw: string, init?: RequestInit) => {
        if (init?.method !== "POST") return inner(raw, init);
        if (/weeks=12/.test(raw))
          return Promise.resolve(
            new Response(JSON.stringify({ detail: "cooling down" }), {
              status: 429,
            }),
          );
        return new Promise<Response>((r) => {
          release = r;
        });
      }),
    );
    render(<App />);
    let p = within(await screen.findByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    fireEvent.click(screen.getByRole("button", { name: "12 W" }));
    await waitFor(() =>
      expect(
        calls.some((c) => c.startsWith("/api/analysis") && /weeks=12/.test(c)),
      ).toBe(true),
    );
    p = within(await screen.findByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    await p.findByText(/Einschätzung nicht gestartet: .*cooling down/);
    await act(async () =>
      release(
        new Response(JSON.stringify({ detail: "boom" }), { status: 500 }),
      ),
    );
    expect(
      p.getByText(/Einschätzung nicht gestartet: .*cooling down/),
    ).toBeInTheDocument();
  });

  it("drops an ask failure once its view's status reloads", async () => {
    let gets = 0;
    const calls = mockFetch((url, init) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        if (init?.method === "POST")
          return new Response(JSON.stringify({ detail: "already running" }), {
            status: 409,
          });
        gets++;
        return analysisStatus({ state: "none", enabled: true });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    expect(
      await p.findByText(/Einschätzung nicht gestartet: .*already running/),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "12 W" }));
    await waitFor(() =>
      expect(
        calls.some((c) => c.startsWith("/api/analysis") && /weeks=12/.test(c)),
      ).toBe(true),
    );
    await screen.findByTestId("assessment");
    const before = gets;
    fireEvent.click(screen.getByRole("button", { name: "4 W" }));
    // Back on the 4-week view with its status loaded anew.
    await waitFor(() => expect(gets).toBeGreaterThan(before));
    await screen.findByTestId("assessment");
    expect(
      screen.queryByText(/Einschätzung nicht gestartet/),
    ).not.toBeInTheDocument();
  });

  it("asks once on a double click", async () => {
    let posts = 0;
    const calls = mockFetch((url, init) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        if (init?.method === "POST" && ++posts > 1)
          return new Response(JSON.stringify({ detail: "already running" }), {
            status: 409,
          });
        return analysisStatus({
          state: "none",
          enabled: true,
          running: posts > 0,
        });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    const button = p.getByRole("button", { name: "Jetzt einschätzen" });
    fireEvent.click(button);
    fireEvent.click(button);
    // Running from the click on, before the POST answers.
    expect(p.getByRole("button", { name: "läuft …" })).toBeDisabled();
    expect(await p.findByText(/läuft, meist unter einer Minute/)).toBeVisible();
    expect(
      calls.filter((c) => c.startsWith("POST /api/analysis")),
    ).toHaveLength(1);
    expect(p.queryByText(/Einschätzung nicht gestartet/)).toBeNull();
  });

  // Back on a view before the other one loaded, the page still shows the
  // view's last status while its new one is on its way.
  async function backBeforeLoad(): Promise<(r: Response) => void> {
    const inner = globalThis.fetch;
    const held: { raw: string; release: (r: Response) => void }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn((raw: string, init?: RequestInit) =>
        init?.method !== "POST" && raw.startsWith("/api/analysis")
          ? new Promise<Response>((release) => held.push({ raw, release }))
          : inner(raw, init),
      ),
    );
    const fourWeeks = (h: { raw: string }) =>
      new URL(h.raw, "http://riptide.test").searchParams.get("weeks") === "4";
    fireEvent.click(screen.getByRole("button", { name: "12 W" }));
    fireEvent.click(screen.getByRole("button", { name: "4 W" }));
    await waitFor(() => expect(held.some(fourWeeks)).toBe(true));
    return held.find(fourWeeks)!.release;
  }

  it("reloads the status after an ask that found a run going", async () => {
    let started = false;
    mockFetch((url, init) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        // Someone else's run started meanwhile: the POST finds it going.
        if (init?.method === "POST") {
          started = true;
          return new Response(JSON.stringify({ detail: "already running" }), {
            status: 409,
          });
        }
        return analysisStatus({
          state: "none",
          enabled: true,
          running: started,
        });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    expect(await p.findByText(/läuft, meist unter einer Minute/)).toBeVisible();
    expect(p.getByRole("button", { name: "läuft …" })).toBeDisabled();
    // Its own reload keeps the box: it says why nothing was started.
    expect(
      p.getByText(/Einschätzung nicht gestartet: .*already running/),
    ).toBeInTheDocument();
  });

  it("keeps an ask failure past a status sent before it", async () => {
    mockFetch((url, init) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        if (init?.method === "POST")
          return new Response(JSON.stringify({ detail: "already running" }), {
            status: 409,
          });
        return analysisStatus({ state: "none", enabled: true });
      }
      return metrics();
    });
    render(<App />);
    await screen.findByTestId("assessment");
    const release = await backBeforeLoad();
    const p = within(screen.getByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    await p.findByText(/Einschätzung nicht gestartet: .*already running/);
    await act(async () =>
      release(
        new Response(
          JSON.stringify(analysisStatus({ state: "none", enabled: true })),
        ),
      ),
    );
    expect(
      p.getByText(/Einschätzung nicht gestartet: .*already running/),
    ).toBeInTheDocument();
  });

  it("keeps an ask's run past a status sent before it", async () => {
    let asked = false;
    mockFetch((url, init) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        if (init?.method === "POST") asked = true;
        return analysisStatus({ state: "none", enabled: true, running: asked });
      }
      return metrics();
    });
    render(<App />);
    await screen.findByTestId("assessment");
    const release = await backBeforeLoad();
    const p = within(screen.getByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    await p.findByText(/läuft, meist unter einer Minute/);
    // The status asked before the POST says it is not running.
    await act(async () =>
      release(
        new Response(
          JSON.stringify(analysisStatus({ state: "none", enabled: true })),
        ),
      ),
    );
    expect(p.getByText(/läuft, meist unter einer Minute/)).toBeInTheDocument();
    expect(p.getByRole("button", { name: "läuft …" })).toBeDisabled();
  });

  it("shows a view's late ask failure past another view's", async () => {
    const calls = mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return analysisStatus({ state: "none", enabled: true });
      return metrics();
    });
    // The 4-week POST hangs until released; the 12-week one fails at once.
    const inner = globalThis.fetch;
    let release: (r: Response) => void = () => {};
    vi.stubGlobal(
      "fetch",
      vi.fn((raw: string, init?: RequestInit) => {
        if (init?.method !== "POST") return inner(raw, init);
        if (/weeks=12/.test(raw))
          return Promise.resolve(
            new Response(JSON.stringify({ detail: "cooling down" }), {
              status: 429,
            }),
          );
        return new Promise<Response>((r) => {
          release = r;
        });
      }),
    );
    render(<App />);
    let p = within(await screen.findByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    fireEvent.click(screen.getByRole("button", { name: "12 W" }));
    await waitFor(() =>
      expect(
        calls.some((c) => c.startsWith("/api/analysis") && /weeks=12/.test(c)),
      ).toBe(true),
    );
    p = within(await screen.findByTestId("assessment"));
    fireEvent.click(p.getByRole("button", { name: "Jetzt einschätzen" }));
    await p.findByText(/Einschätzung nicht gestartet: .*cooling down/);
    fireEvent.click(screen.getByRole("button", { name: "4 W" }));
    // The 4-week panel with its status loaded anew, its POST still out.
    p = within(await screen.findByTestId("assessment"));
    await act(async () =>
      release(
        new Response(JSON.stringify({ detail: "boom" }), { status: 500 }),
      ),
    );
    expect(
      p.getByText(/Einschätzung nicht gestartet: .*boom/),
    ).toBeInTheDocument();
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
      prFact({
        pr_id: 11,
        title: "a",
        prod_at: null,
        to_prod_s: null,
        prod_state: "not_yet",
        prod_route: null,
      }),
      prFact({ pr_id: 12, title: "b", change_type: "other" }),
      prFact({ pr_id: 13, title: "c", change_type: null }),
      prFact({ pr_id: 14, title: "d", change_type: "spike" }),
      // Placed through a lower bound: its time is at most that.
      prFact({
        pr_id: 15,
        title: "e",
        prod_lower_bound: true,
        prod_route: "image",
      }),
      prFact({
        pr_id: 16,
        title: "f",
        prod_at: null,
        to_prod_s: null,
        prod_state: "not_placeable",
        prod_reason: "in_silence",
        prod_route: null,
      }),
      prFact({
        pr_id: 17,
        title: "g",
        prod_at: null,
        to_prod_s: null,
        prod_state: null,
        prod_route: null,
        library: true,
      }),
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
    expect(
      (await row("a")).getByText("noch nicht auf PROD"),
    ).toBeInTheDocument();
    expect((await row("b")).getByText("Anderes")).toBeInTheDocument();
    expect((await row("c")).getAllByText("–").length).toBeGreaterThan(0);
    // a prefix the UI does not know yet shows as the backend sent it
    expect((await row("d")).getByText("spike")).toBeInTheDocument();
    // On PROD: the time, the route and the day in its tooltip; through a
    // lower bound "höchstens", as it may have gone out earlier.
    const onProd = (await row("b")).getByTitle(/^Auf PROD seit /);
    expect(onProd.title).toMatch(/Compare-Link/);
    expect(onProd.textContent).toBe(dur(0.8 * 86400));
    const atMost = (await row("e")).getByTitle(/^Auf PROD seit /);
    expect(atMost.textContent).toBe(`höchstens ${dur(0.8 * 86400)}`);
    expect(atMost.title).toMatch(/gebaut hat\. Höchstens: /);
    // Not placeable, with the reason in plain words.
    expect((await row("f")).getByText("nicht zuordenbar")).toBeInTheDocument();
    expect(
      (await row("f")).getByText("eine Lücke in den Daten"),
    ).toBeInTheDocument();
    // A library's PR is never rolled out: no "not yet".
    expect(
      (await row("g")).getByText("Bibliothek, wird nicht ausgerollt"),
    ).toBeInTheDocument();
  });

  it("says what all wait on", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const a = assessment();
        const l = analysisStatus().layout;
        return analysisStatus({
          enabled: true,
          // Without an earlier assessment nothing can be resolved.
          layout: {
            ...l,
            cards: l.cards.filter((c) => c.column !== "done"),
            tally: { ...l.tally, done: 0 },
          },
          assessment: {
            ...a,
            compared: false,
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
    expect(p.getByText("alle 3 warten auf Änderungen")).toBeInTheDocument();
    // The gain alone is evidence enough: the group runs.
    expect(p.getByRole("heading", { name: "Läuft" })).toBeInTheDocument();
    expect(p.queryByText("erledigt")).toBeNull();
  });

  // Without a gain or a resolution no group says "runs": "wenn nichts
  // läuft, ist läuft gut übertrieben" (user). The foot says why: no gain
  // that counts where one could be told; without a previous period to
  // compare with, the limits say that instead, never "no gain". With the
  // model's words or the rules', the same.
  it.each([
    ["the rules' words", false],
    ["the model's words", true],
  ])(
    "says nothing runs without evidence of what runs, with %s",
    async (_, worded) => {
      mockFetch((url) => {
        if (url.pathname === "/api/facts") return facts();
        if (url.pathname === "/api/analysis") {
          const a = assessment();
          const base = worded ? analysedStatus() : analysisStatus();
          const keep = (s: { tone: string }) => s.tone !== "good";
          return {
            ...base,
            assessment: {
              ...a,
              signals: a.signals.filter(keep),
              limits: [],
              quiet_reasons: worded
                ? ["findings"]
                : ["no_baseline", "findings"],
              limited: !worded,
            },
            layout: {
              ...base.layout,
              cards: base.layout.cards.filter((c) => c.column === "bad"),
              // The backend's tally: its gains it never had.
              tally: { good: 0, done: 0, bad: 4, hint: 0, limit: 0, talk: 1 },
            },
          };
        }
        return metrics();
      });
      render(<App />);
      const p = within(await screen.findByTestId("assessment"));
      expect(p.queryByRole("heading", { name: "Läuft" })).toBeNull();
      expect(
        p.getByText(
          "Von 11 aktiven Repos: 8 ohne Befund, 1 mit zu wenig Daten für ein Urteil. 1 Repo ohne Aktivität, das in der Vorperiode aktiv war.",
        ),
      ).toBeInTheDocument();
      const noGain = p.queryByText("Keine Verbesserung, die als Beleg zählt.");
      const noBaseline = p.queryByText(
        "Noch kein voller Vergleich mit der Vorperiode: Nicht jede Zahl hat dort einen vergleichbaren Wert.",
      );
      expect([noGain != null, noBaseline != null]).toEqual([worded, !worded]);
      expect(
        within(p.getByRole("list", { name: "Bilanz" })).queryByText("läuft"),
      ).toBeNull();
    },
  );

  // States: a red card, a hint in its own neutral group and what could not
  // be judged apart, each with k of n, the worst three with their number,
  // how many more, since when and the team's threshold, all the backend's.
  // A kind and a column the page does not know yet still render.
  it("shows state cards in their three groups", async () => {
    const unit = (name: string, value: number, o = {}) => ({
      name,
      project: null,
      value,
      count: 4,
      of: null,
      typical: null,
      since: "2026-10-02T09:00:00Z",
      lower_bound: false,
      week: null,
      names: ["kasse-prod"],
      held: false,
      reason: null,
      ci_seconds: null,
      query: null,
      measure: null,
      ...o,
    });
    const card = (
      key: string,
      unitKind: string,
      worst: ReturnType<typeof unit>[],
      rest: ReturnType<typeof unit>[],
      threshold: { key: string; value: number; unit: string },
      facts = "deploys",
    ) => ({
      key,
      unit: unitKind,
      count: worst.length + rest.length,
      judged: 9,
      unjudged: 1,
      idle: 2,
      worst,
      more: rest.length,
      units: [...worst, ...rest],
      since: "2026-10-02T09:00:00Z",
      since_lower_bound: true,
      threshold: { ...threshold, min_key: null, min: null },
      // The period's end, Monday 00:00 in Zurich: "as of" the Sunday.
      at: "2026-11-08T23:00:00Z",
      was: null,
      facts,
    });
    const fact = (id: string, o = {}) => ({
      id,
      label: "state_units",
      unit: "count",
      value: 1,
      prev: null,
      verdict: "none",
      magnitude: null,
      ...o,
    });
    const state = (
      id: string,
      tone: string,
      st: unknown,
      kind = "state",
      more: unknown[] = [],
    ) =>
      ({
        id,
        kind,
        tone,
        source: "argocd",
        repo: null,
        run_kind: null,
        metric: null,
        weight: 9,
        seen: 0,
        facts: [fact(`${id}.count`), ...more],
        waiting: null,
        drivers: [],
        state: st,
      }) as unknown as Signal;
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const base = analysisStatus();
        const signals = [
          state(
            "state:no_release",
            "attention",
            card(
              "no_release",
              "project",
              [
                unit("kasse", 41, { query: "kasse-prod" }),
                unit("lager", 31, { names: ["lager-prod"] }),
                unit("portal", 29, { count: 1, names: ["portal-prod"] }),
              ],
              [unit("tools", 29.5, { names: ["tools-prod"] })],
              { key: "release_breach_days", value: 28, unit: "days" },
            ),
            "state",
            // The team's release rate got notably worse: the backend folds
            // it onto the card that speaks for it.
            [
              fact("change:deploy_frequency.value", {
                label: "deploy_per_week",
                unit: "per_week",
                value: 0.5,
                prev: 2,
                verdict: "worse",
                magnitude: "notable",
              }),
            ],
          ),
          state(
            "state:bot_loop",
            "hint",
            card(
              "bot_loop",
              "branch",
              [
                unit("kasse/kasse-api:renovate/lib-2.x", 23, {
                  ci_seconds: 7200,
                  names: [],
                  query: "kasse-api/renovate%2Flib-2.x",
                }),
              ],
              [],
              { key: "bot_loop_failures", value: 10, unit: "count" },
              "runs",
            ),
          ),
          state(
            "state:untraced",
            "limit",
            card(
              "untraced",
              "repo",
              [
                unit("kasse/kasse-web", 2, {
                  of: 9,
                  reason: "in_silence",
                  names: [],
                  query: "kasse/kasse-web",
                }),
              ],
              [],
              { key: "", value: 1, unit: "count" },
              "prs",
            ),
          ),
          state("future:thing", "hint", null, "future_kind"),
        ];
        return {
          ...base,
          assessment: { ...base.assessment, signals },
          layout: {
            focus: [],
            cards: [
              {
                signal: "state:no_release",
                column: "bad",
                lever: "plan_release",
              },
              {
                signal: "state:bot_loop",
                column: "hint",
                lever: "stop_bot_loop",
              },
              {
                signal: "state:untraced",
                column: "limit",
                lever: "check_source",
              },
              { signal: "future:thing", column: "future_column", lever: "" },
            ],
            also: [],
            tally: { good: 0, done: 0, bad: 1, hint: 2, limit: 1, talk: 0 },
          },
        } as unknown as AnalysisStatus;
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    expect(p.getByRole("heading", { name: "Hakt" })).toBeInTheDocument();
    expect(p.getByRole("heading", { name: "Hinweise" })).toBeInTheDocument();
    expect(
      p.getByRole("heading", { name: "Nicht beurteilbar" }),
    ).toBeInTheDocument();
    // Hints and what cannot be judged say they are no finding.
    expect(
      p.getByRole("heading", { name: "Hinweise" }).nextSibling?.textContent,
    ).toBe("kein Befund, nur zur Kenntnis");
    const red = within(
      p.getByText("Lange kein Release nach PROD").closest("article")!,
    );
    expect(red.getByText("4 von 9 Projekten")).toBeInTheDocument();
    // Each unit: its name, then its number in the state's words.
    expect(
      red.getByText(
        "letztes Release vor 41 Tagen (kasse-prod), seither 4 PRs gemerged",
      ),
    ).toBeInTheDocument();
    expect(
      red.getByText(
        "letztes Release vor 29 Tagen (portal-prod), seither 1 PR gemerged",
      ),
    ).toBeInTheDocument();
    // The rest behind "+1 weiteres", the backend's list as sent.
    expect(red.queryByText("tools")).toBeNull();
    fireEvent.click(red.getByRole("button", { name: "+1 weiteres" }));
    expect(red.getByText("tools")).toBeInTheDocument();
    expect(
      red.getByText(/^letztes Release vor 29\.5 Tagen/),
    ).toBeInTheDocument();
    fireEvent.click(red.getByRole("button", { name: "weniger zeigen" }));
    expect(red.queryByText("tools")).toBeNull();
    // The team's worsening the state speaks for, as a line of its own.
    expect(
      red.getByText(
        "Releases pro Woche: 0.5, deutlich schlechter als in der Vorperiode",
      ),
    ).toBeInTheDocument();
    expect(red.getByText("vorher 2.0")).toBeInTheDocument();
    // Since when, as of when, the agreement with the team's own number.
    expect(
      red.getByText(
        /^seit mindestens dem 02\.10\.2026 · Stand 08\.11\.2026 · 1 mit zu wenig Daten$/,
      ),
    ).toBeInTheDocument();
    expect(
      red.getByText(
        "Abmachung: mindestens ein Release nach PROD pro Sprint. Gezählt ab mehr als 28 Tagen ohne Release.",
      ),
    ).toBeInTheDocument();
    // A bot's branch, worded as CI time; its unit word is the bot's.
    const hint = within(
      p.getByText("Bot-Branch scheitert immer wieder").closest("article")!,
    );
    expect(hint.getByText("1 von 9 Bot-Branches")).toBeInTheDocument();
    expect(
      hint.getByText(/^23 fehlgeschlagene Builds in Folge, 2.0 \S+ CI-Zeit$/),
    ).toBeInTheDocument();
    expect(
      hint.getByText("Gezählt ab 10 fehlgeschlagenen Builds in Folge."),
    ).toBeInTheDocument();
    const limit = within(
      p
        .getByText("Nicht messbar, ob gemergte PRs auf PROD sind")
        .closest("article")!,
    );
    expect(
      limit.getByText(
        "2 von 9 gemergten PRs nicht zuordenbar: eine Lücke in den Daten",
      ),
    ).toBeInTheDocument();
    const tally = within(p.getByRole("list", { name: "Bilanz" }));
    expect(tally.getByText("Hinweise")).toBeInTheDocument();
    expect(tally.getByText("nicht beurteilbar")).toBeInTheDocument();
    // A unit's name opens its rows: the backend's search, the card's table.
    expect(red.queryByRole("button", { name: "lager" })).toBeNull();
    fireEvent.click(hint.getByRole("button", { name: /renovate/ }));
    expect(window.location.search).toBe(
      "?q=kasse-api%2Frenovate%252Flib-2.x&tab=runs",
    );
    fireEvent.click(red.getByRole("button", { name: "kasse" }));
    expect(window.location.search).toBe("?q=kasse-prod&tab=deploys");
    // A state whose rows carry a mark links to all of them by it.
    fireEvent.click(
      limit.getByRole("button", {
        name: "«nicht zuordenbar» in den Daten ansehen →",
      }),
    );
    // (The PR table is the default: no tab in the URL.)
    expect(window.location.search).toBe("?q=nicht+zuordenbar");
  });

  // A gain the cards left out still counts: the tally and the foot follow
  // the backend's tally, never the cards shown. Once a gain stood as a
  // green chip under "also spotted" while the page said nothing was better.
  it("never calls a period unremarkable with a gain spotted", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const base = analysisStatus();
        return analysisStatus({
          layout: {
            ...base.layout,
            cards: base.layout.cards.filter((c) => c.column === "bad"),
            also: base.layout.cards.filter((c) => c.column !== "bad"),
          },
        });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    expect(
      within(p.getByRole("list", { name: "Bilanz" })).getByText("läuft"),
    ).toBeInTheDocument();
    expect(p.queryByText(/Keine Verbesserung|Noch kein Vergleich/)).toBeNull();
  });

  // Stuck, but every finding sits with a focus repo: the heading stays, so
  // the sentence has its subject.
  it("says under its heading where stuck findings without a card are", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const base = analysisStatus();
        return analysisStatus({
          layout: {
            ...base.layout,
            cards: base.layout.cards.filter((c) => c.column !== "bad"),
          },
        });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    const stuck = p.getByRole("heading", { name: "Hakt" });
    expect(stuck.parentElement?.nextElementSibling?.textContent).toBe(
      "Ohne eigene Karte: Die Befunde stehen weiter unten.",
    );
    expect(p.queryByText("Nichts hakt in diesen Wochen.")).toBeNull();
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

  it("colours a jam's waits as the open PRs chart does", async () => {
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
                      draft: 1,
                      first_review: 4,
                      review: 2,
                      changes: 1,
                      merge: 3,
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
    // Legend and bar in the wait chain's order, one colour per wait.
    const want: [string, string][] = [
      ["wartet auf erstes Review: 4", "bg-s1"],
      ["wartet auf Review: 2", "bg-s3"],
      ["wartet auf Änderungen: 1", "bg-s4"],
      ["wartet auf Merge: 3", "bg-s2"],
      ["wartet auf Entwurf: 1", "bg-other"],
    ];
    const keys = want.map(([text]) => p.getByText(text));
    keys.forEach((k, i) =>
      expect(k.querySelector("i")).toHaveClass(want[i][1]),
    );
    const bar = keys[0].parentElement!.previousElementSibling!;
    expect([...bar.children].map((s) => s.className)).toEqual(
      want.map(([, c]) => `rounded-sm ${c}`),
    );
  });

  // Only where the backend says the period is quiet: measured, compared,
  // and nothing raised.
  it("says so when nothing stands out", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const a = assessment();
        return analysisStatus({
          enabled: true,
          assessment: {
            ...a,
            signals: [],
            focus: [],
            question: null,
            quiet: true,
            quiet_reasons: [],
            limited: false,
            limits: [],
          },
          layout: {
            focus: [],
            cards: [],
            also: [],
            tally: { good: 0, done: 0, bad: 0, hint: 0, limit: 0, talk: 0 },
          },
        });
      }
      return metrics(4);
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    // One sentence each, no empty group.
    expect(p.queryAllByRole("heading", { level: 4 })).toEqual([]);
    expect(
      p.getByText("Keine Verbesserung, die als Beleg zählt."),
    ).toBeInTheDocument();
    expect(
      p.getByText("In diesen Wochen fällt nichts auf."),
    ).toBeInTheDocument();
    expect(p.queryByText("Nichts hakt in diesen Wochen.")).toBeNull();
    expect(p.queryByRole("note")).toBeNull();
    expect(p.queryByText("Im Fokus")).toBeNull();
    expect(p.queryByText("Für die nächste Retro")).toBeNull();
  });

  // The two shapes the backend sends: releases per week, whole counts
  // (null before Argo CD reported), and a kind's success rate. No line
  // without a week of this period.
  it("draws a card's weeks", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const a = assessment();
        const base = analysisStatus();
        const gain = (
          metric: string,
          label: string,
          unit: "per_week" | "share",
          value: number,
          prev: number,
          series: { prev: (number | null)[]; cur: (number | null)[] },
        ) => ({
          ...a.signals.find((s) => s.id === "change:merged_per_week")!,
          id: `change:${metric}`,
          metric,
          facts: [
            {
              id: `change:${metric}.value`,
              label,
              unit,
              value,
              prev,
              verdict: "better" as const,
              magnitude: "minor" as const,
            },
          ],
          series,
        });
        const deploys = gain(
          "deploy_frequency",
          "deploy_per_week",
          "per_week",
          4.5,
          3.9,
          { prev: [null, 3], cur: [4, 5] },
        );
        const builds = gain(
          "build_success:feature",
          "build_success",
          "share",
          0.95,
          0.9,
          { prev: [0.8, 0.9], cur: [0.94, 0.96] },
        );
        const stale = gain(
          "build_success:main",
          "build_success",
          "share",
          0.95,
          0.9,
          { prev: [0.8, 0.9], cur: [null, null] },
        );
        const card = (signal: string) => ({
          signal,
          column: "good" as const,
          lever: "name_what_helped",
        });
        return analysisStatus({
          assessment: { ...a, signals: [...a.signals, deploys, builds, stale] },
          layout: {
            ...base.layout,
            cards: [
              card(deploys.id),
              card(builds.id),
              card(stale.id),
              ...base.layout.cards,
            ],
          },
        });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    expect(
      p.getByRole("img", { name: "Pro Woche, Vorperiode: –, 3; jetzt: 4, 5" }),
    ).toBeInTheDocument();
    expect(
      p.getByRole("img", {
        name: "Pro Woche, Vorperiode: 80 %, 90 %; jetzt: 94 %, 96 %",
      }),
    ).toBeInTheDocument();
    expect(p.getAllByRole("img", { name: /^Pro Woche/ })).toHaveLength(2);
  });

  // Findings, nothing stuck: the cards say it, no calm word (only quiet
  // has one). The tally's 0 stands where nothing limits the counts.
  it("shows what runs without a calm word beside it", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const a = assessment();
        const base = analysedStatus();
        return analysedStatus({
          assessment: {
            ...a,
            signals: a.signals.filter((s) => s.tone === "good"),
            focus: [],
            question: null,
            quiet_reasons: ["findings"],
            limited: false,
            limits: [],
          },
          layout: {
            focus: [],
            cards: base.layout.cards.filter((c) => c.column !== "bad"),
            also: [],
            tally: { good: 1, done: 1, bad: 0, hint: 0, limit: 0, talk: 0 },
          },
        });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    expect(p.getByRole("heading", { name: "Läuft" })).toBeInTheDocument();
    expect(p.getByText("Mehr PRs kommen durch.")).toBeInTheDocument();
    expect(p.queryByText(/Nichts hakt/)).toBeNull();
    expect(p.queryByText("In diesen Wochen fällt nichts auf.")).toBeNull();
    expect(
      within(p.getByRole("list", { name: "Bilanz" })).getByText("0"),
    ).toBeInTheDocument();
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
      screen.getByText("Keine abgeschlossenen Builds im Zeitraum."),
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
    // A stage promotion is listed faded, the people's PRs are not.
    expect(screen.getByText("Promotion").closest("tr")).toHaveAttribute(
      "data-dim",
    );
    expect(key.closest("tr")).not.toHaveAttribute("data-dim");
    // The PR's number beside its title links to it in Bitbucket; without a
    // URL it is plain text.
    const pr = screen.getByRole("link", { name: "#1" });
    expect(pr).toHaveAttribute(
      "href",
      "https://bitbucket.example.com/projects/KASSE/repos/kasse-api/pull-requests/1",
    );
    expect(pr).toHaveAttribute("target", "_blank");
    expect(pr.closest("td")).toBe(key.closest("td"));
    expect(screen.getByText("#4")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "#4" })).toBeNull();
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
    fireEvent.click(within(tabs).getByRole("tab", { name: /Builds/ }));
    expect(window.location.search).toBe("?q=kasse-api%3A1.1&tab=runs");
    // The UI's word where the status plainly says it, else the CI's.
    const runs = screen.getByText("Unstable").closest("table")!;
    expect(within(runs).getAllByText("Erfolgreich")).toHaveLength(2);
    expect(within(runs).getByText("Fehlgeschlagen")).toBeInTheDocument();
    // A GitOps repo's run, a stage promotion's check, is listed faded.
    expect(within(runs).getByText("#13").closest("tr")).toHaveAttribute(
      "data-dim",
    );
    expect(within(runs).getByText("#12").closest("tr")).not.toHaveAttribute(
      "data-dim",
    );
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
    const kinds = screen.getByRole("group", { name: "Build-Art" });
    // feature builds first: kasse-api broke one run, too few to compare
    expect(
      within(kinds).getByRole("button", { name: "Feature- und Bugfix-Builds" }),
    ).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("Kein Vergleich")).toBeInTheDocument();
    expect(
      screen.getByText(/unter 10 Builds in einer der beiden Perioden;/),
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
            failing_more: true,
            flaky_runs: 3,
            flaky_seconds: 2700,
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
            failing_more: false,
            flaky_runs: 0,
            flaky_seconds: 0,
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
            failing_more: false,
            flaky_runs: 0,
            flaky_seconds: 0,
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
        failing_more: 1,
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
      screen.getByText(/Vorperiode 10 % → jetzt 40 % fehlgeschlagen/),
    ).toBeInTheDocument();
    expect(screen.getByText("Langsamer geworden")).toBeInTheDocument();
    expect(
      screen.getByText(/Vorperiode 10 min → jetzt 30 min \(3\.0×\)/),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Team 10/)).toBeNull();
    expect(
      screen.getByText(
        /keine vergleichbaren Builds dieser Art in der Vorperiode;/,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/fehleranfälliger geworden als in der Vorperiode/),
    ).toHaveTextContent(
      "Von 2 Repos sind 1 langsamer, 1 fehleranfälliger geworden als in der Vorperiode",
    );
    expect(screen.getByText("2.0 h")).toBeInTheDocument();
    // The red time's flaky part, counted by the backend.
    expect(screen.getByText("3 flaky")).toHaveAttribute(
      "title",
      "0.8 h davon in Builds, die ein späterer Build auf demselben Commit erfolgreich machte",
    );
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
      /kasse\/slow-apiTypisch10 min → 30 minFehlerquote0 % → 0 %Builds12CI-Zeit6.0 h$/,
    );
    fireEvent.mouseLeave(hit("kasse/slow-api"));
    // By keyboard too, the tooltip's content as the target's name.
    fireEvent.focus(hit("kasse/new-api"));
    expect(screen.getByRole("tooltip")).toHaveTextContent(
      "Kein Vergleichkeine vergleichbaren Builds dieser Art in der Vorperiode",
    );
    fireEvent.blur(hit("kasse/new-api"));
    expect(screen.queryByRole("tooltip")).toBeNull();
    expect(hit("kasse/new-api")).toHaveAttribute("tabindex", "0");
    expect(
      within(quiver).getByRole("img", { name: /^kasse\/slow-api; Typisch: / }),
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
      screen.getAllByRole("button", { name: "Builds ansehen →" })[1],
    );
    expect(window.location.search).toBe("?q=kasse%2Fslow-api&tab=runs");
    // Stuck together across repos: the agents, said once.
    expect(
      screen.getByText(/2 Builds aus 2 Repos starteten am/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "3 Builds aus 2 Repos starteten zwischen 05.11. 23:50 und 06.11. 00:15",
      ),
    ).toBeInTheDocument();
    expect(screen.getAllByText(/Repos starteten/)).toHaveLength(2);
    expect(
      screen.getByText(/^25 Builds hingen im Zeitraum/),
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
    expect(screen.getByTitle(/^Typischer Wert über 4 der /)).toHaveTextContent(
      "davon typischerweise 7 min Warten auf einen Agent, 3 min Bauen",
    );
    // In the build section; the assessment's agent-wait card is its own.
    const build = document.getElementById("build")!;
    expect(within(build).getAllByText(/Warten auf einen Agent/)).toHaveLength(
      1,
    );
    expect(
      screen.getByText("#7").parentElement?.parentElement,
    ).toHaveTextContent("20.0 h");
    expect(
      screen.getByTitle("Der Build meldet nicht, wann er einen Agent bekam"),
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
      screen.getByText(
        /typischer Dauer der erfolgreichen Builds, ohne Ausreisser, ab 3 davon/,
      ),
    ).toBeInTheDocument();
    // The wait beside the build, in the hotspots row too.
    expect(screen.getByText(/· 1 min Warten, 9 min Bauen/)).toBeInTheDocument();
    const steady = screen.getByText("kasse/steady-web").closest(".contents");
    expect(steady).toHaveTextContent(/25 min30 min2 min \/ 23 min12/);
    expect(
      screen.getByTitle(
        "Typischer Wert über 6 der 12 Builds, die melden, wann sie einen Agent bekamen",
      ),
    ).toBeInTheDocument();
    const mono = screen.getByText("kasse/mono").closest(".contents");
    expect(
      within(mono as HTMLElement).getByTitle(
        "Keiner der Builds meldet, wann er einen Agent bekam",
      ),
    ).toHaveTextContent("–");
    // In the backend's order, its link last.
    const links = screen.getAllByRole("button", { name: "Builds ansehen →" });
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
    ).toHaveTextContent("4 von 4 Builds fehlgeschlagen (100 %)");
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
    expect(tip).toHaveTextContent("2 von 5 Builds fehlgeschlagen (40 %)");
    fireEvent.mouseLeave(cells[4]);
    fireEvent.mouseEnter(cells[2]);
    expect(tipNow()).toHaveTextContent(
      "1 von 2 Builds fehlgeschlagenunter 3 Builds: kein Urteil",
    );
    fireEvent.mouseLeave(cells[2]);
    fireEvent.mouseEnter(cells[1]);
    expect(tipNow()).toHaveTextContent("keine Builds");
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
    const kinds = screen.getByRole("group", { name: "Build-Art" });
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

  it("reports a 2xx that is not JSON, a sign-in page say", async () => {
    mockFetch(
      () =>
        new Response("<html>sign in</html>", {
          status: 200,
          headers: { "Content-Type": "text/html" },
        }),
    );
    render(<App />);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "200: answer is not JSON (a sign-in page?)",
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
      await screen.findByText("Feature- und Bugfix-Builds: kein Build erkannt"),
    ).toBeInTheDocument();
    // the backend's count, not the other card's 70 runs
    expect(
      screen.getByText(
        /44 Builds auf Branches ohne bekanntes Präfix stehen unter «Übrige Branches»/,
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
    const kinds = screen.getByRole("group", { name: "Build-Art" });
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
    expect(screen.queryByText(/kein Build erkannt/)).toBeNull();
    // the prefixes on the other card instead
    expect(screen.getByText(/^Häufigste Präfixe:/)).toBeInTheDocument();
    // no feature builds, none hiding: the first kind
    const kinds = screen.getByRole("group", { name: "Build-Art" });
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
    expect(screen.queryByText(/kein Build erkannt/)).toBeNull();
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
    // On the overview's tile and under the deploy section's head, where its
    // change stands too.
    expect(
      await screen.findAllByText(/Argo CD meldet erst seit 07\.11\.2026/),
    ).toHaveLength(2);
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
      await screen.findAllByText(
        /Weniger als eine Woche Daten: wenig belastbar/,
      ),
    ).toHaveLength(2);
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

  it("reads a kind's hours under 3 min as < 0.1 h, never 0.0 h", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      const m = metrics();
      m.build.kinds[0] = {
        ...m.build.kinds[0],
        failed: 1,
        ci_seconds: 54000,
        wasted_seconds: 120,
        flaky_runs: 1,
        flaky_of_failed: 1,
        flaky_seconds: 60,
      };
      return m;
    });
    render(<App />);
    expect(
      await screen.findByText(
        "1 fehlgeschlagener Build flaky: danach erfolgreich auf demselben Commit (100 % der fehlgeschlagenen, < 0.1 h)",
      ),
    ).toBeInTheDocument();
    const red = screen.getByText(
      "in fehlgeschlagenen Builds, von 15.0 h CI-Zeit",
    );
    // The big number above it: the red runs' time, under 3 min.
    expect(red).toHaveTextContent(/^< 0\.1 hin fehlgeschlagenen Builds/);
  });

  it("shows a kind's flaky runs as the backend counted them, and the runs", async () => {
    const calls = mockFetch((url) => {
      if (url.pathname === "/api/facts") {
        const f = facts();
        // The commit the backend picked to show, not commit_sha.
        f.runs.rows[1] = {
          ...f.runs.rows[1],
          flaky: true,
          shown_sha: "e".repeat(40),
        };
        return f;
      }
      const m = metrics();
      m.build.kinds[0] = {
        ...m.build.kinds[0],
        runs: 20,
        failed: 4,
        flaky_runs: 2,
        flaky_rate: 0.1,
        flaky_of_failed: 0.5,
        flaky_seconds: 1800,
        unjudged: 1,
      };
      m.comparison.build[2] = {
        ...m.comparison.build[2],
        flaky: change("worse", 0.02),
      };
      // Releases: nothing red now, flaky before; the improvement shows.
      m.comparison.build[0] = {
        ...m.comparison.build[0],
        flaky: change("better", 0.1),
      };
      return m;
    });
    render(<App />);
    expect(
      await screen.findByText(
        "2 fehlgeschlagene Builds flaky: danach erfolgreich auf demselben Commit (50 % der fehlgeschlagenen, 0.5 h) · 1 Build ohne Commit nicht beurteilt",
      ),
    ).toBeInTheDocument();
    // The rate, the previous one under it as everywhere, the verdict below.
    const worse = screen.getByText(
      "Flaky-Quote 10 % der Builds mit Commit",
    ).parentElement!;
    expect(within(worse).getByText("vorher 2 %")).toHaveClass(
      "text-danger-ink",
    );
    expect(
      within(worse.parentElement!).getByText("Schlechter"),
    ).toBeInTheDocument();
    const better = screen.getByText(
      "Flaky-Quote 0 % der Builds mit Commit",
    ).parentElement!;
    expect(within(better).getByText("vorher 10 %")).toHaveClass("text-online");
    expect(
      within(better.parentElement!).getByText("Besser"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Kein fehlgeschlagener Build, also auch kein flaky"),
    ).toBeInTheDocument();
    // A kind with failed builds but none flaky says so.
    expect(
      screen.getAllByText(
        "Kein fehlgeschlagener Build war danach auf demselben Commit erfolgreich",
      ).length,
    ).toBeGreaterThan(0);
    fireEvent.click(
      screen.getByRole("button", { name: /Flaky-Builds ansehen/ }),
    );
    await waitFor(() => expect(factsCalls(calls).at(-1)).toMatch(/&q=flaky$/));
    const runs = (await screen.findByText("#11")).closest("tr")!;
    expect(within(runs).getByText("Flaky")).toBeInTheDocument();
    expect(within(runs).getByText("eeeeeeee")).toBeInTheDocument();
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
    // (The flow's per-repo list of PRs to production, folded, apart.)
    const rows = screen
      .getAllByText(/^(kasse\/kasse-api|portal\/portal-web)$/)
      .filter((r) => !r.closest("details"));
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
      within(cols[0]).getByText("3", { selector: ".text-danger-ink" }),
    ).toBeInTheDocument();
    expect(
      within(cols[1]).queryByText("1", { selector: ".text-danger-ink" }),
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
    expect(screen.getByText("Gerade ist kein PR offen.")).toBeInTheDocument();
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

  it("shows a draft without an age", async () => {
    const rows = facts().prs.rows;
    mockFetch((url) =>
      url.pathname === "/api/facts"
        ? facts({
            prs: {
              count: 1,
              rows: [{ ...rows[2], waiting: "draft", age_s: null }],
            },
          })
        : metrics(),
    );
    render(<App />);
    expect(await screen.findByText("Entwurf")).toBeInTheDocument();
    expect(screen.queryByText(/Entwurf ·/)).not.toBeInTheDocument();
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
            min_prs: 10,
            before_prod: 0,
            reaches_prod: true,
            bucket_bounds_s: STAGE_BOUNDS,
          },
        }}
      />,
    );
    expect(document.body.textContent).not.toMatch(/NaN/);
    expect(screen.getAllByText(/25 % der Zeit/)).toHaveLength(4);
    expect(screen.queryByText(/Bis null/)).toBeNull();
    expect(screen.getByText("gemerged → auf PROD")).toBeInTheDocument();
    // Without a stage before PROD the stretch is named from the merge.
    expect(screen.getAllByText("Ab Merge bis PROD").length).toBeGreaterThan(0);
    // No stage is hot: no lever named.
    expect(screen.queryByText(/Der Hebel liegt/)).toBeNull();
  });

  it("names no share of the time to production when no change reached it", () => {
    const m = metrics();
    const stages = m.lead_chain.stages
      .map((s) =>
        s.key === "to_prod"
          ? {
              ...s,
              n: 0,
              p50: null,
              p90: null,
              low_sample: true,
              histogram: null,
              durations_s: [],
            }
          : s,
      )
      .map((s) => ({ ...s, share: null, hot: false }));
    render(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={{
          ...m,
          lead_chain: { ...m.lead_chain, stages, reaches_prod: false },
        }}
      />,
    );
    expect(
      screen.getByText(/keine Änderung bis PROD verfolgt/),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("img", { name: /Anteil an der Zeit/ }),
    ).toBeNull();
    expect(screen.queryByText(/der Zeit$/)).toBeNull();
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
    expect(tip).toHaveTextContent("Von INTG bis PROD");
    expect(tip).toHaveTextContent("PRs3");
    expect(tip).toHaveTextContent("typischer Wert16.8 h");
    expect(tip).toHaveTextContent("die langsamsten 10 %: über16.8 h");
    expect(document.body.textContent).not.toMatch(/NaN/);
    // Assistive tech gets the numbers once, as a table; a stage without
    // PRs reads as such, not as dashes.
    const table = screen.getByRole("table", { name: "Wartezeit je Abschnitt" });
    const merge = within(table).getByRole("row", { name: /^Merge/ });
    expect(merge).toHaveTextContent("Merge0keine PRs");
    expect(
      within(table).getByRole("row", { name: /^Von INTG bis PROD/ }),
    ).toHaveTextContent("Von INTG bis PROD316.8 h16.8 h");
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

describe("throughput", () => {
  it("shows PR throughput and batch size as the backend sends them", () => {
    const base = metrics();
    const m = {
      ...base,
      throughput: {
        merged: 47,
        merged_per_week: 11.75,
        since: "2026-10-21T08:00:00Z",
        umbrella: 1,
        housekeeping: 1234,
        twice: 2,
        housekeeping_repos: 4,
        housekeeping_window_s: 1800,
        library: 0,
        coverage: {
          to_prod: { count: 30, n: 47, rate: 30 / 47 },
          sized: { count: 40, n: 47, rate: 40 / 47 },
          opened: { count: 45, n: 47, rate: 45 / 47 },
        },
        batch_size: { n: 40, p50: 86.5, p90: 412 },
        batch_files: { n: 40, p50: 5, p90: 14 },
        weeks: base.throughput.weeks,
      },
    };
    render(
      <>
        <MergedTile m={m} />
        <BatchTile m={m} />
        <ThroughputCard m={m} />
      </>,
    );
    const merged = screen
      .getByText("PR Throughput")
      .closest("div")!.parentElement!;
    expect(within(merged).getByText("11.8")).toBeInTheDocument();
    expect(within(merged).getByText("PRs pro Woche")).toBeInTheDocument();
    // What the count leaves out, each reason only when it left one out,
    // its meaning on hover, counts in Swiss format. PRs into a feature
    // branch count: no word of them.
    expect(
      within(merged).getByText(/^47 PRs im Zeitraum gemerged/).textContent,
    ).toBe(
      `47 PRs im Zeitraum gemerged · ${num(1234)} Aufräum-PRs nicht gezählt · 1 Merge eines Sammel-Branches nicht gezählt · 2 PRs doppelt, nicht gezählt`,
    );
    // The rule's numbers are the backend's, never the page's own.
    expect(
      within(merged).getByTitle(
        /^Derselbe Titel in 4 oder mehr Repos innert 30 Minuten eröffnet/,
      ),
    ).toBeInTheDocument();
    expect(
      within(merged).getByTitle(/^Derselbe Branch kam schon/),
    ).toBeInTheDocument();
    expect(
      within(merged).getByTitle(/^Der PR brachte nur die Arbeit anderer PRs/),
    ).toBeInTheDocument();
    expect(merged.textContent).not.toMatch(/Feature-Branch/);
    // Bitbucket started inside the period: the rate is since then.
    expect(
      within(merged).getByText(
        "Bitbucket liefert erst seit dem 21.10.2026 Daten: Die Wochen davor sind leer, die Rate gilt ab dann.",
      ),
    ).toBeInTheDocument();
    expect(within(merged).getByText("vorher 0.5")).toBeInTheDocument();
    expect(within(merged).getByText("Besser")).toBeInTheDocument();

    const batch = screen.getByText("Batch Size").closest("div")!.parentElement!;
    expect(within(batch).getByText("87")).toBeInTheDocument();
    expect(
      within(batch).getByText(
        "grösste 10 % ab 412 Zeilen · typisch 5 Dateien · Grösse bekannt bei 40 von 47",
      ),
    ).toBeInTheDocument();
    expect(within(batch).getByText("vorher 80")).toBeInTheDocument();
    expect(within(batch).getByText("Schlechter")).toBeInTheDocument();
    // Two charts, a column per week each.
    expect(screen.getAllByTestId("week-col")).toHaveLength(
      2 * m.period.weeks.length,
    );
  });

  it("says so when no PR has a size", () => {
    const m = metrics();
    render(
      <BatchTile
        m={{
          ...m,
          throughput: {
            ...m.throughput,
            batch_size: { n: 0, p50: null, p90: null },
            batch_files: { n: 0, p50: null, p90: null },
          },
        }}
      />,
    );
    expect(screen.getByText("Keine PR-Grössen im Zeitraum.")).toBeVisible();
  });
});

describe("colour", () => {
  // One meaning per colour: the change under a number in its verdict's
  // colour, the number itself ink; the deploy section says it too.
  it("colours the change, never the number", async () => {
    mockFetch();
    render(<App />);
    await screen.findByRole("heading", { name: "Einschätzung" });
    const better = await screen.findAllByText("vorher 0.1");
    // The overview's tile and the deploy section's head.
    expect(better).toHaveLength(2);
    better.forEach((el) => expect(el).toHaveClass("text-online"));
    expect(
      within(document.getElementById("deploys")!).getByText("vorher 0.1"),
    ).toBeInTheDocument();
    // The lead time's tile and the flow chain's PROD box, alike.
    const worse = screen.getAllByText("vorher 1.0 Tage");
    expect(worse).toHaveLength(2);
    worse.forEach((el) => expect(el).toHaveClass("text-danger-ink"));
    // The verdict's pill: worse is red, no longer ochre.
    expect(screen.getAllByText("Schlechter")[0]).toHaveClass("text-danger-ink");
  });

  // A repo the backend judged more reliable says so in green, its numbers
  // kept, instead of "unremarkable".
  it("marks a repo that got better", async () => {
    const m = metrics();
    m.build.hotspots.kinds.forEach((k) => {
      k.repos[0].failure_change = change("better", 0.3);
    });
    mockFetch((url) => (url.pathname === "/api/facts" ? facts() : m));
    render(<App />);
    const pill = await screen.findByText("Verlässlicher geworden");
    expect(pill).toHaveClass("bg-online/15");
    const row = pill.parentElement!.parentElement!;
    expect(within(row).queryByText("Unauffällig")).toBeNull();
    expect(row.textContent).toMatch(/Vorperiode 30 % → jetzt/);
  });
});

describe("strip", () => {
  it("draws the backend's scores, worse left and red, better green, no undirected row", () => {
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
    // freq, lead, lead to intg, deploy success, review, within a day, PR
    // throughput, batch size, jammed, three kinds; first touch and open PRs
    // have no direction.
    expect(rows).toHaveLength(12);
    expect(within(rows[6]).getByText("PR Throughput")).toBeInTheDocument();
    expect(within(rows[6]).getAllByText("0.8")[0]).toBeInTheDocument();
    expect(within(rows[7]).getByText("Batch Size")).toBeInTheDocument();
    expect(within(rows[7]).getByText("140")).toBeInTheDocument();
    // A long label wraps, never cut off with an ellipsis.
    for (const row of rows)
      expect(row.firstElementChild!.className).not.toMatch(/truncate/);
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
    expect(better).toHaveClass("text-online");
    expect(within(review).getByTestId("strip-bar").className).toContain(
      "from-online",
    );
    expect(better.style.left).toContain("50%");

    expect(within(rows[2]).getByText("Vorperiode unvollständig")).toBeVisible();
    expect(within(rows[5]).getByText("zu wenig Daten")).toBeInTheDocument();
    expect(within(rows[9]).getByText("Erfolgsquote, Releases")).toBeVisible();

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
    expect(rows).toHaveLength(10);
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
    // Red CI time written red; the hatch in the bar stays dark, so the
    // repo's hue still tells it.
    const red = screen.getByText("· 100.0 h fehlgeschlagen");
    expect(red).toHaveClass("text-danger-ink");
    expect(red.parentElement?.textContent).toBe(
      "kasse/kasse-web 200.0 h · 100.0 h fehlgeschlagen",
    );
    expect(
      screen.getByText("Nach Repo, über alle Build-Arten"),
    ).toBeInTheDocument();
    const chart = screen.getByRole("img", {
      name: /^CI-Zeit von der Build-Art zum Ausgang/,
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
      screen.getByText(
        /Ausreisser: Builds über 6× der typischen Dauer ihrer Art/,
      ),
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

// What the data cannot tell, said as such: never a calm word where nothing
// could be measured or compared, never a count that hides what it leaves out.
describe("limits", () => {
  const quietless = (o: Partial<AnalysisStatus["assessment"]>) => {
    const a = assessment();
    return analysisStatus({
      assessment: {
        ...a,
        signals: [],
        focus: [],
        question: null,
        quiet: false,
        ...o,
      },
      layout: {
        focus: [],
        cards: [],
        also: [],
        tally: { good: 0, done: 0, bad: 0, hint: 0, limit: 0, talk: 0 },
      },
    });
  };

  it("names every limit in the backend's order, and nothing calm", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return quietless({
          quiet_reasons: ["no_baseline", "source_gap", "not_measurable"],
          not_measurable: "to_prod",
          limits: [
            {
              source: "pipeline",
              kind: "starts",
              from: "2026-10-21T09:00:00Z",
              to: null,
            },
            {
              source: "bitbucket",
              kind: "silence",
              from: "2026-11-05T10:00:00Z",
              to: "2026-11-09T18:00:00Z",
            },
          ],
          coverage: {
            to_prod: { count: 0, n: 7, rate: 0 },
            sized: { count: 7, n: 7, rate: 1 },
            opened: { count: 7, n: 7, rate: 1 },
          },
        });
      return metrics();
    });
    render(<App />);
    const panel = await screen.findByTestId("assessment");
    const note = within(panel).getByRole("note", {
      name: "Was diese Einschätzung einschränkt",
    });
    expect([...note.children].map((c) => c.textContent)).toEqual([
      "Noch kein voller Vergleich mit der Vorperiode: Nicht jede Zahl hat dort einen vergleichbaren Wert.",
      "Die CI liefert erst seit dem 21.10.2026 Daten.",
      "Bitbucket lieferte vom 05.11.2026 bis 09.11.2026 keine Daten, obwohl Builds liefen: Ereignisse gingen verloren. Die PR-Zahlen bleiben ohne Urteil.",
      "Nicht messbar: 7 PRs wurden in diesen Wochen gemerged, keiner liess sich bis PROD verfolgen.",
    ]);
    expect(panel.textContent).not.toMatch(
      /fällt nichts auf|Nichts hakt|Keine Verbesserung|unauffällig/,
    );
    // No big 0 "stuck" beside the limits: it would read as nothing wrong.
    expect(
      within(within(panel).getByRole("list", { name: "Bilanz" })).queryByText(
        "hakt",
      ),
    ).toBeNull();
  });

  // Why a period is not measurable, as the backend says: the page words
  // the reason, whatever its own metrics show. A missing release is no
  // tracing gap.
  it.each([
    [
      "bitbucket" as const,
      "Nicht messbar: Bitbucket liefert für das Team keine Daten.",
    ],
    [
      "argocd" as const,
      "Nicht messbar: Argo CD liefert für das Team keine Daten.",
    ],
    [
      "no_merged" as const,
      "Nicht messbar: In diesen Wochen wurde kein PR gemerged, der als Arbeit zählt.",
    ],
    ["no_release" as const, "In diesen Wochen ging nichts nach PROD."],
    [
      "to_prod" as const,
      `Nicht messbar: ${num(1234)} PRs wurden in diesen Wochen gemerged, keiner liess sich bis PROD verfolgen.`,
    ],
  ])("says why it is not measurable: %s", async (reason, want) => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return quietless({
          quiet_reasons: ["not_measurable"],
          not_measurable: reason,
          limits: [],
          coverage: {
            to_prod: { count: 0, n: 1234, rate: 0 },
            sized: { count: 0, n: 1234, rate: 0 },
            opened: { count: 0, n: 1234, rate: 0 },
          },
        });
      return metrics();
    });
    render(<App />);
    const panel = await screen.findByTestId("assessment");
    expect(
      within(panel).getByRole("note", {
        name: "Was diese Einschätzung einschränkt",
      }).textContent,
    ).toBe(want);
  });

  // Nothing compared at all, as the backend says: the limit says so, and
  // the head claims no comparison.
  it("says when nothing could be compared, and claims no comparison", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return quietless({
          quiet_reasons: ["no_baseline"],
          nothing_compared: true,
          limits: [],
        });
      const m = metrics();
      return { ...m, comparison: { ...m.comparison, any: false } };
    });
    render(<App />);
    const panel = await screen.findByTestId("assessment");
    expect(
      within(panel).getByRole("note", {
        name: "Was diese Einschätzung einschränkt",
      }).textContent,
    ).toBe(
      "Noch kein Vergleich möglich: Für die Vorperiode gibt es keine vergleichbaren Daten.",
    );
    expect(
      screen.getByText(/^Team team-a, alle Produkte/).textContent,
    ).not.toMatch(/verglichen/);
  });

  // In a product view the lost events leave deploys and builds uncompared
  // too, and a hole still running at the read has no end to name.
  it("names in a product what lost events leave uncompared, and a hole still open", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return quietless({
          quiet_reasons: ["source_gap"],
          limits: [
            {
              source: "bitbucket",
              kind: "silence",
              from: "2026-11-05T10:00:00Z",
              to: "2026-11-09T18:00:00Z",
            },
            {
              source: "bitbucket",
              kind: "silence",
              from: "2026-11-12T10:00:00Z",
              to: null,
            },
          ],
        });
      return metrics(4, { product: "kasse" });
    });
    render(<App />);
    const panel = await screen.findByTestId("assessment");
    const note = within(panel).getByRole("note", {
      name: "Was diese Einschätzung einschränkt",
    });
    expect([...note.children].map((c) => c.textContent)).toEqual([
      "Bitbucket lieferte vom 05.11.2026 bis 09.11.2026 keine Daten, obwohl Builds liefen: Ereignisse gingen verloren. Die PR-Zahlen bleiben ohne Urteil, und mit ihnen Deploys und Builds: Sie kommen über Bitbucket zum Produkt.",
      "Bitbucket liefert seit dem 12.11.2026 keine Daten, obwohl Builds laufen: Ereignisse gehen verloren. Die PR-Zahlen bleiben ohne Urteil, und mit ihnen Deploys und Builds: Sie kommen über Bitbucket zum Produkt.",
    ]);
  });

  // A product of libraries only: nothing to roll out, no hole, no limit.
  // Where libraries are left out of the PRs traced, the sentence says so.
  it("says a product holds only libraries, and whose PRs were traced", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return quietless({
          quiet_reasons: [],
          limited: false,
          libraries_only: true,
          library: 5,
          limits: [],
        });
      return metrics(4, { product: "kasse" });
    });
    render(<App />);
    const panel = await screen.findByTestId("assessment");
    expect(
      within(panel).getByText(
        "Dieses Produkt enthält nur Bibliotheken; sie werden nicht ausgerollt.",
      ),
    ).toBeInTheDocument();
    expect(within(panel).queryByRole("note")).toBeNull();
  });

  it("names the PRs of repos rolled out when libraries are left out", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return quietless({
          quiet_reasons: ["not_measurable"],
          not_measurable: "to_prod",
          library: 1,
          limits: [],
          coverage: {
            to_prod: { count: 0, n: 12, rate: 0 },
            sized: { count: 0, n: 13, rate: 0 },
            opened: { count: 0, n: 13, rate: 0 },
          },
        });
      return metrics();
    });
    render(<App />);
    const panel = await screen.findByTestId("assessment");
    expect(
      within(panel).getByRole("note", {
        name: "Was diese Einschätzung einschränkt",
      }).textContent,
    ).toBe(
      "Nicht messbar: 12 PRs aus Repos, die ausgerollt werden, wurden in diesen Wochen gemerged, keiner liess sich bis PROD verfolgen.",
    );
  });

  // Whether the period is limited is the backend's word, not a reading of
  // its reasons here: limited false shows the 0 stuck even beside a reason.
  it("takes limited from the backend", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return quietless({
          quiet_reasons: ["no_baseline", "findings"],
          limited: false,
          limits: [],
        });
      return metrics();
    });
    render(<App />);
    const panel = await screen.findByTestId("assessment");
    expect(
      within(within(panel).getByRole("list", { name: "Bilanz" })).getByText(
        "hakt",
      ),
    ).toBeInTheDocument();
  });

  // The agent wait's rule in the backend's numbers, never the page's own.
  it("words the agent-wait rule with the backend's numbers", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") return analysisStatus();
      const m = metrics();
      return {
        ...m,
        build: { ...m.build, agent_wait_min_s: 180, agent_wait_share: 0.4 },
      };
    });
    render(<App />);
    const panel = await screen.findByTestId("assessment");
    expect(
      within(panel).getByText(
        "40 % der typischen Build-Zeit oder mehr, und mindestens 3 Minuten, ist Warten auf einen Agent: die Kapazität der Plattform, nicht der Build.",
      ),
    ).toBeInTheDocument();
  });

  it("shows the rules' view beside an outdated analysis, and offers to assess again", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis")
        return analysisStatus({ state: "none", enabled: true, outdated: true });
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    expect(
      p.getByText(
        "Die letzte Einschätzung beruht auf älteren Regeln und wird nicht mehr gezeigt. Hier steht, was die aktuellen Regeln sehen. «Neu einschätzen» schreibt sie neu.",
      ),
    ).toBeInTheDocument();
    expect(
      p.getByRole("button", { name: "↻ Neu einschätzen" }),
    ).toBeInTheDocument();
    // No wording of its own: the rules' titles, no headline.
    expect(p.queryByRole("heading", { level: 3 })).toBeNull();
    expect(
      p.getByText("PRs gemerged pro Woche: deutlich besser"),
    ).toBeInTheDocument();
  });

  it("asks no question the backend did not raise, whatever a model wrote", async () => {
    mockFetch((url) => {
      if (url.pathname === "/api/facts") return facts();
      if (url.pathname === "/api/analysis") {
        const base = analysedStatus();
        return analysedStatus({
          assessment: { ...assessment(), question: null },
          layout: {
            ...base.layout,
            tally: { ...base.layout.tally, talk: 0 },
          },
        });
      }
      return metrics();
    });
    render(<App />);
    const p = within(await screen.findByTestId("assessment"));
    expect(p.queryByText(/Passt das zu unserer Art zu reviewen/)).toBeNull();
    expect(
      within(p.getByRole("list", { name: "Bilanz" })).queryByText(
        "zum Besprechen",
      ),
    ).toBeNull();
    // The model's retro questions stay.
    expect(p.getAllByText("Warum liegen diese PRs?")).toHaveLength(2);
  });

  it("says why a product shows no deploys, and what the team view leaves out", async () => {
    // portal deploys nothing of its own; kasse does.
    mockFetch((url) => {
      const m = sample(url);
      if (url.pathname !== "/api/metrics") return m;
      const mm = m as Metrics;
      const none = url.searchParams.get("product") === "portal";
      return {
        ...mm,
        delivery: {
          ...mm.delivery,
          frequency: {
            ...mm.delivery.frequency,
            // The backend says when they may be the product's: the page
            // decides nothing itself.
            ...(none
              ? {
                  syncs: 0,
                  releases: 0,
                  resyncs: 0,
                  first_seen: 0,
                  maybe_unattributed: true,
                }
              : {}),
            unattributed: 2,
            infra: 4,
            infra_apps: ["cert-sync", "log-shipper"],
            grace_days: 21,
          },
        },
      };
    });
    render(<App />);
    const infra = await screen.findByText(
      "4 Deploys von Infrastruktur-Apps nicht gezählt",
    );
    expect(infra).toHaveAttribute("title", "cert-sync, log-shipper");
    // The onboarding's grace as the backend sends it, never a number of
    // the page's own.
    expect(
      screen.getAllByText(/aus den ersten 21 Tagen der Argo-CD-Daten/).length,
    ).toBeGreaterThan(0);
    // In the team view they count: no word of them on the tile.
    expect(screen.queryByText(/keinem Produkt zuordenbar/)).toBeNull();
    fireEvent.click(
      screen.getAllByRole("button", { name: "Was heisst das?" })[0],
    );
    expect(
      screen.getByText(
        /Nicht gezählt: 4 Deploys von Infrastruktur-Apps, die keinem Produkt des Teams gehören \(cert-sync, log-shipper\)\./,
      ),
    ).toBeInTheDocument();

    const products = screen.getByRole("group", { name: "Produkt" });
    fireEvent.click(within(products).getByRole("button", { name: /portal/ }));
    expect(
      await screen.findByText("2 Deploys keinem Produkt zuordenbar"),
    ).toBeInTheDocument();
    expect(screen.queryByText("keine Deploys im Zeitraum")).toBeNull();
    expect(screen.queryByText(/Infrastruktur-Apps nicht gezählt/)).toBeNull();

    // A product with deploys of its own: they are not why it shows none.
    fireEvent.click(within(products).getByRole("button", { name: /kasse/ }));
    expect(
      await screen.findByText(/Releases? im Zeitraum/),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("2 Deploys keinem Produkt zuordenbar"),
    ).toBeNull();
  });

  it("counts open PRs of unknown state apart, builds without a repo and bots' builds", () => {
    const onFacts = vi.fn();
    const m = metrics();
    const mm: Metrics = {
      ...m,
      open_prs: { ...m.open_prs, unknown: 2 },
      build: {
        ...m.build,
        unattributed: 5,
        bot_runs: {
          runs: 1500,
          ci_seconds: 90000,
          failed: 300,
          failed_seconds: 18000,
        },
      },
      throughput: {
        ...m.throughput,
        library: 3,
        coverage: {
          ...m.throughput.coverage,
          to_prod: { count: 4, n: 6, rate: 4 / 6 },
        },
      },
      coverage: {
        ...m.coverage,
        merged: 6,
        on_prod: 4,
        lower_bound: 1,
        not_yet: 1,
        oldest_not_yet: "2026-10-20T09:00:00Z",
        not_placeable: 1,
        reasons: {
          no_merge_commit: 0,
          release_unresolved: 0,
          repo_untraced: 0,
          in_silence: 1,
        },
        library: 3,
      },
    };
    render(
      <>
        <Flow why={null} onWhy={() => {}} onFacts={onFacts} m={mm} />
        <Build m={mm} onFacts={onFacts} />
      </>,
    );
    const unknown = screen.getByText(/^2 PRs mit unbekanntem Zustand: /);
    expect(unknown.textContent).toMatch(/In keiner Zahl oben\./);
    fireEvent.click(within(unknown).getByRole("button", { name: "Ansehen →" }));
    expect(onFacts).toHaveBeenCalledWith("prs", "Unbekannt");
    expect(
      screen.getByText(
        "5 Builds ohne Repo: Weder ihr Commit noch ihr Pipeline-Name nennt eines. In der Teamansicht gezählt, in keiner Produktansicht.",
      ),
    ).toBeInTheDocument();
    // Bots' builds as what they cost, CI time, never a finding.
    expect(
      screen.getByText(
        `Bot-Builds: ${num(1500)} Builds, ${ciHours(90000)} CI-Zeit, davon 300 fehlgeschlagen (${ciHours(18000)}). Kosten, kein Befund: in keiner anderen Zahl dieses Abschnitts.`,
      ),
    ).toBeInTheDocument();
    // A library's PRs are out of the base of those traced to production,
    // said as such: never rolled out, never "not traced". The PRs not on
    // production yet stand next to it, the oldest's merge with them; the
    // ones tracing cannot place with why.
    const traced = screen.getByText(
      /4 von 6 gemergten PRs bis PROD verfolgt\./,
    ).textContent;
    expect(traced).toMatch(
      /1 gemergter PR noch nicht auf PROD, gemerged am 20\.10\.2026\./,
    );
    expect(traced).toMatch(
      /1 gemergter PR nicht zuordenbar: eine Lücke in den Daten \(1\)\./,
    );
    expect(traced).toMatch(
      /Bei 1 der verfolgten PRs ist die Zeit bis PROD höchstens so lang/,
    );
    expect(traced).toMatch(/3 PRs in Bibliotheken, werden nicht ausgerollt/);
  });

  // No merged PR to trace: no "0 of 0" under the line that says so.
  it("says nothing of tracing without a merged PR", () => {
    const m = metrics();
    render(
      <Flow
        why={null}
        onWhy={() => {}}
        onFacts={() => {}}
        m={{
          ...m,
          throughput: {
            ...m.throughput,
            merged: 0,
            coverage: {
              to_prod: { count: 0, n: 0, rate: null },
              sized: { count: 0, n: 0, rate: null },
              opened: { count: 0, n: 0, rate: null },
            },
          },
          coverage: { ...m.coverage, merged: 0, on_prod: 0, not_yet: 0 },
        }}
      />,
    );
    expect(screen.queryByText(/bis PROD verfolgt/)).toBeNull();
  });

  it("marks in the facts what counts nowhere and why", async () => {
    mockFetch((url) =>
      url.pathname === "/api/facts"
        ? facts({
            prs: {
              count: 2,
              rows: [
                prFact({
                  pr_id: 5,
                  state: "unknown",
                  merged_at: null,
                  prod_at: null,
                  to_prod_s: null,
                  idle_s: 9 * 86400,
                }),
                prFact({ pr_id: 6, not_counted: "housekeeping" }),
                prFact({ pr_id: 7, not_counted: "umbrella" }),
                prFact({ pr_id: 8, via_umbrella: true }),
                prFact({
                  pr_id: 9,
                  repo: "kasse/kasse-lib",
                  library: true,
                  prod_at: null,
                  to_prod_s: null,
                }),
              ],
            },
            deploys: {
              count: 3,
              rows: [
                deployFact({ app: "kasse-neu", first_deploy: true }),
                deployFact({
                  revision: "r9",
                  class: "failed",
                  phase: "Error",
                  new_images: [],
                }),
                deployFact({ app: "log-shipper", revision: "r8", infra: true }),
              ],
            },
          })
        : sample(url),
    );
    render(<App />);
    await screen.findByRole("tablist", { name: "Faktentabelle" });
    const data = within(document.getElementById("daten")!);
    expect(await data.findByText("Unbekannt")).toBeInTheDocument();
    expect(data.getByText("9.0 Tage ohne Event")).toBeInTheDocument();
    const housekeeping = data.getByText("Nicht gezählt: Aufräum-PR");
    expect(housekeeping.closest("tr")).toHaveAttribute("data-dim");
    const umbrella = data.getByText(
      "Nicht gezählt: Merge eines Sammel-Branches",
    );
    expect(umbrella.closest("tr")).toHaveAttribute("data-dim");
    // Counted, how it was merged said; not faded.
    const via = data.getByText("über Sammel-Branch gemerged").closest("tr")!;
    expect(via).not.toHaveAttribute("data-dim");
    // A library's PR is never rolled out: no "no" in the column to PROD.
    const lib = data.getByText("kasse/kasse-lib").closest("tr")!;
    expect(
      within(lib).getByText("Bibliothek, wird nicht ausgerollt"),
    ).toBeInTheDocument();
    expect(within(lib).queryByText("Nein")).toBeNull();
    fireEvent.click(data.getByRole("tab", { name: /Deploys/ }));
    const first = (await data.findByText("kasse-neu")).closest("tr")!;
    expect(within(first).getByText("Release")).toBeInTheDocument();
    expect(within(first).getByText("Erstdeploy")).toBeInTheDocument();
    expect(data.getByText("Error")).toBeInTheDocument();
    const infra = data.getByText("log-shipper").closest("tr")!;
    expect(
      within(infra).getByText("Infrastruktur, nicht gezählt"),
    ).toBeInTheDocument();
    expect(infra).toHaveAttribute("data-dim");
  });

  it("says when Bitbucket's events were lost, and when each source reports", async () => {
    mockFetch((url) => {
      const m = sample(url);
      if (url.pathname !== "/api/metrics") return m;
      const mm = m as Metrics;
      return {
        ...mm,
        meta: {
          ...mm.meta,
          silences: [
            {
              source: "bitbucket",
              from: "2026-11-05T10:00:00Z",
              to: "2026-11-09T18:00:00Z",
              unseen_runs: 4,
              lost: true,
              ongoing: false,
            },
            // A team that did not deploy, or push, for two days: no gap in
            // the data; the backend says which were lost.
            {
              source: "argocd",
              from: "2026-11-02T10:00:00Z",
              to: "2026-11-05T09:00:00Z",
              unseen_runs: 0,
              lost: false,
              ongoing: false,
            },
          ],
          // The page names the backend's list as it is, never a filter of
          // its own over `silences`; one still running has no end.
          lost: [
            {
              source: "bitbucket",
              from: "2026-11-05T10:00:00Z",
              to: "2026-11-09T18:00:00Z",
              unseen_runs: 4,
              lost: true,
              ongoing: false,
            },
            {
              source: "bitbucket",
              from: "2026-11-12T10:00:00Z",
              to: "2026-11-16T10:00:00Z",
              unseen_runs: 2,
              lost: true,
              ongoing: true,
            },
          ],
          repo_starts: [
            { repo: "kasse/kasse-api", noergler: "2026-10-01T08:00:00Z" },
            { repo: "portal/portal-web", noergler: null },
          ],
        },
      };
    });
    render(<App />);
    expect(
      await screen.findByText(
        "Bitbucket lieferte vom 05.11. 11:00 bis 09.11. 19:00 keine Daten, obwohl Builds liefen: Ereignisse gingen verloren.",
      ),
    ).toBeInTheDocument();
    expect(screen.getAllByText(/lieferte vom/)).toHaveLength(1);
    expect(
      screen.getByText(
        "Bitbucket liefert seit dem 12.11. 11:00 keine Daten, obwohl Builds laufen: Ereignisse gehen verloren.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText(/^noergler seit /)).toHaveAttribute(
      "title",
      "kasse/kasse-api: seit 01.10.2026\nportal/portal-web: keine Daten",
    );
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
