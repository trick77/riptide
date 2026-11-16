-- team_analyses: the LLM's wording of a team's assessment over closed
-- weeks, one row per attempt. Text about computed numbers, never the source
-- of one: kept for its cost, for the history the next assessment reads
-- (what recurs, what is gone) and so a page view never calls the model.
-- The assessment it worded is stored with it, numbers and all, so the page
-- shows the text next to the numbers it was written from.
CREATE TABLE team_analyses (
    id            BIGSERIAL PRIMARY KEY,
    team          TEXT NOT NULL,
    weeks         INTEGER NOT NULL,
    product       TEXT NOT NULL DEFAULT '',
    bots          BOOLEAN NOT NULL DEFAULT FALSE,
    period_from   TIMESTAMPTZ NOT NULL,
    period_to     TIMESTAMPTZ NOT NULL,
    trigger       TEXT NOT NULL CHECK (trigger IN ('scheduled', 'manual')),
    status        TEXT NOT NULL CHECK (status IN ('running', 'done', 'failed')),
    model         TEXT NOT NULL,
    input_hash    TEXT,
    assessment    JSONB,
    output        JSONB,
    report        JSONB,
    input_tokens  BIGINT,
    output_tokens BIGINT,
    cost_nano_usd BIGINT,
    error         TEXT,
    attempts      INTEGER NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ
);
COMMENT ON TABLE team_analyses IS 'LLM wording of a team assessment, one row per attempt; never a source of numbers';
COMMENT ON COLUMN team_analyses.product IS 'Bitbucket project key of the view; empty for all products';
COMMENT ON COLUMN team_analyses.period_to IS 'end of the last closed week the assessment covers (Monday 00:00 in BACKEND_ANALYSIS_TZ)';
COMMENT ON COLUMN team_analyses.input_hash IS 'model, prompt and digest: the same input is never asked twice';
COMMENT ON COLUMN team_analyses.assessment IS 'the metrics.Assessment worded, numbers and all';
COMMENT ON COLUMN team_analyses.output IS 'the validated answer, de and en, repos named';
COMMENT ON COLUMN team_analyses.report IS 'what the validator dropped and which mandatory signals the answer missed';
COMMENT ON COLUMN team_analyses.attempts IS 'tries of a scheduled run: a failed or abandoned one is claimed again, up to a cap';
COMMENT ON COLUMN team_analyses.cost_nano_usd IS 'billionths of a USD: the gateway''s figure, or the list price for a vendor called directly; NULL unpriced';

-- One scheduled run per view and closed week, however many replicas tick.
CREATE UNIQUE INDEX team_analyses_scheduled
    ON team_analyses (team, weeks, product, bots, period_to) WHERE trigger = 'scheduled';
CREATE INDEX team_analyses_view ON team_analyses (team, weeks, product, bots, created_at DESC);
