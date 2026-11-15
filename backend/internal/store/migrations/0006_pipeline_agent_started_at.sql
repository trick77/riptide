-- agent_started_at: when a CI run got its build agent. started_at is when
-- the CI started the run, before it waited for one; the wait between the
-- two grows with cluster load and is no build time. Optional; NULL when the
-- sender did not say.
ALTER TABLE pipeline_events ADD COLUMN agent_started_at TIMESTAMPTZ;
COMMENT ON COLUMN pipeline_events.agent_started_at IS 'when the run got its build agent; started_at to here is the wait for one; NULL when not sent';

-- The payload's value as a timestamp, for rows the parser did not fill: an
-- ISO 8601 date and time, without an offset taken as UTC like the parser
-- does. Anything else is absent, never an error: senders posted the field
-- before riptide read it, unchecked, and one bad row must not fail this
-- migration (the init container would crash-loop on it) nor an insert
-- through the trigger below. The pattern comes before the cast because
-- timestamptz takes more than ISO 8601: 'infinity' would break every read of
-- the team's runs (pgx scans no infinite time), 'now' would store the
-- migration's time, and a date alone would be read in the session time
-- zone. Epoch numbers, which the parser accepts too, are left out: no
-- sender posted one before the parser read the field.
CREATE FUNCTION pipeline_events_payload_agent_started_at(p_payload JSONB) RETURNS TIMESTAMPTZ
LANGUAGE plpgsql STABLE AS $$
DECLARE
    v TEXT;
BEGIN
    IF jsonb_typeof(p_payload->'agent_started_at') IS DISTINCT FROM 'string' THEN
        RETURN NULL;
    END IF;
    v := btrim(p_payload->>'agent_started_at');
    IF v !~ '^\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?([Zz]|[+-]\d{2}(:?\d{2})?)?$' THEN
        RETURN NULL;
    END IF;
    IF v !~ '([Zz]|[+-]\d{2}(:?\d{2})?)$' THEN
        v := v || 'Z';
    END IF;
    -- Shaped right, still not a time (2026-02-30): absent too. The block
    -- costs a subtransaction, so only the cast sits in it.
    BEGIN
        RETURN v::timestamptz;
    EXCEPTION WHEN others THEN
        RETURN NULL;
    END;
END
$$;

-- Old pods mid-rollout (or after a rollback) keep agent_started_at only in
-- the payload: fill the column for them, as 0004 does for build_type. A row
-- that already carries one is left alone.
CREATE FUNCTION pipeline_events_fill_agent_started_at() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.agent_started_at IS NULL AND NEW.payload ? 'agent_started_at' THEN
        NEW.agent_started_at := pipeline_events_payload_agent_started_at(NEW.payload);
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER trg_pipeline_events_fill_agent_started_at BEFORE INSERT ON pipeline_events
    FOR EACH ROW EXECUTE FUNCTION pipeline_events_fill_agent_started_at();

-- Backfill the rows written before this migration. The modified_at trigger is
-- off: it marks operator fix-ups, and a schema change is not one.
ALTER TABLE pipeline_events DISABLE TRIGGER trg_pipeline_events_modified_at;
UPDATE pipeline_events
SET agent_started_at = pipeline_events_payload_agent_started_at(payload)
WHERE payload ? 'agent_started_at';
ALTER TABLE pipeline_events ENABLE TRIGGER trg_pipeline_events_modified_at;
