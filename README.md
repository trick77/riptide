<p align="left">
  <img src="logo-text.png" alt="riptide">
</p>

**riptide** collects delivery events from your toolchain and shows the
DevOps metrics computed from them.

## Table of contents

- [Overview](#overview)
- [What it collects](#what-it-collects)
- [Reading the event tables](#reading-the-event-tables)
- [Metrics](#metrics)
- [Quickstart (local)](#quickstart-local)
- [UI and read API (UI listener)](#ui-and-read-api-ui-listener)
- [Database](#database)
- [Documentation](#documentation)

## Overview

riptide is **built for the enterprise** — for organisations running self-hosted
toolchains behind a corporate firewall: Bitbucket Data Center, on-prem Jenkins
or Tekton (OpenShift Pipelines), OpenShift, and ArgoCD. It is **not** a SaaS,
has no third-party data egress, runs entirely inside your cluster, and is
designed for the realities of regulated environments (mandatory team /
cost-centre attribution, auditable config-as-code, no admin UIs that bypass
change control).

## What it collects

Raw events from:

- **Bitbucket** (PR + push webhooks)
- **CI pipelines** — Jenkins and Tekton supported via the same source-agnostic
  `/webhooks/pipeline` endpoint; any CI that can POST JSON works
- **ArgoCD** (sync notifications)
- **Noergler** *(optional)* — AI code-review agent forwarding LLM
  finops (model, tokens, `cost_usd`) and reviewer-precision (disagree
  feedback) via `/webhooks/noergler`. Sender-side config is opt-in.

…stored append-only in Postgres for later metric computation by other suite
components or ad-hoc SQL.

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
`other`. They already count on the deploy side. Leave them out of every PR
metric; the GitOps repos are the ones whose commits Argo CD deploys, so no list
of names is needed:

```sql
-- GitOps repos: their commits are what argocd_events.revision points at.
gitops_repos AS (
  SELECT DISTINCT b.repo_full_name
  FROM argocd_events a
  JOIN bitbucket_events b ON b.commit_sha = a.revision
)
-- ... WHERE repo_full_name NOT IN (SELECT repo_full_name FROM gitops_repos)
```

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
| **Deployment frequency** | Releases per `team` / time window in prod. A release is a successful deploy whose images differ from the same App's previous successful deploy, counted once per GitOps `revision` — see the query below. Not every sync is a release: a config change or a manual re-sync redeploys the same images (measured: a third of all intg syncs, and 7 of 8 syncs of one monitoring App), and one release reconciles every App sharing the revision (intranet + extranet), so `COUNT(*)` overcounts twice over. Report the release count next to the syncs and the first-seen deploys it was derived from, so a reader can see where the number comes from. Group by `app_name` for the per-App drill-down; drop infrastructure Apps (monitoring, bots) by name in your own report. |
| **Lead time for changes** | Per commit, not per release: `SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY lead_time) FROM lead_time_changes WHERE environment='prod' AND NOT is_merge AND NOT author_is_service_account`. The `lead_time_changes` view maps every commit that shipped to the **first** deploy that carried it, so a release rolled out to four Apps counts each change once. Exclude merge commits and release tooling, and report bots on their own line — see [Lead time for changes (DORA)](docs/dora-lead-time.md) for why, and for the coverage limits. Group by `team` or `repo_full_name`; `change_type` is not available here, since these commits come from master pushes, which carry no branch prefix. Do **not** substitute the newest commit per release: it answers "how stale was the freshest change" and reads ~7× too fast. |
| **Lead time breakdown** | Where the lead time is spent, per human PR in app repos, as five stages: coding (oldest commit on the branch → PR ready), review (ready → approval), merge (approval → merged), to intg (merged → the merge commit's first intg deploy), to prod (intg → prod). One stage for review, not pickup + review: measured, 95 of 115 approvals were the reviewer's first and only touch, so "first review → approval" is zero by construction; the count is reported as a review-depth signal. Stages 4–5 reach only PRs whose merge commit `lead_time_changes` resolves, so they cover fewer PRs than 1–3 — report the count per stage. See the query below. |
| **PR cycle time** | `pr:merged.occurred_at − pr:opened.occurred_at` per PR, keyed by `(repo_full_name, pr_id)`: PR ids are numbered per repository, so `pr_id` alone joins unrelated PRs. A PR opened before ingestion started has no `pr:opened` and drops out. Exclude GitOps repos: their PRs are stage promotions (see [Reading the event tables](#reading-the-event-tables)). |
| **Time to first review** *(DX Core 4 "code review pickup time")* | Two-part computation per PR — see the SQL block below the table. Clock-start = `COALESCE(pr:ready_for_review, pr:opened)`: PRs opened ready start at `pr:opened`; PRs opened as drafts start at the synthetic `pr:ready_for_review` (emitted by the parser when a `pr:modified` payload carries `previousDraft=true, draft=false`). Engagement = first reviewer touch (`pr:comment:added`, `pr:reviewer:approved`, `pr:reviewer:unapproved`, `pr:reviewer:needs_work`, `pr:reviewer:updated`) where `author != pr_opener AND NOT is_automated AND occurred_at >= clock-start`. The five-event reviewer union covers every touch Bitbucket DC emits (silent approvals, retracted approvals, "needs work" flips, bare reviewer-status changes); the `occurred_at >= clock-start` guard drops early-feedback comments solicited during the draft phase, which would otherwise produce negative pickup times. `NOT is_automated` strips bot comments (noergler / Renovate / etc.) — every review-time bot must be recognisable, otherwise its instant comment drives the metric toward zero. Detection matches the `automation` config block against **both** the login handle (`author`) and the display name (`author_display_name`), because a bot is often provisioned as an ordinary user account whose login says nothing. `is_automated` is decided at ingest, so rows written before a bot was recognised stay marked human: the `non_human_identities` CTE in the query below filters those at read time from the accounts senders declare (`reviewer_handle` + `reviewer_account_kind` on noergler rollups, `actor_handle` + `actor_account_kind` on pipeline events) — riptide stores the declaration, it carries no account names of its own. |
| **Build success rate** | `pipeline_events` with `phase = 'COMPLETED'` grouped by `status`. Slice by `source` to compare Jenkins vs Tekton, by `pipeline_name` / `team` for ownership. |
| **Build duration** | `pipeline_events.duration_seconds` (a Postgres `GENERATED ALWAYS AS (finished_at − started_at)` column). |
| **Deploy success rate** | `argocd_events` with `operation_phase IN ('Succeeded', 'Failed')` aggregated, filtered to `environment = 'prod'` for the prod-only view. |
| **Deploy duration** | `argocd_events.duration_seconds` (generated column). Filter by `environment = 'prod'` for production-only timing. |

#### Deployment-frequency query

One row per team and week, with the three numbers a report should show side
by side: the releases, plus the two counts they were filtered from.

```sql
WITH deploys AS (
  SELECT team, app_name, revision, occurred_at,
         -- Sorted: the same images in another order are not a change.
         (SELECT array_agg(i ORDER BY i)
            FROM jsonb_array_elements_text(payload->'images') i) AS images
  FROM argocd_events
  WHERE operation_phase = 'Succeeded' AND environment = 'prod'
),
compared AS (
  SELECT *,
         lag(images) OVER (PARTITION BY app_name ORDER BY occurred_at) AS previous_images,
         row_number() OVER (PARTITION BY app_name ORDER BY occurred_at) = 1 AS first_seen
  FROM deploys
)
SELECT team,
       date_trunc('week', occurred_at) AS week,
       -- new images against the App's previous deploy: the release count
       count(DISTINCT revision) FILTER (WHERE NOT first_seen
                                          AND images IS DISTINCT FROM previous_images) AS releases,
       -- an App's first deploy in the data has nothing to compare against:
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
             AND e.author IS DISTINCT FROM p.pr_opener
             AND NOT e.is_automated
             AND NOT EXISTS (SELECT 1 FROM non_human_identities n WHERE n.handle = lower(e.author))
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
payload carries a draft→ready flip (`previousDraft=true, draft=false`); other
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
  AND e.author IS DISTINCT FROM ps.pr_opener
  AND NOT e.is_automated
  AND NOT EXISTS (
        SELECT 1 FROM non_human_identities n WHERE n.handle = lower(e.author)
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
| **LLM review cost per KLOC** *(noergler)* | `SUM(cost_usd) / NULLIF(SUM(lines_added + lines_removed), 0) * 1000 GROUP BY team, outcome` over `noergler_pr_rollups`. Diff-size normalised cost — fair comparison across small fixes and large refactors. |
| **Wasted LLM review** *(noergler)* | `SUM(cost_usd) FROM noergler_pr_rollups WHERE outcome IN ('declined','deleted')`. Review effort spent on code that never shipped. |
| **CI compute time per pipeline / team** | `SUM(pipeline_events.duration_seconds) GROUP BY pipeline_name, team`. The unit metric for CI cost attribution. |
| **Wasted CI** | `SUM(duration_seconds) WHERE status IN ('FAILURE','Failed')` — failed builds × time. Quantifies the cost of flakes / broken tests. |
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

- **Change failure rate / failed deployment recovery time** (DORA's
  current term, replacing MTTR). No reliable incident source today.
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
[Correlating deploys back to commits](docs/correlating-deploys-to-commits.md).

## Quickstart (local)

```bash
docker compose up   # boots Postgres + runs `riptide migrate` + starts riptide
```

The UI is on http://127.0.0.1:8080, the webhooks on :8000. Open
http://localhost:8000/docs for Swagger UI; the contract itself is
`/openapi.yaml` ([`backend/api/openapi.yaml`](backend/api/openapi.yaml)).

riptide is one Go binary, `riptide`, in [`backend/`](backend/), with the UI
embedded:

| Command | Does |
| --- | --- |
| `riptide serve` (default) | the collector (webhooks, `:8000`) and the UI listener (UI and `/api`, `127.0.0.1:8080`) |
| `riptide migrate` | applies pending schema migrations and exits; run it before `serve` |
| `riptide onboard-bitbucket` | creates, updates or removes the riptide webhook on Bitbucket repos |
| `riptide check-onboarding` | lists which repos, pipelines and apps reported recently |
| `riptide version` | prints the version |
| `riptide -healthcheck` | probes `/healthz`, exit 0 when live: the container's liveness probe |
| `riptide -readycheck` | probes `/readyz`, exit 0 when the database answers (a schema that is behind stops `serve` at startup) |

The probes `/healthz` and `/readyz` answer only a caller on the same host
(loopback, no forwarding header) and 404 anyone else, so deploy them as exec
probes (`/riptide -healthcheck`, `/riptide -readycheck`), not HTTP probes. The
image is distroless: no shell, no wget.

The gate is [ship](https://github.com/trick77/ship): `make check` runs what CI
runs (lint, vuln scan, tests with a Postgres it starts from `compose.yaml`'s
`db` image, coverage). Tests by hand: `docker compose up -d db`, then
`SHIP_TEST_DSN='postgres://riptide:riptide@localhost:5432/riptide?sslmode=disable' go -C backend test ./...`.
The database schema is in [docs/schema.md](docs/schema.md), generated from the
migrations. Releases are cut by hand with the Release workflow.

## UI and read API (UI listener)

`riptide serve` runs two listeners. The collector (`BACKEND_LISTEN_ADDR`,
`:8000`) takes the webhooks, `/auth/ping` and the probes. The UI listener
(`BACKEND_UI_ADDR`, `127.0.0.1:8080`) serves the UI on `/` and the read API
under `/api`; locally, `docker compose up` publishes it on
http://127.0.0.1:8080.
In the pod only the auth proxy sidecar reaches it on loopback, so the route
that carries webhooks cannot reach `/api`. The read API does no
authentication of its own, so riptide refuses a non-loopback
`BACKEND_UI_ADDR` unless `BACKEND_UI_ALLOW_REMOTE=true` (compose sets it: the
container binds every interface, the host publishes the port on its own
loopback), and a UI port the collector already binds. Access is decided by
the auth proxy in front of the UI listener: every authenticated user can read
every team, deliberately; per-team scoping would belong in the proxy.

`GET /api/dataset?team=<team>&from=<date>&to=<date>` returns one team's facts
for the window `[from, to)` (RFC 3339 or `YYYY-MM-DD`, where a date as `to`
includes that day; the default is the last 28 days, the maximum 400 days):
one row per PR, deploy, shipped commit, CI run and noergler review. It never
returns aggregates: the UI filters by product and computes the percentiles.
Each field follows the rules of the [Metrics](#metrics) queries, and a test
checks it against them on the same data. `gitops`, `service_account` and
deploy `kind` are set from the data, never from name lists; `bot` adds
riptide's automation detection as it stands at read time, the same for a
PR's opener, its reviewers and a commit's author. A PR's `environments` lists
its merge commit's first deploy per environment, earliest first, including
deploys after the window; `meta.production_stage` names the production one.
`meta.build_type_from` tells a run's missing `build_type` before the team's
pipelines sent any from one the sender left out. The shape is `Dataset` in
[`backend/api/openapi.yaml`](backend/api/openapi.yaml). `GET /api/teams`
lists the configured teams for the UI's team picker.

The UI ([`ui/`](ui/), React, built into the binary) is one page per team:
whole calendar weeks (4, 12 or 26), filtered by product (Bitbucket project)
and searchable. Every number has a "Woher?" with its definition, its basis
and the query it follows. Build numbers are never averaged across kinds of
run: releases, master builds, feature/PR builds and other branches each get
their own success rate and duration. The view lives in the query string
(`?team=…&product=…&w=12&bots=1`), so a link or a printout reproduces it.
`make fe-build` builds it; `npm --prefix ui run dev` serves it with hot
reload against a local `riptide serve` on 127.0.0.1:8080.

## Database

**Postgres is provisioned externally — riptide is not responsible for
the database lifecycle.** Connection URL (with credentials) is supplied at
runtime via the `BACKEND_DB_URL` env var.

The local `compose.yaml` runs a throwaway Postgres for development only —
production deployments connect to the cluster's existing Postgres.

## Authentication: per-source team keys

Each team has **one secret per source** in `team-keys.json`. A leaked
secret is therefore scoped to a single source — an exposed ArgoCD token
cannot be replayed against `/webhooks/pipeline` or `/webhooks/bitbucket`.

`team-keys.json` shape:

```json
{
  "<team>": {
    "bitbucket": "<hmac-secret>",
    "argocd":    "<bearer-token>",
    "jenkins":   "<bearer-token>",
    "noergler":  "<bearer-token>"
  }
}
```

`noergler` is optional per team; the others are required for any team
that uses the corresponding source.

| Source                    | Endpoint                          | Auth on the wire                                            | `team-keys.json` key |
| ------------------------- | --------------------------------- | ----------------------------------------------------------- | -------------------- |
| Bitbucket DC              | `POST /webhooks/bitbucket/{team}` | `X-Hub-Signature: sha256=<hex>` (HMAC over raw body)        | `bitbucket`          |
| ArgoCD                    | `POST /webhooks/argocd`           | `Authorization: Bearer <raw>`                               | `argocd`             |
| Jenkins / Tekton          | `POST /webhooks/pipeline`         | `Authorization: Bearer <raw>`                               | `jenkins`            |
| Noergler *(optional)*     | `POST /webhooks/noergler`         | `Authorization: Bearer <raw>`                               | `noergler`           |

**Strict source binding.** Riptide looks up the bearer against *only* the
team's secret for the endpoint's source. Argument: an `argocd` token
presented to `/webhooks/pipeline` returns `401`, even if the same team
owns both keys.

**Bitbucket is HMAC-only.** BBS DC's REST API silently drops
`credentials.password` on POST/PUT (verified empirically — UI Save works,
REST doesn't), so Basic auth via REST is unusable. HMAC via
`configuration.secret` round-trips fine. The `riptide onboard-bitbucket`
command provisions HMAC; team identity comes from the URL path.

**Gotcha — Kubernetes Secret reads are base64-wrapped**. `oc get secret X
-o jsonpath='{.data.Y}'` returns the wrapped form. Always pipe through
`base64 -d` to get the raw value:

```bash
oc -n argocd get secret argocd-notifications-secret \
  -o jsonpath='{.data.riptide-token-checkout}' | base64 -d
```

Use `stringData:` (not `data:`) when *writing* — it does the wrap for you.

If you paste base64-of-raw where raw is expected, the symptom is `401
{"detail":"Invalid credentials."}` — `team-keys.json` doesn't contain the
wrapped value, so the lookup fails.

## Documentation

See [`docs/`](docs/) for setup and onboarding guides:

- [Setup: Bitbucket webhook](docs/setup-bitbucket-webhook.md)
- [Setup: Jenkins notification](docs/setup-jenkins-notification.md)
- [Setup: Tekton / OpenShift Pipelines](docs/setup-tekton-pipeline.md)
- [Setup: ArgoCD notification](docs/setup-argocd-notification.md)
- [Setup: Noergler notification](docs/setup-noergler-notification.md)
- [Onboarding a team](docs/onboarding-a-team.md)
- [Change notes](docs/change-notes.md) — what to do on the operator side after a deploy
- [Lead time for changes (DORA)](docs/dora-lead-time.md)
- [Correlating deploys back to commits](docs/correlating-deploys-to-commits.md)
