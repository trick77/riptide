-- noergler_pr_rollups: the newest pr_completed row per pr_key. A rollup is
-- cumulative over the PR's life, and a declined PR that is reopened and
-- merged emits again (two rows: declined, then merged). Summed over
-- noergler_events, the earlier row's spend counted twice. Plain view: the
-- raw rows stay append-only and nothing needs refreshing.
--
-- Newest by occurred_at (the sender's emit time), then by id for a tie, so a
-- delayed older rollup cannot win by arriving last. A deleted rollup ranks
-- below any other: a declined PR deleted later (older senders emitted both)
-- carries 0 lines and 0 files, and would wipe the declined row's diff size.
-- Per pr_key, not per team: the newest rollup already carries the PR's whole
-- spend. It also moves a reopened PR's whole spend into its merge week.
-- Known gap: a PR declined (or deleted) again after a reopen sends no second
-- rollup, so the spend after the first decline is missing here.
--
-- Columns are listed, not *: a column added to noergler_events later is a
-- deliberate CREATE OR REPLACE here, not a silent omission.
CREATE INDEX ix_noergler_events_pr_completed_newest
    ON noergler_events (pr_key, (outcome = 'deleted'), occurred_at DESC, id DESC)
    WHERE event_type = 'pr_completed';

CREATE VIEW noergler_pr_rollups AS
SELECT DISTINCT ON (pr_key)
       id, delivery_id, event_type, pr_key, repo, commit_sha,
       prompt_tokens, completion_tokens, elapsed_ms, findings_count, cost_usd,
       occurred_at, created_at, team, payload, outcome, merge_commit_sha,
       lines_added, lines_removed, files_changed, total_runs, models_used,
       first_review_at, reviewer_handle, reviewer_account_kind
  FROM noergler_events
 WHERE event_type = 'pr_completed'
 ORDER BY pr_key, (outcome = 'deleted'), occurred_at DESC, id DESC;
