package metrics

import (
	"cmp"
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
)

// States are findings that stand without a previous period: a team's own
// agreement broken (a release per sprint, merged work on production within
// one), work held up now (a merge pause, approved PRs waiting), CI time a
// bot burns in a loop, and what the data cannot judge. Each is re-derived on
// every read from the data, judged per unit (a project, a repo, an App, a
// branch), never pooled across busy and quiet units, and summed up on one
// card: k of n units, the worst three with their number, since when. A
// state carries no comparison: its threshold is the team's (config
// Thresholds), and the card names it.

// SignalState is a state's signal kind: Signal.State says which.
const SignalState = "state"

// ToneLimit is a data-honesty state's tone: a unit the rules cannot judge,
// with the reason. Never a finding: it keeps the period from being called
// quiet (QuietUnjudged), and the page groups it apart ("nicht beurteilbar").
const ToneLimit = "limit"

// State keys (StateCard.Key). Each key has one tone, but for the lasting
// states (tierLasting): a hint until a unit has held it two sprints.
const (
	// StateNoRelease: a project rolled out with merged work, its last
	// release to production more than ReleaseBreachDays back (the sprint
	// rule broken). StateReleaseDue: more than ReleaseHintDays, a hint.
	StateNoRelease  = "no_release"
	StateReleaseDue = "release_due"
	// StateNotOnProd: a deployable repo's merged PRs not on production, the
	// oldest more than NotOnProdDays on the trunk. StateSlowToProd: its
	// typical wait from merge to production over NotOnProdDays.
	StateNotOnProd  = "not_on_prod"
	StateSlowToProd = "slow_to_prod"
	// StateMergePause: a project's week whose merges fell to
	// MergePauseShare of its typical week while approved PRs waited.
	StateMergePause = "merge_pause"
	// StateApprovedWaiting: a repo's approved PRs waiting for their merge
	// at the period's end.
	StateApprovedWaiting = "approved_waiting"
	// StateBotLoop: a bot branch failing BotLoopFailures builds in a row,
	// CI time wasted (never a quality finding).
	StateBotLoop = "bot_loop"
	// StateUntraced: a repo's merged PRs tracing cannot place on production
	// (Coverage's reasons): its delivery is not measurable.
	StateUntraced = "untraced"
	// StateUnattributed: releases to production of no project: the sprint
	// rule cannot be measured for the projects they belong to.
	StateUnattributed = "unattributed"
	// StateNoProdStage: Argo CD reports deploys, never one to the
	// production stage: the stage configuration or the notification is off.
	StateNoProdStage = "no_prod_stage"
	// StateUnmeasurable is a state raised before that the data can no
	// longer judge: "nicht mehr messbar", never "gelöst".
	StateUnmeasurable = "unmeasurable"
)

// State units (StateCard.Unit).
const (
	UnitProject = "project"
	UnitRepo    = "repo"
	UnitApp     = "app"
	UnitBranch  = "branch"
	UnitTeam    = "team"
)

// Threshold units (StateThreshold.Unit).
const (
	ThresholdDays        = "days"
	ThresholdWorkingDays = "working_days"
	ThresholdCount       = "count"
	ThresholdShare       = "share"
	ThresholdMinutes     = "minutes"
)

// Tiers rank the states (states-catalogue section 4): a team rule broken
// first, then what is held up now, then what lasts; data honesty in its own
// slot. A Signal's Weight.
const (
	tierRule    = 9
	tierNow     = 7
	tierLasting = 5
	tierLimit   = 4
)

// StateLookbackWeeks bounds every state's look back: a release, a PR still
// before production, a typical week. Older data is read by no state.
const StateLookbackWeeks = 26

// SprintDays is the team's sprint (owner, 2026-11): a lasting state that
// held a unit two sprints is red, not a hint.
const SprintDays = 14

// StateThreshold is what a state measured against, the team's config key
// and value, for the card to name; Min is the fewest cases or units it
// needs besides, where it has such a count.
type StateThreshold struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	// MinKey and Min: a second threshold, a count, when the state has one.
	MinKey *string `json:"min_key"`
	Min    *int    `json:"min"`
}

// StateUnit is one unit a state names: its number against the threshold,
// the cases behind it and since when.
type StateUnit struct {
	// Name is the unit: a project key, a repo, an App, a bot branch
	// ("<repo>:<branch>").
	Name    string  `json:"name"`
	Project *string `json:"project"`
	// Value is measured in the threshold's unit; the card's number.
	Value float64 `json:"value"`
	// Count is the cases it rests on (PRs, builds, waiting PRs); Of is the
	// base of a share; Typical is what it was measured against where that
	// is no constant (a project's typical week).
	Count   *int     `json:"count"`
	Of      *int     `json:"of"`
	Typical *float64 `json:"typical"`
	// Since is when it began: the last release, the oldest waiting PR's
	// merge or approval, a loop's first failure. LowerBound: at least since
	// then (the data starts there).
	Since      *time.Time `json:"since"`
	LowerBound bool       `json:"lower_bound"`
	// Week is the calendar week of a dated state (a merge pause).
	Week *Week `json:"week"`
	// Names are what the unit holds: a project's Apps, the repos of a
	// week's waiting PRs.
	Names []string `json:"names"`
	// Held: below the threshold now, kept because the last assessment named
	// it and its number has not fallen below HysteresisShare of it.
	Held bool `json:"held"`
	// Reason is a data-honesty unit's reason (a TraceReasons key).
	Reason *string `json:"reason"`
	// CISeconds is a bot loop's CI time.
	CISeconds *float64 `json:"ci_seconds"`
	// Query is the facts search listing the unit's rows in the card's facts
	// table (StateCard.Facts): a repo's or an App's name, a project's App
	// of its last release, a project's repos, a branch's pipeline. Null
	// where no search finds them: a team-wide unit, branches without a PR.
	Query *string `json:"query"`
	// Measure is what Value measured where a state judges a unit two ways
	// (StateFirstReviewWait): MeasureOldest the oldest case waiting at the
	// period's end, MeasureTypical the period's typical case. Null for
	// every other state.
	Measure *string `json:"measure"`
}

// What a unit's Value measured (StateUnit.Measure).
const (
	MeasureOldest  = "oldest"
	MeasureTypical = "typical"
)

// StateCard is what a state's signal says beyond its facts: which state,
// over which units, k of n, the worst three and the rest, since when, the
// threshold. Units is every unit it names, ranked, for the facts; Worst the
// first three of them, More the rest's count.
type StateCard struct {
	Key  string `json:"key"`
	Unit string `json:"unit"`
	// Count are the units it names, Judged those a rule could judge
	// (Count among them), Unjudged those without the data to, Idle those
	// with nothing to judge (no work in the span).
	Count    int `json:"count"`
	Judged   int `json:"judged"`
	Unjudged int `json:"unjudged"`
	Idle     int `json:"idle"`
	// Worst are the first three of Units, More how many follow.
	Worst []StateUnit `json:"worst"`
	More  int         `json:"more"`
	Units []StateUnit `json:"units"`
	// Since is the earliest of its units'.
	Since           *time.Time     `json:"since"`
	SinceLowerBound bool           `json:"since_lower_bound"`
	Threshold       StateThreshold `json:"threshold"`
	// At is the instant it was judged at: the period's end.
	At time.Time `json:"at"`
	// Was is the state an unmeasurable card was (StateUnmeasurable), else
	// null.
	Was *string `json:"was"`
	// Facts is the facts table its units are rows of: prs, deploys, runs.
	Facts string `json:"facts"`
}

// Unit statuses while a state is judged.
const (
	unitIdle = iota
	unitUnjudged
	unitClear
	unitFires
)

// stateUnit is a unit being judged: its card row, its status, and how far
// past the threshold it is (Value over the threshold, for the rank).
type stateUnit struct {
	StateUnit
	status int
}

// stateEval is one state judged over its units.
type stateEval struct {
	key, unit, source, tone, facts string
	tier                           int
	threshold                      StateThreshold
	units                          []stateUnit
	// lasting: a hint until a unit has held it SprintDays*2, then red.
	lasting bool
	// lower: a firing unit's Value at or above this keeps it named by the
	// last assessment firing (hysteresis); 0: no hysteresis.
	lower float64
	// valueUnit is the units' Value's fact unit where it is not the
	// threshold's (a merge pause's waiting PRs).
	valueUnit string
	// judges: a unit judged clear rests on a sample (a typical value on
	// enough cases, a share on enough), so the calm line counts its repo as
	// judged; a state clear for want of anything to name judges nothing.
	judges bool
}

// thresholds are p's, or the starting values.
func (p Params) thresholds() config.Thresholds {
	if p.Thresholds != nil {
		return *p.Thresholds
	}
	return config.DefaultThresholds()
}

// StatePeriod is the whole weeks from StateLookbackWeeks before cur's end
// up to prev's start: what the states look back into beyond the compared
// periods (dataset.Params.Lean); nil when prev reaches that far already.
func StatePeriod(cur, prev Period) *Period {
	loc := prev.location()
	end := prev.From.In(loc)
	first := cur.To.In(loc).AddDate(0, 0, -7*StateLookbackWeeks)
	weeks := int(end.Sub(first).Hours()/(24*7) + 0.5)
	if weeks <= 0 {
		return nil
	}
	p := weeksBefore(weeks, end)
	return &p
}

// workingDays is the Monday-to-Friday time in [from, to) in loc, in days of
// 24 hours: the waits a person causes count no weekend.
func workingDays(from, to time.Time, loc *time.Location) float64 {
	if !to.After(from) {
		return 0
	}
	from, to = from.In(loc), to.In(loc)
	var out time.Duration
	for day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc); day.Before(to); day = day.AddDate(0, 0, 1) {
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			continue
		}
		s, e := day, day.AddDate(0, 0, 1)
		if from.After(s) {
			s = from
		}
		if to.Before(e) {
			e = to
		}
		if e.After(s) {
			out += e.Sub(s)
		}
	}
	return out.Hours() / 24
}

// calendarDays is to less from in whole days.
func calendarDays(from, to time.Time) float64 {
	return float64(int(to.Sub(from).Hours() / 24))
}

// stateUnits is the history's units of a state: the names its last
// assessment named.
func (a *assessor) lastUnits(id string) map[string]bool {
	out := map[string]bool{}
	for _, n := range a.h.StateUnits[id] {
		out[n] = true
	}
	return out
}

// judge turns e into its signal: hysteresis for the units the last
// assessment named, the firing units ranked, worst three, since. No unit
// firing: nothing, but a state raised before and open since that its
// units can no longer judge reads "nicht mehr messbar"; one judged clear
// resolves (resolvedSignals). A gated source judges nothing.
func (a *assessor) judge(e stateEval) {
	id := SignalState + ":" + e.key
	if a.gaps[e.source] {
		for i := range e.units {
			if e.units[i].status != unitIdle {
				e.units[i].status = unitUnjudged
			}
		}
	}
	last := a.lastUnits(id)
	var fire []StateUnit
	card := StateCard{Key: e.key, Unit: e.unit, Threshold: e.threshold, At: a.p.Period.To, Facts: e.facts,
		Worst: []StateUnit{}, Units: []StateUnit{}}
	for _, u := range e.units {
		if u.status == unitClear && e.lower > 0 && last[u.Name] && u.Value >= e.lower {
			u.status, u.Held = unitFires, true
		}
		// A repo's or an App's rows are found by its name; a state names
		// another search where the unit's name finds nothing.
		if u.Query == nil && e.facts != "" && u.Name != "" && (e.unit == UnitRepo || e.unit == UnitApp) {
			u.Query = ptr(u.Name)
		}
		switch u.status {
		case unitFires:
			card.Judged++
			if u.Names == nil {
				u.Names = []string{}
			}
			fire = append(fire, u.StateUnit)
		case unitClear:
			card.Judged++
		case unitUnjudged:
			card.Unjudged++
		default:
			card.Idle++
		}
	}
	a.stateJudged[id] = card.Judged > 0
	// The calm line's repos: judged by a state whose clear verdict rests on
	// evidence (judges; never on PRs while Bitbucket's events were lost), or
	// named by one (a finding; data honesty is none).
	if e.unit == UnitRepo || e.unit == UnitBranch {
		pr := e.source == dataset.SourceBitbucket || e.source == dataset.SourceNoergler
		for _, u := range e.units {
			repo, _, _ := strings.Cut(u.Name, ":")
			if e.judges && (u.status == unitClear || u.status == unitFires) && (!a.lost || !pr) {
				a.stateRepos[repo] = true
			}
		}
		if e.tone != ToneLimit {
			for _, u := range fire {
				repo, _, _ := strings.Cut(u.Name, ":")
				a.stateFlagged[repo] = true
			}
		}
	}
	if len(fire) == 0 {
		return
	}
	slices.SortStableFunc(fire, func(x, y StateUnit) int {
		return cmp.Or(cmp.Compare(y.Value, x.Value), compareSince(x.Since, y.Since), cmp.Compare(x.Name, y.Name))
	})
	card.Count, card.Units = len(fire), fire
	card.Worst = fire[:min(len(fire), 3)]
	card.More = len(fire) - len(card.Worst)
	tone := e.tone
	for _, u := range fire {
		if u.Since != nil && (card.Since == nil || u.Since.Before(*card.Since)) {
			card.Since, card.SinceLowerBound = u.Since, u.LowerBound
		}
		if e.lasting && u.Since != nil && a.p.Period.To.Sub(*u.Since) >= 2*SprintDays*24*time.Hour {
			tone = ToneAttention
		}
	}
	worst := fire[0].Value
	vu := e.valueUnit
	if vu == "" {
		vu = thresholdFactUnit(e.threshold.Unit)
	}
	s := Signal{
		ID: id, Kind: SignalState, Tone: tone, Source: e.source, Weight: e.tier, State: &card,
		Facts: []Fact{
			plain(id+".count", "state_units", UnitCount, f64(card.Count)),
			plain(id+".judged", "state_judged", UnitCount, f64(card.Judged)),
			plain(id+".worst", "state_"+e.key, vu, &worst),
			plain(id+".threshold", "state_threshold", thresholdFactUnit(e.threshold.Unit), &e.threshold.Value),
		},
	}
	// Breadth first, then the distance past the threshold.
	if card.Judged > 0 {
		s.score = float64(card.Count)/float64(card.Judged)*100 + min(worst/max(e.threshold.Value, 1), 99)
	}
	a.add(s)
}

// thresholdFactUnit is a threshold unit as a fact's.
func thresholdFactUnit(u string) string {
	switch u {
	case ThresholdDays, ThresholdWorkingDays:
		return UnitDays
	case ThresholdShare:
		return UnitShare
	}
	// Minutes are a bare number, labelled "minutes".
	return UnitCount
}

func compareSince(x, y *time.Time) int {
	switch {
	case x == nil && y == nil:
		return 0
	case x == nil:
		return 1
	case y == nil:
		return -1
	}
	return x.Compare(*y)
}

// stateSignals judges every state, then what became of the states the
// last assessments raised: judged clear, resolved (resolvedSignals); not
// judgeable any more, "nicht mehr messbar".
func (a *assessor) stateSignals() {
	a.stateJudged, a.stateRepos, a.stateFlagged = map[string]bool{}, map[string]bool{}, map[string]bool{}
	a.states = newStateInput(a)
	a.releaseStates()
	a.notOnProdStates()
	a.mergePauseState()
	a.approvedWaitingState()
	a.firstReviewState()
	a.deployFailingStates()
	a.trunkFailingState()
	a.buildsSilentState()
	a.branchFailingState()
	a.buildStates()
	a.branchStates()
	a.reviewStates()
	a.botLoopState()
	a.honestyStates()
	raised := map[string]bool{}
	for _, s := range a.out {
		raised[s.ID] = true
	}
	for _, id := range slices.Sorted(mapsKeys(a.h.Open)) {
		key, ok := strings.CutPrefix(id, SignalState+":")
		if !ok || raised[id] || key == StateUnmeasurable {
			continue
		}
		if judged, known := a.stateJudged[id]; known && judged {
			continue
		}
		card := StateCard{Key: StateUnmeasurable, Unit: UnitTeam, At: a.p.Period.To, Was: ptr(key),
			Worst: []StateUnit{}, Units: []StateUnit{}}
		sid := SignalState + ":" + StateUnmeasurable + ":" + key
		a.out = append(a.out, Signal{
			ID: sid, Kind: SignalState, Tone: ToneLimit, Source: cmp.Or(stateSource[key], dataset.SourceArgoCD),
			Weight: tierLimit, State: &card, Facts: []Fact{plain(sid+".count", "state_units", UnitCount, f64(0))},
		})
	}
}

// mapsKeys are m's keys, for slices.Sorted.
func mapsKeys[V any](m map[string]V) iter.Seq[string] { return maps.Keys(m) }

// coveringStates are the states that speak for a team number: its notable
// worsening is a fact on their card, not a card of its own (states-catalogue
// section 4: worsened changes only where no state covers them).
var coveringStates = map[string][]string{
	"deploy_frequency":             {StateNoRelease, StateReleaseDue},
	"lead_time":                    {StateNotOnProd, StateSlowToProd},
	"review_time":                  {StateFirstReviewWait, StateApprovedWaiting},
	"merged_per_week":              {StateMergePause},
	"batch_size":                   {StateBigPRs},
	"build_success:" + KindMain:    {StateTrunkFailing, StateFlakyBuilds},
	"build_success:" + KindFeature: {StateBranchFailing, StateFlakyBuilds},
	"build_success:" + KindRelease: {StateReleaseFailing},
	"deploy_success":               {StateDeployFailing, StateDeployFailures},
}

// foldChanges puts a notable worsening of a team number a state covers on
// that state's card, as a fact against the previous period, and drops its
// own card: one finding, said once.
func (a *assessor) foldChanges() {
	// Only onto a red card: a red worsening never turns into a hint.
	byKey := map[string]int{}
	for i, s := range a.out {
		if s.State != nil && s.Tone == ToneAttention {
			byKey[s.State.Key] = i
		}
	}
	var drop []string
	for j, s := range a.out {
		if s.Kind != SignalChange || s.Tone != ToneAttention || s.Metric == nil {
			continue
		}
		for _, key := range coveringStates[*s.Metric] {
			i, ok := byKey[key]
			if !ok {
				continue
			}
			// A change naming its repos keeps those no unit of the state names:
			// it is shown per unit, where no state covers the unit.
			if len(s.Drivers) > 0 {
				named := map[string]bool{}
				for _, u := range a.out[i].State.Units {
					named[u.Name] = true
				}
				left := slices.DeleteFunc(slices.Clone(s.Drivers), func(d Driver) bool { return named[d.Repo] })
				if len(left) > 0 {
					a.out[j].Drivers = left
					break
				}
			}
			a.out[i].Facts = append(a.out[i].Facts, s.Facts...)
			drop = append(drop, s.ID)
			break
		}
	}
	a.out = slices.DeleteFunc(a.out, func(s Signal) bool { return slices.Contains(drop, s.ID) })
}

// stateResolved is a state the last assessments raised and the data now
// judges clear: "gelöst", once (History drops it after).
func (a *assessor) stateResolved(id string) *Signal {
	key, _ := strings.CutPrefix(id, SignalState+":")
	if !a.stateJudged[id] {
		return nil
	}
	s := Signal{ID: SignalResolved + ":" + id, Kind: SignalResolved, Tone: ToneGood, Weight: 2, Metric: ptr(id),
		Source: stateSource[key]}
	s.Facts = []Fact{plain(s.ID+".count", "state_units", UnitCount, f64(0))}
	return &s
}

// stateSource is each state's source, for a resolution's gate.
var stateSource = map[string]string{
	StateNoRelease: dataset.SourceArgoCD, StateReleaseDue: dataset.SourceArgoCD,
	StateNotOnProd: dataset.SourceArgoCD, StateSlowToProd: dataset.SourceArgoCD,
	StateMergePause: dataset.SourceBitbucket, StateApprovedWaiting: dataset.SourceBitbucket,
	StateBotLoop:  dataset.SourcePipeline,
	StateUntraced: dataset.SourceArgoCD, StateUnattributed: dataset.SourceArgoCD, StateNoProdStage: dataset.SourceArgoCD,
}

// stateInput is what the states read across the windows: the merged PRs of
// the period, the previous one and the look-back before it, each once.
type stateInput struct {
	// prs are the people's merged PRs (peopleMerged) of every window read,
	// each once, in the view's product.
	prs []dataset.PR
	// traces are every window's repo traces, for placing an older PR.
	traces map[string]dataset.RepoTrace
	// th are the thresholds.
	th config.Thresholds
	// loc is the period's zone.
	loc *time.Location
	// view is the period's dataset with traces: what places an older PR.
	view dataset.Dataset
}

func newStateInput(a *assessor) *stateInput {
	in := &stateInput{traces: map[string]dataset.RepoTrace{}, th: a.p.thresholds(), loc: a.p.Period.location()}
	seen := map[string]bool{}
	for _, ds := range []*dataset.Dataset{a.ds, a.prev, a.earlier} {
		if ds == nil {
			continue
		}
		for repo, t := range ds.Traces {
			in.traces[repo] = t
		}
		for _, pr := range ds.PRs {
			key := pr.Repo + "#" + strconv.FormatInt(pr.PRID, 10)
			if seen[key] || pr.GitOps || pr.Bot || pr.State != "merged" || pr.MergedAt == nil || !a.f.inKey(pr.Project) {
				continue
			}
			seen[key] = true
			in.prs = append(in.prs, pr)
		}
	}
	in.view = *a.ds
	in.view.Traces = in.traces
	return in
}

// placed is pr placed on production at cutoff, by the traces of every
// window read.
func (in *stateInput) placed(pr dataset.PR, cutoff time.Time) placement {
	return prodPlacement(&in.view, pr, cutoff)
}
