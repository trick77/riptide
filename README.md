# riptide-collector

Ingestion service for the **riptide** DevOps delivery-metrics suite.

Collects raw events from:
- **Bitbucket** (PR + push webhooks)
- **Jenkins** (build-pipeline notifications)
- **ArgoCD** (sync notifications)

…and stores them, append-only, in Postgres for later metric computation.

See `docs/` for setup and onboarding guides; see `/Users/jan/.claude/plans/valiant-knitting-biscuit.md` for the full design.

## Quickstart (local)

```bash
uv sync
podman-compose up   # boots Postgres + runs migrations + starts the app on :8000
```

Open http://localhost:8000/docs for Swagger UI.

## Database

**Postgres is provisioned externally — riptide-collector is not responsible for the database lifecycle.** Connection URL (with credentials) is supplied at runtime via the `RIPTIDE_DB_URL` env var, which on OpenShift is sourced from the `riptide-collector-secrets` Secret created from `openshift/shared/secret.env.example`.

The local `compose.yaml` runs a throwaway Postgres for development only — production deployments connect to the cluster's existing Postgres.
