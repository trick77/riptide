-- Silences by stored time, as 0012 for pipeline_events.
CREATE INDEX ix_argocd_events_team_created ON argocd_events (team, created_at);
