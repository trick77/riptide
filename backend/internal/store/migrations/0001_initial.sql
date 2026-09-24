-- Initial schema: the four append-only event tables, the modified_at
-- trigger, and the lead-time views.
--
-- This is the schema the Python collector reached at Alembic revision 0004,
-- squashed into one file for the Go rewrite. Every counter column is BIGINT,
-- where Alembic had INTEGER, so a large value never overflows into a 500.
-- Column order follows Alembic's (columns added by 0002/0003 come after
-- payload), so a CSV exported from the Python-era database reloads with a
-- plain \copy.
--
-- Never edit this file once applied; add 0002_*.sql.

-- modified_at is kept by a trigger, not only by the application, so a raw-SQL
-- UPDATE bumps it too. Event rows are append-only; the trigger is for the rare
-- operator fix-up.
CREATE OR REPLACE FUNCTION riptide_set_modified_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.modified_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE bitbucket_events (
    id                  BIGSERIAL PRIMARY KEY,
    delivery_id         VARCHAR NOT NULL,
    event_type          VARCHAR NOT NULL,
    repo_full_name      VARCHAR,
    pr_id               BIGINT,
    commit_sha          VARCHAR,
    author              VARCHAR,
    branch_name         VARCHAR,
    change_type         VARCHAR,
    jira_keys           VARCHAR[] NOT NULL DEFAULT '{}',
    automation_source   VARCHAR,
    is_automated        BOOLEAN NOT NULL GENERATED ALWAYS AS (automation_source IS NOT NULL) STORED,
    lines_added         BIGINT,
    lines_removed       BIGINT,
    files_changed       BIGINT,
    is_revert           BOOLEAN NOT NULL DEFAULT false,
    occurred_at         TIMESTAMPTZ NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    team                VARCHAR,
    payload             JSONB NOT NULL,
    author_display_name VARCHAR,
    CONSTRAINT uq_bitbucket_events_delivery_id UNIQUE (delivery_id)
);
COMMENT ON COLUMN bitbucket_events.author_display_name IS 'actor.displayName; bot accounts often identify themselves only here';
CREATE INDEX ix_bitbucket_events_repo_full_name ON bitbucket_events (repo_full_name);
CREATE INDEX ix_bitbucket_events_pr_id ON bitbucket_events (pr_id);
CREATE INDEX ix_bitbucket_events_commit_sha ON bitbucket_events (commit_sha);
CREATE INDEX ix_bitbucket_events_jira_keys_gin ON bitbucket_events USING gin (jira_keys);
-- Serves the lead-time views: repo equality + branch equality + occurred_at
-- range, which is exactly how a release window is looked up.
CREATE INDEX ix_bitbucket_events_repo_branch_occurred ON bitbucket_events (repo_full_name, branch_name, occurred_at);

CREATE TABLE pipeline_events (
    id                 BIGSERIAL PRIMARY KEY,
    delivery_id        VARCHAR NOT NULL,
    source             VARCHAR NOT NULL,
    pipeline_name      VARCHAR NOT NULL,
    run_id             VARCHAR NOT NULL,
    phase              VARCHAR NOT NULL,
    status             VARCHAR,
    commit_sha         VARCHAR,
    started_at         TIMESTAMPTZ,
    finished_at        TIMESTAMPTZ,
    duration_seconds   INTEGER GENERATED ALWAYS AS (EXTRACT(EPOCH FROM finished_at - started_at)::int) STORED,
    occurred_at        TIMESTAMPTZ NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    team               VARCHAR,
    payload            JSONB NOT NULL,
    image_ref          VARCHAR,
    actor_handle       VARCHAR,
    actor_account_kind VARCHAR,
    CONSTRAINT uq_pipeline_events_delivery_id UNIQUE (delivery_id)
);
COMMENT ON COLUMN pipeline_events.source IS 'ci system that produced the event: jenkins / tekton / etc.';
COMMENT ON COLUMN pipeline_events.run_id IS 'ci-system run identifier (Jenkins build number, Tekton PipelineRun name)';
COMMENT ON COLUMN pipeline_events.image_ref IS 'full image reference published by the run; joins to argocd payload->''images''';
COMMENT ON COLUMN pipeline_events.actor_handle IS 'git-host account this CI acts through; self-declared';
COMMENT ON COLUMN pipeline_events.actor_account_kind IS 'bot | service | human — what actor_handle is, per the sender';
CREATE INDEX ix_pipeline_events_source ON pipeline_events (source);
CREATE INDEX ix_pipeline_events_pipeline_name ON pipeline_events (pipeline_name);
CREATE INDEX ix_pipeline_events_commit_sha ON pipeline_events (commit_sha);
CREATE INDEX ix_pipeline_events_image_ref ON pipeline_events (image_ref);

CREATE TABLE argocd_events (
    id                    BIGSERIAL PRIMARY KEY,
    delivery_id           VARCHAR NOT NULL,
    app_name              VARCHAR NOT NULL,
    revision              VARCHAR NOT NULL,
    sync_status           VARCHAR,
    destination_namespace VARCHAR,
    environment           VARCHAR,
    operation_phase       VARCHAR,
    started_at            TIMESTAMPTZ,
    finished_at           TIMESTAMPTZ,
    duration_seconds      INTEGER GENERATED ALWAYS AS (EXTRACT(EPOCH FROM finished_at - started_at)::int) STORED,
    occurred_at           TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    team                  VARCHAR,
    payload               JSONB NOT NULL,
    CONSTRAINT uq_argocd_events_delivery_id UNIQUE (delivery_id)
);
COMMENT ON COLUMN argocd_events.destination_namespace IS 'kubernetes namespace the app deployed into, as Argo CD reported it (trimmed)';
COMMENT ON COLUMN argocd_events.environment IS 'lowercased suffix of destination_namespace after the last ''-''; matches config.environments.production_stage for prod metrics';
CREATE INDEX ix_argocd_events_app_name ON argocd_events (app_name);
CREATE INDEX ix_argocd_events_revision ON argocd_events (revision);
CREATE INDEX ix_argocd_events_environment ON argocd_events (environment);

CREATE TABLE noergler_events (
    id                    BIGSERIAL PRIMARY KEY,
    delivery_id           VARCHAR NOT NULL,
    event_type            VARCHAR NOT NULL,
    pr_key                VARCHAR,
    repo                  VARCHAR,
    commit_sha            VARCHAR,
    prompt_tokens         BIGINT,
    completion_tokens     BIGINT,
    elapsed_ms            BIGINT,
    findings_count        BIGINT,
    cost_usd              NUMERIC(12, 6),
    finding_id            VARCHAR,
    verdict               VARCHAR,
    actor                 VARCHAR,
    occurred_at           TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    modified_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    team                  VARCHAR,
    payload               JSONB NOT NULL,
    outcome               VARCHAR,
    merge_commit_sha      VARCHAR,
    lines_added           BIGINT,
    lines_removed         BIGINT,
    files_changed         BIGINT,
    total_runs            BIGINT,
    models_used           VARCHAR[],
    first_review_at       TIMESTAMPTZ,
    reviewer_handle       VARCHAR,
    reviewer_account_kind VARCHAR,
    CONSTRAINT uq_noergler_events_delivery_id UNIQUE (delivery_id)
);
COMMENT ON COLUMN noergler_events.event_type IS 'pr_completed | feedback';
COMMENT ON COLUMN noergler_events.reviewer_handle IS 'git-host account the reviewer posts under; self-reported automation identity';
COMMENT ON COLUMN noergler_events.reviewer_account_kind IS 'bot | service | human — what reviewer_handle is, per the sender';
CREATE INDEX ix_noergler_events_event_type ON noergler_events (event_type);
CREATE INDEX ix_noergler_events_pr_key ON noergler_events (pr_key);
CREATE INDEX ix_noergler_events_commit_sha ON noergler_events (commit_sha);
CREATE INDEX ix_noergler_events_outcome ON noergler_events (outcome);

CREATE TRIGGER trg_bitbucket_events_modified_at BEFORE UPDATE ON bitbucket_events
    FOR EACH ROW EXECUTE FUNCTION riptide_set_modified_at();
CREATE TRIGGER trg_pipeline_events_modified_at BEFORE UPDATE ON pipeline_events
    FOR EACH ROW EXECUTE FUNCTION riptide_set_modified_at();
CREATE TRIGGER trg_argocd_events_modified_at BEFORE UPDATE ON argocd_events
    FOR EACH ROW EXECUTE FUNCTION riptide_set_modified_at();
CREATE TRIGGER trg_noergler_events_modified_at BEFORE UPDATE ON noergler_events
    FOR EACH ROW EXECUTE FUNCTION riptide_set_modified_at();

-- Lead-time views. Plain views, not materialized: nothing to refresh, so the
-- no-rollup-jobs invariant holds. docs/dora-lead-time.md explains them.
--
-- commit_sightings: one row per commit of a master push. Master pushes only: a
-- change enters the release stream when it lands on the integration branch,
-- and feature-branch pushes would count it twice. Bitbucket caps commits[] at
-- 5 per push; measured, that bites 0.6 % of master pushes, and it drops
-- changes rather than mis-timing the ones it keeps.
CREATE VIEW commit_sightings AS
SELECT
    b.repo_full_name,
    b.team,
    c ->> 'id'                                                AS commit_sha,
    to_timestamp((c ->> 'authorTimestamp')::bigint / 1000)    AS authored_at,
    to_timestamp((c ->> 'committerTimestamp')::bigint / 1000) AS committed_at,
    b.occurred_at                                             AS seen_at,
    c -> 'author' ->> 'name'                                  AS author,
    c -> 'author' ->> 'displayName'                           AS author_display_name,
    -- The git host states this itself; a SERVICE account is release tooling,
    -- and its commits are created by the release rather than shipped by it.
    upper(coalesce(c -> 'author' ->> 'type', 'NORMAL')) = 'SERVICE'
                                                              AS author_is_service_account,
    coalesce(jsonb_array_length(c -> 'parents'), 0) > 1        AS is_merge,
    c ->> 'message'                                           AS message
FROM bitbucket_events b
CROSS JOIN LATERAL jsonb_array_elements(b.payload -> 'commits') AS c
WHERE b.event_type = 'repo:refs_changed'
  AND b.branch_name = 'master';

-- deploy_commit_ranges: which App-repo commit range a deploy shipped. The
-- GitOps release commit lists every component it bumped as a compare link
-- carrying the previous and the new App-repo SHA; those two bound the range.
-- The LIKE prefilter keeps the regex off every push payload in the table.
--
-- Two assumptions, both stated in docs/dora-lead-time.md: the GitOps release
-- commit is a push tip (commit_sha stores the push's toHash), and both
-- boundary SHAs are push tips in the App repo, which makes the push-timestamp
-- range exact.
CREATE VIEW deploy_commit_ranges AS
WITH release_commit AS (
    SELECT DISTINCT
        a.app_name,
        a.environment,
        a.team,
        a.occurred_at   AS deployed_at,
        b.payload::text AS body
    FROM argocd_events a
    JOIN bitbucket_events b ON b.commit_sha = a.revision
    WHERE a.operation_phase = 'Succeeded'
      AND b.payload::text LIKE '%compare/diff?targetBranch=%'
),
bump AS (
    SELECT
        app_name, environment, team, deployed_at,
        -- Slugs may carry '_' and '.', so the class is wider than it looks;
        -- the repo join below compares the slug exactly rather than with
        -- LIKE, where '_' would act as a wildcard.
        regexp_matches(
            body,
            'repos/([a-z0-9._-]+)/compare/diff\?targetBranch=([0-9a-f]{40})&sourceBranch=([0-9a-f]{40})',
            'g'
        ) AS m
    FROM release_commit
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
JOIN commit_sightings prev
  ON prev.commit_sha = bump.m[2]
 AND split_part(prev.repo_full_name, '/', 2) = bump.m[1]
JOIN commit_sightings curr
  ON curr.commit_sha = bump.m[3]
 AND curr.repo_full_name = prev.repo_full_name;

-- lead_time_changes: every commit against the FIRST successful deploy per
-- environment whose range contained it, so a change rolled out to four Apps
-- of one service counts once. The range is matched by push time, so every
-- commit of a push belongs to one side of the boundary; a boundary commit
-- riptide never saw drops the whole window, never counts it as fast.
CREATE VIEW lead_time_changes AS
SELECT
    r.environment,
    s.repo_full_name,
    s.team,
    s.commit_sha,
    s.authored_at,
    s.committed_at,
    s.author,
    s.author_display_name,
    s.author_is_service_account,
    s.is_merge,
    min(r.deployed_at)                 AS first_deployed_at,
    min(r.deployed_at) - s.authored_at AS lead_time
FROM deploy_commit_ranges r
JOIN commit_sightings s
  ON s.repo_full_name = r.repo_full_name
 AND s.seen_at > r.range_start_at
 AND s.seen_at <= r.range_end_at
GROUP BY
    r.environment, s.repo_full_name, s.team, s.commit_sha, s.authored_at,
    s.committed_at, s.author, s.author_display_name,
    s.author_is_service_account, s.is_merge
HAVING min(r.deployed_at) > s.authored_at;
