# Metrics and the event tables

How each metric riptide shows is defined, as SQL over the raw event tables,
and how to read those tables without fooling yourself. riptide's own
numbers come from `backend/internal/metrics`; a test runs the
[deployment-frequency](#deployment-frequency-query) and
[lead-time breakdown](#lead-time-breakdown-query) queries below on the same
data and checks the two agree, so keep their headings and SQL blocks in
place.

- [Reading the event tables](#reading-the-event-tables)
- [Metrics](#metrics)
- [How the UI's numbers are computed](#how-the-uis-numbers-are-computed)

## Reading the event tables

Before writing a query, know what an event means. Three readings look obvious
and are wrong, all measured on production data:

**`repo:refs_changed` is a ref-movement log, not developer activity.** It says
master now points at a different commit — it does not say a person pushed. In
that dataset, 16 295 of these events were on `master` and only 1 210 carried a
human name; every one of those was a merge commit (`Pull request #NNN: …`),
already counted on the PR side as `pr:merged`. The other 15 000 were release
tooling: `[maven-release-plugin] prepare for next development iteration`,
`[gradle-release] …`, a component-version job, and Renovate landing updates.
Build per-author or per-repo activity views on PR events; use
`repo:refs_changed` for what moved, and for revert detection.

**`change_type` only means something where a branch prefix exists.** It is
parsed from `branch_name`, so a push to `master` has nothing to classify and
falls to `other`. Computed over all events it read 83 % `other`, which says
nothing about the work: restrict it to `pr:opened` / `pr:merged` rows, where
the source branch carries the prefix.

**A PR into a GitOps repo is a release, not work.** Where stages are promoted
by PR (a branch like `<product>_PROD_<versions>` bumping image versions in the
infra repo), those PRs are opened and merged within minutes, mostly without an
approval, and carry no branch prefix. In one month's data they were 146 of 265
human PRs: blended in, PR cycle time read 10 min instead of 45, 136 PRs seemed
merged unreviewed where the app repos had none, and the change mix was half
`other`. They already count on the deploy side. A promotion is mechanical even
when a human opens and merges it, and so is a CI run in a GitOps repo, which
only checks one. Leave both out of every PR and build metric; only the review
spend total keeps them, with their spend on its own line. The GitOps repos are
the ones whose commits Argo CD deploys, so no list of names is needed:

```sql
-- GitOps repos: their commits are what argocd_events.revision points at.
gitops_repos AS (
  SELECT DISTINCT b.repo_full_name
  FROM argocd_events a
  JOIN bitbucket_events b ON b.commit_sha = a.revision
  WHERE b.repo_full_name IS NOT NULL
)
-- ... WHERE repo_full_name NOT IN (SELECT repo_full_name FROM gitops_repos)
```

riptide reads that set once per dataset (`backend/internal/dataset/sql/gitops.sql`)
and hands it to its queries as a `text[]` parameter (`= ANY($n)`, `<> ALL($n)`);
a new dataset query takes the parameter, never a CTE of its own.

The general rule: an event's name describes what the git host did, not who did
it or why. Check `author` and the commit message before reading intent into a
row count.

## Metrics

riptide ingests; **metrics are computed on read** as SQL queries
(or, eventually, materialized views) over the raw event tables. The config
and the schema have been designed so the following are all derivable from
the data captured in v1.

> Anything below not currently in v1 is marked *(planned)*; the schema reserves
> room for it without rework.

> `argocd_events.environment` is the lowercased suffix of the destination
> namespace (after the last `-`) — e.g. `payments-prod` → `prod`,
> `checkout-intg` → `intg`. Which suffix counts as "production" is configured
> in `config/riptide.json` (`environments.production_stage`,
> default `prod`). The literal `'prod'` in the example queries below is a
> placeholder — substitute whatever your `production_stage` is set to. Rows
> ingested before this column existed have `environment = NULL`.

### From the DORA / DX / SPACE families

| Metric | How it's computed |
|---|---|
| **Deployment frequency** | Releases per `team` / time window in prod. A release is a successful deploy whose images differ from the same App's previous successful deploy naming images, counted once per GitOps `revision` — see the query below. A deploy naming no images deployed none: a sync, never compared, and never an App's first-seen deploy. Not every sync is a release: a config change or a manual re-sync redeploys the same images (measured: a third of all intg syncs, and 7 of 8 syncs of one monitoring App), and one release reconciles every App sharing the revision (intranet + extranet), so `COUNT(*)` overcounts twice over. Report the release count next to the syncs and the first-seen deploys it was derived from, so a reader can see where the number comes from. Group by `app_name` for the per-App drill-down; drop infrastructure Apps (monitoring, bots) by name in your own report. |
| **Change lead time** | Per commit, not per release: `SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY lead_time) FROM lead_time_changes WHERE environment='prod' AND NOT is_merge AND NOT author_is_service_account`. The `lead_time_changes` view maps every commit that shipped to the **first** deploy that carried it, so a release rolled out to four Apps counts each change once. Exclude merge commits and release tooling, and report bots on their own line — see [Change lead time (DORA)](dora-lead-time.md) for why, and for the coverage limits. Group by `team` or `repo_full_name`; `change_type` is not available here, since these commits come from master pushes, which carry no branch prefix. Do **not** substitute the newest commit per release: it answers "how stale was the freshest change" and reads ~7× too fast. |
| **Lead time breakdown** | Where the lead time is spent, per human PR in app repos, as five stages: coding (oldest commit on the branch → PR ready), review (ready → approval), merge (approval → merged), to intg (merged → the merge commit's first intg deploy), to prod (intg → prod). One stage for review, not pickup + review: measured, 95 of 115 approvals were the reviewer's first and only touch, so "first review → approval" is zero by construction; the count is reported as a review-depth signal. Stages 4–5 reach only PRs whose merge commit `lead_time_changes` resolves, so they cover fewer PRs than 1–3 — report the count per stage. See the query below. |
| **PR cycle time** | `pr:merged.occurred_at − pr:opened.occurred_at` per PR, keyed by `(repo_full_name, pr_id)`: PR ids are numbered per repository, so `pr_id` alone joins unrelated PRs. A PR opened before ingestion started has no `pr:opened` and drops out. Exclude GitOps repos: their PRs are stage promotions (see [Reading the event tables](#reading-the-event-tables)). Merged PRs only: a PR still open is in no cycle time, however long it waits — see Open PR age. |
| **Open PR age** | The PRs open at an instant T (the period's end), per repo, with their age and what they wait on. Open at T: no `pr:merged` / `pr:deleted` before T, the PR's latest event before T is no `pr:declined`, and the latest event carrying a `pullRequest.state` has it `OPEN` (none does: open unless declined). Every PR event carries the PR, so a declined PR reopened is open again on its next event. Only events before T count, so the previous period's end reads what was open then; a PR open all along that riptide first heard of after it counts there too, else it would read as a new long-open one. Age: T − its last turn ready, the first event after the last one whose `pullRequest.draft` was true, whether or not the parser recognised the flip; never seen a draft (opened ready, or made ready before ingestion) T − `pullRequest.createdDate`, which every PR event carries, so a PR opened before ingestion has one too. Time as a draft is its author's, not the team's: a long draft just made ready is no long-open PR (user, 2026-11-20). A draft has no age and is in no median. Waits on, the first that applies: draft; changes (a human `NEEDS_WORK` in the latest event's `reviewers` or `participants`, Bitbucket's own status, approvals reset by a push included); merge (a human `APPROVED` there); review (a reviewer touch as in the pickup-time query); first review. Long open: 7 days or more; a repo with 4 long-open PRs or more is jammed (both chosen, not measured; the jam was 3 until 2026-11-18). A draft is never long open and never inactive: it waits on its author and may lie as long as it wants (user, 2026-11-18); it stays open, waiting on "draft". Draft is the latest event's `pullRequest.draft`, so a ready PR turned back into a draft is stored as a synthetic `pr:back_to_draft` (else it read as ready until its next event). Per repo the median age of its ready PRs, never the oldest PR's, which one PR alone decides. Exclude GitOps repos and bots as for cycle time. Inactive: no event of any kind on the PR for 30 days before T (for a PR riptide first heard of after T, since its creation); counted apart, in no other number. Webhook-only data cannot tell a PR whose close event Bitbucket never delivered from one nobody touches, and such losses recur: a lost one counts as open until it goes inactive, and alone never makes a jam. A PR without any event since ingestion is missing. |
| **Time to first review** *(DX Core 4 "code review pickup time")* | Two-part computation per PR — see the SQL block below the table. Clock-start = `COALESCE(pr:ready_for_review, pr:opened)`: PRs opened ready start at `pr:opened`; PRs opened as drafts start at the synthetic `pr:ready_for_review` (emitted by the parser when a `pr:modified` payload carries `previousDraft=true, draft=false`). Engagement = first reviewer touch (`pr:comment:added`, `pr:reviewer:approved`, `pr:reviewer:unapproved`, `pr:reviewer:needs_work`, `pr:reviewer:updated`) where `lower(author) != lower(pr_opener) AND NOT is_automated AND occurred_at >= clock-start`. The five-event reviewer union covers every touch Bitbucket DC emits (silent approvals, retracted approvals, "needs work" flips, bare reviewer-status changes); the `occurred_at >= clock-start` guard drops early-feedback comments solicited during the draft phase, which would otherwise produce negative pickup times. `NOT is_automated` strips bot comments (noergler / Renovate / etc.) — every review-time bot must be recognisable, otherwise its instant comment drives the metric toward zero. Detection matches the `automation` config block against **both** the login handle (`author`) and the display name (`author_display_name`), because a bot is often provisioned as an ordinary user account whose login says nothing. `is_automated` is decided at ingest, so rows written before a bot was recognised stay marked human: the `non_human_identities` CTE in the query below filters those at read time, on both names too, from the accounts senders declare (`reviewer_handle` + `reviewer_account_kind` on noergler rollups, `actor_handle` + `actor_account_kind` on pipeline events) — riptide stores the declaration, it carries no account names of its own. A declared handle is matched against both the login and the display name, so it must never equal a person's display name: that person's reviews and PRs would drop out as a bot's. |
| **Build success rate** | `pipeline_events` with `phase = 'COMPLETED'` grouped by `status`. Slice by `source` to compare Jenkins vs Tekton, by `pipeline_name` / `team` for ownership. |
| **Build duration** | `pipeline_events.duration_seconds` (a Postgres `GENERATED ALWAYS AS (finished_at − started_at)` column). Every duration median, spread and comparison is over green runs only: a red run stops wherever it failed, so mixed in it makes a repo that fails early read fast (user, 2026-11-18: "stupide"). Red runs count in the failure rate and their time in the CI time in red runs, never in a duration; an outlier is any run past the cut, red or green. |
| **Deploy success rate** | `argocd_events` with `operation_phase IN ('Succeeded', 'Failed')` aggregated, filtered to `environment = 'prod'` for the prod-only view. |
| **Deploy duration** | `argocd_events.duration_seconds` (generated column). Filter by `environment = 'prod'` for production-only timing. |

#### Deployment-frequency query

One row per team and week, with the three numbers a report should show side
by side: the releases, plus the two counts they were filtered from.

```sql
WITH deploys AS (
  SELECT id, team, app_name, revision, occurred_at,
         -- Sorted: the same images in another order are not a change.
         (SELECT array_agg(i ORDER BY i)
            FROM jsonb_array_elements_text(payload->'images') i) AS images
  FROM argocd_events
  WHERE operation_phase = 'Succeeded' AND environment = 'prod'
),
compared AS (
  -- A sync naming no images deployed none: its own partition, so it is a
  -- sync and the next deploy compares with the images before it.
  SELECT *,
         lag(images) OVER (PARTITION BY team, app_name, images IS NULL ORDER BY occurred_at, id) AS previous_images,
         images IS NOT NULL
           AND row_number() OVER (PARTITION BY team, app_name, images IS NULL ORDER BY occurred_at, id) = 1 AS first_seen
  FROM deploys
)
SELECT team,
       date_trunc('week', occurred_at) AS week,
       -- new images against the App's previous deploy: the release count
       count(DISTINCT revision) FILTER (WHERE NOT first_seen
                                          AND images IS DISTINCT FROM previous_images) AS releases,
       -- an App's first deploy naming images has nothing to compare against:
       -- a release or a re-sync, unknown. Shown apart, never added in.
       count(DISTINCT revision) FILTER (WHERE first_seen) AS first_seen,
       -- every successful sync, releases or not
       count(DISTINCT revision) AS syncs
FROM compared
GROUP BY team, week
ORDER BY week;
```

`first_seen` is large only in the weeks after an App is onboarded and falls to
zero once every App has deployed twice.

#### Lead-time breakdown query

One row per stage with its PR count, p50 and p90. In the data it was measured
on, the delivery side took about a day from first commit to intg and then
waited close to two weeks for production, which a single lead-time number
hides. Coding time starts at the oldest commit pushed to the branch, which a
rebase resets.

```sql
WITH non_human_identities AS (
  SELECT DISTINCT lower(reviewer_handle) AS handle
  FROM noergler_events
  WHERE reviewer_handle IS NOT NULL AND reviewer_account_kind IN ('bot', 'service')
  UNION
  SELECT DISTINCT lower(actor_handle)
  FROM pipeline_events
  WHERE actor_handle IS NOT NULL AND actor_account_kind IN ('bot', 'service')
),
gitops_repos AS (
  SELECT DISTINCT b.repo_full_name
  FROM argocd_events a
  JOIN bitbucket_events b ON b.commit_sha = a.revision
),
prs AS (
  SELECT repo_full_name, pr_id,
         -- branch_name is stored lowercased; the payload keeps the original
         MAX(lower(payload->'pullRequest'->'fromRef'->>'displayId'))
             FILTER (WHERE event_type = 'pr:opened') AS source_branch,
         MAX(author) FILTER (WHERE event_type = 'pr:opened') AS pr_opener,
         BOOL_OR(is_automated) FILTER (WHERE event_type = 'pr:opened') AS bot_pr,
         COALESCE(MIN(occurred_at) FILTER (WHERE event_type = 'pr:ready_for_review'),
                  MIN(occurred_at) FILTER (WHERE event_type = 'pr:opened')) AS ready_at,
         MIN(occurred_at) FILTER (WHERE event_type = 'pr:reviewer:approved') AS approved_at,
         MIN(occurred_at) FILTER (WHERE event_type = 'pr:merged') AS merged_at,
         MAX(payload->'pullRequest'->'properties'->'mergeCommit'->>'id')
             FILTER (WHERE event_type = 'pr:merged') AS merge_commit
  FROM bitbucket_events
  WHERE pr_id IS NOT NULL
    AND repo_full_name NOT IN (SELECT repo_full_name FROM gitops_repos)
  GROUP BY repo_full_name, pr_id
),
chain AS (
  SELECT p.*,
         -- oldest commit pushed to the PR's branch; a rebase resets it
         (SELECT to_timestamp(MIN((c->>'authorTimestamp')::bigint) / 1000.0)
            FROM bitbucket_events e
            CROSS JOIN LATERAL jsonb_array_elements(e.payload->'commits') c
           WHERE e.event_type = 'repo:refs_changed'
             AND e.repo_full_name = p.repo_full_name
             AND e.branch_name = p.source_branch) AS first_commit_at,
         -- same reviewer touch as the pickup-time query
         (SELECT MIN(e.occurred_at)
            FROM bitbucket_events e
           WHERE e.repo_full_name = p.repo_full_name AND e.pr_id = p.pr_id
             AND e.event_type IN ('pr:comment:added', 'pr:reviewer:approved',
                                  'pr:reviewer:unapproved', 'pr:reviewer:needs_work',
                                  'pr:reviewer:updated')
             AND lower(e.author) IS DISTINCT FROM lower(p.pr_opener)
             AND NOT e.is_automated
             AND NOT EXISTS (SELECT 1 FROM non_human_identities n
                              WHERE n.handle IN (lower(e.author), lower(e.author_display_name)))
             AND e.occurred_at >= p.ready_at) AS first_review_at,
         -- the merge commit's first deploy per stage
         (SELECT first_deployed_at FROM lead_time_changes l
           WHERE l.commit_sha = p.merge_commit AND l.environment = 'intg') AS intg_at,
         (SELECT first_deployed_at FROM lead_time_changes l
           WHERE l.commit_sha = p.merge_commit AND l.environment = 'prod') AS prod_at
  FROM prs p
  WHERE NOT COALESCE(p.bot_pr, false) AND p.merged_at IS NOT NULL AND p.ready_at IS NOT NULL
),
stages AS (
  SELECT 1 AS step, 'coding: first commit -> PR ready' AS stage, ready_at - first_commit_at AS wait
    FROM chain WHERE first_commit_at <= ready_at
  UNION ALL
  -- One stage, not pickup + review: most approvals are the reviewer's first
  -- and only touch, so "first review -> approval" is zero by construction.
  SELECT 2, 'review: PR ready -> approval', approved_at - ready_at
    FROM chain WHERE approved_at >= ready_at
  UNION ALL
  SELECT 3, 'merge: approval -> merged', merged_at - approved_at
    FROM chain WHERE merged_at >= approved_at
  UNION ALL
  SELECT 4, 'to intg: merged -> on intg', intg_at - merged_at
    FROM chain WHERE intg_at >= merged_at
  UNION ALL
  SELECT 5, 'to prod: on intg -> on prod', prod_at - intg_at
    FROM chain WHERE prod_at >= intg_at
)
SELECT step, stage,
       count(*) AS prs,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY wait) AS p50,
       percentile_cont(0.9) WITHIN GROUP (ORDER BY wait) AS p90,
       -- review depth: approved with no reviewer touch before the approval
       CASE WHEN step = 2 THEN
         (SELECT count(*) FROM chain WHERE approved_at IS NOT NULL AND first_review_at = approved_at)
       END AS approved_on_first_touch
FROM stages
GROUP BY step, stage
ORDER BY step;
```

#### Pickup-time query

The collector emits a synthetic `pr:ready_for_review` row when a `pr:modified`
payload carries a draft→ready flip (`previousDraft=true, draft=false`), and a
synthetic `pr:back_to_draft` for the way back, which the open PRs read; other
`pr:modified` variants — title / description / target-branch changes — are
dropped at parse time. The raw `eventKey` survives on `payload.eventKey` for
traceability. With that in place, the metric is one CTE:

```sql
WITH non_human_identities AS (
  -- Accounts senders declared as non-human, learned from the stream rather
  -- than from config: each sender reports the git-host account it acts
  -- through and what that account is. `is_automated` is decided at ingest,
  -- so rows written before an identity was declared are filtered here.
  SELECT DISTINCT lower(reviewer_handle) AS handle
  FROM noergler_events
  WHERE reviewer_handle IS NOT NULL AND reviewer_account_kind IN ('bot', 'service')
  UNION
  SELECT DISTINCT lower(actor_handle)
  FROM pipeline_events
  WHERE actor_handle IS NOT NULL AND actor_account_kind IN ('bot', 'service')
),
gitops_repos AS (
  -- Stage-promotion PRs are releases, not work: see "Reading the event tables".
  SELECT DISTINCT b.repo_full_name
  FROM argocd_events a
  JOIN bitbucket_events b ON b.commit_sha = a.revision
),
pickup_start AS (
  SELECT
    repo_full_name,
    pr_id,
    COALESCE(
      MIN(occurred_at) FILTER (WHERE event_type = 'pr:ready_for_review'),
      MIN(occurred_at) FILTER (
        WHERE event_type = 'pr:opened'
          AND COALESCE(payload->'pullRequest'->>'draft', 'false') = 'false'
      )
    ) AS clock_start,
    MAX(author) FILTER (WHERE event_type = 'pr:opened') AS pr_opener
  FROM bitbucket_events
  WHERE repo_full_name NOT IN (SELECT repo_full_name FROM gitops_repos)
  GROUP BY repo_full_name, pr_id
)
SELECT
  e.repo_full_name,
  e.pr_id,
  MIN(e.occurred_at) - ps.clock_start AS pickup_interval
FROM bitbucket_events e
JOIN pickup_start ps USING (repo_full_name, pr_id)
WHERE ps.clock_start IS NOT NULL
  AND e.event_type IN (
        'pr:comment:added',
        'pr:reviewer:approved',
        'pr:reviewer:unapproved',
        'pr:reviewer:needs_work',
        'pr:reviewer:updated'
      )
  AND lower(e.author) IS DISTINCT FROM lower(ps.pr_opener)
  AND NOT e.is_automated
  AND NOT EXISTS (
        SELECT 1 FROM non_human_identities n
         WHERE n.handle IN (lower(e.author), lower(e.author_display_name))
      )
  AND e.occurred_at >= ps.clock_start
GROUP BY e.repo_full_name, e.pr_id, ps.clock_start;
```

The `occurred_at >= ps.clock_start` filter is load-bearing: a reviewer can
comment on a draft PR (typically when the author solicits early feedback), and
without this guard the engagement timestamp could land before the ready signal
and produce a negative interval. Both "early feedback in draft" and "the act of
flipping the switch" are intentionally excluded from the metric — pickup time
measures reviewer engagement *after the PR is ready*, nothing else.

The `draft = false` guard inside the `pr:opened` branch of the `COALESCE` is the
same rule applied to the opposite tail: a PR opened as a draft that never gets
flipped to ready has no clock-start, so it's excluded from the metric entirely.
Early-feedback comments on a never-ready draft don't inflate the numerator,
because there's no clock-start to subtract from in the first place.

### Quality / process signals from Bitbucket

| Metric | How it's computed |
|---|---|
| **PR size** | `lines_added`, `lines_removed`, `files_changed` on `noergler_pr_rollups` (one row per PR; a reopened PR has two `pr_completed` rows in `noergler_events`), joined to Bitbucket PRs on `pr_key` = `'<repo_full_name>#<pr_id>'`. The same columns exist on `bitbucket_events` but are always NULL: Bitbucket DC webhooks carry no diff stats, and fetching them would put an outbound REST call in the ingest path. Covers the repos noergler reviews. |
| **Revert rate** | `COUNT(*) WHERE is_revert = true` over total commits — a free, weak Change-Failure-Rate proxy. |
| **Hotfix rate** | `COUNT(*) WHERE change_type = 'hotfix'` over total deploys per window — operational-pain signal. |
| **Change mix** | Distribution of `change_type` (feature / bugfix / hotfix / chore / refactor / docs / other) per team per week, over `pr:opened` / `pr:merged` rows only — a push to `master` has no prefix to classify and would swamp the distribution with `other`. Exclude GitOps repos: stage-promotion PRs carry no prefix either. |
| **Tickets per deploy** | `COUNT(DISTINCT unnest(jira_keys))` per deploy — small-batch indicator. Jira keys are extracted at write time from PR title, description, branch name, and commit messages via regex `[A-Z][A-Z0-9]+-\d+`, deduplicated, GIN-indexed. |
| **Untracked-work rate** | `COUNT(*) WHERE jira_keys = '{}'` over merged PRs outside GitOps repos — process-compliance signal. |
| **Per-ticket flow** | `WHERE 'ABC-1234' = ANY(jira_keys)` returns every event for a ticket across Bitbucket / pipeline / Argo (joined via commit_sha). |
| **Human vs automated split** | `WHERE NOT is_automated` (Renovate / Dependabot / Snyk / Mend / generic-bot detection runs at write time and tags `automation_source`; an account the git host itself marks as a service account — Bitbucket DC's built-in system user, which files the default PR tasks — is tagged `service-account` with no config entry), plus the `non_human_identities` filter for accounts a sender declared. Keep `bot` and `service` apart when reading: a **bot** authors work of its own and its velocity is worth its own view, while a **service** account (a CI user pushing merges) authors nothing and should simply not appear in human activity. Default dashboards exclude both. |
| **AI reviewer precision** *(noergler)* | `1 - count(noergler_events WHERE event_type='feedback' AND verdict='disagreed') / sum(noergler_pr_rollups.findings_count)` per repo × week — findings on both sides, since one PR can collect several disagreements. Higher = the AI review is more useful. Filter on `outcome='merged'` to score precision only on PRs that shipped. |

### FinOps signals

For CI / deploy compute, riptide captures **duration and attribution** but
does not assign currency — multiply by your own $/runner-second to convert.
LLM review cost is the exception: when the noergler source is wired up,
events arrive pre-priced in USD.

| Signal | How it's computed |
|---|---|
| **LLM review spend per PR / team** *(noergler)* | `SUM(cost_usd), SUM(prompt_tokens + completion_tokens) GROUP BY team` over the `noergler_pr_rollups` view, the newest cumulative rollup per PR (a declined PR reopened and merged emits twice; summing `noergler_events` double-counts it). Filter on `outcome='merged'` for "spend that actually shipped"; keep all outcomes for total LLM-review spend including abandoned PRs. Pre-priced — no multiplier needed. |
| **LLM review cost per KLOC** *(noergler)* | `SUM(cost_usd) / NULLIF(SUM(lines_added + lines_removed), 0) * 1000 GROUP BY team, outcome` over `noergler_pr_rollups WHERE repo NOT IN (<GitOps repos>)`. Diff-size normalised cost — fair comparison across small fixes and large refactors. GitOps repos out: a stage-promotion PR is a few lines at a full review's price (riptide reports its spend on its own line). GitOps repos as in the PR cycle-time query: those holding a commit `argocd_events.revision` points at. |
| **Wasted LLM review** *(noergler)* | `SUM(cost_usd) FROM noergler_pr_rollups WHERE outcome IN ('declined','deleted') AND repo NOT IN (<GitOps repos>)`. Review effort spent on code that never shipped. riptide's share divides it by all spend except stage promotions. |
| **CI compute time per pipeline / team** | `SUM(pipeline_events.duration_seconds) GROUP BY pipeline_name, team`. The unit metric for CI cost attribution. This raw SUM includes runs that waited a day for a Jenkins agent, and a single outage can outweigh a month of real builds. riptide's metrics (`internal/metrics`, shown in the UI's CI time) leave out runs over an hour and past 6× the median of their kind (and of their repo, given 3 runs) and report their hours on their own. |
| **Wasted CI** | `SUM(duration_seconds) WHERE status IN ('FAILURE','Failed')` — failed builds × time. Quantifies the cost of flakes / broken tests. riptide's metrics count `UNSTABLE` too and leave the same outliers out. |
| **Bot-driven pipeline churn** | `pipeline_events` joined to `bitbucket_events` via `commit_sha` filtered on `is_automated = true`. Renovate / Dependabot can drive 40–70% of pipeline runs in many orgs; useful input for batching policies. |
| **Deploy compute** | `SUM(argocd_events.duration_seconds) GROUP BY app_name, team, environment` — keep `environment` in the grouping to attribute prod vs. non-prod compute separately. |
| **Cost-by-change-type** | Group pipeline / argocd compute by `bitbucket_events.change_type` (joined via `commit_sha`): hotfix vs. feature spend, week over week. |

What riptide does **not** provide today, and the natural seam for it:

- **Currency.** Add a `unit_cost` config (per-runner $/sec) in
  `config/riptide.json`, or pull real per-namespace cost from
  **OpenCost / Kubecost** if it already runs in the cluster, and join on the
  per-source identifier (`pipeline_name`, `app_name`). Either is a follow-up
  component, not a v1 collector concern.
- **Cloud bill imports** (AWS CUR / GCP billing export) — out of scope for an
  enterprise self-hosted, on-prem-first product.

### Intentionally deferred

- **Change fail rate, deployment rework rate, failed deployment recovery
  time** (DORA's current names; MTTR is gone). All three hinge on
  knowing which deploy failed or answered a production incident; no
  reliable incident source today.
  Schema reserves room for an ArgoCD rollback proxy (revision N+1 <
  revision N within X hours) and a manual `POST /events/incident`
  endpoint as follow-ups.
- **Pre-aggregated metric tables.** Compute on read; only materialize when
  query volume justifies it.

Bitbucket and pipeline events join on the **commit SHA**
(`bitbucket_events.commit_sha` = `pipeline_events.commit_sha`, App-repo SHA on
both sides). Argo CD does not: `argocd_events.revision` is the GitOps-repo SHA.
Deploys reach the commit through the image reference — `pipeline_events.image_refs`
against `argocd_events.payload->'images'` — as described in
[Correlating deploys back to commits](correlating-deploys-to-commits.md).

## How the UI's numbers are computed

The backend computes every number the UI shows; the UI renders and formats,
it computes nothing. `GET /api/metrics?team=<team>&weeks=4|12|26&tz=<zone>`
returns the page's metrics over closed calendar weeks in the viewer's time
zone, the last one ending at Monday 00:00 of the week running now: deploy
frequency, lead times and their breakdown, code review, builds per kind
of run with their hotspots, and
noergler costs; each against the same number of whole weeks right before
(a second read of that window). The running week counts in none of them:
a week half over reads as a low week, and the assessment, which reads the
same closed weeks, would show other numbers than the tiles below it (same
zone, a fresh analysis: the same). A third read, Monday 00:00 to now,
gives the two things of now: the PRs open now (the page's one number of
now; compared with those open at the previous period's end) and
`running_week`, the week's releases, merged PRs and runs so far, counted
as the period counts them. What the page leads with is
`/api/analysis`, below.
`product=<key>` narrows it to one Bitbucket project,
bots' PRs never count. `GET /api/facts` takes the same parameters
plus `q` and returns the PRs, deploys and runs behind the numbers, the
period's and the running week's, newest
first (the PRs open now ahead of them, oldest first, with
their age and wait), searched; a row of the running week is marked
`running` (a PR by its merge, else its opening; never one open now) and
counts in no number. It reads no lead times or reviews, so it takes the product
as given, the one `/api/metrics` answered. Each number follows the rules of
the [Metrics](#metrics) queries above. `gitops`, `service_account` and
deploy `kind` are set from the data, never from name lists; `bot` adds
riptide's automation detection as it stands at read time, the same for a
PR's opener, its reviewers and a commit's author.

Each compared number carries a verdict and a `score`, its change's size
for the overview's strip: log2(now / previous), negated where lower is
better, clamped to ±1 (halving, doubling); from 0 to anything is ±1. A share
scores its points linearly, 25 points = ±1: a ratio near 0 % or 100 %
explodes (0 % to 3 % failures is infinite) on a change the 3-point tolerance
calls the same. A count (jammed repos) adds 2 to both sides first: one
jammed repo more is no doubling (0 to 1 scores 0.58, 10 to 20 still
0.87). A same verdict scores 0 (within the tolerance the change is
noise), no verdict no score; Argo CD silent for the team is
`not_reporting`, not too few. `directed` says whether the number
has a better direction at all, whatever the verdict (an undirected one with
too few data says `low_sample`); the strip draws only the directed ones.

Deploy frequency also comes per calendar day in the viewer's zone, the
period's weeks Monday first: each release on the day of its first deploy in
the period, so the days add up to the weekly bars. A day's shade is a
step, 0 for none to 4 for four releases or more. A day after the period's
end or before Argo CD reported is no data, not a day without a release, so
each weekday's releases are per whole day with data, never per calendar
day: today and Argo CD's first day show their releases but are left out of
the rate. Argo CD's data end is no cutoff: an outage and a team that did
not deploy both send no events.

Each lead-time stage carries its waits counted into `bucket_bounds_s`: 22
log-spaced buckets from 5 min to 28 days, the same for every stage (the
production wait runs to two weeks and more, so 14 days would pile it on
the last edge). A shorter wait counts in the first bucket, a longer one in
the last. Below the low-sample threshold the stage lists its waits one
by one (`durations_s`) instead of the counts (`histogram` null) and the ridgeline draws them as dots, not a
curve: a density from fewer than `min_prs` (10) values would be invented. The UI smooths the
curve through the counts; the counts, medians and p90s are the backend's.
Shares and the hot stage only when some PR reached production
(`reaches_prod`): without a production wait the medians add up to the time
to merge, and coding would read as most of a time to production nothing
reached. A product whose prod deploys predate its CI reports showed exactly
that; with it go the stage signal and the wait share.

Open PRs list the jammed repos first (3 PRs or more open 7 days or more),
then by long-open PRs, then by the median age; the top 8 and every repo
with a long-open PR. The jammed repos now are compared with the previous
period's end; more than then is a finding to look at, any at all a hint. A
long-open PR or two in a repo is no finding: a PR left lying, or one whose
close event never came. Inactive PRs (no event for 30 days) show as their
own count and last among the open PRs in the PR table. A Marimekko draws
the same repos in the same order: width the repo's open PRs, height the
share per wait, per wait how many are long open (`long_waiting`);
inactive PRs not in it.

Build hotspots compare each repo with its own runs of the same kind
(release, main, feature, other, unknown) in the previous period, never with
the team's: a kind mixes toolchains, and against the team a UI build with
Playwright smoke tests is always slow while a Gradle repo that doubled
hides. A repo whose median takes at least 1.5 times its previous one is
slow; one whose failure rate is at least twice its previous one and 5
points above it, with 2 failed runs or more, is failing more (at most
halfway from the previous rate to 100 %, or a repo failing most runs could
never be flagged); both from 10 runs in each period on. A trend, not
flakiness: a repo steadily red a third of the time is never flagged. A
repo new to the kind, or
without enough runs, or before the run data covers the previous period, is
not compared; the kind counts the repos that got slower or fail more instead
of comparing its mixed median. A repo steady at 25 minutes is never slow
against itself, so each kind also lists its 5 slowest repos by median,
outliers aside, from 3 green runs with a duration on, flagged or
not and without a threshold. Every repo row carries the wait for a build
agent and the build itself, as the kind does, over the runs that report
when they got one, and its CI time of the kind, counted as the kind's (the
rows add up to at most the kind's: no row without a run with a verdict,
and the list is cut). Above the table each repo is an arrow from its
previous median and failure rate to its current ones, the circle's area
its share of the kind's CI time; red only where the backend flagged it, no
arrow without both previous values (`prev_complete`). The repos it cannot
draw are counted (`unplotted`).
Each kind's heatmap gives those repo rows, plus one row for the repos past
the cut, a cell per week, bucketed as the kind's weekly rate: runs and
failures counted as in the row, outliers included, and a class the backend
picks: none under 3 runs (no verdict, hatched), 0 without a failure, then
under 10 %, 10–20 %, 20–40 % and 40 % or more. A repo failing more shows as a red
row, a shared outage as a red column. Starting values, not measured.
A run of a commit Bitbucket never saw counts with the repository its
pipeline is named after. A run succeeded on SUCCESS (Jenkins), Succeeded
or Completed (Tekton) and failed on FAILURE, UNSTABLE or Failed; anything
else (ABORTED, NOT_BUILT, Tekton's None) is neither a failure nor a run (a
superseded build is cancelled, not broken), in the hotspots as in the runs
per kind, where only its CI time counts. A Jenkins `timeout()` ends
ABORTED too, so a repo that keeps timing out shows as CI time, not as
failures. Failing repos rank by the CI time their
broken runs wasted. A run past six times its kind's median of green runs
and over an hour (and, with 3 green runs of its repo or more, six times the
repo's median) is
an outlier:
several repos stuck at the same time point at shared infrastructure, not at
a repo. Where the CI hours went splits each kind's time, outliers
included, into succeeded, flaky, failed (FAILURE, UNSTABLE, Failed), without a
verdict and outlier, no second twice: an outlier counts as one whatever its
verdict, so the first four sum to the kind's CI time and all five to its
time in `ci_flow`; a negative duration (clock skew) is no duration. Hours
add up across kinds; each share is of the kind's own time or of all of it,
never one kind's rate against another's. The same total splits by repo
across kinds, the 3 largest named and the rest folded, without a verdict:
more CI time can be more work. The thresholds are starting
values, not measured. The shapes are in
[`backend/api/openapi.yaml`](../backend/api/openapi.yaml).

A failed run is flaky when a later run of the same pipeline (same
`source` and `pipeline_name`) passed on the same code under another
`run_id`: red, then green without a code change. The same code is the
`head_sha` and `base_sha` a PR build sends when the CI built a merge of the
PR with its target branch (that merge commit is new on every build; the
same head on a moved target is other code), else the `commit_sha`; a head
without its base is never paired. Paired within a kind,
an unknown run with main (a default-branch run before the build type was
reported), never with a release: a release skips the tests a main build
failed. Counted in the red run's kind; later means a later start,
or a later end when a start is missing, never one's start against the
other's end. Runs
without a verdict between them neither pass nor break it; a COMPLETED
sent twice for one run is no rerun. Each kind counts its flaky runs, their
rate over the runs with a verdict and a commit (compared with the previous
period, lower better), their share of the red runs and their CI time, a
part of the wasted time; each repo row its own, shown, never a flag. The
facts mark each such run, searchable as Flaky. A lower bound: a retry
inside a run (Jenkins `retry`, Tekton task retries), a commit built once,
a rerun after the period's end and a CI merge commit sent without its
`head_sha` and `base_sha` all go unseen. A run that failed before its checkout has no
commit: a red run, never flaky, counted apart (`unjudged`), and a sender
must still report it, or the infrastructure failures vanish. A sender that
starts reporting them adds red runs from that day on: the period across it
fails more against the previous one with nothing changed in the builds. Whether the tests,
the infrastructure or the network failed is not in the data. Not
measured on a real import yet.

### The overview's assessment and its LLM wording

`GET /api/analysis` (same query) is the overview's "what now". It covers
the last *closed* weeks (`BACKEND_ANALYSIS_TZ`, never the running week)
against the same number before, as the page does in the viewer's zone;
its open PRs are those at the period's end, the page's those open now. `metrics.Assess` raises the signals:

- a jam (4 PRs open 7 days or more in a repo; attention when it was jammed
  at the previous end too, or notably more repos are: 2 or more);
- the lead-time stage with the longest median (attention from half the
  time to production on 10 PRs, else a hint), and a repo slower than its
  own previous period (1.5 times its median, as the hotspots flag it);
- a steady slow build: among a kind's 3 slowest repos in both periods and
  slower than the kind's median. No factor, since a kind mixes toolchains;
- repos failing more, as the hotspots flag them;
- a change of a directed team number: any gain past the tolerance (10 %,
  3 points for a share), a worsening only past 25 % (5 points; a count
  damped by 2 and moved by at least 2: one jammed repo more is no news), the review
  time's worsening split by repo. A small gain is evidence of what runs; a
  small worsening would fill the stuck side with noise (user, 2026-11-18).
  That covers deploys, lead time, review, within-day, jammed repos, build
  success per kind, merged PRs per week, open PRs over that throughput
  (weeks of work standing open) and the waiting part of the lead time.
  Gains rank by the size of their move;
- a moved bottleneck, deploy frequency and success moving opposite ways
  (then the better one of the two is no gain: more throughput with rising
  instability is no progress), agent waits of half a run or more, several
  repos stuck at once, forgotten PRs, re-syncs over half the syncs;
- what the latest analysis of the window raised and is gone now; a
  worsening that turned into a gain shows as resolved only. Gone means
  judged and no worse: a number unknown now, or a build repo with too few
  runs to judge (10, a steady slow one 3 timed), resolves nothing.

The layout puts the gains and resolutions on the left ("runs") and the rest
on the right ("stuck"), with a tally of each. Gains rank last, so they get
slots of their own (the best 2 gains, 1 resolution) on top of the 4 cards,
worded or not. The best gain and resolution always keep theirs; a lesser
one the model worded shows beside them, past the slots if need be, since
worded text is never dropped (nor where a focus repo carries its signal).
Without a gain or a resolution the left says
"unremarkable": a quiet period is no success (user: "wenn nichts läuft, ist
läuft gut übertrieben"). A change of deploy frequency or of a kind's build
success carries its weeks in both periods (`series`), the numbers the
deploy and build sections count per week anyway; review time, lead time and
merged PRs have no weekly series, so their cards draw none.

A source silent for 7 days at the end is a gap: its signals are left out.
The repos are ranked by their signals' weight; a jam or a steady slow build
alone makes a candidate, and repos without a signal stay out of it. The
question is the first undirected number that moved notably, in a fixed
order (feature share, untracked, wasted share, cost per kLOC, open PRs,
approvals without a comment), else the approvals without a comment when
there are approvals and Bitbucket is no gap: a conversation starter, not a
finding (a PR that needs no change gets approved without one). Starting
values, user-chosen 2026-11-16, not measured.

The model (`BACKEND_ANALYSIS_MODEL`) words this and nothing else:

- it sees repos under an alias;
- it writes numbers only as `{{fact id}}`, which the page fills in;
- it picks levers only from the glossary, and none presumes a cause riptide
  cannot see.

The validator drops what breaks a rule: a digit, an unknown fact, a
foreign lever, a sharp s, a repo outside the candidates or an overlong text.
It lists the mandatory signals the text missed (the top 3, and the
strongest gain, which ranks last); the page shows a missed gain or
resolution in its slot with the rule text, the rest as "also spotted". The schedule words every team's views, all products and then each product with data, after its week
closes, Monday 06:00: 4 weeks weekly, 12 every second week, 26 monthly.
The page shows the newest analysis of the same window while it is no
older than its schedule's gap; without one, the same signals with the rule
texts.
