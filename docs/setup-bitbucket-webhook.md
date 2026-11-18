# Setup: Bitbucket webhook

Wire a Bitbucket Data Center project or repository to send PR + push events
to riptide. Authentication is **HMAC-SHA256** via BBS's native
`configuration.secret`; the team's secret is the `bitbucket` entry in
`team-keys.json`.

The canonical path is `POST /onboard/{team}`, self-service: riptide
provisions the webhook with HMAC, the event set and the per-team URL
(`/webhooks/bitbucket/{team}`) itself. Manual UI configuration is the
fallback only.

## Prerequisites

- **Your own Bitbucket HTTP access token** with admin on the projects (or
  repos) to wire. Used for the one request, never stored.
- **Your team's Bitbucket HMAC secret** — the value stored under
  `<team>.bitbucket` in `team-keys.json`. The platform team hands it out
  during onboarding (see [`onboarding-a-team.md`](onboarding-a-team.md)).
  This secret authenticates Bitbucket webhooks and `/onboard` **only** — the
  team's ArgoCD / Jenkins bearers are separate values.
- The instance runs with `BACKEND_PUBLIC_URL` (its base URL as Bitbucket
  reaches it) and `BACKEND_BITBUCKET_URL`; without them `/onboard` answers
  503.

## Path A: `POST /onboard/{team}` (recommended)

```http
POST https://riptide.example.com/onboard/<team>
Authorization: Bearer <team's bitbucket key>
X-Bitbucket-Token: <your Bitbucket token>
Content-Type: application/json

{
  "action": "onboard",
  "projects": [
    {"key": "PLAT"},
    {"key": "~JDOE", "repos": ["tool"]}
  ]
}
```

- **A project without `repos`** gets one project webhook: every repo in it
  delivers, new ones included. Onboarding it deletes the team's repo webhooks
  below it, which would deliver every event a second time (each delivery has
  its own id, nothing dedupes them).
- **A project with `repos`** gets one repo webhook each: a personal `~USER`
  project, or a project shared with another team. A project webhook sends
  every repo's events to your team, the other team's included.
- `action`: `status` (default, writes nothing), `onboard`, `remove`.
  `"dry_run": true` reports what onboard or remove would write.
- Idempotent: rerun after edits. Every run rewrites the secret, since
  Bitbucket never returns it.
- A webhook named `riptide` that delivers to your team's path is yours on any
  host: after a host move, one `onboard` points them all at
  `BACKEND_PUBLIC_URL`. One delivering to another team's path is left alone
  and listed; yours goes next to it.

The answer lists every target with its status, plus `text`, the same as a
table. `healthy` is false when a status row is not `ok` or an onboard row
failed. The full contract: `/docs` on the instance.

## Path B: manual UI fallback

1. **Repository settings → Webhooks → Add webhook**.
2. **Title**: `riptide`.
3. **URL**: `https://riptide.<env>.example.com/webhooks/bitbucket/<team>`
   — note the trailing `/<team>` segment; team identity is read from the
   path.
4. **Status**: Active.
5. **Skip certificate verification**: leave **off**.
6. **Secret**: paste the team's `bitbucket` HMAC secret (the `BACKEND_TEAM_KEY`
   value). BBS uses this to sign each delivery.
7. **Triggers** — select:
   - Repository: **Push**
   - Pull request: **Opened**, **Source branch updated**, **Modified**,
     **Approved**, **Unapproved**, **Needs work**, **Reviewer updated**,
     **Merged**, **Declined**, **Deleted**, **Comment added**
8. **Save**.

The **Modified** trigger is required for DX Core 4 pickup-time accuracy on
PRs opened as drafts: BBS emits `pr:modified` on title / description /
target / draft changes, and the parser keeps only the draft flips: draft→ready
(re-typed as a synthetic `pr:ready_for_review` row) is the clock-start signal
for the pickup metric documented in
[metrics.md](metrics.md#pickup-time-query); ready→draft (`pr:back_to_draft`)
takes the PR out of the long-open count. The
**Reviewer updated** trigger covers silent reviewer-status changes
(adding / removing reviewers without a comment) that also feed the
pickup metric.

Do **not** use the `Custom headers` field for auth and do **not** populate
the top-level `credentials` block via REST — BBS DC silently drops
`credentials.password` on REST POST/PUT.

## Verify

1. Trigger a small event (e.g., push a commit to a PR).
2. Check riptide's log for the `msg=webhook_processed` line of the delivery:
   ```bash
   oc logs -n <namespace> deployment/riptide -c app --tail=50 | grep '"webhook_source":"bitbucket"'
   ```
3. You should see a line with the `delivery_id`, `event_type`, `repo`, and
   `team` matching what you triggered. `team` is the segment from the
   webhook URL.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Bitbucket shows `401` with `Invalid signature.` | Wrong/missing HMAC secret in BBS, or wrong team segment in the URL. Run `onboard` again: it always rewrites the secret (BBS redacts it on read-back). A `status` run lists every hook still pointing elsewhere. |
| Bitbucket shows `422` | Malformed payload — open an issue with the delivery UUID |
| No log line at all | Webhook URL wrong, or network policy blocks Bitbucket → cluster |
