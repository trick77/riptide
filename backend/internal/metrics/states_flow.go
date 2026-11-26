package metrics

import (
	"slices"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// ---- Approved work ---------------------------------------------------------

// waiting reports whether an approved PR was still waiting at t: approved
// by a person before it, neither merged nor closed by then.
func waitingAt(ap dataset.Approval, t time.Time) bool {
	return ap.ApprovedAt != nil && ap.ApprovedAt.Before(t) && (ap.MergedAt == nil || !ap.MergedAt.Before(t)) &&
		(ap.ClosedAt == nil || !ap.ClosedAt.Before(t))
}

// approvals are the people's approved PRs of the view (dataset.Approvals):
// a bot's PR is out, a library's in (its merge is work like any other).
func (a *assessor) approvals() []dataset.Approval {
	var out []dataset.Approval
	for _, ap := range a.ds.Approvals {
		if !ap.Bot && ap.ApprovedAt != nil && a.f.inKey(ap.Project) {
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

// approvedWaitingState judges each repo at the period's end: approved PRs
// (a person's approval, not the author's) waiting for their merge longer
// than ApprovedWaitDays working days, at least ApprovedWaitingPRs of them.
// A PR whose merge may lie in a lost Bitbucket silence after its approval
// is not counted: its state is unknown. A lasting state: a hint until a
// unit has held it two sprints.
func (a *assessor) approvedWaitingState() {
	th, end, loc := a.states.th, a.p.Period.To, a.states.loc
	lost := a.lostSilences()
	type acc struct {
		project string
		n       int
		oldest  *time.Time
		wait    float64
	}
	by := map[string]*acc{}
	for repo := range a.activeRepos() {
		by[repo] = &acc{project: projectOf(repo)}
	}
	for _, ap := range a.approvals() {
		if !waitingAt(ap, end) {
			continue
		}
		unknown := slices.ContainsFunc(lost, func(s dataset.Silence) bool {
			return s.From.After(*ap.ApprovedAt) && s.From.Before(end)
		})
		w := workingDays(*ap.ApprovedAt, end, loc)
		r := by[ap.Repo]
		if r == nil {
			r = &acc{project: ap.Project}
			by[ap.Repo] = r
		}
		if unknown || w <= th.ApprovedWaitDays {
			continue
		}
		r.n++
		if r.oldest == nil || ap.ApprovedAt.Before(*r.oldest) {
			r.oldest, r.wait = ap.ApprovedAt, w
		}
	}
	e := stateEval{key: StateApprovedWaiting, unit: UnitRepo, source: dataset.SourceBitbucket, tone: ToneHint,
		facts: "prs", tier: tierLasting, lasting: true,
		threshold: StateThreshold{Key: "approved_wait_days", Value: th.ApprovedWaitDays, Unit: ThresholdWorkingDays,
			MinKey: ptr("approved_waiting_prs"), Min: ptr(th.ApprovedWaitingPRs)}}
	for _, repo := range slices.Sorted(mapsKeys(by)) {
		r := by[repo]
		u := stateUnit{StateUnit: StateUnit{Name: repo, Project: ptr(r.project), Value: round1(r.wait), Count: ptr(r.n),
			Names: []string{}}, status: unitClear}
		if r.oldest != nil {
			u.Since = ptr(*r.oldest)
		}
		if r.n >= th.ApprovedWaitingPRs {
			u.status = unitFires
		}
		e.units = append(e.units, u)
	}
	a.judge(e)
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

// activeRepos are the period's repos with people's work (activeRepos).
func (a *assessor) activeRepos() map[string]bool {
	act, _ := activeRepos(a.ds, a.p.Period, a.m, a.f)
	return act
}

// ---- Merge pause -----------------------------------------------------------

// minBaselineWeeks is the fewest covered weeks a project's typical week
// needs: fewer, and its week cannot be judged a pause.
const minBaselineWeeks = 3

// mergePauseState judges each project's weeks of the period: a week whose
// people's merges fell to MergePauseShare of the project's typical week
// (the median of the MergePauseWeeks weeks before it Bitbucket covered,
// at least MergePauseMinTypical) while MergePauseWaiting approved PRs
// waited ApprovedWaitDays working days or longer. Not "no merge": one
// hotfix let through a freeze would hide it; not one or two approved PRs:
// a holiday week. A lost Bitbucket silence takes its working time out of
// the week (its merges are not there, nor can the typical week expect
// them); a baseline week it covers by more than a fifth is no baseline. A
// PR's wait counts to its merge or close, or to the read's now: how long
// an approved PR of the week waited is known only after it. The latest
// pause of a project is its unit; one in the period's last week is held up
// now.
func (a *assessor) mergePauseState() {
	th, loc := a.states.th, a.states.loc
	now := a.ds.Meta.GeneratedAt
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
	// covered is the share of the week's working time Bitbucket reported.
	covered := func(start, end time.Time) float64 {
		if bb == nil || !bb.Before(end) {
			return 0
		}
		from := start
		if bb.After(from) {
			from = *bb
		}
		all := workingDays(start, end, loc)
		if all <= 0 {
			return 0
		}
		in := workingDays(from, end, loc)
		for _, s := range lost {
			f, t := s.From, s.To
			if f.Before(from) {
				f = from
			}
			if t.After(end) {
				t = end
			}
			in -= workingDays(f, t, loc)
		}
		return max(in, 0) / all
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
	last := len(a.p.Period.Weeks) - 1
	e := stateEval{key: StateMergePause, unit: UnitProject, source: dataset.SourceBitbucket, tone: ToneAttention,
		facts: "prs", tier: tierLasting, valueUnit: UnitCount,
		threshold: StateThreshold{Key: "merge_pause_share", Value: th.MergePauseShare, Unit: ThresholdShare,
			MinKey: ptr("merge_pause_waiting"), Min: ptr(th.MergePauseWaiting)}}
	for _, p := range slices.Sorted(mapsKeys(projects)) {
		if !a.f.inKey(p) {
			continue
		}
		u := stateUnit{StateUnit: StateUnit{Name: p, Project: ptr(p), Names: []string{}}, status: unitIdle}
		unjudged := false
		for i, w := range a.p.Period.Weeks {
			start, end := w.Start.In(loc), w.End.In(loc)
			var base []float64
			for k := 1; k <= th.MergePauseWeeks; k++ {
				bs, be := start.AddDate(0, 0, -7*k), start.AddDate(0, 0, -7*(k-1))
				if covered(bs, be) >= 0.8 {
					base = append(base, float64(count(merges[p], bs, be)))
				}
			}
			f := covered(start, end)
			if len(base) < minBaselineWeeks || f < 0.2 {
				unjudged = true
				continue
			}
			typical := *Median(base)
			if typical < th.MergePauseMinTypical {
				continue
			}
			if u.status == unitIdle {
				u.status = unitClear
			}
			n := count(merges[p], start, end)
			if float64(n) > th.MergePauseShare*typical*f {
				continue
			}
			var held []string
			waiting := 0
			for _, ap := range waits[p] {
				// Approved before the week ended, not merged or closed before it
				// began: waiting in it.
				if !ap.ApprovedAt.Before(end) || (ap.MergedAt != nil && ap.MergedAt.Before(start)) ||
					(ap.ClosedAt != nil && ap.ClosedAt.Before(start)) {
					continue
				}
				until := now
				for _, t := range []*time.Time{ap.MergedAt, ap.ClosedAt} {
					if t != nil && t.Before(until) {
						until = *t
					}
				}
				if workingDays(*ap.ApprovedAt, until, loc) >= th.ApprovedWaitDays {
					waiting++
					if !slices.Contains(held, ap.Repo) {
						held = append(held, ap.Repo)
					}
				}
			}
			if waiting < th.MergePauseWaiting {
				continue
			}
			slices.Sort(held)
			week := w
			u.status, u.Value, u.Count, u.Typical = unitFires, float64(waiting), ptr(n), ptr(typical)
			u.Since, u.Week, u.Names = ptr(w.Start), &week, held
			if i == last {
				e.tier = tierNow
			}
		}
		if u.status == unitIdle && unjudged {
			u.status = unitUnjudged
		}
		e.units = append(e.units, u)
	}
	a.judge(e)
}
