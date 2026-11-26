-- Every branch the team pushed to since $2, per repo: its first and last
-- push, whether every push's head commit was a tool's (a git identity whose
-- e-mail domain is .invalid, the top-level domain reserved for names that
-- resolve nowhere: the identity behind housekeeping commits), whether a PR
-- came from it at or after its first push, whether it is any PR's target in
-- the repo (trunk, develop, a release line), and whether its last head
-- reached the repo's trunk without a PR (merged): a trunk push whose tip it
-- is (a fast-forward), that lists it, or whose commit names it a parent (a
-- merge commit, the push's tip, never hidden by the 5-commit cap). What the branch
-- states read (metrics): a branch's deletion is not stored (a delete-only
-- push is ignored at ingest), so "still exists" is never known; a recent
-- push proves it existed then. A push storing several branches keeps only
-- its first in branch_name. GitOps repos are promotion branches, out.
--
-- $1 team, $2 since, $3 the GitOps repos (sql/gitops.sql).
WITH pushes AS (
  SELECT e.repo_full_name, e.branch_name, e.occurred_at, e.author, e.author_display_name, e.commit_sha,
         -- The push's head commit (the first listed, its toHash) by an
         -- e-mail of the .invalid domain: a tool's work. Not every listed
         -- commit: a new branch's first push lists commits it shares with
         -- the trunk.
         coalesce(lower(e.payload->'commits'->0->'author'->>'emailAddress') LIKE '%.invalid', false) AS tool,
         coalesce(e.payload->'commits'->0->'author'->>'name', '') AS head_name,
         coalesce(e.payload->'commits'->0->'author'->>'displayName', '') AS head_display_name
  FROM bitbucket_events e
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
),
branches AS (
  SELECT repo_full_name, branch_name, min(occurred_at) AS first_at, max(occurred_at) AS last_at, count(*) AS pushes,
         bool_and(tool) AS tool,
         (array_agg(author ORDER BY occurred_at DESC))[1] AS author,
         (array_agg(author_display_name ORDER BY occurred_at DESC))[1] AS author_display_name,
         (array_agg(head_name ORDER BY occurred_at DESC))[1] AS head_name,
         (array_agg(head_display_name ORDER BY occurred_at DESC))[1] AS head_display_name,
         (array_agg(commit_sha ORDER BY occurred_at DESC))[1] AS head_sha
  FROM pushes
  GROUP BY repo_full_name, branch_name
),
targets AS (
  SELECT DISTINCT e.repo_full_name, lower(e.payload->'pullRequest'->'toRef'->>'displayId') AS target
  FROM bitbucket_events e
  WHERE e.team = $1 AND e.event_type IN ('pr:opened', 'pr:merged')
    AND e.repo_full_name IN (SELECT repo_full_name FROM branches)
),
-- The trunk branches of the repos pushed to, and every SHA their pushes
-- since $2 brought: the tip, the listed commits, their parents. Read once
-- and joined, never per branch: repo_trunks aggregates over every merge.
trunks AS MATERIALIZED (
  SELECT r.repo, t.branch
  FROM (SELECT DISTINCT repo_full_name AS repo FROM branches) r CROSS JOIN (VALUES ('master'), ('main')) t(branch)
  UNION
  SELECT rt.repo_full_name, rt.trunk
  FROM repo_trunks rt
  WHERE rt.repo_full_name IN (SELECT repo_full_name FROM branches)
),
-- Each trunk push's commits read out of its payload once (a payload is
-- large: release tooling's pushes dominate the trunk).
trunk_pushes AS MATERIALIZED (
  SELECT m.repo_full_name AS repo, m.commit_sha AS tip,
         -- one odd payload must not fail every read
         CASE WHEN jsonb_typeof(m.payload->'commits') = 'array' THEN m.payload->'commits' END AS commits
  FROM bitbucket_events m
  JOIN trunks t ON t.repo = m.repo_full_name AND t.branch = m.branch_name
  WHERE m.team = $1 AND m.event_type = 'repo:refs_changed' AND m.occurred_at >= $2
),
landed AS MATERIALIZED (
  SELECT DISTINCT tp.repo, s.sha
  FROM trunk_pushes tp
  CROSS JOIN LATERAL (
    SELECT tp.tip AS sha
    UNION ALL
    SELECT lower(c->>'id') FROM jsonb_array_elements(tp.commits) c
    UNION ALL
    SELECT lower(p->>'id')
    FROM jsonb_array_elements(tp.commits) c
    CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(c->'parents') = 'array' THEN c->'parents' END) p
  ) s
  -- the branches' heads only, an array computed once (an IN subquery here
  -- made the planner rescan the pushes per head)
  WHERE s.sha = ANY ((SELECT array_agg(head_sha) FROM branches)::text[])
)
SELECT b.repo_full_name AS repo,
       split_part(b.repo_full_name, '/', 1) AS project,
       b.branch_name AS branch,
       b.first_at,
       b.last_at,
       b.pushes,
       b.tool,
       coalesce(b.author, '') AS author,
       coalesce(b.author_display_name, '') AS author_display_name,
       -- The last push's head commit's git identity: a tool declared in the
       -- config's automation authors is a bot's too.
       b.head_name,
       b.head_display_name,
       -- PR events store the source branch, lowercased, as branch_name.
       EXISTS (SELECT 1 FROM bitbucket_events p
                WHERE p.repo_full_name = b.repo_full_name AND p.branch_name = b.branch_name
                  AND p.event_type LIKE 'pr:%' AND p.occurred_at >= b.first_at) AS has_pr,
       EXISTS (SELECT 1 FROM targets t WHERE t.repo_full_name = b.repo_full_name AND t.target = b.branch_name) AS target,
       EXISTS (SELECT 1 FROM landed l WHERE l.repo = b.repo_full_name AND l.sha = b.head_sha) AS merged
FROM branches b
ORDER BY b.repo_full_name, b.branch_name
