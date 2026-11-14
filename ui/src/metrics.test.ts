import { describe, expect, it } from "vitest";
import {
  builds,
  chain,
  changeMix,
  costs,
  cycleTimes,
  deployFrequency,
  environments,
  histogram,
  leadTime,
  mergedPRs,
  repoDurations,
  runKind,
  type Filter,
} from "./metrics";
import { period } from "./period";
import {
  NOW,
  ago,
  dataset,
  deploy,
  leadTime as lt,
  pr,
  review,
  run,
} from "./test/fixtures";

const all: Filter = { product: null, bots: false };
const p4 = period(4, NOW);

describe("deploy frequency", () => {
  const ds = dataset({
    deploys: [
      // one release reconciling two Apps: one revision
      deploy({ app: "kasse-intranet", revision: "r1", at: ago(1) }),
      deploy({ app: "kasse-extranet", revision: "r1", at: ago(1) }),
      deploy({ revision: "r2", kind: "sync", new_images: [], at: ago(2) }),
      deploy({
        app: "portal-prod",
        project: "portal",
        revision: "r3",
        kind: "first_seen",
        new_images: [],
        at: ago(9),
      }),
      // a revision that is a release for one App, a sync for another
      deploy({ app: "a", revision: "r4", kind: "sync", at: ago(10) }),
      deploy({ app: "b", revision: "r4", kind: "release", at: ago(10) }),
      deploy({ revision: "r5", phase: "Failed", kind: null, at: ago(3) }),
      deploy({ revision: "r6", environment: "intg", at: ago(1) }),
      deploy({ revision: "r7", at: ago(60) }), // before the window
    ],
  });

  it("counts releases once per revision, apart from syncs and first deploys", () => {
    const d = deployFrequency(ds, p4, all, "prod");
    expect([d.releases, d.syncs, d.firstSeen]).toEqual([2, 4, 1]);
    expect(d.perWeek).toBeCloseTo(2 / p4.span);
    expect([d.succeeded, d.failed]).toEqual([6, 1]);
    expect(d.weeks).toEqual([
      { release: 0, sync: 0, firstSeen: 0 },
      { release: 0, sync: 0, firstSeen: 0 },
      { release: 1, sync: 0, firstSeen: 1 },
      { release: 1, sync: 1, firstSeen: 0 },
    ]);
  });

  it("filters by product and environment", () => {
    expect(
      deployFrequency(ds, p4, { product: "portal", bots: false }, "prod").syncs,
    ).toBe(1);
    expect(deployFrequency(ds, p4, all, "intg").releases).toBe(1);
    expect(
      deployFrequency(dataset(), period(1, NOW), all, "prod").perWeek,
    ).toBe(0);
  });

  it("orders the environments, production last", () => {
    expect(environments(ds, all)).toEqual(["intg", "prod"]);
    // the product's own deploys only
    expect(environments(ds, { product: "portal", bots: false })).toEqual([
      "prod",
    ]);
    expect(
      environments(
        dataset({ deploys: [deploy({ environment: "test" })] }),
        all,
      ),
    ).toEqual(["test"]);
  });
});

describe("lead time", () => {
  const ds = dataset({
    lead_times: [
      lt({ lead_time_s: 100 }),
      lt({ lead_time_s: 300 }),
      lt({ lead_time_s: 50, is_merge: true }),
      lt({ lead_time_s: 60, service_account: true }),
      lt({ lead_time_s: 70, bot: true }),
      lt({ lead_time_s: 900, environment: "intg" }),
      lt({ lead_time_s: 5, project: "portal" }),
    ],
  });
  it("is per human commit, merges and tooling out, bots apart", () => {
    const l = leadTime(ds, all, "prod");
    expect(l.human).toEqual({ n: 3, p50: 100, p90: 260 });
    expect(l.bot.n).toBe(1);
    expect(
      leadTime(ds, { product: "kasse", bots: false }, "prod").human.n,
    ).toBe(2);
  });
});

describe("PRs", () => {
  const ds = dataset({
    prs: [
      pr({ pr_id: 1 }),
      pr({ pr_id: 2, bot: true, change_type: "other" }),
      pr({ pr_id: 3, gitops: true, repo: "kasse/kasse-gitops" }),
      pr({ pr_id: 4, state: "open", merged_at: null }),
      pr({ pr_id: 5, merged_at: ago(40) }), // merged before the window
      pr({
        pr_id: 6,
        project: "portal",
        repo: "portal/portal-web",
        change_type: "bugfix",
        opened_at: ago(10),
        merged_at: ago(9),
      }),
      pr({ pr_id: 7, opened_at: null, change_type: null, merged_at: ago(9) }),
    ],
  });

  it("are merged in the window, GitOps promotions out, bots per the filter", () => {
    expect(mergedPRs(ds, p4, all).map((x) => x.pr_id)).toEqual([1, 6, 7]);
    expect(
      mergedPRs(ds, p4, { product: null, bots: true }).map((x) => x.pr_id),
    ).toEqual([1, 2, 6, 7]);
    expect(
      mergedPRs(ds, p4, { product: "portal", bots: true }).map((x) => x.pr_id),
    ).toEqual([6]);
  });

  it("cycle time needs the opening", () => {
    const c = cycleTimes(mergedPRs(ds, p4, all));
    expect(c).toHaveLength(2);
    expect(c[0]).toBeCloseTo(0.2 * 86400);
    expect(c[1]).toBeCloseTo(86400);
    expect(
      histogram([60, 900, 3600, 5 * 3600, 2 * 86400, 9 * 86400, 10]),
    ).toEqual([2, 1, 1, 1, 1, 1]);
  });

  it("mixes change types per merge week", () => {
    const mix = changeMix(mergedPRs(ds, p4, { product: null, bots: true }), p4);
    expect(mix[3]).toEqual({ feature: 1, bugfix: 0, hotfix: 0, other: 1 });
    expect(mix[2]).toEqual({ feature: 0, bugfix: 1, hotfix: 0, other: 1 });
  });
});

describe("lead-time chain", () => {
  const h = 3600;
  const ds = dataset({
    prs: [
      pr({
        pr_id: 1,
        first_commit_at: ago(3),
        ready_at: ago(2),
        approved_at: ago(2 - 1 / 24),
        merged_at: ago(2 - 2 / 24),
        environments: [
          { environment: "intg", first_deployed_at: ago(2 - 4 / 24) },
          { environment: "prod", first_deployed_at: ago(1) },
        ],
      }),
      // approved after a comment, never deployed
      pr({
        pr_id: 2,
        approved_on_first_touch: false,
        approved_at: ago(1.5),
        ready_at: ago(2),
        merged_at: ago(1.4),
      }),
      // a bot's PR and a draft never made ready drop out
      pr({ pr_id: 3, bot: true }),
      pr({ pr_id: 4, ready_at: null }),
      // a commit after ready (rebased): no coding stage, never negative
      pr({
        pr_id: 5,
        first_commit_at: ago(1.95),
        ready_at: ago(2),
        approved_at: null,
        merged_at: ago(1.5),
      }),
    ],
  });

  it("splits per human PR into five stages, counts per stage", () => {
    const c = chain(ds, p4, all);
    expect(c.preEnvironment).toBe("intg");
    const by = Object.fromEntries(c.stages.map((s) => [s.key, s.spread]));
    expect(by.coding.n).toBe(2);
    expect(by.coding.p50).toBeCloseTo(86400);
    expect(by.review.n).toBe(2);
    expect(by.review.p50).toBeCloseTo(((1 + 12) / 2) * h);
    expect(by.merge.n).toBe(2);
    expect(by.toPre).toMatchObject({ n: 1 });
    expect(by.toPre.p50).toBeCloseTo(2 * h);
    expect(by.toProd.p50).toBeCloseTo(20 * h);
    expect([c.approved, c.approvedOnFirstTouch]).toEqual([2, 1]);
  });

  it("has no stage before production when nothing deployed elsewhere", () => {
    expect(chain(dataset({ prs: [pr()] }), p4, all).preEnvironment).toBeNull();
    // straight to production: the deploy time still counts, from the merge
    const c = chain(
      dataset({
        prs: [
          pr({
            merged_at: ago(2),
            environments: [{ environment: "prod", first_deployed_at: ago(1) }],
          }),
        ],
      }),
      p4,
      all,
    );
    const toProd = c.stages.find((s) => s.key === "toProd")!.spread;
    expect(toProd.n).toBe(1);
    expect(toProd.p50).toBeCloseTo(86400);
  });

  it("measures a hotfix that reached production first from its merge", () => {
    const c = chain(
      dataset({
        prs: [
          // the regular PR sets intg as the stage before production
          pr({
            pr_id: 1,
            merged_at: ago(3),
            environments: [
              { environment: "intg", first_deployed_at: ago(2.9) },
              { environment: "prod", first_deployed_at: ago(2) },
            ],
          }),
          // the hotfix: prod an hour after the merge, intg days later
          pr({
            pr_id: 2,
            merged_at: ago(5),
            environments: [
              { environment: "prod", first_deployed_at: ago(5 - 1 / 24) },
              { environment: "intg", first_deployed_at: ago(2) },
            ],
          }),
        ],
      }),
      p4,
      all,
    );
    const by = Object.fromEntries(c.stages.map((s) => [s.key, s.spread]));
    expect(by.toPre.n).toBe(1);
    expect(by.toProd.n).toBe(2);
    expect(by.toProd.p50).toBeCloseTo((0.9 * 86400 + 3600) / 2);
  });
});

describe("deploy weeks and data start", () => {
  it("puts a revision in the week of its first deploy, once", () => {
    // Sunday evening and Monday morning around the KW 45/46 boundary
    const ds = dataset({
      deploys: [
        deploy({ app: "a", revision: "r1", at: "2026-11-08T20:00:00+01:00" }),
        deploy({
          app: "b",
          revision: "r1",
          at: "2026-11-09T08:00:00+01:00",
          kind: "sync",
        }),
      ],
    });
    const d = deployFrequency(ds, p4, all, "prod");
    expect(d.releases).toBe(1);
    expect(d.weeks[2]).toEqual({ release: 1, sync: 0, firstSeen: 0 });
    expect(d.weeks[3]).toEqual({ release: 0, sync: 0, firstSeen: 0 });
  });

  it("rates only the weeks Argo CD reported", () => {
    const ds = dataset({ deploys: [deploy({ at: ago(1) })] });
    ds.meta.data_start.argocd = ago(7);
    const d = deployFrequency(ds, p4, all, "prod");
    expect(d.since).toBe(ago(7));
    expect(d.perWeek).toBeCloseTo(1);
    ds.meta.data_start.argocd = ago(60);
    expect(deployFrequency(ds, p4, all, "prod").since).toBeNull();
  });
});

describe("builds", () => {
  const ds = dataset({
    runs: [
      run({ build_type: "release", duration_s: 300 }),
      run({
        build_type: "HOTFIX",
        branch: "release/1.1",
        branch_kind: "release",
        duration_s: 200,
      }),
      run({ status: "FAILURE", duration_s: 900 }),
      run({ duration_s: 600 }),
      run({
        branch_kind: "feature",
        status: "UNSTABLE",
        duration_s: 100,
        pipeline_name: "kasse-api/feature%2Fx",
      }),
      run({ branch_kind: "pr", duration_s: 120 }),
      run({
        branch_kind: "other",
        build_type: null,
        repo: null,
        project: null,
        pipeline_name: "nightly",
      }),
      // before the pipelines sent a build_type: a master run is unknown
      run({ build_type: null, finished_at: ago(20), started_at: ago(20) }),
      run({ status: null }), // no status: not counted
    ],
  });

  it("never mixes kinds of runs", () => {
    const b = builds(ds, p4, all);
    expect(b.map((x) => [x.kind, x.runs, x.succeeded])).toEqual([
      ["release", 2, 2],
      ["main", 2, 1],
      ["feature", 2, 1],
      ["other", 1, 1],
      ["unknown", 1, 1],
    ]);
    const main = b[1];
    expect(main.duration.p50).toBe(750);
    expect([main.ciSeconds, main.wastedSeconds, main.failed]).toEqual([
      1500, 900, 1,
    ]);
    expect(main.weekly[3]).toBe(0.5);
    expect(main.weekly[0]).toBeNull();
    expect(builds(ds, p4, { product: "portal", bots: false })).toEqual([]);
  });

  it("classifies a run", () => {
    expect(runKind(run({ build_type: null }), null)).toBe("unknown");
    expect(runKind(run({ build_type: null }), ago(30))).toBe("main");
    expect(
      runKind(run({ build_type: null, branch_kind: "feature" }), null),
    ).toBe("feature");
  });

  it("ranks repos by median duration", () => {
    expect(repoDurations(ds, all, "main")).toEqual([
      { repo: "kasse/kasse-api", duration: 750, runs: 2 },
    ]);
    expect(repoDurations(ds, all, "other")[0].repo).toBe("nightly");
  });
});

describe("costs", () => {
  const ds = dataset({
    reviews: [
      review({ cost_usd: "1.250000" }),
      review({
        pr_key: "kasse/kasse-api#2",
        cost_usd: "0.750000",
        lines_added: 300,
        lines_removed: 200,
      }),
      review({
        pr_key: "kasse/kasse-api#3",
        outcome: "declined",
        cost_usd: "0.400000",
      }),
      review({
        pr_key: "kasse/kasse-api#4",
        outcome: "deleted",
        cost_usd: null,
      }),
      review({
        pr_key: "portal/portal-web#1",
        repo: "portal/portal-web",
        project: "portal",
        cost_usd: "3.000000",
      }),
    ],
  });
  it("splits merged and wasted spend, normalises per KLOC", () => {
    const c = costs(ds, all);
    expect(c.total).toBeCloseTo(5.4);
    expect(c.merged).toBeCloseTo(5);
    expect(c.mergedReviews).toBe(3);
    expect(c.perKloc).toBeCloseTo((5 / 1500) * 1000);
    expect(c.wasted).toBeCloseTo(0.4);
    expect(c.rows.map((r) => r.label)).toEqual(["portal", "kasse"]);
    expect(c.reviewedRepos).toBe(2);
  });
  it("by repo within a product", () => {
    const c = costs(ds, { product: "kasse", bots: false });
    expect(c.rows).toEqual([
      { label: "kasse/kasse-api", value: expect.closeTo(2.4) },
    ]);
    expect(costs(dataset(), all).perKloc).toBeNull();
  });
});
