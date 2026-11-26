# Change lead time (DORA), per PR

**How long from a developer's first commit of a change to that change running
in production.** The unit is the pull request, not the commit: a team reasons
about PRs, and a commit count says little about the work (owner's decision).
One of DORA's three throughput metrics.

## The metric

Per PR: the people's PRs merged in the period that reached production by the
period's end, each from its first commit (the oldest commit pushed to its
branch, else its opening) to the first production deploy that shipped it. The
page shows the typical value, the slowest 10 % and the basis ("aus n PRs"),
next to how many of the period's merged PRs it could place ("n von m
gemergten PRs bis PROD verfolgt") and how many were provably not on production
yet at the period's end ("n gemergte PRs noch nicht auf PROD").

- **Bots' PRs are in no number**, neither are their pushes: dependency updates
  ship, but the number describes the team's work.
- **A person's PR from a bot's branch** (metrics.md, "Human vs automated
  split") starts at the oldest commit of a push of its own life by no bot
  (declared, a SERVICE account, or a bot by riptide's automation detection),
  else at its opening: the bot pushed its commits to the branch for days
  before the person took it over, and they are no work of the person's
  (owner, 2026-11). Every other PR keeps the oldest commit pushed to its
  branch, whoever pushed it.
- **Library repos' PRs are outside the base**: libraries are work, but never
  rolled out, so never "not traced" either.
- **A closed period is frozen at its end.** Only deploys before the end count:
  a PR merged in the period and shipped after it is "not on production yet" in
  that period, for good. The comparison with the previous period is fair: each
  had the same time. What tells which Apps deploy a repo is read up to the end
  too: a release counts only once a deploy before the end showed its App
  deploys the repo, and which image repositories are the repo's is what its
  runs had published by the release. A repo first deployed after the end
  stays a library in the period.

## Placing a PR on production

`dataset.trace` (backend/internal/dataset/trace.go, reading
`sql/trace_*.sql`) does it once per page request, over all history, the team's
data only. Two steps.

**Where a PR reached the trunk.** The trunk is master, main, or the branch most
of the repo's merged PRs went into (`repo_trunks`). A PR into the trunk arrives
with its merge commit's first push to that branch (Bitbucket's merge pushes it
in the same second; the merge time when that push was not seen; a
fast-forward merge's commit was pushed to the source branch before, which is
not the trunk). Merges are true merge commits and always the push's tip, so
Bitbucket's cap of five commits per push never hides one. A PR into another
branch arrives with the first PR with a merge commit merged from that branch
into a trunk at or after it (its umbrella), one level deep; a PR Bitbucket
marked merged when an umbrella's merge brought it in, with the umbrella. A PR
with none of these is not placeable (`no_merge_commit`, or `off_trunk` for one
into another branch no PR merged on). A PR into a trunk other than master or
main (develop, where most PRs go), itself or through its umbrella, also
arrives on master or main with the first PR merged from its trunk into one
of them: releases built from master hold it there, releases built from
develop where it reached develop.

**Which stretch of the trunk a deploy shipped.** For every successful deploy,
per repo, the first of these routes that resolves:

1. **Compare links** in the GitOps release commit, from the previous to the new
   App-repo commit
   ([Correlating deploys back to commits](correlating-deploys-to-commits.md)).
   The new end must have reached the trunk, by its push, before the deploy. The
   old end likewise (exact), or as the tip before the repo's first push riptide
   holds (that push's `fromHash`, exact). An old end no payload names predates
   the data: the stretch then starts at the repo's first trunk push, a **lower
   bound**. Several links of one repo in one release (Apps on different
   versions) are merged into one stretch.
2. **The deployed images.** Each deployed reference is matched exactly to the
   CI run that published it (the latest run started before the deploy). The
   run's commit, placed where it reached the trunk, is the head, but never
   after the run started: a UI job pushes its own release commit minutes into
   the run, and what landed in between is not in the image. The start is the
   newest head of the same image repository's tags in this App before (tags are
   compared as sets: an App may run two tags of one image side by side), exact;
   else the deploy that first ran them (a lower bound: nothing pushed after it
   was in them; a re-sync of the same tags is no new start); for an App's first
   deploy of the image repository, after what another App's deploy of it
   shipped, else the repo's first trunk push (a lower bound: everything on the
   trunk before the head, minus what an earlier deploy shipped). A head older
   than a lower bound start ships an empty stretch that leaves the release
   unresolved for what came before it.

A known run head also caps a compare link's new end: a release tool's commit
the link names may be pushed after the build started. The App's next link
from that commit starts where the head cut it, so what landed in between is
in the next release, not in a gap.

A PR is on an environment from the first deploy there whose stretch holds its
arrival on the trunk. Never counted twice: the first deploy wins.

## Not on production, or not placeable

At a cutoff (the period's end; for the facts, now), a merged PR not shipped by
then is **not yet on production** only with proof: every release (a deploy that
brought new images, or an App's first) of the Apps known to deploy its repo
since the PR reached the trunk was resolved for the repo and held none of it.
Otherwise it is **not placeable**, with the first reason that applies:

| Reason | Meaning |
|---|---|
| `off_trunk` | merged into another branch, and no PR merged that branch into the trunk by the cutoff: its work may wait there, or have gone on by a push or a hotfix deploy; the same when its umbrella merges only after the cutoff, and for a PR on a develop trunk no PR merged on to master by the cutoff while the master releases since were resolved exactly (an unresolved one keeps `release_unresolved`) |
| `no_merge_commit` | nothing marks where it reached the trunk |
| `repo_untraced` | no deploy ever showed an App deploys its repo (a compare link naming it, an image of its runs, an image named after it) |
| `in_silence` | a release that may have shipped it lay in or within a week after a lost Bitbucket silence, Argo CD did not report yet when it reached the trunk, or a release shows it went out unseen (below) |
| `release_unresolved` | a release since then could not be resolved: it may have shipped it |

A release resolved by a lower bound that starts after the PR's arrival may have
shipped it, or an earlier one did: unresolved for that PR. A release whose
exact stretch starts after the PR's arrival proves the App ran it already, with
a deploy riptide did not see (one lost, or before Argo CD reported): on
production, since when no one can tell, so `in_silence`, never "not yet".

## Reading it

Lower is better, and the split between environments is where the signal is:
the breakdown in the flow section shows where the time goes (coding, review,
merge, to the stage before production, to production), on the same placement.

Beware the tempting shortcut: taking the *newest* commit in each release and
calling that lead time. It answers "how stale was the freshest change" and
reads far too fast. Per PR from its first commit, or not at all.

## Limits, so nobody over-reads the number

- **The trunk is assumed linear and never rewritten.** "Reached the trunk no
  later than the head" stands for "is an ancestor of the head". A force push on
  the trunk counts dropped commits as shipped.
- **A lower bound reads long.** A PR placed by one (`lower_bound`) may have gone
  out earlier with a deploy riptide could not resolve; the page counts them.
  When they are more than a fifth of either period's PRs
  (`metrics.LeadLowerBoundShare`), no change against the previous period is
  judged (`incomplete`): the medians would differ by what tracing missed.
- **A PR without a start** (no pushed commit, no opening) is traced but in no
  time (`unmeasured`), said next to the number.
- **Releases riptide cannot resolve place nothing**, and the PRs that may have
  gone out with them are not placeable, never "not yet". The deploy frequency
  counts them (`releases_unresolved`).
- **A monorepo App shares its repo's stretch**: a PR touching only App B also
  reads as shipped by App A's release. Riptide sees no file paths.
- **Hotfix and release branches**: a head that never reached the trunk resolves
  no stretch; a PR into such a branch is `off_trunk`.
- **Ingestion is forward-only.** The metric covers what riptide has seen.

## The SQL views

`commit_sightings` (one row per commit of a push to the repo's trunk),
`deploy_commit_ranges` (compare links with both ends sighted) and
`lead_time_changes` (per commit, its first deploy) stay for ad-hoc per-commit
queries and for binaries mid-rollout. They know the first route's exact case
only; the page reads neither.
