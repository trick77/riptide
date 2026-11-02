# AGENTS.md

## Commands

```bash
docker compose up -d db              # Postgres for the DB tests
export RIPTIDE_TEST_DSN='postgres://riptide:riptide@localhost:5432/riptide?sslmode=disable'
cd backend && go test -race ./...    # DB tests skip without RIPTIDE_TEST_DSN
```

`docker ps` fails → ask the user to start OrbStack.

## Architecture invariants

- **Raw payload always stored**, whole body, even fields already extracted into columns. Don't drop unused fields.
- **`riptide.json` is config, not data** (teams + org-wide automation rules, edited via PR, hot-reloaded). Never move it into Postgres.
- **Team keys: separate file**, mounted in production, never committed. The bearer **is** the team identity: every webhook tagged `team = caller_team`.
- **No `service` column, no `service_id` on the wire.** Aggregate per source by repo / pipeline / app name, org-wide by `team`. It served only single-pane labelling and was dropped; never propose it again.
- **Metrics computed on read.** No aggregation tables, no rollup jobs in v1. Schema additions preserve raw events.
- **Deploy↔commit join = full image reference**, per `docs/correlating-deploys-to-commits.md`. Image **tags are not SHAs**: measured 0 of 4 936 refs, all semver; never parse a SHA out of a tag.
- **Read the README's "Reading the event tables" before writing a query.** `repo:refs_changed` is ref movement (~15 000 of 16 295 master events were release tooling), and `change_type` means something only on PR events. Never infer intent from an event-type name.
- **Lead time per `docs/dora-lead-time.md`**: per commit, bots their own line. Never quote the newest-commit-per-release shortcut: measured 26.7 h against a real 193.6 h.
- **`change_type` on Bitbucket events only.** Don't denormalise onto pipeline / Argo rows; join at read time.
- **Automation: config is the last resort.** Senders declare themselves (`reviewer_handle` / `actor_handle` + account kind); only accounts nobody reports get an `automation` entry.
- **CI events are source-tagged, not source-routed.** Every CI lands in `pipeline_events` via `POST /webhooks/pipeline`, told apart by `source`. No per-CI tables or endpoints.
- **Noergler carries finops + reviewer-precision only.** Never re-emit PR lifecycle: `bitbucket_events` covers it. A rollup is cumulative and a declined PR reopened and merged emits again: read spend via `noergler_pr_rollups`, never a SUM over `noergler_events`. Known gap: declined (or deleted) again after a reopen emits nothing. `pr_completed` is also the source for PR diff size (Bitbucket webhooks carry none).
- **Senders verify at startup via `GET /auth/ping`** (authenticated, returns the team) so a wrong token fails fast. Never reuse `/health` or `/ready` (both unauth).
- **Database external, no Kubernetes here.** riptide ships no Postgres, manifests, Helm, Kustomize or cluster helper scripts; deployment lives elsewhere.

## Repo conventions

- **No web framework, no ORM, no logging library.** `net/http` ServeMux, pgx with raw SQL, `log/slog`. Do not add one.
- **Layering.** `internal/parse` is pure (typed `*Draft` or a validation error, no HTTP / DB / config); `internal/api` does HTTP, auth, config-derived fields, persist. Never extract in a handler.
- Bitbucket parsing is permissive (its shapes vary); the other sources are strict schemas.
- Authenticate before reading the body: a bad body behind a bad key is a 401, never a 422.
- Response bodies are structs, not maps: `encoding/json` sorts map keys.
- `api/openapi.yaml` is the contract, tested against the real mux: a handler change and its spec change land in the same commit.
- `backend/` is the collector. A future suite component gets its own top-level directory and module, not a package in this one.
- Tests: real Postgres, never SQLite.
- `ci/` gate scripts come from the repo family (noergler, loom) but call Go helpers instead of Python, so CI runs no Python. Keep it that way.

## Logging & Splunk

- Everything logs through `logging.Setup()`'s logger; never write to stdout otherwise (one JSON object per line, Splunk auto-extracts).
- **Field names generic across sources** (`event_type`, `status`, `delivery_id`, `team`, `repo`, `commit_sha`). Never pre-namespace with the source; `webhook_source` disambiguates. Namespace only on a genuine collision of meaning.
- **Exactly one `msg=webhook_processed` per request** that authenticates and parses, with `delivery_id` even on ignored / skipped so triage has a key. Preserve when adding sources.
- **Persist failures**: log `webhook_persist_failed` and answer 500. Never swallow.
- Splunk `props.conf` is owned by the platform team; reference copy in `docs/splunk-props.conf`.

## Out of v1

Push back unless the user is explicit:
- Change failure rate / failed deployment recovery time: no reliable incident source; schema leaves room for rollback-proxy detection
- Backfill workers (ingestion is forward-only)
- Aggregation API or metric endpoints (reads are SQL, or a future sibling component)
