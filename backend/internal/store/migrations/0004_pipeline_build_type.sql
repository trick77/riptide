-- build_type: what kind of build a run was, as the pipeline reports it
-- (release, hotfix, build, ...). Lowercased, free text: riptide does not
-- decide which values exist. Sent on COMPLETED events only; NULL when the
-- sender did not say.
ALTER TABLE pipeline_events ADD COLUMN build_type VARCHAR;
COMMENT ON COLUMN pipeline_events.build_type IS 'kind of build as the pipeline reports it (release, hotfix, build, ...); trimmed, lowercased, NULL when not sent';

-- The parser's rule in SQL: a string, trimmed of any whitespace (tabs and
-- newlines too, like Go's strings.TrimSpace; btrim strips spaces only) and
-- lowercased, blank is absent. A non-string (which the parser rejects, but a
-- pre-0004 collector stored) is absent too, never its text.
CREATE FUNCTION pipeline_events_payload_build_type(p_payload JSONB) RETURNS VARCHAR
LANGUAGE sql IMMUTABLE AS $$
    SELECT nullif(lower(regexp_replace(p_payload->>'build_type', '^\s+|\s+$', '', 'g')), '')
    WHERE jsonb_typeof(p_payload->'build_type') = 'string'
$$;

-- Old pods mid-rollout (or after a rollback) keep build_type only in the
-- payload: fill the column for them, as 0003 does for image_refs. A row that
-- already carries one is left alone.
CREATE FUNCTION pipeline_events_fill_build_type() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.build_type IS NULL THEN
        NEW.build_type := pipeline_events_payload_build_type(NEW.payload);
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER trg_pipeline_events_fill_build_type BEFORE INSERT ON pipeline_events
    FOR EACH ROW EXECUTE FUNCTION pipeline_events_fill_build_type();

-- Backfill the rows written before this migration. The modified_at trigger is
-- off: it marks operator fix-ups, and a schema change is not one.
ALTER TABLE pipeline_events DISABLE TRIGGER trg_pipeline_events_modified_at;
UPDATE pipeline_events
SET build_type = pipeline_events_payload_build_type(payload)
WHERE payload ? 'build_type';
ALTER TABLE pipeline_events ENABLE TRIGGER trg_pipeline_events_modified_at;

-- No index on build_type: the read API returns it per run and the UI filters
-- in the browser; nothing selects by it. The read API's indexes are 0005's.

-- deploy_commit_ranges, same columns and meaning as in 0001, planned so its
-- cost grows with history instead of with its square. 0001 looked up each
-- range boundary in commit_sightings, a view the planner cannot estimate:
-- it hashed on the repo slug alone and expanded every master push's commits
-- once per (bump, push) pair, measured 14 s on a year of one team's data.
-- Here the master commits are expanded once and both boundaries are joined
-- on SHA, and the release commits are found through the Argo CD revisions
-- (commit_sha index) before their text is searched, never by casting every
-- payload in the table.
CREATE OR REPLACE VIEW deploy_commit_ranges AS
WITH release_bump AS MATERIALIZED (
    -- Per deployed GitOps revision, the component bumps its release commit
    -- lists, parsed once per revision rather than once per deploy of it.
    -- OFFSET 0 keeps the lookup per revision on the commit_sha index: pulled
    -- up into a join, the planner casts every payload in the table instead.
    SELECT rev.revision,
           -- Slugs may carry '_' and '.', so the class is wider than it
           -- looks; the join below compares the slug exactly rather than
           -- with LIKE, where '_' would act as a wildcard.
           regexp_matches(
               b.body,
               'repos/([a-z0-9._-]+)/compare/diff\?targetBranch=([0-9a-f]{40})&sourceBranch=([0-9a-f]{40})',
               'g'
           ) AS m
    FROM (SELECT DISTINCT revision FROM argocd_events WHERE operation_phase = 'Succeeded') rev
    CROSS JOIN LATERAL (
        SELECT e.payload::text AS body
        FROM bitbucket_events e
        WHERE e.commit_sha = rev.revision
        OFFSET 0
    ) b
    -- prefilter: keeps the regex off bodies without a compare link
    WHERE b.body LIKE '%compare/diff?targetBranch=%'
),
bump AS (
    -- The same bump seen twice (two events carrying the release commit)
    -- is one range, not two.
    SELECT DISTINCT a.app_name, a.environment, a.team, a.occurred_at AS deployed_at, rb.m
    FROM argocd_events a
    JOIN release_bump rb ON rb.revision = a.revision
    WHERE a.operation_phase = 'Succeeded'
),
sightings AS MATERIALIZED (
    SELECT repo_full_name, split_part(repo_full_name, '/', 2) AS slug, commit_sha, seen_at
    FROM commit_sightings
)
SELECT
    bump.app_name,
    bump.environment,
    bump.team,
    bump.deployed_at,
    prev.repo_full_name,
    bump.m[2]    AS range_start_sha,
    bump.m[3]    AS range_end_sha,
    prev.seen_at AS range_start_at,
    curr.seen_at AS range_end_at
FROM bump
JOIN sightings prev
  ON prev.commit_sha = bump.m[2]
 AND prev.slug = bump.m[1]
JOIN sightings curr
  ON curr.commit_sha = bump.m[3]
 AND curr.repo_full_name = prev.repo_full_name;
