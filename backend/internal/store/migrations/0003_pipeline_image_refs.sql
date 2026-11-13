-- image_refs: every image a run published. One release can push several (a
-- monorepo UI building three apps, a multi-module service), and a run must stay
-- one row: an event per image would multiply build counts and CI time.
--
-- Expand only. image_ref stays, unwritten by this collector, until a later
-- migration drops it with the trigger below: during a rolling update the old
-- pods still insert image_ref (and keep a sent image_refs only in the
-- payload), and a rollback to the old image must find its column. The trigger
-- fills image_refs for exactly those rows, so the deploy join sees them too.
--
-- NOT NULL DEFAULT '{}' like jira_keys: "published nothing" is an empty list,
-- never NULL, so the join needs no COALESCE.
ALTER TABLE pipeline_events ADD COLUMN image_refs VARCHAR[] NOT NULL DEFAULT '{}';
COMMENT ON COLUMN pipeline_events.image_refs IS 'full image references published by the run; each joins to an entry of argocd payload->''images''';
COMMENT ON COLUMN pipeline_events.image_ref IS 'deprecated, no longer written: folded into image_refs, dropped by a later migration';
-- Nothing reads image_ref any more; its index would only cost every insert.
DROP INDEX ix_pipeline_events_image_ref;

-- The image list a row carries outside image_refs: the old scalar column and
-- an image_refs kept in the payload. Same rules as the parser: scalar first,
-- trimmed, blanks and non-strings dropped, first occurrence wins.
CREATE FUNCTION pipeline_events_legacy_image_refs(p_ref VARCHAR, p_payload JSONB) RETURNS VARCHAR[]
LANGUAGE sql IMMUTABLE AS $$
    SELECT coalesce(array_agg(x.ref ORDER BY x.first), '{}')
    FROM (
        SELECT btrim(s.v) AS ref, min(s.ord) AS first
        FROM (
            SELECT p_ref AS v, 0::bigint AS ord
            UNION ALL
            SELECT t.el #>> '{}', t.ord
            FROM jsonb_array_elements(
                     CASE WHEN jsonb_typeof(p_payload->'image_refs') = 'array'
                          THEN p_payload->'image_refs' END) WITH ORDINALITY AS t(el, ord)
            WHERE jsonb_typeof(t.el) = 'string'
        ) s
        WHERE btrim(s.v) <> ''
        GROUP BY btrim(s.v)
    ) x
$$;

CREATE FUNCTION pipeline_events_fill_image_refs() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.image_refs = '{}' THEN
        NEW.image_refs := pipeline_events_legacy_image_refs(NEW.image_ref, NEW.payload);
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER trg_pipeline_events_fill_image_refs BEFORE INSERT ON pipeline_events
    FOR EACH ROW EXECUTE FUNCTION pipeline_events_fill_image_refs();

-- Backfill the rows written before this migration. The modified_at trigger is
-- off: it marks operator fix-ups, and a schema change is not one.
ALTER TABLE pipeline_events DISABLE TRIGGER trg_pipeline_events_modified_at;
UPDATE pipeline_events
SET image_refs = pipeline_events_legacy_image_refs(image_ref, payload)
WHERE image_ref IS NOT NULL OR payload ? 'image_refs';
ALTER TABLE pipeline_events ENABLE TRIGGER trg_pipeline_events_modified_at;

-- After the backfill: built in bulk instead of maintained row by row, which
-- keeps the migration's lock window short. Overlap (&&) and containment (@>)
-- use it; `= ANY(image_refs)` does not.
CREATE INDEX ix_pipeline_events_image_refs_gin ON pipeline_events USING gin (image_refs);
