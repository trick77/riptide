-- GitOps repos: their commits are what argocd_events.revision points at
-- (docs/metrics.md, "Reading the event tables"). Over all data, not the
-- window, read once per dataset and handed to prs, open_prs, runs and
-- reviews as an array: one definition, one scan of the event tables.
SELECT coalesce(array_agg(DISTINCT repo_full_name), '{}')::text[] AS repos
FROM bitbucket_events
WHERE commit_sha IN (SELECT revision FROM argocd_events)
  AND repo_full_name IS NOT NULL
