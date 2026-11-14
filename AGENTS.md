# AGENTS.md

## Commands

```bash
make check                           # the gate (ship check): starts the compose db image itself
cd backend && go test -race ./...    # DB tests skip without SHIP_TEST_DSN (compose `db`)
```

`docker ps` fails → ask the user to start OrbStack. Tests only in `internal/testdb`'s per-test schemas: a local `public` may hold real data.

- Commit subjects + PR titles: Go style `area: what` (`api: …`, `store: …`), gated by ship; release notes list them by area.
- Fresh clone: `ship hooks` before the first push (check, conform and init install it too); without the pre-push hook a push skips the gate.
- Schema: read `docs/schema.md` first (generated, regen in its header).

## Architecture invariants

- **Raw payload always stored**: whole body, even fields extracted into columns.
- **`riptide.json` is config, not data** (teams + automation rules, PR-edited, hot-reloaded). Never into Postgres.
- **Team keys: separate file**, mounted in prod, never committed. The bearer **is** the team identity: every webhook tagged `team = caller_team`.
- **No `service` column, no `service_id` on the wire.** Aggregate by repo / pipeline / app, org-wide by `team`. Dropped; never re-propose.
- **Metrics computed on read.** No aggregation tables, no rollup jobs in v1.
- **Deploy↔commit join = full image reference**, per `docs/correlating-deploys-to-commits.md`. Image **tags are not SHAs**: measured 0 of 4 936 refs, all semver; never parse a SHA out of a tag.
- **Read the README's "Reading the event tables" before any query.** `repo:refs_changed` is ref movement (~15 000 of 16 295 master events were release tooling), and `change_type` means something only on PR events. Never infer intent from an event-type name.
- **Lead time per `docs/dora-lead-time.md`**: per commit, bots their own line. Never quote the newest-commit-per-release shortcut: measured 26.7 h against a real 193.6 h.
- **`change_type` on Bitbucket events only.** Never denormalise onto pipeline / Argo rows; join on read.
- **Automation: config is the last resort.** Senders declare themselves (`reviewer_handle` / `actor_handle` + kind); `automation` entries only for accounts nobody reports.
- **CI events are source-tagged, not source-routed**: every CI posts to `POST /webhooks/pipeline` → `pipeline_events`, told apart by `source`. No per-CI tables or endpoints.
- **Noergler: finops + reviewer precision only**, never PR lifecycle. Rollups are cumulative and re-emitted on reopen: spend via `noergler_pr_rollups`, never SUM over `noergler_events`. Gap: a second decline after a reopen emits nothing. PR diff size comes from `pr_completed` (Bitbucket sends none).
- **Senders verify at startup via `GET /auth/ping`** (authenticated, returns the team): a wrong token fails fast. Never `/healthz` / `/readyz` (kit's, loopback only).
- **Database external, no Kubernetes here**: no manifests, Helm, Kustomize, cluster scripts.

## Repo conventions

- **No web framework, no ORM, no logging library.** `net/http` ServeMux, pgx raw SQL, `log/slog`, ship's kit only.
- **Error bodies stay `{"detail": …}`** (senders read it): never kit's `WriteError` / `Recover` (`{"error": …}`).
- Env vars `BACKEND_*` (2026-11); old `RIPTIDE_*` names refuse startup. Senders' `RIPTIDE_TOKEN` / `RIPTIDE_URL` are theirs: leave them.
- **Layering.** `internal/parse` is pure (`*Draft` or validation error; no HTTP / DB / config); `internal/api` does HTTP, auth, config-derived fields, persist. Never extract in a handler.
- Bitbucket parsing permissive (shapes vary), other sources strict.
- Authenticate before reading the body: a bad body behind a bad key is a 401, never a 422.
- Response bodies are structs, not maps: `encoding/json` sorts map keys.
- `api/openapi.yaml` is the contract, tested against the real mux: handler + spec change in one commit.
- Tests: real Postgres, never SQLite.
- Migration lock key `0x72697074` predates kit/migrate: never change it, old and new binaries must serialise.

## Logging & Splunk

- All logs via `setupLog`'s kit Splunk logger; nothing else writes stdout (one JSON object per line, Splunk auto-extracts).
- **Field names generic across sources** (`event_type`, `status`, `delivery_id`, `team`, `repo`, `commit_sha`); `webhook_source` disambiguates. Namespace only on a real clash of meaning.
- **Exactly one `msg=webhook_processed` per authenticated, parsed request**, with `delivery_id` even on ignored / skipped (triage key), new sources included.
- **Persist failure**: log `webhook_persist_failed`, answer 500, never swallow.
- Splunk `props.conf` is the platform team's; reference copy `docs/splunk-props.conf`.

## UI (`ui/`, embedded)

- `ui/src/metrics.ts` reapplies each README query's filters (the API serves supersets with flags); percentiles = `percentile_cont`.
- **Build numbers never across run kinds.** User: releases skip tests, feature builds fail more.
- Screenshots or numbers from a real import never into the repo or a PR.

## Out of v1

Push back unless the user is explicit:
- Change failure rate / recovery time: no reliable incident source (schema has room for a rollback proxy)
- Backfill workers (ingestion is forward-only)
- Aggregates or metric endpoints: `/api` serves per-team facts, UI listener only (loopback); the UI computes
