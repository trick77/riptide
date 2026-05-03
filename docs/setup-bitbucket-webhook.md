# Setup: Bitbucket webhook

Wire a Bitbucket Data Center repository to send PR + push events to
riptide-collector. Authentication is **HMAC-SHA256** via BBS's native
`configuration.secret`; the team's secret is the `bitbucket` entry in
`team-keys.json`.

The canonical path is `scripts/bitbucket_onboarding.py` — manual UI
configuration is the fallback only. The script provisions the webhook
with HMAC, the correct event set, and the correct per-team URL
(`/webhooks/bitbucket/{team}`).

## Prerequisites

- Repo admin access to the Bitbucket repository (or use the script with a
  REPO_ADMIN token).
- The riptide-collector base URL for your environment (ask the platform
  team).
- **Your team's Bitbucket HMAC secret** — the value stored under
  `<team>.bitbucket` in `team-keys.json`. The platform team hands it out
  during onboarding (see [`onboarding-a-team.md`](onboarding-a-team.md)).
  This secret authenticates Bitbucket webhooks **only** — the team's
  ArgoCD / Jenkins bearers are separate values.

## Path A: onboarding script (recommended)

```bash
export BITBUCKET_TOKEN=...     # repo-admin REST token
export RIPTIDE_TEAM_KEY=...    # team's `bitbucket` HMAC secret
uv run python scripts/bitbucket_onboarding.py path/to/team-config.json
```

The script is idempotent (rerun after edits), supports `--dry-run` and
`--remove`, and surfaces every diff before applying. See
`scripts/bitbucket-onboarding.example.json` for the input shape.

## Path B: manual UI fallback

1. **Repository settings → Webhooks → Add webhook**.
2. **Title**: `riptide`.
3. **URL**: `https://riptide-collector.<env>.example.com/webhooks/bitbucket/<team>`
   — note the trailing `/<team>` segment; team identity is read from the
   path.
4. **Status**: Active.
5. **Skip certificate verification**: leave **off**.
6. **Secret**: paste the team's `bitbucket` HMAC secret (the `RIPTIDE_TEAM_KEY`
   value). BBS uses this to sign each delivery.
7. **Triggers** — select:
   - Repository: **Push**
   - Pull request: **Created**, **Updated**, **Approved**, **Merged**,
     **Declined**, **Comment created**
8. **Save**.

Do **not** use the `Custom headers` field for auth and do **not** populate
the top-level `credentials` block via REST — BBS DC silently drops
`credentials.password` on REST POST/PUT.

## Verify

1. Trigger a small event (e.g., push a commit to a PR).
2. Check the collector logs:
   ```bash
   oc logs -n riptide deployment/riptide-collector --tail=50 | grep bitbucket_event_received
   ```
3. You should see a line with the `delivery_id`, `event_type`, `repo`, and
   `team` matching what you triggered. `team` is the segment from the
   webhook URL.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Bitbucket shows `401` with `Invalid signature.` | Wrong/missing HMAC secret in BBS, or wrong team segment in the URL. Re-run the onboarder, which always rewrites the secret (BBS redacts it on read-back). |
| Bitbucket shows `422` | Malformed payload — open an issue with the delivery UUID |
| No log line at all | Webhook URL wrong, or network policy blocks Bitbucket → cluster |
