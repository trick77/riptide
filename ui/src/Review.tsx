import type { Metrics } from "./api";
import { GroupedBars, WeekStack } from "./charts";
import { dur, durParts, pct } from "./format";
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

// Wording of the backend's cycle-time buckets by upper bound in seconds;
// a bound without one says so plainly rather than mislabel a count.
const BUCKET_LABEL: Record<number, string> = {
  900: "< 15 min",
  3600: "15–60 min",
  14400: "1–4 h",
  86400: "4–24 h",
  259200: "1–3 Tage",
};
const OPEN_LABEL: Record<number, string> = { 259200: "> 3 Tage" };

function bucketLabels(bounds: number[]): string[] {
  const last = bounds[bounds.length - 1];
  return bounds
    .map((b) => BUCKET_LABEL[b] ?? `bis ${dur(b)}`)
    .concat(OPEN_LABEL[last] ?? `ab ${dur(last)}`);
}

type Props = {
  m: Metrics;
  why: string | null;
  onWhy: (key: string) => void;
};

export function Review({ m, why, onWhy }: Props) {
  // As the numbers were computed, not as the toggle stands while they load.
  const bots = m.bots;
  const r = m.review;
  const c = durParts(r.cycle.p50);
  const rv = durParts(r.review.p50);
  const botNote = bots ? "Bots eingeschlossen" : "ohne Bots";

  const whys: Record<string, Why> = {
    cycle: {
      title: "PR-Cycle-Time",
      def: "pr:merged minus pr:opened pro PR, Schlüssel (repo_full_name, pr_id).",
      basis: `${r.cycle.n} menschliche PRs, gemergt im Zeitraum.`,
      filters: ["ohne GitOps-Repos", "ohne Bots", "pr:merged im Zeitraum"],
      coverage:
        "PRs, die vor Datenbeginn geöffnet wurden, haben kein pr:opened und fehlen.",
      query: "README · PR cycle time",
    },
    review: {
      title: "Review-Zeit",
      def: "Von PR bereit (opened, bei Drafts ready_for_review) bis zum ersten Approval eines menschlichen Reviewers.",
      basis: `${r.review.n} PRs mit Approval.`,
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
      basis: `${r.approved_on_first_touch} von ${r.approved} approvten PRs.`,
      filters: ["ohne Bot-Kommentare", "ohne Bots", "ohne GitOps-Repos"],
      coverage: "Mündliche Reviews sieht riptide nicht.",
      query: "README · Lead-time breakdown query",
    },
    untracked: {
      title: "Ohne Jira-Key",
      def: "Gemergte PRs ohne Jira-Key in Titel, Beschreibung, Branch oder Commits.",
      basis: `${r.untracked} von ${r.merged} gemergten PRs (${botNote}).`,
      filters: ["jira_keys = {}", "ohne GitOps-Repos"],
      coverage: "Vollständig.",
      query: "README · Untracked-work rate",
    },
  };
  const open = why && whys[why] ? whys[why] : null;

  const hist = [r.cycle_histogram.human, r.cycle_histogram.bot];
  const labels = bucketLabels(r.cycle_histogram.upper_bounds_s);
  const series = [
    { key: "h", label: "Menschen", className: "bg-s1" },
    ...(bots ? [{ key: "b", label: "Bots", className: "bg-s2" }] : []),
  ];
  const weeks = m.period.weeks;

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
          unit={r.cycle.p50 == null ? "" : `${c.unit} p50`}
          note={`p90 ${dur(r.cycle.p90)} · ${r.cycle.n} PRs`}
          open={why === "cycle"}
          onWhy={() => onWhy("cycle")}
        />
        <Tile
          label="Review-Zeit"
          value={rv.value}
          unit={r.review.p50 == null ? "" : `${rv.unit} p50`}
          note={`PR bereit bis Approval · p90 ${dur(r.review.p90)}`}
          open={why === "review"}
          onWhy={() => onWhy("review")}
        />
        <Tile
          label="Approval beim 1. Kontakt"
          value={pct(r.first_touch_rate)}
          note={`${r.approved_on_first_touch} von ${r.approved} · ohne Kommentar vor dem Approval`}
          open={why === "touch"}
          onWhy={() => onWhy("touch")}
        />
        <Tile
          label="Ohne Jira-Key"
          value={pct(r.untracked_rate)}
          note={`${r.untracked} von ${r.merged} gemergten PRs`}
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
            labels={labels}
            series={series}
            values={hist.slice(0, series.length)}
            ariaLabel={`PR-Cycle-Time: ${labels.map((l, i) => `${l} ${hist[0][i]}`).join(", ")}`}
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
            labels={weeks.map((w) => `KW ${w.kw}`)}
            series={[
              { key: "feature", label: "feature", className: "bg-s1" },
              { key: "bugfix", label: "bugfix", className: "bg-s2" },
              { key: "hotfix", label: "hotfix", className: "bg-s3" },
              { key: "other", label: "anderes / ohne", className: "bg-other" },
            ]}
            values={r.change_mix}
            ariaLabel={`Change-Mix: ${r.change_mix.map((x, i) => `KW ${weeks[i].kw} ${x.feature} feature, ${x.bugfix} bugfix`).join("; ")}`}
          />
        </Card>
      </div>
    </section>
  );
}
