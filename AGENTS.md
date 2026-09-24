# AGENTS.md

## Commands

The Go module is in `backend/`; `ci/` (coverage gates), `scripts/` (operator scripts: table export/truncate, onboarding examples), `docs/`, `openshift/` and `archive/` are at the root. `hack/` holds OpenShift/Kubernetes helpers only; anything else gets its own home. Go commands run from `backend/`, scripts and `make` from the root.

```bash
docker compose up -d db              # Postgres 17 on :5432 for the store/api tests
export RIPTIDE_TEST_DSN='postgres://riptide:riptide@localhost:5432/riptide?sslmode=disable'
cd backend && go test -race ./...    # DB tests skip without RIPTIDE_TEST_DSN
make backend-coverage                # coverprofile → Cobertura → ci/coverage-gate.sh (85 % floor, cmd/ excluded)
./ci/patch-coverage.sh origin/master     # ≥ 75 % of changed lines covered
gofmt -l .                           # must print nothing
cd backend && go vet ./... && golangci-lint run ./...
riptide migrate                      # init container; `serve` never migrates
docker compose up                    # Postgres + migrate + app on :8000
```

`docker ps` fails → ask the user to start OrbStack.

## Architecture invariants

- **Append-only.** Handlers `INSERT … ON CONFLICT (delivery_id) DO NOTHING`. Never `UPDATE` / `DELETE` event rows. `delivery_id` = per-source dedup key, so retries are idempotent.
- **Raw payload always stored** in `payload JSONB`, whole body, even for fields already extracted into columns. Don't drop unused fields.
- **`riptide.json` is config, not data.** Teams + org-wide automation rules. Edits via PR, the pod re-reads it every `RIPTIDE_CONFIG_RELOAD_SECONDS` and applies a changed, valid file. Never move it into Postgres.
- **Team keys are a separate file**, production-mounted from a Secret, never committed. Raw tokens, compared in constant time, hot-reloaded with the config; a reload that leaves a configured team without keys, or gives two teams one token, is rejected. The bearer **is** the team identity — every webhook tagged `team = caller_team`.
- **No `service` column, no `service_id` on the wire.** Aggregate per source by `repo_full_name` / `pipeline_name` / `app_name` / `repo`, org-wide by `team`. Join identifiers are lowercased at ingest (`commit_sha`, `revision`, `repo_full_name`, `branch_name`, `repo`) → case-stable. It served only single-pane labelling and was dropped; never propose it again.
- **Metrics computed on read.** No aggregation tables, no rollup jobs in v1. Schema additions preserve raw events.
- **Correlation, in priority order.** Bitbucket↔Pipeline: `commit_sha` (App-repo SHA both sides, deterministic). Argo CD: the **full image reference** — senders report `pipeline_events.image_ref` (`registry/path:tag`), Argo stores the same strings in `payload->'images'`. `argocd_events.revision` is the GitOps-repo SHA (four Apps of one service share one) and matches neither other source. Image **tags are not SHAs** — measured: 0 of 4 936 refs, all semver; never parse a SHA out of a tag. Pre-`image_ref` rows: read-time fallback in `docs/correlating-deploys-to-commits.md`. Never `service_id` or name mappings.
- **`repo:refs_changed` is ref movement, not developer activity.** Measured: of 16 295 master-ref events ~15 000 were release tooling (maven/gradle release plugins, component-version job, Renovate); the 1 210 human-authored ones were merge commits already counted as `pr:merged`. Read activity and `change_type` off PR events — a `master` push has no branch prefix, so change mix over all events reads 83 % `other`. Never infer intent from an event-type name; check `author` and the commit message.
- **Lead time is per commit, against the first deploy that carried it** — the `lead_time_changes` view (over `commit_sightings` + `deploy_commit_ranges`). Exclude merge commits and service-account commits; report bots as their own line, never blended (87 % of commits reaching prod were Renovate's). Never quote the newest-commit-per-release shortcut as lead time: measured 26.7 h against a real 193.6 h.
- **`change_type` on Bitbucket events only.** Don't denormalise onto pipeline / Argo rows; join at read time.
- **Automation detection is config-last.** Order: configured `automation` authors (matched against login *and* display name, case-insensitive) → acting user's `type == "SERVICE"` from the payload → `*-bot` name shape. Senders also declare themselves (`reviewer_handle` / `actor_handle` + account kind, read-time filter). Only accounts nobody reports get a config entry. `automation` is org-wide, at the config root.
- **CI events are source-tagged, not source-routed.** Every CI lands in `pipeline_events` via `POST /webhooks/pipeline`, told apart by `source`. No per-CI tables or endpoints. Dedup key `source#pipeline_name#run_id#phase`.
- **Noergler carries finops + reviewer-precision only.** `event_type` ∈ `pr_completed` | `feedback`; the pre-rollup `completed` is rejected. Never re-emit PR lifecycle — `bitbucket_events` covers open / merged / declined. Dedup keys `pr_completed#<pr_key>#<outcome>`, `feedback#<finding_id>#<verdict>`. `pr_completed` is also the source for PR diff size (Bitbucket webhooks carry none) and for the reviewer's own account.
- **Senders verify at startup via `GET /auth/ping`** — authenticated, returns the caller's team, so a wrong token fails fast. Never reuse `/health` (unauth liveness) or `/ready` (unauth readiness).
- **`modified_at` has a Postgres trigger** (`riptide_set_modified_at`), so any `UPDATE`, raw SQL included, bumps it. Keep the trigger when changing migrations.
- **Database is external.** Never add a Postgres Deployment to `openshift/`.

## Repo conventions

- **No web framework, no ORM, no logging library in the Go module.** `net/http` ServeMux with method patterns, pgx with raw SQL (one function per query), `log/slog` with the handler in `internal/logging`. Do not add one.
- **Layering.** `internal/api`: HTTP + auth + dispatch + config-derived fields (automation, ignored stages) + persist. `internal/parse`: pure functions returning a typed `*Draft` or a validation error, no HTTP / DB / config. Never extract in a handler.
- Schemas **strict** for `/webhooks/pipeline`, `/webhooks/argocd`, `/webhooks/noergler`: invalid payloads 422 with FastAPI's `{"detail": [{type, loc, msg}]}`, every failing field listed. Pipeline and Argo CD allow unknown fields (kept in `payload`); noergler forbids them. Bitbucket is permissive map parsing; its shapes vary.
- Optional fields: accept `""` (and whitespace) as absent. A templated-but-unset param arrives empty far more often than missing, and rejecting it drops the whole event.
- Authenticate before reading the body: a bad body behind a bad key is a 401, never a 422.
- Response bodies are structs, not maps: `encoding/json` sorts map keys.
- `api/openapi.yaml` is the contract; the kin-openapi test drives the real mux and validates every response and every accepted fixture against it, so a handler change and its spec change land in the same commit. Nullable types are `type: [string, "null"]`, never a `oneOf` with a null branch. CI lints it with Spectral.
- **Migrations**: `internal/store/migrations/NNNN_*.sql`, embedded, applied in name order by `riptide migrate` under an advisory lock and recorded in `schema_migrations`. Never edit an applied migration; add the next number. `serve` checks `SchemaCurrent` at startup and refuses to run behind.
- `backend/` is the collector (`cmd/riptide`, `internal/*`). A future suite component gets its own top-level directory and module, not a package in this one.
- Tests: real Postgres, never SQLite. `internal/testdb` gives each test its own schema; tests skip without `RIPTIDE_TEST_DSN`. Fixtures are `internal/parse/testdata/*.json`.
- Coverage floor 85 % of lines (`ci/coverage-floors`), patch coverage 75 %. The `ci/` gate scripts come from the repo family (noergler, loom); here they call `ci/diff-cover.go` and `ci/cobertura-lines.go` instead of Python, so CI runs no Python. Keep it that way.

## Logging & Splunk

- **One JSON object per line on stdout**, auto-extracted by Splunk (`KV_MODE=json`, sourcetype `riptide:collector:json`). Leading keys fixed: `timestamp` (first, Splunk only scans 128 chars), `log_level`, `service`, `version`, `env`, `msg`.
- **`logging.Setup()` is the single entry point**; everything logs through the `*slog.Logger` it returns. Never write to stdout otherwise.
- **Splunk-reserved keys are forbidden**: `source`, `sourcetype`, `host`, `index`, `time`, `_time`, `_raw`, `event`. CI vendor → `ci_system`. The handler renames them to `splunk_<key>` as a safety net, not a licence.
- **Field names generic across sources** (`event_type`, `status`, `phase`, `delivery_id`, `team`, `repo`, `commit_sha`). Never pre-namespace with the source — `webhook_source` already disambiguates. Namespace only on a genuine collision of meaning.
- **Exactly one `msg=webhook_processed` per request** that authenticates and parses: `webhook_source` ∈ {bitbucket,pipeline,argocd,noergler}, `outcome` ∈ {accepted,deduped,ignored,skipped}, `delivery_id`, `team`, plus source-specific fields. Include `delivery_id` even on ignored / skipped so triage has a key.
- **`outcome=deduped`** comes from `RETURNING delivery_id` returning no row. Preserve when adding sources.
- **Persist failures**: log `webhook_persist_failed` with the error and answer 500. Never swallow.
- **Access log** binds `request_id` (from a safe `X-Request-Id`, else generated, echoed back) with `logging.With`, so every line of the request carries it; one `http_request` line per request. `/health` and `/ready` silenced.
- Splunk `props.conf` is owned by the platform team; reference copy in [`docs/splunk-props.conf`](docs/splunk-props.conf).

## OpenShift layout

`openshift/` is suite-level, one directory per component. New component → own `openshift/<component>/kustomization.yaml`, added to `resources:` in `openshift/kustomization.yaml`. Every container: explicit cpu+memory `requests` AND `limits`, no exceptions. `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, never a fixed `runAsUser` — OpenShift assigns a random UID per project. Migrations run as the `migrate` init container.

## Out of v1

Push back unless the user is explicit:
- Change failure rate / failed deployment recovery time — no reliable incident source; schema leaves room for rollback-proxy detection
- Backfill workers (ingestion is forward-only)
- Aggregation API or metric endpoints (reads are SQL, or a future sibling component)
- Helm chart (Kustomize suffices)
- Postgres manifests
