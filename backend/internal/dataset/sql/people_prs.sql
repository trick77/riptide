-- The PRs people opened from a bot's branch, over all history, and the span
-- of their branch's builds each one owns. The ingest flags every event on a
-- branch an automation source's branch_prefixes names, whoever authored it
-- (config.DetectAutomationSource). Read here, who opened the PR decides
-- (owner, 2026-11): a person's PR from such a branch is the person's work,
-- its reviewers are judged by their accounts alone, its builds are the
-- team's. A PR the bot opened stays the bot's, whoever pushed to it. Who is
-- a person: sql/people.sql, the rule's one place. The flag and the opener
-- as every PR query reads them (sql/fragments/pr_flagged.sql,
-- pr_opener.sql), so an open PR without a stored pr:opened is the same
-- person's here as among the open PRs.
--
-- A branch name is reused: the bot's next PR comes from it again. A PR owns
-- the builds of its branch from its opening back to the close of the latest
-- PR from the same repo and branch seen before it (one still open then
-- holds the branch up to this opening), so before its opening too, and up
-- to its own close or the next PR's opening from the branch, whichever
-- comes first; one never seen closed and never followed, up to now.
--
-- $1 team, $2 the people (sql/people.sql).
WITH
-- Every PR of the team: its branch (PR events store the source branch,
-- lowercased, as branch_name), its opening (pr:opened, else its first event
-- riptide saw), its close as prs.sql's state reads it (merged, deleted, or
-- declined and not reopened since; NULL while open), its flag and opener.
lives AS MATERIALIZED (
  SELECT e.repo_full_name, e.pr_id,
         max(e.branch_name) AS branch,
         min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:opened') AS opened_event_at,
         coalesce(min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:opened'), min(e.occurred_at)) AS opened_at,
         CASE
           WHEN bool_or(e.event_type = 'pr:merged') THEN min(e.occurred_at) FILTER (WHERE e.event_type = 'pr:merged')
           WHEN bool_or(e.event_type = 'pr:deleted') THEN max(e.occurred_at) FILTER (WHERE e.event_type = 'pr:deleted')
           WHEN max(e.occurred_at) FILTER (WHERE e.event_type = 'pr:declined')
                >= coalesce(max(e.occurred_at) FILTER (WHERE e.event_type IN ('pr:opened', 'pr:reopened')), '-infinity')
             THEN max(e.occurred_at) FILTER (WHERE e.event_type = 'pr:declined')
         END AS closed_at,
         /*{{pr_flagged}}*/ AS flagged,
         /*{{pr_opener}}*/ AS author,
         /*{{pr_opener_display_name}}*/ AS display_name
  FROM bitbucket_events e
  WHERE e.team = $1 AND e.event_type LIKE 'pr:%'
    AND e.repo_full_name IS NOT NULL AND e.pr_id IS NOT NULL AND e.branch_name IS NOT NULL
  GROUP BY e.repo_full_name, e.pr_id
)
SELECT t.repo_full_name AS repo,
       t.branch,
       t.pr_id,
       coalesce(t.author, '') AS author,
       coalesce(t.display_name, '') AS display_name,
       (SELECT max(least(coalesce(o.closed_at, 'infinity'), t.opened_at))
          FROM lives o
         WHERE o.repo_full_name = t.repo_full_name AND o.branch = t.branch AND o.pr_id <> t.pr_id
           AND o.opened_at < t.opened_at) AS from_at,
       least(t.closed_at,
             (SELECT min(o.opened_event_at)
                FROM lives o
               WHERE o.repo_full_name = t.repo_full_name AND o.branch = t.branch AND o.pr_id <> t.pr_id
                 AND o.opened_event_at > t.opened_at)) AS until_at
FROM lives t
WHERE t.flagged AND lower(t.author) = ANY($2::text[])
ORDER BY t.repo_full_name, t.pr_id
