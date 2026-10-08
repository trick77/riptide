-- noergler_pr_rollups: the newest pr_completed row per pr_key. A rollup is
-- cumulative over the PR's life, and a reopened PR emits again at its next
-- terminal outcome (declined, reopened, merged lands two rows). Summed over
-- noergler_events, the earlier row's spend counted twice. Plain view: the
-- raw rows stay append-only and nothing needs refreshing.
--
-- Newest by occurred_at (the sender's emit time), then by id for a tie.
-- Known gap: declined, reopened, declined again repeats the delivery id
-- (pr_completed#<pr_key>#declined), so the second rollup is deduped and the
-- spend between the two declines is missing here.
CREATE VIEW noergler_pr_rollups AS
SELECT DISTINCT ON (pr_key) *
  FROM noergler_events
 WHERE event_type = 'pr_completed'
 ORDER BY pr_key, occurred_at DESC, id DESC;
