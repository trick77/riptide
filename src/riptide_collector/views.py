"""Read-time views for metrics that need more than a single table scan.

The definitions live here rather than inline in the migration so that the
migration and the tests execute the same SQL — a view is a metric definition,
and a definition nobody can test drifts.

Layered on purpose:

- `commit_sightings` expands `payload->'commits'` from master pushes into one
  row per commit, with the timestamps and the classification the metrics filter
  on (merge commit, service account, author).
- `deploy_commit_ranges` says which commit range a deploy shipped. This is the
  fragile one: today it reads the release-note bump links out of the GitOps
  commit. When senders report `pipeline_events.image_ref` it becomes "between
  the previous and the current successful deploy of this app" and nothing else
  changes.
- `lead_time_changes` maps every commit to the FIRST successful deploy per
  environment whose range contained it, so a change rolled out to four Apps of
  one service counts once.

Plain views, not materialized: nothing to refresh, so the no-rollup-jobs
invariant holds.
"""

# Master pushes only: a change enters the release stream when it lands on the
# integration branch, and feature-branch pushes would count it twice. Bitbucket
# caps `commits[]` at 5 per push — measured, that bites 0.6 % of master pushes,
# and it drops changes rather than mis-timing the ones it keeps.
COMMIT_SIGHTINGS = """
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
  AND b.branch_name = 'master'
"""

# The release commit in the GitOps repo lists every component it bumped as a
# compare link carrying the previous and the new App-repo SHA; those two bound
# the range the deploy shipped. The LIKE prefilter keeps the regex off every
# push payload in the table.
#
# Two assumptions, both true of the release generators this was built against
# and both stated in docs/dora-lead-time.md:
#
# - The GitOps release commit is a push *tip*, since `commit_sha` stores the
#   push's toHash. A release split across two pushes where Argo only ever
#   reports the second revision leaves the first push's bumps unresolvable.
# - Both boundary SHAs are push tips in the App repo, which is what makes the
#   push-timestamp range below exact. Where a boundary is not the tip, the
#   commits that share its push are attributed to the wrong side.
#
# Both disappear once senders report `pipeline_events.image_ref`: the range
# then comes from consecutive deploys of the same app, with no message parsing.
DEPLOY_COMMIT_RANGES = r"""
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
        -- the repo join below compares the slug exactly rather than with LIKE,
        -- where '_' would act as a wildcard.
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
 AND curr.repo_full_name = prev.repo_full_name
"""

# First deploy only: a change reaches production once, even though the release
# rolls out to every App of the service.
#
# The range is matched by push time, so every commit of a push belongs to one
# side of the boundary. That is exact while the boundary SHA is the push tip
# (see above) and it is why a boundary commit riptide never saw drops the whole
# release window rather than one commit — `deploy_commit_ranges` needs both
# ends. Those windows are absent from the metric, never counted as fast.
LEAD_TIME_CHANGES = """
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
HAVING min(r.deployed_at) > s.authored_at
"""

# Dependency order: each view builds on the one before it.
CREATE_VIEWS = (COMMIT_SIGHTINGS, DEPLOY_COMMIT_RANGES, LEAD_TIME_CHANGES)
DROP_VIEWS = (
    "DROP VIEW IF EXISTS lead_time_changes",
    "DROP VIEW IF EXISTS deploy_commit_ranges",
    "DROP VIEW IF EXISTS commit_sightings",
)
