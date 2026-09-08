# AGENTS.md

## Commands

```bash
uv sync                              # install deps (creates .venv)
uv run pytest                        # all tests (uses testcontainers → needs Docker/OrbStack)
uv run pytest --cov                  # with coverage gate (fail_under = 85, branch coverage)
uv run pytest tests/test_parsers.py  # one file
uv run pytest -k test_revert         # one test by keyword
uv run ruff check . && uv run ruff format --check .   # lint + format check
uv run ruff format .                 # auto-format
uv run basedpyright                  # type-check (strict mode for src/)
RIPTIDE_DB_URL=... uv run alembic upgrade head        # apply migrations
RIPTIDE_DB_URL=... uv run alembic downgrade base      # tear down
podman-compose up                    # local dev: Postgres + migrations + app on :8000
```

If `docker ps` fails, ask the user to start OrbStack.

## Architecture invariants

- **Append-only ingestion.** Every webhook handler does `INSERT … ON CONFLICT (delivery_id) DO NOTHING`; never `UPDATE` or `DELETE` event rows. `delivery_id` is the per-source dedup key, so retries are idempotent.
- **Raw payload always stored.** `payload JSONB` keeps the full request body even if fields are extracted into typed columns. Don't drop fields you don't currently use.
- **`riptide.json` is config, not data.** `openshift/collector/riptide.json` (in-repo sample) declares teams (name + `group_email`) and org-wide automation rules; edits go through PRs and the pod hot-reloads via mtime in `RiptideConfigStore.maybe_reload()`. Never propose moving it into Postgres.
- **Per-team bearer keys live in a separate file**, mounted in production from the `riptide-collector-team-keys` Secret (never committed); `openshift/collector/team-keys.json` is a dev sample with deterministic test hashes (raw dev bearers in `compose.yaml`). Stored as sha256, hot-reloaded by `TeamKeysStore` like the config. The bearer **is** the team identity — every webhook is tagged `team = caller_team`.
- **No `service` column, no `service_id` on the wire.** Per-source aggregations group by `repo_full_name` / `pipeline_name` / `app_name` / `repo`; org-wide rollups by `team`. Identifiers are lowercased at ingest (`commit_sha`, `revision`, `repo_full_name`, `branch_name`, `repo`) so joins are case-stable. Never propose a unified `service` column or `service_id` — it served only single-pane labelling and was dropped.
- **`automation` is org-wide.** Bot definitions live at the config root, not per team. Config is the last resort, not the first: prefer what the upstream payload already states (the acting user's `type == "SERVICE"` — `actor` on push and reviewer events, `pullRequest.author.user` on PR lifecycle ones → `automation_source = "service-account"`, ranked above the `*-bot` name guess) and what senders declare about themselves (`reviewer_handle` / `actor_handle` + kind). Only accounts nobody reports get a config entry.
- **Metrics are computed on read, not at ingest.** No aggregation tables or scheduled rollup jobs in v1. Schema additions preserve raw events; new metrics are SQL against existing rows or future materialized views.
- **Commit SHA joins Bitbucket↔Pipeline; Argo CD joins on the image reference.** `bitbucket_events.commit_sha = pipeline_events.commit_sha` is deterministic (App-repo SHA both sides). `argocd_events.revision` is the **GitOps-repo SHA** — proven empirically, four Apps for one service share one revision — so it does NOT match the other two. Image **tags are not commit SHAs** either (measured in production: 0 of 4 936 image refs, all semver) — never build correlation on parsing a SHA out of a tag. The contract is the **full image reference**: senders report `pipeline_events.image_ref` (`registry/path:tag`), Argo CD stores the same strings in `payload->'images'` from `.app.status.summary.images`, and the join is exact. Historical rows predating `image_ref` fall back to the release-note correlator in `docs/correlating-deploys-to-commits.md`. Never propose `service_id` or hand-coded name mappings to fix correlation.
- **`change_type` lives on Bitbucket events only.** Don't denormalise it onto pipeline / Argo rows; join Pipeline rows via `commit_sha` and Argo rows via `payload->'images'` ↔ `pipeline_events.image_ref` at read time.
- **CI events are source-tagged, not source-routed.** All pipeline events from any CI (Jenkins, Tekton, …) land in the single `pipeline_events` table via `POST /webhooks/pipeline`, distinguished by the `source` column. Do not add per-CI tables or endpoints. The dedup key is `source#pipeline_name#run_id#phase`.
- **Noergler events carry finops + reviewer-precision only.** The `noergler_events` table is `event_type`-discriminated (`pr_completed` | `feedback`; historical rows may carry the pre-0002 `completed`) and is fed by `POST /webhooks/noergler` from optional noergler instances. Do not re-emit PR lifecycle from noergler — `bitbucket_events` already covers open / merged / declined. Dedup keys: `pr_completed#<pr_key>#<outcome>` and `feedback#<finding_id>#<verdict>`. `pr_completed` is also the source for PR diff size and for `reviewer_handle`, the bot's own account.
- **Senders verify reachability + bearer at startup via `GET /auth/ping`** — authenticated, returns `{"status":"ok","team":"<caller_team>"}`, so a wrong token fails fast. Never reuse `/health` (unauth liveness) or `/ready` (unauth readiness) for it; those answer different questions.
- **`modified_at` has a Postgres trigger** (`riptide_set_modified_at`), not just SQLAlchemy `onupdate`. Raw-SQL updates also bump it. Keep the trigger when changing migrations.
- **Database is external.** `riptide-collector` does NOT manage Postgres. Do not add a Postgres Deployment to `openshift/`.
- **Pyright strict for `src/`, standard for `tests/` and `migrations/`.** New code under `src/` must satisfy strict mode — no `Any` leaks; narrow `Optional`s with `isinstance` or helpers like `_as_dict()` in `routers/bitbucket.py`.

## Repo conventions

- **Layering.** Routers do HTTP + auth + dispatch only. Payload extraction lives in `parsers_<source>.py` (e.g. `parsers_bitbucket.py`) as pure functions returning a typed `*EventDraft` — no HTTP, no DB, no config. The router computes config-derived fields (`automation_source`) and persists. Never put extraction in routers, and keep JSON-shape coercion helpers with the extractor that uses them.
- Single flat package `riptide_collector`, not a namespace package. Future suite components (`riptide-api`, `riptide-dashboard`) get their own top-level package — leave room for them.
- Webhook routers are factories returning an `APIRouter`, wired in `main.py::create_app`. Bitbucket takes the config for automation detection (`make_router(config, session_factory, auth_dep)`); Pipeline, ArgoCD and Noergler take just `(session_factory, auth_dep)`. Pass the config only when a router needs `automation` rules or team metadata.
- Pydantic schemas: **strict** for `/webhooks/pipeline` and `/webhooks/argocd` (we own the contract — invalid payloads must 422); **permissive raw-dict parsing** for Bitbucket (its payload shapes vary; we best-effort extract).
- Use `_as_dict()` / `_as_list()` helpers in `routers/bitbucket.py` to coerce arbitrary JSON shapes — basedpyright strict won't accept chained `.get()` on `Optional[dict]`.
- Tests use real Postgres via testcontainers, never SQLite. The `client` fixture in `tests/conftest.py` depends on `session_factory` which truncates tables per test.
- `.pre-commit-config.yaml` runs ruff + basedpyright + uv-lock-check; expect CI to enforce the same.

## Logging & Splunk

- **One JSON object per line, on stdout.** Splunk Connect for Kubernetes tails the container log and auto-extracts via `KV_MODE=json` for sourcetype `riptide:collector:json` (pod annotation in `openshift/collector/deployment.yaml`).
- **Stdlib loggers (uvicorn, sqlalchemy, alembic) are bridged through structlog.** Do NOT add separate logging handlers or re-init `logging.basicConfig` — `configure_logging()` in `logging_config.py` is the single entry point.
- **Splunk-reserved names are forbidden as kwargs**: `source`, `sourcetype`, `host`, `index`, `time`, `_time`, `_raw`, `event`. CI vendor is `ci_system`, the structlog event name is `msg`, severity is `log_level`. `_strip_reserved` namespaces accidents under `splunk_<name>` as a safety net — never rely on it, pick the right name.
- **Field naming:** generic names that mean the same across sources (`event_type`, `status`, `phase`, `delivery_id`, `team`, `repo`, `commit_sha`). Never pre-namespace with the source (`noergler_event_type`) — `webhook_source` already disambiguates in `stats by webhook_source, event_type`. Namespace only when two sources genuinely mean different things by one word and would collide in a panel.
- **Exactly one `msg=webhook_processed` per request**, with `webhook_source ∈ {bitbucket,pipeline,argocd,noergler}`, `outcome ∈ {accepted,deduped,ignored,skipped}`, `delivery_id`, `team`, plus source-specific fields (`app`, `revision`, `phase` for argocd). Include `delivery_id` even on `ignored`/`skipped` so triage has a key.
- **`outcome=deduped`** is detected via `RETURNING delivery_id` on the `INSERT ... ON CONFLICT DO NOTHING` — a `None` scalar means the row already existed. Preserve this when adding new sources.
- **Persist failures**: wrap the `async with session_factory()` block in `try/except Exception: logger.exception("webhook_persist_failed", ...); raise`. Never swallow.
- **Access log**: `access_log` middleware in `main.py` emits `msg=http_request` with `request_id`, `method`, `path`, `status_code`, `duration_ms`. `request_id` is bound to contextvars so every log in the request inherits it. `/health` and `/ready` are silenced; uvicorn.access stays at WARNING.
- The Splunk `props.conf` stanza is owned by the platform team; a copy for reference lives in [`docs/splunk-props.conf`](docs/splunk-props.conf).

## OpenShift layout

`openshift/` is suite-level, one directory per component (`openshift/collector/`). Adding a component: create `openshift/<component>/` with its own `kustomization.yaml`, add it to `resources:` in `openshift/kustomization.yaml`, give every container explicit cpu+memory `requests` AND `limits` (no exceptions), and set `runAsNonRoot: true` + `readOnlyRootFilesystem: true` with no fixed `runAsUser` (OpenShift assigns a random UID per project).

## What's intentionally out of v1

If asked to add these, push back unless the user is explicit:
- Change failure rate / failed deployment recovery time (DORA's current term, formerly MTTR) — no reliable incident source yet; schema reserves room for rollback-proxy detection
- Backfill workers (forward-only ingestion only)
- Aggregation API or metric endpoints (collector ingests; reads are SQL or future siblings)
- Helm chart (Kustomize is enough for v1)
- Postgres deployment manifests
