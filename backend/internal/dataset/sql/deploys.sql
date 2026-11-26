-- One row per Argo CD sync of the team in the window that finished
-- (Succeeded, or Failed / Error: the notification template sends both for a
-- sync that deployed nothing), with its kind per docs/metrics.md's
-- deployment-frequency query:
--   release     a successful deploy whose images differ from the same App's
--               previous successful deploy naming images
--   sync        a successful redeploy of the same images (config change,
--               manual re-sync), or one naming no images, even the App's first
--   first_seen  the App's first successful deploy naming images in the data:
--               nothing to compare against; Go counts it a release once
--               Argo CD has reported long enough (dataset.FirstDeployGrace)
--   NULL        a failed sync: it deployed nothing
-- The previous deploy is looked up over the App's whole history, not the
-- window, so the window start does not turn every App's first deploy in it
-- into first_seen. Partitioned by team, App and environment: the doc query's
-- per-App lag after its environment filter, and another team's App of the
-- same name never decides this team's kinds. A release reconciles every App
-- sharing its GitOps revision: count releases as distinct revisions.
--
-- Whether an App is infrastructure, and the product of a deploy whose own
-- revision names none, are the App's (sql/apps.sql), decided in Go.
--
-- $1 team, $2 window start (inclusive), $3 window end (exclusive).
WITH window_apps AS (
  SELECT DISTINCT app_name, environment
  FROM argocd_events
  WHERE team = $1 AND occurred_at >= $2 AND occurred_at < $3
    AND operation_phase IN ('Succeeded', 'Failed', 'Error')
),
history AS (
  SELECT a.id, a.images,
         lag(a.images) OVER w AS previous_images,
         a.images IS NOT NULL AND row_number() OVER w = 1 AS first_seen
  FROM (
    SELECT e.id, e.team, e.app_name, e.environment, e.occurred_at,
           -- Sorted: the same images in another order are not a change.
           (SELECT array_agg(i ORDER BY i) FROM jsonb_array_elements_text(e.payload->'images') i) AS images
    FROM argocd_events e
    JOIN window_apps wa ON wa.app_name = e.app_name AND wa.environment IS NOT DISTINCT FROM e.environment
    WHERE e.team = $1 AND e.operation_phase = 'Succeeded' AND e.occurred_at < $3
  ) a
  -- A sync naming no images deployed none: its own partition, so it is a
  -- sync and the next deploy compares with the images before it.
  WINDOW w AS (PARTITION BY a.team, a.app_name, a.environment, a.images IS NULL ORDER BY a.occurred_at, a.id)
),
revisions AS (
  SELECT DISTINCT revision FROM argocd_events
   WHERE team = $1 AND occurred_at >= $2 AND occurred_at < $3
     AND operation_phase IN ('Succeeded', 'Failed', 'Error')
),
-- The revision's product is the project of the GitOps repo it is a commit
-- in, first sighting first: pushed, or the merge commit of a PR merged in
-- the UI (no push event; one index probe per revision,
-- ix_bitbucket_events_merge_commit).
revision_projects AS (
  SELECT r.revision, /*{{revision_project}}*/ AS project
  FROM revisions r
)
SELECT a.id,
       a.app_name AS app,
       rp.project,
       a.environment,
       a.revision,
       -- The revision's first successful sync to this environment by the
       -- team, over all history: a release counts in the week it first
       -- deployed, whatever window reads it.
       (SELECT min(r.occurred_at)
          FROM argocd_events r
         WHERE r.team = a.team AND r.revision = a.revision
           AND r.environment IS NOT DISTINCT FROM a.environment
           AND r.operation_phase = 'Succeeded') AS revision_first_at,
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
LEFT JOIN revision_projects rp ON rp.revision = a.revision
WHERE a.team = $1 AND a.occurred_at >= $2 AND a.occurred_at < $3
  AND a.operation_phase IN ('Succeeded', 'Failed', 'Error')
ORDER BY a.occurred_at, a.id
