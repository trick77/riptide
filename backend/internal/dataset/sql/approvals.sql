-- Every PR of the team approved since $2, whenever it was opened or merged:
-- what the states of approved work read (metrics: a merge pause, approved
-- PRs waiting). A PR opened while Bitbucket's events were lost has no
-- pr:opened, only its approval and merge: the approval event itself is the
-- evidence. Its author is the one every PR event's payload names, so a PR
-- merged by another person is still its opener's. GitOps repos' PRs are
-- stage promotions, out.
--
-- An approval need not last: a reviewer withdraws it, sets needs work, or
-- Bitbucket resets it when the source branch moves. Every PR event carries
-- the reviewers and other participants with their status, Bitbucket's own
-- view: `timeline` holds, for each event that may change it, the people
-- approving after it (null when the payload carries neither list; Go then
-- reads the event itself). Go keeps the people other than the author
-- (dataset.Approval.Spans). `activity` are every PR event's times: what
-- tells a PR nobody touches.
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
-- Each one's first approval ever: before it, a push resets nothing.
firsts AS MATERIALIZED (
  SELECT a.repo_full_name, a.pr_id,
         (SELECT min(x.occurred_at) FROM bitbucket_events x
           WHERE x.repo_full_name = a.repo_full_name AND x.pr_id = a.pr_id
             AND x.event_type = 'pr:reviewer:approved') AS first_approved_at
  FROM approved a
),
ev AS MATERIALIZED (
  SELECT e.id, e.repo_full_name, e.pr_id, e.event_type, e.occurred_at, e.author, e.author_display_name,
         e.is_automated, x.pr->'author'->'user' AS pr_author,
         CASE WHEN jsonb_typeof(x.pr->'reviewers') = 'array' OR jsonb_typeof(x.pr->'participants') = 'array' THEN
           (SELECT coalesce(jsonb_agg(jsonb_build_object(
                     'author', coalesce(r->'user'->>'name', ''),
                     'display_name', coalesce(r->'user'->>'displayName', ''),
                     'bot', EXISTS (SELECT 1 FROM non_human_identities n
                                     WHERE n.handle IN (lower(r->'user'->>'name'), lower(r->'user'->>'displayName'))))),
                     '[]'::jsonb)
              FROM jsonb_array_elements(
                     CASE WHEN jsonb_typeof(x.pr->'reviewers') = 'array' THEN x.pr->'reviewers' ELSE '[]'::jsonb END ||
                     CASE WHEN jsonb_typeof(x.pr->'participants') = 'array' THEN x.pr->'participants' ELSE '[]'::jsonb END) r
             WHERE upper(r->>'status') = 'APPROVED')
         END AS approvers
  FROM bitbucket_events e
  JOIN firsts a ON a.repo_full_name = e.repo_full_name AND a.pr_id = e.pr_id
  -- The PR object out of the payload once, for the events that may change
  -- who approves: a reviewer's, and a push to the source branch after the
  -- first approval (Bitbucket may reset approvals then). A comment's
  -- payload is never fetched, nor an earlier push's. OFFSET 0 keeps the
  -- planner from folding it into each use.
  CROSS JOIN LATERAL (SELECT CASE WHEN e.event_type IN ('pr:reviewer:approved', 'pr:reviewer:unapproved',
                                                        'pr:reviewer:needs_work', 'pr:reviewer:updated')
                                    OR (e.event_type = 'pr:from_ref_updated' AND e.occurred_at > a.first_approved_at)
                                  THEN e.payload->'pullRequest' END AS pr OFFSET 0) x
  WHERE e.event_type LIKE 'pr:%'
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
                    'at', e.occurred_at, 'event', e.event_type, 'author', coalesce(e.author, ''),
                    'display_name', coalesce(e.author_display_name, ''),
                    'bot', e.is_automated OR EXISTS (SELECT 1 FROM non_human_identities n
                                                       WHERE n.handle IN (lower(e.author), lower(e.author_display_name))),
                    'approvers', e.approvers)
                  ORDER BY e.occurred_at, e.id) FILTER (WHERE e.approvers IS NOT NULL OR e.event_type IN
                    ('pr:reviewer:approved', 'pr:reviewer:unapproved', 'pr:reviewer:needs_work')), '[]'::jsonb)
           AS timeline,
         array_agg(e.occurred_at ORDER BY e.occurred_at) AS activity
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
       p.timeline,
       p.activity
FROM prs p
ORDER BY p.repo_full_name, p.pr_id
