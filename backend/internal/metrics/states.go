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
//
// Nothing about a state comes from a stored analysis: what it was a week
// before (the assessment before this one) is judged again from the same
// reads, as of that instant and against today's thresholds. That decides
// what is held (hysteresis), "gelöst" and "nicht mehr messbar", and whether
// a lasting state held two sprints.

// SignalState is a state's signal kind: Signal.State says which.
const SignalState = "state"

// ToneLimit is a data-honesty state's tone: a unit the rules cannot judge,
// with the reason. Never a finding: it keeps the period from being called
// quiet (QuietUnjudged), and the page groups it apart ("nicht beurteilbar").
const ToneLimit = "limit"

// State keys (StateCard.Key). Each key has one tone, but for the lasting
// states (stateEval.lasting): a hint until a unit has held it two sprints.
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
	// StateUnmeasurable is a state the assessment a week before raised on
	// units the data can no longer judge: "nicht mehr messbar", never
	// "gelöst".
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

// Tiers rank the states: a team rule broken
// first, then what is held up now, then what lasts; data honesty in its own
// slot. A Signal's Weight.
const (
	tierRule    = 9
	tierNow     = 7
	tierLasting = 5
	tierLimit   = 4
)

// StateLookbackWeeks bounds every state's look back: a release, a PR still
// before production, a typical week, a branch's pushes. Older data is read
// by no state.
const StateLookbackWeeks = config.StateLookbackWeeks

// SprintDays is the team's sprint (owner, 2026-11): a lasting state that
// held a unit two sprints is red, not a hint.
const SprintDays = 14

// lastingSamples are the weekly instants before the period's end a lasting
// state without a known onset must have fired at too to have held two
// sprints.
const lastingSamples = 2 * SprintDays / 7

// StateThreshold is what a state measured against, the team's config key
// and value, for the card to name; Min is the fewest cases or units it
// needs besides, where it has such a count; Within the span a case must lie
// in, where the state has one.
type StateThreshold struct {
	Key   string  `json:"key"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	// MinKey and Min: a second threshold, a count, when the state has one.
	MinKey *string `json:"min_key"`
	Min    *int    `json:"min"`
	// WithinKey and Within: a third, a span: approved within so many
	// minutes, declined after so many working days, pushed within so many
	// working days. WithinKey null where the span is the rule's own, no
	// threshold of the team's. WithinUnit is the span's unit
	// (ThresholdMinutes, ThresholdWorkingDays, ThresholdDays); null without
	// a span.
	WithinKey  *string  `json:"within_key"`
	Within     *float64 `json:"within"`
	WithinUnit *string  `json:"within_unit"`
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
	// Since is when the state began to hold for the unit, where the data
	// tells: the last release, the oldest waiting PR's arrival on the
	// trunk, when the Nth approved PR had waited past the threshold, a
	// loop's first failure. LowerBound: at least since then (the data
	// starts there).
	Since      *time.Time `json:"since"`
	LowerBound bool       `json:"lower_bound"`
	// Week is the calendar week of a dated state (a merge pause).
	Week *Week `json:"week"`
	// Names are what the unit holds: a project's Apps, the repos of a
	// week's waiting PRs.
	Names []string `json:"names"`
	// Held: below the threshold now, kept because the assessment a week
	// before named it and its number has not fallen below HysteresisShare
	// of it.
	Held bool `json:"held"`
	// TwoSprints: a lasting state's unit that has held it two sprints, what
	// turns its card red (the card's other units may be fresh).
	TwoSprints bool `json:"two_sprints"`
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
	Since           *time.Time `json:"since"`
	SinceLowerBound bool       `json:"since_lower_bound"`
	// Threshold is the team's threshold it measured against; null for a
	// data-honesty state (it measures what the data cannot tell, against
	// no agreement) and an unmeasurable one.
	Threshold *StateThreshold `json:"threshold"`
	// At is the instant it was judged at: the period's end.
	At time.Time `json:"at"`
	// Was is the state an unmeasurable card was (StateUnmeasurable), else
	// null.
	Was *string `json:"was"`
	// Facts is the facts table its units are rows of: prs, deploys, runs.
	Facts string `json:"facts"`
	// ValueUnit is what its units' Value is (a fact unit: days, share,
	// count), for the page to format it.
	ValueUnit string `json:"value_unit"`
	// NamesKind is what its units' Names are where the page words them:
	// NamesBuildKind a build kind, NamesStage a stage; empty otherwise
	// (Apps, repos, branches, shown as they are).
	NamesKind string `json:"names_kind"`
}

// What a state's units' Names are (StateCard.NamesKind).
const (
	NamesBuildKind = "build_kind"
	NamesStage     = "stage"
)

// Unit statuses while a state is judged.
const (
	unitIdle = iota
	unitUnjudged
	unitClear
	unitFires
	// unitEscalated: past the hint into the breach its sibling state names
	// (StateReleaseDue past ReleaseBreachDays): judged, neither firing here
	// nor clear.
	unitEscalated
)

// stateUnit is a unit being judged: its card row, its status, and when the
// state began to hold for it where the data tells (began; nil: a sampled
// state, whose two sprints the earlier instants show). ongoing: an episodic
// state's episode still runs at the instant (stateEval.episodic).
type stateUnit struct {
	StateUnit
	status  int
	began   *time.Time
	ongoing bool
	// after is what the instant tells of an episodic unit's episode beyond
	// its status: afterKnown (whether it runs on), afterUnknown (its latest
	// stretch could not be judged: no telling whether it is over),
	// afterQuiet (nothing to judge then: no news).
	after int
}

// What an instant tells of an episode (stateUnit.after).
const (
	afterKnown = iota
	afterUnknown
	afterQuiet
)

// stateEval is one state judged over its units at an instant.
type stateEval struct {
	key, unit, source, tone, facts string
	tier                           int
	threshold                      StateThreshold
	units                          []stateUnit
	// lasting: a hint until a unit has held it SprintDays*2, then red.
	lasting bool
	// lower: a firing unit's Value at or above this keeps it named when the
	// assessment a week before named it (hysteresis); 0: no hysteresis.
	lower float64
	// valueUnit is the units' Value's fact unit where it is not the
	// threshold's (a merge pause's waiting PRs).
	valueUnit string
	// judges: a unit judged clear rests on a sample (a typical value on
	// enough cases, a share on enough), so the calm line counts its repo as
	// judged; a state clear for want of anything to name judges nothing.
	judges bool
	// namesKind is the card's NamesKind.
	namesKind string
	// episodic: a unit fires for a dated episode in the period (a week's
	// merge pause, a run of failed builds), shown while the episode lies in
	// the view; it is over once the episode no longer runs at the instant
	// (stateUnit.ongoing). "Gelöst" follows the instant after it ended, not
	// the week it leaves the view, whatever the view's length.
	episodic bool
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

// StateLookbackFrom is StateLookbackWeeks before cur's end: where every
// state's look back starts, whatever the view's length (a 26-week view
// reads its previous period too, which no state reads).
func StateLookbackFrom(cur Period) time.Time {
	return cur.To.In(cur.location()).AddDate(0, 0, -7*StateLookbackWeeks).UTC()
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

// afterWorkingDays is the instant days working days (workingDays') after
// from in loc: when a wait from from first counts more than days.
func afterWorkingDays(from time.Time, days float64, loc *time.Location) time.Time {
	left := time.Duration(days * float64(24*time.Hour))
	t := from.In(loc)
	for {
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		next := day.AddDate(0, 0, 1)
		if wd := day.Weekday(); wd != time.Saturday && wd != time.Sunday {
			if span := next.Sub(t); span >= left {
				return t.Add(left).UTC()
			} else {
				left -= span
			}
		}
		t = next
	}
}

// calendarDays is to less from in whole days.
func calendarDays(from, to time.Time) float64 {
	return float64(int(to.Sub(from).Hours() / 24))
}

// nthBegan is when the nth case of crossings (each the instant one case
// first counted past the threshold) had crossed: when "at least n cases"
// began to hold for the cases still counted. Nil with fewer.
func nthBegan(crossings []time.Time, n int) *time.Time {
	if n < 1 || len(crossings) < n {
		return nil
	}
	cs := slices.Clone(crossings)
	slices.SortFunc(cs, func(x, y time.Time) int { return x.Compare(y) })
	return ptr(cs[n-1])
}

// stateAt is an instant the states are judged at, and the view's weeks
// ending there: the period's end (now), or a whole week back each (the
// assessment before this one; a lasting state's two sprints).
type stateAt struct {
	end    time.Time
	period Period
	// now: the period's own end, where the open PRs and the period's
	// numbers (coverage, the hotspots) are known; an earlier instant
	// re-derives what it can from the events read.
	now bool
}

// instant is the instant weeks whole weeks before the period's end.
func (a *assessor) instant(weeks int) stateAt {
	if weeks == 0 {
		return stateAt{end: a.p.Period.To, period: a.p.Period, now: true}
	}
	end := a.p.Period.To.In(a.states.loc).AddDate(0, 0, -7*weeks)
	return stateAt{end: end.UTC(), period: weeksBefore(len(a.p.Period.Weeks), end)}
}

// stateRule judges one or more states at an instant; earlier: it can judge
// an instant before the period's end from the events read.
type stateRule struct {
	keys    []string
	earlier bool
	judge   func(at stateAt) []stateEval
}

// rules are every state rule, in the order their cards rank on a tie.
func (a *assessor) rules() []stateRule {
	return []stateRule{
		{[]string{StateNoRelease, StateReleaseDue}, true, a.releaseStates},
		{[]string{StateNotOnProd, StateSlowToProd}, true, a.notOnProdStates},
		{[]string{StateMergePause}, true, a.mergePauseState},
		{[]string{StateApprovedWaiting}, true, a.approvedWaitingState},
		{[]string{StateFirstReviewWait}, true, a.firstReviewState},
		{[]string{StateDeployFailing, StateDeployFailures}, true, a.deployFailingStates},
		{[]string{StateTrunkFailing}, true, a.trunkFailingState},
		{[]string{StateBuildsSilent}, true, a.buildsSilentState},
		{[]string{StateBranchFailing}, true, a.branchFailingState},
		{[]string{StateFlakyBuilds, StateLongBuilds, StateReleaseFailing}, true, a.buildStates},
		{[]string{StateBranchLife, StateBranchOpen, StateBranchSilent}, true, a.branchStates},
		{[]string{StateBigPRs, StateFastApproval, StateUnapproved, StateLateDecline}, true, a.reviewStates},
		{[]string{StateBotLoop}, true, a.botLoopState},
		{[]string{StateUntraced, StateUnattributed, StateNoProdStage}, false, a.honestyStates},
	}
}

// evalAt judges the states want names at at, by key; a rule that cannot
// judge an earlier instant is left out there.
func (a *assessor) evalAt(at stateAt, want func(key string) bool) map[string]stateEval {
	out := map[string]stateEval{}
	for _, r := range a.rules() {
		if (!at.now && !r.earlier) || !slices.ContainsFunc(r.keys, want) {
			continue
		}
		for _, e := range r.judge(at) {
			if a.gaps[e.source] {
				// A gated source judges nothing.
				for i := range e.units {
					if e.units[i].status != unitIdle {
						e.units[i].status = unitUnjudged
					}
				}
			}
			out[e.key] = e
		}
	}
	return out
}

// firing are the names of e's units that fire.
func firing(e stateEval) map[string]bool {
	out := map[string]bool{}
	for _, u := range e.units {
		if u.status == unitFires {
			out[u.Name] = true
		}
	}
	return out
}

// stateSignals judges every state at the period's end and a week before,
// the assessment before this one, from the same reads: a unit that fired
// then and is still within HysteresisShare of the threshold is held; a
// state that fired then and fires on none of its units now is "gelöst"
// where every unit it named is clear on evidence, "nicht mehr messbar"
// where one can no longer be judged, and neither where a unit fell idle or
// went (a project nobody works on is no news). A lasting state is red once
// a unit has held it two sprints: from its onset where the data tells, else
// firing at every week back over them.
func (a *assessor) stateSignals() {
	a.stateJudged, a.stateRepos, a.stateFlagged = map[string]bool{}, map[string]bool{}, map[string]bool{}
	a.states = newStateInput(a)
	all := func(string) bool { return true }
	cur := a.evalAt(a.instant(0), all)
	// The weeks back, the latest first: what the assessments before this one
	// named (hysteresis, resolution) and a lasting state's two sprints.
	back := make([]map[string]stateEval, lastingSamples)
	for w := range back {
		back[w] = a.evalAt(a.instant(w+1), all)
	}
	for _, r := range a.rules() {
		for _, key := range r.keys {
			if e, ok := cur[key]; ok {
				a.judge(e, named(back, key), back)
			}
		}
	}
	for _, r := range a.rules() {
		for _, key := range r.keys {
			p, ok := back[0][key]
			switch {
			case !ok:
			case p.episodic:
				a.settled(p, ongoing(p), cur[key])
			default:
				a.settled(p, named(back, key), cur[key])
			}
		}
	}
}

// ongoing are the units of an episodic state e whose episode ran at its
// instant: what it raised as held up then.
func ongoing(e stateEval) map[string]bool {
	out := map[string]bool{}
	for _, u := range e.units {
		if u.status == unitFires && u.ongoing {
			out[u.Name] = true
		}
	}
	return out
}

// named are the units of state key the assessment a week before named:
// those that fired then, and those within its hysteresis then that it had
// named the week before, back as far as the weeks read (lastingSamples):
// a held unit stays held while its number stays in the band.
func named(back []map[string]stateEval, key string) map[string]bool {
	out := map[string]bool{}
	if len(back) == 0 {
		return out
	}
	e, ok := back[0][key]
	if !ok {
		return out
	}
	earlier := named(back[1:], key)
	for _, u := range e.units {
		if u.status == unitFires || (inBand(e, u) && earlier[u.Name]) {
			out[u.Name] = true
		}
	}
	return out
}

// inBand reports whether u, judged clear in e, stands within e.lower of the
// threshold: held, had it been named before.
func inBand(e stateEval, u stateUnit) bool {
	return e.lower > 0 && u.status == unitClear && u.Value >= e.lower
}

// firedAt reports whether unit name fired in e: past the threshold itself,
// not merely within its hysteresis.
func firedAt(e stateEval, name string) bool {
	return slices.ContainsFunc(e.units, func(u stateUnit) bool { return u.Name == name && u.status == unitFires })
}

// judge turns e into its signal: a unit the week before named and still
// within e.lower of the threshold held, the firing units ranked, worst
// three, since, a lasting state red where a unit held it two sprints
// (back: the weeks before, the latest first). No unit firing: nothing.
func (a *assessor) judge(e stateEval, named map[string]bool, back []map[string]stateEval) {
	id := SignalState + ":" + e.key
	var fire []StateUnit
	card := StateCard{Key: e.key, Unit: e.unit, At: a.p.Period.To, Facts: e.facts, Worst: []StateUnit{}, Units: []StateUnit{}}
	if e.threshold.Key != "" {
		card.Threshold = ptr(e.threshold)
	}
	end := a.p.Period.To
	tone := e.tone
	for _, u := range e.units {
		if u.status == unitClear && e.lower > 0 && named[u.Name] && u.Value >= e.lower {
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
			// Only a unit past the threshold itself, not one held within the
			// hysteresis, has held a lasting state two sprints: the card names
			// which (TwoSprints) and turns red.
			if e.lasting && !u.Held && a.heldTwoSprints(e.key, u, end, back) {
				u.TwoSprints = true
				tone = ToneAttention
			}
			fire = append(fire, u.StateUnit)
		case unitClear, unitEscalated:
			card.Judged++
		case unitUnjudged:
			card.Unjudged++
		default:
			card.Idle++
		}
	}
	a.stateJudged[id] = card.Judged > 0
	a.calmLine(e, fire)
	if len(fire) == 0 {
		return
	}
	slices.SortStableFunc(fire, func(x, y StateUnit) int {
		return cmp.Or(cmp.Compare(y.Value, x.Value), compareSince(x.Since, y.Since), cmp.Compare(x.Name, y.Name))
	})
	card.Count, card.Units = len(fire), fire
	card.Worst = fire[:min(len(fire), 3)]
	card.More = len(fire) - len(card.Worst)
	for _, u := range fire {
		if u.Since != nil && (card.Since == nil || u.Since.Before(*card.Since)) {
			card.Since, card.SinceLowerBound = u.Since, u.LowerBound
		}
	}
	worst := fire[0].Value
	vu := e.valueUnit
	if vu == "" {
		vu = thresholdFactUnit(e.threshold.Unit)
	}
	card.ValueUnit, card.NamesKind = vu, e.namesKind
	s := Signal{
		ID: id, Kind: SignalState, Tone: tone, Source: e.source, Weight: e.tier, State: &card,
		Facts: []Fact{
			plain(id+".count", "state_units", UnitCount, f64(card.Count)),
			plain(id+".judged", "state_judged", UnitCount, f64(card.Judged)),
			plain(id+".worst", "state_"+e.key, vu, &worst),
		},
	}
	// A data-honesty state measures against no agreement: no threshold to
	// name.
	if card.Threshold != nil {
		s.Facts = append(s.Facts,
			plain(id+".threshold", "state_threshold", thresholdFactUnit(e.threshold.Unit), &e.threshold.Value))
	}
	// Breadth first, then the distance past the threshold.
	if card.Judged > 0 {
		s.score = float64(card.Count)/float64(card.Judged)*100 + min(worst/max(e.threshold.Value, 1), 99)
	}
	a.add(s)
}

// heldTwoSprints reports whether a lasting state's firing unit u has held
// two sprints by end: from its onset where the data tells it (began), else
// fired at every week back over them. A week its number stood only within
// the hysteresis counts for nothing: a unit first past the threshold now is
// a hint, whatever it stood at before.
func (a *assessor) heldTwoSprints(key string, u stateUnit, end time.Time, back []map[string]stateEval) bool {
	if u.began != nil {
		return end.Sub(*u.began) >= 2*SprintDays*24*time.Hour
	}
	if len(back) < lastingSamples {
		return false
	}
	for _, evals := range back {
		e, ok := evals[key]
		if !ok || !firedAt(e, u.Name) {
			return false
		}
	}
	return true
}

// calmLine notes the repos e judged on evidence and those it names (a
// finding; data honesty is none): the calm line's. A project's or an App's
// finding names its project's repos.
func (a *assessor) calmLine(e stateEval, fire []StateUnit) {
	pr := e.source == dataset.SourceBitbucket || e.source == dataset.SourceNoergler
	if e.unit == UnitRepo || e.unit == UnitBranch {
		for _, u := range e.units {
			repo, _, _ := strings.Cut(u.Name, ":")
			if e.judges && (u.status == unitClear || u.status == unitFires) && (!a.lost || !pr) {
				a.stateRepos[repo] = true
			}
		}
	}
	if e.tone == ToneLimit {
		return
	}
	for _, u := range fire {
		switch e.unit {
		case UnitRepo, UnitBranch:
			repo, _, _ := strings.Cut(u.Name, ":")
			a.stateFlagged[repo] = true
		case UnitProject, UnitApp:
			if u.Project == nil {
				continue
			}
			for repo := range a.activeRepos() {
				if projectOf(repo) == *u.Project {
					a.stateFlagged[repo] = true
				}
			}
		}
	}
}

// settled is what became of a state the assessment a week before raised
// (prev, judged again as of then; named, the units it named, held ones
// among them) and that fires on none of its units now (cur): "gelöst"
// where every unit it named is clear on evidence, "nicht
// mehr messbar" where one of them cannot be judged now, nothing where one
// fell idle, went, or escalated into a sibling state. Once: a week on, the
// instant before no longer raised it. An episodic state is judged by its
// running episodes (ongoing): over on every unit now, it is "gelöst" though
// an ended episode still shows on its card.
func (a *assessor) settled(prev stateEval, named map[string]bool, cur stateEval) {
	if len(named) == 0 || a.gaps[prev.source] {
		return
	}
	if (prev.episodic && len(ongoing(cur)) > 0) || (!prev.episodic && len(firing(cur)) > 0) {
		return
	}
	// Held now: still raised.
	if cur.lower > 0 && slices.ContainsFunc(cur.units, func(u stateUnit) bool {
		return named[u.Name] && u.status == unitClear && u.Value >= cur.lower
	}) {
		return
	}
	status := map[string]int{}
	for _, u := range cur.units {
		status[u.Name] = u.status
		switch {
		case !prev.episodic:
		case u.after == afterUnknown:
			status[u.Name] = unitUnjudged
		case u.after == afterQuiet:
			status[u.Name] = unitIdle
		case u.status == unitFires:
			// Fires for an ended episode (none runs now).
			status[u.Name] = unitClear
		}
	}
	var unjudged []string
	clear := 0
	for _, name := range slices.Sorted(maps.Keys(named)) {
		st, ok := status[name]
		switch {
		case !ok:
		case st == unitUnjudged:
			unjudged = append(unjudged, name)
		case st == unitClear:
			clear++
		}
	}
	id := SignalState + ":" + prev.key
	switch {
	case len(unjudged) > 0:
		card := StateCard{Key: StateUnmeasurable, Unit: prev.unit, At: a.p.Period.To, Was: ptr(prev.key),
			Count: len(unjudged), Unjudged: len(unjudged), Worst: []StateUnit{}, Units: []StateUnit{},
			ValueUnit: UnitCount}
		for _, name := range unjudged {
			u := StateUnit{Name: name, Names: []string{}}
			if prev.unit == UnitTeam {
				u.Name = ""
			}
			card.Units = append(card.Units, u)
		}
		card.Worst = card.Units[:min(len(card.Units), 3)]
		card.More = len(card.Units) - len(card.Worst)
		sid := SignalState + ":" + StateUnmeasurable + ":" + prev.key
		a.add(Signal{
			ID: sid, Kind: SignalState, Tone: ToneLimit, Source: prev.source, Weight: tierLimit, State: &card,
			Facts: []Fact{plain(sid+".count", "state_units", UnitCount, f64(len(unjudged)))},
		})
	case clear == len(named):
		s := Signal{ID: SignalResolved + ":" + id, Kind: SignalResolved, Tone: ToneGood, Weight: 2, Metric: ptr(id),
			Source: prev.source}
		s.Facts = []Fact{plain(s.ID+".count", "state_units", UnitCount, f64(len(named)))}
		a.add(s)
	}
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

// mapsKeys are m's keys, for slices.Sorted.
func mapsKeys[V any](m map[string]V) iter.Seq[string] { return maps.Keys(m) }

// coveringStates are the states that speak for a team number: its notable
// worsening is a fact on their card, not a card of its own (worsened
// changes only where no state covers them). Only states
// that can be red cover: a hint never takes a red change's place.
var coveringStates = map[string][]string{
	"deploy_frequency":             {StateNoRelease, StateReleaseDue},
	"lead_time":                    {StateNotOnProd, StateSlowToProd},
	"review_time":                  {StateFirstReviewWait, StateApprovedWaiting},
	"merged_per_week":              {StateMergePause},
	"batch_size":                   {StateBigPRs},
	"build_success:" + KindMain:    {StateTrunkFailing, StateFlakyBuilds},
	"build_success:" + KindFeature: {StateFlakyBuilds},
	"build_success:" + KindRelease: {StateReleaseFailing},
	"deploy_success":               {StateDeployFailing},
}

// foldChanges puts a notable worsening of a team number a red state covers
// on that state's card, as a fact against the previous period, and drops
// its own card: one finding, said once. A change naming its repos keeps
// those no unit of any covering red state names, shown per unit where no
// state covers the unit.
func (a *assessor) foldChanges() {
	red := map[string]int{}
	for i, s := range a.out {
		if s.State != nil && s.Tone == ToneAttention {
			red[s.State.Key] = i
		}
	}
	var drop []string
	for j, s := range a.out {
		if s.Kind != SignalChange || s.Tone != ToneAttention || s.Metric == nil {
			continue
		}
		var cards []int
		named := map[string]bool{}
		for _, key := range coveringStates[*s.Metric] {
			if i, ok := red[key]; ok {
				cards = append(cards, i)
				for _, u := range a.out[i].State.Units {
					named[u.Name] = true
				}
			}
		}
		if len(cards) == 0 {
			continue
		}
		if len(s.Drivers) > 0 {
			left := slices.DeleteFunc(slices.Clone(s.Drivers), func(d Driver) bool { return named[d.Repo] })
			if len(left) > 0 {
				a.out[j].Drivers = left
				continue
			}
		}
		a.out[cards[0]].Facts = append(a.out[cards[0]].Facts, s.Facts...)
		drop = append(drop, s.ID)
	}
	a.out = slices.DeleteFunc(a.out, func(s Signal) bool { return slices.Contains(drop, s.ID) })
}

// stateSource is each state's source, for a resolution's gate.
var stateSource = map[string]string{
	StateNoRelease: dataset.SourceArgoCD, StateReleaseDue: dataset.SourceArgoCD,
	StateNotOnProd: dataset.SourceArgoCD, StateSlowToProd: dataset.SourceArgoCD,
	StateMergePause: dataset.SourceBitbucket, StateApprovedWaiting: dataset.SourceBitbucket,
	StateBotLoop:  dataset.SourcePipeline,
	StateUntraced: dataset.SourceArgoCD, StateUnattributed: dataset.SourceArgoCD, StateNoProdStage: dataset.SourceArgoCD,
}

// stateInput is what the states read across the windows, each row once:
// the period's, the previous one's and the look-back's before it, bounded
// at StateLookbackWeeks before the period's end.
type stateInput struct {
	// prs are the people's merged PRs (peopleMerged) of every window read,
	// merged in the look-back, in the view's product.
	prs []dataset.PR
	// all is every PR of every window read: what an earlier instant's
	// period counts (mergedPRs over it), declines included.
	all *dataset.Dataset
	// runs are the period's and the previous period's runs (the look-back
	// window reads none); deploys every window's.
	runs    []dataset.Run
	deploys []dataset.Deploy
	// traces are every window's repo traces, for placing an older PR.
	traces map[string]dataset.RepoTrace
	// th are the thresholds.
	th config.Thresholds
	// loc is the period's zone.
	loc *time.Location
	// from is the look-back's start.
	from time.Time
	// view is the period's dataset with traces: what places an older PR.
	view dataset.Dataset
	// names names the runs' repos; gitops tells a GitOps repo's run.
	names  repoNamer
	gitops func(dataset.Run) bool
	// infra are the repos only infrastructure Apps deploy (infraRepos).
	infra map[string]bool
}

func newStateInput(a *assessor) *stateInput {
	in := &stateInput{traces: map[string]dataset.RepoTrace{}, th: a.p.thresholds(), loc: a.p.Period.location(),
		from: StateLookbackFrom(a.p.Period)}
	seen, seenAll := map[string]bool{}, map[string]bool{}
	all := &dataset.Dataset{Meta: a.ds.Meta, Apps: a.ds.Apps, GitOpsRepos: a.ds.GitOpsRepos}
	for i, ds := range []*dataset.Dataset{a.ds, a.prev, a.earlier} {
		if ds == nil {
			continue
		}
		for repo, t := range ds.Traces {
			in.traces[repo] = t
		}
		for _, pr := range ds.PRs {
			key := pr.Repo + "#" + strconv.FormatInt(pr.PRID, 10)
			if !seenAll[key] {
				seenAll[key] = true
				all.PRs = append(all.PRs, pr)
			}
			if seen[key] || pr.GitOps || pr.Bot || pr.State != "merged" || pr.MergedAt == nil || !a.f.inKey(pr.Project) ||
				pr.MergedAt.Before(in.from) {
				continue
			}
			seen[key] = true
			in.prs = append(in.prs, pr)
		}
		// The windows do not overlap: a deploy or a run is read by one only.
		for _, d := range ds.Deploys {
			if !d.At.Before(in.from) {
				in.deploys = append(in.deploys, d)
			}
		}
		if i <= 1 {
			in.runs = append(in.runs, ds.Runs...)
		}
	}
	all.Runs = in.runs
	in.all = all
	in.names = newRepoNamer(in.runs)
	in.gitops = gitopsRun(all)
	in.view = *a.ds
	in.view.Traces = in.traces
	in.infra = infraRepos(a.ds, in.traces)
	return in
}

// placed is pr placed on production at cutoff, by the traces of every
// window read.
func (in *stateInput) placed(pr dataset.PR, cutoff time.Time) placement {
	return prodPlacement(&in.view, pr, cutoff)
}

// runsIn are the runs read with an instant in p, for the build section's
// numbers over it (build).
func (in *stateInput) runsIn(p Period) *dataset.Dataset {
	ds := &dataset.Dataset{Meta: in.all.Meta, GitOpsRepos: in.all.GitOpsRepos}
	for _, r := range in.runs {
		if p.In(runAt(r)) {
			ds.Runs = append(ds.Runs, r)
		}
	}
	return ds
}

// infraRepos are the repos tracing knows an App deploys whose every
// release came from infrastructure Apps (dataset.App.Infra as of the
// period's end): a log shipper's repo is no team's delivery to judge. A
// repo's App is the one releasing at the instant and stage of its trace's
// release (dataset.AppRelease), or a stretch's; a repo with none known is
// judged.
func infraRepos(ds *dataset.Dataset, traces map[string]dataset.RepoTrace) map[string]bool {
	infra := map[string]bool{}
	for _, app := range ds.Apps {
		infra[app.Name] = app.Infra
	}
	type at struct {
		env string
		at  time.Time
	}
	apps := map[at][]string{}
	for _, r := range ds.Releases {
		k := at{r.Environment, r.At.UTC()}
		apps[k] = append(apps[k], r.App)
	}
	out := map[string]bool{}
	for repo, t := range traces {
		known, human := false, false
		for _, r := range t.Releases {
			names := apps[at{r.Environment, r.At.UTC()}]
			for _, s := range r.Stretches {
				names = append(names, s.App)
			}
			for _, n := range names {
				isInfra, ok := infra[n]
				if !ok {
					continue
				}
				known = true
				human = human || !isInfra
			}
		}
		if known && !human {
			out[repo] = true
		}
	}
	return out
}
