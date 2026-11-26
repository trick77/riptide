-- repo_trunks: a repo's trunk besides master and main, the branch most of
-- its merged PRs went into over all history (several when tied: nothing
-- tells one of them apart, least of all its name). A repo whose PRs go to
-- master has master here too. A trunk is master, main, or one of these:
-- commit_sightings and the tracing's positions read the view; the PR read
-- applies the same rule to the merged PRs it reads anyway. Derived on read,
-- nothing stored: a repo whose PRs move to another branch moves its trunk
-- with them.
CREATE VIEW repo_trunks AS
SELECT repo_full_name, target AS trunk
FROM (
    SELECT repo_full_name,
           lower(payload->'pullRequest'->'toRef'->>'displayId') AS target,
           -- Partitioned by the repo: a filter on repo_full_name reaches the
           -- pr:merged events below the window, one repo's at a time.
           rank() OVER (PARTITION BY repo_full_name ORDER BY count(DISTINCT pr_id) DESC) AS place
    FROM bitbucket_events
    WHERE event_type = 'pr:merged' AND repo_full_name IS NOT NULL
      AND payload->'pullRequest'->'toRef'->>'displayId' IS NOT NULL
    GROUP BY repo_full_name, lower(payload->'pullRequest'->'toRef'->>'displayId')
) t
WHERE place = 1;

-- commit_sightings, the same columns as in 0001: one row per commit of a
-- push to the repo's trunk (master, main, or repo_trunks'), no longer
-- master's alone. A payload whose commits is no array (a sender's odd shape)
-- adds no row instead of failing every read of the view.
CREATE OR REPLACE VIEW commit_sightings AS
SELECT
    b.repo_full_name,
    b.team,
    c ->> 'id'                                                AS commit_sha,
    to_timestamp((c ->> 'authorTimestamp')::bigint / 1000)    AS authored_at,
    to_timestamp((c ->> 'committerTimestamp')::bigint / 1000) AS committed_at,
    b.occurred_at                                             AS seen_at,
    c -> 'author' ->> 'name'                                  AS author,
    c -> 'author' ->> 'displayName'                           AS author_display_name,
    upper(coalesce(c -> 'author' ->> 'type', 'NORMAL')) = 'SERVICE'
                                                              AS author_is_service_account,
    coalesce(jsonb_array_length(c -> 'parents'), 0) > 1        AS is_merge,
    c ->> 'message'                                           AS message
FROM bitbucket_events b
CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(b.payload -> 'commits') = 'array' THEN b.payload -> 'commits' END) AS c
WHERE b.event_type = 'repo:refs_changed'
  AND (b.branch_name IN ('master', 'main')
       OR EXISTS (SELECT 1 FROM repo_trunks t
                   WHERE t.repo_full_name = b.repo_full_name AND t.trunk = b.branch_name));
