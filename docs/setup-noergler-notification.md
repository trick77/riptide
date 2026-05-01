# Setup: Noergler notification

Have a [noergler](https://github.com/trick77/noergler) instance forward
PR-review events to riptide-collector. Endpoint: `POST /webhooks/noergler`.

Noergler emits **two event types**, both keyed off review activity (not PR
lifecycle — PR open/merge/close already comes in via Bitbucket):

| Event | When | Carries |
|---|---|---|
| `completed` | After an LLM review run finishes | model, token counts, elapsed time, cost (finops) |
| `feedback` | When a reviewer disagrees with or acknowledges a finding | finding id, verdict, actor (reviewer-precision) |

Lead-time, activity, and other PR-lifecycle metrics are **not** emitted by
noergler — riptide already derives them from the Bitbucket source.

## Per-team token

Each team has its own bearer (the platform team hands it out — see
`docs/onboarding-a-team.md`). Each noergler instance carries the **raw**
bearer of the team that operates it; every event from that instance is
recorded in riptide with `team = <that team>`. Do not share tokens across
teams.

## Optional configuration

In noergler, set:

```
RIPTIDE_URL=https://riptide-collector.example.com
RIPTIDE_TOKEN=<your-team-raw-bearer>
```

If either is unset, noergler runs standalone and does not emit. When set,
noergler verifies reachability and bearer validity at startup via
`GET /auth/ping`:

- 200 → `{"status": "ok", "team": "<your-team>"}` — log and continue.
- 401 → noergler **fails to start** with a clear error (token rejected).
- Connection error / timeout → noergler **starts** with a warning;
  riptide may be temporarily down. Runtime emissions are best-effort.

## Payloads

### `completed`

```json
{
  "event_type": "completed",
  "pr_key": "PROJ/payments-api#42",
  "repo": "acme/payments-api",
  "commit_sha": "<git sha being reviewed>",
  "run_id": "<noergler-internal review-run id>",
  "model": "gpt-4o-2024-08-06",
  "prompt_tokens": 12345,
  "completion_tokens": 678,
  "elapsed_ms": 8200,
  "findings_count": 3,
  "cost_usd": "0.124500",
  "finished_at": "2026-04-29T18:01:00Z"
}
```

`run_id` alone is the idempotency key — noergler may safely retry. The
`commit_sha` enables joins to `bitbucket_events`, `pipeline_events`, and
`argocd_events` for cost-vs-deployment analysis.

### `feedback`

```json
{
  "event_type": "feedback",
  "pr_key": "PROJ/payments-api#42",
  "finding_id": "<noergler-internal finding id>",
  "verdict": "disagreed",
  "actor": "alice@example.com",
  "repo": "acme/payments-api",
  "occurred_at": "2026-04-29T18:05:00Z"
}
```

`verdict` is `"disagreed"` or `"acknowledged"`. The same `finding_id` may
flip verdicts over time; both verdicts are recorded as distinct rows.
Idempotency key is `(finding_id, verdict)`.

## Verify

```sql
-- finops: cost-by-model, last 7 days
SELECT model,
       SUM(prompt_tokens + completion_tokens) AS tokens,
       SUM(cost_usd) AS spend,
       COUNT(*) AS runs
FROM noergler_events
WHERE event_type = 'completed' AND created_at > now() - interval '7 days'
GROUP BY model
ORDER BY spend DESC;

-- reviewer precision: 1 - disagreed/total, last 7 days
SELECT 1.0 - (
    SUM(CASE WHEN event_type='feedback' AND verdict='disagreed' THEN 1 ELSE 0 END)
    / NULLIF(SUM(CASE WHEN event_type='completed' THEN findings_count ELSE 0 END), 0)
) AS precision_estimate
FROM noergler_events
WHERE created_at > now() - interval '7 days';
```

## Troubleshooting

- **All rows have `team = null`**: the bearer dependency was bypassed —
  shouldn't happen, since `/webhooks/noergler` requires it.
- **Wrong team**: noergler is using another team's bearer. Reset
  `RIPTIDE_TOKEN`.
- **No rows arriving** but noergler logs say emit succeeded: check that
  the team in `riptide-catalog.json` actually has a key entry — startup
  cross-validation prevents missing keys, but a stale deployment can
  diverge.
