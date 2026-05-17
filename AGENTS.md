# AGENTS.md

This file provides guidance to coding agents (Claude Code, Cursor, etc.) when working in this repository.

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

If `docker ps` fails, ask the user to start OrbStack.

## Architecture invariants

- **Append-only ingestion.** Every webhook handler does `INSERT … ON CONFLICT (delivery_id) DO NOTHING`. Never `UPDATE` or `DELETE` event rows. Webhook retries must be idempotent. `delivery_id` is the dedup key for each source.
- **Raw payload always stored.** `payload JSONB` keeps the full request body even if fields are extracted into typed columns. Don't drop fields you don't currently use.
- **`riptide.json` is config, not data.** `openshift/collector/riptide.json` is the in-repo sample; it declares teams (name + `group_email`) and org-wide automation rules. Edits go through PRs. The running pod hot-reloads via mtime in `RiptideConfigStore.maybe_reload()`. Do not propose moving the config into Postgres.
- **Per-team bearer keys live in a separate file**: in production it is mounted from the `riptide-collector-team-keys` Secret (never committed); the in-repo `openshift/collector/team-keys.json` is a dev sample with deterministic test hashes (raw dev bearers documented in `compose.yaml`). Stored as sha256 hashes; `TeamKeysStore` hot-reloads it the same way as the config. The bearer **is** the team identity — every webhook is tagged with `team = caller_team`.
- **No `service` column. No `service_id` on the wire.** Per-source aggregations group by `repo_full_name` / `pipeline_name` / `app_name` / `repo`; org-wide rollups group by `team`. Cross-source joins for BB↔Pipeline use `commit_sha`; Argo CD joins are described in the next bullet. Identifiers are lowercased at ingest (`commit_sha`, `revision`, `repo_full_name`, `branch_name`, `repo`) so SQL joins are case-stable. Do not propose adding a unified `service` column or `service_id` field — it served only single-pane labelling and was dropped.
- **`automation` is org-wide.** Bot definitions live at the config root, not per team.
- **Metrics are computed on read, not at ingest.** Don't add aggregation tables or scheduled rollup jobs in v1. Schema additions should preserve raw events; new metrics are SQL queries against existing rows or future materialized views.
- **Commit SHA joins Bitbucket↔Pipeline; Argo CD needs `payload->'images'`.** `bitbucket_events.commit_sha = pipeline_events.commit_sha` is a deterministic join (App-repo SHA on both sides). `argocd_events.revision` is the **GitOps-repo SHA** (proven empirically: four Apps for one service share one revision), so it does NOT directly match the other two. The App-repo SHA is embedded in image tags rendered via `.app.status.summary.images`; the receiver stores them in `payload->'images'`. A future correlator extracts SHAs from those tags to bridge Argo CD events to pipeline events. Do not propose adding a `service_id` or hand-coded service-name mappings to fix correlation — the image-tag SHA is the contract.
- **`change_type` lives on Bitbucket events only.** Don't denormalise it onto pipeline / Argo rows; join Pipeline rows via `commit_sha` and Argo rows via the image-tag SHA in `payload->'images'` at read time.
- **CI events are source-tagged, not source-routed.** All pipeline events from any CI (Jenkins, Tekton, …) land in the single `pipeline_events` table via `POST /webhooks/pipeline`, distinguished by the `source` column. Do not add per-CI tables or endpoints. The dedup key is `source#pipeline_name#run_id#phase`.
- **Noergler events carry finops + reviewer-precision only.** The `noergler_events` table is `event_type`-discriminated (`completed` | `feedback`) and is fed by `POST /webhooks/noergler` from optional noergler instances. Do not re-emit PR lifecycle from noergler — `bitbucket_events` already covers open / merged / declined. Dedup keys: `completed#<run_id>` and `feedback#<finding_id>#<verdict>`.
- **Senders verify reachability + bearer at startup via `GET /auth/ping`.** Authenticated endpoint returning `{"status":"ok","team":"<caller_team>"}`. Use this from any sender (noergler, future ones) to fail-fast on a wrong token. Don't reuse `/health` (unauth liveness) or `/ready` (unauth readiness) for this — those answer different questions.
- **`modified_at` has a Postgres trigger** (`riptide_set_modified_at`), not just SQLAlchemy `onupdate`. Raw-SQL updates also bump it. Keep the trigger when changing migrations.
- **Database is external.** `riptide-collector` does NOT manage Postgres. Do not add a Postgres Deployment to `openshift/`.
- **Pyright strict for `src/`, standard for `tests/` and `migrations/`.** New code under `src/` must satisfy strict mode — no `Any` leaks; narrow `Optional`s with `isinstance` or helpers like `_as_dict()` in `routers/bitbucket.py`.

## Repo conventions

- **Layering.** Routers do HTTP + auth + dispatch only. Source-specific payload extraction lives in `parsers_<source>.py` (e.g. `parsers_bitbucket.py`) as pure functions returning a typed `*EventDraft` dataclass — no HTTP, no DB, no config. The router computes config-derived fields (e.g. `automation_source`) and persists. Don't put extraction logic in routers; don't duplicate JSON-shape coercion (`_as_dict`/`_as_list`-style helpers belong with the extractor that uses them).
- Single Python package, `riptide_collector` (flat top-level, not a namespace package). Future suite components (e.g. `riptide-api`, `riptide-dashboard`) get their own top-level package, e.g. `riptide_dashboard` — leave architectural room for them.
- Webhook routers are factories that return an `APIRouter`. Bitbucket needs the config for automation detection (`make_router(config, session_factory, auth_dep)`); Pipeline, ArgoCD, and Noergler don't, so they take just `(session_factory, auth_dep)`. They're wired up in `src/riptide_collector/main.py::create_app`. Add the config only when a router actually needs `automation` rules or team metadata.
- Pydantic schemas: **strict** for `/webhooks/pipeline` and `/webhooks/argocd` (we own the contract — invalid payloads must 422); **permissive raw-dict parsing** for Bitbucket (its payload shapes vary; we best-effort extract).
- Use `_as_dict()` / `_as_list()` helpers in `routers/bitbucket.py` to coerce arbitrary JSON shapes — pyright strict won't accept chained `.get()` on `Optional[dict]`.
- Tests use real Postgres via testcontainers, never SQLite. The `client` fixture in `tests/conftest.py` depends on `session_factory` which truncates tables per test.
- `.pre-commit-config.yaml` runs ruff + pyright + uv-lock-check; expect CI to enforce the same.

## Logging & Splunk

- **One JSON object per line, on stdout.** OpenShift's Splunk Connect for Kubernetes (SCK) tails the container log; Splunk auto-extracts fields via `KV_MODE=json` for sourcetype `riptide:collector:json` (set as pod annotation in `openshift/collector/deployment.yaml`).
- **Stdlib loggers (uvicorn, sqlalchemy, alembic) are bridged through structlog.** Do NOT add separate logging handlers or re-init `logging.basicConfig` — `configure_logging()` in `logging_config.py` is the single entry point.
- **Splunk-reserved field names are forbidden as kwargs**: `source`, `sourcetype`, `host`, `index`, `time`, `_time`, `_raw`, `event`. The CI vendor field is `ci_system` (not `source`); the structlog event name lives in `msg` (renamed from `event`); severity lives in `log_level` (renamed from `level`). A runtime processor (`_strip_reserved`) namespaces accidental reserved kwargs under `splunk_<name>` as a safety net — do not rely on it; pick the right name from the start.
- **Field-naming convention** for non-reserved kwargs: prefer generic names that mean the same across sources (`event_type`, `status`, `phase`, `delivery_id`, `team`, `repo`, `commit_sha`). Don't pre-namespace with the source name (`noergler_event_type`, `pipeline_status`) — `webhook_source` already disambiguates in `stats by webhook_source, event_type`. Only namespace when two sources legitimately mean different things by the same word and would collide in a single Splunk panel.
- **Webhook handlers emit exactly one `msg=webhook_processed` log per request** with required fields `webhook_source ∈ {bitbucket,pipeline,argocd,noergler}`, `outcome ∈ {accepted,deduped,ignored,skipped}`, `delivery_id`, `team`. Source-specific fields go alongside (e.g. `app`, `revision`, `phase` for argocd). Include `delivery_id` even on `ignored`/`skipped` paths so triage has a key.
- **`outcome=deduped`** is detected via `RETURNING delivery_id` on the `INSERT ... ON CONFLICT DO NOTHING` — a `None` scalar means the row already existed. Preserve this when adding new sources.
- **Persist failures**: wrap the `async with session_factory()` block in `try/except Exception: logger.exception("webhook_persist_failed", ...); raise`. Never swallow.
- **Access log** is emitted by the `access_log` middleware in `main.py` as `msg=http_request` with `request_id`, `method`, `path`, `status_code`, `duration_ms`. `request_id` is bound to contextvars so any log within the request inherits it. `/health` and `/ready` are silenced; uvicorn.access is set to WARNING (do not lower it).
- **Splunk `props.conf` snippet** (owned by platform team, kept here for reference):
  ```
  [riptide:collector:json]
  SHOULD_LINEMERGE = false
  LINE_BREAKER     = ([\r\n]+)
  KV_MODE          = json
  TIME_PREFIX      = "timestamp":\s*"
  TIME_FORMAT      = %Y-%m-%dT%H:%M:%S.%6NZ
  TRUNCATE         = 0
  ```

## OpenShift layout

`openshift/` is **suite-level**, structured per-component. The collector lives in `openshift/collector/`. When adding a new component:
1. Create `openshift/<component>/` with its own `kustomization.yaml`.
2. Add it to the `resources:` list in `openshift/kustomization.yaml`.
3. Every container needs explicit `requests` AND `limits` for cpu and memory — no exceptions.
4. Use `runAsNonRoot: true` and `readOnlyRootFilesystem: true`; no fixed `runAsUser` (OpenShift assigns a random UID per project).

## What's intentionally out of v1

If asked to add these, push back unless the user is explicit:
- Change failure rate / failed deployment recovery time (DORA's current term, formerly MTTR) — no reliable incident source yet; schema reserves room for rollback-proxy detection
- Backfill workers (forward-only ingestion only)
- Aggregation API or metric endpoints (collector ingests; reads are SQL or future siblings)
- Helm chart (Kustomize is enough for v1)
- Postgres deployment manifests
