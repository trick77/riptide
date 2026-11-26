# Change notes

What an operator has to do after deploying a change, and why. Newest first.
Code-only changes are not listed — only the ones that need action outside the
repository.

Real account handles, hostnames and tokens belong in the mounted
`riptide.json` / team-keys Secret, never in this file: the repository is public.

## 2026-11-26 — the page's words, numbers per repo

**Rules version 4**: the state titles, the words and the levers the model
is told changed (Default-Branch, Projekt, Library, Rollout). Analyses stored
under version 3 are outdated and not shown until assessed again: one model
call per team, at the next Monday run or on "Neu einschätzen". No
migration, no config change.

**`/api/metrics` sets the numbers out per repo** (`repos`) and week by week
(`trends`), from the rows the request reads anyway: no new database read.
Watch `GET /api/metrics` durations after the deploy. A compared number
without a comparison now names its cause (`reason` `source_started`,
`lost`, `first_deploys`, with the source and dates); a client reading
`incomplete` for those sees the new codes.

## 2026-11-26 — states: findings judged against the team's thresholds

**`riptide.json` takes an optional top-level `thresholds` block**: the
values the assessment's states fire at (`release_breach_days`,
`not_on_prod_days`, `approved_wait_days`, `bot_loop_failures` and the rest;
the list with the starting values is in [metrics.md](metrics.md), "States").
Absent, the starting values apply; an unknown key, a value of 0 or below, a
share over 1, a fractional count, or a value past what the states' 26-week
look-back holds (182 days, 130 working days, 26 weeks; 1440 minutes;
`silent_builds_days` 15) refuses the config (hot reload keeps the old one).
Each card prints the value it measured against. A merge pause counts the
PRs open with a person's approval at the week's end, however long they
waited: there is no `merge_pause_wait_days`.

**Rules version 3**: analyses stored under version 2 are outdated and not
shown until assessed again. The assessment reads more per request: the
weeks before the compared periods back to 26 weeks before the period's end
(PRs and deploys, in parallel with the compared periods), the approvals
since 30 days before that look-back in every view, and the branches'
pushes (trunk and bots' pushes left out before their payloads are read),
with the first PR into each branch; no migration, no new
index. On a small database the planner scans `bitbucket_events`; with more
rows the existing `(team, occurred_at)` and `(repo_full_name, branch_name,
occurred_at)` indexes serve them. Watch `GET /api/analysis` durations after
the deploy.

**A branch merged onto its trunk without a PR** is no branch "without a PR"
any more: the branches' read also reads the trunk pushes (tip, listed
commits and their parents) of the repos with a branch without a PR, from
that branch's last push on, once per request; a branch cut from the trunk
is found by its head's SHA index. Watch `GET /api/analysis` on a database
with many release-tooling pushes to the trunk.

**A bot's branch's builds are bots' builds on every page** (a branch whose
every push is a bot's: flagged at ingest, its head commit by an `.invalid`
e-mail identity, or its pusher or head commit automation, the config's
automation `authors` included; never by its last push alone): the build
numbers of past periods drop them once with this change, and the bot
CI-time line takes them. Every page reads the branches' pushers once per
request (pushes off the trunk; a flagged push's payload is not read).

**A branch named after its ticket without a prefix** (`KAS-12-login`) is a
feature branch now: its builds count as feature builds, its PRs (stored as
change type "other") as features, on read, past periods too. `release-12`,
`renovate-3`, `PR-7` and the like are not.

## 2026-11-26 — lead time per PR, traced to production

**Migration 0017** adds the view `repo_trunks` and redefines
`commit_sightings` (same columns): a repo's trunk is master, main or the
branch most of its merged PRs go into, no longer master alone. No index,
no table change: an old pod keeps working, its per-commit lead-time read
somewhat slower while both run.

**The lead time is per PR now**, from its first commit to the first
production deploy that shipped it, over the PRs merged in the period; the
per-commit number and its bot line are gone. Many more PRs reach
production: a release whose compare link starts before riptide's data, a
repo whose trunk is main, and an image's build are traced too (see
[dora-lead-time.md](dora-lead-time.md)).

**Closed periods are frozen at their end**: a deploy after it moves no
number of the period. Numbers of past periods change once with this
change; stored analyses keep their wording until assessed again.

**`coverage.reasons` and `prod_reason` take a fifth reason, `off_trunk`**:
a PR merged into a branch no PR had merged into the trunk by the cutoff
is not placeable, no longer "not on production yet"; so is a PR on a
develop trunk not merged on to master by then while master's releases went
out resolved. A client that lists the reasons by a fixed set has to add it.

## 2026-11-25 — counting as it happened, and saying what is missing

**Migrations 0012 to 0016** build five indexes, one per migration, so a
build holds only its own table: `(team, created_at)` on pipeline, Argo CD
and noergler events (silences are read off when an event was stored),
then on `bitbucket_events` the lowercased merge commit of `pr:merged`
events (the deploy's product, the GitOps repos and the silences probe it
per SHA instead of reading every payload) and its `(team, created_at)`,
last. Plain `CREATE INDEX` in the migration's transaction, as 0005: each
blocks INSERTs into its table while it builds. A webhook waits for the
build; Bitbucket gives up after its timeout and never retries, so a
webhook held longer is lost. The build time on a full table is not
measured: deploy outside the busiest hour, and look at the data line for a
Bitbucket silence across the deploy afterwards.

**Infrastructure Apps are derived, not configured.** An App is
infrastructure when none of its deploys over 26 weeks names a product.
Symptom of a misclass: Apps of a real product listed under "Deploys von
Infrastruktur-Apps nicht gezählt" on the deploy tile (its title lists them).
Cause: a GitOps repo of the team that sends Bitbucket no webhook. Remedy:
add the webhook to that GitOps repo (`POST /onboard/{team}`); from its next
commit on, its Apps have a product again.

**Library repos need no builds any more.** A repo that no compare link
names, that built no image and after which no deployed image is named is a
library; its PRs leave the base of the PRs traced to production. A product
of libraries only says so instead of "nothing went to PROD".

**Migration 0011** adds `team_analyses.rules_version`. Every analysis
stored before it is of older rules: the page no longer shows its text or
cards, shows the current rules' view with a note instead, and offers to
assess again. The schedule does not re-word a week it already worded, so
click "assess again" on the views people read, or wait for each view's
next scheduled run.

**Numbers of past periods change once**, since every metric is computed on
read: an App's first production deploy counts as a release once Argo CD has
reported for the team over 14 days; Argo CD's `Error` phase counts as a
failed deploy; infrastructure Apps (none of their deploys names a product)
count nowhere, and a deploy without a product of its own is its App's
product's; the umbrella PR that carried other merged PRs and housekeeping
PRs across many repos are no merged work, while PRs into a feature branch
or merged along with an umbrella now count; library repos' PRs leave the
base of the PRs traced to production; the builds of bot branches
(`branch_prefixes`) count in no build number; open PRs whose close was
lost in a Bitbucket outage are of unknown state, not open; a dependency
bot provisioned as an ordinary user is recognised by its name. Both
periods of a comparison are counted alike, an App classed by the Apps as
of the period's end, as the stored assessment classes it; see
[metrics.md](metrics.md).

**Name automation sources by their tool** in `riptide.json`: an account
whose login or display name holds a source's name as whole words now counts
as that bot, at read time only (ingest writes what it wrote before). A
source named with an ordinary word would catch people; one under 4
letters is logged at load as `automation_source_name_short`. Every source's
`branch_prefixes` now also mark that bot's CI runs.

## 2026-11-18 — Bitbucket webhooks self-service, `onboard-bitbucket` gone

**`POST /onboard/{team}` replaces `riptide onboard-bitbucket`.** A team
reports, creates, updates or removes its webhooks itself, with its
`bitbucket` key as a bearer and its own Bitbucket token in
`X-Bitbucket-Token`, per project (one hook for every repo, new ones
included) or per repo; see
[setup-bitbucket-webhook.md](setup-bitbucket-webhook.md). Onboarding a
project deletes the team's repo hooks below it.

**Set `BACKEND_PUBLIC_URL`** (riptide's base URL as Bitbucket reaches it) and
**`BACKEND_BITBUCKET_URL`**; without both `/onboard` answers 503. The route
in front of riptide must pass `/onboard` without a login, like
`/webhooks`, and riptide must reach Bitbucket outbound. Onboarding a project
of dozens of repos lists every repo's hooks: give that route a router
timeout of 300 s; riptide stops one request's work at 270 s.

**After a host move run `onboard` once per team**: hooks named `riptide`
delivering to the team's path are rewritten whatever host they point at.
Bitbucket redelivers nothing, so what went to the old host is lost.

## 2026-11-16 — Jira keys link to their tickets

**`riptide.json` takes `jira.base_url`**, the Jira instance's root URL. Every
Jira key in the PR table, in the title or before it, links to
`<base_url>/browse/<KEY>`. Bitbucket payloads name no Jira instance, so set it
in the production `riptide.json`; it is hot-reloaded. Unset, the keys are
shown without a link.

```json
"jira": { "base_url": "https://jira.example.com" }
```

## 2026-11-15 — projects left out of the metrics

**`riptide.json` takes a top-level `excluded_projects`**, a list of
Bitbucket project keys the metrics leave out: shared libraries, pipeline
definitions, tooling. Add the key of such a project to the production
`riptide.json`; it is hot-reloaded and applies to past periods too, the
events stay stored. See
[onboarding-a-team.md](onboarding-a-team.md#1-add-the-team-to-the-config).

## 2026-11-15 — the wait for a build agent

**Migration 0006** adds `pipeline_events.agent_started_at` and fills it from
the payload of rows that already carry the field; a value that does not
parse stays NULL. Nothing to configure.

**CI senders may send `agent_started_at`** (when the run got its build agent,
see [setup-jenkins-notification.md](setup-jenkins-notification.md)). The
build section then shows the wait for an agent and the build apart, over the
runs that send it; a run without it keeps one duration, wait included.

## 2026-11-15 — the image runs from /

**The image's working directory is `/`**, no longer the distroless
`/home/nonroot`. Under OpenShift's random uid riptide could not even look
into that 0700 directory for its optional `./.env` and refused to start; a
`workingDir: /` set in a deployment as a workaround can go. The relative
default paths `config/riptide.json` and `config/team-keys.json` now resolve
under `/config/`: a deployment that mounted them under `/home/nonroot/config/`
without setting `BACKEND_CONFIG_PATH` / `BACKEND_TEAM_KEYS_PATH` sets both
(compose.yaml does already).

## 2026-11-14 — metrics computed in the backend

**`GET /api/dataset` is gone** (contract 2.0.0). The UI now reads
`GET /api/metrics` and `GET /api/facts`, computed in the backend; nothing
to configure. Anything else that read `/api/dataset` through the auth proxy
has to move to those two.

## 2026-11-14 — riptide-collector is now riptide

**The image is `ghcr.io/trick77/riptide`**, no longer
`ghcr.io/trick77/riptide-collector` (whose tags up to 0.5.0 stay). Point the
mirror and the deployment at the new name; the deployment's own objects
(Deployment, Service, Routes, Secrets) may follow, the commands in these
docs assume `deployment/riptide`.

**Splunk: `service=riptide`** on every log line instead of
`riptide-collector`, the start and stop lines are `msg=riptide_starting` /
`riptide_stopped`. The binary sets no sourcetype; the pod annotation
`splunk.com/sourcetype` does. The reference stanza is now `riptide:json`
(was `riptide:collector:json`, see [splunk-props.conf](splunk-props.conf)).
Order matters: the platform team adds the `[riptide:json]` stanza first,
keeping the old one, then the annotation changes with the deployment; the
old stanza goes once nothing logs under it. Saved searches on the old names
need the new ones.

The binary, its commands, the env variables and the API are unchanged.

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
no `apk`. It runs as `65532:65532` (was `1001`); the binary writes nothing to
disk. Under OpenShift's random uid it did not start until the image ran from
`/`: see 2026-11-15, the image runs from /.

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
`non_human_identities` CTE in the pickup-time query in
[metrics.md](metrics.md#pickup-time-query).

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
