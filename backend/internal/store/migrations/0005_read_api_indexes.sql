-- The read API's indexes.
--
-- Plain CREATE INDEX, not CONCURRENTLY, though it holds a lock that blocks
-- INSERTs into the table while it builds. CONCURRENTLY waits for every
-- transaction in the database that started before it, and kit's migrate
-- holds its session advisory lock while it runs: a second migrator waiting
-- for that lock (another replica's init container) is such a transaction,
-- and waits in turn for the build. Measured: "deadlock detected" in ten
-- tests of one run, where the test packages migrate one database side by
-- side. A webhook arriving during a build waits for it, it is not refused.

-- The read API (/api/dataset) selects every source by team and an
-- occurred_at window. Without these each request scans all teams' rows.
CREATE INDEX ix_bitbucket_events_team_occurred ON bitbucket_events (team, occurred_at);
CREATE INDEX ix_pipeline_events_team_occurred ON pipeline_events (team, occurred_at);
CREATE INDEX ix_argocd_events_team_occurred ON argocd_events (team, occurred_at);
CREATE INDEX ix_noergler_events_team_occurred ON noergler_events (team, occurred_at);

-- A PR's whole history by its key: PR ids are numbered per repository, so
-- the key is (repo_full_name, pr_id), and ix_bitbucket_events_pr_id alone
-- matches every repository's PR of that number.
CREATE INDEX ix_bitbucket_events_repo_pr ON bitbucket_events (repo_full_name, pr_id) WHERE pr_id IS NOT NULL;
