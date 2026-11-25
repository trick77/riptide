-- Every change of the team first deployed to an environment, over all
-- history: lead_time_changes (docs/dora-lead-time.md), per commit. Read once
-- per page request (dataset.LoadShared), whatever its windows: the view's
-- per-commit aggregation costs the same for one window as for all history,
-- and Go takes from it each window's lead times (first deployed in the
-- window) and its merged PRs' stage times (their merge commits, no window
-- end: a PR merged in the window that reached production after it still
-- has a production stage).
--
-- The team filter is on a grouping column of the view, so Postgres applies
-- it to commit_sightings before the per-commit aggregation.
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
-- $1 team.
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
       -- lowercased like every SHA column riptide stores
       lower(l.commit_sha) AS commit_sha,
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
  -- Deploys stored before argocd_events.environment existed name no stage.
  AND l.environment IS NOT NULL
ORDER BY l.first_deployed_at, l.environment, l.commit_sha
