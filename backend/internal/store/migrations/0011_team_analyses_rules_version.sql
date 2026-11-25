-- rules_version: the assessment rules (metrics.RulesVersion) an analysis
-- was assessed under. A row stores its signals, question and text as those
-- rules raised them; after a rule changes (a signal dropped, a threshold
-- moved) the page must not show it as the assessment's wording. Nullable,
-- no default: a binary from before this column inserts and finishes rows
-- without it, and those read as older than any version, as every row
-- written before it does.
ALTER TABLE team_analyses ADD COLUMN rules_version INTEGER;
COMMENT ON COLUMN team_analyses.rules_version IS 'metrics.RulesVersion the assessment was made under; NULL: before the column, older than any';
