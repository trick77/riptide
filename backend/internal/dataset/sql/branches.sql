-- Every branch the team pushed to since $2, per repo, as of any instant the
-- branch states judge at (metrics): every push's time, the first PR event
-- from it at or after its first push, since when it is a PR's target in the
-- repo (trunk, develop, a release line, an epic), and when its last head
-- reached the repo's trunk without a PR (merged): a trunk push whose tip it is (a
-- fast-forward), that lists it, or whose commit names it a parent (a merge
-- commit, the push's tip, never hidden by the 5-commit cap). A branch's
-- deletion is not stored (a delete-only push is ignored at ingest), so
-- "still exists" is never known; a recent push proves it existed then. A
-- push storing several branches keeps only its first in branch_name. GitOps
-- repos are promotion branches, out. Whose a branch is (a bot's, a tool's)
-- is sql/branch_owners.sql's.
--
-- Only a branch without a PR and no PR's target can be one merged without a
-- PR: its head is looked for on the trunk, in the pushes from its last push
-- on, of its repo alone. A trunk push's payload is large (release tooling's
-- pushes dominate the trunk): read for those repos and times only.
--
-- $1 team, $2 since, $3 the GitOps repos (sql/gitops.sql).
WITH branches AS MATERIALIZED (
  SELECT e.repo_full_name, e.branch_name, min(e.occurred_at) AS first_at, max(e.occurred_at) AS last_at,
         count(*) AS pushes, array_agg(e.occurred_at ORDER BY e.occurred_at) AS push_times,
         (array_agg(e.commit_sha ORDER BY e.occurred_at DESC))[1] AS head_sha
  FROM bitbucket_events e
  WHERE e.team = $1 AND e.occurred_at >= $2
    AND e.event_type = 'repo:refs_changed'
    AND e.repo_full_name IS NOT NULL AND e.branch_name IS NOT NULL
    -- The trunk is no branch of work.
    AND e.branch_name NOT IN ('master', 'main')
    -- A bot's push (the ingest flag, its branch prefix included) is no work
    -- on a branch.
    AND NOT e.is_automated
    AND NOT (e.repo_full_name = ANY($3::text[]))
  GROUP BY e.repo_full_name, e.branch_name
),
-- PR events store the source branch, lowercased, as branch_name.
with_pr AS MATERIALIZED (
  SELECT b.*,
         (SELECT min(p.occurred_at) FROM bitbucket_events p
           WHERE p.repo_full_name = b.repo_full_name AND p.branch_name = b.branch_name
             AND p.event_type LIKE 'pr:%' AND p.occurred_at >= b.first_at) AS pr_at
  FROM branches b
),
-- The targets of the PRs of the repos pushed to, each since its first PR:
-- an epic branch is a target from the first PR into it on, also once a PR
-- came from it.
targets AS MATERIALIZED (
  SELECT e.repo_full_name, lower(e.payload->'pullRequest'->'toRef'->>'displayId') AS target, min(e.occurred_at) AS since
  FROM bitbucket_events e
  WHERE e.team = $1 AND e.event_type IN ('pr:opened', 'pr:merged')
    AND e.repo_full_name IN (SELECT repo_full_name FROM with_pr)
  GROUP BY 1, 2
),
candidates AS MATERIALIZED (
  SELECT w.repo_full_name, w.branch_name, w.last_at, w.head_sha
  FROM with_pr w
  WHERE w.pr_at IS NULL
    AND NOT EXISTS (SELECT 1 FROM targets t WHERE t.repo_full_name = w.repo_full_name AND t.target = w.branch_name)
),
-- Where their work lands: master, main, and every branch a PR of the repo
-- went into (the trunk repo_trunks names among them, a develop, a release
-- line). Merged into any of them without a PR, a branch is done work. From
-- the repo's earliest candidate's last push on.
trunks AS MATERIALIZED (
  SELECT r.repo, t.branch, r.since
  FROM (SELECT repo_full_name AS repo, min(last_at) AS since FROM candidates GROUP BY repo_full_name) r
  CROSS JOIN LATERAL (SELECT 'master' UNION SELECT 'main'
                      UNION SELECT target FROM targets x WHERE x.repo_full_name = r.repo AND x.target IS NOT NULL) t(branch)
),
-- Each such trunk push's SHAs read out of its payload once: the tip, the
-- listed commits, their parents.
landed AS MATERIALIZED (
  SELECT m.repo_full_name AS repo, s.sha, min(m.occurred_at) AS at
  FROM bitbucket_events m
  JOIN trunks t ON t.repo = m.repo_full_name AND t.branch = m.branch_name
  -- The commits out of the payload once per push, not once per use: the
  -- payload is stored out of line, each read fetches and unpacks it.
  -- OFFSET 0 keeps the planner from folding it back into each use.
  CROSS JOIN LATERAL (SELECT CASE WHEN jsonb_typeof(m.payload->'commits') = 'array' THEN m.payload->'commits' END
                        AS commits OFFSET 0) x
  CROSS JOIN LATERAL (
    SELECT m.commit_sha AS sha
    UNION ALL
    SELECT lower(c->>'id') FROM jsonb_array_elements(x.commits) c
    UNION ALL
    SELECT lower(p->>'id')
    FROM jsonb_array_elements(x.commits) c
    CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(c->'parents') = 'array' THEN c->'parents' END) p
  ) s
  WHERE m.team = $1 AND m.event_type = 'repo:refs_changed' AND m.occurred_at >= t.since
    -- the candidates' heads only, an array computed once (an IN subquery
    -- here made the planner rescan the pushes per head)
    AND s.sha = ANY ((SELECT array_agg(head_sha) FROM candidates)::text[])
  GROUP BY m.repo_full_name, s.sha
)
SELECT w.repo_full_name AS repo,
       split_part(w.repo_full_name, '/', 1) AS project,
       w.branch_name AS branch,
       w.first_at,
       w.last_at,
       w.pushes,
       w.push_times,
       w.pr_at,
       (SELECT t.since FROM targets t WHERE t.repo_full_name = w.repo_full_name AND t.target = w.branch_name)
         AS target_at,
       (SELECT least(
                 -- A trunk push whose tip the head is, at any time: a
                 -- fast-forward, or a branch cut from the trunk with no
                 -- commit of its own (by the SHA's index, no payload read).
                 (SELECT min(m.occurred_at) FROM bitbucket_events m
                   WHERE m.commit_sha = c.head_sha AND m.repo_full_name = c.repo_full_name
                     AND m.event_type = 'repo:refs_changed' AND m.team = $1
                     AND m.branch_name IN (SELECT t.branch FROM trunks t WHERE t.repo = c.repo_full_name)),
                 (SELECT l.at FROM landed l WHERE l.repo = c.repo_full_name AND l.sha = c.head_sha))
          FROM candidates c
         WHERE c.repo_full_name = w.repo_full_name AND c.branch_name = w.branch_name) AS merged_at
FROM with_pr w
ORDER BY w.repo_full_name, w.branch_name
