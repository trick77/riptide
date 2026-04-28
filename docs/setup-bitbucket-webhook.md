# Setup: Bitbucket webhook

Wire a Bitbucket repository to send PR + push events to riptide-collector.

## Prerequisites

- Repo admin access to the Bitbucket repository.
- The riptide-collector URL for your environment (ask the platform team).
- The shared bearer token (ask the platform team — stored in the cluster Secret `riptide-collector-secrets`).

## Steps

1. In Bitbucket: **Repository settings → Webhooks → Add webhook**.
2. **Title**: `riptide-collector`.
3. **URL**: `https://riptide-collector.<env>.example.com/webhooks/bitbucket`.
4. **Status**: Active.
5. **Skip certificate verification**: leave **off**.
6. **Triggers** — select:
   - Repository: **Push**
   - Pull request: **Created**, **Updated**, **Approved**, **Merged**, **Declined**, **Comment created**
7. **Custom headers**:
   - `Authorization: Bearer <token>` (the shared bearer token)
8. **Save**.

## Verify

1. Trigger a small event (e.g., push a commit to a PR).
2. Check the collector logs:
   ```bash
   oc logs -n riptide deployment/riptide-collector --tail=50 | grep bitbucket_event_received
   ```
3. You should see a line with the `delivery_id`, `event_type`, and `repo` matching what you triggered.
4. If `service` is `null`, the repo is not yet in the catalog — see `onboarding-a-team.md`.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Bitbucket shows `401` | Wrong/missing bearer token in the custom header |
| Bitbucket shows `422` | Malformed payload — open an issue with the delivery UUID |
| No log line at all | Webhook URL wrong, or network policy blocks Bitbucket → cluster |
