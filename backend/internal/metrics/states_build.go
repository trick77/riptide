package metrics

import (
	"cmp"
	"slices"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// ---- Bot builds ------------------------------------------------------------

// botLoopState judges each bot branch's builds in the period (owner,
// 2026-11: a bot's failed build is never a quality finding, only the CI
// time it burns counts): a branch failing BotLoopFailures builds in a row
// is a loop, a hint about wasted CI time. A build without a verdict
// (aborted) neither breaks nor extends a run of failures; a success ends
// it. The unit is the branch, named "<repo>:<branch>".
func (a *assessor) botLoopState() {
	th := a.states.th
	names := newRepoNamer(a.ds.Runs)
	gitops := gitopsRun(a.ds)
	by := map[string][]dataset.Run{}
	project := map[string]*string{}
	for _, r := range a.ds.Runs {
		if !r.Bot || r.Status == nil || gitops(r) || !a.f.in(r.Project) || !a.p.Period.In(runAt(r)) {
			continue
		}
		key := names(r) + ":" + r.Branch
		by[key] = append(by[key], r)
		project[key] = r.Project
	}
	e := stateEval{key: StateBotLoop, unit: UnitBranch, source: dataset.SourcePipeline, tone: ToneHint,
		facts: "runs", tier: tierLasting, valueUnit: UnitCount,
		threshold: StateThreshold{Key: "bot_loop_failures", Value: float64(th.BotLoopFailures), Unit: ThresholdCount}}
	for _, key := range slices.Sorted(mapsKeys(by)) {
		rs := by[key]
		slices.SortStableFunc(rs, func(x, y dataset.Run) int { return cmp.Or(runAt(x).Compare(*runAt(y)), cmp.Compare(x.RunID, y.RunID)) })
		best, n := 0, 0
		var first, bestFirst *time.Time
		secs, bestSecs := 0.0, 0.0
		for _, r := range rs {
			switch verdict(r) {
			case VerdictFailure:
				if n == 0 {
					first, secs = runAt(r), 0
				}
				n++
				secs += max(seconds(r), 0)
				if n > best {
					best, bestFirst, bestSecs = n, first, secs
				}
			case VerdictSuccess:
				n = 0
			}
		}
		u := stateUnit{StateUnit: StateUnit{Name: key, Project: project[key], Value: float64(best), Count: ptr(len(rs)),
			Since: bestFirst, CISeconds: ptr(bestSecs), Names: []string{}}, status: unitClear}
		if best >= th.BotLoopFailures {
			u.status = unitFires
		}
		e.units = append(e.units, u)
	}
	a.judge(e)
}
