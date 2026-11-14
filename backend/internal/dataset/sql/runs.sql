-- One row per finished CI run of the team in the window (phase COMPLETED;
-- a run also reports STARTED, which would count it twice). The branch and
-- its kind are read off pipeline_name in Go.
--
-- The run's repository is the one Bitbucket saw its commit in: pipeline and
-- Bitbucket rows join on the App-repo commit SHA (README). One repository
-- per run, however many events carry the SHA (a push and the PR's events).
--
-- $1 team, $2 window start (inclusive), $3 window end (exclusive).
SELECT p.source,
       p.pipeline_name,
       p.build_type,
       p.run_id,
       p.status,
       p.started_at,
       p.finished_at,
       p.duration_seconds AS duration_s,
       r.repo_full_name AS repo,
       split_part(r.repo_full_name, '/', 1) AS project,
       p.image_refs::text[] AS image_refs
FROM pipeline_events p
LEFT JOIN LATERAL (
  SELECT b.repo_full_name
  FROM bitbucket_events b
  WHERE b.commit_sha = p.commit_sha AND b.repo_full_name IS NOT NULL
  ORDER BY b.occurred_at
  LIMIT 1
) r ON true
WHERE p.team = $1 AND p.occurred_at >= $2 AND p.occurred_at < $3
  AND p.phase = 'COMPLETED'
ORDER BY p.occurred_at, p.id
