-- The team's finished CI runs that published images, over all history:
-- docs/correlating-deploys-to-commits.md's join, a deployed image's full
-- reference equal to one of a run's image_refs, as Go does it
-- (dataset.trace). The run's commit is its head; its repo is the one
-- Bitbucket saw the commit in, as runs.sql finds it.
--
-- $1 team.
SELECT p.id,
       lower(p.commit_sha) AS commit_sha,
       p.started_at,
       p.image_refs::text[] AS image_refs,
       (SELECT b.repo_full_name
          FROM bitbucket_events b
         WHERE b.commit_sha = p.commit_sha AND b.repo_full_name IS NOT NULL
         ORDER BY b.occurred_at
         LIMIT 1) AS repo
FROM pipeline_events p
WHERE p.team = $1 AND p.phase = 'COMPLETED' AND cardinality(p.image_refs) > 0 AND p.commit_sha IS NOT NULL
ORDER BY p.started_at, p.id
