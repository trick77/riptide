package metrics

import (
	"slices"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// ---- Approved work ---------------------------------------------------------

// approvals are the people's approved PRs of the view (dataset.Approvals):
// a bot's PR is out, a library's in (its merge is work like any other).
func (a *assessor) approvals() []dataset.Approval {
	var out []dataset.Approval
	for _, ap := range a.ds.Approvals {
		if !ap.Bot && len(ap.Spans) > 0 && a.f.inKey(ap.Project) {
			out = append(out, ap)
		}
	}
	return out
}

// lostSilences are the lost Bitbucket silences of every window read, each
// once: spans whose merges and closes riptide never heard of.
func (a *assessor) lostSilences() []dataset.Silence {
	var out []dataset.Silence
	for _, ds := range []*dataset.Dataset{a.ds, a.prev, a.earlier} {
		if ds == nil {
			continue
		}
		for _, s := range ds.Meta.Silences {
			if s.Lost && !slices.ContainsFunc(out, func(o dataset.Silence) bool { return o.From.Equal(s.From) && o.To.Equal(s.To) }) {
				out = append(out, s)
			}
		}
	}
	return out
}

// lostBetween reports whether a lost Bitbucket silence overlaps (from, to):
// a merge or close in it riptide never heard of.
func lostBetween(lost []dataset.Silence, from, to time.Time) bool {
	return slices.ContainsFunc(lost, func(s dataset.Silence) bool { return s.To.After(from) && s.From.Before(to) })
}

// approvalAt is ap's approval at t: since when the approval of a person
// standing then stood (one withdrawn, set to needs work by its approver or
// reset by a push stands no more; given again, it stands from then), nil
// when none stood or the PR was merged or closed by then; and whether a
// person's needs work, any reviewer's, held the PR then: approved, but not
// ready to merge.
func approvalAt(ap dataset.Approval, t time.Time) (since *time.Time, needsWork bool) {
	if ap.ClosedBy(t) {
		return nil, false
	}
	if since = ap.ApprovedSince(t); since == nil {
		return nil, false
	}
	return since, ap.NeedsWorkAt(t)
}

// approvedWaitingState judges each repo at the instant: approved PRs (a
// person's approval, not the author's, standing then) waiting for their
// merge longer than ApprovedWaitDays working days, at least
// ApprovedWaitingPRs of them. An approved PR stays counted while it is open
// and its approval stands, touched or not: the forgotten one is what the
// state is for. A PR whose merge may lie in a lost Bitbucket silence since
// its approval is of unknown state: it counts for nothing, and makes its
// repo unjudged where it could have decided it. One a reviewer's needs work
// holds is no PR waiting for its merge, nor done: where it could have
// decided the repo, the repo is not judged at all (idle: no "gelöst"). The
// state holds since the ApprovedWaitingPRs-th of the PRs waiting had waited
// past the threshold. A lasting state: a hint until a unit has held it two
// sprints.
func (a *assessor) approvedWaitingState(at stateAt) []stateEval {
	th, end, loc := a.states.th, at.end, a.states.loc
	lost := a.lostSilences()
	type acc struct {
		project   string
		n         int
		unknown   int
		held      int
		wait      float64
		crossings []time.Time
	}
	by := map[string]*acc{}
	for repo := range a.activeRepos() {
		by[repo] = &acc{project: projectOf(repo)}
	}
	for _, ap := range a.approvals() {
		since, needsWork := approvalAt(ap, end)
		if since == nil {
			continue
		}
		w := workingDays(*since, end, loc)
		r := by[ap.Repo]
		if r == nil {
			r = &acc{project: ap.Project}
			by[ap.Repo] = r
		}
		switch {
		case w <= th.ApprovedWaitDays:
			continue
		case lostBetween(lost, *since, end):
			r.unknown++
			continue
		case needsWork:
			r.held++
			continue
		}
		r.n++
		r.wait = max(r.wait, w)
		r.crossings = append(r.crossings, afterWorkingDays(*since, th.ApprovedWaitDays, loc))
	}
	e := stateEval{key: StateApprovedWaiting, unit: UnitRepo, source: dataset.SourceBitbucket, tone: ToneHint,
		facts: "prs", tier: tierLasting, lasting: true,
		threshold: StateThreshold{Key: "approved_wait_days", Value: th.ApprovedWaitDays, Unit: ThresholdWorkingDays,
			MinKey: ptr("approved_waiting_prs"), Min: ptr(th.ApprovedWaitingPRs)}}
	for _, repo := range slices.Sorted(mapsKeys(by)) {
		r := by[repo]
		u := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(r.project), Value: round1(r.wait), Count: ptr(r.n),
			Names: []string{}}, status: unitClear}
		switch {
		case r.n >= th.ApprovedWaitingPRs:
			u.status = unitFires
			u.began = nthBegan(r.crossings, th.ApprovedWaitingPRs)
			u.Since = u.began
		case r.n+r.unknown >= th.ApprovedWaitingPRs:
			// The PRs of unknown state could make it: no telling.
			u.status = unitUnjudged
		case r.n+r.unknown+r.held >= th.ApprovedWaitingPRs:
			// Held by needs work: not waiting for the merge, not done either.
			u.status = unitIdle
		}
		e.units = append(e.units, u)
	}
	return []stateEval{e}
}

// projectOf is a repo's Bitbucket project: its full name's first part.
func projectOf(repo string) string {
	for i := range len(repo) {
		if repo[i] == '/' {
			return repo[:i]
		}
	}
	return repo
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// activeRepos are the period's repos with people's work (activeRepos),
// computed once.
func (a *assessor) activeRepos() map[string]bool {
	if a.active == nil {
		a.active, _ = activeRepos(a.ds, a.p.Period, a.m, a.f)
	}
	return a.active
}

// ---- Merge pause -----------------------------------------------------------

// minBaselineWeeks is the fewest covered weeks a project's typical week
// needs: fewer, and its week cannot be judged a pause.
const minBaselineWeeks = 3

// mergePauseState judges each project's weeks of the instant's period: a
// week whose people's merges fell to MergePauseShare of the project's
// typical week (the median of the MergePauseWeeks weeks before it Bitbucket
// covered, at least MergePauseMinTypical) while at the week's end
// MergePauseWaiting PRs or more were open with a person's approval standing
// (approvalAt), however long they had waited. Not "no merge": one hotfix
// let through a freeze would hide it; not one or two approved PRs: a
// holiday week. Not an approval a reviewer's needs work holds (not ready to
// merge), nor one on a PR untouched for OpenInactiveDays by then (given up
// long ago), nor one whose merge may lie in a lost silence since it. A lost
// Bitbucket silence takes its working time out of the week (its merges are
// not there, nor can the typical week expect them): the unit's Typical is
// what is left, Lost says so, and a week left expecting fewer than
// MergePauseMinTypical is not judged; a baseline week it
// covers by more than a fifth, or one before the look-back, is no baseline.
// Read as of the week's end: a closed week reads the same whenever it is
// read. The latest pause of a project is its unit, an episode; one in the
// period's last week is held up now, over once the week after merges again.
func (a *assessor) mergePauseState(at stateAt) []stateEval {
	th, loc := a.states.th, a.states.loc
	lost := a.lostSilences()
	bb := a.ds.Meta.DataStart.Bitbucket
	merges := map[string][]time.Time{}
	for _, pr := range a.states.prs {
		if notCounted(pr) == "" {
			merges[pr.Project] = append(merges[pr.Project], *pr.MergedAt)
		}
	}
	waits := map[string][]dataset.Approval{}
	for _, ap := range a.approvals() {
		waits[ap.Project] = append(waits[ap.Project], ap)
	}
	// covered is the share of the week's working time Bitbucket reported
	// and the look-back holds, and whether a lost silence took working time
	// out of it.
	covered := func(start, end time.Time) (float64, bool) {
		if bb == nil || !bb.Before(end) || start.Before(a.states.from) {
			return 0, false
		}
		from := start
		if bb.After(from) {
			from = *bb
		}
		all := workingDays(start, end, loc)
		if all <= 0 {
			return 0, false
		}
		in, gap := workingDays(from, end, loc), 0.0
		for _, s := range lost {
			f, t := s.From, s.To
			if f.Before(from) {
				f = from
			}
			if t.After(end) {
				t = end
			}
			gap += workingDays(f, t, loc)
		}
		return max(in-gap, 0) / all, gap > 0
	}
	count := func(ts []time.Time, start, end time.Time) int {
		n := 0
		for _, t := range ts {
			if !t.Before(start) && t.Before(end) {
				n++
			}
		}
		return n
	}
	projects := map[string]bool{}
	for p := range merges {
		projects[p] = true
	}
	for p := range waits {
		projects[p] = true
	}
	last := len(at.period.Weeks) - 1
	e := stateEval{key: StateMergePause, unit: UnitProject, source: dataset.SourceBitbucket, tone: ToneAttention,
		facts: "prs", tier: tierLasting, valueUnit: UnitCount, episodic: true,
		threshold: StateThreshold{Key: "merge_pause_share", Value: th.MergePauseShare, Unit: ThresholdShare,
			MinKey: ptr("merge_pause_waiting"), Min: ptr(th.MergePauseWaiting)}}
	for _, p := range slices.Sorted(mapsKeys(projects)) {
		if !a.f.inKey(p) {
			continue
		}
		// A project's PRs are its repos' ("<project>/<repo>").
		u := stateUnit{StateUnit: StateUnit{Name: p, Project: ptr(p), Names: []string{}, Query: ptr(p + "/")},
			status: unitIdle}
		unjudged := false
		for i, w := range at.period.Weeks {
			start, end := w.Start.In(loc), w.End.In(loc)
			var base []float64
			for k := 1; k <= th.MergePauseWeeks; k++ {
				bs, be := start.AddDate(0, 0, -7*k), start.AddDate(0, 0, -7*(k-1))
				if c, _ := covered(bs, be); c >= 0.8 {
					base = append(base, float64(count(merges[p], bs, be)))
				}
			}
			f, gap := covered(start, end)
			if len(base) < minBaselineWeeks || f < 0.2 {
				unjudged = true
				if i == last {
					// Whether a pause before it went on, the last week cannot tell.
					u.after = afterUnknown
				}
				continue
			}
			typical := *Median(base)
			if typical < th.MergePauseMinTypical {
				if i == last {
					u.after = afterQuiet
				}
				continue
			}
			// What the week could hold of its typical merges outside a lost
			// silence: too little left to expect, and merges riptide never
			// heard of could make the pause, so the week is not judged.
			expect := typical * f
			if expect < th.MergePauseMinTypical {
				unjudged = true
				if i == last {
					u.after = afterUnknown
				}
				continue
			}
			if u.status == unitIdle {
				u.status = unitClear
			}
			n := count(merges[p], start, end)
			if float64(n) > th.MergePauseShare*expect {
				continue
			}
			var held []string
			waiting := 0
			for _, ap := range waits[p] {
				since, needsWork := approvalAt(ap, end)
				if since == nil || needsWork || lostBetween(lost, *since, end) {
					continue
				}
				if touched := ap.LastActivity(end); touched == nil || end.Sub(*touched) >= OpenInactiveDays*24*time.Hour {
					continue
				}
				waiting++
				if !slices.Contains(held, ap.Repo) {
					held = append(held, ap.Repo)
				}
			}
			if waiting < th.MergePauseWaiting {
				continue
			}
			slices.Sort(held)
			week := w
			u.status, u.Value, u.Count, u.Typical = unitFires, float64(waiting), ptr(n), ptr(round1(expect))
			u.Since, u.Week, u.Names, u.Lost = ptr(w.Start), &week, held, gap
			u.ongoing = i == last
			if i == last {
				e.tier = tierNow
			}
		}
		if u.status == unitIdle && unjudged {
			u.status = unitUnjudged
		}
		e.units = append(e.units, u)
	}
	return []stateEval{e}
}
