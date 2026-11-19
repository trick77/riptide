-- Where each source's data starts and ends for the team, so the UI can tell
-- "no deploys in this window" from "argocd was not reporting yet". Starts
-- over all history, ends as of $2: a past window's end, NULL for a window
-- ending now (a sender's clock running ahead must not hide its latest
-- event). $1 team.
SELECT
  (SELECT min(occurred_at) FROM bitbucket_events WHERE team = $1) AS bitbucket,
  (SELECT min(occurred_at) FROM pipeline_events  WHERE team = $1) AS pipeline,
  (SELECT min(occurred_at) FROM argocd_events    WHERE team = $1) AS argocd,
  (SELECT min(occurred_at) FROM noergler_events  WHERE team = $1) AS noergler,
  -- Each source's latest event before the window's end: how fresh the data
  -- is, and for a closed period whether the source went quiet within it (a
  -- later event would hide that).
  (SELECT max(occurred_at) FROM bitbucket_events WHERE team = $1 AND ($2::timestamptz IS NULL OR occurred_at < $2)) AS bitbucket_end,
  (SELECT max(occurred_at) FROM pipeline_events  WHERE team = $1 AND ($2::timestamptz IS NULL OR occurred_at < $2)) AS pipeline_end,
  (SELECT max(occurred_at) FROM argocd_events    WHERE team = $1 AND ($2::timestamptz IS NULL OR occurred_at < $2)) AS argocd_end,
  (SELECT max(occurred_at) FROM noergler_events  WHERE team = $1 AND ($2::timestamptz IS NULL OR occurred_at < $2)) AS noergler_end,
  -- When riptide first received a pipeline row for the team: build data
  -- (build_type and friends) is complete only from here on.
  (SELECT min(created_at)  FROM pipeline_events  WHERE team = $1) AS build_data_from,
  -- When the team's pipelines first reported a build_type: before it, a
  -- run's NULL build_type means "not collected yet", after it "the sender
  -- said nothing".
  (SELECT min(created_at)  FROM pipeline_events  WHERE team = $1 AND build_type IS NOT NULL) AS build_type_from
