-- The PRs people opened from a bot's branch, over all history, and the span
-- of their branch's builds each one owns. The ingest flags every event on a
-- branch an automation source's branch_prefixes names, whoever authored it
-- (config.DetectAutomationSource). Read here, who opened the PR decides
-- (owner, 2026-11): a person's PR from such a branch is the person's work,
-- its reviewers are judged by their accounts alone, its builds are the
-- team's. A PR the bot opened stays the bot's, whoever pushed to it. Who is
-- a person: sql/people.sql, the rule's one place.
--
-- A branch name is reused: the bot's next PR comes from it again. A PR owns
-- the builds of its branch after the latest earlier PR from the same repo
-- and branch closed (prs.sql's bound on its pushes), so before its opening
-- too, and up to its own close; an open one's up to now.
--
-- $1 team, $2 the people (sql/people.sql).
WITH
-- The team's PRs the ingest flagged, as prs.sql reads the flag, a person
-- opened: on pr:opened or pr:merged, which names the PR's author too (a PR
-- opened before ingestion started has only its merge). PR events store the
-- source branch, lowercased, as branch_name.
flagged AS (
  SELECT e.repo_full_name, e.pr_id,
         coalesce(max(e.branch_name) FILTER (WHERE e.event_type = 'pr:opened'), max(e.branch_name)) AS branch,
         coalesce(max(e.author) FILTER (WHERE e.event_type = 'pr:opened'), max(e.author)) AS author,
         coalesce(max(e.author_display_name) FILTER (WHERE e.event_type = 'pr:opened'), max(e.author_display_name))
           AS display_name,
         -- opened, else merged
         min(e.occurred_at) AS opened_at
  FROM bitbucket_events e
  WHERE e.team = $1 AND e.event_type IN ('pr:opened', 'pr:merged')
    AND e.repo_full_name IS NOT NULL AND e.pr_id IS NOT NULL AND e.branch_name IS NOT NULL
  GROUP BY e.repo_full_name, e.pr_id
  HAVING bool_or(e.is_automated)
     AND lower(coalesce(max(e.author) FILTER (WHERE e.event_type = 'pr:opened'), max(e.author))) = ANY($2::text[])
)
SELECT t.repo_full_name AS repo,
       t.branch,
       t.pr_id,
       coalesce(t.author, '') AS author,
       coalesce(t.display_name, '') AS display_name,
       -- the latest close of an earlier PR from the branch: opened before
       -- this one (or, opened before ingestion started, closed before it)
       (SELECT max(c.occurred_at)
          FROM bitbucket_events c
         WHERE c.repo_full_name = t.repo_full_name
           AND c.branch_name = t.branch
           AND c.event_type IN ('pr:merged', 'pr:declined', 'pr:deleted')
           AND c.pr_id IS DISTINCT FROM t.pr_id
           AND (c.occurred_at < t.opened_at
                OR EXISTS (SELECT 1 FROM bitbucket_events o
                            WHERE o.repo_full_name = c.repo_full_name AND o.pr_id = c.pr_id
                              AND o.event_type = 'pr:opened' AND o.occurred_at < t.opened_at)))
         AS from_at,
       -- its own close, as prs.sql's state: merged, deleted, or declined and
       -- not reopened since; NULL while open
       (SELECT CASE
                 WHEN bool_or(x.event_type = 'pr:merged') THEN min(x.occurred_at) FILTER (WHERE x.event_type = 'pr:merged')
                 WHEN bool_or(x.event_type = 'pr:deleted') THEN max(x.occurred_at) FILTER (WHERE x.event_type = 'pr:deleted')
                 WHEN max(x.occurred_at) FILTER (WHERE x.event_type = 'pr:declined')
                      >= coalesce(max(x.occurred_at) FILTER (WHERE x.event_type IN ('pr:opened', 'pr:reopened')), '-infinity')
                   THEN max(x.occurred_at) FILTER (WHERE x.event_type = 'pr:declined')
               END
          FROM bitbucket_events x
         WHERE x.repo_full_name = t.repo_full_name AND x.pr_id = t.pr_id
           AND x.event_type IN ('pr:merged', 'pr:deleted', 'pr:declined', 'pr:opened', 'pr:reopened'))
         AS until_at
FROM flagged t
ORDER BY t.repo_full_name, t.pr_id
