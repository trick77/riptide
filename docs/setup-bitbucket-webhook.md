# Setup: Bitbucket webhook

Wire a Bitbucket repository to send PR + push events to riptide-collector.

## Prerequisites

- Repo admin access to the Bitbucket repository.
- The riptide-collector URL for your environment (ask the platform team).
- **Your team's** bearer token. Each team has its own; the platform team
  hands it out when onboarding (see `docs/onboarding-a-team.md`). The token
  identifies your team to the collector — do not share it with another team.

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
   - `Authorization: Bearer <your-team-token>`
   - *Optional:* `X-Riptide-Service-Id: <opaque service id from your CMDB, e.g. srv0417>`
     — sets the `service` column on every event from this repo. If absent,
     riptide falls back to `service = repository.full_name`.
8. **Save**.

## Verify

1. Trigger a small event (e.g., push a commit to a PR).
2. Check the collector logs:
   ```bash
   oc logs -n riptide deployment/riptide-collector --tail=50 | grep bitbucket_event_received
   ```
3. You should see a line with the `delivery_id`, `event_type`, `repo`, and
   `team` matching what you triggered. `team` is whichever team the bearer
   token identifies.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Bitbucket shows `401` | Wrong/missing bearer token in the custom header |
| Bitbucket shows `422` | Malformed payload — open an issue with the delivery UUID |
| No log line at all | Webhook URL wrong, or network policy blocks Bitbucket → cluster |
