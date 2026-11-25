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
-- The window Apps' successful deploys over the 26 weeks before the window's
-- end: an App none of them names a product of is infrastructure
-- (monitoring, a dependency bot): its GitOps commits are in a repo no
-- Bitbucket event names.
app_deploys AS (
  SELECT e.app_name, e.revision, e.occurred_at
  FROM argocd_events e
  WHERE e.team = $1 AND e.operation_phase = 'Succeeded'
    AND e.occurred_at >= $3 - interval '26 weeks' AND e.occurred_at < $3
    AND e.app_name IN (SELECT app_name FROM window_apps)
),
revisions AS (
  SELECT revision FROM argocd_events
   WHERE team = $1 AND occurred_at >= $2 AND occurred_at < $3
     AND operation_phase IN ('Succeeded', 'Failed', 'Error')
  UNION
  SELECT revision FROM app_deploys
),
-- A GitOps commit Bitbucket saw only as a PR's merge commit (merged in the
-- UI, no push event of its own) names the repo too. The payload keeps the
-- sender's casing, revisions are lowercased; nothing indexes the payload, so
-- read once for every revision (MATERIALIZED: inlined, the lookup below
-- would scan the table per revision).
merges AS MATERIALIZED (
  SELECT m.sha, m.repo_full_name, m.occurred_at
  FROM (SELECT lower(b.payload->'pullRequest'->'properties'->'mergeCommit'->>'id') AS sha,
               b.repo_full_name, b.occurred_at
          FROM bitbucket_events b
         WHERE b.event_type = 'pr:merged' AND b.repo_full_name IS NOT NULL) m
  JOIN revisions r ON r.revision = m.sha
),
-- The revision's product is the project of the GitOps repo it is a commit
-- in, first sighting first.
revision_projects AS (
  SELECT r.revision,
         (SELECT split_part(x.repo_full_name, '/', 1)
            FROM (SELECT b.repo_full_name, b.occurred_at
                    FROM bitbucket_events b
                   WHERE b.commit_sha = r.revision AND b.repo_full_name IS NOT NULL
                  UNION ALL
                  SELECT m.repo_full_name, m.occurred_at FROM merges m WHERE m.sha = r.revision) x
           ORDER BY x.occurred_at
           LIMIT 1) AS project
  FROM revisions r
),
-- Infrastructure only once Bitbucket could have seen a GitOps commit of the
-- App: a deploy after the team's first Bitbucket event. Without one (no
-- Bitbucket data yet, or none before the App's deploys) an App without a
-- product is unknown, not infrastructure.
infra_apps AS (
  SELECT d.app_name
  FROM app_deploys d
  JOIN revision_projects rp ON rp.revision = d.revision
  GROUP BY d.app_name
  HAVING bool_and(rp.project IS NULL)
     AND bool_or(d.occurred_at > (SELECT min(occurred_at) FROM bitbucket_events WHERE team = $1))
)
SELECT a.app_name AS app,
       -- The App's product is the project of the GitOps repo it deploys
       -- from: its revision is a commit there.
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
       END::text[] AS new_images,
       a.app_name IN (SELECT app_name FROM infra_apps) AS infra
FROM argocd_events a
LEFT JOIN history h ON h.id = a.id
LEFT JOIN revision_projects rp ON rp.revision = a.revision
WHERE a.team = $1 AND a.occurred_at >= $2 AND a.occurred_at < $3
  AND a.operation_phase IN ('Succeeded', 'Failed', 'Error')
ORDER BY a.occurred_at, a.id
