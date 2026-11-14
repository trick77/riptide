-- One row per PR noergler reviewed whose final rollup closed in the window:
-- noergler_pr_rollups, the newest cumulative rollup per PR, never a sum over
-- noergler_events (a declined PR reopened and merged emits twice). The
-- window filter applies after the view picked the newest rollup, so a PR is
-- counted in the window its final outcome landed in.
--
-- $1 team, $2 window start (inclusive), $3 window end (exclusive).
SELECT r.pr_key,
       r.repo,
       split_part(r.repo, '/', 1) AS project,
       r.outcome,
       r.cost_usd::text AS cost_usd,
       r.lines_added,
       r.lines_removed,
       r.files_changed,
       r.findings_count,
       r.total_runs,
       r.occurred_at AS closed_at
FROM noergler_pr_rollups r
WHERE r.team = $1 AND r.occurred_at >= $2 AND r.occurred_at < $3
ORDER BY r.occurred_at, r.pr_key
