-- One row per PR, keyed (repo_full_name, pr_id): PR ids are numbered per
-- repository. A PR belongs to the window when its pr:opened or pr:merged
-- falls in it; its timestamps then come from its whole history, so a PR
-- opened before the window keeps its opened_at.
--
-- docs/metrics.md's lead-time breakdown and pickup-time queries, per PR instead of
-- aggregated, and with their WHERE filters turned into flags: `gitops` (a
-- stage promotion, not work) and `bot`. The UI drops what it does not count.
-- `bot` here is the ingest-time flag and sender declarations; Go adds
-- riptide's automation detection on the opener's login and display name, as
-- for commits. The ingest flag of a person's PR from a bot's branch is the
-- branch's, never the person's (sql/people.sql): flagged, opened by one of
-- $6, the PR is no bot's, and its reviewers are judged by their accounts
-- alone. The merge commit's deploys are joined in Go.
--
-- $1 team, $2 window start (inclusive), $3 window end (exclusive), $4 the
-- GitOps repos (sql/gitops.sql), $5 dataset.HousekeepingWindow in seconds,
-- $6 the people (sql/people.sql).
WITH non_human_identities AS (
  -- Accounts senders declared as non-human (docs/metrics.md, pickup-time query):
  -- is_automated is decided at ingest, so rows written before an identity
  -- was declared are caught here.
/*{{non_human_identities}}*/
),
window_prs AS (
  SELECT DISTINCT repo_full_name, pr_id
  FROM bitbucket_events
  WHERE team = $1
    AND occurred_at >= $2 AND occurred_at < $3
    AND event_type IN ('pr:opened', 'pr:merged')
    AND pr_id IS NOT NULL AND repo_full_name IS NOT NULL
),
-- The window's PRs' events, the PR object taken out of each payload once:
-- every field below reads it, and the whole payload, stored out of line,
-- would be fetched and unpacked again for each.
pr_events AS MATERIALIZED (
  SELECT e.id, e.repo_full_name, e.pr_id, e.event_type, e.occurred_at, e.author, e.author_display_name,
         e.is_automated, e.change_type, e.payload->'pullRequest' AS pr
  FROM bitbucket_events e
  JOIN window_prs w ON w.repo_full_name = e.repo_full_name AND w.pr_id = e.pr_id
),
prs AS (
  SELECT e.repo_full_name, e.pr_id,
         -- pr:opened carries the PR as opened; a PR opened before ingestion
         -- started has only its pr:merged, which names the same PR author.
         coalesce(max(lower(e.pr->'fromRef'->>'displayId')) FILTER (WHERE e.event_type = 'pr:opened'),
                  max(lower(e.pr->'fromRef'->>'displayId')) FILTER (WHERE e.event_type = 'pr:merged'))
             AS source_branch,
         -- The same as the sender spelled it: a ticket key in it is upper
         -- case, and lowercased it is no key (parse.JiraKeys).
         coalesce(max(e.pr->'fromRef'->>'displayId') FILTER (WHERE e.event_type = 'pr:opened'),
                  max(e.pr->'fromRef'->>'displayId') FILTER (WHERE e.event_type = 'pr:merged'))
             AS source_ref,
         -- The opener and the ingest flag of a PR, one definition in every
         -- query (sql/fragments/pr_opener.sql, pr_flagged.sql): pr:opened's,
         -- else its author's other events' (every PR event but a reviewer's
         -- and a draft flip names the PR's author), so a PR opened before
         -- ingestion started has them too.
         /*{{pr_opener}}*/ AS pr_opener,
         /*{{pr_opener_display_name}}*/ AS pr_opener_display_name,
         -- The title as last seen: every PR event carries the PR, and the
         -- author may have edited it since opening.
         (array_agg(e.pr->>'title' ORDER BY e.occurred_at DESC, e.id DESC)
             FILTER (WHERE e.pr->>'title' IS NOT NULL))[1] AS title,
         -- The title as opened, what a tool opens across repos at once.
         min(e.pr->>'title') FILTER (WHERE e.event_type = 'pr:opened') AS opened_title,
         -- The PR's page as Bitbucket links it; Go keeps only an http(s) URL.
         (array_agg(e.pr->'links'->'self'->0->>'href' ORDER BY e.occurred_at DESC, e.id DESC)
             FILTER (WHERE e.pr->'links'->'self'->0->>'href' IS NOT NULL))[1] AS url,
         /*{{pr_flagged}}*/ AS automated,
         -- change_type means something only on these two (docs/metrics.md).
         coalesce(max(e.change_type) FILTER (WHERE e.event_type = 'pr:opened'),
                  max(e.change_type) FILTER (WHERE e.event_type = 'pr:merged')) AS change_type,
         min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:opened') AS opened_at,
         -- Pickup clock start (docs/metrics.md, pickup-time query): the draft->ready
         -- flip, else the opening of a PR not opened as a draft. A draft never
         -- made ready has none.
         coalesce(min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:ready_for_review'),
                  min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:opened'
                                               AND coalesce(e.pr->>'draft', 'false') = 'false'))
             AS ready_at,
         min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:merged') AS merged_at,
         max(e.occurred_at) FILTER (WHERE e.event_type = 'pr:declined') AS declined_at,
         max(e.occurred_at) FILTER (WHERE e.event_type = 'pr:deleted') AS deleted_at,
         max(e.occurred_at) FILTER (WHERE e.event_type IN ('pr:opened', 'pr:reopened')) AS last_opened_at,
         max(e.pr->'properties'->'mergeCommit'->>'id')
             FILTER (WHERE e.event_type = 'pr:merged') AS merge_commit,
         -- The branch it went into, as merged: a PR may be retargeted.
         coalesce(max(lower(e.pr->'toRef'->>'displayId')) FILTER (WHERE e.event_type = 'pr:merged'),
                  max(lower(e.pr->'toRef'->>'displayId')) FILTER (WHERE e.event_type = 'pr:opened'))
             AS target_branch
  FROM pr_events e
  GROUP BY e.repo_full_name, e.pr_id
),
-- Every merged PR's target branch in the window's repos, over their whole
-- history (the repo index, then their pr:merged events), and who opened it:
-- a bot's PR into a branch makes no umbrella of it, as Go tells a bot. A
-- person's from a bot's branch is the person's ($6).
targets AS MATERIALIZED (
  SELECT e.repo_full_name, e.pr_id,
         lower(e.payload->'pullRequest'->'toRef'->>'displayId') AS target,
         e.author, e.author_display_name,
         (e.is_automated AND NOT coalesce(lower(e.author) = ANY($6::text[]), false))
           OR EXISTS (SELECT 1 FROM non_human_identities n
                       WHERE n.handle IN (lower(e.author), lower(e.author_display_name))) AS bot
  FROM bitbucket_events e
  WHERE e.event_type = 'pr:merged'
    AND e.repo_full_name IN (SELECT repo_full_name FROM window_prs)
    AND e.payload->'pullRequest'->'toRef'->>'displayId' IS NOT NULL
),
target_counts AS (
  SELECT repo_full_name, target, count(DISTINCT pr_id) AS n
  FROM targets
  GROUP BY repo_full_name, target
),
-- The repo's trunks besides master and main: the branch most of its merged
-- PRs went into, over its whole history, so a window holding one PR into
-- an umbrella branch does not make that branch the trunk. Branches tied
-- for most are all trunk: nothing tells one of them apart, least of all
-- its name. View repo_trunks' rule (trace_positions.sql reads the view),
-- here over targets, read anyway: the view would read the events again,
-- measurably slower.
trunks AS (
  SELECT t.repo_full_name, t.target AS trunk
  FROM target_counts t
  WHERE t.n = (SELECT max(u.n) FROM target_counts u WHERE u.repo_full_name = t.repo_full_name)
),
-- Oldest commit pushed to the PR's branch; a rebase resets it (docs/metrics.md,
-- lead-time breakdown query). A branch name is reused (a long-lived
-- source branch, measured in a month: up to 138 pushes on one branch, up to
-- 12 PRs from one branch), so only the pushes of this PR's own life count:
-- after the latest earlier PR from the same repo and branch was closed, and
-- not after this PR became ready.
bounded AS (
  SELECT p.repo_full_name, p.pr_id, p.source_branch,
         -- ready, else merged: a PR opened before ingestion started has no
         -- ready_at, and must not reach into a later PR's pushes.
         coalesce(p.ready_at, p.merged_at) AS until_at,
         -- PR events store the source branch, lowercased, as branch_name.
         (SELECT max(c.occurred_at)
            FROM bitbucket_events c
           WHERE c.repo_full_name = p.repo_full_name
             AND c.branch_name = p.source_branch
             AND c.event_type IN ('pr:merged', 'pr:declined', 'pr:deleted')
             AND c.pr_id IS DISTINCT FROM p.pr_id
             -- earlier: opened before this PR (or, opened before ingestion
             -- started, closed before it)
             AND (c.occurred_at < coalesce(p.opened_at, p.ready_at, p.merged_at)
                  OR EXISTS (SELECT 1 FROM bitbucket_events o
                              WHERE o.repo_full_name = c.repo_full_name AND o.pr_id = c.pr_id
                                AND o.event_type = 'pr:opened'
                                AND o.occurred_at < coalesce(p.opened_at, p.ready_at, p.merged_at))))
           AS previous_closed_at
  FROM prs p
  WHERE p.source_branch IS NOT NULL
),
branch_pushes AS MATERIALIZED (
  -- Each push to a PR branch expanded once, however many PRs share the
  -- branch: its oldest commit, and who pushed it.
  SELECT e.id, e.repo_full_name, e.branch_name, e.occurred_at AS pushed_at,
         coalesce(e.author, '') AS author, coalesce(e.author_display_name, '') AS author_display_name,
         (SELECT min((c->>'authorTimestamp')::bigint)
            FROM jsonb_array_elements(e.payload->'commits') c) AS first_ms
  FROM bitbucket_events e
  JOIN (SELECT DISTINCT repo_full_name, source_branch FROM bounded) br
    ON e.repo_full_name = br.repo_full_name AND e.branch_name = br.source_branch
  WHERE e.event_type = 'repo:refs_changed'
),
first_commits AS (
  SELECT b.repo_full_name, b.pr_id, to_timestamp(min(bp.first_ms) / 1000.0) AS first_commit_at
  FROM bounded b
  JOIN branch_pushes bp ON bp.repo_full_name = b.repo_full_name AND bp.branch_name = b.source_branch
  WHERE bp.pushed_at > coalesce(b.previous_closed_at, '-infinity')
    AND bp.pushed_at <= coalesce(b.until_at, 'infinity')
  GROUP BY b.repo_full_name, b.pr_id
),
chain AS (
  SELECT p.*, fc.first_commit_at, pb.person_on_bot_branch,
         -- Merged into another branch than the trunk (a feature or umbrella
         -- branch). Merged work all the same: it was reviewed and merged. A
         -- target the payload does not name counts as the trunk.
         p.target_branch IS NOT NULL AND p.target_branch NOT IN ('master', 'main')
           AND NOT EXISTS (SELECT 1 FROM trunks t
                            WHERE t.repo_full_name = p.repo_full_name AND t.trunk = p.target_branch) AS off_trunk,
         -- Reviewer touches after the clock start, the opener, bots and
         -- declared non-humans excluded (docs/metrics.md, pickup-time query), oldest
         -- first. Go drops the authors riptide's automation detection
         -- recognises today and takes first_review_at and approved_at from
         -- what is left, so both count the same reviewers. On a person's PR
         -- from a bot's branch every event carries the branch's flag: a
         -- reviewer there is a bot by its account alone, a SERVICE one
         -- (which the branch's flag hid at ingest) here.
         (SELECT coalesce(/*{{touches}}*/, '[]'::jsonb)
            FROM bitbucket_events e
           WHERE e.repo_full_name = p.repo_full_name AND e.pr_id = p.pr_id
             AND e.event_type IN (/*{{touch_events}}*/)
             AND lower(e.author) IS DISTINCT FROM lower(p.pr_opener)
             AND (NOT e.is_automated
                  OR (pb.person_on_bot_branch AND upper(coalesce(e.payload->'actor'->>'type', '')) <> 'SERVICE'))
             AND NOT EXISTS (SELECT 1 FROM non_human_identities n
                              WHERE n.handle IN (lower(e.author), lower(e.author_display_name)))
             AND e.occurred_at >= p.ready_at) AS touches,
         -- A person's PR from a bot's branch: the pushes of its own life
         -- (bounded) and who pushed them, oldest first; NULL for any other
         -- PR. Go starts its coding time at the first commit a person pushed,
         -- else at its opening (dataset.ownFirstCommit): the bot's commits on
         -- the branch before are no work of theirs (owner, 2026-11). A pusher
         -- is a bot by its account: declared, a SERVICE one (the branch's
         -- flag hid it at ingest, its payload read for that alone), the rest
         -- in Go.
         CASE WHEN pb.person_on_bot_branch THEN
           (SELECT coalesce(jsonb_agg(jsonb_build_object(
                     'first', to_timestamp(bp.first_ms / 1000.0),
                     'author', bp.author, 'display_name', bp.author_display_name,
                     'bot', EXISTS (SELECT 1 FROM non_human_identities n
                                     WHERE n.handle IN (lower(bp.author), lower(bp.author_display_name)))
                            OR (SELECT upper(coalesce(x.payload->'actor'->>'type', '')) = 'SERVICE'
                                  FROM bitbucket_events x WHERE x.id = bp.id))
                   ORDER BY bp.pushed_at, bp.id), '[]'::jsonb)
              FROM bounded b
              JOIN branch_pushes bp ON bp.repo_full_name = b.repo_full_name AND bp.branch_name = b.source_branch
             WHERE b.repo_full_name = p.repo_full_name AND b.pr_id = p.pr_id
               AND bp.pushed_at > coalesce(b.previous_closed_at, '-infinity')
               AND bp.pushed_at <= coalesce(b.until_at, 'infinity'))
         END AS person_pushes
  FROM prs p
  LEFT JOIN first_commits fc
    ON fc.repo_full_name = p.repo_full_name AND fc.pr_id = p.pr_id
  -- Flagged at ingest, opened by a person ($6): its flag is the branch's.
  CROSS JOIN LATERAL (SELECT coalesce(p.automated AND lower(p.pr_opener) = ANY($6::text[]), false)
                             AS person_on_bot_branch) pb
)
SELECT c.repo_full_name AS repo,
       split_part(c.repo_full_name, '/', 1) AS project,
       c.pr_id,
       c.title,
       c.opened_title,
       -- none for a PR deleted since: its page is gone
       CASE WHEN c.deleted_at IS NULL THEN c.url END AS url,
       c.repo_full_name = ANY($4::text[]) AS gitops,
       (coalesce(c.automated, false) AND NOT c.person_on_bot_branch)
         OR EXISTS (SELECT 1 FROM non_human_identities n
                     WHERE n.handle IN (lower(c.pr_opener), lower(c.pr_opener_display_name))) AS bot,
       c.change_type,
       ARRAY(SELECT DISTINCT k
               FROM bitbucket_events e CROSS JOIN LATERAL unnest(e.jira_keys) k
              WHERE e.repo_full_name = c.repo_full_name AND e.pr_id = c.pr_id
                AND e.event_type IN ('pr:opened', 'pr:merged')
              ORDER BY k)::text[] AS jira_keys,
       CASE
         WHEN c.merged_at IS NOT NULL THEN 'merged'
         WHEN c.deleted_at IS NOT NULL THEN 'deleted'
         -- a declined PR reopened later is open again
         WHEN c.declined_at IS NOT NULL
              AND c.declined_at >= coalesce(c.last_opened_at, '-infinity') THEN 'declined'
         ELSE 'open'
       END AS state,
       -- when a declined or deleted PR closed, as its state says; NULL for
       -- an open or merged one
       CASE
         WHEN c.merged_at IS NOT NULL THEN NULL
         WHEN c.deleted_at IS NOT NULL THEN c.deleted_at
         WHEN c.declined_at IS NOT NULL
              AND c.declined_at >= coalesce(c.last_opened_at, '-infinity') THEN c.declined_at
       END AS closed_at,
       c.opened_at,
       c.ready_at,
       c.touches,
       c.merged_at,
       c.first_commit_at,
       c.person_pushes,
       -- lowercased like every SHA column riptide stores; the payload keeps
       -- the sender's casing
       lower(c.merge_commit) AS merge_commit,
       -- the opener, for Go's automation detection only; never sent
       coalesce(c.pr_opener, '') AS opener,
       coalesce(c.pr_opener_display_name, '') AS opener_display_name,
       -- The PR's size at merge: noergler's final rollup, the whole PR diff
       -- over the files it reviews (no lock files, no vendored bundles).
       -- Bitbucket sends no diff size. NULL for a PR noergler never
       -- reviewed, and for a rollup that is not the merge's.
       r.lines_added,
       r.lines_removed,
       r.files_changed,
       c.target_branch,
       coalesce(c.source_ref, '') AS source_ref,
       c.off_trunk,
       -- When its work reached the trunk, and which trunk branch (NULL: the
       -- payload names none), what Go places it on production by
       -- (dataset.trace): a PR into the trunk by its merge commit's first
       -- push to that branch (Bitbucket's merge pushes it in the same second;
       -- its merge when that push was not seen; a fast-forward merge's
       -- commit was pushed to the source branch first); a PR into another
       -- branch by the first PR with a merge commit merged from that branch
       -- into a trunk at or after it (its umbrella), by that one's merge
       -- commit alike, one level deep. NULL without a merge commit: Go places
       -- a PR an umbrella's merge marked merged by the umbrella's.
       u.trunk_at,
       u.trunk_branch,
       -- A PR that reached a trunk other than master or main (develop),
       -- itself or through its umbrella, reaches master or main onward, by
       -- the first PR merged from that trunk into one of them at or after it
       -- got there, alike: releases built from master hold it there. Behind
       -- a CASE: a join would cost every PR's read.
       CASE WHEN u.trunk_branch NOT IN ('master', 'main') THEN
         (SELECT coalesce((SELECT min(e.occurred_at) FILTER (WHERE e.branch_name = m.target)
                             FROM bitbucket_events e
                            WHERE e.commit_sha = lower(m.merge_commit) AND e.repo_full_name = c.repo_full_name
                              AND e.event_type = 'repo:refs_changed'), m.merged_at)
            FROM (SELECT x.occurred_at AS merged_at,
                         x.payload->'pullRequest'->'properties'->'mergeCommit'->>'id' AS merge_commit,
                         lower(x.payload->'pullRequest'->'toRef'->>'displayId') AS target
                    FROM bitbucket_events x
                   WHERE x.repo_full_name = c.repo_full_name AND x.branch_name = u.trunk_branch
                     AND x.event_type = 'pr:merged' AND x.pr_id <> c.pr_id
                     AND x.occurred_at >= CASE WHEN c.off_trunk THEN u.trunk_at ELSE c.merged_at END
                     AND x.payload->'pullRequest'->'properties'->'mergeCommit'->>'id' IS NOT NULL
                     AND lower(x.payload->'pullRequest'->'toRef'->>'displayId') IN ('master', 'main')
                   ORDER BY x.occurred_at, x.id
                   LIMIT 1) m)
       END AS onward_at,
       -- the same PR's target
       CASE WHEN u.trunk_branch NOT IN ('master', 'main') THEN
         (SELECT lower(x.payload->'pullRequest'->'toRef'->>'displayId')
            FROM bitbucket_events x
           WHERE x.repo_full_name = c.repo_full_name AND x.branch_name = u.trunk_branch
             AND x.event_type = 'pr:merged' AND x.pr_id <> c.pr_id
             AND x.occurred_at >= CASE WHEN c.off_trunk THEN u.trunk_at ELSE c.merged_at END
             AND x.payload->'pullRequest'->'properties'->'mergeCommit'->>'id' IS NOT NULL
             AND lower(x.payload->'pullRequest'->'toRef'->>'displayId') IN ('master', 'main')
           ORDER BY x.occurred_at, x.id
           LIMIT 1)
       END AS onward_branch,
       -- The openers of the other merged PRs of the repo into this one's
       -- source branch: Go makes it an umbrella when one is a person's. It
       -- carries their work, which they count; it is none of its own.
       (SELECT coalesce(jsonb_agg(jsonb_build_object('author', coalesce(t.author, ''),
                                                     'display_name', coalesce(t.author_display_name, ''), 'bot', t.bot)
                                  ORDER BY t.pr_id), '[]'::jsonb)
          FROM targets t
         WHERE t.repo_full_name = c.repo_full_name AND t.target = c.source_branch
           AND t.pr_id <> c.pr_id) AS umbrella_openers,
       -- A merged PR's title as opened (spaces folded, case aside) opened in
       -- the team's repos within $5 seconds of its opening, itself included:
       -- what Go counts housekeeping from (dataset.HousekeepingWindow), over
       -- all history, so a batch opened across a period's edge is one batch
       -- in both periods. A person's opening from a bot's branch is the
       -- person's ($6).
       CASE WHEN c.merged_at IS NOT NULL AND c.opened_at IS NOT NULL AND c.opened_title IS NOT NULL THEN
         (SELECT coalesce(jsonb_agg(jsonb_build_object('repo', o.repo_full_name, 'at', o.occurred_at,
                                                       'author', coalesce(o.author, ''),
                                                       'display_name', coalesce(o.author_display_name, ''),
                                                       'bot', (o.is_automated
                                                               AND NOT coalesce(lower(o.author) = ANY($6::text[]), false))
                                                              OR o.repo_full_name = ANY($4::text[])
                                                              OR EXISTS (SELECT 1 FROM non_human_identities n
                                                                          WHERE n.handle IN (lower(o.author), lower(o.author_display_name))))
                                    ORDER BY o.occurred_at, o.id), '[]'::jsonb)
            FROM bitbucket_events o
           WHERE o.team = $1 AND o.event_type = 'pr:opened'
             AND o.occurred_at >= c.opened_at - make_interval(secs => $5)
             AND o.occurred_at <= c.opened_at + make_interval(secs => $5)
             AND lower(regexp_replace(btrim(o.payload->'pullRequest'->>'title'), '\s+', ' ', 'g'))
                 = lower(regexp_replace(btrim(c.opened_title), '\s+', ' ', 'g')))
       ELSE '[]'::jsonb END AS siblings
FROM chain c
-- The pushes of a merge commit: found by the commit (an index probe), then
-- the target branch's taken (a FILTER, so the planner keeps the probe
-- rather than walking the branch's pushes).
LEFT JOIN LATERAL (
  SELECT coalesce((SELECT min(e.occurred_at) FILTER (WHERE e.branch_name = c.target_branch)
                     FROM bitbucket_events e
                    WHERE e.commit_sha = lower(c.merge_commit) AND e.repo_full_name = c.repo_full_name
                      AND e.event_type = 'repo:refs_changed'),
                  c.merged_at) AS trunk_at,
         c.target_branch AS trunk_branch
  WHERE c.merged_at IS NOT NULL AND c.merge_commit IS NOT NULL AND NOT c.off_trunk
  UNION ALL
  SELECT coalesce((SELECT min(e.occurred_at) FILTER (WHERE e.branch_name = m.target)
                     FROM bitbucket_events e
                    WHERE e.commit_sha = lower(m.merge_commit) AND e.repo_full_name = c.repo_full_name
                      AND e.event_type = 'repo:refs_changed'), m.merged_at),
         m.target
  FROM (SELECT x.occurred_at AS merged_at,
               x.payload->'pullRequest'->'properties'->'mergeCommit'->>'id' AS merge_commit,
               lower(x.payload->'pullRequest'->'toRef'->>'displayId') AS target
          FROM bitbucket_events x
         -- PR events store the source branch, lowercased, as branch_name.
         WHERE x.repo_full_name = c.repo_full_name AND x.branch_name = c.target_branch
           AND x.event_type = 'pr:merged' AND x.pr_id <> c.pr_id AND x.occurred_at >= c.merged_at
           -- the first into a trunk: one into another branch first moves
           -- the work no closer
           AND x.payload->'pullRequest'->'properties'->'mergeCommit'->>'id' IS NOT NULL
           AND (lower(x.payload->'pullRequest'->'toRef'->>'displayId') IN ('master', 'main')
                OR EXISTS (SELECT 1 FROM trunks t
                            WHERE t.repo_full_name = c.repo_full_name
                              AND t.trunk = lower(x.payload->'pullRequest'->'toRef'->>'displayId')))
         ORDER BY x.occurred_at, x.id
         LIMIT 1) m
  WHERE c.merged_at IS NOT NULL AND c.off_trunk
) u ON true
-- pr_key is '<repo_full_name>#<pr_id>', lowercased at ingest.
LEFT JOIN noergler_pr_rollups r
  ON r.pr_key = lower(c.repo_full_name) || '#' || c.pr_id AND r.outcome = 'merged'
ORDER BY c.repo_full_name, c.pr_id
