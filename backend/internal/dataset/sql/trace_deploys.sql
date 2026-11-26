-- Every successful deploy of the team over all history, with the images it
-- ran, trimmed, each once, sorted: what tracing (dataset.trace) reads to
-- tell which stretch of its repos' trunks a deploy shipped. Read once per
-- page request (dataset.LoadShared), whatever its windows: a deploy's
-- stretch depends on the deploys before it, never on a window.
--
-- $1 team.
SELECT a.id,
       a.app_name AS app,
       a.environment,
       a.revision,
       a.occurred_at AS at,
       coalesce((SELECT array_agg(DISTINCT btrim(i) ORDER BY btrim(i))
                   FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(a.payload->'images') = 'array'
                                                       THEN a.payload->'images' END) i
                  WHERE btrim(i) <> ''), '{}')::text[] AS images
FROM argocd_events a
WHERE a.team = $1 AND a.operation_phase = 'Succeeded'
  -- Deploys stored before argocd_events.environment existed name no stage.
  AND a.environment IS NOT NULL
ORDER BY a.occurred_at, a.id
