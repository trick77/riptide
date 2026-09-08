# Setup: Noergler notification

Have a [noergler](https://github.com/trick77/noergler) instance forward
PR-review events to riptide-collector. Endpoint: `POST /webhooks/noergler`.

Noergler emits **two event types**, both keyed off review activity (not PR
lifecycle — PR open/merge/close already comes in via Bitbucket):

| Event | When | Carries |
|---|---|---|
| `pr_completed` | Once per PR, when it reaches a terminal outcome (merged / declined / deleted) | outcome, final diff size, aggregated token counts, elapsed time, cost, models used (finops) |
| `feedback` | When a reviewer disagrees with or acknowledges a finding | finding id, verdict, actor (reviewer-precision) |

Lead-time, activity, and other PR-lifecycle metrics are **not** emitted by
noergler — riptide already derives them from the Bitbucket source.

## Per-team token

Each team has its own bearer (the platform team hands it out — see
`docs/onboarding-a-team.md`). Use the team's **`noergler`** entry from
`team-keys.json` — the per-source binding is strict, so the team's
`argocd` / `jenkins` / `bitbucket` secrets will be rejected here. The
`noergler` source is optional per team; teams without one cannot post to
`/webhooks/noergler`.

Each noergler instance carries the **raw** bearer of the team that
operates it; every event from that instance is recorded in riptide with
`team = <that team>`. Do not share tokens across teams.

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

### `pr_completed`

```json
{
  "event_type": "pr_completed",
  "outcome": "merged",
  "pr_key": "PROJ/payments-api#42",
  "repo": "acme/payments-api",
  "reviewer_handle": "riptide-reviewer",
  "source_commit_sha": "<source-branch HEAD when the PR closed>",
  "merge_commit_sha": "<merge commit, only when outcome = merged>",
  "lines_added": 320,
  "lines_removed": 75,
  "files_changed": 12,
  "total_runs": 3,
  "total_prompt_tokens": 38420,
  "total_completion_tokens": 2110,
  "total_elapsed_ms": 24800,
  "total_findings_count": 7,
  "total_cost_usd": "0.382100",
  "models_used": ["gpt-4o-2024-08-06"],
  "first_review_at": "2026-04-29T17:30:00Z",
  "closed_at": "2026-04-29T18:42:00Z"
}
```

One rollup per PR: `(pr_key, outcome)` is the idempotency key, so noergler may
safely retry. `outcome` is `merged`, `declined` or `deleted` — only merged PRs
shipped, so throughput and DORA queries filter on it, while FinOps keeps all
three to see review spend on code that never landed. `source_commit_sha` and
`merge_commit_sha` join to `bitbucket_events` and `pipeline_events` for
cost-vs-deployment analysis. `pr_key` (`<repo>#<pr id>`) joins to
`bitbucket_events (repo_full_name, pr_id)` — that is also where riptide gets PR
diff sizes from, since Bitbucket's webhooks carry none.

`reviewer_handle` is optional but recommended: it is the account noergler posts
its review comments under on the git host. Reporting it lets riptide recognise
those comments as automation without every installation adding the handle to its
`automation` config; unrecognised, the bot counts as a human reviewer and drives
the code-review pickup-time metric toward zero.

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
-- finops: cost by model, last 7 days
SELECT m AS model,
       SUM(prompt_tokens + completion_tokens) AS tokens,
       SUM(cost_usd) AS spend,
       COUNT(*) AS prs
FROM noergler_events, unnest(models_used) AS m
WHERE event_type = 'pr_completed' AND created_at > now() - interval '7 days'
GROUP BY 1
ORDER BY spend DESC;

-- review spend that never shipped, last 7 days
SELECT outcome, COUNT(*) AS prs, SUM(cost_usd) AS spend
FROM noergler_events
WHERE event_type = 'pr_completed' AND created_at > now() - interval '7 days'
GROUP BY 1;

-- reviewer precision: 1 - disagreed PRs / reviewed PRs, last 7 days
SELECT 1.0 - (
    COUNT(*) FILTER (WHERE event_type = 'feedback' AND verdict = 'disagreed')::numeric
    / NULLIF(COUNT(*) FILTER (WHERE event_type = 'pr_completed'), 0)
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
  the team in `riptide.json` actually has a key entry — startup
  cross-validation prevents missing keys, but a stale deployment can
  diverge.
