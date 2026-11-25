-- noergler's first event per repository of the team, over all history:
-- it is set up repo by repo, so a PR without a size in a repo it reached
-- only later is a gap in the data, not an unreviewed PR. Bitbucket's per
-- repository is left out: no index serves it, a read of the team's every
-- event.
--
-- $1 team.
SELECT lower(repo) AS repo, min(occurred_at) AS noergler
FROM noergler_events
WHERE team = $1 AND repo IS NOT NULL
GROUP BY lower(repo)
