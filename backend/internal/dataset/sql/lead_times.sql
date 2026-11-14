-- Every change of the team first deployed to an environment in the window:
-- lead_time_changes (docs/dora-lead-time.md), per commit. The PRs' stage
-- times come from sql/environments.sql, which has no window end.
--
-- The team filter is on a grouping column of the view, so Postgres applies
-- it to commit_sightings before the per-commit aggregation; the window
-- bounds are on min(), so they can only filter its result.
--
-- Bots and service accounts are flagged, never dropped (DORA doc: report
-- bots on their own line, leave release tooling out):
--   service_account  the git host marks the author SERVICE, or a sender
--                    declared the handle a service
--   declared_bot     a sender declared the handle a bot; Go adds riptide's
--                    ingest-time automation detection on author and
--                    author_display_name (commit_sightings has no
--                    is_automated), so a commit and a PR by the same bot
--                    agree
--
-- $1 team, $2 window start (inclusive), $3 window end (exclusive).
WITH declared AS (
  SELECT lower(reviewer_handle) AS handle, reviewer_account_kind AS kind
  FROM noergler_events
  WHERE reviewer_handle IS NOT NULL AND reviewer_account_kind IN ('bot', 'service')
  UNION
  SELECT lower(actor_handle), actor_account_kind
  FROM pipeline_events
  WHERE actor_handle IS NOT NULL AND actor_account_kind IN ('bot', 'service')
)
SELECT l.repo_full_name AS repo,
       split_part(l.repo_full_name, '/', 1) AS project,
       l.environment,
       l.commit_sha,
       l.authored_at,
       l.first_deployed_at,
       extract(epoch FROM l.lead_time)::bigint AS lead_time_s,
       l.is_merge,
       l.author_is_service_account
         OR EXISTS (SELECT 1 FROM declared d
                     WHERE d.kind = 'service' AND d.handle IN (lower(l.author), lower(l.author_display_name)))
         AS service_account,
       EXISTS (SELECT 1 FROM declared d
                WHERE d.kind = 'bot' AND d.handle IN (lower(l.author), lower(l.author_display_name)))
         AS declared_bot,
       coalesce(l.author, '') AS author,
       coalesce(l.author_display_name, '') AS author_display_name
FROM lead_time_changes l
WHERE l.team = $1
  AND l.first_deployed_at >= $2 AND l.first_deployed_at < $3
  -- Deploys stored before argocd_events.environment existed name no stage.
  AND l.environment IS NOT NULL
ORDER BY l.first_deployed_at, l.environment, l.commit_sha
