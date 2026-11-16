-- One row per finished CI run of the team in the window (phase COMPLETED;
-- a run also reports STARTED, which would count it twice; only its agent
-- time is read from there). The branch and
-- its kind are read off pipeline_name in Go.
--
-- The run's repository is the one Bitbucket saw its commit in: pipeline and
-- Bitbucket rows join on the App-repo commit SHA (docs/metrics.md). One repository
-- per run, however many events carry the SHA (a push and the PR's events).
--
-- `gitops` flags a run in a GitOps repo, as in prs.sql: a stage promotion's
-- check is mechanical, not the team's build; Go leaves it out of the build
-- numbers.
--
-- $1 team, $2 window start (inclusive), $3 window end (exclusive).
WITH gitops_repos AS (
  -- GitOps repos: their commits are what argocd_events.revision points at
  -- (docs/metrics.md, "Reading the event tables"). Over all data, not the window.
  SELECT DISTINCT repo_full_name
  FROM bitbucket_events
  WHERE commit_sha IN (SELECT revision FROM argocd_events)
    AND repo_full_name IS NOT NULL
)
SELECT p.source,
       p.pipeline_name,
       p.build_type,
       p.run_id,
       p.commit_sha,
       p.status,
       p.started_at,
       COALESCE(p.agent_started_at, s.agent_started_at) AS agent_started_at,
       p.finished_at,
       p.duration_seconds AS duration_s,
       r.repo_full_name AS repo,
       split_part(r.repo_full_name, '/', 1) AS project,
       p.image_refs::text[] AS image_refs,
       coalesce(r.repo_full_name IN (SELECT repo_full_name FROM gitops_repos), false) AS gitops
FROM pipeline_events p
LEFT JOIN LATERAL (
  SELECT b.repo_full_name
  FROM bitbucket_events b
  WHERE b.commit_sha = p.commit_sha AND b.repo_full_name IS NOT NULL
  ORDER BY b.occurred_at
  LIMIT 1
) r ON true
-- The agent time is known when the run gets its agent, so a sender may post
-- it on STARTED only: the same run's STARTED row of the same team (the
-- bearer is the team; another team may run a job of the same name). The
-- phase is the sender's spelling, so compared without case.
LEFT JOIN LATERAL (
  SELECT st.agent_started_at
  FROM pipeline_events st
  WHERE p.agent_started_at IS NULL
    AND st.team = p.team AND st.source = p.source AND st.pipeline_name = p.pipeline_name
    AND st.run_id = p.run_id AND upper(st.phase) = 'STARTED' AND st.agent_started_at IS NOT NULL
  ORDER BY st.id
  LIMIT 1
) s ON true
WHERE p.team = $1 AND p.occurred_at >= $2 AND p.occurred_at < $3
  AND p.phase = 'COMPLETED'
ORDER BY p.occurred_at, p.id
