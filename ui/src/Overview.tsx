// The page's head: what the numbers ask the team to look at, then the
// DORA numbers Riptide measures, each against the previous period.

import type { TFunction } from "i18next";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { Finding, Metrics } from "./api";
import { stageNames } from "./Flow";
import { dur, durParts, fullDate, num, pct, stage } from "./format";
import type { Tab } from "./state";
import { Strip } from "./strip";
import {
  Compare,
  Direction,
  Pill,
  SectionHead,
  SubHead,
  Tile,
  type Tone,
  type Why,
  WhyPanel,
} from "./ui";

type Props = {
  m: Metrics;
  why: string | null;
  onWhy: (key: string) => void;
  onFacts: (tab: Tab, q: string) => void;
};

const TONE: Record<Finding["tone"], Tone> = {
  attention: "warn",
  hint: "none",
  talk: "none",
};

// A finding's words: the headline, what it rests on, what to do, the rule
// that fired, and where its facts are.
type Words = {
  area: string;
  title: ReactNode;
  body: ReactNode;
  lever: [string, ReactNode];
  rule: ReactNode;
  link: [string, () => void];
};

function words(
  t: TFunction,
  f: Finding,
  m: Metrics,
  onFacts: Props["onFacts"],
  toFlow: () => void,
): Words | null {
  const prod = stage(m.delivery.production_stage);
  const h = m.build.hotspots;
  if (f.kind === "stage") {
    const s = m.lead_chain.stages.find((x) => x.key === f.stage);
    if (!s) return null;
    const [name, desc] = stageNames(t, m.lead_chain, prod)[s.key];
    const lc = m.lead_chain;
    return {
      area: t("overview.area.flow"),
      title:
        s.key === "to_prod"
          ? t("overview.stageTitleProd", { share: pct(s.share), prod })
          : t("overview.stageTitle", { share: pct(s.share), prod, name }),
      body:
        s.key === "to_prod"
          ? t("overview.stageBodyProd", {
              prod,
              before: dur(lc.before_prod),
              wait: dur(s.p50),
            })
          : t("overview.stageBody", {
              desc,
              p50: dur(s.p50),
              p90: dur(s.p90),
            }),
      lever: [t("common.lever"), t(`overview.stageLever.${s.key}`)],
      rule:
        f.tone === "attention"
          ? t("overview.ruleAttention", {
              share: pct(lc.attention_share),
              min: lc.min_prs,
            })
          : s.low_sample
            ? t("overview.ruleLowSample", { min: lc.min_prs })
            : t("overview.ruleHint", {
                share: pct(lc.attention_share),
                min: lc.min_prs,
              }),
      link: [t("overview.toFlow"), toFlow],
    };
  }
  if (f.kind === "first_touch") {
    const r = m.review;
    return {
      area: t("overview.area.review"),
      title: t("overview.firstTouchTitle", { rate: pct(r.first_touch_rate) }),
      body: t("overview.firstTouchBody"),
      lever: [t("common.teamQuestion"), t("overview.firstTouchQuestion")],
      rule: t("overview.firstTouchRule", {
        n: r.approved_on_first_touch,
        total: r.approved,
      }),
      link: [t("overview.toReview"), toFlow],
    };
  }
  if (f.kind === "open_prs") {
    const o = m.open_prs;
    // The backend sorts the repos: jammed first, the most long-open PRs
    // first among them.
    const top = o.repos[0];
    if (!top?.jammed) return null;
    return {
      area: t("overview.area.flow"),
      title: t("overview.openTitle", { count: o.jammed }),
      body: t("overview.openBody", {
        repo: top.repo,
        n: top.long,
        days: o.long_days,
        p50: dur(top.age.p50),
        first: o.jammed_waiting.first_review,
        jammed: o.jammed_long,
      }),
      lever: [t("common.lever"), t("overview.openLever")],
      rule:
        f.tone === "attention"
          ? t("overview.openRuleAttention", {
              min: o.jam_min,
              days: o.long_days,
              prev: num(m.comparison.open_jammed.prev ?? 0),
            })
          : t("overview.openRuleHint", {
              min: o.jam_min,
              days: o.long_days,
            }),
      link: [
        t("overview.openPRsOf", { repo: top.repo }),
        () => onFacts("prs", top.repo),
      ],
    };
  }
  const k = h.kinds.find((x) => x.kind === f.run_kind);
  const repo = k?.repos.find((x) => x.repo === f.repo);
  if (!k || !repo) return null;
  const kind = t(`build.kind.${k.kind}`);
  const link: [string, () => void] = [
    t("overview.runsOf", { repo: repo.repo }),
    () => onFacts("runs", repo.repo),
  ];
  if (f.kind === "flaky")
    return {
      area: t("overview.area.build"),
      title: t("overview.flakyTitle", {
        repo: repo.repo,
        rate: pct(repo.failure_rate),
        kind,
      }),
      body: t("overview.flakyBody", {
        failures: repo.failures,
        runs: repo.runs,
        prevRate: pct(repo.failure_change.prev),
        hours: num(repo.wasted_seconds / 3600, 1),
      }),
      lever: [t("common.lever"), t("overview.flakyLever")],
      rule: t("overview.flakyRule", {
        factor: num(h.flaky_factor, 1),
        points: num(h.flaky_min_points * 100, 0),
        failures: h.flaky_min_failures,
        min: h.min_runs,
      }),
      link,
    };
  return {
    area: t("overview.area.build"),
    title: t("overview.slowTitle", {
      repo: repo.repo,
      factor: num(repo.factor ?? 0, 1),
    }),
    body: t("overview.slowBody", {
      kind,
      p50: dur(repo.duration.p50),
      prevP50: dur(repo.duration_change.prev),
    }),
    lever: [t("common.lever"), t("overview.slowLever")],
    rule: t("overview.slowRule", {
      factor: num(h.slow_factor, 1),
      min: h.min_runs,
    }),
    link,
  };
}

function Findings({ m, onFacts }: Pick<Props, "m" | "onFacts">) {
  const { t } = useTranslation();
  const toFlow = () => document.getElementById("fluss")?.scrollIntoView?.();
  const items = m.findings
    .map((f) => [f, words(t, f, m, onFacts, toFlow)] as const)
    .filter((x): x is [Finding, Words] => x[1] != null);
  if (!items.length)
    return <p className="m-0 text-[13.5px] text-muted">{t("overview.none")}</p>;
  return (
    <ul className="m-0 grid list-none grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-3 p-0">
      {items.map(([f, w], i) => (
        <li
          key={i}
          className={`flex min-w-0 flex-col gap-2 rounded-ui-lg border border-l-[3px] border-border bg-panel px-4 py-3.5 ${
            f.tone === "attention"
              ? "border-l-ochre"
              : "border-l-elevated-border"
          }`}
        >
          <span className="self-start">
            <Pill tone={TONE[f.tone]}>
              {t(`overview.tone.${f.tone}`)} · {w.area}
            </Pill>
          </span>
          <h4 className="m-0 text-[15px] leading-snug font-medium text-ink">
            {w.title}
          </h4>
          <p className="m-0 text-[13px] text-ink-dim">{w.body}</p>
          <p className="m-0 text-[13px] text-ink-dim">
            <span className="font-medium text-ink">{w.lever[0]}:</span>{" "}
            {w.lever[1]}
          </p>
          <p className="m-0 text-[11.5px] text-muted">{w.rule}</p>
          <button
            type="button"
            onClick={w.link[1]}
            className="no-print mt-auto self-start border-0 bg-transparent p-0 text-left text-[13px] text-accent-strong hover:underline"
          >
            {w.link[0]}
          </button>
        </li>
      ))}
    </ul>
  );
}

export function Overview({ m, why, onWhy, onFacts }: Props) {
  const { t } = useTranslation();
  const bots = m.bots;
  const prod = stage(m.delivery.production_stage);
  const freq = m.delivery.frequency;
  const lt = m.delivery.lead_time;
  const c = m.comparison;
  const sinceNote = freq.since
    ? t("overview.sinceNote", { date: fullDate(new Date(freq.since)) }) +
      (freq.low_sample ? t("overview.sinceLow") : "")
    : null;
  const lead = durParts(lt.human.p50);

  const whys: Record<string, Why> = {
    freq: {
      title: t("overview.freq.label"),
      means: t("overview.freq.means"),
      watch: t("overview.freq.watch"),
      lever: t("overview.freq.lever"),
      def: t("overview.freq.def", { prod }),
      basis: t("overview.freq.basis", {
        releases: t("common.releases", { count: freq.releases }),
        resyncs: t("common.resyncs", { count: freq.resyncs }),
        firstSeen: t("common.firstDeploys", { count: freq.first_seen }),
        syncs: t("common.revisions", { count: freq.syncs }),
      }),
      filters: [
        "operation_phase = Succeeded",
        `environment = ${prod}`,
        t("overview.freq.filterImages"),
        t("overview.freq.filterRevision"),
      ],
      coverage:
        (sinceNote ? `${sinceNote} ` : "") +
        (freq.first_seen > 0
          ? t("overview.freq.coverageFirst")
          : t("overview.freq.coverageAll")),
      query: "docs/metrics.md · Deployment-frequency query",
    },
    lead: {
      title: t("overview.lead.label", { prod }),
      means: t("overview.lead.means"),
      watch: t("overview.lead.watch"),
      lever: t("overview.lead.lever"),
      def: t("overview.lead.def", { prod }),
      basis: t("overview.lead.basis", {
        count: lt.human.n,
        bots: lt.bot.n ? t("overview.lead.basisBots", { n: lt.bot.n }) : "",
      }),
      filters: [
        "NOT is_merge",
        "NOT service account",
        t("overview.lead.filterBots"),
        `environment = ${prod}`,
      ],
      coverage: t("overview.lead.coverage"),
      query: "lead_time_changes · docs/dora-lead-time.md",
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
        "operation_phase IN (Succeeded, Failed)",
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
        lead={t("overview.intro")}
      />
      <Findings m={m} onFacts={onFacts} />
      <Strip m={m} />

      <SubHead
        title={t("overview.throughput")}
        text={t("overview.throughputText")}
      />
      <div className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
        <Tile
          label={t("overview.freq.label")}
          dora={t("overview.freq.dora")}
          question={t("overview.freq.question", { prod })}
          value={freq.syncs ? num(freq.per_week, 1) : "–"}
          unit={freq.syncs ? t("overview.freq.unit") : ""}
          note={
            freq.syncs
              ? t("overview.freq.note", { count: freq.releases })
              : freq.argo_reporting
                ? t("overview.freq.noDeploys")
                : t("overview.freq.notReporting")
          }
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
              ? t("overview.freq.firstSeen", { count: freq.first_seen })
              : undefined)
          }
          compare={
            <Compare
              change={c.deploy_frequency}
              now={freq.syncs ? num(freq.per_week, 1) : "–"}
              format={(v) => num(v, 1)}
            />
          }
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
            (lt.human.n === 0
              ? t("overview.lead.noHuman")
              : t("overview.lead.slowest", {
                  p90: dur(lt.human.p90),
                  count: lt.human.n,
                })) +
            (bots && lt.bot.n > 0
              ? t("overview.lead.bots", { p50: dur(lt.bot.p50), n: lt.bot.n })
              : "")
          }
          direction={
            <Direction
              arrow="down"
              label={t("common.shorterBetter")}
              why={t("overview.lead.directionWhy")}
            />
          }
          warn={lt.low_sample ? t("overview.lead.lowSample") : undefined}
          compare={
            <Compare
              change={c.lead_time}
              now={dur(lt.human.p50)}
              format={dur}
            />
          }
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
          compare={
            <Compare
              change={c.deploy_success}
              now={pct(freq.success_rate)}
              format={pct}
            />
          }
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
    </section>
  );
}
