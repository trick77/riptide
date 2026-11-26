-- Every branch the team pushed to since $2, per repo, and who pushed it:
-- whether the ingest flagged every push a bot's (is_automated), and each
-- distinct pusher and head commit's git identity of the pushes it did not
-- flag, with whether the head commit was a tool's (a git identity whose
-- e-mail domain is .invalid, the top-level domain reserved for names that
-- resolve nowhere: the identity behind housekeeping commits). Go decides
-- from it which branches are a bot's (BranchOwner.bot): every push a bot's,
-- by any of these, never the last one alone. A bot's branch's builds are a
-- bot's (Run.Bot) in every number.
--
-- $1 team, $2 since, $3 the GitOps repos (sql/gitops.sql).
WITH pushes AS (
  SELECT e.repo_full_name, e.branch_name, e.is_automated AS flagged,
         coalesce(e.author, '') AS author, coalesce(e.author_display_name, '') AS author_display_name,
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
  -- unpacks it. A push the ingest flagged is a bot's whatever its commit:
  -- its payload is never read. OFFSET 0 keeps the planner from folding it
  -- back.
  CROSS JOIN LATERAL (SELECT CASE WHEN NOT e.is_automated THEN e.payload->'commits'->0->'author' END AS author
                      OFFSET 0) h
  WHERE e.team = $1 AND e.occurred_at >= $2
    AND e.event_type = 'repo:refs_changed'
    AND e.repo_full_name IS NOT NULL AND e.branch_name IS NOT NULL
    -- The trunk is no branch of work, and holds most pushes (release
    -- tooling): left out before its payloads are read.
    AND e.branch_name NOT IN ('master', 'main')
    AND NOT (e.repo_full_name = ANY($3::text[]))
)
SELECT repo_full_name AS repo, branch_name AS branch,
       bool_and(flagged) AS flagged,
       coalesce(jsonb_agg(DISTINCT jsonb_build_object(
                  'tool', tool, 'author', author, 'author_display_name', author_display_name,
                  'head_name', head_name, 'head_display_name', head_display_name)) FILTER (WHERE NOT flagged),
                '[]'::jsonb) AS pushers
FROM pushes
GROUP BY repo_full_name, branch_name
ORDER BY repo_full_name, branch_name
