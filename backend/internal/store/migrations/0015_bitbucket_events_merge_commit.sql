-- A PR merged in the Bitbucket UI sends no push: its merge commit is known
-- only from the pr:merged payload, in the sender's casing. Argo CD's
-- revision (the product of a deploy, sql/fragments/revision_project.sql),
-- the GitOps repos (sql/gitops.sql) and a CI run's commit
-- (sql/silences.sql) are looked up by it, one probe per SHA, lowercased as
-- every stored SHA is. Queries must repeat the expression and the predicate
-- to use it. The pr:merged events only, a small part of the table, but the
-- build reads all of it once, holding its INSERTs (0012): one index a file.
CREATE INDEX ix_bitbucket_events_merge_commit
    ON bitbucket_events ((lower(payload->'pullRequest'->'properties'->'mergeCommit'->>'id')))
    WHERE event_type = 'pr:merged';
