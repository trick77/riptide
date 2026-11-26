// The page's head: the assessment of the closed weeks (what the numbers
// raise, worded by an LLM where a model is set), then the DORA numbers
// Riptide measures, each against the previous period.

import { useTranslation } from "react-i18next";
import type { Metrics, Query } from "./api";
import { Assessment } from "./Assessment";
import { dur, durParts, fullDate, num, pct, stage } from "./format";
import type { Tab } from "./state";
import { Strip } from "./strip";
import { BatchTile, MergedTile } from "./Throughput";
import {
  Direction,
  SectionHead,
  SubHead,
  Tile,
  type Why,
  WhyPanel,
} from "./ui";

type Props = {
  m: Metrics;
  query: Query;
  why: string | null;
  onWhy: (key: string) => void;
  onFacts: (tab: Tab, q: string) => void;
};

export function Overview({ m, query, why, onWhy, onFacts }: Props) {
  const { t } = useTranslation();
  const prod = stage(m.delivery.production_stage);
  const freq = m.delivery.frequency;
  const lt = m.delivery.lead_time;
  const c = m.comparison;
  const sinceNote = freq.since
    ? t("overview.sinceNote", { date: fullDate(new Date(freq.since)) }) +
      (freq.low_sample ? t("overview.sinceLow") : "")
    : null;
  const lead = durParts(lt.human.p50);
  // Team-wide counts the backend sends in every view. A product view counts
  // no deploy of an App without a known product: when it shows none, they
  // may be why, and the backend says so (maybe_unattributed). Infrastructure
  // Apps count nowhere; the team view names how many.
  const unattributed = freq.maybe_unattributed
    ? t("overview.freq.unattributed", { count: freq.unattributed })
    : null;
  const infra =
    !m.product && freq.infra > 0 ? (
      <span title={freq.infra_apps.join(", ")}>
        {t("overview.freq.infra", { count: freq.infra })}
      </span>
    ) : null;
  const freqNote = (
    freq.syncs
      ? [t("overview.freq.note", { count: freq.releases }), unattributed, infra]
      : freq.argo_reporting
        ? [unattributed ?? t("overview.freq.noDeploys"), infra]
        : [t("overview.freq.notReporting")]
  )
    .filter((x) => x != null)
    .map((x, i) => (
      <span key={i}>
        {i > 0 && " · "}
        {x}
      </span>
    ));

  const whys: Record<string, Why> = {
    freq: {
      title: t("overview.freq.label"),
      means: t("overview.freq.means"),
      watch: t("overview.freq.watch"),
      lever: t("overview.freq.lever"),
      def: t("overview.freq.def", { prod }),
      basis:
        t("overview.freq.basis", {
          releases: t("common.releases", { count: freq.releases }),
          resyncs: t("common.resyncs", { count: freq.resyncs }),
          firstSeen: t("common.firstDeploys", { count: freq.first_seen }),
          syncs: t("common.revisions", { count: freq.syncs }),
        }) +
        (freq.first_releases > 0
          ? t("overview.freq.basisFirst", { count: freq.first_releases })
          : ""),
      filters: [
        "operation_phase = Succeeded",
        `environment = ${prod}`,
        t("overview.freq.filterImages"),
        t("overview.freq.filterRevision"),
      ],
      coverage:
        (sinceNote ? `${sinceNote} ` : "") +
        (freq.first_seen > 0
          ? t("overview.freq.coverageFirst", { days: freq.grace_days })
          : t("overview.freq.coverageAll", { days: freq.grace_days })) +
        (freq.infra > 0
          ? t("overview.freq.coverageInfra", {
              count: freq.infra,
              apps: freq.infra_apps.join(", "),
            })
          : "") +
        (freq.unattributed > 0
          ? t("overview.freq.coverageUnattributed", {
              count: freq.unattributed,
            })
          : ""),
      query: "docs/metrics.md · Deployment-frequency query",
    },
    lead: {
      title: t("overview.lead.label", { prod }),
      means: t("overview.lead.means"),
      watch: t("overview.lead.watch"),
      lever: t("overview.lead.lever"),
      def: t("overview.lead.def", { prod }),
      basis: t("overview.lead.basis", { count: lt.human.n, prod }),
      filters: [t("overview.lead.filterBots"), `environment = ${prod}`],
      coverage:
        t("overview.lead.coverage", {
          prod,
          traced: m.coverage.on_prod,
          count: m.coverage.merged,
        }) +
        (m.coverage.not_yet > 0
          ? " " + t("overview.lead.notYet", { count: m.coverage.not_yet, prod })
          : ""),
      query: "docs/dora-lead-time.md",
    },
    dsucc: {
      title: t("overview.dsucc.label"),
      means: t("overview.dsucc.means"),
      watch: t("overview.dsucc.watch"),
      lever: t("overview.dsucc.lever"),
      def: t("overview.dsucc.def", { prod }),
      basis: t("overview.dsucc.basis", {
        finished: freq.finished,
        failed: freq.failed,
      }),
      filters: [
        "operation_phase IN (Succeeded, Failed, Error)",
        `environment = ${prod}`,
      ],
      coverage: t("overview.dsucc.coverage"),
      query: "argocd_events",
    },
  };
  const open = why && whys[why] ? whys[why] : null;

  return (
    <section id="ueberblick" aria-labelledby="h-ueberblick" className="mt-9">
      <SectionHead
        id="h-ueberblick"
        eyebrow={t("overview.eyebrow")}
        title={t("overview.title")}
        lead={t("overview.intro", { weeks: query.weeks })}
      />
      <Assessment query={query} m={m} onFacts={onFacts} />
      <Strip m={m} />

      <SubHead title={t("overview.throughput")} />
      <div className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
        <Tile
          label={t("overview.freq.label")}
          dora={t("overview.freq.dora")}
          question={t("overview.freq.question", { prod })}
          value={freq.syncs ? num(freq.per_week, 1) : "–"}
          unit={freq.syncs ? t("overview.freq.unit") : ""}
          note={freqNote}
          direction={
            <Direction
              arrow="up"
              label={t("overview.freq.direction")}
              why={t("overview.freq.directionWhy")}
            />
          }
          warn={
            sinceNote ??
            (freq.first_seen > 0
              ? t("overview.freq.firstSeen", {
                  count: freq.first_seen,
                  days: freq.grace_days,
                })
              : undefined)
          }
          compare={{
            change: c.deploy_frequency,
            format: (v) => num(v, 1),
          }}
          open={why === "freq"}
          onWhy={() => onWhy("freq")}
        />
        <Tile
          label={t("overview.lead.label", { prod })}
          dora={t("overview.lead.dora")}
          question={t("overview.lead.question", { prod })}
          value={lead.value}
          unit={
            lt.human.p50 == null ? "" : t("common.typical", { unit: lead.unit })
          }
          note={
            lt.human.n === 0
              ? t("overview.lead.noHuman", { prod })
              : t("overview.lead.slowest", {
                  p90: dur(lt.human.p90),
                  count: lt.human.n,
                })
          }
          direction={
            <Direction
              arrow="down"
              label={t("common.shorterBetter")}
              why={t("overview.lead.directionWhy")}
            />
          }
          warn={lt.low_sample ? t("overview.lead.lowSample") : undefined}
          compare={{
            change: c.lead_time,
            format: dur,
          }}
          open={why === "lead"}
          onWhy={() => onWhy("lead")}
        />
        <Tile
          label={t("overview.dsucc.label")}
          dora={t("overview.dsucc.dora")}
          question={t("overview.dsucc.question")}
          value={pct(freq.success_rate)}
          unit={freq.finished ? t("overview.dsucc.unit") : ""}
          note={t("overview.dsucc.note", {
            failed: freq.failed,
            count: freq.finished,
          })}
          direction={
            <Direction
              arrow="up"
              label={t("overview.dsucc.direction")}
              why={t("common.ownHistory")}
            />
          }
          compare={{
            change: c.deploy_success,
            format: pct,
          }}
          open={why === "dsucc"}
          onWhy={() => onWhy("dsucc")}
        />
        <div className="flex flex-col gap-1 rounded-ui border border-dashed border-border px-3.5 py-3">
          <span className="text-[11px] font-medium tracking-[.12em] text-muted uppercase">
            {t("overview.instability.label")}
          </span>
          <div className="-mt-1 text-[11px] text-faint">
            {t("overview.instability.dora")}
          </div>
          <div className="text-[13px] text-ink-dim">
            {t("overview.instability.question")}
          </div>
          <div className="text-[22px] font-medium text-faint">
            {t("overview.instability.value")}
          </div>
          <div className="text-xs text-muted">
            {t("overview.instability.text")}
          </div>
        </div>
      </div>
      {open && <WhyPanel why={open} />}

      <SubHead title={t("throughput.head")} text={t("throughput.scope")} />
      <div className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
        <MergedTile m={m} />
        <BatchTile m={m} />
      </div>
    </section>
  );
}
