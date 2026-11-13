# Correlating deploys back to commits

Lead time for changes, tickets-per-deploy and per-ticket flow all need the same
thing: given an Argo CD deploy, which App-repo commits did it ship?

`argocd_events.revision` does **not** answer that. It is the GitOps-repo SHA —
several Apps of one service share it, and it matches neither
`pipeline_events.commit_sha` nor `bitbucket_events.commit_sha`. Image tags do
not answer it either: they are usually versions (`registry/app:2.0.41`), not
commit SHAs.

## The contract: image reference

CI senders report the full image references they published, `image_ref` for
one, `image_refs` for several; both land in `pipeline_events.image_refs`. A run
stays one row however many images it pushes (a monorepo UI building three apps,
a multi-module service), so build counts and CI time are not multiplied. Argo CD
stores the rendered references of the synced manifests in
`argocd_events.payload->'images'`. They are the same strings, so the join is
exact and the pipeline row carries the App-repo `commit_sha`:

```sql
-- Per deployed image, the run that produced it: the latest run started before
-- the deploy that pushed that tag. A tag can be pushed twice (a run that fails
-- after its push, then a re-run, possibly of another commit); the registry
-- holds the last push, so that run is the one Argo deployed.
-- No status filter: senders report an image once it is pushed, and an UNSTABLE
-- run, or one failing in a later step, still published what Argo deployed.
-- started_at, not finished_at: a release run often deploys (infra-repo bump)
-- before its last stage ends.
with produced as (
  select distinct on (a.id, img.ref)
         a.id as deploy_id, a.app_name, a.environment, a.occurred_at as deployed_at,
         p.commit_sha, p.pipeline_name
  from argocd_events a
  cross join lateral jsonb_array_elements_text(a.payload->'images') as img(ref)
  -- @> uses the GIN index on image_refs; `img.ref = any(p.image_refs)` scans.
  join pipeline_events p
    on p.image_refs @> array[img.ref]::varchar[]
   -- STARTED and COMPLETED rows can carry the same images; keep one phase.
   and p.phase = 'COMPLETED'
   and p.started_at <= a.occurred_at
  where a.operation_phase = 'Succeeded'
  order by a.id, img.ref, p.started_at desc
)
-- One row per deploy and commit: a deploy carrying two images of one run (a
-- multi-module service) must count that commit once, or deploy counts and
-- lead-time averages double.
select distinct deploy_id, app_name, environment, deployed_at, commit_sha, pipeline_name
from produced;
```

Lead time then measures from the first sighting of `p.commit_sha` in
`bitbucket_events` to `a.occurred_at`.

Senders that publish no image simply omit both fields; those runs stay
uncorrelated (`image_refs = '{}'`), which is honest.

## Fallback for rows collected before image references

Where the GitOps repo is itself onboarded to the Bitbucket webhook, its commits
are in `bitbucket_events` and `argocd_events.revision` matches
`bitbucket_events.commit_sha` for those rows. If the release commit's message
lists the component bumps it carries — many release-note generators emit compare
links containing both the old and the new App-repo SHA — those SHAs can be
extracted at read time:

```sql
with release as (
  select distinct a.revision, a.app_name, a.environment,
         a.occurred_at as deployed_at, b.payload::text as body
  from argocd_events a
  join bitbucket_events b on b.commit_sha = a.revision
  where a.operation_phase = 'Succeeded'
    -- prefilter: keeps the regex off every push payload in the table
    and b.payload::text like '%compare/diff?targetBranch=%'
),
bumped as (
  select revision, app_name, environment, deployed_at,
         regexp_matches(
           body,
           'repos/([a-z0-9-]+)/compare/diff\?targetBranch=([0-9a-f]{40})&sourceBranch=([0-9a-f]{40})',
           'g') as m
  from release
)
select app_name, environment, deployed_at,
       m[1] as app_repo, m[3] as commit_sha
from bumped;
```

This depends on a site-specific message format and only sees releases that go
through the release-PR path — a direct version-bump commit carries no links.
Treat it as a way to make history queryable, never as the contract: prefer
`image_refs` for everything collected from now on.

## What not to do

Do not introduce a `service_id`, a service column, or a hand-maintained mapping
of app names to repositories to paper over a missing join. Correlation stays
identifier-based: commit SHA between Bitbucket and CI, image reference between
CI and Argo CD.
