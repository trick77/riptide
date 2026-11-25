-- Silences by stored time, as 0012 for pipeline_events; the last build
-- holding bitbucket_events' INSERTs.
CREATE INDEX ix_bitbucket_events_team_created ON bitbucket_events (team, created_at);
