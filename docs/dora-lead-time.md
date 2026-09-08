# Lead time for changes (DORA), per commit

**How long from a developer committing to that change running in production.**
One row per change, not per release: DORA's second key metric.

Riptide ships three views for it. Query `lead_time_changes`; the two below it
exist so the fragile part stays swappable.

| View | One row per | Purpose |
|---|---|---|
| `commit_sightings` | commit seen on a master push | expands `payload->'commits'`, with timestamps and classification |
| `deploy_commit_ranges` | (deploy, app repo) | which commit range a deploy shipped |
| `lead_time_changes` | (environment, commit) | the change and the **first** deploy that carried it |

## The metric

```sql
-- Lead time to production, human-authored changes, last 90 days
SELECT
    percentile_cont(0.5) WITHIN GROUP (ORDER BY lead_time) AS p50,
    percentile_cont(0.9) WITHIN GROUP (ORDER BY lead_time) AS p90,
    count(*) AS changes
FROM lead_time_changes
WHERE environment = 'prod'
  AND NOT is_merge
  AND NOT author_is_service_account
  -- Match the login AND the display name, the same rule the rest of riptide
  -- uses: a bot is often provisioned with a nondescript login. NOT EXISTS
  -- rather than NOT IN, so a commit with an unresolved author is kept rather
  -- than silently dropped by NULL propagation.
  AND NOT EXISTS (
        SELECT 1 FROM unnest(:automation_handles) AS h(handle)
        WHERE lower(h.handle) IN (lower(author), lower(author_display_name))
      )
  AND first_deployed_at > now() - interval '90 days';
```

Group by `team`, `repo_full_name`, or `date_trunc('week', first_deployed_at)`
for the breakdowns. Swap `environment` for the stage you treat as production.

## What counts as a change

The views classify; the query decides. The defaults that make the number mean
what people think it means:

- **Exclude merge commits** (`NOT is_merge`). A squash-and-merge lands the change
  once; counting the merge commit too double-counts it.
- **Exclude release tooling** (`NOT author_is_service_account`, plus your CI
  account if it is not flagged `SERVICE` by the git host). `[maven-release-plugin]
  prepare for next development iteration`, `[gradle-release] …` and
  component-version commits are created *by* the release, so their lead time is
  near zero and they drag the median down.
- **Report bots separately, never blended.** Dependency updates are real changes
  that ship, but in one measured dataset Renovate authored 87 % of all commits
  reaching production. Blending makes the median describe the bot's cadence
  (p50 209.3 h) rather than the team's (p50 193.6 h). Two lines, always.

## Reading it

Lower is better, and the split between environments is where the signal is. In
the dataset this was built on, human changes reached **intg in 4.0 h (p50)** but
**production in 193.6 h (p50), p90 505.6 h** — the delivery pipeline is fast and
the wait is entirely in front of production, in release scheduling. A single
blended number would have hidden that.

Beware the tempting shortcut this replaces: taking the *newest* commit in each
release and calling that lead time. It answers "how stale was the freshest
change" and reads 26.7 h on the same data — 7× too flattering.

## Limits, so nobody over-reads the number

- **`authored_at` is the commit's own timestamp**, which a rebase rewrites.
  That is DORA's "code committed"; `committed_at` sits next to it for comparison.
- **Coverage is bounded by range resolution.** A deploy whose commit range cannot
  be resolved contributes nothing — it is absent, never counted as fast. Count
  deploys, not bumps: one deploy fans out to a row per bumped component.

  ```sql
  SELECT count(DISTINCT a.id) AS deploys,
         count(DISTINCT a.id) FILTER (WHERE r.deployed_at IS NULL) AS unresolved
  FROM argocd_events a
  LEFT JOIN deploy_commit_ranges r
         ON r.app_name = a.app_name AND r.deployed_at = a.occurred_at
  WHERE a.operation_phase = 'Succeeded';
  ```

- **A missing boundary commit costs a whole release, not one change.** The range
  needs both endpoint SHAs, so if either is absent — the `commits[]` cap below,
  or an ingest gap — every change in that window disappears. It does not recover
  later either: the next release's range starts at *this* release's head, so the
  window is skipped, not deferred.
- **Bitbucket caps `commits[]` at 5 per push.** Measured, that bites 0.6 % of
  master pushes. For changes it keeps it is harmless, but via the point above a
  dropped commit that happens to be a release boundary costs its whole window.
- **Boundary commits are assumed to be push tips.** Range membership is decided
  by push time, so every commit of a push lands on one side of the boundary.
  That is exact when the boundary SHA is the tip of its push, which is what a
  release cut normally is. Where it is not, commits sharing that push are
  attributed to the neighbouring release. Likewise the GitOps release commit
  must be a push tip to be found at all — a release split across two pushes
  where Argo reports only the second revision leaves the first unresolvable.
  Both go away with `image_ref`-based ranges.
- **Ranges come from release notes today.** `deploy_commit_ranges` reads the
  compare links a release-note generator writes into the GitOps commit, so a
  release landed as a direct version bump is invisible to it. That is the view to
  replace once CI senders report `pipeline_events.image_ref`: the range then
  becomes "between the previous and the current successful deploy of this app",
  which needs no release notes at all. See
  [Correlating deploys back to commits](correlating-deploys-to-commits.md).
- **Ingestion is forward-only.** The metric covers what riptide has seen.
