-- Gaps between one source's consecutive events for the team, 48 hours or
-- longer, while another source stored an event of the team inside the gap:
-- the candidates for a silence. Go keeps those with two working days in
-- them (dataset.SilenceMin). A gap needs an event on both sides: before the
-- source's first event it was not reporting yet (meta's data start), after
-- its last one the end-of-period gap is meta's data end.
--
-- unseen_runs, for Bitbucket only: the CI runs inside the gap that built a
-- commit Bitbucket never saw, of a pipeline whose other commits it did see.
-- A run pushed while Bitbucket was quiet is evidence its events were lost;
-- a team on holiday whose nightly job keeps running has none.
--
-- $1 team, $2 look-back start (inclusive), $3 end (exclusive).
WITH events AS (
  SELECT 'bitbucket' AS source, occurred_at FROM bitbucket_events
   WHERE team = $1 AND occurred_at >= $2 AND occurred_at < $3
  UNION ALL
  SELECT 'bitbucket', max(occurred_at) FROM bitbucket_events WHERE team = $1 AND occurred_at < $2
  UNION ALL
  SELECT 'pipeline', occurred_at FROM pipeline_events
   WHERE team = $1 AND occurred_at >= $2 AND occurred_at < $3
  UNION ALL
  SELECT 'pipeline', max(occurred_at) FROM pipeline_events WHERE team = $1 AND occurred_at < $2
  UNION ALL
  SELECT 'argocd', occurred_at FROM argocd_events
   WHERE team = $1 AND occurred_at >= $2 AND occurred_at < $3
  UNION ALL
  SELECT 'argocd', max(occurred_at) FROM argocd_events WHERE team = $1 AND occurred_at < $2
  UNION ALL
  SELECT 'noergler', occurred_at FROM noergler_events
   WHERE team = $1 AND occurred_at >= $2 AND occurred_at < $3
  UNION ALL
  SELECT 'noergler', max(occurred_at) FROM noergler_events WHERE team = $1 AND occurred_at < $2
),
gaps AS (
  SELECT source, lag(occurred_at) OVER (PARTITION BY source ORDER BY occurred_at) AS from_at, occurred_at AS to_at
  FROM events
  WHERE occurred_at IS NOT NULL
)
SELECT g.source, g.from_at, g.to_at,
       CASE WHEN g.source = 'bitbucket' THEN
         (SELECT count(*)
            FROM pipeline_events p
           WHERE p.team = $1 AND p.phase = 'COMPLETED' AND p.commit_sha IS NOT NULL
             AND p.occurred_at > g.from_at AND p.occurred_at < g.to_at
             AND NOT EXISTS (SELECT 1 FROM bitbucket_events b WHERE b.commit_sha IN (p.commit_sha, p.head_sha))
             AND EXISTS (SELECT 1
                           FROM pipeline_events q
                           JOIN bitbucket_events b ON b.commit_sha = q.commit_sha
                          WHERE q.team = p.team AND q.source = p.source AND q.pipeline_name = p.pipeline_name))
       ELSE 0 END AS unseen_runs
FROM gaps g
WHERE g.from_at IS NOT NULL AND g.to_at - g.from_at >= interval '48 hours'
  AND ((g.source <> 'bitbucket' AND EXISTS (SELECT 1 FROM bitbucket_events x
                                             WHERE x.team = $1 AND x.occurred_at > g.from_at AND x.occurred_at < g.to_at))
    OR (g.source <> 'pipeline' AND EXISTS (SELECT 1 FROM pipeline_events x
                                            WHERE x.team = $1 AND x.occurred_at > g.from_at AND x.occurred_at < g.to_at))
    OR (g.source <> 'argocd' AND EXISTS (SELECT 1 FROM argocd_events x
                                          WHERE x.team = $1 AND x.occurred_at > g.from_at AND x.occurred_at < g.to_at))
    OR (g.source <> 'noergler' AND EXISTS (SELECT 1 FROM noergler_events x
                                            WHERE x.team = $1 AND x.occurred_at > g.from_at AND x.occurred_at < g.to_at)))
ORDER BY g.from_at, g.source
