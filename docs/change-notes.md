# Change notes

What an operator has to do after deploying a change, and why. Newest first.
Code-only changes are not listed — only the ones that need action outside the
repository.

Real account handles, hostnames and tokens belong in the mounted
`riptide.json` / team-keys Secret, never in this file: the repository is public.

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
