// The single facts behind every number, searchable: PRs, deploys, runs.

import { type ReactNode, useMemo } from "react";
import type { Dataset, Deploy, PR, Run } from "./api";
import { KIND_LABEL } from "./Build";
import { dateTime, dur, shortImage, shortSha } from "./format";
import { inProduct, runKind, type Filter } from "./metrics";
import type { Tab } from "./state";
import { between } from "./stats";
import { Mono, Pill, SectionHead, type Tone } from "./ui";

// Rows past this are left out of the table, not of any number.
const LIMIT = 300;

const PR_STATE: Record<PR["state"], [string, Tone]> = {
  merged: ["Gemergt", "ok"],
  open: ["Offen", "none"],
  declined: ["Abgelehnt", "none"],
  deleted: ["Gelöscht", "none"],
};

const RUN_TONE: Record<string, Tone> = {
  SUCCESS: "ok",
  FAILURE: "fail",
  UNSTABLE: "warn",
  ABORTED: "none",
};

function deployKind(d: Deploy): [string, Tone] {
  if (d.phase !== "Succeeded") return [d.phase, "fail"];
  if (d.kind === "release") return ["Release", "ok"];
  if (d.kind === "first_seen") return ["Erstdeploy", "warn"];
  return ["Sync ohne neue Images", "none"];
}

// The Jira keys a PR's title does not already show, three at most: a
// promotion PR collects every key it ships.
export function jiraPrefix(p: PR): string {
  const title = p.title ?? "";
  const keys = p.jira_keys.filter((k) => !title.includes(k));
  if (keys.length <= 3) return keys.join(" ");
  return `${keys.slice(0, 3).join(" ")} +${keys.length - 3}`;
}

// Newest first. The API's instants are all UTC with a Z, so string order
// is time order.
function newest<T>(rows: T[], at: (r: T) => string | null): T[] {
  return [...rows].sort((a, b) => {
    const x = at(a) ?? "";
    const y = at(b) ?? "";
    return x < y ? 1 : x > y ? -1 : 0;
  });
}

// A wait the metrics would count: a negative one (approved before the
// ready flip, say) is left out there, so it shows as unknown here.
function wait(from: string | null, to: string | null): string {
  const s = between(from, to);
  return dur(s != null && s >= 0 ? s : null);
}

const matches = (q: string, ...fields: (string | null | undefined)[]) =>
  !q || fields.join(" ").toLowerCase().includes(q);

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

type Sorted = { prs: PR[]; deploys: Deploy[]; runs: Run[] };

// Sorted once per dataset; a search keystroke only filters.
function sortAll(ds: Dataset): Sorted {
  return {
    prs: newest(ds.prs, (p) => p.merged_at ?? p.opened_at),
    deploys: newest(ds.deploys, (d) => d.at),
    runs: newest(ds.runs, (r) => r.finished_at ?? r.started_at),
  };
}

function prRows(prs: PR[], f: Filter, q: string): PR[] {
  return prs.filter(
    (p) =>
      inProduct(p.project, f) &&
      (f.bots || !p.bot) &&
      matches(
        q,
        p.repo,
        p.title,
        p.change_type,
        `#${p.pr_id}`,
        ...p.jira_keys,
        PR_STATE[p.state][0],
      ),
  );
}

function deployRows(deploys: Deploy[], f: Filter, q: string): Deploy[] {
  return deploys.filter(
    (d) =>
      inProduct(d.project, f) &&
      // every image it ran, not only the new ones: a search for an image
      // finds its re-syncs and failed syncs too
      matches(
        q,
        d.app,
        d.environment,
        d.revision,
        ...d.images,
        deployKind(d)[0],
      ),
  );
}

function runRows(runs: Run[], f: Filter, q: string): Run[] {
  return runs.filter(
    (r) =>
      inProduct(r.project, f) &&
      matches(
        q,
        r.pipeline_name,
        r.repo,
        r.commit_sha,
        r.status,
        r.build_type,
        ...r.image_refs,
      ),
  );
}

export function Facts({
  ds,
  filter,
  q,
  tab,
  onTab,
}: {
  ds: Dataset;
  filter: Filter;
  q: string;
  tab: Tab;
  onTab: (t: Tab) => void;
}) {
  const needle = q.trim().toLowerCase();
  const sorted = useMemo(() => sortAll(ds), [ds]);
  const prod = ds.meta.production_stage;
  const prs = prRows(sorted.prs, filter, needle);
  const deploys = deployRows(sorted.deploys, filter, needle);
  const runs = runRows(sorted.runs, filter, needle);
  const counts: Record<Tab, number> = {
    prs: prs.length,
    deploys: deploys.length,
    runs: runs.length,
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
          { label: `bis ${ds.meta.production_stage}`, right: true },
          { label: "Status" },
        ]}
        rows={prs.slice(0, LIMIT).map((p) => {
          const [state, tone] = PR_STATE[p.state];
          const prodAt =
            p.environments.find((e) => e.environment === prod)
              ?.first_deployed_at ?? null;
          return [
            <>
              {jiraPrefix(p) && (
                <Mono className="text-accent-strong">{jiraPrefix(p)} </Mono>
              )}
              {p.title ?? <span className="text-muted">#{p.pr_id}</span>}
            </>,
            <Mono className="whitespace-nowrap">{p.repo}</Mono>,
            <span className="text-muted">
              {p.gitops ? "Promotion" : (p.change_type ?? "–")}
              {p.bot ? " · Bot" : ""}
            </span>,
            wait(p.ready_at, p.approved_at),
            wait(p.opened_at, p.merged_at),
            p.merged_at
              ? prodAt
                ? wait(p.merged_at, prodAt)
                : "noch nicht"
              : "–",
            <Pill tone={tone}>{state}</Pill>,
          ];
        })}
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
        rows={deploys.slice(0, LIMIT).map((d) => {
          const [kind, tone] = deployKind(d);
          return [
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
            <Pill tone={tone}>{kind}</Pill>,
          ];
        })}
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
        rows={runs
          .slice(0, LIMIT)
          .map((r) => [
            <Mono className="whitespace-nowrap">{r.pipeline_name}</Mono>,
            <span className="text-muted">#{r.run_id}</span>,
            <span className="text-muted">
              {KIND_LABEL[runKind(r, ds.meta.build_type_from)]}
            </span>,
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
            {needle
              ? `gefiltert nach «${q.trim()}»`
              : "Neueste zuerst · die Suche oben filtert alle Tabellen"}
            {counts[tab] > LIMIT
              ? ` · die ersten ${LIMIT} von ${counts[tab]}`
              : ""}
          </span>
        </div>
        <div className="mt-2.5 overflow-x-auto">
          {counts[tab] === 0 ? (
            <p className="my-3.5 text-[13.5px] text-muted">
              {needle
                ? `Keine Treffer für «${q.trim()}».`
                : "Keine Einträge im Zeitraum."}
            </p>
          ) : (
            table
          )}
        </div>
      </div>
    </section>
  );
}
