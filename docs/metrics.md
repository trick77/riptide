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
the ones whose commits Argo CD deploys, so no list of names is needed. A
commit counts whether Bitbucket saw it pushed or only as the merge commit of
a PR merged in the UI: Bitbucket sends no push for that merge, and a GitOps
repo promoted only through PRs would otherwise never be recognised. The
payload keeps the sender's casing; revisions are lowercased:

```sql
-- GitOps repos: their commits are what argocd_events.revision points at,
-- pushed or a PR's merge commit.
gitops_repos AS (
  SELECT DISTINCT b.repo_full_name
  FROM argocd_events a
  JOIN bitbucket_events b ON b.commit_sha = a.revision
  WHERE b.repo_full_name IS NOT NULL
  UNION
  SELECT DISTINCT b.repo_full_name
  FROM argocd_events a
  JOIN bitbucket_events b
    ON b.event_type = 'pr:merged'
   AND lower(b.payload->'pullRequest'->'properties'->'mergeCommit'->>'id') = a.revision
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
| **Deployment frequency** | Releases per `team` / time window in prod. A release is a successful deploy whose images differ from the same App's previous successful deploy naming images, counted once per GitOps `revision` — see the query below. A deploy naming no images deployed none: a sync, never compared, and never an App's first-seen deploy. Not every sync is a release: a config change or a manual re-sync redeploys the same images (measured: a third of all intg syncs, and 7 of 8 syncs of one monitoring App), and one release reconciles every App sharing the revision (intranet + extranet), so `COUNT(*)` overcounts twice over. Report the release count next to the syncs and the first-seen deploys it was derived from, so a reader can see where the number comes from. Group by `app_name` for the per-App drill-down. riptide adds three rules the query below leaves out, see after it: an App's first deploy counts as a release once Argo CD has reported for the team longer than 14 days; infrastructure Apps count nowhere; a deploy's product is the Bitbucket project of the GitOps repo its revision is a commit of. |
| **Change lead time** | Per PR, not per commit and not per release (owner's decision, 2026-11): the people's PRs merged in the period that reached production by its end, each from its first commit (the oldest commit pushed to its branch, else its opening) to the **first** production deploy that shipped it, so a release rolled out to four Apps counts each PR once. The placement, routes and lower bounds are [Change lead time (DORA)](dora-lead-time.md)'s; the basis ("aus n PRs") and how many of the period's merged PRs were placed, not yet on production or not placeable stand next to it (`coverage`). Bots' PRs count nowhere, library repos' PRs are outside the base. A closed period is frozen at its end: a PR shipped after it was not on production then. Do **not** substitute the newest commit per release: it answers "how stale was the freshest change" and reads far too fast. |
| **Lead time breakdown** | Where the lead time is spent, per human PR in app repos, as five stages: coding (oldest commit on the branch → PR ready), review (ready → approval), merge (approval → merged), to intg (merged → the PR's first intg deploy), to prod (intg → prod). One stage for review, not pickup + review: measured, 95 of 115 approvals were the reviewer's first and only touch, so "first review → approval" is zero by construction; the count is reported as a review-depth signal. Stages 4–5 reach only PRs placed on a deploy before the period's end, the change lead time's placement, so they cover fewer PRs than 1–3 — report the count per stage. The query below reads stages 4–5 from `lead_time_changes`, which knows the exact compare-link route only: on data where the other routes place PRs it reaches fewer. |
| **PR cycle time** | `pr:merged.occurred_at − pr:opened.occurred_at` per PR, keyed by `(repo_full_name, pr_id)`: PR ids are numbered per repository, so `pr_id` alone joins unrelated PRs. A PR opened before ingestion started has no `pr:opened` and drops out. Exclude GitOps repos: their PRs are stage promotions (see [Reading the event tables](#reading-the-event-tables)). Merged PRs only: a PR still open is in no cycle time, however long it waits — see Open PR age. |
| **PR throughput** *(Flow Framework "flow velocity")* | Human PRs merged per week: the merged PRs of the cycle-time row (GitOps repos and bots out) over the period's weeks, or over the weeks since Bitbucket's first event when that lies inside the period (as deploy frequency does for Argo CD), and per week by branch prefix. A PR opened before ingestion started counts: only its merge is needed. A PR is a PR wherever the work happened (owner, 2026-11): one merged into a feature or umbrella branch, not the repo's trunk (`pullRequest.toRef.displayId`; the trunk is master, main, or the branch most of the repo's PRs target, every branch tied for most), and one Bitbucket marked merged without a merge commit of its own in the second another PR of the repo merged (an **implicit merge**: an umbrella branch brought its commits in) are merged work, marked in the facts as merged via an umbrella branch. Two kinds of merged PR are no merged work and are left out of this and every other PR number, each counted under its first reason so the page can say how many: the **umbrella** itself, the vehicle of other people's merged PRs, whose work counts on them: a PR whose source branch is the target of other merged PRs of the repo, over its whole history, or whose merge implicitly merged another person's PR (a bot's PR never makes a person's uncounted, either way); **housekeeping**, a tool's change: a person's PR whose title as opened (trimmed, case and inner spaces aside) was opened by people in at least 3 of the team's repos within an hour, without a person's approval (a bot's, a service's or a declared non-human account's approval is none) and without a ticket key in its title or source branch (owner's decision, 2026-11; how long it stood open says nothing, some of one batch are merged at once, others days later). The openings are the team's of all history, not the window's PRs: a batch opened across a period's edge is housekeeping in both periods. A PR marked merged when an umbrella went in (an implicit merge) whose source branch another merged PR of the repo, counted, came from is the same work and counted once (**twice**: one PR into the umbrella branch, a second of the same branch into master). The umbrella's openers and the siblings' are told bot or person as every PR's opener is, riptide's automation detection at read included. A PR of a **library** repo is merged work, but out of the base of the PRs traced to production (`coverage.to_prod`): a repo that is never rolled out, on positive evidence (owner, 2026-11: a library PR must never read as not traced, nor as a missed release). Over all history: no deployed GitOps release commit names its slug in a compare link, no run of it built an image (deployed or not yet: a repo building one is deployable, in doubt), and no image Argo CD deployed for the team has a path segment equal to its slug (a deployable repo's image is named after it, even when its CI does not report; the tag and digest of the last segment aside, a registry's port is no tag), while the team's tracing works at all (a compare link names some repo, some run's image Argo CD deployed, by its full reference). It needs no build: a starter or client library whose CI does not report is one. A repo one of whose PRs in the window reached any stage by the window's end is none, whatever the evidence (a deploy after a closed period's end does not reclass it). The rule's residual: a deployable repo whose CI does not report, whose images are named otherwise (one service, several images of other names) and whose release commits name no compare link reads as a library. A view whose counted PRs are all libraries' says so (`libraries_only`): nothing to trace, no release missed, no limit. The weekly rate is null when the weeks since Bitbucket's first event are under one: three PRs in six hours are no 84 a week. Per team, never per person. More is better, read next to the batch size: more PRs of the same size is more work done, more because they got smaller is the same work in better portions. |
| **Batch size** *(DORA capability "working in small batches")* | Changed lines per merged human PR, added plus removed, median and 90th percentile, and the median per week. The size is the PR's final rollup in `noergler_pr_rollups` (`outcome = 'merged'`), joined on `pr_key` = `lower(repo_full_name) || '#' || pr_id`: Bitbucket DC webhooks carry no diff size. noergler counts the whole PR diff at close, over the files it reviews only, so lock files and vendored bundles are out. No size: a PR noergler never reviewed (no rollup), and a rollup counting 0 lines (noergler could not fetch the diff at close and had no run to fall back on). The median, not the mean, which one large PR decides. Smaller is better. |
| **Open PR age** | The PRs open at an instant T (the period's end), per repo, with their age and what they wait on. Open at T: no `pr:merged` / `pr:deleted` before T, the PR's latest event before T is no `pr:declined`, and the latest event carrying a `pullRequest.state` has it `OPEN` (none does: open unless declined). Every PR event carries the PR, so a declined PR reopened is open again on its next event. Only events before T count, so the previous period's end reads what was open then; a PR open all along that riptide first heard of after it counts there too, else it would read as a new long-open one. Age: T − its last turn ready, the first event after the last one whose `pullRequest.draft` was true, whether or not the parser recognised the flip; never seen a draft (opened ready, or made ready before ingestion) T − `pullRequest.createdDate`, which every PR event carries, so a PR opened before ingestion has one too. Time as a draft is its author's, not the team's: a long draft just made ready is no long-open PR (user, 2026-11-20). A draft has no age and is in no median. Waits on, the first that applies: draft; changes (a human `NEEDS_WORK` in the latest event's `reviewers` or `participants`, Bitbucket's own status, approvals reset by a push included); merge (a human `APPROVED` there); review (a reviewer touch as in the pickup-time query); first review. Long open: 7 days or more; a repo with 4 long-open PRs or more is jammed (both chosen, not measured; the jam was 3 until 2026-11-18). A draft is never long open and never inactive: it waits on its author and may lie as long as it wants (user, 2026-11-18); it stays open, waiting on "draft". Draft is the latest event's `pullRequest.draft`, so a ready PR turned back into a draft is stored as a synthetic `pr:back_to_draft` (else it read as ready until its next event). Per repo the median age of its ready PRs, never the oldest PR's, which one PR alone decides. Exclude GitOps repos and bots as for cycle time. Inactive: no event of any kind on the PR for 30 days before T (for a PR riptide first heard of after T, since its creation); counted apart, in no other number. Webhook-only data cannot tell a PR whose close event Bitbucket never delivered from one nobody touches, and such losses recur: a lost one counts as open until it goes inactive, and alone never makes a jam. Where the loss is evident it is said: a PR whose latest event came before a Bitbucket silence with unseen runs (see [Sources and silences](#sources-and-silences)) and that has had none since is of **unknown** state, counted apart, in no open, long, jam, wait or WIP number, and listed in the PR table as unknown. Its state is never guessed from the review bot's rollup: noergler never decides a PR's lifecycle. A PR without any event since ingestion is missing. |
| **Time to first review** *(DX Core 4 "code review pickup time")* | Two-part computation per PR — see the SQL block below the table. Clock-start = `COALESCE(pr:ready_for_review, pr:opened)`: PRs opened ready start at `pr:opened`; PRs opened as drafts start at the synthetic `pr:ready_for_review` (emitted by the parser when a `pr:modified` payload carries `previousDraft=true, draft=false`). Engagement = first reviewer touch (`pr:comment:added`, `pr:reviewer:approved`, `pr:reviewer:unapproved`, `pr:reviewer:needs_work`, `pr:reviewer:updated`) where `lower(author) != lower(pr_opener) AND NOT is_automated AND occurred_at >= clock-start`. The five-event reviewer union covers every touch Bitbucket DC emits (silent approvals, retracted approvals, "needs work" flips, bare reviewer-status changes); the `occurred_at >= clock-start` guard drops early-feedback comments solicited during the draft phase, which would otherwise produce negative pickup times. `NOT is_automated` strips bot comments (noergler / Renovate / etc.) — every review-time bot must be recognisable, otherwise its instant comment drives the metric toward zero. Detection matches the `automation` config block against **both** the login handle (`author`) and the display name (`author_display_name`), because a bot is often provisioned as an ordinary user account whose login says nothing. `is_automated` is decided at ingest, so rows written before a bot was recognised stay marked human: the `non_human_identities` CTE in the query below filters those at read time, on both names too, from the accounts senders declare (`reviewer_handle` + `reviewer_account_kind` on noergler rollups, `actor_handle` + `actor_account_kind` on pipeline events) — riptide stores the declaration, it carries no account names of its own. A declared handle is matched against both the login and the display name, so it must never equal a person's display name: that person's reviews and PRs would drop out as a bot's. |
| **Build success rate** | `pipeline_events` with `phase = 'COMPLETED'` grouped by `status`. Slice by `source` to compare Jenkins vs Tekton, by `pipeline_name` / `team` for ownership. |
| **Build duration** | `pipeline_events.duration_seconds` (a Postgres `GENERATED ALWAYS AS (finished_at − started_at)` column). Every duration median, spread and comparison is over green runs only: a red run stops wherever it failed, so mixed in it makes a repo that fails early read fast (user, 2026-11-18: "stupide"). Red runs count in the failure rate and their time in the CI time in red runs, never in a duration; an outlier is any run past the cut, red or green. |
| **Deploy success rate** | `argocd_events` with `operation_phase IN ('Succeeded', 'Failed', 'Error')` aggregated, filtered to `environment = 'prod'` for the prod-only view: Failed and Error both count as failed (the notification template sends either for a sync that deployed nothing). Infrastructure Apps left out, as for deploy frequency. Only what Argo CD reported: a sync whose notification was lost is in no count, so the rate is no proof of completeness. |
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

riptide counts three things the query leaves out (owner's decisions,
2026-11):

- **A first deploy is a release after the grace.** An App's first deploy
  naming images more than 14 days after the team's first Argo CD event
  (over all history) is an App going live: it counts as a release
  everywhere one counts (frequency, weekly bars, calendar, running week,
  comparison) and stays marked as the App's first deploy in the facts.
  Within the 14 days it is the onboarding's first look at an App deployed
  long before: `first_seen`, release or re-sync unknown, as in the query.
  Without the rule an App going live weeks after collection began kept the
  count at no release for good.
- **Infrastructure Apps count nowhere.** A deploy's product is the
  Bitbucket project of the GitOps repo its revision is a commit of, pushed
  or a PR's merge commit (see [Reading the event tables](#reading-the-event-tables)).
  An App none of whose successful deploys over the 26 weeks before the
  window's end names a product, one of them after the team's first
  Bitbucket event, is infrastructure (a log shipper, a dependency bot):
  its releases, re-syncs and failures are in no number the team is judged
  on, only counted apart (`infra`, `infra_apps`) and listed in the facts.
  Never when the team has no deploy with a product at all (its GitOps repo
  sends Bitbucket nothing, and every App would drop out), and never an App
  whose first deploy, counted as a release, came while Bitbucket's events
  were lost or within 7 days after such a hole closed (a lost silence, see
  [Sources and silences](#sources-and-silences)): its GitOps commit may lie
  in the hole, synced to production days later (a hole ending Friday, the
  deploy on Monday). A new App going live after an outage is a release
  without a product. The page and the analysis class the period and the
  previous one by the Apps as of the period's end, the running week by its
  own: one App is never infrastructure in one period and a product's in
  the other, which would move the comparison by bookkeeping, and the
  page's tile says what the stored assessment says. A revision an
  infrastructure App and another App both deployed is the other App's,
  counted there only.
  The class is derived from the data, never configured (owner, 2026-11:
  the real infrastructure Apps must stay out without config work). Its one
  false positive: a team with a second GitOps repo that sends Bitbucket no
  webhook. None of that repo's commits names a product, so every App
  deployed from it is listed as infrastructure on the deploy tile
  (`infra_apps`) and counts in no number. The remedy is the webhook on
  that GitOps repo; from its next commit on, its Apps have a product again.
- **An App does not change products.** A deploy whose revision names none
  (its GitOps commit fell into an outage) is its App's product's when the
  App's other deploys name one, the one most of them name.
- **A deploy without a product is not "no deploys".** A deploy of an App
  with no known product counts in the team view and in no product view;
  `unattributed` counts them team-wide in every view, so a product showing
  none can say they may be why.

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
  -- as in "Reading the event tables": pushed or a PR's merge commit
  SELECT DISTINCT b.repo_full_name
  FROM argocd_events a
  JOIN bitbucket_events b ON b.commit_sha = a.revision
  UNION
  SELECT DISTINCT b.repo_full_name
  FROM argocd_events a
  JOIN bitbucket_events b
    ON b.event_type = 'pr:merged'
   AND lower(b.payload->'pullRequest'->'properties'->'mergeCommit'->>'id') = a.revision
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
  UNION
  SELECT DISTINCT b.repo_full_name
  FROM argocd_events a
  JOIN bitbucket_events b
    ON b.event_type = 'pr:merged'
   AND lower(b.payload->'pullRequest'->'properties'->'mergeCommit'->>'id') = a.revision
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
| **Change mix** | Distribution of `change_type` (feature / bugfix / hotfix / chore / refactor / docs / other) per team per week, over `pr:opened` / `pr:merged` rows only — a push to `master` has no prefix to classify and would swamp the distribution with `other`. Exclude GitOps repos: stage-promotion PRs carry no prefix either. A branch named after its ticket without a prefix (`KAS-12-login`, `kas-12`) is a feature, on read (stored as `other`), and its CI runs are feature builds; a dash scheme of a release line, a dependency bot or Jenkins' `PR-<n>` is not. |
| **Tickets per deploy** | `COUNT(DISTINCT unnest(jira_keys))` per deploy — small-batch indicator. Jira keys are extracted at write time from PR title, description, branch name, and commit messages via regex `[A-Z][A-Z0-9]+-\d+`, deduplicated, GIN-indexed. |
| **Untracked-work rate** | `COUNT(*) WHERE jira_keys = '{}'` over merged PRs outside GitOps repos — process-compliance signal. |
| **Per-ticket flow** | `WHERE 'ABC-1234' = ANY(jira_keys)` returns every event for a ticket across Bitbucket / pipeline / Argo (joined via commit_sha). |
| **Human vs automated split** | `WHERE NOT is_automated` (Renovate / Dependabot / Snyk / Mend / generic-bot detection runs at write time and tags `automation_source`, and again at read time on the PR opener and the commit author; besides the configured authors, the branch prefix and the name shape `*-bot`, `*[bot]`, `bot-*`, an account whose login or display name holds a configured automation source's name as whole words (`renovate_team`, "Renovate Team") is that bot, so a dependency bot provisioned as an ordinary user is caught on its direct trunk pushes too, which then count as bot commits in the lead time. That name rule applies at read time only, never written into `automation_source`: a config that guessed wrong is corrected by fixing it. A source whose name is an ordinary word would catch people with that word in their name: name sources by their tool (a name under 4 letters is warned of at load, `automation_source_name_short`); an account the git host itself marks as a service account — Bitbucket DC's built-in system user, which files the default PR tasks — is tagged `service-account` with no config entry), plus the `non_human_identities` filter for accounts a sender declared. Keep `bot` and `service` apart when reading: a **bot** authors work of its own and its velocity is worth its own view, while a **service** account (a CI user pushing merges) authors nothing and should simply not appear in human activity. Default dashboards exclude both. |
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
- **Builds that start and never report an end.** The data holds such runs;
  no state names them yet.

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
`/api/analysis`, below. What every window of one request reads alike is
read once for all of them, first, and kept for that request only: the
GitOps repos, the silences, the Apps' classes, the library evidence and
the tracing (every deploy's stretch of its repos' trunks, all history: each
window's PRs are placed on the deploys that shipped them, and a closed
period reads only the deploys before its end; see
[Change lead time](dora-lead-time.md)).
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
that. No assessment signal reads the shares (some stage always takes
longest); the flow section still marks the largest stage.

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
A run's repository is the one whose Bitbucket events carry its commit,
else the one other runs of the same pipeline have. A run of a commit
Bitbucket never saw (a push lost in an outage) takes the repository its
pipeline is named after: a Jenkins multibranch job is
`<repo-slug>/<branch>`, and when the first segment is the slug of exactly
one repository the team is known for (by its Bitbucket events or another
run), the run is that repository's. Only a name of exactly two segments:
Jenkins encodes a `/` inside a branch, so a third segment is a folder's
job, and a folder named like a repository is none. Two projects with a repo of that name,
or none, leave the run without one: in the team view's numbers, in no
product's, and counted (`unattributed`) so a product view can say so.
A run succeeded on SUCCESS (Jenkins), Succeeded
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

### Trends and the numbers per repo

`trends` carries three numbers week by week across the previous period, the
period and the week running now (`split` marks the period's first week,
`running` the last): the people's merged PRs and their typical size as
`throughput.weeks` counts them, and the releases to production as
`delivery.frequency.weeks` does. A week is a gap, null with a reason, never
a zero: `no_data` when the series' source started after the week's start
(Bitbucket for merged PRs; Bitbucket and noergler for the size; Argo CD for
releases), `lost` when a lost Bitbucket silence overlaps the week's working
time (one reaching into a Sunday took no merges), `none` when a week has no
PR with a size. Built from the sections the request computes anyway, no
read of its own.

A team number pools busy and quiet repos: nine repos releasing once a
month and one shipping five times a day read as "twice a week". `repos`
sets the period's numbers out per repo of the view, each repo with a merged
PR in the period or the previous one, from the rows the request already
holds (both periods' PRs, tracing's releases, the build section's runs per
repo):

- A repo's releases are its Apps' releases to production that tracing does
  not show leaving it unchanged, read as tracing knew them by the period's
  end, each GitOps revision once; an App's first deploy within the grace
  after Argo CD's first event counts as no release, as the team's frequency
  has it, but its last release was then or before. Its last one against the team's
  `release_hint_days` and `release_breach_days` puts it in a group: rolls
  out regularly, rarely, or not at all (`none`, the sprint rule broken). Not
  judged: a library (never rolled out), a repo nobody works on (no counted
  merge since its last release nor within `release_breach_days`, as the
  release state counts a project active), and, as `unjudged` with a reason,
  one no App is known to deploy, one only infrastructure Apps deploy, or
  Argo CD not reporting. Never judged as regular for want of data.
- The wait from the trunk to production, the typical pickup to the first
  review and a build kind's typical duration and failures are the states'
  own numbers (`slow_to_prod`, `first_review_wait`, `long_builds`,
  `release_failing`), the same helper and the same minimums: a repo under
  them is counted as too few (`thin`), never placed on a strip. Batch size
  per repo needs 3 PRs with a size.
- A previous value per repo only where its sources saw the previous period
  whole, as the comparison holds.

The ranking orders the groups none, rare, unjudged, regular, library,
idle; the longest without a release first, within regular those past a red
threshold first, then the slowest to production. It opens 8 rows, or all of
them when fewer than 3 would stay hidden, and names the first 5 hidden per
group.

Each strip (`spreads`) carries the repos' values at positions 0..1 with the
better end at 0, on a scale fitted to the values and the team's threshold:
a share from the nearest 10 % below its lowest value to 100 %, values
spanning a factor of 8 or more on a log scale (one daily repo would squeeze
the rest into one spot), else a linear one; ticks on round values of the
unit. Each dot's verdict is against the team's threshold where the number
has one (`breach` where its state is the rule broken, `hint` where it is a
hint), `neutral` where it has none. A repo that released nothing in the
period while not rolling out (`none`) is set apart at the strip's end, not
placed at zero. Placed repos at one value only make no strip (every dot on
one spot reads as a picture of nothing): `same` carries the value for a line
instead. The typical repo is the median of the placed repos. A note says what the team's number hides,
from 3 repos with a value on: one repo carrying half of an added-up count or
more (`dominant`), repos more than twice or under half the typical one's
value (`far`), or the team's number itself that far from the typical repo
(`pooled`: it adds the repos up, or counts every case so the busiest repos
decide it). Starting values, not measured.

### Sources and silences

What the data covers is computed from the data, never from a list of dates.
Per source (Bitbucket, the CI, Argo CD, noergler) the meta carries its
first event over all history (`data_start`) and its latest (`data_end`),
noergler's first event per repository the window's PRs name
(`repo_starts`: it is set up repo by repo), and the **silences**: spans of 2
working days or more in all (Monday to Friday, UTC) in which one source
stored nothing for the team while another stored events, every one that
overlaps the window: 36 working hours on each side of a period boundary
are one silence in both periods, and one the window ends inside counts
with its closing event after the end (up to the read's now). A silence
still running at the read is one too, from the source's last event to now
(`ongoing`), when another source stored events since: an outage running at
Monday's scheduled read is a hole then, its open PRs unknown, its counts
uncompared, not only once its closing event comes. Silent 7 days at the
period's end, it is that gate's limit (`silent_end`), named once. The
page names it with no end ("seit"). The instants are when
riptide stored the events (`created_at`), not when they say they happened:
a sender's retry storing an old event days late, dated inside a hole,
would otherwise split it or erase it. That is a literal reading: a team
that does not deploy or review for two days shows an Argo CD or noergler
silence too. Only Bitbucket's can be told apart: `unseen_runs` counts the
CI runs in the span that built a commit Bitbucket never saw (pushed, a
PR's source head, or the merge commit of a PR merged in the UI, which
sends no push), of a pipeline whose other commits it did see. Above 0 the
silence is `lost`: Bitbucket's events were lost, not the team idle. The
page's data line names each source's start and latest event and the lost
silences; any other silence is left out there, since it reads the same as
a team that simply did not push, build or deploy. While a lost silence
touches either period, no Bitbucket number is compared, on the page or in
the assessment; in a product view the deploy and build numbers neither,
which reach a product through Bitbucket's commits, nor a repo's build
hotspots (no repo is flagged slower or failing more, nor judged on its
builds).

Two holes the stored time cannot see:

- **riptide itself down.** When no source stores anything for 2 working
  days or more (riptide or its database down, the ingress broken), no
  source was silent while another reported: no silence, though
  Bitbucket's events of those days are lost for good (it does not retry).
  The data line shows every source's latest event; a gap common to all is
  only visible there.
- **Replays and imports.** A replay or backfill that later stores the
  events of a hole does not close it: it stays `lost`, since the events
  came after its end, by stored time. A bulk import that does not keep
  `created_at` (every row stored at the import's instant) shows no silence
  in the imported span at all, and a hole in it is missed.

Where a number rests on part of the merged PRs, the page says how many of
how many (`throughput.coverage`, of the people's merged PRs): traced to
production (`to_prod`, the time to production and the breakdown's last
stages; `delivery.lead_time.unmeasured` of them have no start, neither a
pushed commit nor an opening, and are in no time), with a size (`sized`,
the batch size), seen opened (`opened`, the cycle time). A PR missing
there is missing from that number, not a PR that took no time.

### The overview's assessment and its LLM wording

`GET /api/analysis` (same query) is the overview's "what now". It covers
the last *closed* weeks (`BACKEND_ANALYSIS_TZ`, never the running week)
against the same number before, as the page does in the viewer's zone;
its open PRs are those at the period's end, the page's those open now.
Only what the team itself causes and can change is judged: infrastructure
Apps and the builds of bot branches and bot PRs never raise a finding and
enter no judged number (owner's decision, 2026-11; a failed dependency-bot
build is no quality problem of the team, only its CI time is). A bot's
build is flagged as the dataset is read: its branch starts with one of the
config's automation `branch_prefixes`, or it is the PR-<n> build of a bot's
PR. It is in no build number of the page either (success, failures,
durations, hotspots, flaky, CI time), counted apart (`build.bot_runs`, runs
and CI seconds). `metrics.Assess` raises the signals:

- a jam (4 PRs open 7 days or more in a repo; attention when it was jammed
  at the previous end too, or notably more repos are: 2 or more);
- a repo slower than its own previous period (1.5 times its median, as the
  hotspots flag it), and repos failing more, as the hotspots flag them;
- a change of a directed team number, better or worse alike only when it
  moved notably: 25 %, 5 points for a share (owner's decision, 2026-11;
  until then a gain counted from 10 % / 3 points, so 1 release to 2 read
  notably better). A count is damped by 2 and must move by at least 2. A
  share over fewer than 20 cases in either period must move by 2 cases or
  more (one case is over 5 points there); deploy frequency on fewer than 10
  releases must move notably in its release count. The review time's
  worsening is split by repo. That covers deploys, lead time, review,
  within-day, jammed repos, build success per kind, merged PRs per week,
  the changed lines per merged PR (batch size, smaller is better) and open
  PRs over that throughput (weeks of work standing open). Gains rank by the
  size of their move;
- agent waits: half a kind's typical build or more, and at least two
  minutes, waiting for an agent (a hint; the page words the rule with the
  numbers `build.agent_wait_share` and `agent_wait_min_s` send);
- what an earlier analysis of the window raised and is resolved now, on
  evidence only: today's number back at or better than the value before
  the problem was raised (the stored signal's previous-period fact, from the
  earliest analysis that raised it since its last resolution, 8 weeks of
  history, analyses of older rules not read: they counted otherwise),
  never because the baseline drifted to the bad level. A jam resolves only
  at 2 PRs or fewer still holding it, clearly under the jam: its long-open
  PRs, and those every open number leaves out since, of unknown state (last
  heard of before Bitbucket's events were lost) or inactive for 30 days; a
  jam whose PRs only fell silent is not resolved. A number unknown now
  resolves nothing. A resolution names no next step.

Dropped in rules version 2, never to be raised again on the same premises:
the longest stage (some stage always takes longest, and its share was a
ratio of medians over different PRs), a moved bottleneck (a near tie flips
on noise), throughput against stability, re-syncs without new images (they
were infrastructure Apps', not deploys that ship nothing), a steady slow
build (the slowest three are always someone), forgotten PRs and several
repos stuck at once (each fired on a single case; the open PRs and the
build hotspots show them as numbers), and the waiting part of the lead
time. Stage shares stay plain numbers in the flow section.

#### States (rules version 3)

A state is a finding that stands without a previous period: a team's own
agreement broken, work held up now, what lasts, what the data cannot judge.
Signal kind `state`, its card in `signal.state`. Derived from the data on
every assessment, never from stored analyses (a view without a model
judges the same); a stored analysis shows the states it was
assessed with, frozen like its other signals, so its words and cards agree
(a closed period changes only by late events); judged at the period's end per
unit, a project, a repo, an App or a bot branch, never pooled across busy and
quiet units; summed up on one card per state: `count` of `judged` units,
the worst three with their number (`worst`), how many more (`more`), every
unit for the facts (`units`), since when the state holds (`since`, the
earliest unit's; a lower bound where the data starts there), and the
threshold measured against (`threshold`, a key of `riptide.json`'s
`thresholds` block, with a second count `min` and a span `within` where the
state has them, all printed on the card; null for data honesty, which
measures against no agreement). `value_unit` says what a unit's `value` is
and `names_kind` what its `names` are (a build kind, a stage), so the page
formats without deciding. A unit with nothing to judge (no work in the span) is
`idle`; one without the data to judge is `unjudged`, never "ohne Befund".
Each unit names the facts search that lists its rows in the card's facts
table (`query`, the `/api/facts` `q`): a repo's or an App's name, the App of
a project's last release (the deploy table holds no project), `<project>/`
for a project's PRs, a branch's pipeline name (the runs table knows no
`<repo>:<branch>`); none for a team-wide unit or branches without a PR,
whose card has no facts table. The facts search lists a merged PR by where
it stands on production in the column's words ("auf PROD", "noch nicht auf
PROD", "nicht zuordenbar" with its reason, "höchstens" on a time through a
lower bound, both languages); exactly those words list exactly those PRs.
The facts read as of now, within the facts window: a card judged at a
closed period's end over the look-back may count a PR the link no longer
lists (shipped since, merged before the window), so the page words the
link as today's rows, never as the card's set.

Eligibility, shared by every state: bots' PRs, branches and builds,
infrastructure Apps and GitOps repos are never judged; library repos are
work (merges, review, size, branches) but never judged for delivery; a
source silent at the period's end judges nothing of its states (its units
read unjudged). Look-back: at most 26 weeks before the period's end
(`StateLookbackWeeks`), in every view (a 26-week view reads its previous
26 weeks too; no state looks into them); the analysis reads the weeks
before the compared periods for it (PRs and deploys only), the approvals
from 30 days before the look-back's start and the branches' pushes from it,
once per request, whatever the view's length. A thresholds value past what
the look-back holds is refused (`thresholds` bounds: days 182, working days
130, minutes 1440, weeks 26; `silent_builds_days` 15, what the runs of the
shortest view hold). Whose a branch is is read on every page: a bot's when
every push is a bot's (flagged at ingest, its head commit by a git identity
of the `.invalid` e-mail domain, or its pusher or head commit automation by
riptide's detection, the config's automation `authors` included), never
because of its last push alone. Such a branch's builds are bots' builds in
every number.

Tones and groups: `attention` red ("Hakt"); `hint` a neutral group of its
own ("Hinweise", never red); `limit` what cannot be judged ("Nicht
beurteilbar", no finding: it adds the quiet reason `unjudged`). A lasting
state is a hint until a unit has held it two sprints (28 days), then red:
held since its onset where the data tells it (when the Nth approved or
ready PR had waited past the threshold, when a branch without a PR passed
`branch_open_days`), else firing at each of the four weekly instants back
over those 28 days (a state over a period's sample: builds, review and
size, branch life), each judged again from the data. A week its number
stood only within the hysteresis counts for nothing, nor does a unit held
within it now: first past the threshold, a unit is a hint whatever it stood
at before. The unit that held it two sprints is marked (`two_sprints`); one
such unit turns the whole card red, its fresh units beside it. A card
none of whose units is past the threshold at the period's end, every one
only held within the hysteresis (below), breaks no agreement now: a hint
whatever the state's tone, never red, no worsening folded onto it; one
unit past it keeps the card's tone, the held ones beside it.
Ranking (`weight`): the team's rule broken (9), held up now (7), lasting
(5), data honesty (4); within a tier by breadth (count over judged), then
the distance past the threshold. Layout slots: 4 red, 3 hints, 1 limit, 2
gains, 1 resolution; the rest are "also spotted". With a model's words, the
red slots it leaves free take the red state cards it did not word (their
words are the page's), in rank; a red change it did not word stays a chip.

Persistence, judged again a week before the period's end (the assessment
before this one) from the same reads and against today's thresholds (a
changed threshold resolves nothing): a unit that fired then keeps firing
while its number stays at or above `hysteresis_share` (0.8) of the
threshold (`held`). A state that fired then and fires on none of its units
now is resolved, once ("gelöst"), only where every unit it named is clear on
evidence and its card was red then, by the same rules as one now (a
lasting state's two sprints read from the four weeks before that
instant): a hint then resolves silently, no card, nothing in the tally.
Where one of them can no longer be judged it reads
`unmeasurable` ("nicht mehr messbar", tone limit, its units named, no
lever), never resolved; a unit fallen idle, gone or escalated (a release
hint past the breach) is neither. A held unit's chain reads the four weeks
back: one in the band longer drops out silently (no card, no "gelöst")
while under the threshold; past it again, it fires. An episode (a merge
pause's week, a run of failed trunk or feature builds, a bot's loop) shows
on its card, dated, while it lies in the period; it is resolved the
instant after it ended (the week after the pause merged again, the trunk
green again), not when its week leaves the view, and a running one keeps
the state unresolved. States the period's numbers alone tell
(data honesty; the waiting PRs of `first_review_wait`, known at the
period's end only) are judged at the end only. A notable worsening of a
team number a red state covers (deploy frequency: the release states; lead
time: merged work before production; review time: the review waits; merged
PRs per week: the merge pause; trunk and feature build success: trunk
failing and flaky builds; release build success: release builds failing;
deploy success: deploy failing; batch size: big PRs) is a fact on that
card, not a card of its own; a change naming driver repos keeps those no
unit of any covering red state names. A hint covers nothing: a red
worsening never turns into a hint. The folding is team-wide where the
change names no repos: a red card on one project takes the team's
worsening onto it.

The states, their conditions and starting values (`thresholds` keys):

- `no_release`: a project rolled out (an App of it released to production)
  whose last release lies more than `release_breach_days` (28) back, red;
  `release_due` more than `release_hint_days` (14), a hint. An App's first
  deploy counts as a release (inside `FirstDeployGrace` a lower bound: it
  may have run those images before). Only a project with merged work since
  that release or within the last 28 days is judged; one nobody works on is
  idle. A library-only project has no App and is never judged.
- `not_on_prod`: a deployable repo's merged PRs not on production at the
  period's end, the oldest on the trunk more than `not_on_prod_days` (14),
  red. A PR merged into a branch that has not gone on is not on the trunk;
  a library's never counts; a repo no deploy shows an App deploying gets
  `untraced`, never this; a repo only infrastructure Apps release (each of
  its trace's releases matched to the App releasing then) is neither; an
  oldest PR tracing cannot place makes the repo unjudged. `slow_to_prod`:
  its typical wait from merge to production over the period's PRs that got
  there, on `not_on_prod_min_prs` (5).
- `merge_pause`: a project's week whose merged PRs fell to
  `merge_pause_share` (0.2) of its typical week (median of the
  `merge_pause_weeks` (8) weeks before that Bitbucket covered and the
  look-back holds, at least 3 such weeks, typical at least
  `merge_pause_min_typical` (3)) while at the week's end `merge_pause_waiting`
  (3) PRs or more were open with a person's approval standing, however long
  they had waited: a wait cut at the week's end is short for a PR approved
  late in it (a waiting-time threshold of half a day was tuned to one case
  and dropped). Not an approval another reviewer's needs work holds (not
  ready to merge), nor one on a PR untouched for `OpenInactiveDays` (30) by
  then (given up long ago), nor one whose merge may lie in a lost silence
  since it; a lost silence takes its working time out of the week and its
  expectation: the unit's `typical` is the typical week scaled to the
  working time left, `lost` says so (the page names the days Bitbucket
  delivered no data), and a week left expecting fewer than
  `merge_pause_min_typical` merges is not judged, since merges riptide
  never heard of could make the pause. Read as of the week's end: a closed
  week reads the same whenever it is read. The card names the approved PRs
  open at the week's end. Held up now when it is the period's last week.
- `approved_waiting`: a repo's PRs with a person's approval (not the
  author's) standing at the period's end, waiting longer than
  `approved_wait_days` (1) working days for their merge, at least
  `approved_waiting_prs` (2). An approval withdrawn, set to needs work by
  its approver or reset by a push (the reviewer status every PR event
  carries) waits no more; given again, it waits from then. A PR stays
  counted while it is open and approved, touched or not: the forgotten
  approved PR is what the state is for, and it never reads "gelöst" while
  it waits, only once merged, closed or no longer approved. Another
  reviewer's needs work leaves the approval standing in Bitbucket, but the
  PR is not ready to merge: it waits for nothing here, and where it could
  decide its repo the repo is not judged (no "gelöst" either). One whose
  merge may lie in a lost silence since its approval is of unknown state
  (its repo unjudged where it could decide it). Closed at an earlier instant
  is read from the events before it (a decline reopened later was closed
  between). The approvals are read whatever the PR's opening (one opened
  while events were lost has none), approved within the look-back and 30
  days before it: one approved earlier and still waiting is not read.
  Holds since the Nth PR had waited past the threshold. Lasting.
- `first_review_wait`: a repo's ready PRs waiting longer than
  `first_review_days` (2) working days for anyone's first touch, at least
  `first_review_waiting` (3), holding since the third had waited past it;
  or its typical pickup over it on `first_review_min_prs` (5) merged PRs.
  The unit's `measure` says which (`oldest`, `typical`); a repo with neither
  is unjudged. Lasting.
- `deploy_failing`: an App's last production deploy failed (Failed or
  Error) and none succeeded for `deploy_failing_days` (1) working day,
  counted from the run's first failure over the look-back's deploys (before
  the period too; a lower bound where the data starts), red, held up now;
  `deploy_failures`: `deploy_failure_streaks` (3) runs of failures in the
  period each ended by a success, a hint.
- `trunk_failing`: a repo's trunk builds (master, main, or the branch most
  of its PRs arrive on) failed from the first failure to the next success
  (or the period's end) for more than `trunk_failing_days` (1) working day
  with `trunk_failed_builds` (2) failures, the run counted from its first
  failure in the runs read; release builds aside. Each trunk branch is a
  sequence of its own: a green master build says nothing of a develop
  trunk. Red; held up now when still failing at the end.
- `branch_failing`: a person's feature or bugfix branch failing
  `branch_failing_builds` (5) builds in a row over `branch_failing_days`
  (1) working day. Never a bot's branch. A hint.
- `flaky_builds`, `long_builds`, `release_failing` per repo, its worst kind
  (one unit per repo), never across kinds: flaky builds `flaky_builds` (3) and `flaky_share` (1/3) of
  the failed ones on `flaky_min_runs` (10); a typical successful feature or
  trunk build over `build_minutes` (15) on `build_min_runs` (5); release
  builds failing `release_builds_failed` (2) and `release_builds_share`
  (0.2) on `release_builds_min` (5). Lasting.
- `builds_silent`: a repo with `silent_builds_merges` (3) merged PRs in the
  last `silent_builds_days` (10) working days, no build reported in them,
  after `silent_builds_before` (10) builds in the four weeks before. Data
  ("meldet nicht"), tone limit.
- `big_prs` (`big_pr_share` 1/3 of the sized merged PRs over `big_pr_lines`
  400, on `big_pr_min` 6), `fast_approval` (`fast_approval_prs` 3 big PRs
  approved within `fast_approval_minutes` 10 of being ready, the first
  touch), `unapproved` (`unapproved_share` 0.2 merged without a person's
  approval, on `unapproved_min` 10), `late_decline` (`declined_prs` 3 PRs
  declined in the period after more than `declined_days` 10 working days,
  whenever opened, unless a later PR from the same branch carries the work
  on; team-wide, the repos named): hints until held two sprints.
- `branch_life`: a repo's typical working days from a branch's first commit
  to its PR being ready over `branch_life_days` (5) on `branch_life_min_prs`
  (5) (a lower bound: a rebase resets the first commit). `branch_open`:
  branches without a PR worked on for more than `branch_open_days` (14):
  a run of pushes, none `branch_silent_days` (14) or more after the one
  before, the first more than 14 days back, the last within
  `branch_active_days` (5) working days. A branch left lying and pushed
  again starts a new run: open from its new start plus 14 days, never for
  the time it lay silent. `branch_silent`:
  `branch_silent_min` (3) branches without a PR last pushed
  `branch_silent_days` (14) to `branch_silent_max_days` (60) ago; whether
  they still exist Bitbucket does not report (deletions are not stored).
  Branches that are a PR's target (from the first PR into them on, an epic
  branch with a PR of its own included), the trunk, release lines, a
  dependency bot's scheme, bots' pushes, and bots' branches (every push a
  bot's, above) are out, and so is a branch merged without a PR: its last head
  reached the trunk or a branch a PR of the repo went into (a push whose
  tip it is, at any time: a fast-forward, or a branch cut from there with no
  commit of its own; after its last push, one that lists it or whose commit
  names it a parent; the merge commit is a push's tip, never hidden by the
  five-commit cap). Only pushes, PRs and merges before the instant judged
  count: a branch pushed after a closed period's end was silent at it.
  Hints; `branch_open` lasting, held since 14 days after its run's first
  push.
- `bot_loop`: a bot branch failing `bot_loop_failures` (10) builds in a
  row, an aborted build neither breaking nor extending it: CI time wasted,
  a hint, never a quality finding. The build section's line is
  `build.bot_runs` (runs, CI seconds, and of them the failed ones).
- Data honesty (tone limit): `untraced`, a repo's merged PRs tracing cannot
  place on production, with the main reason (`coverage`'s); `unattributed`,
  releases to production of no project; `no_prod_stage`, Argo CD reports
  deploys, none to the production stage.

The calm line counts a repo with a state's finding (not data honesty) as
not calm, and one a state judged clear on a sample (a typical value or a
share on enough cases) as judgeable.

The layout puts the gains and resolutions on the left ("runs") and the rest
on the right ("stuck"), with a tally of each and of the question. Gains rank
last, so they get slots of their own (the best 2 gains, 1 resolution) on
top of the 4 cards, worded or not. The best gain and resolution always keep
theirs; a lesser one the model worded shows beside them, past the slots if
need be, since worded text is never dropped (nor where a focus repo carries
its signal). Without a gain or a resolution no group says "runs": a quiet
period is no success (user: "wenn nichts läuft, ist läuft gut
übertrieben"). A change of deploy frequency or of a kind's build success
carries its weeks in both periods (`series`), the numbers the deploy and
build sections count per week anyway; review time, lead time and merged
PRs have no weekly series, so their cards draw none.

**Quiet is decided here, not by the model or the page.** The page may say
that nothing stands out only when `quiet` is true (owner: never
"unremarkable" where nothing could be measured or compared). Otherwise
`quiet_reasons` says why not, in this order, and the page and the model say
that instead, one sentence each:

- `no_baseline`: no team number resting on enough cases has a previous
  period to compare with, or a source started reporting after the previous
  period began. Each number's sample gate holds it; the jammed repos (a
  count of whatever is open) and deploy frequency on fewer than 10
  releases in either period (0 against 0 reads "same") compare nothing.
  When no number of the page has a previous value at all (`comparison.any`
  false, `nothing_compared`), the page says no comparison was possible and
  its head names no previous period;
- `source_gap`: a hole in a source's data the comparison crosses
  (`limits`, with its dates): a source silent for 7 days at the period's
  end (`silent_end`, a gate: its signals are left out), a lost Bitbucket
  silence overlapping either period (`silence`: every number Bitbucket
  counts loses its verdict, lead time aside; in a product view deploy
  frequency, deploy success and build success too), or a source whose
  first event lies inside the period (`starts`). Another source's silence
  in mid-period never gates: there is no evidence it lost anything;
- `not_measurable`, with the first reason that applies in
  `not_measurable`: `bitbucket` never reported; `argocd` does not report;
  `no_merged`, no PR counted as merged work; `no_release`, none of the
  merged PRs reached production and nothing new went to production in the
  period at all, no release and no App's first deploy (a re-sync carries
  no PR; a missing release, not a tracing gap); `to_prod`, deploys went to
  production but no merged PR could be traced to one (the page names the
  PRs of repos rolled out when libraries' are left out, `library`). Merged
  PRs of library repos only had nothing to trace: measurable, and said
  (`libraries_only`), no limit;
- `unjudged`: a data-honesty state (tone limit) names what could not be
  judged; no finding;
- `findings`: a signal or the question stands out; a gain, a hint, a state
  or a resolution included.

The repos: `active` had people's work in the period: a PR merged as merged
work, a build of a person's branch, an open PR. A repo whose only merge was
housekeeping or an umbrella, or whose only builds were a bot's, had
nothing by people and is not active: idle when it was active in the
previous period, else in no count at all; a pipeline whose name names no
repository is no repo. Of them `calm` are without a signal and judgeable (a build kind
compared with the previous period, or 10 merged PRs in both periods while
Bitbucket's events are whole), `unjudged` without a signal and too little
data to judge; `idle` were active in the previous period only; `repos` is
active and idle. Open PRs make no repo judgeable: many open behind a merge
freeze riptide cannot see read as fine beside a jam. The page words these
counts as sent and never reads a repo it could not judge as fine.

The repos are ranked by their signals' weight; a jam alone makes a
candidate, and repos without a signal stay out of it. The question is the
first undirected number that moved notably, in a fixed order (feature
share, untracked, wasted share, cost per kLOC, open PRs, approvals without
a comment); with none, there is no question, and the page shows none, even
where a model wrote one. A question counts as standing out. Starting
values, user-chosen 2026-11-16, not measured.

An analysis stores the rules version it was assessed under
(`metrics.RulesVersion`, `team_analyses.rules_version`). Signals, question
and wording are frozen in the stored row; an analysis of older rules, or
from before the column, would show cards the rules no longer raise. It is
not shown: the status says `outdated`, the page shows the current rules'
view with a note and offers to assess again. The schedule does not re-word
a week it already worded, so a view stays outdated until someone assesses
again or its next scheduled run.

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
