# AGENTS.md

## Commands

```bash
uv sync                              # deps → .venv
uv run pytest                        # tests (testcontainers → needs Docker/OrbStack)
uv run pytest --cov                  # coverage gate, fail_under 85, branch
uv run ruff check . && uv run ruff format --check .
uv run basedpyright                  # strict for src/
RIPTIDE_DB_URL=... uv run alembic upgrade head   # / downgrade base
podman-compose up                    # Postgres + migrations + app on :8000
```

`docker ps` fails → ask the user to start OrbStack.

## Architecture invariants

- **Append-only.** Handlers `INSERT … ON CONFLICT (delivery_id) DO NOTHING`. Never `UPDATE` / `DELETE` event rows. `delivery_id` = per-source dedup key, so retries are idempotent.
- **Raw payload always stored** in `payload JSONB`, whole body, even for fields already extracted into columns. Don't drop unused fields.
- **`riptide.json` is config, not data.** Teams + org-wide automation rules. Edits via PR, pod hot-reloads by mtime. Never move it into Postgres.
- **Team keys are a separate file**, production-mounted from a Secret, never committed. Stored sha256, hot-reloaded. The bearer **is** the team identity — every webhook tagged `team = caller_team`.
- **No `service` column, no `service_id` on the wire.** Aggregate per source by `repo_full_name` / `pipeline_name` / `app_name` / `repo`, org-wide by `team`. Join identifiers are lowercased at ingest (`commit_sha`, `revision`, `repo_full_name`, `branch_name`, `repo`) → case-stable. It served only single-pane labelling and was dropped; never propose it again.
- **Metrics computed on read.** No aggregation tables, no rollup jobs in v1. Schema additions preserve raw events.
- **Correlation, in priority order.** Bitbucket↔Pipeline: `commit_sha` (App-repo SHA both sides, deterministic). Argo CD: the **full image reference** — senders report `pipeline_events.image_ref` (`registry/path:tag`), Argo stores the same strings in `payload->'images'`. `argocd_events.revision` is the GitOps-repo SHA (four Apps of one service share one) and matches neither other source. Image **tags are not SHAs** — measured: 0 of 4 936 refs, all semver; never parse a SHA out of a tag. Pre-`image_ref` rows: read-time fallback in `docs/correlating-deploys-to-commits.md`. Never `service_id` or name mappings.
- **`repo:refs_changed` is ref movement, not developer activity.** Measured: of 16 295 master-ref events ~15 000 were release tooling (maven/gradle release plugins, component-version job, Renovate); the 1 210 human-authored ones were merge commits already counted as `pr:merged`. Read activity and `change_type` off PR events — a `master` push has no branch prefix, so change mix over all events reads 83 % `other`. Never infer intent from an event-type name; check `author` and the commit message.
- **`change_type` on Bitbucket events only.** Don't denormalise onto pipeline / Argo rows; join at read time.
- **Automation detection is config-last.** Order: configured `automation` authors (matched against login *and* display name, case-insensitive) → acting user's `type == "SERVICE"` from the payload → `*-bot` name shape. Senders also declare themselves (`reviewer_handle` / `actor_handle` + account kind, read-time filter). Only accounts nobody reports get a config entry. `automation` is org-wide, at the config root.
- **CI events are source-tagged, not source-routed.** Every CI lands in `pipeline_events` via `POST /webhooks/pipeline`, told apart by `source`. No per-CI tables or endpoints. Dedup key `source#pipeline_name#run_id#phase`.
- **Noergler carries finops + reviewer-precision only.** `event_type` ∈ `pr_completed` | `feedback` (historical rows: pre-0002 `completed`). Never re-emit PR lifecycle — `bitbucket_events` covers open / merged / declined. Dedup keys `pr_completed#<pr_key>#<outcome>`, `feedback#<finding_id>#<verdict>`. `pr_completed` is also the source for PR diff size (Bitbucket webhooks carry none) and for the reviewer's own account.
- **Senders verify at startup via `GET /auth/ping`** — authenticated, returns the caller's team, so a wrong token fails fast. Never reuse `/health` (unauth liveness) or `/ready` (unauth readiness).
- **`modified_at` has a Postgres trigger** (`riptide_set_modified_at`), not just SQLAlchemy `onupdate`, so raw-SQL updates bump it too. Keep the trigger when changing migrations.
- **Database is external.** Never add a Postgres Deployment to `openshift/`.

## Repo conventions

- **Layering.** Routers: HTTP + auth + dispatch + config-derived fields + persist. Extraction: `parsers_<source>.py`, pure functions returning a typed `*EventDraft`, no HTTP / DB / config. Keep JSON-coercion helpers beside the extractor using them. Never extract in a router.
- Pass the config to a router only when it needs `automation` rules or team metadata.
- Schemas **strict** for `/webhooks/pipeline`, `/webhooks/argocd`, `/webhooks/noergler` — we own those contracts, invalid payloads must 422. Bitbucket is permissive raw-dict parsing; its shapes vary.
- Optional fields: accept `""` as absent. A templated-but-unset param arrives empty far more often than missing, and rejecting it drops the whole event.
- Coerce arbitrary JSON with the `_as_dict()` / `_as_list()` helpers — basedpyright strict rejects chained `.get()` on `Optional[dict]`.
- Pyright strict for `src/`, standard for `tests/` and `migrations/`. No `Any` leaks in `src/`.
- Single flat package `riptide_collector`. Future suite components get their own top-level package.
- Tests: real Postgres via testcontainers, never SQLite. Per-test truncation via the `session_factory` fixture.
- `.pre-commit-config.yaml` = ruff + basedpyright + uv-lock-check; CI enforces the same.

## Logging & Splunk

- **One JSON object per line on stdout**, auto-extracted by Splunk (`KV_MODE=json`, sourcetype `riptide:collector:json`).
- **`configure_logging()` is the single entry point**; stdlib loggers (uvicorn, sqlalchemy, alembic) are bridged through structlog. Never add handlers or re-init `logging.basicConfig`.
- **Splunk-reserved kwargs are forbidden**: `source`, `sourcetype`, `host`, `index`, `time`, `_time`, `_raw`, `event`. CI vendor → `ci_system`, event name → `msg`, severity → `log_level`. `_strip_reserved` is a safety net, not a licence.
- **Field names generic across sources** (`event_type`, `status`, `phase`, `delivery_id`, `team`, `repo`, `commit_sha`). Never pre-namespace with the source — `webhook_source` already disambiguates. Namespace only on a genuine collision of meaning.
- **Exactly one `msg=webhook_processed` per request**: `webhook_source` ∈ {bitbucket,pipeline,argocd,noergler}, `outcome` ∈ {accepted,deduped,ignored,skipped}, `delivery_id`, `team`, plus source-specific fields. Include `delivery_id` even on ignored / skipped so triage has a key.
- **`outcome=deduped`** comes from `RETURNING delivery_id` — a `None` scalar means the row existed. Preserve when adding sources.
- **Persist failures**: `try/except Exception: logger.exception("webhook_persist_failed", …); raise`. Never swallow.
- **Access log** binds `request_id` to contextvars so every log in the request inherits it. `/health` and `/ready` silenced; uvicorn.access at WARNING.
- Splunk `props.conf` is owned by the platform team; reference copy in [`docs/splunk-props.conf`](docs/splunk-props.conf).

## OpenShift layout

`openshift/` is suite-level, one directory per component. New component → own `openshift/<component>/kustomization.yaml`, added to `resources:` in `openshift/kustomization.yaml`. Every container: explicit cpu+memory `requests` AND `limits`, no exceptions. `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, never a fixed `runAsUser` — OpenShift assigns a random UID per project.

## Out of v1

Push back unless the user is explicit:
- Change failure rate / failed deployment recovery time — no reliable incident source; schema leaves room for rollback-proxy detection
- Backfill workers (ingestion is forward-only)
- Aggregation API or metric endpoints (reads are SQL, or a future sibling component)
- Helm chart (Kustomize suffices)
- Postgres manifests
