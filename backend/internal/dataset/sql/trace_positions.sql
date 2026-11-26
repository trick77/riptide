-- Where commits arrived on their repo's trunk (master, main, or the branch
-- repo_trunks names), for the SHAs Go asks about: the compare links' ends
-- and the image runs' heads (dataset.trace). Two kinds of row:
--   at     the first trunk push of the team that brought sha to repo's
--          trunk branch, as its tip or listed in its commits[] (a release
--          push lists the release commit second, under the tool's next
--          version)
--   start  each trunk branch's first push of the team: when riptide's data
--          of it begins, and the push's fromHash, the tip before it, which
--          nothing riptide holds lists (a compare link's old end may be it)
-- A push's branch_name is the first ref it moved; one push may move
-- several (start reads the change of its own branch). Over all history,
-- read once per page request.
--
-- $1 team, $2 the SHAs, lowercased.
WITH repos AS MATERIALIZED (
  SELECT DISTINCT repo_full_name AS repo
  FROM bitbucket_events
  WHERE team = $1 AND event_type = 'repo:refs_changed' AND repo_full_name IS NOT NULL
),
trunks AS MATERIALIZED (
  SELECT r.repo, b.branch
  FROM repos r CROSS JOIN (VALUES ('master'), ('main')) b(branch)
  UNION
  SELECT t.repo_full_name, t.trunk
  FROM repo_trunks t
  WHERE t.repo_full_name IN (SELECT repo FROM repos)
),
pushes AS MATERIALIZED (
  SELECT b.repo_full_name AS repo, b.branch_name AS branch, b.occurred_at AS at, b.commit_sha AS tip,
         b.payload->'commits' AS commits
  FROM bitbucket_events b
  JOIN trunks t ON t.repo = b.repo_full_name AND t.branch = b.branch_name
  WHERE b.team = $1 AND b.event_type = 'repo:refs_changed'
),
arrivals AS (
  SELECT tip AS sha, repo, branch, at FROM pushes WHERE tip = ANY($2::text[])
  UNION ALL
  SELECT lower(c->>'id'), p.repo, p.branch, p.at
  FROM pushes p
  -- one odd payload must not fail every read
  CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(p.commits) = 'array' THEN p.commits END) c
  WHERE lower(c->>'id') = ANY($2::text[])
)
SELECT 'at' AS kind, a.sha, a.repo, a.branch, a.at
FROM (SELECT DISTINCT ON (sha, repo) sha, repo, branch, at FROM arrivals ORDER BY sha, repo, at, branch) a
UNION ALL
SELECT 'start',
       -- the change of this branch's ref (a push may move several), of the
       -- first such event
       (SELECT lower(c->>'fromHash')
          FROM bitbucket_events b
          CROSS JOIN LATERAL jsonb_array_elements(
            CASE WHEN jsonb_typeof(b.payload->'changes') = 'array' THEN b.payload->'changes' END) c
         WHERE b.team = $1 AND b.repo_full_name = f.repo AND b.branch_name = f.branch
           AND b.event_type = 'repo:refs_changed' AND b.occurred_at = f.at
           AND lower(c->'ref'->>'displayId') = f.branch
         ORDER BY b.id
         LIMIT 1),
       f.repo, f.branch, f.at
FROM (SELECT DISTINCT ON (repo, branch) repo, branch, at FROM pushes ORDER BY repo, branch, at) f
