-- The team's repositories, over all history, whose slug (the name after the
-- project) is one of $2, lowercased: the candidates a run without a
-- Bitbucket-known commit is matched to by its pipeline name. The distinct
-- names walk the repo_full_name index one name at a time, not the team's
-- every event.
--
-- $1 team, $2 slugs, lowercased.
WITH RECURSIVE names AS (
  SELECT min(repo_full_name) AS repo FROM bitbucket_events
  UNION ALL
  SELECT (SELECT min(b.repo_full_name) FROM bitbucket_events b WHERE b.repo_full_name > n.repo)
  FROM names n
  WHERE n.repo IS NOT NULL
)
SELECT coalesce(array_agg(n.repo ORDER BY n.repo), '{}')::text[] AS repos
FROM names n
WHERE n.repo IS NOT NULL
  AND lower(split_part(n.repo, '/', 2)) = ANY($2::text[])
  AND EXISTS (SELECT 1 FROM bitbucket_events b WHERE b.repo_full_name = n.repo AND b.team = $1)
