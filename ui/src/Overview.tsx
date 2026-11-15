// The page's head: what the numbers ask the team to look at, then the
// DORA numbers Riptide measures, each against the previous period.

import type { ReactNode } from "react";
import type { Finding, Metrics } from "./api";
import { KIND_LABEL } from "./Build";
import { stageNames } from "./Flow";
import { dur, durParts, fullDate, num, pct } from "./format";
import type { Tab } from "./state";
import {
  Compare,
  Direction,
  Pill,
  SectionHead,
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

const TONE: Record<Finding["tone"], [string, Tone]> = {
  attention: ["Achtung", "warn"],
  hint: ["Hinweis", "none"],
  talk: ["Gesprächsanlass", "none"],
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

const STAGE_LEVER: Record<string, string> = {
  coding:
    "kleinere Schritte, früher einen PR öffnen, damit Review und Deploy früher beginnen",
  review: "Review-Zeiten im Team vereinbaren, PRs kleiner schneiden",
  merge: "nach dem Approval gleich mergen; Merge-Checks und Freigaben prüfen",
  to_pre: "nach dem Merge automatisch auf die Vor-Umgebung deployen",
  to_prod:
    "kleinere, häufigere Releases; prüfen, ob prod an einen festen Termin gebunden ist",
};

function words(
  f: Finding,
  m: Metrics,
  onFacts: Props["onFacts"],
  toFlow: () => void,
): Words | null {
  const prod = m.delivery.production_stage;
  const h = m.build.hotspots;
  if (f.kind === "stage") {
    const s = m.lead_chain.stages.find((x) => x.key === f.stage);
    if (!s) return null;
    const [name, desc] = stageNames(m.lead_chain, prod)[s.key];
    return {
      area: "Fluss",
      title:
        s.key === "to_prod"
          ? `${pct(s.share)} der Zeit bis ${prod} wartet fertiger Code auf das Deploy nach ${prod}.`
          : `${pct(s.share)} der Zeit bis ${prod} liegt in der Etappe «${name}».`,
      body:
        s.key === "to_prod"
          ? `Bis vor ${prod} braucht eine Änderung typischerweise ${dur(m.lead_chain.before_prod)}, danach wartet sie typischerweise ${dur(s.p50)}.`
          : `${desc}: typischerweise ${dur(s.p50)}, die langsamsten 10 % über ${dur(s.p90)}.`,
      lever: ["Hebel", STAGE_LEVER[s.key]],
      rule:
        f.tone === "attention"
          ? `Regel: Etappe mit dem längsten Median, ab ${pct(m.lead_chain.attention_share)} der Zeit und ${m.lead_chain.min_prs} PRs (Startwert)`
          : s.low_sample
            ? `Regel: Etappe mit dem längsten Median. Unter ${m.lead_chain.min_prs} PRs zu wenig belastbar für «Achtung», darum nur ein Hinweis`
            : `Regel: Etappe mit dem längsten Median. Eine gibt es immer, darum nur ein Hinweis; «Achtung» ab ${pct(m.lead_chain.attention_share)} der Zeit und ${m.lead_chain.min_prs} PRs`,
      link: ["Zum Fluss →", toFlow],
    };
  }
  if (f.kind === "first_touch") {
    const r = m.review;
    return {
      area: "Review",
      title: `${pct(r.first_touch_rate)} der Approvals erfolgen ohne vorherigen Kommentar.`,
      body: "Der Wert ist weder gut noch schlecht: Eingespielte Pairs approven ohne Kommentar, wer durchwinkt, ebenfalls.",
      lever: [
        "Frage fürs Team",
        "Besprechen wir grosse Änderungen ausserhalb von Bitbucket?",
      ],
      rule: `Steht immer da: kein Grenzwert, keine Richtung · ${r.approved_on_first_touch} von ${r.approved} Approvals`,
      link: ["Zum Review →", toFlow],
    };
  }
  const k = h.kinds.find((x) => x.kind === f.run_kind);
  const repo = k?.repos.find((x) => x.repo === f.repo);
  if (!k || !repo) return null;
  const kind = KIND_LABEL[k.kind];
  const link: [string, () => void] = [
    `Läufe von ${repo.repo} ansehen →`,
    () => onFacts("runs", repo.repo),
  ];
  if (f.kind === "flaky")
    return {
      area: "Build",
      title: `${repo.repo}: ${pct(repo.failure_rate)} der ${kind} scheitern.`,
      body: `${repo.failures} von ${repo.runs} Läufen rot, im Team ${pct(k.failure_rate)}. Das kostet ${num(repo.wasted_seconds / 3600, 1)} h CI-Zeit und jedes Mal einen Neustart.`,
      lever: [
        "Hebel",
        "instabile Tests finden und isolieren, statt Läufe einfach neu zu starten",
      ],
      rule: `Regel: auffällig ab ${num(h.flaky_factor, 1)}× der Team-Quote, höchstens halbwegs bis 100 %, ab ${h.min_runs} Läufen (Startwert)`,
      link,
    };
  return {
    area: "Build",
    title: `${repo.repo} liefert Feedback ${num(repo.factor ?? 0, 1)}× langsamer als das Team.`,
    body: `${kind} dauern typischerweise ${dur(repo.duration.p50)}, im Team ${dur(k.duration.p50)}. So lange wartet, wer pusht.`,
    lever: [
      "Hebel",
      "Build-Cache nutzen, Tests parallelisieren, langsame Integrationstests aus dem PR-Build nehmen",
    ],
    rule: `Regel: Median ab ${num(h.slow_factor, 1)}× Team, ab ${h.min_runs} Läufen (Startwert)`,
    link,
  };
}

function Findings({ m, onFacts }: Pick<Props, "m" | "onFacts">) {
  const toFlow = () => document.getElementById("fluss")?.scrollIntoView?.();
  const items = m.findings
    .map((f) => [f, words(f, m, onFacts, toFlow)] as const)
    .filter((x): x is [Finding, Words] => x[1] != null);
  if (!items.length)
    return (
      <p className="m-0 text-[13.5px] text-muted">
        Keine Auffälligkeiten nach den Regeln, die Riptide anwendet.
      </p>
    );
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
            <Pill tone={TONE[f.tone][1]}>
              {TONE[f.tone][0]} · {w.area}
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
  const bots = m.bots;
  const prod = m.delivery.production_stage;
  const freq = m.delivery.frequency;
  const lt = m.delivery.lead_time;
  const c = m.comparison;
  const sinceNote = freq.since
    ? `Argo CD meldet erst seit ${fullDate(new Date(freq.since))}: die Wochen davor sind leer, die Rate gilt ab dann.${
        freq.low_sample ? " Weniger als eine Woche Daten: wenig belastbar." : ""
      }`
    : null;
  const lead = durParts(lt.human.p50);

  const whys: Record<string, Why> = {
    freq: {
      title: "Deploy-Frequenz",
      means:
        "Wie oft neuer Code bei den Nutzern ankommt. Häufige kleine Releases tragen wenig Risiko je Release und bringen Feedback früh.",
      watch:
        "die Wochen stark schwanken oder Releases ausbleiben: dann sammeln sich Änderungen zu grossen Releases.",
      lever:
        "kleinere Änderungen, automatisierte Deploys, kein fester Release-Termin für prod.",
      def: `Erfolgreiche Deploys auf ${prod}, deren Images sich vom vorherigen Deploy derselben Argo-App unterscheiden, einmal pro GitOps-Revision gezählt, geteilt durch die Wochen im Zeitraum, in denen Argo CD meldete.`,
      basis: `${freq.releases} Releases, ${freq.resyncs} Syncs ohne neue Images und ${freq.first_seen} Erstdeploys aus ${freq.syncs} Revisionen. Aktualisiert ein Release mehrere Apps über dieselbe GitOps-Revision, zählt es einmal.`,
      filters: [
        "operation_phase = Succeeded",
        `environment = ${prod}`,
        "images ≠ vorheriger Deploy",
        "pro revision",
      ],
      coverage:
        (sinceNote ? `${sinceNote} ` : "") +
        (freq.first_seen > 0
          ? "Der erste Deploy einer App seit Datenbeginn hat keinen Vorgänger: Release oder Re-Sync ist unbekannt, er zählt nicht als Release."
          : "Jede App hat einen Vorgänger in den Daten, alle Deploys sind vergleichbar."),
      query: "README · Deployment-frequency query",
    },
    lead: {
      title: `Lead Time bis ${prod}`,
      means:
        "Die Zeit, in der eine fertige Änderung noch keinem Nutzer hilft. Lang heisst: Feedback kommt spät, Fehler lassen sich schwer einer Änderung zuordnen, Releases werden gross.",
      watch:
        "die langsamsten 10 % weit über dem typischen Wert liegen: dann bleiben einzelne Änderungen hängen, oft hinter einem Release-Termin.",
      lever:
        "Wo die Zeit liegt, zeigt der Fluss darunter; dort liegt der Hebel, nicht in den kurzen Etappen.",
      def: `Pro Commit vom Autorenzeitpunkt bis zum ersten Deploy auf ${prod}, der ihn enthält. Merge-Commits und Release-Tooling ausgenommen, Bots separat.`,
      basis: `${lt.human.n} menschliche Commits, zuerst deployt im Zeitraum${lt.bot.n ? `, dazu ${lt.bot.n} von Bots` : ""}.`,
      filters: [
        "NOT is_merge",
        "NOT service account",
        "Bots separat",
        `environment = ${prod}`,
      ],
      coverage:
        "Nur Commits, die Riptide einem Deploy zuordnen kann (Image-Referenz des Builds oder Release-Notes); ein Deploy ohne Zuordnung fehlt hier.",
      query: "lead_time_changes · docs/dora-lead-time.md",
    },
    dsucc: {
      title: "Deploy-Erfolg",
      means:
        "Ob das Ausrollen selbst klappt: ein abgebrochener oder fehlgeschlagener Sync heisst, jemand muss eingreifen.",
      watch:
        "der Wert sinkt: dann scheitern Deploys an Konfiguration, Ressourcen oder Abhängigkeiten.",
      lever:
        "fehlgeschlagene Syncs unter «Daten» ansehen; meist ist es dieselbe App oder dieselbe Ursache.",
      def: `Anteil der Syncs auf ${prod} mit operation_phase Succeeded an allen abgeschlossenen Syncs. Keine DORA-Metrik: die Change fail rate zählt Deploys, nach denen jemand eingreifen muss, auch wenn der Sync gelang.`,
      basis: `${freq.finished} abgeschlossene Syncs, davon ${freq.failed} Failed.`,
      filters: [
        "operation_phase IN (Succeeded, Failed)",
        `environment = ${prod}`,
      ],
      coverage: "Vollständig: Argo CD meldet jeden abgeschlossenen Sync.",
      query: "argocd_events",
    },
  };
  const open = why && whys[why] ? whys[why] : null;

  return (
    <section id="ueberblick" aria-labelledby="h-ueberblick" className="mt-9">
      <SectionHead
        id="h-ueberblick"
        eyebrow="Überblick"
        title="Was jetzt?"
        lead="Befunde aus den Regeln, die Riptide anwendet, dazu ein fester Gesprächsanlass. Jeder nennt Regel und Zahl und führt zu den Fakten dahinter."
      />
      <Findings m={m} onFacts={onFacts} />

      <div className="mt-7 flex flex-wrap items-baseline gap-x-3.5 gap-y-1">
        <h4 className="m-0 font-serif text-[19px] font-medium text-ink">
          Throughput und Instability
        </h4>
        <span className="text-[13px] text-muted">
          So gliedert DORA die fünf Metriken. Beide Seiten gehören zusammen:
          mehr Throughput bei steigender Instability ist kein Fortschritt.
        </span>
      </div>
      <div className="mt-3 grid grid-cols-[repeat(auto-fit,minmax(240px,1fr))] gap-3">
        <Tile
          label="Deploy-Frequenz"
          dora="DORA · Deployment frequency · Throughput"
          question={`Wie oft geht neuer Code nach ${prod}?`}
          value={freq.syncs ? num(freq.per_week, 1) : "–"}
          unit={freq.syncs ? "Releases pro Woche" : ""}
          note={
            freq.syncs
              ? `${freq.releases} Releases im Zeitraum`
              : freq.argo_reporting
                ? "keine Deploys im Zeitraum"
                : "Argo CD meldet für das Team noch nicht"
          }
          direction={
            <Direction
              arrow="up"
              label="öfter ist besser"
              why="kleinere Releases, weniger Risiko pro Release"
            />
          }
          warn={
            sinceNote ??
            (freq.first_seen > 0
              ? `${freq.first_seen} Erstdeploys ohne Vergleichswert, nicht gezählt`
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
          label={`Lead Time bis ${prod}`}
          dora="DORA · Change lead time · Throughput"
          question={`Wie lange braucht eine Änderung vom Commit bis ${prod}?`}
          value={lead.value}
          unit={lt.human.p50 == null ? "" : `${lead.unit}, typischer Wert`}
          note={
            (lt.human.n === 0
              ? "keine menschlichen Commits zugeordnet"
              : `die langsamsten 10 %: über ${dur(lt.human.p90)} · ${lt.human.n} Commits von Menschen`) +
            (bots && lt.bot.n > 0
              ? ` · Bots typischerweise ${dur(lt.bot.p50)} (${lt.bot.n})`
              : "")
          }
          direction={
            <Direction
              arrow="down"
              label="kürzer ist besser"
              why="schnelleres Feedback von echten Nutzern"
            />
          }
          warn={
            lt.low_sample
              ? "Weniger als 10 Commits: wenig belastbar"
              : undefined
          }
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
          label="Deploy-Erfolg"
          dora="keine DORA-Metrik"
          question="Klappt das Ausrollen selbst?"
          value={pct(freq.success_rate)}
          unit={freq.finished ? "der Argo-Syncs" : ""}
          note={`${freq.failed} von ${freq.finished} Syncs fehlgeschlagen`}
          direction={
            <Direction
              arrow="up"
              label="höher ist besser"
              why="nur mit dem eigenen Verlauf vergleichen"
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
            Instability
          </span>
          <div className="-mt-1 text-[11px] text-faint">
            DORA · Change fail rate · Deployment rework rate · Failed deployment
            recovery time
          </div>
          <div className="text-[13px] text-ink-dim">
            Wie oft macht ein Deploy Ärger, und wie schnell ist er behoben?
          </div>
          <div className="text-[22px] font-medium text-faint">
            nicht gemessen
          </div>
          <div className="text-xs text-muted">
            Dafür braucht es eine Incident-Quelle. Ohne sie zeigt Riptide nur
            Throughput; die Instability-Seite fehlt. Hotfix-Anteil oder
            Deploy-Erfolg ersetzen sie nicht.
          </div>
        </div>
      </div>
      {open && <WhyPanel why={open} />}
    </section>
  );
}
