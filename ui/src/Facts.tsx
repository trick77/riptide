// The single facts behind every number, searched by the backend: PRs,
// deploys, runs.

import type { ReactNode } from "react";
import type { DeployFact, Facts as FactsData, PRFact } from "./api";
import { KIND_LABEL } from "./Build";
import { dateTime, dur, shortImage, shortSha } from "./format";
import type { Tab } from "./state";
import { Mono, Pill, SectionHead, type Tone } from "./ui";

// The backend searches these words too (metrics.prStateTerm, deployClass):
// change one here, change it there.
const PR_STATE: Record<PRFact["state"], [string, Tone]> = {
  merged: ["Gemergt", "ok"],
  open: ["Offen", "none"],
  declined: ["Abgelehnt", "none"],
  deleted: ["Gelöscht", "none"],
};

// A failed sync shows its phase.
const DEPLOY_CLASS: Record<DeployFact["class"], [string | null, Tone]> = {
  failed: [null, "fail"],
  release: ["Release", "ok"],
  first_seen: ["Erstdeploy", "warn"],
  sync: ["Sync ohne neue Images", "none"],
};

const RUN_TONE: Record<string, Tone> = {
  SUCCESS: "ok",
  FAILURE: "fail",
  UNSTABLE: "warn",
  ABORTED: "none",
};

type Col = { label: string; right?: boolean };

function Table({ cols, rows }: { cols: Col[]; rows: ReactNode[][] }) {
  return (
    <table className="w-full min-w-[760px] border-separate border-spacing-0 text-[13.5px]">
      <thead>
        <tr>
          {cols.map((c, i) => (
            <th
              key={c.label}
              className={`border-b border-border pb-2 text-[11px] font-medium tracking-[.12em] text-muted uppercase ${
                c.right ? "text-right" : "text-left"
              } ${i < cols.length - 1 ? "pr-2.5" : ""}`}
            >
              {c.label}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((r, i) => (
          <tr key={i} className="hover:bg-active">
            {r.map((cell, j) => (
              <td
                key={j}
                className={`border-b border-border-soft py-[7px] text-ink-dim ${cols[j].right ? "text-right tabular-nums whitespace-nowrap" : ""} ${
                  j < r.length - 1 ? "pr-2.5" : ""
                }`}
              >
                {cell}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export function Facts({
  facts,
  q,
  tab,
  onTab,
}: {
  facts: FactsData;
  q: string;
  tab: Tab;
  onTab: (t: Tab) => void;
}) {
  const prod = facts.production_stage;
  const counts: Record<Tab, number> = {
    prs: facts.prs.count,
    deploys: facts.deploys.count,
    runs: facts.runs.count,
  };
  const tabs: [Tab, string][] = [
    ["prs", "PRs"],
    ["deploys", "Deploys"],
    ["runs", "Läufe"],
  ];

  let table: ReactNode;
  if (tab === "prs") {
    table = (
      <Table
        cols={[
          { label: "PR" },
          { label: "Repo" },
          { label: "Typ" },
          { label: "Review", right: true },
          { label: "Cycle", right: true },
          { label: `bis ${prod}`, right: true },
          { label: "Status" },
        ]}
        rows={facts.prs.rows.map((p) => [
          <>
            {p.jira_prefix && (
              <Mono className="text-accent-strong">{p.jira_prefix} </Mono>
            )}
            {p.title ?? <span className="text-muted">#{p.pr_id}</span>}
          </>,
          <Mono className="whitespace-nowrap">{p.repo}</Mono>,
          <span className="text-muted">
            {p.gitops ? "Promotion" : (p.change_type ?? "–")}
            {p.bot ? " · Bot" : ""}
          </span>,
          dur(p.review_s),
          dur(p.cycle_s),
          p.merged_at ? (p.prod_at ? dur(p.to_prod_s) : "noch nicht") : "–",
          <Pill tone={PR_STATE[p.state][1]}>{PR_STATE[p.state][0]}</Pill>,
        ])}
      />
    );
  } else if (tab === "deploys") {
    table = (
      <Table
        cols={[
          { label: "Zeitpunkt" },
          { label: "Argo-App" },
          { label: "Stufe" },
          { label: "Revision" },
          { label: "Neue Images" },
          { label: "Zählt als" },
        ]}
        rows={facts.deploys.rows.map((d) => [
          <span className="whitespace-nowrap tabular-nums">
            {dateTime(d.at)}
          </span>,
          <Mono className="whitespace-nowrap">{d.app}</Mono>,
          <span className="text-muted">{d.environment ?? "–"}</span>,
          <Mono className="text-muted">{shortSha(d.revision)}</Mono>,
          <Mono>
            {d.new_images.length
              ? d.new_images.map(shortImage).join(", ")
              : "–"}
          </Mono>,
          <Pill tone={DEPLOY_CLASS[d.class][1]}>
            {DEPLOY_CLASS[d.class][0] ?? d.phase}
          </Pill>,
        ])}
      />
    );
  } else {
    table = (
      <Table
        cols={[
          { label: "Pipeline" },
          { label: "Lauf", right: true },
          { label: "Art" },
          { label: "Commit" },
          { label: "Dauer", right: true },
          { label: "Images" },
          { label: "Status" },
        ]}
        rows={facts.runs.rows.map((r) => [
          <Mono className="whitespace-nowrap">{r.pipeline_name}</Mono>,
          <span className="text-muted">#{r.run_id}</span>,
          <span className="text-muted">{KIND_LABEL[r.kind]}</span>,
          <Mono className="text-muted">{shortSha(r.commit_sha)}</Mono>,
          dur(r.duration_s),
          <Mono>
            {r.image_refs.length
              ? r.image_refs.map(shortImage).join(", ")
              : "–"}
          </Mono>,
          r.status ? (
            <Pill tone={RUN_TONE[r.status.toUpperCase()] ?? "none"}>
              {r.status}
            </Pill>
          ) : (
            "–"
          ),
        ])}
      />
    );
  }

  return (
    <section id="daten" aria-labelledby="h-daten" className="mt-10">
      <SectionHead
        id="h-daten"
        eyebrow="Einzelfakten hinter allen Zahlen"
        title="Daten"
      />
      <div className="rounded-ui-lg border border-border bg-panel px-[18px] pt-3.5 pb-2">
        <div className="flex flex-wrap items-center justify-between gap-2.5">
          <div
            role="tablist"
            aria-label="Faktentabelle"
            className="inline-flex gap-0.5"
          >
            {tabs.map(([key, label]) => (
              <button
                key={key}
                type="button"
                role="tab"
                aria-selected={tab === key}
                onClick={() => onTab(key)}
                className={`rounded-ui-sm border-0 px-3 py-1.5 text-[13.5px] ${
                  tab === key
                    ? "bg-active text-ink"
                    : "bg-transparent text-muted"
                }`}
              >
                {label}{" "}
                <span className="text-muted tabular-nums">{counts[key]}</span>
              </button>
            ))}
          </div>
          <span className="text-[12.5px] text-muted">
            {q
              ? `gefiltert nach «${q}»`
              : "Neueste zuerst · die Suche oben filtert alle Tabellen"}
            {counts[tab] > facts.limit
              ? ` · die ersten ${facts.limit} von ${counts[tab]}`
              : ""}
          </span>
        </div>
        <div className="mt-2.5 overflow-x-auto">
          {counts[tab] === 0 ? (
            <p className="my-3.5 text-[13.5px] text-muted">
              {q ? `Keine Treffer für «${q}».` : "Keine Einträge im Zeitraum."}
            </p>
          ) : (
            table
          )}
        </div>
      </div>
    </section>
  );
}
