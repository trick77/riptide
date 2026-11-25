-- Silences by stored time, as 0012 for pipeline_events.
CREATE INDEX ix_noergler_events_team_created ON noergler_events (team, created_at);
