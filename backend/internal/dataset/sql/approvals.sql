-- Every PR of the team approved since $2, whenever it was opened or merged:
-- what the states of approved work read (metrics: a merge pause, approved
-- PRs waiting). A PR opened while Bitbucket's events were lost has no
-- pr:opened, only its approval and merge: the approval event itself is the
-- evidence. Its author is the one every PR event's payload names, so a PR
-- merged by another person is still its opener's. GitOps repos' PRs are
-- stage promotions, out. Go keeps the approvals of people other than the
-- author (dataset.Approval).
--
-- $1 team, $2 approvals since, $3 the GitOps repos (sql/gitops.sql).
WITH non_human_identities AS (
/*{{non_human_identities}}*/
),
approved AS (
  SELECT DISTINCT repo_full_name, pr_id
  FROM bitbucket_events
  WHERE team = $1 AND occurred_at >= $2
    AND event_type = 'pr:reviewer:approved'
    AND pr_id IS NOT NULL AND repo_full_name IS NOT NULL
    AND NOT (repo_full_name = ANY($3::text[]))
),
ev AS MATERIALIZED (
  SELECT e.id, e.repo_full_name, e.pr_id, e.event_type, e.occurred_at, e.author, e.author_display_name,
         e.is_automated, e.payload->'pullRequest'->'author'->'user' AS pr_author
  FROM bitbucket_events e
  JOIN approved a ON a.repo_full_name = e.repo_full_name AND a.pr_id = e.pr_id
  WHERE e.event_type IN ('pr:opened', 'pr:merged', 'pr:declined', 'pr:deleted', 'pr:reopened',
                         'pr:reviewer:approved')
),
prs AS (
  SELECT e.repo_full_name, e.pr_id,
         coalesce(max(e.pr_author->>'name'), max(e.author) FILTER (WHERE e.event_type = 'pr:opened'), '') AS opener,
         coalesce(max(e.pr_author->>'displayName'),
                  max(e.author_display_name) FILTER (WHERE e.event_type = 'pr:opened'), '') AS opener_display_name,
         coalesce(bool_or(e.is_automated) FILTER (WHERE e.event_type IN ('pr:opened', 'pr:merged')), false) AS automated,
         min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:merged') AS merged_at,
         max(e.occurred_at) FILTER (WHERE e.event_type = 'pr:declined') AS declined_at,
         max(e.occurred_at) FILTER (WHERE e.event_type = 'pr:deleted') AS deleted_at,
         max(e.occurred_at) FILTER (WHERE e.event_type IN ('pr:opened', 'pr:reopened')) AS last_opened_at,
         coalesce(jsonb_agg(jsonb_build_object(
                    'at', e.occurred_at, 'author', coalesce(e.author, ''),
                    'display_name', coalesce(e.author_display_name, ''),
                    'bot', e.is_automated OR EXISTS (SELECT 1 FROM non_human_identities n
                                                       WHERE n.handle IN (lower(e.author), lower(e.author_display_name))))
                  ORDER BY e.occurred_at, e.id) FILTER (WHERE e.event_type = 'pr:reviewer:approved'), '[]'::jsonb)
           AS approvals
  FROM ev e
  GROUP BY e.repo_full_name, e.pr_id
)
SELECT p.repo_full_name AS repo,
       split_part(p.repo_full_name, '/', 1) AS project,
       p.pr_id,
       p.opener,
       p.opener_display_name,
       p.automated
         OR EXISTS (SELECT 1 FROM non_human_identities n
                     WHERE n.handle IN (lower(p.opener), lower(p.opener_display_name))) AS bot,
       p.merged_at,
       -- declined (and not reopened since) or deleted; NULL when merged or open
       CASE
         WHEN p.merged_at IS NOT NULL THEN NULL
         WHEN p.deleted_at IS NOT NULL THEN p.deleted_at
         WHEN p.declined_at IS NOT NULL AND p.declined_at >= coalesce(p.last_opened_at, '-infinity') THEN p.declined_at
       END AS closed_at,
       p.approvals
FROM prs p
ORDER BY p.repo_full_name, p.pr_id
