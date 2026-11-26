-- Every branch the team pushed to since $2, per repo, and whose it is: the
-- last pusher, the last push's head commit's git identity, and whether every
-- push's head commit was a tool's (a git identity whose e-mail domain is
-- .invalid, the top-level domain reserved for names that resolve nowhere:
-- the identity behind housekeeping commits). Go decides from it which
-- branches are a bot's (dataset.Shared.botBranch), on every read: a bot's
-- branch's builds are a bot's (Run.Bot) in every number.
--
-- $1 team, $2 since, $3 the GitOps repos (sql/gitops.sql).
WITH pushes AS (
  SELECT e.repo_full_name, e.branch_name, e.occurred_at, e.author, e.author_display_name,
         -- The push's head commit (the first listed, its toHash) by an
         -- e-mail of the .invalid domain: a tool's work. Not every listed
         -- commit: a new branch's first push lists commits it shares with
         -- the trunk.
         coalesce(lower(h.author->>'emailAddress') LIKE '%.invalid', false) AS tool,
         coalesce(h.author->>'name', '') AS head_name,
         coalesce(h.author->>'displayName', '') AS head_display_name
  FROM bitbucket_events e
  -- The head commit's author out of the payload once per push, not once
  -- per field: the payload is stored out of line, each read fetches and
  -- unpacks it. OFFSET 0 keeps the planner from folding it back.
  CROSS JOIN LATERAL (SELECT e.payload->'commits'->0->'author' AS author OFFSET 0) h
  WHERE e.team = $1 AND e.occurred_at >= $2
    AND e.event_type = 'repo:refs_changed'
    AND e.repo_full_name IS NOT NULL AND e.branch_name IS NOT NULL
    -- The trunk is no branch of work, and holds most pushes (release
    -- tooling): left out before its payloads are read.
    AND e.branch_name NOT IN ('master', 'main')
    -- A bot's push (the ingest flag, its branch prefix included) is no
    -- work on a branch: out before its payload is read too.
    AND NOT e.is_automated
    AND NOT (e.repo_full_name = ANY($3::text[]))
)
SELECT repo_full_name AS repo, branch_name AS branch,
       bool_and(tool) AS tool,
       coalesce((array_agg(author ORDER BY occurred_at DESC))[1], '') AS author,
       coalesce((array_agg(author_display_name ORDER BY occurred_at DESC))[1], '') AS author_display_name,
       (array_agg(head_name ORDER BY occurred_at DESC))[1] AS head_name,
       (array_agg(head_display_name ORDER BY occurred_at DESC))[1] AS head_display_name
FROM pushes
GROUP BY repo_full_name, branch_name
ORDER BY repo_full_name, branch_name
