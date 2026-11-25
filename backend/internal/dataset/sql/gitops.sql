-- GitOps repos: their commits are what argocd_events.revision points at
-- (docs/metrics.md, "Reading the event tables"), pushed or the merge commit
-- of a PR merged in the UI (Bitbucket sends no push for it; the payload keeps
-- the sender's casing, revisions are lowercased). Over all data, not the
-- window, read once per dataset and handed to prs, open_prs, runs and
-- reviews as an array: one definition, one scan of the event tables. The
-- merge commits are probed per distinct revision
-- (ix_bitbucket_events_merge_commit), never by reading every payload.
SELECT coalesce(array_agg(DISTINCT repo_full_name), '{}')::text[] AS repos
FROM (
  SELECT repo_full_name
  FROM bitbucket_events
  WHERE commit_sha IN (SELECT revision FROM argocd_events)
    AND repo_full_name IS NOT NULL
  UNION
  SELECT b.repo_full_name
  FROM (SELECT DISTINCT revision FROM argocd_events) r
  JOIN bitbucket_events b
    ON b.event_type = 'pr:merged'
   AND lower(b.payload->'pullRequest'->'properties'->'mergeCommit'->>'id') = r.revision
  WHERE b.repo_full_name IS NOT NULL
) g
