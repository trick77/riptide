package metrics

import (
	"cmp"
	"slices"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// Where a merged PR stood on production at an instant (prodPlacement).
const (
	// ProdOnProd: a production deploy before the instant shipped it.
	ProdOnProd = "on_prod"
	// ProdNotYet: none did, provably: every release of its repo's Apps
	// since it reached the trunk is resolved and none held it, or there was
	// none.
	ProdNotYet = "not_yet"
	// ProdNotPlaceable: tracing cannot tell; the reason says why.
	ProdNotPlaceable = "not_placeable"
)

// Why a merged PR cannot be placed, the first that applies.
const (
	// TraceNoMergeCommit: the payload names no merge commit, and no umbrella
	// brought it in: nothing marks where it reached the trunk.
	TraceNoMergeCommit = "no_merge_commit"
	// TraceOffTrunk: merged into another branch than the trunk, and no PR
	// merged that branch into the trunk by the instant: it may be waiting
	// there, or have gone on some other way (a push, a hotfix branch
	// deployed as it is).
	TraceOffTrunk = "off_trunk"
	// TraceRepoUntraced: no deploy ever showed an App deploys its repo (no
	// compare link names it, no image of its runs, none named after it).
	TraceRepoUntraced = "repo_untraced"
	// TraceInSilence: a release that may have shipped it lay in or just
	// after a lost Bitbucket silence, Argo CD did not report yet when it
	// reached the trunk, or a release shows it went out with a deploy
	// riptide did not see (TraceRelease.ShippedBefore).
	TraceInSilence = "in_silence"
	// TraceReleaseUnresolved: a release of its repo's Apps since it reached
	// the trunk could not be resolved: it may have shipped it.
	TraceReleaseUnresolved = "release_unresolved"
)

// placement is where a merged PR stood on production at an instant: its
// state, the reason when not placeable, and on production the first deploy
// with its route and whether its stretch was a lower bound.
type placement struct {
	state, reason string
	at            *time.Time
	route         string
	lowerBound    bool
}

// prodPlacement places a merged PR on production as of cutoff, reading only
// what happened before it: a closed period stays as it was at its end
// whatever deploys later (owner's decision, 2026-11). A library's PR is no
// one's to place (dataset.PR.Library): callers leave it out.
func prodPlacement(ds *dataset.Dataset, pr dataset.PR, cutoff time.Time) placement {
	prod := ds.Meta.ProductionStage
	for _, e := range pr.Environments {
		if e.Environment == prod && e.FirstDeployedAt.Before(cutoff) {
			return placement{state: ProdOnProd, at: ptr(e.FirstDeployedAt), route: e.Route, lowerBound: e.LowerBound}
		}
	}
	notPlaceable := func(reason string) placement { return placement{state: ProdNotPlaceable, reason: reason} }
	switch {
	case pr.OffTrunk && (pr.TrunkAt == nil || !pr.TrunkAt.Before(cutoff)):
		// Merged into a branch no PR had merged on into the trunk by the
		// cutoff: whether its work waits there or went on otherwise, nothing
		// tells. The same before and after its umbrella merges later, so a
		// closed period stays as it was.
		return notPlaceable(TraceOffTrunk)
	case pr.TrunkAt == nil:
		return notPlaceable(TraceNoMergeCommit)
	case !pr.TrunkAt.Before(cutoff):
		return placement{state: ProdNotYet}
	}
	rt := ds.Traces[pr.Repo]
	if rt.DeployedSince == nil || !rt.DeployedSince.Before(cutoff) {
		return notPlaceable(TraceRepoUntraced)
	}
	if argo := ds.Meta.DataStart.ArgoCD; argo == nil || argo.After(*pr.TrunkAt) {
		return notPlaceable(TraceInSilence)
	}
	// Where it stood on a trunk branch: where it reached its trunk, and
	// onward on master or main when it got there before the cutoff.
	type position struct {
		at     time.Time
		branch string
	}
	at := []position{{*pr.TrunkAt, deref(pr.TrunkBranch)}}
	if pr.OnwardAt != nil && pr.OnwardAt.Before(cutoff) {
		at = append(at, position{*pr.OnwardAt, deref(pr.OnwardBranch)})
	}
	earlier, silent, unresolved := false, false, false
	for _, r := range rt.Releases {
		// Evidence only when a deploy before the cutoff had shown its App
		// deploys the repo: a later one must not move a closed period.
		if r.Environment != prod || !r.At.After(*pr.TrunkAt) || !r.At.Before(cutoff) || !r.Known.Before(cutoff) {
			continue
		}
		resolved := r.Unchanged
		for _, p := range at {
			earlier = earlier || r.ShippedBefore(p.at, p.branch)
			resolved = resolved || r.Resolved(p.at, p.branch)
		}
		switch {
		case resolved:
		case r.Silence:
			silent = true
		default:
			unresolved = true
		}
	}
	switch {
	case earlier:
		// On production, but since a deploy riptide did not see: when, no
		// one can tell.
		return notPlaceable(TraceInSilence)
	case unresolved:
		return notPlaceable(TraceReleaseUnresolved)
	case silent:
		return notPlaceable(TraceInSilence)
	}
	return placement{state: ProdNotYet}
}

// deref is *s, "" for nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// TraceReasons counts the PRs tracing could not place, by reason.
type TraceReasons struct {
	NoMergeCommit     int `json:"no_merge_commit"`
	OffTrunk          int `json:"off_trunk"`
	ReleaseUnresolved int `json:"release_unresolved"`
	RepoUntraced      int `json:"repo_untraced"`
	InSilence         int `json:"in_silence"`
}

func (r *TraceReasons) add(reason string) {
	switch reason {
	case TraceNoMergeCommit:
		r.NoMergeCommit++
	case TraceOffTrunk:
		r.OffTrunk++
	case TraceReleaseUnresolved:
		r.ReleaseUnresolved++
	case TraceRepoUntraced:
		r.RepoUntraced++
	case TraceInSilence:
		r.InSilence++
	}
}

// TraceCounts is how a set of the period's merged PRs stood on production
// at the period's end. Merged = OnProd + NotYet + NotPlaceable.
type TraceCounts struct {
	// Merged are the merged PRs (Throughput.Merged's) of repos that are
	// rolled out: the base. Library PRs are never in it.
	Merged int `json:"merged"`
	// OnProd were shipped to production by the period's end; LowerBound of
	// them by a stretch whose start was a lower bound (they may have gone
	// out earlier).
	OnProd     int `json:"on_prod"`
	LowerBound int `json:"lower_bound"`
	// NotYet were provably not on production at the period's end;
	// OldestNotYet is the merge of the oldest of them, null without one.
	NotYet       int        `json:"not_yet"`
	OldestNotYet *time.Time `json:"oldest_not_yet"`
	// NotPlaceable are the rest: tracing cannot tell, Reasons say why.
	NotPlaceable int          `json:"not_placeable"`
	Reasons      TraceReasons `json:"reasons"`
	// Library are the merged PRs of library repos (dataset.PR.Library):
	// work, never rolled out, outside Merged.
	Library int `json:"library"`
}

func (c *TraceCounts) add(pr dataset.PR, pl placement) {
	if pr.Library {
		c.Library++
		return
	}
	c.Merged++
	switch pl.state {
	case ProdOnProd:
		c.OnProd++
		if pl.lowerBound {
			c.LowerBound++
		}
	case ProdNotYet:
		c.NotYet++
		if pr.MergedAt != nil && (c.OldestNotYet == nil || pr.MergedAt.Before(*c.OldestNotYet)) {
			c.OldestNotYet = ptr(*pr.MergedAt)
		}
	default:
		c.NotPlaceable++
		c.Reasons.add(pl.reason)
	}
}

// RepoCoverage is TraceCounts for one repo.
type RepoCoverage struct {
	Repo    string `json:"repo"`
	Project string `json:"project"`
	TraceCounts
}

// Coverage is how the period's merged PRs (the people's, Throughput.Merged)
// reached production, frozen at the period's end: in all, and per repo
// (every repo with a merged PR, library repos too), the repos with PRs not
// yet on production first (most, then the oldest), then those tracing
// cannot place, then by name.
type Coverage struct {
	TraceCounts
	Repos []RepoCoverage `json:"repos"`
}

// coverage places humans (mergedPRs) on production as of the period's end.
func coverage(ds *dataset.Dataset, humans []dataset.PR, p Period) Coverage {
	out := Coverage{Repos: []RepoCoverage{}}
	by := map[string]*RepoCoverage{}
	for _, pr := range humans {
		var pl placement
		if !pr.Library {
			pl = prodPlacement(ds, pr, p.To)
		}
		out.add(pr, pl)
		r := by[pr.Repo]
		if r == nil {
			r = &RepoCoverage{Repo: pr.Repo, Project: pr.Project}
			by[pr.Repo] = r
		}
		r.add(pr, pl)
	}
	for _, r := range by {
		out.Repos = append(out.Repos, *r)
	}
	slices.SortFunc(out.Repos, func(a, b RepoCoverage) int {
		oldest := func(r RepoCoverage) time.Time {
			if r.OldestNotYet == nil {
				return time.Time{}
			}
			return *r.OldestNotYet
		}
		byOldest := 0
		if a.NotYet > 0 && b.NotYet > 0 {
			byOldest = oldest(a).Compare(oldest(b))
		}
		return cmp.Or(cmp.Compare(b.NotYet, a.NotYet), byOldest, cmp.Compare(b.NotPlaceable, a.NotPlaceable),
			cmp.Compare(a.Repo, b.Repo))
	})
	return out
}

// leadStart is where a PR's lead time starts: its first commit (the oldest
// pushed to its branch, sql/prs.sql), else its opening; nil when neither is
// known (a PR opened before Bitbucket reported).
func leadStart(pr dataset.PR) *time.Time {
	if pr.FirstCommitAt != nil {
		return pr.FirstCommitAt
	}
	return pr.OpenedAt
}
