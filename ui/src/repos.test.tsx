// The per-repo view, the trend lines and the sticky header's navigation:
// what the backend sends, drawn and worded; the computing is the backend's.

import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import type { RepoSpread } from "./api";
import { SectionNav, sectionOf, useActiveSection } from "./nav";
import { Ranking } from "./Ranking";
import {
  RepoStrip,
  SpreadLines,
  StripBlock,
  spreadOf,
  unitValue,
} from "./repostrip";
import { num } from "./format";
import i18n from "./i18n";
import { analysisStatus, facts, metrics, trends } from "./test/fixtures";
import { TrendChart } from "./trends";

const t = i18n.t.bind(i18n);

describe("repo strip", () => {
  const m = metrics();
  const rel = spreadOf(m, "releases")!;

  it("draws a dot per repo by the backend's verdict, apart at the end", () => {
    const onRepo = vi.fn();
    render(<RepoStrip s={rel} label="Releases" onRepo={onRepo} />);
    const dots = screen.getAllByTestId("strip-dot");
    expect(dots.map((d) => d.getAttribute("data-verdict"))).toEqual([
      "within",
      "within",
      "hint",
      "apart",
    ]);
    // Dots at one value stack instead of overprinting each other.
    expect(dots[0].getAttribute("cx")).toBe(dots[1].getAttribute("cx"));
    expect(dots[0].getAttribute("cy")).not.toBe(dots[1].getAttribute("cy"));
    // The set-apart dot sits right of every placed one.
    const cx = dots.map((d) => Number(d.getAttribute("cx")));
    expect(cx[3]).toBeGreaterThan(Math.max(...cx.slice(0, 3)));
    // The caption reads alone: what is missing, and when.
    expect(screen.getByText("kein Release im Zeitraum")).toBeInTheDocument();
    expect(screen.getByText("typisch")).toBeInTheDocument();
  });

  it("names the repo and its value on hover and focus, opens it on click and Enter", () => {
    const onRepo = vi.fn();
    render(<RepoStrip s={rel} label="Releases" onRepo={onRepo} />);
    const dot = screen.getByRole("button", { name: /^lager\/lager-ui: / });
    fireEvent.mouseEnter(dot);
    const tip = screen.getByRole("tooltip");
    expect(tip).toHaveTextContent("lager/lager-ui");
    expect(tip).toHaveTextContent("0.25 pro Woche · 4 Releases im Zeitraum");
    fireEvent.mouseLeave(dot);
    expect(screen.queryByRole("tooltip")).toBeNull();
    fireEvent.focus(dot);
    expect(screen.getByRole("tooltip")).toBeInTheDocument();
    fireEvent.click(dot);
    fireEvent.keyDown(dot, { key: "Enter" });
    expect(onRepo).toHaveBeenCalledTimes(2);
    expect(onRepo).toHaveBeenCalledWith("lager/lager-ui");
    // The verdict in words, not by colour alone.
    expect(tip).toHaveTextContent(
      "0.25 pro Woche · 4 Releases im Zeitraum · Hinweis",
    );
    const apart = screen.getByRole("button", { name: /^lager\/lager-api: / });
    expect(apart).toHaveAccessibleName(
      "lager/lager-api: kein Release im Zeitraum · Abmachung verletzt",
    );
    expect(
      screen.getAllByRole("button", { name: /^kasse\/kasse-api: / })[0],
    ).toHaveAccessibleName(
      "kasse/kasse-api: 1.0 pro Woche · 4 Releases im Zeitraum · innerhalb der Abmachung",
    );
  });

  it("is one tab stop, the arrow keys move between its dots", () => {
    render(<RepoStrip s={rel} label="Releases" />);
    const dots = screen.getAllByTestId("strip-dot");
    expect(dots.map((d) => d.getAttribute("tabindex"))).toEqual([
      "0",
      "-1",
      "-1",
      "-1",
    ]);
    dots[0].focus();
    fireEvent.keyDown(dots[0], { key: "ArrowRight" });
    expect(document.activeElement).toBe(dots[1]);
    expect(dots[1]).toHaveAttribute("tabindex", "0");
    expect(dots[0]).toHaveAttribute("tabindex", "-1");
    fireEvent.keyDown(dots[1], { key: "End" });
    expect(document.activeElement).toBe(dots[3]);
    fireEvent.keyDown(dots[3], { key: "ArrowRight" });
    expect(document.activeElement).toBe(dots[0]);
  });

  it("sets the threshold's label and the typical one apart where they meet", () => {
    const prod = spreadOf(m, "to_prod")!;
    const { container } = render(
      <RepoStrip s={{ ...prod, typical_x: 0.7 }} label="Bis PROD" />,
    );
    const anchor = (text: string) =>
      [...container.querySelectorAll("text")]
        .find((e) => e.textContent === text)
        ?.getAttribute("text-anchor");
    // The mark (0.68) left of the typical line (0.7): its label reads to
    // the left, the typical one's to the right.
    expect(anchor("14 Tage")).toBe("end");
    expect(anchor("typisch")).toBe("start");
  });

  it("stacks every dot of a crowded spot where it can be reached", () => {
    const crowd = {
      ...rel,
      dots: Array.from({ length: 15 }, (_, i) => ({
        ...rel.dots[0],
        repo: `kasse/r${i}`,
      })),
    };
    render(<RepoStrip s={crowd} label="Releases" />);
    const spots = screen
      .getAllByTestId("strip-dot")
      .map((d) => `${d.getAttribute("cx")},${d.getAttribute("cy")}`);
    expect(new Set(spots).size).toBe(15);
  });

  it("draws nothing the backend did not show", () => {
    const { container } = render(
      <RepoStrip s={{ ...rel, shown: false }} label="x" />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("says what the team's number hides, in the backend's terms", () => {
    const lines = (s: RepoSpread) => {
      const { container, unmount } = render(<SpreadLines s={s} />);
      const out = [...container.querySelectorAll("span")].map(
        (e) => e.textContent,
      );
      unmount();
      return out;
    };
    expect(lines(rel)).toEqual([
      "Das typische Repo: 1.0 pro Woche, aus 3 Repos",
      "2 von 3 Repos liegen nahe beim typischen Repo, 1 weit darunter.",
    ]);
    expect(lines(spreadOf(m, "merged")!)).toContain(
      "kasse/kasse-api allein bringt 60 % aller gemergten PRs des Teams.",
    );
    expect(lines(spreadOf(m, "batch")!)).toContain(
      "Die Zahl des Teams nimmt alle PRs mit bekannter Grösse in einen Topf: Repos mit vielen davon bestimmen sie. Das typische Repo liegt bei 80 Zeilen.",
    );
    expect(lines(spreadOf(m, "to_prod")!)).toEqual([
      "Das typische Repo: 9.5 Tage, aus 2 Repos",
      "3 Repos mit zu wenigen PRs für einen typischen Wert",
    ]);
  });

  it("says in a line what a strip of one value would draw on one spot", () => {
    const pickup = spreadOf(m, "pickup")!;
    render(<StripBlock s={pickup} title="Pro Repo: Warten" />);
    expect(screen.queryAllByTestId("strip-dot")).toHaveLength(0);
    expect(
      screen.getByText(`Alle 3 Repos: < ${num(0.1, 1)} Arbeitstage`),
    ).toBeInTheDocument();
    expect(
      screen.getByText("2 Repos mit zu wenigen PRs für einen typischen Wert"),
    ).toBeInTheDocument();
    expect(screen.queryByText(/typische Repo/)).toBeNull();
  });

  it("says what is set apart or too thin even without a strip", () => {
    // One repo placed, two that released nothing: no strip, still the line.
    const { unmount } = render(
      <StripBlock
        s={{ ...rel, shown: false, same: null, apart: 2, thin: 0 }}
        title="Pro Repo: Releases"
      />,
    );
    expect(
      screen.getByText("2 Repos haben im Zeitraum nichts ausgerollt."),
    ).toBeInTheDocument();
    unmount();
    // Every repo too thin: the line that says so.
    render(
      <StripBlock
        s={{
          ...rel,
          shown: false,
          same: null,
          apart: 0,
          thin: 3,
          typical: null,
          note: null,
          dots: [],
        }}
        title="Pro Repo: Releases"
      />,
    );
    expect(
      screen.getByText(
        "3 Repos mit zu wenigen Releases für einen typischen Wert",
      ),
    ).toBeInTheDocument();
  });

  it("writes each unit's value", () => {
    expect(unitValue(t, "days", 14)).toBe("14 Tage");
    expect(unitValue(t, "days", 2.5)).toBe("2.5 Tage");
    expect(unitValue(t, "working_days", 1)).toBe("1 Arbeitstage");
    expect(unitValue(t, "lines", 1200)).toBe(`${num(1200)} Zeilen`);
    expect(unitValue(t, "seconds", 900)).toBe("15 min");
    expect(unitValue(t, "share", 0.95)).toBe("95 %");
    expect(unitValue(t, "per_week", 12)).toBe("12 pro Woche");
    expect(unitValue(t, "per_week", 0)).toBe("0 pro Woche");
  });
});

describe("ranking", () => {
  it("opens the backend's rows by group, summarises the rest, shows all on demand", () => {
    const onRepo = vi.fn();
    render(<Ranking m={metrics()} onRepo={onRepo} />);
    const rank = screen.getByTestId("ranking");
    expect(within(rank).getAllByTestId("rank-row")).toHaveLength(8);
    // The groups say what the rule decides, cut by the team's thresholds.
    for (const g of [
      "Kein Release seit über 28 Tagen",
      "Letztes Release vor 15–28 Tagen",
      "Nicht beurteilbar",
      "Release in den letzten 14 Tagen",
    ])
      expect(within(rank).getAllByText(g).length).toBeGreaterThan(0);
    expect(
      within(rank).getByText(
        "beim Projekt des Repos, obwohl daran gearbeitet wurde",
      ),
    ).toBeInTheDocument();
    // How a row writes its releases is the backend's: the count over its
    // weeks under one a week, the project's last release below.
    expect(within(rank).getByText("1 Release in 4 Wochen")).toBeInTheDocument();
    expect(
      within(rank).getAllByText("Projekt: letztes vor 19 Tagen").length,
    ).toBeGreaterThan(0);
    // The pills: the release rule broken, a hint, what cannot be judged.
    expect(
      within(rank).getByText("seit 37 Tagen kein Release"),
    ).toBeInTheDocument();
    expect(
      within(rank).getByText("letztes Release vor 19 Tagen"),
    ).toBeInTheDocument();
    expect(
      within(rank).getByText("keine App bekannt, die es ausrollt"),
    ).toBeInTheDocument();
    expect(
      within(rank).getByText("typisch 16.5 Tage bis PROD"),
    ).toBeInTheDocument();
    // A repo that rolls nothing out: its wait reads alone, its PRs below.
    expect(within(rank).getByText("kein PR auf PROD")).toBeInTheDocument();
    expect(
      within(rank).getByText(/3 PRs noch nicht auf PROD/),
    ).toBeInTheDocument();
    // The hidden rows, a line per group: a group partly open reads as the
    // rest of the group above, never as a second group of the same name; a
    // group with no row open gets its head and rule.
    const more = within(rank).getAllByTestId("rank-more");
    expect(more.map((r) => r.textContent)).toEqual([
      "und 1 weiteres Repo dieser Gruppe: kasse-scan",
      expect.stringMatching(
        /^Libraries1werden nie selbst ausgerollt, darum fehlt hier kein Release.*kasse-lib$/,
      ),
    ]);
    expect(
      within(rank).getAllByText("Release in den letzten 14 Tagen"),
    ).toHaveLength(1);
    fireEvent.click(
      within(rank).getByRole("button", { name: "alle 9 zeigen" }),
    );
    expect(within(rank).getAllByTestId("rank-row")).toHaveLength(9);
    expect(within(rank).queryByTestId("rank-more")).toBeNull();
    expect(
      within(rank).getByRole("button", { name: "nur die ersten 8 zeigen" }),
    ).toHaveAttribute("aria-expanded", "true");
    // A row opens the repo's facts, by mouse and by keyboard.
    const row = within(rank).getAllByTestId("rank-row")[0];
    fireEvent.click(row);
    fireEvent.keyDown(row, { key: "Enter" });
    expect(onRepo).toHaveBeenCalledWith("lager/lager-api");
    expect(onRepo).toHaveBeenCalledTimes(2);
  });

  it("shows nothing without a repo", () => {
    const m = metrics();
    const { container } = render(
      <Ranking
        m={{ ...m, repos: { ...m.repos, rows: [] } }}
        onRepo={() => {}}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });
});

describe("trend lines", () => {
  const tr = trends(4);

  it("breaks the line at a gap, hatches it and marks the running week", () => {
    render(
      <TrendChart
        weeks={tr.weeks}
        split={tr.split}
        series={tr.merged}
        format={(v) => String(v)}
        label="Gemergte PRs"
      />,
    );
    const chart = screen.getByTestId("trend-chart");
    // KW before Bitbucket's data and the lost week: two gaps, never zero.
    expect(screen.getAllByTestId("trend-gap")).toHaveLength(2);
    // The weeks between the gaps are two lines; the running week apart.
    expect(chart.querySelectorAll("path")).toHaveLength(2);
    expect(screen.getByTestId("trend-running")).toBeInTheDocument();
    expect(chart).toHaveAccessibleName(
      /KW 47 \d+ \(laufende Woche, bis jetzt\)/,
    );
    expect(chart).toHaveAccessibleName(
      /KW 39 keine Daten: Die Quelle lieferte damals noch nichts/,
    );
    expect(chart.textContent).toContain("Vorperiode");
  });

  it("draws a week alone between gaps as a dot on the small line", () => {
    const series = {
      values: [null, 3, null, 4, 5],
      n: [0, 3, 0, 4, 5],
      gaps: ["no_data", "", "lost", "", ""] as const,
    };
    render(
      <TrendChart
        weeks={tr.weeks.slice(0, 5).map((w) => ({ ...w, running: false }))}
        split={2}
        series={{ ...series, gaps: [...series.gaps] }}
        format={(v) => String(v)}
        label="x"
        small
      />,
    );
    const spark = screen.getByTestId("trend-spark");
    expect(spark.querySelectorAll("path")).toHaveLength(1);
    expect(spark.querySelectorAll("circle")).toHaveLength(1);
  });
});

describe("sticky header", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    // The page height a test set goes with it.
    delete (document.documentElement as { scrollHeight?: number }).scrollHeight;
  });

  it("marks 'more' while one of its sections is read, and closes on Escape or outside", () => {
    render(<SectionNav active="kosten" />);
    const nav = screen.getByRole("navigation", { name: "Bereiche" });
    const more = nav.querySelector("details")!;
    const summary = more.querySelector("summary")!;
    expect(summary).toHaveAttribute("aria-current", "location");
    expect(summary).toHaveTextContent("Kosten");
    more.open = true;
    fireEvent.keyDown(document, { key: "Escape" });
    expect(more.open).toBe(false);
    more.open = true;
    fireEvent.pointerDown(document.body);
    expect(more.open).toBe(false);
    more.open = true;
    fireEvent.pointerDown(summary);
    expect(more.open).toBe(true);
  });

  it("reads a malformed link's section as it stands", () => {
    expect(sectionOf("#%E0")).toBe("%E0");
    expect(sectionOf("#%")).toBe("%");
    expect(sectionOf("#review")).toBe("review");
    expect(sectionOf("#f%C3%BCr")).toBe("für");
  });

  it("marks the section being read and keeps every link's target", () => {
    render(<SectionNav active="fluss" />);
    const nav = screen.getByRole("navigation", { name: "Bereiche" });
    expect(within(nav).getByRole("link", { name: "Fluss" })).toHaveAttribute(
      "aria-current",
      "location",
    );
    expect(
      within(nav).getByRole("link", { name: "Review" }),
    ).not.toHaveAttribute("aria-current");
    // The rest behind "Weitere", closed until asked for; a link closes it.
    const more = nav.querySelector("details")!;
    expect(more.open).toBe(false);
    more.open = true;
    fireEvent.click(
      within(nav).getByRole("link", { name: "Kosten", hidden: true }),
    );
    expect(more.open).toBe(false);
  });

  it("marks the last section whose top passed under the header", () => {
    // Three sections at fixed places on a page scrolled by `y`.
    let y = 0;
    const tops: Record<string, number> = { a: 0, b: 600, c: 1200 };
    // A page far longer than the window: never at its bottom here.
    Object.defineProperty(document.documentElement, "scrollHeight", {
      configurable: true,
      value: 50_000,
    });
    document.body.innerHTML = "";
    for (const id of Object.keys(tops)) {
      const el = document.createElement("div");
      el.id = id;
      el.getBoundingClientRect = () => ({ top: tops[id] - y }) as DOMRect;
      document.body.appendChild(el);
    }
    const seen: IntersectionObserverCallback[] = [];
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        constructor(cb: IntersectionObserverCallback) {
          seen.push(cb);
        }
        observe() {}
        disconnect() {}
      },
    );
    const ids = ["a", "b", "c"] as const;
    const head = () => 56;
    let active = "";
    function Probe() {
      active = useActiveSection(ids, true, head);
      return null;
    }
    render(<Probe />);
    expect(active).toBe("a");
    y = 560;
    act(() => {
      window.dispatchEvent(new Event("scroll"));
    });
    expect(active).toBe("b");
    y = 1150;
    act(() => {
      seen[0]([], {} as IntersectionObserver);
    });
    expect(active).toBe("c");
  });

  it("marks by the page's order, not the list's", () => {
    let y = 0;
    const tops: Record<string, number> = { a: 0, z: 600, b: 1200 };
    Object.defineProperty(document.documentElement, "scrollHeight", {
      configurable: true,
      value: 50_000,
    });
    document.body.innerHTML = "";
    for (const id of Object.keys(tops)) {
      const el = document.createElement("div");
      el.id = id;
      el.getBoundingClientRect = () => ({ top: tops[id] - y }) as DOMRect;
      document.body.appendChild(el);
    }
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
    // "z" is behind "more", listed last, read between a and b.
    const ids = ["a", "b", "z"] as const;
    let active = "";
    function Probe() {
      active = useActiveSection(ids, true, () => 56);
      return null;
    }
    render(<Probe />);
    y = 560;
    act(() => {
      window.dispatchEvent(new Event("scroll"));
    });
    expect(active).toBe("z");
  });
});

describe("deep links", () => {
  const scrollIntoView = Element.prototype.scrollIntoView;
  beforeEach(() => {
    window.history.replaceState(null, "", "/?w=12#review");
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    Element.prototype.scrollIntoView = scrollIntoView;
    window.history.replaceState(null, "", "/");
  });

  it("keep the section through a filter change and land there on load", async () => {
    const into = vi.fn();
    Element.prototype.scrollIntoView = into;
    const m = metrics(12);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (raw: string) => {
        const url = new URL(raw, "http://riptide.test");
        if (url.pathname === "/api/teams")
          return new Response(JSON.stringify({ teams: ["team-a"] }));
        if (url.pathname === "/api/facts")
          return new Response(JSON.stringify(facts()));
        if (url.pathname === "/api/analysis")
          return new Response(JSON.stringify(analysisStatus()));
        return new Response(JSON.stringify(m));
      }),
    );
    render(<App />);
    await screen.findAllByText(/KW 35–46/);
    // Landed on the section once the numbers were in.
    await vi.waitFor(() => expect(into).toHaveBeenCalled());
    expect(into.mock.contexts[0]).toBe(document.getElementById("review"));
    fireEvent.click(screen.getByRole("button", { name: "4 W" }));
    // Four weeks are the default: no query left, the section kept.
    expect(window.location.search).toBe("");
    expect(window.location.hash).toBe("#review");
  });
});
