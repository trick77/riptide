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
import { num, weekSpan } from "./format";
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

  // A repo's own releases have no threshold of its own: the backend sends
  // every dot neutral and sets apart each repo without one, which reads
  // as such, never as an agreement broken.
  it("keeps a dot set apart neutral where the backend judges none", () => {
    const neutral: RepoSpread = {
      ...rel,
      dots: rel.dots.map((d) => ({ ...d, verdict: "neutral" })),
    };
    render(<RepoStrip s={neutral} label="Releases" />);
    const apart = screen.getByRole("button", { name: /^lager\/lager-api: / });
    expect(apart).toHaveAccessibleName(
      "lager/lager-api: kein Release im Zeitraum",
    );
    expect(apart.getAttribute("stroke")).not.toMatch(/danger/);
    expect(
      screen.getByText("kein Release im Zeitraum").getAttribute("fill"),
    ).toBe("var(--color-muted)");
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
    // The groups say what the rule decides, cut by the team's thresholds,
    // and that it is the project's release in the heading itself.
    for (const g of [
      "Projekt ohne Release seit über 28 Tagen",
      "Letztes Release des Projekts vor 15–28 Tagen",
      "Nicht beurteilbar",
      "Projekt mit Release in den letzten 14 Tagen",
    ])
      expect(within(rank).getAllByText(g).length).toBeGreaterThan(0);
    expect(
      within(rank).getByText("obwohl daran gearbeitet wurde"),
    ).toBeInTheDocument();
    expect(within(rank).queryByText(/beim Projekt des Repos/)).toBeNull();
    // A heading that says all has no sub-line, never a key.
    expect(rank.textContent).not.toMatch(/repos\.groupSub/);
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
      within(rank).getAllByText("Projekt mit Release in den letzten 14 Tagen"),
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

  // A project never rolled out is no breach (owner, 2026-11-26): its own
  // group right after the breach, neutral; the row says so where the
  // others name the project's last release, and its waiting work keeps
  // its red flag.
  it("sets a project never rolled out apart, neutral", () => {
    const m = metrics();
    const v = m.repos;
    const never = {
      ...v.rows[0],
      repo: "post/post-api",
      project: "post",
      group: "never" as const,
      last_release: null,
      days_since: null,
      merged: 5,
      flags: [
        { key: "not_on_prod" as const, tone: "breach" as const, value: 20 },
      ],
    };
    render(
      <Ranking
        m={{
          ...m,
          repos: {
            ...v,
            rows: [v.rows[0], never, ...v.rows.slice(1)],
            open: 9,
            groups: [
              v.groups[0],
              { key: "never", count: 1, hidden_count: 0, hidden: [], more: 0 },
              ...v.groups.slice(1),
            ],
          },
        }}
        onRepo={() => {}}
      />,
    );
    const rank = screen.getByTestId("ranking");
    const head = within(rank).getByText("Noch kein PROD-Release");
    expect(head.parentElement!.querySelector("span")!.className).not.toMatch(
      /bg-danger/,
    );
    const row = within(rank).getAllByTestId("rank-row")[1];
    expect(row).toHaveTextContent("noch kein PROD-Release des Projekts");
    expect(row).toHaveTextContent("ältester PR seit 20 Tagen nicht auf PROD");
    // Nothing of it on production, said without the breach's red.
    const lying = within(row).getByText("kein PR auf PROD");
    expect(lying.className).not.toMatch(/danger/);
    expect(
      within(rank).queryByText(/kein Release des Projekts nach/),
    ).toBeNull();
  });

  // The count over the weeks reads as German counts them, and a rate of
  // none is 0, as the strips write it.
  it("writes one week and none as such", () => {
    const m = metrics();
    const v = m.repos;
    const first = {
      ...v.rows[3],
      releases: 1,
      releases_per_week: null,
      releases_base: 1,
      releases_form: "in_weeks" as const,
      merged: 0,
      merged_per_week: 0,
    };
    render(
      <Ranking
        m={{ ...m, repos: { ...v, rows: [first], open: 1, groups: [] } }}
        onRepo={() => {}}
      />,
    );
    const row = screen.getByTestId("rank-row");
    expect(row).toHaveTextContent("1 Release in 1 Woche");
    expect(row).not.toHaveTextContent("1 Wochen");
    expect(row).not.toHaveTextContent("0.00");
  });

  // A span across a new year names both years, so 52 weeks never read as
  // "KW 48–47"; within one year the short form stays.
  it("names the years of a span across a new year", () => {
    const m = metrics();
    const weeks = m.trends.weeks.map((w, i) => ({
      ...w,
      year: i < m.trends.split ? 2025 : 2026,
    }));
    render(
      <Ranking
        m={{ ...m, trends: { ...m.trends, weeks } }}
        onRepo={() => {}}
      />,
    );
    const first = weeks[0].kw;
    const last = weeks.filter((w) => !w.running).at(-1)!.kw;
    expect(
      screen.getByText(
        `Die Balken zeigen die Releases pro Woche, KW ${first}/2025–KW ${last}/2026; die Vorperiode ist hinterlegt.`,
      ),
    ).toBeInTheDocument();
    expect(weekSpan(t, weeks[4], weeks[7])).toBe(
      `KW ${weeks[4].kw}–${weeks[7].kw}`,
    );
    // An assessment stored before the year: the short form.
    expect(weekSpan(t, { ...weeks[0], year: 0 }, weeks[7])).toBe(
      `KW ${first}–${weeks[7].kw}`,
    );
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
