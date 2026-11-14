-- One row per PR, keyed (repo_full_name, pr_id): PR ids are numbered per
-- repository. A PR belongs to the window when its pr:opened or pr:merged
-- falls in it; its timestamps then come from its whole history, so a PR
-- opened before the window keeps its opened_at.
--
-- The README's lead-time breakdown and pickup-time queries, per PR instead of
-- aggregated, and with their WHERE filters turned into flags: `gitops` (a
-- stage promotion, not work) and `bot`. The UI drops what it does not count.
-- `bot` here is the ingest-time flag and sender declarations; Go adds
-- riptide's automation detection on the opener's login and display name, as
-- for commits. The merge commit's deploys are joined in Go.
--
-- $1 team, $2 window start (inclusive), $3 window end (exclusive).
WITH non_human_identities AS (
  -- Accounts senders declared as non-human (README, pickup-time query):
  -- is_automated is decided at ingest, so rows written before an identity
  -- was declared are caught here.
  SELECT DISTINCT lower(reviewer_handle) AS handle
  FROM noergler_events
  WHERE reviewer_handle IS NOT NULL AND reviewer_account_kind IN ('bot', 'service')
  UNION
  SELECT DISTINCT lower(actor_handle)
  FROM pipeline_events
  WHERE actor_handle IS NOT NULL AND actor_account_kind IN ('bot', 'service')
),
gitops_repos AS (
  -- GitOps repos: their commits are what argocd_events.revision points at
  -- (README, "Reading the event tables"). Over all data, not the window.
  SELECT DISTINCT repo_full_name
  FROM bitbucket_events
  WHERE commit_sha IN (SELECT revision FROM argocd_events)
    AND repo_full_name IS NOT NULL
),
window_prs AS (
  SELECT DISTINCT repo_full_name, pr_id
  FROM bitbucket_events
  WHERE team = $1
    AND occurred_at >= $2 AND occurred_at < $3
    AND event_type IN ('pr:opened', 'pr:merged')
    AND pr_id IS NOT NULL AND repo_full_name IS NOT NULL
),
prs AS (
  SELECT e.repo_full_name, e.pr_id,
         -- pr:opened carries the PR as opened; a PR opened before ingestion
         -- started has only its pr:merged, which names the same PR author.
         coalesce(max(lower(e.payload->'pullRequest'->'fromRef'->>'displayId')) FILTER (WHERE e.event_type = 'pr:opened'),
                  max(lower(e.payload->'pullRequest'->'fromRef'->>'displayId')) FILTER (WHERE e.event_type = 'pr:merged'))
             AS source_branch,
         coalesce(max(e.author) FILTER (WHERE e.event_type = 'pr:opened'),
                  max(e.author) FILTER (WHERE e.event_type = 'pr:merged')) AS pr_opener,
         coalesce(max(e.author_display_name) FILTER (WHERE e.event_type = 'pr:opened'),
                  max(e.author_display_name) FILTER (WHERE e.event_type = 'pr:merged')) AS pr_opener_display_name,
         bool_or(e.is_automated) FILTER (WHERE e.event_type IN ('pr:opened', 'pr:merged')) AS automated,
         -- change_type means something only on these two (README).
         coalesce(max(e.change_type) FILTER (WHERE e.event_type = 'pr:opened'),
                  max(e.change_type) FILTER (WHERE e.event_type = 'pr:merged')) AS change_type,
         min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:opened') AS opened_at,
         -- Pickup clock start (README, pickup-time query): the draft->ready
         -- flip, else the opening of a PR not opened as a draft. A draft never
         -- made ready has none.
         coalesce(min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:ready_for_review'),
                  min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:opened'
                                               AND coalesce(e.payload->'pullRequest'->>'draft', 'false') = 'false'))
             AS ready_at,
         min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:reviewer:approved') AS approved_at,
         min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:merged') AS merged_at,
         max(e.occurred_at) FILTER (WHERE e.event_type = 'pr:declined') AS declined_at,
         max(e.occurred_at) FILTER (WHERE e.event_type = 'pr:deleted') AS deleted_at,
         max(e.occurred_at) FILTER (WHERE e.event_type IN ('pr:opened', 'pr:reopened')) AS last_opened_at,
         max(e.payload->'pullRequest'->'properties'->'mergeCommit'->>'id')
             FILTER (WHERE e.event_type = 'pr:merged') AS merge_commit
  FROM bitbucket_events e
  JOIN window_prs w ON w.repo_full_name = e.repo_full_name AND w.pr_id = e.pr_id
  GROUP BY e.repo_full_name, e.pr_id
),
-- Oldest commit pushed to the PR's branch; a rebase resets it (README,
-- lead-time breakdown query). A branch name is reused (a long-lived
-- source branch, measured in a month: up to 138 pushes on one branch, up to
-- 12 PRs from one branch), so only the pushes of this PR's own life count:
-- after the latest earlier PR from the same repo and branch was closed, and
-- not after this PR became ready.
bounded AS (
  SELECT p.repo_full_name, p.pr_id, p.source_branch,
         -- ready, else merged: a PR opened before ingestion started has no
         -- ready_at, and must not reach into a later PR's pushes.
         coalesce(p.ready_at, p.merged_at) AS until_at,
         -- PR events store the source branch, lowercased, as branch_name.
         (SELECT max(c.occurred_at)
            FROM bitbucket_events c
           WHERE c.repo_full_name = p.repo_full_name
             AND c.branch_name = p.source_branch
             AND c.event_type IN ('pr:merged', 'pr:declined', 'pr:deleted')
             AND c.pr_id IS DISTINCT FROM p.pr_id
             -- earlier: opened before this PR (or, opened before ingestion
             -- started, closed before it)
             AND (c.occurred_at < coalesce(p.opened_at, p.ready_at, p.merged_at)
                  OR EXISTS (SELECT 1 FROM bitbucket_events o
                              WHERE o.repo_full_name = c.repo_full_name AND o.pr_id = c.pr_id
                                AND o.event_type = 'pr:opened'
                                AND o.occurred_at < coalesce(p.opened_at, p.ready_at, p.merged_at))))
           AS previous_closed_at
  FROM prs p
  WHERE p.source_branch IS NOT NULL
),
branch_pushes AS MATERIALIZED (
  -- Each push to a PR branch expanded once, however many PRs share the
  -- branch: its oldest commit.
  SELECT e.repo_full_name, e.branch_name, e.occurred_at AS pushed_at,
         (SELECT min((c->>'authorTimestamp')::bigint)
            FROM jsonb_array_elements(e.payload->'commits') c) AS first_ms
  FROM bitbucket_events e
  JOIN (SELECT DISTINCT repo_full_name, source_branch FROM bounded) br
    ON e.repo_full_name = br.repo_full_name AND e.branch_name = br.source_branch
  WHERE e.event_type = 'repo:refs_changed'
),
first_commits AS (
  SELECT b.repo_full_name, b.pr_id, to_timestamp(min(bp.first_ms) / 1000.0) AS first_commit_at
  FROM bounded b
  JOIN branch_pushes bp ON bp.repo_full_name = b.repo_full_name AND bp.branch_name = b.source_branch
  WHERE bp.pushed_at > coalesce(b.previous_closed_at, '-infinity')
    AND bp.pushed_at <= coalesce(b.until_at, 'infinity')
  GROUP BY b.repo_full_name, b.pr_id
),
chain AS (
  SELECT p.*, fc.first_commit_at,
         -- Reviewer touches after the clock start, the opener, bots and
         -- declared non-humans excluded (README, pickup-time query), oldest
         -- first. Go drops the authors riptide's automation detection
         -- recognises today and takes first_review_at and approved_at from
         -- what is left, so both count the same reviewers.
         (SELECT coalesce(jsonb_agg(jsonb_build_object(
                     'at', e.occurred_at,
                     'approved', e.event_type = 'pr:reviewer:approved',
                     'author', coalesce(e.author, ''),
                     'display_name', coalesce(e.author_display_name, ''))
                   ORDER BY e.occurred_at, e.id), '[]'::jsonb)
            FROM bitbucket_events e
           WHERE e.repo_full_name = p.repo_full_name AND e.pr_id = p.pr_id
             AND e.event_type IN ('pr:comment:added', 'pr:reviewer:approved',
                                  'pr:reviewer:unapproved', 'pr:reviewer:needs_work',
                                  'pr:reviewer:updated')
             AND e.author IS DISTINCT FROM p.pr_opener
             AND NOT e.is_automated
             AND NOT EXISTS (SELECT 1 FROM non_human_identities n WHERE n.handle = lower(e.author))
             AND e.occurred_at >= p.ready_at) AS touches
  FROM prs p
  LEFT JOIN first_commits fc
    ON fc.repo_full_name = p.repo_full_name AND fc.pr_id = p.pr_id
)
SELECT c.repo_full_name AS repo,
       split_part(c.repo_full_name, '/', 1) AS project,
       c.pr_id,
       c.repo_full_name IN (SELECT repo_full_name FROM gitops_repos) AS gitops,
       coalesce(c.automated, false)
         OR EXISTS (SELECT 1 FROM non_human_identities n WHERE n.handle = lower(c.pr_opener)) AS bot,
       c.change_type,
       ARRAY(SELECT DISTINCT k
               FROM bitbucket_events e CROSS JOIN LATERAL unnest(e.jira_keys) k
              WHERE e.repo_full_name = c.repo_full_name AND e.pr_id = c.pr_id
                AND e.event_type IN ('pr:opened', 'pr:merged')
              ORDER BY k)::text[] AS jira_keys,
       CASE
         WHEN c.merged_at IS NOT NULL THEN 'merged'
         WHEN c.deleted_at IS NOT NULL THEN 'deleted'
         -- a declined PR reopened later is open again
         WHEN c.declined_at IS NOT NULL
              AND c.declined_at >= coalesce(c.last_opened_at, '-infinity') THEN 'declined'
         ELSE 'open'
       END AS state,
       c.opened_at,
       c.ready_at,
       c.touches,
       c.merged_at,
       c.first_commit_at,
       -- lowercased like every SHA column riptide stores; the payload keeps
       -- the sender's casing
       lower(c.merge_commit) AS merge_commit,
       -- the opener, for Go's automation detection only; never sent
       coalesce(c.pr_opener, '') AS opener,
       coalesce(c.pr_opener_display_name, '') AS opener_display_name
FROM chain c
ORDER BY c.repo_full_name, c.pr_id
