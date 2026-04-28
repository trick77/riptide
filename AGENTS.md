# AGENTS.md

This file provides guidance to coding agents (Claude Code, Cursor, etc.) when working in this repository.

## What this is

`riptide-collector` is the first component of a planned **riptide** suite of DevOps delivery-metrics tools. It is an append-only ingestion service that captures raw webhook events from Bitbucket, CI pipelines (Jenkins, Tekton, …), and ArgoCD into Postgres for later metric computation by other suite components or ad-hoc SQL.

Future suite components (e.g. `riptide-api`, `riptide-dashboard`) will be added as siblings — leave architectural room for them.

## Commands

```bash
uv sync                              # install deps (creates .venv)
uv run pytest                        # all tests (uses testcontainers → needs Docker/OrbStack)
uv run pytest --cov                  # with coverage gate (fail_under = 85, branch coverage)
uv run pytest tests/test_parsers.py  # one file
uv run pytest -k test_revert         # one test by keyword
uv run ruff check . && uv run ruff format --check .   # lint + format check
uv run ruff format .                 # auto-format
uv run pyright                       # type-check (strict mode for src/)
RIPTIDE_DB_URL=... uv run alembic upgrade head        # apply migrations
RIPTIDE_DB_URL=... uv run alembic downgrade base      # tear down
podman-compose up                    # local dev: Postgres + migrations + app on :8000
```

The DB-backed tests need a running Docker daemon. If `docker ps` fails, ask the user to start OrbStack.

## Architecture invariants

These are not obvious from any single file and are easy to break:

- **Append-only ingestion.** Every webhook handler does `INSERT … ON CONFLICT (delivery_id) DO NOTHING`. Never `UPDATE` or `DELETE` event rows. Webhook retries must be idempotent. The `delivery_id` is the dedup key for each source — see each router for how it's synthesised when no UUID header is present.
- **Raw payload always stored.** `payload JSONB` keeps the full request body even if we extracted fields into typed columns. Don't drop fields you don't currently use.
- **Catalog is config, not data.** `config/service-catalog.json` is the source of truth for service↔team↔repo mapping and bot detection. There is no admin UI or DB-backed catalog. Edits go through PRs to that file. The running pod hot-reloads via mtime in `CatalogStore.maybe_reload()`. Do not propose moving the catalog into Postgres.
- **`automation` is org-wide.** Bot definitions live at the catalog root, not per service — Renovate/Dependabot run across all repos.
- **Metrics are computed on read, not at ingest.** Don't add aggregation tables or scheduled rollup jobs in v1. Schema additions should preserve raw events; new metrics are SQL queries against existing rows or future materialized views.
- **Commit SHA is the universal join key.** All three sources record `commit_sha`/`revision`. Lead-time joins between `bitbucket_events`, `pipeline_events`, `argocd_events` happen on this column.
- **`change_type` lives on Bitbucket events only.** Don't denormalise it onto pipeline / Argo rows; join via `commit_sha` at read time.
- **CI events are source-tagged, not source-routed.** All pipeline events from any CI (Jenkins, Tekton, …) land in the single `pipeline_events` table via `POST /webhooks/pipeline`, distinguished by the `source` column. Do not add per-CI tables or endpoints. The dedup key is `source#pipeline_name#run_id#phase`.
- **`modified_at` has a Postgres trigger** (`riptide_set_modified_at`), not just SQLAlchemy `onupdate`. Raw-SQL updates also bump it. Keep the trigger when changing migrations.
- **Database is external.** `riptide-collector` does NOT manage Postgres. The cluster Postgres is provisioned separately; the URL ships via `riptide-collector-secrets`. Do not add a Postgres Deployment to `openshift/`.
- **Pyright strict for `src/`, standard for `tests/` and `migrations/`.** New code under `src/` must satisfy strict mode — typed dicts, no `Any` leaks, narrow `Optional`s with `isinstance` or helper coercions like `_as_dict()` in `routers/bitbucket.py`.

## Repo conventions

- Single Python package, `riptide_collector` (flat top-level, not a namespace package). When future suite components arrive they get their own top-level package, e.g. `riptide_dashboard`.
- Webhook routers are factories (`make_router(catalog, session_factory, auth_dep)`) that return an `APIRouter`. They're wired up in `src/riptide_collector/main.py::create_app`. Use the same pattern for any new router.
- Pydantic schemas: **strict** for `/webhooks/pipeline` and `/webhooks/argocd` (we own the contract — invalid payloads must 422); **permissive raw-dict parsing** for Bitbucket (its payload shapes vary; we best-effort extract).
- Use `_as_dict()` / `_as_list()` helpers in `routers/bitbucket.py` to coerce arbitrary JSON shapes — pyright strict won't accept chained `.get()` on `Optional[dict]`.
- Tests use real Postgres via testcontainers, never SQLite. The `client` fixture in `tests/conftest.py` depends on `session_factory` which truncates tables per test.
- `.pre-commit-config.yaml` runs ruff + pyright + uv-lock-check; expect CI to enforce the same.

## OpenShift layout

`openshift/` is **suite-level**, structured per-component. The collector lives in `openshift/collector/`. When adding a new component:
1. Create `openshift/<component>/` with its own `kustomization.yaml`.
2. Add it to the `resources:` list in `openshift/kustomization.yaml`.
3. Every container needs explicit `requests` AND `limits` for cpu and memory — no exceptions.
4. Use `runAsNonRoot: true` and `readOnlyRootFilesystem: true`; no fixed `runAsUser` (OpenShift assigns a random UID per project).

## What's intentionally out of v1

If asked to add these, push back unless the user is explicit:
- Change failure rate / MTTR (no reliable incident source yet — schema reserves room for rollback-proxy detection)
- Backfill workers (forward-only ingestion only)
- Aggregation API or metric endpoints (collector ingests; reads are SQL or future siblings)
- Helm chart (Kustomize is enough for v1)
- Postgres deployment manifests
