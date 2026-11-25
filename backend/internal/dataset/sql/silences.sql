-- Gaps between one source's consecutive events for the team, 48 hours or
-- longer, while another source stored an event of the team inside the gap:
-- the candidates for a silence. Go keeps those with two working days in
-- them (dataset.SilenceMin). Before the source's first event it was not
-- reporting yet (meta's data start): no gap. After its last one, the gap
-- runs to the read's end ($3, now): `ongoing`, a source still silent while
-- the others report. An outage running at Monday's scheduled read is a
-- hole then, not only once its closing event comes.
--
-- Every instant here is when riptide stored the event (created_at), not when
-- the event says it happened: a sender's retry storing an old event days
-- late, dated inside a hole, must neither split nor fill it.
--
-- unseen_runs, for Bitbucket only: the CI runs inside the gap that built a
-- commit Bitbucket never saw, of a pipeline whose other commits it did see.
-- A run pushed while Bitbucket was quiet is evidence its events were lost;
-- a team on holiday whose nightly job keeps running has none. Bitbucket
-- sees a commit as a push's tip, a PR's source head, or the merge commit of
-- a PR merged in the UI (which sends no push; the payload keeps the
-- sender's casing, ix_bitbucket_events_merge_commit holds it lowercased).
--
-- $1 team, $2 look-back start (inclusive), $3 end (exclusive): the read's
-- now.
WITH events AS (
  SELECT 'bitbucket' AS source, created_at AS at, false AS ending FROM bitbucket_events
   WHERE team = $1 AND created_at >= $2 AND created_at < $3
  UNION ALL
  SELECT 'bitbucket', max(created_at), false FROM bitbucket_events WHERE team = $1 AND created_at < $2
  UNION ALL
  SELECT 'pipeline', created_at, false FROM pipeline_events
   WHERE team = $1 AND created_at >= $2 AND created_at < $3
  UNION ALL
  SELECT 'pipeline', max(created_at), false FROM pipeline_events WHERE team = $1 AND created_at < $2
  UNION ALL
  SELECT 'argocd', created_at, false FROM argocd_events
   WHERE team = $1 AND created_at >= $2 AND created_at < $3
  UNION ALL
  SELECT 'argocd', max(created_at), false FROM argocd_events WHERE team = $1 AND created_at < $2
  UNION ALL
  SELECT 'noergler', created_at, false FROM noergler_events
   WHERE team = $1 AND created_at >= $2 AND created_at < $3
  UNION ALL
  SELECT 'noergler', max(created_at), false FROM noergler_events WHERE team = $1 AND created_at < $2
  UNION ALL
  -- The read's end: each source's last gap closes there.
  SELECT s, $3::timestamptz, true FROM unnest(ARRAY['bitbucket', 'pipeline', 'argocd', 'noergler']) s
),
gaps AS (
  SELECT source, lag(at) OVER (PARTITION BY source ORDER BY at, ending) AS from_at, at AS to_at, ending AS ongoing
  FROM events
  WHERE at IS NOT NULL
)
SELECT g.source, g.from_at, g.to_at, g.ongoing,
       CASE WHEN g.source = 'bitbucket' THEN
         (SELECT count(*)
            FROM pipeline_events p
           WHERE p.team = $1 AND p.phase = 'COMPLETED' AND p.commit_sha IS NOT NULL
             AND p.created_at > g.from_at AND p.created_at < g.to_at
             AND NOT EXISTS (SELECT 1 FROM bitbucket_events b WHERE b.commit_sha IN (p.commit_sha, p.head_sha))
             AND NOT EXISTS (SELECT 1 FROM bitbucket_events b
                              WHERE b.event_type = 'pr:merged'
                                AND lower(b.payload->'pullRequest'->'properties'->'mergeCommit'->>'id') = p.commit_sha)
             AND EXISTS (SELECT 1
                           FROM pipeline_events q
                          WHERE q.team = p.team AND q.source = p.source AND q.pipeline_name = p.pipeline_name
                            AND (EXISTS (SELECT 1 FROM bitbucket_events b WHERE b.commit_sha = q.commit_sha)
                              OR EXISTS (SELECT 1 FROM bitbucket_events b
                                          WHERE b.event_type = 'pr:merged'
                                            AND lower(b.payload->'pullRequest'->'properties'->'mergeCommit'->>'id') = q.commit_sha))))
       ELSE 0 END AS unseen_runs
FROM gaps g
WHERE g.from_at IS NOT NULL AND g.to_at - g.from_at >= interval '48 hours'
  AND ((g.source <> 'bitbucket' AND EXISTS (SELECT 1 FROM bitbucket_events x
                                             WHERE x.team = $1 AND x.created_at > g.from_at AND x.created_at < g.to_at))
    OR (g.source <> 'pipeline' AND EXISTS (SELECT 1 FROM pipeline_events x
                                            WHERE x.team = $1 AND x.created_at > g.from_at AND x.created_at < g.to_at))
    OR (g.source <> 'argocd' AND EXISTS (SELECT 1 FROM argocd_events x
                                          WHERE x.team = $1 AND x.created_at > g.from_at AND x.created_at < g.to_at))
    OR (g.source <> 'noergler' AND EXISTS (SELECT 1 FROM noergler_events x
                                            WHERE x.team = $1 AND x.created_at > g.from_at AND x.created_at < g.to_at)))
ORDER BY g.from_at, g.source
