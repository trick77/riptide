<p align="left">
  <img src="logo-text.png" alt="riptide">
</p>

Ingestion service for the **riptide** DevOps delivery-metrics suite.

## Table of contents

- [Overview](#overview)
- [What it collects](#what-it-collects)
- [Metrics](#metrics)
- [Quickstart (local)](#quickstart-local)
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

## Metrics

riptide-collector ingests; **metrics are computed on read** as SQL queries
(or, eventually, materialized views) over the raw event tables. The config
and the schema have been designed so the following are all derivable from
the data captured in v1.

> Anything below not currently in v1 is marked *(planned)*; the schema reserves
> room for it without rework.

> `argocd_events.environment` is the lowercased suffix of the destination
> namespace (after the last `-`) — e.g. `payments-prod` → `prod`,
> `checkout-intg` → `intg`. Which suffix counts as "production" is configured
> in `openshift/collector/riptide.json` (`environments.production_stage`,
> default `prod`). The literal `'prod'` in the example queries below is a
> placeholder — substitute whatever your `production_stage` is set to. Rows
> ingested before this column existed have `environment = NULL`.

### From the DORA / DX / SPACE families

| Metric | How it's computed |
|---|---|
| **Deployment frequency** | `COUNT(*)` of `argocd_events` per `app_name` / `team` / time window where `operation_phase = 'Succeeded' AND environment = 'prod'`. Drop the `environment` filter (or slice by it) for staging visibility. |
| **Lead time for changes** | For each merged PR, `MIN(bitbucket_events.occurred_at)` for the PR (first commit) → `argocd_events.occurred_at` of the prod deploy that carries the same `commit_sha` and `environment = 'prod'`. Joined via the SHA. Stratify by `bitbucket_events.change_type` (feature / hotfix / bugfix / …) to see hotfix lead time vs. feature lead time separately. |
| **PR cycle time** | `pullrequest:fulfilled.occurred_at − pullrequest:created.occurred_at` per PR id. |
| **Time to first review** *(DX Core 4 "code review pickup time")* | First reviewer event timestamp − PR-created timestamp on `bitbucket_events`. |
| **Build success rate** | `pipeline_events` with `phase = 'COMPLETED'` grouped by `status`. Slice by `source` to compare Jenkins vs Tekton, by `pipeline_name` / `team` for ownership. |
| **Build duration** | `pipeline_events.duration_seconds` (a Postgres `GENERATED ALWAYS AS (finished_at − started_at)` column). |
| **Deploy success rate** | `argocd_events` with `operation_phase IN ('Succeeded', 'Failed')` aggregated, filtered to `environment = 'prod'` for the prod-only view. |
| **Deploy duration** | `argocd_events.duration_seconds` (generated column). Filter by `environment = 'prod'` for production-only timing. |

### Quality / process signals from Bitbucket

| Metric | How it's computed |
|---|---|
| **PR size** | `lines_added`, `lines_removed`, `files_changed` columns on `bitbucket_events`, enriched on `pr:merged` via the BBS DC `/diff` REST API. NULL on `pr:opened` / `pr:declined` rows. |
| **Commit count** *(per push)* | `SUM(push_commit_count)` on `bitbucket_events WHERE event_type = 'repo:refs_changed'`, group by `team` / `repo_full_name` / week. Enriched per push via the BBS DC `/commits?since={fromHash}&until={toHash}` REST API. |
| **Commit author count** *(per push)* | `SUM(push_author_count)` on the same rows. Distinct count is computed *within* each push — not deduped across pushes within a window. Switching to `commit_authors text[]` would enable cross-push dedup; not in v1. |
| **Revert rate** | `COUNT(*) WHERE is_revert = true` over total commits — a free, weak Change-Failure-Rate proxy. |
| **Hotfix rate** | `COUNT(*) WHERE change_type = 'hotfix'` over total deploys per window — operational-pain signal. |
| **Change mix** | Distribution of `change_type` (feature / bugfix / hotfix / chore / refactor / docs / other) per team per week. |
| **Tickets per deploy** | `COUNT(DISTINCT unnest(jira_keys))` per deploy — small-batch indicator. Jira keys are extracted at write time from PR title, description, branch name, and commit messages via regex `[A-Z][A-Z0-9]+-\d+`, deduplicated, GIN-indexed. |
| **Untracked-work rate** | `COUNT(*) WHERE jira_keys = '{}'` over merged PRs — process-compliance signal. |
| **Per-ticket flow** | `WHERE 'ABC-1234' = ANY(jira_keys)` returns every event for a ticket across Bitbucket / pipeline / Argo (joined via commit_sha). |
| **Human vs automated split** | `WHERE NOT is_automated` (Renovate / Dependabot / Snyk / Mend / generic-bot detection runs at write time and tags `automation_source`). Default dashboards exclude bots; bot velocity is a separate CI-health view. |
| **AI reviewer precision** *(noergler)* | `1 - count(noergler_events WHERE event_type='feedback' AND verdict='disagreed') / count(noergler_events WHERE event_type='completed')` per repo × week. Higher = the AI review is more useful. |

### FinOps signals

For CI / deploy compute, riptide captures **duration and attribution** but
does not assign currency — multiply by your own $/runner-second to convert.
LLM review cost is the exception: when the noergler source is wired up,
events arrive pre-priced in USD.

| Signal | How it's computed |
|---|---|
| **LLM review spend per model / team** *(noergler)* | `SUM(noergler_events.cost_usd), SUM(prompt_tokens + completion_tokens) GROUP BY model, team` over `event_type = 'completed'`. Pre-priced — no multiplier needed. |
| **CI compute time per pipeline / team** | `SUM(pipeline_events.duration_seconds) GROUP BY pipeline_name, team`. The unit metric for CI cost attribution. |
| **Wasted CI** | `SUM(duration_seconds) WHERE status IN ('FAILURE','Failed')` — failed builds × time. Quantifies the cost of flakes / broken tests. |
| **Bot-driven pipeline churn** | `pipeline_events` joined to `bitbucket_events` via `commit_sha` filtered on `is_automated = true`. Renovate / Dependabot can drive 40–70% of pipeline runs in many orgs; useful input for batching policies. |
| **Deploy compute** | `SUM(argocd_events.duration_seconds) GROUP BY app_name, team, environment` — keep `environment` in the grouping to attribute prod vs. non-prod compute separately. |
| **Cost-by-change-type** | Group pipeline / argocd compute by `bitbucket_events.change_type` (joined via `commit_sha`): hotfix vs. feature spend, week over week. |

What riptide does **not** provide today, and the natural seam for it:

- **Currency.** Add a `unit_cost` config (per-runner $/sec) in
  `openshift/collector/riptide.json`, or pull real per-namespace cost from
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

The universal join key across all three sources is the **commit SHA**
(`bitbucket_events.commit_sha`, `pipeline_events.commit_sha`,
`argocd_events.revision`).

## Quickstart (local)

```bash
uv sync
podman-compose up   # boots Postgres + runs migrations + starts the app on :8000
```

Open http://localhost:8000/docs for Swagger UI.

## Database

**Postgres is provisioned externally — riptide-collector is not responsible for
the database lifecycle.** Connection URL (with credentials) is supplied at
runtime via the `RIPTIDE_DB_URL` env var, which on OpenShift is sourced from
the `riptide-collector-secrets` Secret created from
`openshift/secret.env.example`.

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
`configuration.secret` round-trips fine. The `scripts/bitbucket_onboarding.py`
script provisions HMAC; team identity comes from the URL path.

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
- [OpenShift manifests](openshift/README.md)
