// The single facts behind every number, searched by the backend: PRs,
// deploys, runs.

import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { DeployFact, Facts as FactsData, PRFact, RunFact } from "./api";
import { ciWord, dateTime, dur, shortImage, shortSha } from "./format";
import terms from "./i18n/terms.json";
import type { Tab } from "./state";
import { Mono, Pill, SectionHead, type Tone } from "./ui";

// The words of states, classes, types and run outcomes are in
// i18n/terms.json: the backend searches them too, in both languages (the
// *Term maps in backend/internal/metrics/facts.go); a Go test fails when
// they differ.
const PR_STATE_TONE: Record<PRFact["state"], Tone> = {
  merged: "ok",
  open: "none",
  declined: "none",
  deleted: "none",
};

// A failed sync shows its phase.
const DEPLOY_TONE: Record<DeployFact["class"], Tone> = {
  failed: "fail",
  release: "ok",
  first_seen: "warn",
  sync: "none",
};

// A PR's change type, from its branch prefix (parse.ChangeType); a prefix
// terms.json does not know yet shows as the backend sent it.
type ChangeType = keyof typeof terms.en.changeType;
const isChangeType = (s: string): s is ChangeType =>
  Object.hasOwn(terms.en.changeType, s);

// By the backend's verdict, whichever CI's words the status is in; the
// word is the backend's outcome in the viewer's language, or the CI's own.
const RUN_TONE: Record<RunFact["verdict"], Tone> = {
  success: "ok",
  failure: "fail",
  none: "none",
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

// The data section: the search, which filters only these tables, then the
// tables once the facts are in.
export function Facts({
  facts,
  failed,
  q,
  input,
  onInput,
  tab,
  onTab,
}: {
  facts: FactsData | null;
  // The facts did not load; the page's alert says why.
  failed: boolean;
  // q is the search the facts were asked for; input what the box holds.
  q: string;
  input: string;
  onInput: (v: string) => void;
  tab: Tab;
  onTab: (t: Tab) => void;
}) {
  const { t } = useTranslation();
  return (
    <section id="daten" aria-labelledby="h-daten" className="mt-10">
      <SectionHead
        id="h-daten"
        eyebrow={t("facts.eyebrow")}
        title={t("facts.title")}
        lead={t("facts.lead")}
      />
      <div className="no-print mb-3 max-w-[480px]">
        <label htmlFor="search" className="sr-only">
          {t("facts.searchLabel")}
        </label>
        <input
          id="search"
          type="search"
          value={input}
          onChange={(e) => onInput(e.target.value)}
          placeholder={t("facts.placeholder")}
          className="box-border h-9 w-full rounded-ui-sm border border-border bg-bg px-3 text-[13.5px] text-ink focus-visible:outline-2 focus-visible:outline-accent-strong"
        />
      </div>
      {facts ? (
        <FactsTables facts={facts} q={q} tab={tab} onTab={onTab} />
      ) : failed ? (
        <p className="m-0 text-[13.5px] text-muted">{t("facts.failed")}</p>
      ) : (
        <div aria-busy="true" className="skeleton h-40 rounded-ui-lg" />
      )}
    </section>
  );
}

function FactsTables({
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
  const { t } = useTranslation();
  const prod = facts.production_stage;
  const counts: Record<Tab, number> = {
    prs: facts.prs.count,
    deploys: facts.deploys.count,
    runs: facts.runs.count,
  };
  const tabs: [Tab, string][] = [
    ["prs", t("facts.tabPRs")],
    ["deploys", t("facts.tabDeploys")],
    ["runs", t("facts.tabRuns")],
  ];
  const prType = (p: PRFact) =>
    p.gitops
      ? t("facts.promotion")
      : p.change_type
        ? isChangeType(p.change_type)
          ? t(`facts.changeType.${p.change_type}`)
          : p.change_type
        : "–";

  let table: ReactNode;
  if (tab === "prs") {
    table = (
      <Table
        cols={[
          { label: t("facts.colPR") },
          { label: t("facts.colRepo") },
          { label: t("facts.colType") },
          { label: t("facts.colReview"), right: true },
          { label: t("facts.colCycle"), right: true },
          { label: t("facts.colToProd", { prod }), right: true },
          { label: t("facts.colStatus") },
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
            {prType(p)}
            {p.bot ? t("facts.bot") : ""}
          </span>,
          dur(p.review_s),
          dur(p.cycle_s),
          p.merged_at
            ? p.prod_at
              ? dur(p.to_prod_s)
              : t("facts.notOnProd")
            : "–",
          <Pill tone={PR_STATE_TONE[p.state]}>
            {t(`facts.prState.${p.state}`)}
          </Pill>,
        ])}
      />
    );
  } else if (tab === "deploys") {
    table = (
      <Table
        cols={[
          { label: t("facts.colAt") },
          { label: t("facts.colApp") },
          { label: t("facts.colStage") },
          { label: t("facts.colRevision") },
          { label: t("facts.colNewImages") },
          { label: t("facts.colCountsAs") },
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
          <Pill tone={DEPLOY_TONE[d.class]}>
            {d.class === "failed" ? d.phase : t(`facts.deployClass.${d.class}`)}
          </Pill>,
        ])}
      />
    );
  } else {
    table = (
      <Table
        cols={[
          { label: t("facts.colPipeline") },
          { label: t("facts.colRun"), right: true },
          { label: t("facts.colKind") },
          { label: t("facts.colCommit") },
          { label: t("facts.colDuration"), right: true },
          { label: t("facts.colImages") },
          { label: t("facts.colStatus") },
        ]}
        rows={facts.runs.rows.map((r) => [
          <Mono className="whitespace-nowrap">{r.pipeline_name}</Mono>,
          <span className="text-muted">#{r.run_id}</span>,
          <span className="text-muted">{t(`build.kind.${r.kind}`)}</span>,
          <Mono className="text-muted">{shortSha(r.commit_sha)}</Mono>,
          dur(r.duration_s),
          <Mono>
            {r.image_refs.length
              ? r.image_refs.map(shortImage).join(", ")
              : "–"}
          </Mono>,
          r.status ? (
            <Pill tone={RUN_TONE[r.verdict]}>
              {r.outcome
                ? t(`facts.runOutcome.${r.outcome}`)
                : ciWord(r.status)}
            </Pill>
          ) : (
            "–"
          ),
        ])}
      />
    );
  }

  return (
    <div className="rounded-ui-lg border border-border bg-panel px-[18px] pt-3.5 pb-2">
      <div className="flex flex-wrap items-center justify-between gap-2.5">
        <div
          role="tablist"
          aria-label={t("facts.tabs")}
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
                tab === key ? "bg-active text-ink" : "bg-transparent text-muted"
              }`}
            >
              {label}{" "}
              <span className="text-muted tabular-nums">{counts[key]}</span>
            </button>
          ))}
        </div>
        <span className="text-[12.5px] text-muted">
          {q ? t("facts.filtered", { q }) : t("facts.newest")}
          {counts[tab] > facts.limit
            ? t("facts.firstOf", { limit: facts.limit, count: counts[tab] })
            : ""}
        </span>
      </div>
      <div className="mt-2.5 overflow-x-auto">
        {counts[tab] === 0 ? (
          <p className="my-3.5 text-[13.5px] text-muted">
            {q ? t("facts.noHits", { q }) : t("facts.empty")}
          </p>
        ) : (
          table
        )}
      </div>
    </div>
  );
}
