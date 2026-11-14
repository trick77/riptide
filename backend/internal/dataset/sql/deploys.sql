-- One row per Argo CD sync of the team in the window that finished
-- (Succeeded or Failed), with its kind per the README's deployment-frequency
-- query:
--   release     a successful deploy whose images differ from the same App's
--               previous successful deploy
--   sync        a successful redeploy of the same images (config change,
--               manual re-sync)
--   first_seen  the App's first successful deploy in the data: nothing to
--               compare against, release or re-sync unknown
--   NULL        a failed sync: it deployed nothing
-- The previous deploy is looked up over the App's whole history, not the
-- window, so the window start does not turn every App's first deploy in it
-- into first_seen. Partitioned by team, App and environment: the README's
-- per-App lag after its environment filter, and another team's App of the
-- same name never decides this team's kinds. A release reconciles every App
-- sharing its GitOps revision: count releases as distinct revisions.
--
-- $1 team, $2 window start (inclusive), $3 window end (exclusive).
WITH window_apps AS (
  SELECT DISTINCT app_name, environment
  FROM argocd_events
  WHERE team = $1 AND occurred_at >= $2 AND occurred_at < $3
    AND operation_phase IN ('Succeeded', 'Failed')
),
history AS (
  SELECT a.id, a.images,
         lag(a.images) OVER w AS previous_images,
         row_number() OVER w = 1 AS first_seen
  FROM (
    SELECT e.id, e.team, e.app_name, e.environment, e.occurred_at,
           -- Sorted: the same images in another order are not a change.
           (SELECT array_agg(i ORDER BY i) FROM jsonb_array_elements_text(e.payload->'images') i) AS images
    FROM argocd_events e
    JOIN window_apps wa ON wa.app_name = e.app_name AND wa.environment IS NOT DISTINCT FROM e.environment
    WHERE e.team = $1 AND e.operation_phase = 'Succeeded' AND e.occurred_at < $3
  ) a
  WINDOW w AS (PARTITION BY a.team, a.app_name, a.environment ORDER BY a.occurred_at, a.id)
)
SELECT a.app_name AS app,
       -- The App's product is the project of the GitOps repo it deploys
       -- from: its revision is a commit there.
       (SELECT split_part(b.repo_full_name, '/', 1)
          FROM bitbucket_events b
         WHERE b.commit_sha = a.revision AND b.repo_full_name IS NOT NULL
         ORDER BY b.occurred_at
         LIMIT 1) AS project,
       a.environment,
       a.revision,
       a.occurred_at AS at,
       a.operation_phase AS phase,
       a.duration_seconds AS duration_s,
       coalesce((SELECT array_agg(i ORDER BY i) FROM jsonb_array_elements_text(a.payload->'images') i),
                '{}')::text[] AS images,
       CASE
         WHEN h.id IS NULL THEN NULL
         WHEN h.first_seen THEN 'first_seen'
         WHEN h.images IS DISTINCT FROM h.previous_images THEN 'release'
         ELSE 'sync'
       END AS kind,
       -- What a release brought: its images the previous deploy did not
       -- run. Empty for anything but a release: a first_seen has nothing to
       -- compare against.
       CASE
         WHEN h.id IS NOT NULL AND NOT h.first_seen THEN
           ARRAY(SELECT unnest(h.images) EXCEPT SELECT unnest(h.previous_images) ORDER BY 1)
         ELSE '{}'
       END::text[] AS new_images
FROM argocd_events a
LEFT JOIN history h ON h.id = a.id
WHERE a.team = $1 AND a.occurred_at >= $2 AND a.occurred_at < $3
  AND a.operation_phase IN ('Succeeded', 'Failed')
ORDER BY a.occurred_at, a.id
