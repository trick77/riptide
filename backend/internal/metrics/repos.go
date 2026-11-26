package metrics

import (
	"cmp"
	"slices"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
)

// ---- Per repo ---------------------------------------------------------------

// A team number pools busy and quiet repos: one repo shipping five times a
// day and nine shipping once a month read as "twice a week" (owner,
// 2026-11). RepoView sets the period's numbers out per repo: a row per repo
// for the ranking, and per number a strip of the repos' values, each judged
// against the team's own threshold where one exists. Built from the rows the
// page's read already holds: the period's and the previous period's
// datasets, tracing's releases, the build section's runs per repo.

// Repo groups (RepoRow.Group), in the ranking's order: what stands out first.
const (
	// RepoGroupNone: rolled out, and nothing released for more than
	// ReleaseBreachDays while people worked on it (the sprint rule broken).
	RepoGroupNone = "none"
	// RepoGroupRare: its last release more than ReleaseHintDays back.
	RepoGroupRare = "rare"
	// RepoGroupUnjudged: worked on, but whether it was rolled out cannot be
	// told (RepoRow.Reason says why): never counted as regular.
	RepoGroupUnjudged = "unjudged"
	// RepoGroupRegular: released within ReleaseHintDays.
	RepoGroupRegular = "regular"
	// RepoGroupLibrary: a library (dataset.PR.Library): work, never rolled
	// out, so never judged for its releases.
	RepoGroupLibrary = "library"
	// RepoGroupIdle: no counted work since its last release nor within
	// ReleaseBreachDays: nobody works on it, no finding.
	RepoGroupIdle = "idle"
)

// repoGroups are the groups in the ranking's order.
var repoGroups = []string{RepoGroupNone, RepoGroupRare, RepoGroupUnjudged, RepoGroupRegular, RepoGroupLibrary,
	RepoGroupIdle}

// Why a repo's releases cannot be judged (RepoRow.Reason).
const (
	// RepoReasonNotReporting: Argo CD had not reported for the team by the
	// period's end.
	RepoReasonNotReporting = "not_reporting"
	// RepoReasonInfra: only infrastructure Apps deploy it (infraRepos): no
	// team's delivery to judge.
	RepoReasonInfra = "infra"
	// RepoReasonUntraced: no deploy ever showed an App deploys it
	// (TraceRepoUntraced): where its work went nobody can tell.
	RepoReasonUntraced = "untraced"
)

// How the ranking opens and when a spread is worth a word.
const (
	// RepoRankOpen is how many rows the ranking shows before "show all".
	RepoRankOpen = 8
	// RepoRankMinHidden: fewer rows than this behind "show all" are shown
	// at once; a button for one or two rows hides nothing worth hiding.
	RepoRankMinHidden = 3
	// RepoHiddenNamed are the hidden repos a group's summary line names;
	// it counts the rest.
	RepoHiddenNamed = 5
	// RepoMinCases is the fewest PRs a repo's typical value rests on where
	// no threshold of the team's names a minimum (PR size): fewer are listed
	// as too few, never as good.
	RepoMinCases = 3
	// SpreadFarFactor: a repo at more than this many times the typical
	// repo's value, or less than its share, lies far from it.
	SpreadFarFactor = 2.0
	// SpreadDominantShare: one repo carrying this share of a count the team
	// adds up, or more, dominates the team's number.
	SpreadDominantShare = 0.5
	// SpreadMinRepos are the fewest repos with a value that say anything
	// about a spread.
	SpreadMinRepos = 3
)

// Verdicts of a repo's value on a strip (RepoDot.Verdict).
const (
	// DotBreach: past a threshold whose finding is the team's rule broken
	// (red).
	DotBreach = "breach"
	// DotHint: past a threshold whose finding is a hint.
	DotHint = "hint"
	// DotWithin: judged, within the team's threshold.
	DotWithin = "within"
	// DotNeutral: judged; the number has no threshold of the team's.
	DotNeutral = "neutral"
	// DotThin: too few cases to judge: listed as such, never as good.
	DotThin = "thin"
)

// Spread keys (RepoSpread.Key).
const (
	SpreadReleases      = "releases"
	SpreadToProd        = "to_prod"
	SpreadMerged        = "merged"
	SpreadBatch         = "batch"
	SpreadPickup        = "pickup"
	SpreadBuildDuration = "build_duration"
	SpreadBuildSuccess  = "build_success"
)

// Units of a spread's values beyond the facts' (UnitDays, UnitPerWeek,
// UnitSeconds, UnitShare).
const (
	// UnitLines: changed lines, added plus removed.
	UnitLines = "lines"
	// UnitWorkingDays: working days, Monday to Friday (workingDays).
	UnitWorkingDays = "working_days"
)

// Spread notes (SpreadNote.Kind).
const (
	// NoteDominant: one repo carries SpreadDominantShare of the count or
	// more.
	NoteDominant = "dominant"
	// NoteFar: repos lie far from the typical one (SpreadFarFactor).
	NoteFar = "far"
	// NotePooled: the team's number lies SpreadFarFactor or more from the
	// typical repo's: it adds every repo up (SpreadNote.Additive), or it
	// counts every case, so the repos with the most decide it.
	NotePooled = "pooled"
)

// RepoView is the period's numbers per repo of the view.
type RepoView struct {
	// Rows are every repo with a merged PR in the period or the previous
	// one, ranked: by group (repoGroups' order); within none and rare the
	// longest without a release first, within regular those past a
	// threshold first, then the slowest to production; else by name.
	Rows []RepoRow `json:"rows"`
	// Open is how many of Rows the ranking shows before "show all": all of
	// them when fewer than RepoRankMinHidden would stay hidden.
	Open int `json:"open"`
	// Groups are the groups with rows, in order: their size and the repos
	// past Open, which the closed ranking names in one line per group.
	Groups []RepoGroup `json:"groups"`
	// HintDays and BreachDays are the team's thresholds the groups were cut
	// by (release_hint_days, release_breach_days).
	HintDays   int `json:"hint_days"`
	BreachDays int `json:"breach_days"`
	// Spreads are one strip per number: releases, to_prod, merged, batch,
	// pickup, then per build kind its duration and success.
	Spreads []RepoSpread `json:"spreads"`
}

// RepoGroup is one group of the ranking.
type RepoGroup struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
	// HiddenCount are its repos past RepoView.Open; Hidden the first
	// RepoHiddenNamed of them, in rank order; More how many follow those.
	HiddenCount int      `json:"hidden_count"`
	Hidden      []string `json:"hidden"`
	More        int      `json:"more"`
}

// RepoFlag is a threshold a repo's number is past, a state's key: the
// release rule (StateNoRelease, StateReleaseDue), production
// (StateSlowToProd), the first review (StateFirstReviewWait). Tone is the
// dot's verdict, DotBreach or DotHint.
type RepoFlag struct {
	Key   string  `json:"key"`
	Tone  string  `json:"tone"`
	Value float64 `json:"value"`
}

// RepoRow is one repo's numbers over the period. A previous period's value
// (ReleasesPrev, ToProdPrev, MergedPrev, BatchPrev) is null where its
// sources did not see the previous period whole, as the comparison holds
// (ReasonIncomplete).
type RepoRow struct {
	Repo    string `json:"repo"`
	Project string `json:"project"`
	Group   string `json:"group"`
	// Reason is why its releases cannot be judged (RepoReason*); null
	// outside RepoGroupUnjudged.
	Reason *string `json:"reason"`
	// LastRelease is its last release to production before the period's
	// end: a release of an App that deploys it that tracing does not show
	// leaving it unchanged (dataset.TraceRelease.Unchanged), read as tracing
	// knew it then. Null without one. DaysSince is the whole days from it to
	// the period's end; without a release, from Argo CD's first report
	// (SinceLowerBound: at least that long).
	LastRelease     *time.Time `json:"last_release"`
	DaysSince       *float64   `json:"days_since"`
	SinceLowerBound bool       `json:"since_lower_bound"`
	// Releases are such releases in the period, each GitOps revision once,
	// an App's first deploy within FirstDeployGrace of Argo CD's first event
	// none (DeployFrequency counts it a first deploy);
	// ReleasesPerWeek over the period's weeks Argo CD reported, null under a
	// week; ReleasesPrev the previous period's per week.
	Releases        int      `json:"releases"`
	ReleasesPerWeek *float64 `json:"releases_per_week"`
	ReleasesPrev    *float64 `json:"releases_prev"`
	// ReleaseWeeks are its releases per week of the previous period and the
	// period (Trends.Weeks without the running week); null where Argo CD
	// had not reported.
	ReleaseWeeks []*int `json:"release_weeks"`
	// ToProd is the typical days its merged PRs of the period waited from
	// the trunk to production (typicalToProd, as StateSlowToProd judges
	// it), over ToProdN of them; null under NotOnProdMinPRs. ToProdPrev is
	// the previous period's.
	ToProd     *float64 `json:"to_prod"`
	ToProdN    int      `json:"to_prod_n"`
	ToProdPrev *float64 `json:"to_prod_prev"`
	// NotYet are the period's merged PRs not on production at its end
	// (Coverage.Repos).
	NotYet int `json:"not_yet"`
	// Merged are its merged PRs (Throughput's), MergedPerWeek over the
	// period's weeks Bitbucket reported (null under a week), MergedPrev the
	// previous period's; MergedWeeks per trend week as ReleaseWeeks, null in
	// a week Trends.Merged has none.
	Merged        int      `json:"merged"`
	MergedPerWeek *float64 `json:"merged_per_week"`
	MergedPrev    *float64 `json:"merged_prev"`
	MergedWeeks   []*int   `json:"merged_weeks"`
	// Batch is its typical changed lines per merged PR over Sized PRs with
	// a size; null under RepoMinCases. BatchPrev the previous period's.
	Batch     *float64 `json:"batch"`
	Sized     int      `json:"sized"`
	BatchPrev *float64 `json:"batch_prev"`
	// Pickup is its typical wait from ready to the first review in working
	// days (typicalPickup), over PickupN merged PRs; null under
	// FirstReviewMinPRs.
	Pickup  *float64 `json:"pickup"`
	PickupN int      `json:"pickup_n"`
	// Flags are the thresholds its numbers are past.
	Flags []RepoFlag `json:"flags"`
}

// RepoSpread is one number per repo on a strip: each repo's value at its
// position, the typical repo, the team's threshold, and in one line whether
// the team's number hides a wide spread.
type RepoSpread struct {
	Key string `json:"key"`
	// Kind is the build kind of a build spread; null otherwise.
	Kind *string `json:"kind"`
	// Unit is the values' unit: UnitPerWeek, UnitDays, UnitLines,
	// UnitWorkingDays, UnitSeconds, UnitShare.
	Unit string `json:"unit"`
	// HigherBetter: more is better; positions put the better end left
	// either way.
	HigherBetter bool `json:"higher_better"`
	// Shown: placed repos at two values or more, something to set side by
	// side; the page draws no strip otherwise. Same is the one value every
	// placed repo has where two or more share it and none differs: no
	// strip, a line ("all n repos at x").
	Shown bool        `json:"shown"`
	Same  *float64    `json:"same"`
	Scale StripScale  `json:"scale"`
	Dots  []RepoDot   `json:"dots"`
	Mark  *StripMark  `json:"mark"`
	Note  *SpreadNote `json:"note"`
	// Typical is the typical repo: the median of the judged repos' values,
	// its position TypicalX; null without a judged repo.
	Typical  *float64 `json:"typical"`
	TypicalX *float64 `json:"typical_x"`
	// Team is the team's own number the page's tile shows, pooled over every
	// case, where it has one; null otherwise.
	Team *float64 `json:"team"`
	// Judged, Thin and Apart count the dots: judged, too few cases, set
	// apart at the strip's end without a value.
	Judged int `json:"judged"`
	Thin   int `json:"thin"`
	Apart  int `json:"apart"`
}

// StripScale is a strip's axis: Lo to Hi, on a log scale where the values
// span a factor of logScaleFactor or more; X 0 is the better end.
type StripScale struct {
	Lo    float64     `json:"lo"`
	Hi    float64     `json:"hi"`
	Log   bool        `json:"log"`
	Ticks []StripTick `json:"ticks"`
}

// StripTick is a labelled value on a strip's axis.
type StripTick struct {
	Value float64 `json:"value"`
	X     float64 `json:"x"`
}

// StripMark is the team's threshold on a strip: its config key and value.
type StripMark struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
	X     float64 `json:"x"`
}

// RepoDot is one repo on a strip, ordered by value, the better end first.
type RepoDot struct {
	Repo    string `json:"repo"`
	Project string `json:"project"`
	// Value is null for a repo set apart (Apart): nothing released.
	Value *float64 `json:"value"`
	// N is the cases Value rests on.
	N       int    `json:"n"`
	Verdict string `json:"verdict"`
	// X is the position 0..1 on the scale, the better end 0; null when
	// Apart. Clamped: the value lies outside the scale (a 0 on a log scale)
	// and sits at its end.
	X       *float64 `json:"x"`
	Clamped bool     `json:"clamped"`
	Apart   bool     `json:"apart"`
}

// SpreadNote is the line under a strip when the team's number hides the
// repos: Kind NoteDominant names the Repo carrying Share of the count,
// NoteFar how many of Of repos lie Near the typical one, Above and Below
// it by SpreadFarFactor, NotePooled that the team's number is far from the
// typical repo's. Additive: the number adds the repos up (releases, merged
// PRs), rather than pooling every case.
type SpreadNote struct {
	Kind     string   `json:"kind"`
	Repo     *string  `json:"repo"`
	Share    *float64 `json:"share"`
	Of       int      `json:"of"`
	Near     int      `json:"near"`
	Above    int      `json:"above"`
	Below    int      `json:"below"`
	Additive bool     `json:"additive"`
}

// repoAcc gathers one repo's rows.
type repoAcc struct {
	row     RepoRow
	library bool
	// work are its counted merges outside libraries, both periods: whether
	// anyone works on it.
	work               []time.Time
	waits, prevWaits   []float64
	sizes, prevSizes   []float64
	pickups            []float64
	prevMerged         int
	releases, prevRels map[string]bool
	weeks              map[int]map[string]bool
	mergedWeek         []int
}

// trendWeekIndex is the index of the week t falls in among weeks, -1
// outside them.
func trendWeekIndex(weeks []Week, t time.Time) int {
	for i, w := range weeks {
		if !t.Before(w.Start) && t.Before(w.End) {
			return i
		}
	}
	return -1
}

// perWeek is n over the weeks of p since start (a source's first data)
// when that is a week or more; null otherwise.
func perWeek(n int, p Period, start *time.Time) *float64 {
	span := p.Span
	if start != nil && start.After(p.From) {
		span = float64(p.To.Sub(*start)) / float64(7*24*time.Hour)
	}
	if start == nil || span < 1 {
		return nil
	}
	return ptr(float64(n) / span)
}

// repoView is the per-repo view of ds over p.Period, prev over p.Previous,
// m and pm their sections as computeBoth made them.
func repoView(ds, prev *dataset.Dataset, m, pm Metrics, p Params, f filter) RepoView {
	th := p.thresholds()
	prod := ds.Meta.ProductionStage
	loc := p.Period.location()
	end := p.Period.To
	weeks := slices.Concat(pm.Period.Weeks, m.Period.Weeks)
	traces := map[string]dataset.RepoTrace{}
	for _, d := range []*dataset.Dataset{prev, ds} {
		for repo, t := range d.Traces {
			traces[repo] = t
		}
	}
	infra := infraRepos(ds, traces)
	// A release's revision, so one rolled out to several Apps counts once.
	revision := map[string]string{}
	for _, d := range []*dataset.Dataset{prev, ds} {
		for _, dep := range d.Deploys {
			if dep.Environment != nil {
				revision[releaseKey(dep.App, *dep.Environment, dep.At)] = dep.Revision
			}
		}
	}
	// An App's first deploy within FirstDeployGrace of Argo CD's first event
	// is no known release (the team's frequency counts it a first deploy):
	// counted in no repo's releases, though its last release was then or
	// before, as the release state reads it.
	argoStart := ds.Meta.DataStart.ArgoCD
	firstSeen := map[string]bool{}
	for _, r := range ds.Releases {
		if r.First && argoStart != nil && r.At.Sub(*argoStart) <= dataset.FirstDeployGrace {
			firstSeen[releaseKey(r.App, r.Environment, r.At)] = true
		}
	}
	by := map[string]*repoAcc{}
	get := func(pr dataset.PR) *repoAcc {
		r := by[pr.Repo]
		if r == nil {
			r = &repoAcc{row: RepoRow{Repo: pr.Repo, Project: pr.Project, Flags: []RepoFlag{}},
				releases: map[string]bool{}, prevRels: map[string]bool{}, weeks: map[int]map[string]bool{},
				mergedWeek: make([]int, len(weeks))}
			by[pr.Repo] = r
		}
		return r
	}
	humans, prevHumans := mergedPRs(ds, p.Period, f), mergedPRs(prev, p.Previous, f)
	for i, set := range [][]dataset.PR{prevHumans, humans} {
		cur := i == 1
		for _, pr := range set {
			r := get(pr)
			if pr.Library {
				r.library = true
			} else {
				r.work = append(r.work, *pr.MergedAt)
			}
			if w := trendWeekIndex(weeks, *pr.MergedAt); w >= 0 {
				r.mergedWeek[w]++
			}
			lines, _, sized := prSize(pr)
			if cur {
				r.row.Merged++
				if sized {
					r.sizes = append(r.sizes, lines)
				}
				if w, ok := pickupDays(pr, loc); ok {
					r.pickups = append(r.pickups, w)
				}
			} else {
				r.prevMerged++
				if sized {
					r.prevSizes = append(r.prevSizes, lines)
				}
			}
			cutoff := p.Period.To
			if !cur {
				cutoff = p.Previous.To
			}
			if !toProdCandidate(pr, traces, infra) {
				continue
			}
			if e := firstDeployBefore(pr, prod, cutoff); e != nil {
				if cur {
					r.waits = append(r.waits, toProdDays(pr, e.FirstDeployedAt))
				} else {
					r.prevWaits = append(r.prevWaits, toProdDays(pr, e.FirstDeployedAt))
				}
			}
		}
	}
	notYet := map[string]int{}
	for _, c := range m.Coverage.Repos {
		notYet[c.Repo] = c.NotYet
	}
	argo, bitbucket := ds.Meta.DataStart.ArgoCD, ds.Meta.DataStart.Bitbucket
	// A previous value only over a previous period its sources saw whole,
	// as the comparison holds (ReasonIncomplete): a part of it reads low.
	whole := func(starts ...*time.Time) bool {
		return !slices.ContainsFunc(starts, func(s *time.Time) bool { return s == nil || s.After(p.Previous.From) })
	}
	prevRel, prevMerged := whole(argo), whole(bitbucket)
	prevBatch, prevProd := whole(bitbucket, ds.Meta.DataStart.Noergler), whole(argo, bitbucket)
	lost := lostOf(ds, prev)
	recent := end.AddDate(0, 0, -th.ReleaseBreachDays)
	out := RepoView{Rows: []RepoRow{}, Groups: []RepoGroup{}, HintDays: th.ReleaseHintDays,
		BreachDays: th.ReleaseBreachDays}
	for _, r := range by {
		row := &r.row
		row.NotYet = notYet[row.Repo]
		// Releases that may have shipped it, as tracing knew them by the
		// period's end.
		var last *time.Time
		rt := traces[row.Repo]
		for _, rel := range rt.Releases {
			if rel.Environment != prod || rel.Unchanged || !rel.At.Before(end) || !rel.Known.Before(end) {
				continue
			}
			if last == nil || rel.At.After(*last) {
				last = ptr(rel.At)
			}
			key := releaseKey(rel.App, rel.Environment, rel.At)
			if firstSeen[key] {
				continue
			}
			rev := revision[key]
			if rev == "" {
				rev = rel.At.UTC().Format(time.RFC3339Nano)
			}
			switch {
			case p.Period.In(&rel.At):
				r.releases[rev] = true
			case p.Previous.In(&rel.At):
				r.prevRels[rev] = true
			}
			if w := trendWeekIndex(weeks, rel.At); w >= 0 {
				if r.weeks[w] == nil {
					r.weeks[w] = map[string]bool{}
				}
				r.weeks[w][rev] = true
			}
		}
		row.LastRelease = last
		switch {
		case last != nil:
			row.DaysSince = ptr(calendarDays(*last, end))
		case argo != nil && argo.Before(end):
			row.DaysSince, row.SinceLowerBound = ptr(calendarDays(*argo, end)), true
		}
		row.Releases = len(r.releases)
		row.ReleasesPerWeek = perWeek(row.Releases, p.Period, argo)
		if prevRel {
			row.ReleasesPrev = perWeek(len(r.prevRels), p.Previous, argo)
		}
		row.ReleaseWeeks = make([]*int, len(weeks))
		row.MergedWeeks = make([]*int, len(weeks))
		for i, w := range weeks {
			if weekGap(w.Start, w.End, argo, nil, loc) == "" {
				row.ReleaseWeeks[i] = ptr(len(r.weeks[i]))
			}
			if weekGap(w.Start, w.End, bitbucket, lost, loc) == "" {
				row.MergedWeeks[i] = ptr(r.mergedWeek[i])
			}
		}
		row.MergedPerWeek = perWeek(row.Merged, p.Period, bitbucket)
		if prevMerged {
			row.MergedPrev = perWeek(r.prevMerged, p.Previous, bitbucket)
		}
		row.Sized = len(r.sizes)
		if len(r.sizes) >= RepoMinCases {
			row.Batch = Median(r.sizes)
		}
		if len(r.prevSizes) >= RepoMinCases && prevBatch {
			row.BatchPrev = Median(r.prevSizes)
		}
		row.ToProdN = len(r.waits)
		if v, judged, fires := typicalToProd(r.waits, th); judged {
			row.ToProd = ptr(v)
			if fires {
				row.Flags = append(row.Flags, RepoFlag{Key: StateSlowToProd, Tone: DotBreach, Value: v})
			}
		}
		if v, judged, _ := typicalToProd(r.prevWaits, th); judged && prevProd {
			row.ToProdPrev = ptr(v)
		}
		row.PickupN = len(r.pickups)
		if v, judged, fires := typicalPickup(r.pickups, th); judged {
			row.Pickup = ptr(v)
			if fires {
				row.Flags = append(row.Flags, RepoFlag{Key: StateFirstReviewWait, Tone: DotHint, Value: v})
			}
		}
		// Anyone working on it: work since its last release, or within the
		// breach span, as the release rule counts a project active.
		active := slices.ContainsFunc(r.work, func(t time.Time) bool {
			return t.Before(end) && ((last != nil && t.After(*last)) || !t.Before(recent))
		})
		switch {
		case r.library:
			// Never rolled out: no release to be late with.
			row.Group, row.DaysSince, row.SinceLowerBound = RepoGroupLibrary, nil, false
		case !active:
			row.Group = RepoGroupIdle
		case argo == nil || !argo.Before(end):
			row.Group, row.Reason = RepoGroupUnjudged, ptr(RepoReasonNotReporting)
		case infra[row.Repo]:
			row.Group, row.Reason = RepoGroupUnjudged, ptr(RepoReasonInfra)
		case rt.DeployedSince == nil || !rt.DeployedSince.Before(end):
			// No App known to deploy it: no release of its to count either.
			row.Group, row.Reason, row.DaysSince, row.SinceLowerBound = RepoGroupUnjudged, ptr(RepoReasonUntraced), nil, false
		default:
			switch releaseStanding(*row.DaysSince, th) {
			case releaseBreach:
				row.Group = RepoGroupNone
				row.Flags = append([]RepoFlag{{Key: StateNoRelease, Tone: DotBreach, Value: *row.DaysSince}}, row.Flags...)
			case releaseDue:
				row.Group = RepoGroupRare
				row.Flags = append([]RepoFlag{{Key: StateReleaseDue, Tone: DotHint, Value: *row.DaysSince}}, row.Flags...)
			default:
				row.Group = RepoGroupRegular
			}
		}
		out.Rows = append(out.Rows, *row)
	}
	slices.SortFunc(out.Rows, compareRepoRows)
	out.Open = len(out.Rows)
	if len(out.Rows)-RepoRankOpen >= RepoRankMinHidden {
		out.Open = RepoRankOpen
	}
	for _, g := range repoGroups {
		grp := RepoGroup{Key: g, Hidden: []string{}}
		for i, row := range out.Rows {
			if row.Group != g {
				continue
			}
			grp.Count++
			if i < out.Open {
				continue
			}
			grp.HiddenCount++
			if len(grp.Hidden) < RepoHiddenNamed {
				grp.Hidden = append(grp.Hidden, row.Repo)
			} else {
				grp.More++
			}
		}
		if grp.Count > 0 {
			out.Groups = append(out.Groups, grp)
		}
	}
	out.Spreads = repoSpreads(out.Rows, m, th)
	return out
}

// releaseKey names a deploy of an App to an environment at an instant.
func releaseKey(app, env string, at time.Time) string {
	return app + "\x00" + env + "\x00" + at.UTC().Format(time.RFC3339Nano)
}

// compareRepoRows ranks the rows (RepoView.Rows).
func compareRepoRows(a, b RepoRow) int {
	g := func(r RepoRow) int { return slices.Index(repoGroups, r.Group) }
	val := func(v *float64) float64 {
		if v == nil {
			return -1
		}
		return *v
	}
	breach := func(r RepoRow) int {
		if slices.ContainsFunc(r.Flags, func(f RepoFlag) bool { return f.Tone == DotBreach }) {
			return 0
		}
		return 1
	}
	c := cmp.Compare(g(a), g(b))
	if c != 0 {
		return c
	}
	switch a.Group {
	case RepoGroupNone, RepoGroupRare:
		c = cmp.Compare(val(b.DaysSince), val(a.DaysSince))
	case RepoGroupRegular:
		c = cmp.Or(cmp.Compare(breach(a), breach(b)), cmp.Compare(val(b.ToProd), val(a.ToProd)))
	}
	return cmp.Or(c, cmp.Compare(a.Repo, b.Repo))
}

// spreadDot is a repo's value on a strip before its position.
type spreadDot struct {
	repo, project string
	value         *float64
	n             int
	verdict       string
	apart         bool
}

// repoSpreads are the strips of the rows and of the build section's repos
// per kind (KindHotspots.all, as the build states read them).
func repoSpreads(rows []RepoRow, m Metrics, th config.Thresholds) []RepoSpread {
	var rel, prod, merged, batch, pickup []spreadDot
	for _, r := range rows {
		dot := func(v *float64, n int, verdict string) spreadDot {
			return spreadDot{repo: r.Repo, project: r.Project, value: v, n: n, verdict: verdict}
		}
		switch r.Group {
		case RepoGroupNone, RepoGroupRare, RepoGroupRegular:
			verdict := DotWithin
			switch r.Group {
			case RepoGroupNone:
				verdict = DotBreach
			case RepoGroupRare:
				verdict = DotHint
			}
			if r.ReleasesPerWeek != nil {
				d := dot(r.ReleasesPerWeek, r.Releases, verdict)
				// Set apart only when nothing was rolled out: a repo whose one
				// deploy in the period was an App's first is no such repo.
				d.apart = r.Group == RepoGroupNone && r.Releases == 0
				rel = append(rel, d)
			}
		}
		switch {
		case r.ToProd != nil:
			verdict := DotWithin
			if *r.ToProd > float64(th.NotOnProdDays) {
				verdict = DotBreach
			}
			prod = append(prod, dot(r.ToProd, r.ToProdN, verdict))
		case r.ToProdN > 0:
			prod = append(prod, dot(nil, r.ToProdN, DotThin))
		case r.Group == RepoGroupNone:
			// Nothing on production: its work lies there, no wait ended.
			d := dot(nil, 0, DotBreach)
			d.apart = true
			prod = append(prod, d)
		}
		if r.Merged > 0 && r.MergedPerWeek != nil {
			merged = append(merged, dot(r.MergedPerWeek, r.Merged, DotNeutral))
		}
		if r.Sized > 0 {
			verdict := DotNeutral
			if r.Batch == nil {
				verdict = DotThin
			}
			batch = append(batch, dot(r.Batch, r.Sized, verdict))
		}
		if r.PickupN > 0 {
			verdict := DotThin
			if r.Pickup != nil {
				verdict = DotWithin
				if *r.Pickup > th.FirstReviewDays {
					verdict = DotHint
				}
			}
			pickup = append(pickup, dot(r.Pickup, r.PickupN, verdict))
		}
	}
	freq, tp := m.Delivery.Frequency, m.Throughput
	var freqTeam *float64
	if freq.Syncs > 0 {
		freqTeam = ptr(freq.PerWeek)
	}
	out := []RepoSpread{
		newSpread(SpreadReleases, nil, UnitPerWeek, true, rel, nil, freqTeam, true),
		newSpread(SpreadToProd, nil, UnitDays, false, prod,
			&StripMark{Key: "not_on_prod_days", Value: float64(th.NotOnProdDays)}, nil, false),
		newSpread(SpreadMerged, nil, UnitPerWeek, true, merged, nil, tp.MergedPerWeek, true),
		newSpread(SpreadBatch, nil, UnitLines, false, batch, nil, tp.BatchSize.P50, false),
		newSpread(SpreadPickup, nil, UnitWorkingDays, false, pickup,
			&StripMark{Key: "first_review_days", Value: th.FirstReviewDays}, nil, false),
	}
	for _, k := range m.Build.Hotspots.Kinds {
		var dur, succ []spreadDot
		for _, h := range k.all {
			dot := func(v *float64, n int, verdict string) spreadDot {
				return spreadDot{repo: h.Repo, project: projectOf(h.Repo), value: v, n: n, verdict: verdict}
			}
			if h.Duration.P50 != nil {
				verdict := DotThin
				switch {
				case k.Kind == KindFeature || k.Kind == KindMain:
					if _, judged, fires := longBuild(h, th); judged {
						verdict = DotWithin
						if fires {
							verdict = DotHint
						}
					}
				case h.Duration.N >= th.BuildMinRuns:
					verdict = DotNeutral
				}
				dur = append(dur, dot(h.Duration.P50, h.Duration.N, verdict))
			}
			if h.FailureRate != nil {
				verdict := DotThin
				switch {
				case k.Kind == KindRelease:
					if judged, fires := releaseBuildsFailing(h, th); judged {
						verdict = DotWithin
						if fires {
							verdict = DotHint
						}
					}
				case h.Runs >= HotspotMinRuns:
					verdict = DotNeutral
				}
				succ = append(succ, dot(ptr(1-*h.FailureRate), h.Runs, verdict))
			}
		}
		var durMark, succMark *StripMark
		if k.Kind == KindFeature || k.Kind == KindMain {
			durMark = &StripMark{Key: "build_minutes", Value: th.BuildMinutes * 60}
		}
		if k.Kind == KindRelease {
			succMark = &StripMark{Key: "release_builds_share", Value: 1 - th.ReleaseBuildsShare}
		}
		var success *float64
		if k.FailureRate != nil {
			success = ptr(1 - *k.FailureRate)
		}
		out = append(out,
			newSpread(SpreadBuildDuration, ptr(k.Kind), UnitSeconds, false, dur, durMark, k.Duration.P50, false),
			newSpread(SpreadBuildSuccess, ptr(k.Kind), UnitShare, true, succ, succMark, success, false))
	}
	return out
}

// newSpread is the strip of dots: sorted the better end first, positioned
// on a scale fitted to them and the mark, the typical repo among the
// judged ones, and the note on the spread; additive: the values add up to
// the team's count (releases, merged PRs), so one repo can dominate it.
func newSpread(key string, kind *string, unit string, higher bool, dots []spreadDot, mark *StripMark,
	team *float64, additive bool) RepoSpread {
	out := RepoSpread{Key: key, Kind: kind, Unit: unit, HigherBetter: higher, Dots: []RepoDot{}, Team: team}
	// A repo with too few cases is counted, never placed: a dot would read
	// as a value like any other.
	var placed []spreadDot
	var values []float64
	for _, d := range dots {
		if d.verdict == DotThin {
			out.Thin++
			continue
		}
		placed = append(placed, d)
		if d.value != nil && !d.apart {
			values = append(values, *d.value)
		}
	}
	dots = placed
	judged := values
	var markV *float64
	if mark != nil {
		markV = &mark.Value
	}
	out.Scale = stripScale(values, markV, unit, higher)
	pos := func(v float64) (float64, bool) { return out.Scale.position(v, higher) }
	if mark != nil {
		mark.X, _ = pos(mark.Value)
		out.Mark = mark
	}
	out.Typical = Median(judged)
	if out.Typical != nil {
		x, _ := pos(*out.Typical)
		out.TypicalX = &x
	}
	slices.SortStableFunc(dots, func(a, b spreadDot) int {
		val := func(d spreadDot) float64 {
			if d.value == nil {
				return 0
			}
			if higher {
				return -*d.value
			}
			return *d.value
		}
		return cmp.Or(cmp.Compare(boolInt(a.apart || a.value == nil), boolInt(b.apart || b.value == nil)),
			cmp.Compare(val(a), val(b)), cmp.Compare(a.repo, b.repo))
	})
	for _, d := range dots {
		rd := RepoDot{Repo: d.repo, Project: d.project, Value: d.value, N: d.n, Verdict: d.verdict, Apart: d.apart}
		switch {
		case d.apart:
			rd.Value = nil
			out.Apart++
		case d.value != nil:
			x, clamped := pos(*d.value)
			rd.X, rd.Clamped = &x, clamped
			out.Judged++
		default:
			continue
		}
		out.Dots = append(out.Dots, rd)
	}
	// A strip whose dots all sit on one spot says nothing a line cannot,
	// and one of repos set apart only has no axis to read.
	distinct := map[float64]bool{}
	for _, v := range values {
		distinct[v] = true
	}
	out.Shown = len(distinct) >= 2
	if len(distinct) == 1 && len(values) >= 2 {
		out.Same = ptr(values[0])
	}
	out.Note = spreadNote(dots, out.Typical, team, additive)
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// spreadNote is what the team's number (team) hides, if anything: one repo
// carrying SpreadDominantShare of an additive count, else repos far from
// the typical one, else the team's number itself far from it. Nil with
// fewer than SpreadMinRepos judged repos, or nothing to say.
func spreadNote(dots []spreadDot, typical, team *float64, additive bool) *SpreadNote {
	var vals []spreadDot
	for _, d := range dots {
		if d.value != nil && !d.apart && d.verdict != DotThin {
			vals = append(vals, d)
		}
	}
	if len(vals) < SpreadMinRepos || typical == nil {
		return nil
	}
	if additive {
		sum, top := 0, vals[0]
		for _, d := range vals {
			sum += d.n
			if d.n > top.n {
				top = d
			}
		}
		for _, d := range dots {
			if d.apart {
				sum += d.n
			}
		}
		if share := float64(top.n) / float64(max(sum, 1)); sum > 0 && share >= SpreadDominantShare {
			return &SpreadNote{Kind: NoteDominant, Repo: ptr(top.repo), Share: ptr(share), Of: len(vals),
				Additive: true}
		}
	}
	if *typical <= 0 {
		return nil
	}
	n := SpreadNote{Kind: NoteFar, Of: len(vals), Additive: additive}
	for _, d := range vals {
		switch {
		case *d.value > *typical*SpreadFarFactor:
			n.Above++
		case *d.value < *typical/SpreadFarFactor:
			n.Below++
		default:
			n.Near++
		}
	}
	switch {
	case n.Above+n.Below > 0:
		return &n
	case team != nil && (*team >= *typical*SpreadFarFactor || *team <= *typical/SpreadFarFactor):
		n.Kind = NotePooled
		return &n
	}
	return nil
}
