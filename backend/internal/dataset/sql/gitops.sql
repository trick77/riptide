-- GitOps repos: their commits are what argocd_events.revision points at
-- (docs/metrics.md, "Reading the event tables"), pushed or the merge commit
-- of a PR merged in the UI (Bitbucket sends no push for it; the payload keeps
-- the sender's casing, revisions are lowercased). Over all data, not the
-- window, read once per dataset and handed to prs, open_prs, runs and
-- reviews as an array: one definition, one scan of the event tables.
SELECT coalesce(array_agg(DISTINCT repo_full_name), '{}')::text[] AS repos
FROM (
  SELECT repo_full_name
  FROM bitbucket_events
  WHERE commit_sha IN (SELECT revision FROM argocd_events)
    AND repo_full_name IS NOT NULL
  UNION
  SELECT repo_full_name
  FROM bitbucket_events
  WHERE event_type = 'pr:merged' AND repo_full_name IS NOT NULL
    AND lower(payload->'pullRequest'->'properties'->'mergeCommit'->>'id') IN (SELECT revision FROM argocd_events)
) g
