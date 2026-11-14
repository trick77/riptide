// The page's state lives in the query string, so a link or a printed
// report reproduces the view: ?team=…&product=…&w=12&bots=1&q=…

import { WINDOWS, type WindowWeeks } from "./period";

export type Tab = "prs" | "deploys" | "runs";

export type ViewState = {
  team: string | null;
  product: string | null;
  weeks: WindowWeeks;
  bots: boolean;
  q: string;
  tab: Tab;
};

const TABS: Tab[] = ["prs", "deploys", "runs"];

export function parseState(search: string): ViewState {
  const q = new URLSearchParams(search);
  const w = Number(q.get("w"));
  const tab = q.get("tab") as Tab;
  return {
    team: q.get("team") || null,
    product: q.get("product") || null,
    weeks: (WINDOWS as readonly number[]).includes(w) ? (w as WindowWeeks) : 4,
    bots: q.get("bots") === "1",
    q: q.get("q") ?? "",
    tab: TABS.includes(tab) ? tab : "prs",
  };
}

export function stateQuery(s: ViewState): string {
  const q = new URLSearchParams();
  if (s.team) q.set("team", s.team);
  if (s.product) q.set("product", s.product);
  if (s.weeks !== 4) q.set("w", String(s.weeks));
  if (s.bots) q.set("bots", "1");
  if (s.q) q.set("q", s.q);
  if (s.tab !== "prs") q.set("tab", s.tab);
  const str = q.toString();
  return str ? `?${str}` : "";
}
