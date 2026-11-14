import type { Dataset } from "./api";
import { GroupedBars, WeekStack } from "./charts";
import { dur, durParts, pct } from "./format";
import {
  CYCLE_BUCKETS,
  type Chain,
  changeMix,
  cycleTimes,
  histogram,
  mergedPRs,
  spread,
  type Filter,
} from "./metrics";
import type { Period } from "./period";
import {
  Card,
  CardHead,
  Grid,
  Legend,
  SectionHead,
  Swatch,
  Tile,
  type Why,
  WhyPanel,
} from "./ui";

type Props = {
  ds: Dataset;
  period: Period;
  filter: Filter;
  ch: Chain;
  why: string | null;
  onWhy: (key: string) => void;
};

export function Review({ ds, period, filter, ch, why, onWhy }: Props) {
  const humans = mergedPRs(ds, period, filter, false);
  const withBots = mergedPRs(ds, period, filter);
  const bots = withBots.filter((p) => p.bot);
  const cycle = spread(cycleTimes(humans));
  const review = ch.stages.find((s) => s.key === "review")!.spread;
  const untracked = withBots.filter((p) => p.jira_keys.length === 0).length;
  const c = durParts(cycle.p50);
  const r = durParts(review.p50);
  const botNote = filter.bots ? "Bots eingeschlossen" : "ohne Bots";

  const whys: Record<string, Why> = {
    cycle: {
      title: "PR-Cycle-Time",
      def: "pr:merged minus pr:opened pro PR, Schlüssel (repo_full_name, pr_id).",
      basis: `${cycle.n} menschliche PRs, gemergt im Zeitraum.`,
      filters: ["ohne GitOps-Repos", "ohne Bots", "pr:merged im Zeitraum"],
      coverage:
        "PRs, die vor Datenbeginn geöffnet wurden, haben kein pr:opened und fehlen.",
      query: "README · PR cycle time",
    },
    review: {
      title: "Review-Zeit",
      def: "Von PR bereit (opened, bei Drafts ready_for_review) bis zum ersten Approval eines menschlichen Reviewers.",
      basis: `${review.n} PRs mit Approval.`,
      filters: [
        "Reviewer ≠ PR-Ersteller",
        "non_human_identities",
        "ohne GitOps-Repos",
      ],
      coverage: "Vollständig für Bitbucket-Events.",
      query: "README · Lead-time breakdown query",
    },
    touch: {
      title: "Review-Tiefe",
      def: "Anteil der approvten PRs, deren erster Reviewer-Kontakt das Approval selbst ist. Hoch heisst: Review ohne sichtbare Diskussion in Bitbucket.",
      basis: `${ch.approvedOnFirstTouch} von ${ch.approved} approvten PRs.`,
      filters: ["ohne Bot-Kommentare", "ohne Bots", "ohne GitOps-Repos"],
      coverage: "Mündliche Reviews sieht riptide nicht.",
      query: "README · Lead-time breakdown query",
    },
    untracked: {
      title: "Ohne Jira-Key",
      def: "Gemergte PRs ohne Jira-Key in Titel, Beschreibung, Branch oder Commits.",
      basis: `${untracked} von ${withBots.length} gemergten PRs (${botNote}).`,
      filters: ["jira_keys = {}", "ohne GitOps-Repos"],
      coverage: "Vollständig.",
      query: "README · Untracked-work rate",
    },
  };
  const open = why && whys[why] ? whys[why] : null;

  const mix = changeMix(withBots, period);
  const hist = [histogram(cycleTimes(humans)), histogram(cycleTimes(bots))];
  const series = [
    { key: "h", label: "Menschen", className: "bg-s1" },
    ...(filter.bots ? [{ key: "b", label: "Bots", className: "bg-s2" }] : []),
  ];

  return (
    <section id="review" aria-labelledby="h-review" className="mt-10">
      <SectionHead
        id="h-review"
        eyebrow="Nur App-Repos · Promotion-PRs der GitOps-Repos ausgeschlossen"
        title="Code-Review"
      />
      <Grid>
        <Tile
          label="PR-Cycle-Time"
          value={c.value}
          unit={cycle.p50 == null ? "" : `${c.unit} p50`}
          note={`p90 ${dur(cycle.p90)} · ${cycle.n} PRs`}
          open={why === "cycle"}
          onWhy={() => onWhy("cycle")}
        />
        <Tile
          label="Review-Zeit"
          value={r.value}
          unit={review.p50 == null ? "" : `${r.unit} p50`}
          note={`PR bereit bis Approval · p90 ${dur(review.p90)}`}
          open={why === "review"}
          onWhy={() => onWhy("review")}
        />
        <Tile
          label="Approval beim 1. Kontakt"
          value={ch.approved ? pct(ch.approvedOnFirstTouch / ch.approved) : "–"}
          note={`${ch.approvedOnFirstTouch} von ${ch.approved} · ohne Kommentar vor dem Approval`}
          open={why === "touch"}
          onWhy={() => onWhy("touch")}
        />
        <Tile
          label="Ohne Jira-Key"
          value={withBots.length ? pct(untracked / withBots.length) : "–"}
          note={`${untracked} von ${withBots.length} gemergten PRs`}
          open={why === "untracked"}
          onWhy={() => onWhy("untracked")}
        />
      </Grid>
      {open && <WhyPanel why={open} />}

      <div className="mt-3 flex flex-wrap gap-3">
        <Card className="flex-[1_1_460px]">
          <CardHead
            title="PR-Cycle-Time"
            sub="Geöffnet bis gemergt, Anzahl PRs je Dauer"
            right={
              <Legend>
                {series.map((s) => (
                  <Swatch key={s.key} className={s.className} label={s.label} />
                ))}
              </Legend>
            }
          />
          <GroupedBars
            labels={CYCLE_BUCKETS.map((b) => b.label)}
            series={series}
            values={hist.slice(0, series.length)}
            ariaLabel={`PR-Cycle-Time: ${CYCLE_BUCKETS.map((b, i) => `${b.label} ${hist[0][i]}`).join(", ")}`}
          />
        </Card>
        <Card className="flex-[1_1_460px]">
          <CardHead
            title="Change-Mix"
            sub="Gemergte PRs pro Woche nach Branch-Präfix"
            right={
              <Legend>
                <Swatch className="bg-s1" label="feature" />
                <Swatch className="bg-s2" label="bugfix" />
                <Swatch className="bg-s3" label="hotfix" />
                <Swatch className="bg-other" label="anderes / ohne" />
              </Legend>
            }
          />
          <WeekStack
            labels={period.weeks.map((w) => `KW ${w.kw}`)}
            series={[
              { key: "feature", label: "feature", className: "bg-s1" },
              { key: "bugfix", label: "bugfix", className: "bg-s2" },
              { key: "hotfix", label: "hotfix", className: "bg-s3" },
              { key: "other", label: "anderes / ohne", className: "bg-other" },
            ]}
            values={mix}
            ariaLabel={`Change-Mix: ${mix.map((m, i) => `KW ${period.weeks[i].kw} ${m.feature} feature, ${m.bugfix} bugfix`).join("; ")}`}
          />
        </Card>
      </div>
    </section>
  );
}
