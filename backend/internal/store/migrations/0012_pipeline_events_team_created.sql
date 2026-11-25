-- The read path's lookups that had no index, one table per migration
-- (0012 to 0016): kit commits each file on its own, so a build holds its
-- table's lock only while that table's index builds, never another
-- table's.
--
-- A plain index build inside kit's transaction, as 0005 explains: CONCURRENTLY
-- deadlocks against a second migrator waiting for kit's advisory lock. The
-- build blocks INSERTs into its table while it runs: a webhook waits for it.
-- A sender that times out meanwhile may not retry (Bitbucket does not), so
-- bitbucket_events comes last, its two indexes in two files. A replica on
-- the old binary keeps working meanwhile: nothing here changes a column.

-- Silences (sql/silences.sql) are read off when riptide stored a team's
-- events, not when they say they happened: a sender's retry storing an old
-- event late must neither split nor fill a hole.
CREATE INDEX ix_pipeline_events_team_created ON pipeline_events (team, created_at);
