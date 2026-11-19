-- One row per PR noergler reviewed whose final rollup closed in the window:
-- noergler_pr_rollups, the newest cumulative rollup per PR, never a sum over
-- noergler_events (a declined PR reopened and merged emits twice). The
-- window filter applies after the view picked the newest rollup, so a PR is
-- counted in the window its final outcome landed in.
--
-- `gitops` flags a stage promotion, as in prs.sql: teams let noergler review
-- them too. Flagged, not dropped: the spend is real, only its per-change
-- figures leave it out.
--
-- $1 team, $2 window start (inclusive), $3 window end (exclusive), $4 the
-- GitOps repos (sql/gitops.sql).
SELECT r.repo,
       split_part(r.repo, '/', 1) AS project,
       coalesce(r.repo = ANY($4::text[]), false) AS gitops,
       r.outcome,
       r.cost_usd::text AS cost_usd,
       r.lines_added,
       r.lines_removed
FROM noergler_pr_rollups r
WHERE r.team = $1 AND r.occurred_at >= $2 AND r.occurred_at < $3
ORDER BY r.occurred_at, r.pr_key
