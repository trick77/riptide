-- The PRs open at an instant T, keyed (repo_full_name, pr_id): the period's
-- end for the current period and for the previous one alike. Unlike prs.sql
-- no window: a PR opened months ago and still open is the point.
--
-- A PR riptide heard of before T is open at T when no pr:merged or
-- pr:deleted came before T, the latest event before T is no pr:declined,
-- and the latest event before T carrying a state has it OPEN (none does:
-- open unless declined). Every PR event carries the PR with its state, so a
-- declined PR reopened is open again on its next event, whichever event
-- Bitbucket sends for the reopening.
-- A PR riptide heard of only after T (idle since before ingestion) is open
-- at T when it was created before T and its first event after T closes it
-- or carries it open: else the previous end would miss a PR open all along,
-- and it would read as a new long-open one now.
-- What it waits on comes from the events before T only.
--
-- Last event: the latest before T of any kind, or the creation when later:
-- riptide cannot tell a PR whose close event was lost from one nobody
-- touches, Go counts both as inactive after a while. A PR heard of only
-- after T has T: its later event shows it alive, and its creation would
-- make it inactive then and active now, a jam all along read as new.
--
-- Opened: the PR's createdDate, which every PR event carries, so a PR
-- opened before ingestion started has its age too; else its pr:opened.
-- GitOps repos left out (stage promotions, docs/metrics.md). `bot` and the
-- touches as in prs.sql: Go adds riptide's automation detection and drops
-- the opener's own and pre-ready touches.
--
-- One pass over the team's PR events, their scalars only; a payload is read
-- for one event per open PR alone.
--
-- $1 team, $2 the instant T, $3 the GitOps repos (sql/gitops.sql).
WITH non_human_identities AS (
  -- Accounts senders declared as non-human (docs/metrics.md, pickup-time query).
/*{{non_human_identities}}*/
),
pr_events AS MATERIALIZED (
  SELECT e.id, e.repo_full_name, e.pr_id, e.event_type, e.occurred_at, e.author, e.author_display_name,
         e.is_automated, e.change_type, e.jira_keys,
         e.occurred_at < $2 AS before,
         upper(e.payload->'pullRequest'->>'state') AS state,
         coalesce(e.payload->'pullRequest'->>'draft', 'false') = 'true' AS draft,
         e.payload->'pullRequest'->>'createdDate' AS created_ms,
         EXISTS (SELECT 1 FROM non_human_identities n
                  WHERE n.handle IN (lower(e.author), lower(e.author_display_name))) AS declared_non_human
  FROM bitbucket_events e
  WHERE e.team = $1
    AND e.event_type LIKE 'pr:%'
    AND e.pr_id IS NOT NULL AND e.repo_full_name IS NOT NULL
    AND e.repo_full_name <> ALL($3::text[])
),
status AS (
  SELECT repo_full_name, pr_id,
         bool_or(before) AS seen,
         max(occurred_at) FILTER (WHERE before) AS latest_at,
         (array_agg(id ORDER BY occurred_at DESC, id DESC) FILTER (WHERE before))[1] AS latest_id,
         (array_agg(event_type ORDER BY occurred_at DESC, id DESC) FILTER (WHERE before))[1] AS latest_type,
         (array_agg(state ORDER BY occurred_at DESC, id DESC) FILTER (WHERE before AND state IS NOT NULL))[1]
             AS latest_state,
         coalesce(bool_or(event_type IN ('pr:merged', 'pr:deleted')) FILTER (WHERE before), false) AS closed,
         coalesce(bool_or(event_type = 'pr:declined') FILTER (WHERE before), false) AS declined,
         -- After T too: a PR deleted since has no page to link to.
         bool_or(event_type = 'pr:deleted') AS deleted_ever,
         (array_agg(id ORDER BY occurred_at, id) FILTER (WHERE NOT before))[1] AS next_id,
         (array_agg(event_type ORDER BY occurred_at, id) FILTER (WHERE NOT before))[1] AS next_type,
         (array_agg(state ORDER BY occurred_at, id) FILTER (WHERE NOT before))[1] AS next_state,
         max(CASE WHEN created_ms ~ '^\d{1,15}$' THEN to_timestamp(created_ms::bigint / 1000.0) END) AS created_at
  FROM pr_events
  GROUP BY repo_full_name, pr_id
),
open_keys AS (
  SELECT s.repo_full_name, s.pr_id, s.seen, s.created_at, s.latest_at, s.deleted_ever,
         coalesce(s.latest_id, s.next_id) AS payload_id
  FROM status s
  WHERE CASE WHEN s.seen
             THEN NOT s.closed AND s.latest_type <> 'pr:declined'
                  AND coalesce(s.latest_state, CASE WHEN s.declined THEN 'DECLINED' ELSE 'OPEN' END) = 'OPEN'
             ELSE s.created_at < $2
                  AND (s.next_type IN ('pr:merged', 'pr:declined', 'pr:deleted') OR coalesce(s.next_state, 'OPEN') = 'OPEN')
        END
),
details AS (
  SELECT o.repo_full_name, o.pr_id,
         min(e.occurred_at) FILTER (WHERE e.before AND e.event_type = 'pr:opened') AS opened_event_at,
         -- Pickup clock start as in prs.sql.
         coalesce(min(e.occurred_at) FILTER (WHERE e.before AND e.event_type = 'pr:ready_for_review'),
                  min(e.occurred_at) FILTER (WHERE e.before AND e.event_type = 'pr:opened' AND NOT e.draft))
             AS ready_event_at,
         max(e.author) FILTER (WHERE e.event_type = 'pr:opened') AS opened_author,
         max(e.author_display_name) FILTER (WHERE e.event_type = 'pr:opened') AS opened_display_name,
         -- Ingest-time automation as prs.sql reads it, on pr:opened; a PR
         -- opened before ingestion has none, so then on what its side sends
         -- (pushes, edits), never the reviewers'.
         coalesce(bool_or(e.is_automated) FILTER (WHERE e.event_type = 'pr:opened'),
                  bool_or(e.is_automated) FILTER (WHERE e.event_type IN
                      ('pr:modified', 'pr:ready_for_review', 'pr:back_to_draft', 'pr:from_ref_updated', 'pr:merged')),
                  false) AS automated,
         -- change_type means something on PR events only, and every PR
         -- event stores the source branch.
         coalesce(max(e.change_type) FILTER (WHERE e.event_type = 'pr:opened'),
                  (array_agg(e.change_type ORDER BY e.occurred_at DESC, e.id DESC)
                      FILTER (WHERE e.change_type IS NOT NULL))[1]) AS change_type,
         -- Keys hold no space: joined, an aggregate of one row per PR.
         max(array_to_string(e.jira_keys, ' ')) FILTER (WHERE e.event_type = 'pr:opened') AS opened_jira_keys,
         -- Reviewer touches before T, bots and declared non-humans out,
         -- oldest first.
         coalesce(/*{{touches}}*/
                  FILTER (WHERE e.before
                            AND e.event_type IN (/*{{touch_events}}*/)
                            AND NOT e.is_automated AND NOT coalesce(e.declared_non_human, false)),
                  '[]'::jsonb) AS touches
  FROM open_keys o
  JOIN pr_events e ON e.repo_full_name = o.repo_full_name AND e.pr_id = o.pr_id
  GROUP BY o.repo_full_name, o.pr_id
),
prs AS (
  SELECT o.repo_full_name, o.pr_id, o.seen, o.created_at, o.latest_at, o.deleted_ever, d.opened_event_at, d.ready_event_at, d.opened_author,
         d.opened_display_name, d.automated, d.change_type, d.touches, d.opened_jira_keys,
         l.payload->'pullRequest' AS pull, l.jira_keys AS latest_jira_keys,
         -- A PR not seen before T: what it looked like then is unknown.
         o.seen AND coalesce(l.payload->'pullRequest'->>'draft', 'false') = 'true' AS draft
  FROM open_keys o
  JOIN details d ON d.repo_full_name = o.repo_full_name AND d.pr_id = o.pr_id
  JOIN bitbucket_events l ON l.id = o.payload_id
)
SELECT p.repo_full_name AS repo,
       split_part(p.repo_full_name, '/', 1) AS project,
       p.pr_id,
       p.pull->>'title' AS title,
       -- The PR's page as Bitbucket links it; Go keeps only an http(s) URL.
       CASE WHEN NOT p.deleted_ever THEN p.pull->'links'->'self'->0->>'href' END AS url,
       p.automated
         OR EXISTS (SELECT 1 FROM non_human_identities n
                     WHERE n.handle IN (lower(coalesce(p.opened_author, p.pull->'author'->'user'->>'name')),
                                        lower(coalesce(p.opened_display_name, p.pull->'author'->'user'->>'displayName'))))
         AS bot,
       p.change_type,
       -- The keys as opened and as the PR reads at T: its title and
       -- description may have changed since.
       ARRAY(SELECT DISTINCT x
               FROM unnest(string_to_array(coalesce(p.opened_jira_keys, ''), ' ') || p.latest_jira_keys::text[]) x
              WHERE x <> ''
              ORDER BY x)::text[] AS jira_keys,
       coalesce(p.created_at, p.opened_event_at) AS opened_at,
       -- See "Last event" above.
       CASE WHEN p.seen THEN greatest(p.latest_at, coalesce(p.created_at, p.opened_event_at)) ELSE $2 END
           AS last_event_at,
       -- Not a draft at T and no ready event (opened before ingestion, or
       -- made ready without the flip the parser recognises): ready from
       -- its creation.
       CASE WHEN p.seen THEN coalesce(p.ready_event_at,
                                      CASE WHEN NOT p.draft THEN coalesce(p.created_at, p.opened_event_at) END)
       END AS ready_at,
       p.draft,
       p.touches,
       -- Reviewers and other participants with their status, as the latest
       -- event before T carries them: Bitbucket's own view, approvals reset
       -- by a push included, and a non-reviewer's approval too.
       CASE WHEN NOT p.seen THEN '[]'::jsonb ELSE
       (SELECT coalesce(jsonb_agg(jsonb_build_object(
                   'author', coalesce(r->'user'->>'name', ''),
                   'display_name', coalesce(r->'user'->>'displayName', ''),
                   'status', coalesce(upper(r->>'status'), ''))), '[]'::jsonb)
          FROM jsonb_array_elements(
                 CASE WHEN jsonb_typeof(p.pull->'reviewers') = 'array' THEN p.pull->'reviewers' ELSE '[]'::jsonb END ||
                 CASE WHEN jsonb_typeof(p.pull->'participants') = 'array' THEN p.pull->'participants' ELSE '[]'::jsonb END) r
         WHERE lower(r->'user'->>'name') IS DISTINCT FROM lower(coalesce(p.opened_author, p.pull->'author'->'user'->>'name'))
           AND NOT EXISTS (SELECT 1 FROM non_human_identities n
                            WHERE n.handle IN (lower(r->'user'->>'name'), lower(r->'user'->>'displayName'))))
       END AS reviewers,
       coalesce(p.opened_author, p.pull->'author'->'user'->>'name', '') AS opener,
       coalesce(p.opened_display_name, p.pull->'author'->'user'->>'displayName', '') AS opener_display_name
FROM prs p
WHERE coalesce(p.created_at, p.opened_event_at) IS NOT NULL
ORDER BY p.repo_full_name, p.pr_id
