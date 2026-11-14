-- The window's merged PRs' merge commits, first deploy per environment, from
-- lead_time_changes (README, lead-time breakdown query). No upper time
-- bound: a PR merged in the window that reached production after it still
-- has a production stage. The team filter is on a grouping column of the
-- view, so it applies before the per-commit aggregation.
--
-- $1 team, $2 merge commits (lowercased).
SELECT lower(l.commit_sha) AS commit_sha,
       l.environment,
       l.first_deployed_at
FROM lead_time_changes l
WHERE l.team = $1
  AND lower(l.commit_sha) = ANY($2::text[])
  -- Deploys stored before argocd_events.environment existed name no stage.
  AND l.environment IS NOT NULL
