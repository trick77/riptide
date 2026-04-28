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

…stored append-only in Postgres for later metric computation by other suite
components or ad-hoc SQL.

## Metrics

riptide-collector ingests; **metrics are computed on read** as SQL queries
(or, eventually, materialized views) over the raw event tables. The catalog
and the schema have been designed so the following are all derivable from
the data captured in v1.

> Anything below not currently in v1 is marked *(planned)*; the schema reserves
> room for it without rework.

### From the DORA / DX / SPACE families

| Metric | How it's computed |
|---|---|
| **Deployment frequency** | `COUNT(*)` of `argocd_events` per `service` / `team` / time window where `operation_phase = 'Succeeded'`. |
| **Lead time for changes** | For each merged PR, `MIN(bitbucket_events.occurred_at)` for the PR (first commit) → `argocd_events.occurred_at` of the prod deploy that carries the same `commit_sha`. Joined via the SHA. Stratify by `bitbucket_events.change_type` (feature / hotfix / bugfix / …) to see hotfix lead time vs. feature lead time separately. |
| **PR cycle time** | `pullrequest:fulfilled.occurred_at − pullrequest:created.occurred_at` per PR id. |
| **Time to first review** *(DX Core 4 "code review pickup time")* | First reviewer event timestamp − PR-created timestamp on `bitbucket_events`. |
| **Build success rate** | `pipeline_events` with `phase = 'COMPLETED'` grouped by `status`. Slice by `source` to compare Jenkins vs Tekton, by `service`/`team` for ownership. |
| **Build duration** | `pipeline_events.duration_seconds` (a Postgres `GENERATED ALWAYS AS (finished_at − started_at)` column). |
| **Deploy success rate** | `argocd_events` with `operation_phase IN ('Succeeded', 'Failed')` aggregated. |
| **Deploy duration** | `argocd_events.duration_seconds` (generated column). |

### Quality / process signals from Bitbucket

| Metric | How it's computed |
|---|---|
| **PR size** | `lines_added`, `lines_removed`, `files_changed` columns on `bitbucket_events` (extracted from the PR payload). |
| **Revert rate** | `COUNT(*) WHERE is_revert = true` over total commits — a free, weak Change-Failure-Rate proxy. |
| **Hotfix rate** | `COUNT(*) WHERE change_type = 'hotfix'` over total deploys per window — operational-pain signal. |
| **Change mix** | Distribution of `change_type` (feature / bugfix / hotfix / chore / refactor / docs / other) per team per week. |
| **Tickets per deploy** | `COUNT(DISTINCT unnest(jira_keys))` per deploy — small-batch indicator. Jira keys are extracted at write time from PR title, description, branch name, and commit messages via regex `[A-Z][A-Z0-9]+-\d+`, deduplicated, GIN-indexed. |
| **Untracked-work rate** | `COUNT(*) WHERE jira_keys = '{}'` over merged PRs — process-compliance signal. |
| **Per-ticket flow** | `WHERE 'ABC-1234' = ANY(jira_keys)` returns every event for a ticket across Bitbucket / pipeline / Argo (joined via commit_sha). |
| **Human vs automated split** | `WHERE NOT is_automated` (Renovate / Dependabot / Snyk / Mend / generic-bot detection runs at write time and tags `automation_source`). Default dashboards exclude bots; bot velocity is a separate CI-health view. |

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
`openshift/shared/secret.env.example`.

The local `compose.yaml` runs a throwaway Postgres for development only —
production deployments connect to the cluster's existing Postgres.

## Documentation

See [`docs/`](docs/) for setup and onboarding guides:

- [Setup: Bitbucket webhook](docs/setup-bitbucket-webhook.md)
- [Setup: Jenkins notification](docs/setup-jenkins-notification.md)
- [Setup: Tekton / OpenShift Pipelines](docs/setup-tekton-pipeline.md)
- [Setup: ArgoCD notification](docs/setup-argocd-notification.md)
- [Onboarding a team](docs/onboarding-a-team.md)
- [OpenShift manifests](openshift/README.md)
