package metrics

import (
	"cmp"
	"slices"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// The PRs open at the period's end. Every other PR number counts a PR once
// it is merged, so a PR left open for weeks is in none of them: this is
// where it shows, per repo, with what it waits on.
const (
	// OpenLongDays: a PR open this many days or more is long open. Chosen,
	// not measured (2026-11-15: open "for days or even a week++" is the
	// congestion to see).
	OpenLongDays = 7
	// openTop: the repos kept past the long-open ones.
	openTop = 8
)

// What an open PR waits on, one each, the first that applies.
const (
	// WaitDraft: still a draft, its author's.
	WaitDraft = "draft"
	// WaitChanges: a reviewer asked for changes.
	WaitChanges = "changes"
	// WaitMerge: approved, not merged.
	WaitMerge = "merge"
	// WaitReview: looked at, not approved.
	WaitReview = "review"
	// WaitFirstReview: nobody looked at it yet.
	WaitFirstReview = "first_review"
)

// waitsOn is what an open PR waits on. A reviewer's needs-work holds the
// PR whatever another approved: Bitbucket blocks the merge on it.
func waitsOn(pr dataset.OpenPR) string {
	switch {
	case pr.Draft:
		return WaitDraft
	case pr.NeedsWork:
		return WaitChanges
	case pr.Approved:
		return WaitMerge
	case pr.Touched:
		return WaitReview
	}
	return WaitFirstReview
}

// Waiting counts open PRs by what they wait on.
type Waiting struct {
	Draft       int `json:"draft"`
	FirstReview int `json:"first_review"`
	Review      int `json:"review"`
	Changes     int `json:"changes"`
	Merge       int `json:"merge"`
}

func (w *Waiting) add(on string) {
	switch on {
	case WaitDraft:
		w.Draft++
	case WaitFirstReview:
		w.FirstReview++
	case WaitReview:
		w.Review++
	case WaitChanges:
		w.Changes++
	case WaitMerge:
		w.Merge++
	}
}

// OpenRepo is one repo's open PRs.
type OpenRepo struct {
	Repo string `json:"repo"`
	Open int    `json:"open"`
	// Long are those open OpenLongDays or more.
	Long int `json:"long"`
	// OldestS is the oldest one's age.
	OldestS float64 `json:"oldest_s"`
	// Age is over the repo's open PRs.
	Age     Spread  `json:"age"`
	Waiting Waiting `json:"waiting"`
}

// OpenPRs are the PRs open at the period's end: GitOps promotions left out,
// bots only if asked.
type OpenPRs struct {
	// At is the instant: the period's end, now for the current one.
	At       time.Time `json:"at"`
	LongDays int       `json:"long_days"`
	Open     int       `json:"open"`
	Long     int       `json:"long"`
	Age      Spread    `json:"age"`
	Waiting  Waiting   `json:"waiting"`
	// LongWaiting is what the long-open ones wait on.
	LongWaiting Waiting `json:"long_waiting"`
	// Repos: the repos with a long-open PR, by how many, then by the
	// oldest; then the rest the same way, the top openTop and every one
	// with a long-open PR.
	Repos []OpenRepo `json:"repos"`
}

// openAge is an open PR's age at the instant; never negative.
func openAge(pr dataset.OpenPR, at time.Time) float64 {
	return max(at.Sub(pr.OpenedAt).Seconds(), 0)
}

// openPRs is the dataset's PRs open at its end, for the page's filter.
func openPRs(ds *dataset.Dataset, f filter) []dataset.OpenPR {
	var out []dataset.OpenPR
	for _, pr := range ds.OpenPRs {
		if f.inKey(pr.Project) && (f.bots || !pr.Bot) {
			out = append(out, pr)
		}
	}
	return out
}

func openSection(ds *dataset.Dataset, f filter) OpenPRs {
	at := ds.Meta.To
	long := float64(OpenLongDays * 24 * 60 * 60)
	out := OpenPRs{At: at, LongDays: OpenLongDays, Repos: []OpenRepo{}}
	var ages []float64
	type repoAcc struct {
		row  OpenRepo
		ages []float64
	}
	repos := map[string]*repoAcc{}
	for _, pr := range openPRs(ds, f) {
		age, on := openAge(pr, at), waitsOn(pr)
		r := repos[pr.Repo]
		if r == nil {
			r = &repoAcc{row: OpenRepo{Repo: pr.Repo}}
			repos[pr.Repo] = r
		}
		out.Open++
		r.row.Open++
		out.Waiting.add(on)
		r.row.Waiting.add(on)
		if age >= long {
			out.Long++
			r.row.Long++
			out.LongWaiting.add(on)
		}
		r.row.OldestS = max(r.row.OldestS, age)
		ages = append(ages, age)
		r.ages = append(r.ages, age)
	}
	out.Age = spread(ages)
	rows := make([]OpenRepo, 0, len(repos))
	for _, r := range repos {
		r.row.Age = spread(r.ages)
		rows = append(rows, r.row)
	}
	slices.SortFunc(rows, func(a, b OpenRepo) int {
		return cmp.Or(cmp.Compare(b.Long, a.Long), cmp.Compare(b.OldestS, a.OldestS), cmp.Compare(a.Repo, b.Repo))
	})
	for i, r := range rows {
		if i < openTop || r.Long > 0 {
			out.Repos = append(out.Repos, r)
		}
	}
	return out
}
