// The single facts behind every number, searched by the backend: PRs,
// deploys, runs.

import { Fragment, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type {
  DeployFact,
  Facts as FactsData,
  JiraRef,
  PRFact,
  RunFact,
} from "./api";
import { ciWord, dateTime, dur, shortImage, shortSha, stage } from "./format";
import terms from "./i18n/terms.json";
import { TABLE_HEAD } from "./shared";
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
  unknown: "warn",
};

// A failed sync shows its phase. A first deploy is no problem, only
// counted without a verdict (release or re-sync unknown): neutral, its dot
// hatched.
const DEPLOY_TONE: Record<DeployFact["class"], Tone> = {
  failed: "fail",
  release: "ok",
  first_seen: "none",
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

// Text linking out in a new tab when the backend sends a URL, in the
// surrounding colour; plain text without one.
function OutLink({
  href,
  title,
  children,
}: {
  href: string | null;
  title?: string;
  children: string;
}) {
  return href ? (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      title={title}
      className="text-inherit no-underline hover:underline"
    >
      {children}
    </a>
  ) : (
    children
  );
}

// A Jira key, orange wherever it stands; a link to the ticket when the
// backend sends its URL (jira.base_url in the config).
function JiraKey({ jira }: { jira: JiraRef }) {
  return (
    <Mono className="text-accent-strong">
      <OutLink href={jira.url}>{jira.key}</OutLink>
    </Mono>
  );
}

// A PR's number, a link to it in Bitbucket when the backend sends its URL
// (none for a deleted PR). Beside the title, never around it: the title
// holds Jira links of its own.
function PRNumber({ pr }: { pr: PRFact }) {
  const { t } = useTranslation();
  return (
    <span className="whitespace-nowrap text-muted">
      <OutLink href={pr.url} title={t("facts.openPR")}>
        {`#${pr.pr_id}`}
      </OutLink>
    </span>
  );
}

type Col = { label: string; right?: boolean };

// dim fades a row's content, not its border: stage promotions are listed but
// count nowhere.
function Table({
  cols,
  rows,
  dim,
}: {
  cols: Col[];
  rows: ReactNode[][];
  dim?: boolean[];
}) {
  return (
    <table className="w-full min-w-[760px] border-separate border-spacing-0 text-[13.5px]">
      <thead>
        <tr>
          {cols.map((c, i) => (
            <th
              key={c.label}
              className={`${TABLE_HEAD} ${
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
          <tr
            key={i}
            className="hover:bg-active"
            data-dim={dim?.[i] ? "" : undefined}
          >
            {r.map((cell, j) => (
              <td
                key={j}
                className={`border-b border-border-soft py-[7px] text-ink-dim ${cols[j].right ? "text-right tabular-nums whitespace-nowrap" : ""} ${
                  j < r.length - 1 ? "pr-2.5" : ""
                }`}
              >
                {dim?.[i] ? <span className="opacity-55">{cell}</span> : cell}
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
  const prod = stage(facts.production_stage);
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

  // Rows of the running week are listed, counted in no number: say so
  // where they show.
  const anyRunning = (
    tab === "prs"
      ? facts.prs.rows
      : tab === "deploys"
        ? facts.deploys.rows
        : facts.runs.rows
  ).some((r) => r.running);

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
        // Promotions and the merged PRs no number counts: listed, faded.
        dim={facts.prs.rows.map((p) => p.gitops || p.not_counted != null)}
        rows={facts.prs.rows.map((p) => [
          <>
            {p.jira_prefix.map((j) => (
              <Fragment key={j.key}>
                <JiraKey jira={j} />{" "}
              </Fragment>
            ))}
            {p.jira_more > 0 && (
              <Mono className="text-accent-strong">+{p.jira_more} </Mono>
            )}
            {p.title_parts.length > 0 && (
              <>
                <span>
                  {p.title_parts.map((part, i) =>
                    part.jira ? (
                      <JiraKey key={i} jira={part.jira} />
                    ) : (
                      <Fragment key={i}>{part.text}</Fragment>
                    ),
                  )}
                </span>{" "}
              </>
            )}
            <PRNumber pr={p} />
          </>,
          <Mono className="whitespace-nowrap">{p.repo}</Mono>,
          <span className="text-muted">{prType(p)}</span>,
          dur(p.review_s),
          dur(p.cycle_s),
          p.merged_at
            ? p.prod_at
              ? dur(p.to_prod_s)
              : t("facts.notOnProd")
            : "–",
          <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-0.5">
            <Pill tone={PR_STATE_TONE[p.state]}>
              {t(`facts.prState.${p.state}`)}
            </Pill>
            {p.not_counted && (
              <Pill tone="none">{t(`facts.notCounted.${p.not_counted}`)}</Pill>
            )}
            {p.running && <Pill tone="none">{t("facts.running")}</Pill>}
            {p.state === "unknown" && p.idle_s != null && (
              <span className="text-[12.5px] whitespace-nowrap text-muted">
                {t("facts.idleFor", { idle: dur(p.idle_s) })}
              </span>
            )}
            {p.waiting && (
              <span className="text-[12.5px] whitespace-nowrap text-muted">
                {t(`facts.prWaiting.${p.waiting}`)}
                {p.waiting === "inactive"
                  ? ` · ${t("facts.idleFor", { idle: dur(p.idle_s) })}`
                  : p.age_s != null &&
                    ` · ${t("facts.openFor", { age: dur(p.age_s) })}`}
              </span>
            )}
          </span>,
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
        // An infrastructure App's deploys count nowhere: listed, faded.
        dim={facts.deploys.rows.map((d) => d.infra)}
        rows={facts.deploys.rows.map((d) => [
          <span className="whitespace-nowrap tabular-nums">
            {dateTime(d.at)}
          </span>,
          <Mono className="whitespace-nowrap">{d.app}</Mono>,
          <span className="text-muted">
            {d.environment ? stage(d.environment) : "–"}
          </span>,
          <Mono className="text-muted">{shortSha(d.revision)}</Mono>,
          <Mono>
            {d.new_images.length
              ? d.new_images.map(shortImage).join(", ")
              : "–"}
          </Mono>,
          <span className="inline-flex flex-wrap gap-1">
            <Pill
              tone={DEPLOY_TONE[d.class]}
              dot={d.class === "first_seen" ? "texture-unknown" : undefined}
            >
              {d.class === "failed"
                ? d.phase
                : t(`facts.deployClass.${d.class}`)}
            </Pill>
            {/* An App's first deploy counted as a release: still its first
                known deploy, found by that word too. */}
            {d.first_deploy && d.class === "release" && (
              <Pill tone="none">{t("facts.deployClass.first_seen")}</Pill>
            )}
            {d.infra && <Pill tone="none">{t("facts.infra")}</Pill>}
            {d.running && <Pill tone="none">{t("facts.running")}</Pill>}
          </span>,
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
        dim={facts.runs.rows.map((r) => r.gitops)}
        rows={facts.runs.rows.map((r) => [
          <Mono className="whitespace-nowrap">{r.pipeline_name}</Mono>,
          <span className="text-muted">#{r.run_id}</span>,
          <span className="text-muted">{t(`build.kind.${r.kind}`)}</span>,
          <Mono className="text-muted">{shortSha(r.shown_sha)}</Mono>,
          dur(r.duration_s),
          <Mono>
            {r.image_refs.length
              ? r.image_refs.map(shortImage).join(", ")
              : "–"}
          </Mono>,
          r.status ? (
            <span className="inline-flex gap-1">
              <Pill tone={RUN_TONE[r.verdict]}>
                {r.outcome
                  ? t(`facts.runOutcome.${r.outcome}`)
                  : ciWord(r.status)}
              </Pill>
              {/* red, then green on the same commit: red time, as in
                  the CI flow */}
              {r.flaky && <Pill tone="fail">{t("facts.flaky")}</Pill>}
              {r.running && <Pill tone="none">{t("facts.running")}</Pill>}
            </span>
          ) : r.running ? (
            <Pill tone="none">{t("facts.running")}</Pill>
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
          {q
            ? t("facts.filtered", { q })
            : tab === "prs" && facts.prs.rows[0]?.waiting
              ? t("facts.openFirst")
              : t("facts.newest")}
          {counts[tab] > facts.limit
            ? t("facts.firstOf", { limit: facts.limit, count: counts[tab] })
            : ""}
          {anyRunning ? t("facts.runningNote") : ""}
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
