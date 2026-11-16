-- The page has no bots switch any more: every view counts people's PRs
-- only, so a view is team, window and product. Analyses of a bots view
-- go with the column.
DELETE FROM team_analyses WHERE bots;
DROP INDEX team_analyses_scheduled;
DROP INDEX team_analyses_view;
ALTER TABLE team_analyses DROP COLUMN bots;

-- One scheduled run per view and closed week, however many replicas tick.
CREATE UNIQUE INDEX team_analyses_scheduled
    ON team_analyses (team, weeks, product, period_to) WHERE trigger = 'scheduled';
CREATE INDEX team_analyses_view ON team_analyses (team, weeks, product, created_at DESC);
