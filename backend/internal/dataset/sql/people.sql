-- People: the accounts whose PR from a bot's branch is their own work. The
-- ingest flags every event on a branch an automation source's
-- branch_prefixes names, whoever authored it (config.DetectAutomationSource),
-- so a PR a person opens from such a branch reads as a bot's. Read time
-- decides by who opened it (owner, 2026-11): a person's PR from a bot's
-- branch is the person's (sql/people_prs.sql), a PR the bot opened stays
-- the bot's, whoever pushed to it.
--
-- A person, lowercased login: an account the team's history shows opening a
-- PR the ingest did not flag from an ordinary branch, one no configured
-- prefix names now ($2): an opening that predates its bot's prefix in the
-- config was not flagged and proves nothing; never declared
-- non-human by a sender (by login or display name), never tagged a service
-- account or bot-shaped at ingest. The history is the test because the
-- branch prefix is the fallback for a bot account nobody declared
-- (docs/change-notes.md): one that opens PRs from its own branches only
-- stays a bot. Go adds riptide's automation detection on the opener
-- (Params.Automated), as on every PR.
--
-- Read once per page and handed to prs, open_prs, approvals and people_prs
-- as an array: one definition, one scan of the team's openings.
--
-- $1 team, $2 the configured branch_prefixes, lowercased (PR events store
-- the source branch lowercased as branch_name).
WITH non_human_identities AS (
/*{{non_human_identities}}*/
)
SELECT coalesce(array_agg(handle ORDER BY handle), '{}')::text[] AS handles
FROM (
  SELECT lower(o.author) AS handle
  FROM bitbucket_events o
  WHERE o.team = $1 AND o.event_type = 'pr:opened' AND o.author IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM non_human_identities n
                     WHERE n.handle IN (lower(o.author), lower(o.author_display_name)))
  GROUP BY lower(o.author)
  HAVING bool_or(NOT o.is_automated
                 AND NOT EXISTS (SELECT 1 FROM unnest($2::text[]) p
                                  WHERE p <> '' AND starts_with(coalesce(o.branch_name, ''), p)))
     AND NOT bool_or(coalesce(o.automation_source IN ('service-account', 'other-bot'), false))
) p
