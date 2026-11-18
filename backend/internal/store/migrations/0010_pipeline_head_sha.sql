-- head_sha and base_sha: a PR build of a merge. A PR build that checks out
-- a merge of the PR with its target branch (Jenkins' Bitbucket Branch
-- Source, "merge with the target") reports as commit_sha a merge commit
-- Jenkins made itself, new on every build: a rerun of the same code never
-- shares it, and Bitbucket never saw it. head_sha is the PR's own head (the
-- merge's second parent), base_sha the target branch's tip it was merged
-- onto (the first). Together they are the code built: the same head on a
-- moved target is other code. NULL when the sender did not send them.
ALTER TABLE pipeline_events ADD COLUMN head_sha VARCHAR;
ALTER TABLE pipeline_events ADD COLUMN base_sha VARCHAR;
COMMENT ON COLUMN pipeline_events.head_sha IS 'a PR build''s own head when commit_sha is a merge the CI made of it with the target branch; lowercased, NULL otherwise';
COMMENT ON COLUMN pipeline_events.base_sha IS 'the target branch''s tip that merge was made onto, with head_sha; lowercased, NULL otherwise';
-- A run that failed before its checkout (no agent, a git timeout) has no
-- commit and is still a run: the column always allowed NULL, the API now
-- takes such runs.
COMMENT ON COLUMN pipeline_events.commit_sha IS 'the commit built, lowercased; NULL for a run that failed before its checkout';

-- The parser's rule in SQL: a string trimmed of any whitespace (like Go's
-- strings.TrimSpace; btrim strips spaces only), 7 characters or more once
-- trimmed, lowercased. Anything else (which the parser rejects, but a
-- pre-0010 collector stored in the payload) is absent.
CREATE FUNCTION pipeline_events_payload_sha(p_payload JSONB, p_field TEXT) RETURNS VARCHAR
LANGUAGE sql IMMUTABLE AS $$
    SELECT lower(t.v)
    FROM (SELECT regexp_replace(p_payload->>p_field, '^\s+|\s+$', '', 'g') AS v) t
    WHERE jsonb_typeof(p_payload->p_field) = 'string' AND length(t.v) >= 7
$$;

-- Old pods mid-rollout (or after a rollback) keep both only in the payload:
-- fill the columns for them, as 0004 does for build_type. A row that
-- already carries one is left alone.
CREATE FUNCTION pipeline_events_fill_merge_shas() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.head_sha IS NULL THEN
        NEW.head_sha := pipeline_events_payload_sha(NEW.payload, 'head_sha');
    END IF;
    IF NEW.base_sha IS NULL THEN
        NEW.base_sha := pipeline_events_payload_sha(NEW.payload, 'base_sha');
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER trg_pipeline_events_fill_merge_shas BEFORE INSERT ON pipeline_events
    FOR EACH ROW EXECUTE FUNCTION pipeline_events_fill_merge_shas();

-- Backfill the rows written before this migration. The modified_at trigger is
-- off: it marks operator fix-ups, and a schema change is not one.
ALTER TABLE pipeline_events DISABLE TRIGGER trg_pipeline_events_modified_at;
UPDATE pipeline_events
SET head_sha = pipeline_events_payload_sha(payload, 'head_sha'),
    base_sha = pipeline_events_payload_sha(payload, 'base_sha')
WHERE payload ? 'head_sha' OR payload ? 'base_sha';
ALTER TABLE pipeline_events ENABLE TRIGGER trg_pipeline_events_modified_at;

-- No index on either: runs.sql looks Bitbucket up by head_sha (bitbucket_events'
-- commit_sha index), nothing selects pipeline rows by them.
