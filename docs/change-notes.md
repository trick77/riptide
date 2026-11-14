# Change notes

What an operator has to do after deploying a change, and why. Newest first.
Code-only changes are not listed — only the ones that need action outside the
repository.

Real account handles, hostnames and tokens belong in the mounted
`riptide.json` / team-keys Secret, never in this file: the repository is public.

## 2026-11-14 — the UI

**The UI listener now serves the UI on `/`** next to `/api`, from the same
binary and image. Nothing to configure: put the auth proxy sidecar in front
of `BACKEND_UI_ADDR` and give it a Route; the webhook Route stays on the
collector's port. `GET /api/teams` lists the configured teams for the team
picker. The dataset (contract 1.2.0) gains a PR's `title`, a run's
`commit_sha`, a deploy's `new_images` and `meta.data_end`, each source's
latest event.

## 2026-11-13 — UI listener and read API, pipeline build_type

**Migrations 0004 and 0005 run in the init container** (`riptide migrate`,
as every migration). 0004 adds `pipeline_events.build_type` and backfills
it from stored payloads. It also redefines the `deploy_commit_ranges` view,
with the same columns and the same ranges (a range it listed twice is now
listed once): on a year of data it took 14 s, now about 1 s. Ad-hoc queries
on the lead-time views need no change. 0005 adds indexes by team and time on
the four event tables; while each builds, webhook inserts into that table
wait rather than fail (a plain build: a concurrent one deadlocks against a
second migrator, see the file). Nothing else is required.

**A second listener, `BACKEND_UI_ADDR`** (default `127.0.0.1:8080`), serves
the read API (`GET /api/dataset`) and nothing else; the collector on
`BACKEND_LISTEN_ADDR` does not serve `/api`. The default binds loopback, so
without an auth proxy sidecar in the pod nothing can reach it. The read API
does no authentication of its own: put the proxy in front of it, never a
Route or Service. riptide refuses to start on a non-loopback
`BACKEND_UI_ADDR` unless `BACKEND_UI_ALLOW_REMOTE=true` (meant for compose,
where something else keeps the port private), and on a UI port the
collector already binds. If the UI listener cannot bind, `serve` stops, like
it does for the collector's. Every user the proxy lets in can read every
team.

**`build_type`** (optional, pipeline `COMPLETED` events): Jenkins can now
report what kind of build a run was (`release`, `hotfix`, `build`, ...); see
[Setup: Jenkins notification](setup-jenkins-notification.md).

## 2026-11-13 — distroless image, new probes, log field renames

**Every `RIPTIDE_*` variable is now `BACKEND_*`**, suffix unchanged, no
fallback to the old names: `BACKEND_DB_URL`, `BACKEND_CONFIG_PATH`,
`BACKEND_TEAM_KEYS_PATH`, `BACKEND_LOG_LEVEL`, `BACKEND_ENV`,
`BACKEND_LISTEN_ADDR`, `BACKEND_CONFIG_RELOAD_SECONDS`, and
`onboard-bitbucket`'s `BACKEND_TEAM_KEY`. riptide refuses to start while any
old name is still set (environment or `./.env`), naming it.
Senders' own variables (noergler's `RIPTIDE_URL` / `RIPTIDE_TOKEN`, the Jenkins
`RIPTIDE_TOKEN` credential) are theirs and unchanged.

**Switch the probes, the variables and the image in one rollout**: one
Deployment spec change carrying the new tag, the `BACKEND_*` rename and the
probes, never one without the others. The old binary has no `-healthcheck` /
`-readycheck`, the new one no `/health` / `/ready` (404). The new `/healthz`
(liveness) and `/readyz` (readiness: the database answers) answer only a
caller on the pod itself and 404 anyone else, so an HTTP probe from the node
fails. Use exec probes: `/riptide -healthcheck` and `/riptide -readycheck`.
A schema that is behind still stops `serve` at startup. `/readyz` answers
`{"status":"ok"}` only; the team, key and reload-failure counts `/ready`
returned are gone (reload failures stay in the log as `config_reload_failed` /
`team_keys_reload_failed`). A failing readiness check logs
`Readiness check failed` once when it starts failing, not
`readiness_db_unreachable` per probe.

**The image is distroless** (`gcr.io/distroless/static`): no shell, no wget,
no `apk`. It runs as `65532:65532` (was `1001`); OpenShift's random uid still
works, the binary writes nothing to disk.

**Log fields** (Splunk searches and dashboards): the error field is `err`, not
`error`; the access line `http_request` carries `status`, not `status_code`,
plus `ip`, `proto` and `user_agent`, and logs a 4xx (but 401/404) at `warning`
and a 5xx at `error` instead of everything at `info`. `msg`, `service`,
`version` and `env` keep their names; the key order after `timestamp` changed.
Every `msg` name riptide emitted before (`webhook_processed`,
`webhook_persist_failed`, `migration_applied`, ...) is unchanged.

**Releases are by hand**: the Release workflow (`workflow_dispatch`, bump
patch/minor/major/auto); a master push no longer releases. Images keep the
name `ghcr.io/trick77/riptide-collector`, now signed (cosign) and scanned.
Dependabot PRs are no longer auto-merged: merge them by hand.

**`RIPTIDE_VERSION` and `OPENSHIFT_BUILD_COMMIT` are no longer read**: the
version is stamped at build time only (`riptide version`, the `version` log
field).

## 2026-11-13 — several images per CI run

**`pipeline_events.image_refs` replaces `image_ref` for reading.** One release
can push several images (a monorepo UI building three apps, a multi-module
service), and a run must stay one row: an event per image would count as extra
builds and add its duration to CI time again. Migration
`0003_pipeline_image_refs` adds the list and fills it from `image_ref` and from
an `image_refs` an older collector kept only in the payload; `riptide migrate`
applies it, no manual step. Switch saved queries to the join in
[Correlating deploys back to commits](correlating-deploys-to-commits.md).

**`image_ref` stays as a column for one release,** no longer written, so old
pods mid-rollout and a rollback keep inserting. An insert trigger fills
`image_refs` for the rows they write, from `image_ref` or the payload. A later
migration drops the column and the trigger.

**Senders that publish several images send `image_refs`** in the same
`COMPLETED` event. `image_ref` stays accepted on the wire and is merged in, so
single-image senders need no change. Order does not matter: rows written before
the collector is updated are picked up by the backfill, rows an old pod writes
during or after the rollout by the trigger.

## 2026-09-24 — the collector is now Go

**Point the collector at a new, empty database.** The Go collector keeps its
own migration history (`schema_migrations`) and does not read the Alembic one;
its first migration creates the whole schema and fails on a database that
already has the tables. History restarts, as it did for noergler. To keep the
old rows, export them first with `scripts/export-tables.sh` (against the old
database, before switching) and reload them with the `\copy` commands in the
archive's README.txt. The counter columns are `BIGINT` now; the CSVs load
unchanged.

**Run `riptide migrate` before `serve`.** A deployment whose init container
still runs the old `alembic` migration needs this change. The app refuses to start while a migration is pending.

**`RIPTIDE_DB_URL` may stay as it is.** The `postgresql+asyncpg://` form is
still accepted (the driver suffix is stripped); plain `postgres://` is the form
to use going forward. Add `?sslmode=disable` only where the database has no TLS.

**The first Go image is 0.2.0**, released by hand as a minor bump; pushes to
master release automatically again from there.

Behaviour that changed on the wire, all of it fixes:

- Bitbucket deliveries use `X-Request-Id` (what Bitbucket Data Center sends)
  as the delivery id. Without it the id is derived from the body, so two
  different events no longer collapse into one. `X-Hook-UUID`, which names the
  webhook rather than the delivery, is no longer used.
- Bitbucket's "Test connection" ping, empty bodies and non-JSON bodies are
  answered `202 ignored` and logged as `outcome=skipped`, not stored.
- Jira keys in branch names (`feature/ABC-123-...`) are found. They were
  missed because the branch was lowercased first.
- Argo CD's `finished_at: ""` for a running sync is accepted as absent instead
  of a 422 that dropped the event. `""` counts as absent for every optional
  field of the owned contracts, `status` included.
- `payload` holds the request body as received for every source; for
  pipeline, Argo CD and noergler it used to be the validated model.
- Config and team keys reload every `RIPTIDE_CONFIG_RELOAD_SECONDS` (30),
  whichever endpoint is busy; before, Argo CD only saw a changed
  `ignored_stages` after a Bitbucket delivery. A reload that leaves a
  configured team without keys, or gives two teams the same key, is rejected
  and logged.
- Bodies over 1 MiB (10 MiB for Bitbucket) get a 413; numbers too large for
  their column get a 422 instead of a 500; so does JSON that Postgres cannot
  store as JSONB (a `\u0000` escape), which Bitbucket gets answered as
  `202 ignored`. `/ready`'s 503 no longer echoes the database error.
- 422 details list `type`, `loc` and `msg` per field; pydantic's `input`, an
  echo of the whole request body, is gone.

## 2026-09-08 — bot and service-account detection

**Add the accounts nobody declares to the production `riptide.json`.**

Detection now runs in this order: configured `automation` authors → the git
host's own verdict (`type: SERVICE`) → the `*-bot` name shape → identities a
sender declares about itself. Only accounts that reach none of those need a
config entry.

- **Renovate** (or any dependency bot) — needs an entry. Its account is an
  ordinary user to Bitbucket, and it POSTs nothing to riptide, so nothing
  declares it. Add **both** the login handle and the display name to
  `automation.renovate.authors`; matching is case-insensitive and covers either
  field. Without it, only its `renovate/` branches are recognised and its
  pushes to the integration branch count as human activity.
- **The Bitbucket system user** — no entry needed. The server marks it
  `type: SERVICE` and riptide tags it `service-account` on its own.
- **The CI account** — no entry needed once the pipeline sender reports
  `actor_handle`; see below.

Rows already ingested keep the `is_automated` they were written with — the
tables are append-only. Read-time queries filter historical rows through the
`non_human_identities` CTE in the README's pickup-time query.

**Redeploy the collector.** Anything older than the draft→ready parser fix
stores raw `pr:modified` rows and emits no `pr:ready_for_review`, so PRs opened
as drafts have no clock-start for review pickup time. Verify after rollout:
`pr:ready_for_review` rows appear and no-op `pr:modified` rows stop.

## 2026-09-08 — deploy-to-commit correlation

**Have CI senders report `image_ref`.** The full image reference
(`registry/path:tag`), which Argo CD stores verbatim in `payload->'images'`.
This is the only exact link from a deploy back to its build and commit: image
tags are versions, not commit SHAs, and `argocd_events.revision` is the
GitOps-repo SHA. Until senders report it, lead time and per-ticket flow run on
the release-note fallback, whose coverage decays as releases move to direct
version-bump commits. See
[Correlating deploys back to commits](correlating-deploys-to-commits.md).

**Have CI senders report `actor_handle`** (+ `actor_account_kind: service`) —
the git account the CI system commits and pushes as. A CI account can author a
third of all repository events; declared, they leave human-activity metrics
without anyone naming the account in config.

Both fields are optional and additive; senders that omit them keep working.

## 2026-09-08 — noergler integration

**Deploy riptide before noergler.** The rollup schema rejects unknown fields,
so a collector without `reviewer_handle` answers *every* rollup with HTTP 422,
not just the ones carrying a new field — and a rejected rollup is never
retried, because the PR is marked as emitted when it is claimed. Anything
closed in that window is lost.

Once both sides are deployed, noergler declares the account it reviews under,
and riptide filters its comments out of code-review pickup time without any
per-installation configuration.

## 2026-09-08 — migrations

`0003` adds nullable columns (`author_display_name`, `image_ref`,
`actor_handle` / `actor_account_kind`, `reviewer_handle` /
`reviewer_account_kind`); `0004` adds the lead-time views and one index. Both
are applied by the init container on deploy — no manual step, no downtime, and
`downgrade` is clean if a rollback is needed.
