-- Library evidence for the team, over all history: what Go tells a library
-- repo from (dataset.libraryEvidence), built, never rolled out. One row, read
-- once per page, whatever window and product: no repo list to pass.
--   images      every image Argo CD deployed for the team, as referenced: a
--               deployable repo's image is named after it even when its CI
--               does not report
--   traced      the repo slugs a deployed GitOps release commit names in a
--               compare link (deploy_commit_ranges' first step)
--   publishes   the repos a CI run built an image in, deployed or not: a repo
--               building one not deployed yet is a deployable repo not traced
--               yet, in doubt no library. A run's repo by one index probe of
--               its commit (sql/runs.sql's attribution).
--   rolled_out  some run built an image Argo CD deployed, by its full
--               reference (docs/correlating-deploys-to-commits.md): with
--               traced, the team's tracing works at all
--
-- $1 team.
WITH deployed AS MATERIALIZED (
  SELECT DISTINCT i AS image
  FROM argocd_events a
  CROSS JOIN LATERAL jsonb_array_elements_text(CASE WHEN jsonb_typeof(a.payload->'images') = 'array'
                                                    THEN a.payload->'images' ELSE '[]'::jsonb END) i
  WHERE a.team = $1 AND a.operation_phase = 'Succeeded'
),
publishing AS MATERIALIZED (
  SELECT p.commit_sha, bool_or(p.image_refs::text[] && (SELECT coalesce(array_agg(image), '{}') FROM deployed)) AS rolled_out
  FROM pipeline_events p
  WHERE p.team = $1 AND p.phase = 'COMPLETED' AND p.commit_sha IS NOT NULL AND cardinality(p.image_refs) > 0
  GROUP BY p.commit_sha
)
SELECT (SELECT coalesce(array_agg(image ORDER BY image), '{}') FROM deployed)::text[] AS images,
       -- Each deployed revision's events once, each body cast to text once
       -- (OFFSET 0 keeps the planner from casting it again per use), as
       -- deploy_commit_ranges reads them.
       (SELECT coalesce(array_agg(DISTINCT lower(m[1])), '{}')
          FROM (SELECT DISTINCT revision FROM argocd_events WHERE team = $1 AND operation_phase = 'Succeeded') rev
          CROSS JOIN LATERAL (SELECT b.payload::text AS body FROM bitbucket_events b WHERE b.commit_sha = rev.revision OFFSET 0) b
          CROSS JOIN LATERAL regexp_matches(b.body, 'repos/([a-z0-9._-]+)/compare/diff\?targetBranch=', 'g') m
         WHERE b.body LIKE '%compare/diff?targetBranch=%')::text[] AS traced,
       (SELECT coalesce(array_agg(DISTINCT r.repo), '{}')
          FROM publishing x
          CROSS JOIN LATERAL (SELECT b.repo_full_name AS repo
                                FROM bitbucket_events b
                               WHERE b.commit_sha = x.commit_sha AND b.repo_full_name IS NOT NULL
                               LIMIT 1) r)::text[] AS publishes,
       EXISTS (SELECT 1 FROM publishing WHERE rolled_out) AS rolled_out
