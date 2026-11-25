-- Every App of the team with a successful deploy in the 26 weeks before $2,
-- whatever window reads it: what Go decides an App's product and whether it
-- is infrastructure from, once per page (dataset.Classify), so two periods
-- never class one App apart.
--   project   the product most of its deploys' revisions name (ties: the
--             one named first): an App does not change products, so a
--             deploy whose GitOps commit Bitbucket missed is still its
--   last_at   its latest successful deploy: infrastructure only once
--             Bitbucket could have seen one of its GitOps commits
--   first_at  its first successful deploy naming images over all history,
--             the deploy deploys.sql calls first_seen: Go tells from it
--             whether the App went live as a release
--
-- $1 team, $2 end (exclusive).
WITH app_deploys AS (
  SELECT e.app_name, e.revision, e.occurred_at
  FROM argocd_events e
  WHERE e.team = $1 AND e.operation_phase = 'Succeeded'
    AND e.occurred_at >= $2::timestamptz - interval '26 weeks' AND e.occurred_at < $2
),
revisions AS (
  SELECT DISTINCT revision FROM app_deploys
),
revision_projects AS (
  SELECT r.revision, /*{{revision_project}}*/ AS project
  FROM revisions r
),
named AS (
  SELECT d.app_name, rp.project, count(*) AS n, min(d.occurred_at) AS first_named
  FROM app_deploys d
  JOIN revision_projects rp ON rp.revision = d.revision
  WHERE rp.project IS NOT NULL
  GROUP BY d.app_name, rp.project
)
SELECT a.app_name AS app,
       (SELECT n.project FROM named n WHERE n.app_name = a.app_name
         ORDER BY n.n DESC, n.first_named, n.project LIMIT 1) AS project,
       max(a.occurred_at) AS last_at,
       (SELECT min(f.occurred_at)
          FROM argocd_events f
         WHERE f.team = $1 AND f.app_name = a.app_name AND f.operation_phase = 'Succeeded'
           AND CASE WHEN jsonb_typeof(f.payload->'images') = 'array'
                    THEN jsonb_array_length(f.payload->'images') > 0 END) AS first_at
FROM app_deploys a
GROUP BY a.app_name
ORDER BY a.app_name
