-- The release commits of the team's deployed GitOps revisions that carry a
-- compare link (docs/correlating-deploys-to-commits.md): every event whose
-- commit is a revision the team deployed successfully, its body as text.
-- Go reads the links out of it (dataset.compareLinks): Go's regular
-- expressions run in linear time, Postgres' took most of the old range
-- view's time on the same bodies. Each revision's events once, each body
-- cast once (OFFSET 0 keeps the lookup per revision on the commit_sha
-- index, as libraries.sql does); the LIKE keeps every other body out.
--
-- $1 team.
SELECT rev.revision, b.body
FROM (SELECT DISTINCT revision FROM argocd_events WHERE team = $1 AND operation_phase = 'Succeeded') rev
CROSS JOIN LATERAL (
  SELECT e.payload::text AS body
  FROM bitbucket_events e
  WHERE e.commit_sha = rev.revision
  OFFSET 0
) b
WHERE b.body LIKE '%compare/diff?targetBranch=%'
ORDER BY rev.revision
