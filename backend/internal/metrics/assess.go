package metrics

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// The assessment is the overview's "what now": the patterns the numbers
// raise over closed weeks, the few repos that move most. Every number and
// every class of it is decided here; an LLM may word it, never compute or
// judge it. It reads the same rules the page applies (jam, failing more, the
// tolerance), plus the starting values below, user-chosen 2026-11-16 and
// not measured.
const (
	// RulesVersion names the rules below: raised whenever a rule changes
	// what is raised, how it is judged or what the model is told. An
	// analysis stores the version it was assessed under; one of an older
	// version (or none, before the column) words signals the rules no longer
	// raise, and is not shown as the assessment's wording. 2: wave 1 (dropped
	// signals, symmetric thresholds, quiet decided here, resolved on
	// evidence).
	RulesVersion = 2
	// FocusMinWeight is the weight a repo's signals need for it to be a
	// focus candidate: one attention signal, or a hint and a notable change.
	FocusMinWeight = 3
	// FocusCandidates are the repos offered for the focus, FocusMax those
	// shown: two or three repos with the most movement, never all of them
	// watered down to a sentence each.
	FocusCandidates = 5
	FocusMax        = 3
	// AgentWaitShare: when the median wait for a build agent is this much of
	// the median build or more, the time is the platform's, not the build's.
	AgentWaitShare = 0.5
	// AgentWaitMinS is the median wait in seconds a kind's builds need on
	// top of AgentWaitShare: half of a one-minute build is no capacity
	// problem worth a step (owner, 2026-11).
	AgentWaitMinS = 120
	// SilentDays: a source that reported before and is silent this long at
	// the period's end has a gap; its numbers say nothing about the work.
	SilentDays = 7
	// DriverShare: a repo carrying this part of a notable worsening or more
	// is its driver, and counts toward the focus.
	DriverShare = 0.5
	// JamResolvedMax is the most long-open PRs a jammed repo may keep and
	// count as resolved: clearly below the jam (OpenJamMin), not one PR
	// under it, where the next week tips it back.
	JamResolvedMax = OpenJamMin - 2
	// StageAttention is the lead chain's Chain.AttentionShare, the page's
	// mark on a stage taking this part of the time to production or more.
	// No signal reads it since rules version 2: some stage always takes
	// longest, and its share is a ratio of medians over different PRs.
	StageAttention = 0.5
)

// Signal kinds. Dropped in rules version 2, never to be raised again from
// the same premises: slow_stage (one stage always takes longest),
// bottleneck_moved (a near tie flips on noise), throughput_instability,
// resync_share (re-syncs of infrastructure Apps, no "deploys that ship
// nothing"), steady_slow (the slowest three are always someone),
// inactive_prs and shared_stall (fired on one case; the open PRs and the
// build hotspots show them as numbers).
const (
	SignalDataGap     = "data_gap"
	SignalPRJam       = "pr_jam"
	SignalSlower      = "slower_build"
	SignalFailingMore = "failing_more"
	SignalChange      = "change"
	SignalAgentWait   = "agent_wait"
	SignalResolved    = "resolved"
	SignalQuestion    = "question"
)

// Signal tones: gate silences a source's claims; attention and hint go to
// what is stuck, good to what runs; talk is a question without a verdict.
const (
	ToneGate      = "gate"
	ToneAttention = "attention"
	ToneHint      = "hint"
	ToneGood      = "good"
	ToneTalk      = "talk"
)

// Sources a gap can silence.
const (
	SourceBitbucket = "bitbucket"
	SourcePipeline  = "pipeline"
	SourceArgoCD    = "argocd"
	SourceNoergler  = "noergler"
)

// Why a period is not quiet (Assessment.QuietReasons), in this order.
const (
	// QuietNoBaseline: no team number has a previous period to compare
	// with, or a source started reporting after the previous period began.
	QuietNoBaseline = "no_baseline"
	// QuietSourceGap: a source's data has a hole touching the comparison
	// (Limits): silent at the end, Bitbucket's events lost, or a source
	// starting inside the period.
	QuietSourceGap = "source_gap"
	// QuietNotMeasurable: Bitbucket or Argo CD never reported for the team,
	// or no merged PR could be traced to production.
	QuietNotMeasurable = "not_measurable"
	// QuietFindings: a signal or the question stands out.
	QuietFindings = "findings"
)

// Limit kinds.
const (
	// LimitSilentEnd: silent SilentDays or more at the period's end, a gate.
	LimitSilentEnd = "silent_end"
	// LimitSilence: Bitbucket stored nothing for a span while CI runs built
	// commits it never saw (dataset.Silence.UnseenRuns): events lost, not a
	// team idle. Another source's silence has no such evidence and is no
	// limit: a team may simply not deploy or review for two days.
	LimitSilence = "silence"
	// LimitStarts: the source's first event lies after the previous
	// period's start, so the comparison lacks it.
	LimitStarts = "starts"
)

// Fact units, for the UI's formatting.
const (
	UnitSeconds = "seconds"
	UnitShare   = "share"
	UnitCount   = "count"
	UnitPerWeek = "per_week"
	UnitWeeks   = "weeks"
	UnitUSD     = "usd"
	UnitDays    = "days"
)

// Fact is one number of a signal, against the previous period where it has
// one. ID is unique within the assessment; text refers to it as {{id}}.
type Fact struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Unit  string   `json:"unit"`
	Value *float64 `json:"value"`
	Prev  *float64 `json:"prev"`
	// Verdict and Magnitude as a Change's; none without a comparison.
	Verdict   string  `json:"verdict"`
	Magnitude *string `json:"magnitude"`
}

// Driver is a repo's part of a team number's worsening.
type Driver struct {
	Repo  string   `json:"repo"`
	Share float64  `json:"share"`
	Value *float64 `json:"value"`
	Prev  *float64 `json:"prev"`
}

// Signal is one pattern the numbers raise. ID is stable across periods, so
// a later assessment can tell it recurs or is gone.
type Signal struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Tone    string  `json:"tone"`
	Source  string  `json:"source"`
	Repo    *string `json:"repo"`
	RunKind *string `json:"run_kind"`
	// Metric names the number of a change, resolved or question signal.
	Metric *string `json:"metric"`
	// LowSample: the signal rests on fewer than 10 cases; shown, never a
	// headline, and a hint on so few keeps a period quiet.
	LowSample bool `json:"low_sample"`
	// Sample is the PRs the signal rests on, where counted: the page says
	// how few, not only that LowSample.
	Sample *int `json:"sample"`
	// Weight ranks the signals and the focus.
	Weight int `json:"weight"`
	// Seen is how many earlier assessments of the same window raised it,
	// in a row before this one.
	Seen int `json:"seen"`
	// Facts: the first is the hero number.
	Facts   []Fact   `json:"facts"`
	Waiting *Waiting `json:"waiting"`
	Drivers []Driver `json:"drivers"`
	// Series is the hero number week by week, where the page computes it
	// anyway (deploy frequency, build success); absent for the rest, so the
	// model's digest, which marshals signals as they are, never carries it.
	Series *Series `json:"series,omitempty"`
	// score ranks good changes among themselves, the larger move first: the
	// Change's |Score|. Not stored: the stored order already holds it.
	score float64
}

// Series is a number per week, the previous period's and this one's, null
// for a week without a value (no runs).
type Series struct {
	Prev []*float64 `json:"prev"`
	Cur  []*float64 `json:"cur"`
}

// FocusRepo is a repo with movement: its signals and up to three numbers.
type FocusRepo struct {
	Repo   string `json:"repo"`
	Weight int    `json:"weight"`
	// Seen is the most earlier assessments in a row of any of its signals.
	Seen    int      `json:"seen"`
	Signals []string `json:"signals"`
	Facts   []Fact   `json:"facts"`
	Waiting *Waiting `json:"waiting"`
}

// Problem is a problem an earlier assessment raised, not resolved since.
type Problem struct {
	// Tone is the latest tone it was raised in.
	Tone string
	// Before is its hero number before it was raised: the previous-period
	// value of its first fact in the earliest assessment that raised it
	// since it was last resolved, within the history read. A resolution
	// needs today's number back at it or better; nil, nothing resolves it
	// but a jam's own rule.
	Before *float64
}

// History is what earlier assessments of the same window raised: how many
// times in a row each signal ID, the latest one's signals by ID with their
// tone, and the problems open since.
type History struct {
	Seen   map[string]int
	Latest map[string]string
	// Open are the problems (a jam, a build slower or failing more, a
	// notable worsening) raised by any assessment read and not resolved by
	// a later one, by ID: one no longer raised (its comparison drifted to
	// the bad level) stays open until its number is back.
	Open map[string]Problem
	// Runs are the earlier assessments read; none: the view's first.
	Runs int
}

// Limit is a hole in a source's data the comparison crosses: a reason the
// period is not quiet, for the page to name with its dates.
type Limit struct {
	Source string `json:"source"`
	Kind   string `json:"kind"`
	// From is the source's last event before the hole (silent_end,
	// silence) or its first event ever (starts).
	From time.Time `json:"from"`
	// To is the source's first event after a silence, the period's end for
	// silent_end; null for starts.
	To *time.Time `json:"to"`
}

// Assessment is the overview's "what now" over closed weeks.
type Assessment struct {
	Period   Period `json:"period"`
	Previous Period `json:"previous"`
	// Gaps are the sources silenced, their claims left out.
	Gaps []string `json:"gaps"`
	// Signals are ranked, the gates first, repo-less and repo ones alike.
	Signals []Signal `json:"signals"`
	// Focus are the candidates, ranked; the page shows FocusMax of them.
	Focus []FocusRepo `json:"focus"`
	// Active are the repos with a merged PR, a build or an open PR in the
	// period. Of them Calm are those without a signal that a repo rule
	// could judge (judgeable), Unjudged those without a signal and too
	// little data to judge; the rest carry a signal. Idle were active in the
	// previous period and are not now; Repos is Active and Idle.
	Active   int `json:"active"`
	Calm     int `json:"calm"`
	Unjudged int `json:"unjudged"`
	Idle     int `json:"idle"`
	Repos    int `json:"repos"`
	// Team are the compared team numbers, every one, for the trend.
	Team []Fact `json:"team"`
	// Question is the one undirected number that moved notably; null when
	// none did. Never a fixed question: no question without a move.
	Question *Signal `json:"question"`
	// Compared: an assessment of the view for an earlier week was there to
	// tell what is resolved; without one, nothing can be.
	Compared bool `json:"compared"`
	// Quiet: nothing stands out and the period could be measured and
	// compared: the one case the page and the model may say so (owner: never
	// "unremarkable" where nothing could be compared). QuietReasons say why
	// not, in the order of the Quiet* constants; empty when Quiet.
	Quiet        bool     `json:"quiet"`
	QuietReasons []string `json:"quiet_reasons"`
	// Limits are the holes in the data the comparison crosses, oldest first.
	Limits []Limit `json:"limits"`
	// Coverage is of the period's merged PRs, as Throughput's: how many
	// reached production, have a size, were seen opened.
	Coverage PRCoverage `json:"coverage"`
}

// WeekStart is the Monday 00:00 of t's week in loc: where ClosedPeriod
// ends, and the schedule's week.
func WeekStart(t time.Time, loc *time.Location) time.Time { return weekStart(t, loc) }

// ClosedPeriod is the given number of whole calendar weeks ending at the
// start of now's week: an assessment never judges a week still running.
func ClosedPeriod(weeks int, now time.Time, loc *time.Location) Period {
	return weeksBefore(weeks, weekStart(now, loc))
}

// ClosedPrevious is the period ClosedPeriod compares with: the weeks right
// before it.
func ClosedPrevious(weeks int, now time.Time, loc *time.Location) Period {
	return weeksBefore(weeks, weekStart(now, loc).AddDate(0, 0, -7*weeks))
}

// Assess is the assessment of ds over p.Period against prev over
// p.Previous, both closed weeks (ClosedPeriod), with what earlier ones
// raised. prev is never nil, as in Compute. The dependency bots' builds are
// left out of every number it reads (withoutBotBuilds); infrastructure Apps
// are out of the deploy numbers already (deployFrequency).
func Assess(ds, prev *dataset.Dataset, p Params, h History) Assessment {
	bots := botPRBranches(ds, prev)
	ds, prev = withoutBotBuilds(ds, bots), withoutBotBuilds(prev, bots)
	m, pm := computeBoth(ds, prev, nil, p)
	a := &assessor{m: m, pm: pm, ds: ds, prev: prev, p: p, f: newFilter(ds, p), h: h}
	return a.run()
}

type assessor struct {
	m, pm    Metrics
	ds, prev *dataset.Dataset
	p        Params
	f        filter
	h        History
	gaps     map[string]bool
	limits   []Limit
	// lost: Bitbucket's events were lost in one of the two periods
	// (LimitSilence). Its counts read low there, so none of its numbers is
	// compared: a false worsening now, a false gain once the hole lies in the
	// previous period.
	lost bool
	out  []Signal
	// team are the compared team numbers, computed once.
	team []teamChange
}

func (a *assessor) run() Assessment {
	a.gapSignals()
	a.team = a.teamChanges()
	a.jamSignals()
	a.buildSignals()
	a.changeSignals()
	a.resolvedSignals()
	for i := range a.out {
		a.out[i].Seen = a.h.Seen[a.out[i].ID]
		// A change keeps its ID whether worse or better: a worsening after a
		// gain starts a streak of its own (History ends a streak where the
		// tone flips among earlier runs; this is the flip to now).
		if tone, ok := a.h.Latest[a.out[i].ID]; ok && tone != a.out[i].Tone {
			a.out[i].Seen = 0
		}
		if a.out[i].Drivers == nil {
			a.out[i].Drivers = []Driver{}
		}
	}
	rank(a.out)
	out := Assessment{
		Period: a.p.Period, Previous: a.p.Previous, Gaps: append([]string{}, slices.Sorted(maps.Keys(a.gaps))...),
		Signals: a.out, Question: a.question(), Compared: a.h.Runs > 0, Limits: a.limits,
		Coverage: a.m.Throughput.Coverage,
	}
	out.Focus = a.focus(&out)
	out.Team = []Fact{}
	for _, t := range a.team {
		if !a.gaps[t.source] {
			out.Team = append(out.Team, fact("team."+t.metric, t.label, t.unit, t.value, t.c))
		}
	}
	if out.Signals == nil {
		out.Signals = []Signal{}
	}
	out.QuietReasons = a.quietReasons(out)
	out.Quiet = len(out.QuietReasons) == 0
	return out
}

// rank orders the signals: by tone, the gates first, then by weight; good
// changes of one weight by the size of their move.
func rank(ss []Signal) {
	slices.SortStableFunc(ss, func(x, y Signal) int {
		return cmp.Or(cmp.Compare(toneRank(x.Tone), toneRank(y.Tone)), cmp.Compare(y.Weight, x.Weight),
			cmp.Compare(y.score, x.score), cmp.Compare(x.ID, y.ID))
	})
}

func toneRank(t string) int {
	switch t {
	case ToneGate:
		return 0
	case ToneAttention:
		return 1
	case ToneHint:
		return 2
	case ToneGood:
		return 3
	}
	return 4
}

// add keeps s unless its source is silenced.
func (a *assessor) add(s Signal) {
	if a.gaps[s.Source] {
		return
	}
	a.out = append(a.out, s)
}

// fact is a number against its previous one, judged as c.
func fact(id, label, unit string, v *float64, c Change) Fact {
	return Fact{ID: id, Label: label, Unit: unit, Value: v, Prev: c.Prev, Verdict: c.Verdict, Magnitude: c.Magnitude}
}

// plain is a number without a comparison.
func plain(id, label, unit string, v *float64) Fact {
	return Fact{ID: id, Label: label, Unit: unit, Value: v, Verdict: ChangeNone}
}

func f64(n int) *float64 { return ptr(float64(n)) }

// ---- Bots' builds ----------------------------------------------------------

// botBranchStems are the dependency bots' branch schemes, as build.go's
// notFeatureStems names them: renovate/…, dependabot-….
var botBranchStems = map[string]bool{"renovate": true, "dependabot": true, "snyk": true}

// botPRBranches are the PR-<n> branches (lowercased, by repo) of the bots'
// PRs either dataset holds: a Jenkins PR build names its PR, not the bot's
// branch. A bot PR neither opened, merged nor open in the windows is not
// known, and its builds stay in.
func botPRBranches(ds ...*dataset.Dataset) map[string]bool {
	out := map[string]bool{}
	add := func(repo string, id int64) { out[repo+"\x00pr-"+strconv.FormatInt(id, 10)] = true }
	for _, d := range ds {
		for _, pr := range d.PRs {
			if pr.Bot {
				add(pr.Repo, pr.PRID)
			}
		}
		for _, pr := range d.OpenPRs {
			if pr.Bot {
				add(pr.Repo, pr.PRID)
			}
		}
	}
	return out
}

// botBuild reports whether r built a bot's branch: a dependency bot's
// scheme, or a bot PR's PR-<n>.
func botBuild(r dataset.Run, botPRs map[string]bool) bool {
	p := branchPrefix(r.Branch)
	if botBranchStems[strings.TrimSuffix(strings.TrimSuffix(p, "/"), "-…")] {
		return true
	}
	return r.Repo != nil && botPRs[*r.Repo+"\x00"+strings.ToLower(r.Branch)]
}

// withoutBotBuilds is ds without the bots' builds (owner, 2026-11: a bot's
// failed build is no quality problem of the team's, and no judged number
// reads it). A copy: the page's dataset keeps them.
func withoutBotBuilds(ds *dataset.Dataset, botPRs map[string]bool) *dataset.Dataset {
	if !slices.ContainsFunc(ds.Runs, func(r dataset.Run) bool { return botBuild(r, botPRs) }) {
		return ds
	}
	cp := *ds
	cp.Runs = slices.DeleteFunc(slices.Clone(ds.Runs), func(r dataset.Run) bool { return botBuild(r, botPRs) })
	return &cp
}

// ---- Gaps and limits -------------------------------------------------------

// gapSignals silence a source that reported before and went quiet for
// SilentDays at the period's end: a dead webhook must not read as a team
// that stopped deploying. They and the other holes the comparison crosses
// are the limits.
func (a *assessor) gapSignals() {
	a.gaps = map[string]bool{}
	a.limits = []Limit{}
	end := a.p.Period.To
	ends, starts := a.m.Meta.DataEnd, a.m.Meta.DataStart
	for _, s := range []struct {
		name      string
		at, start *time.Time
	}{
		{SourceBitbucket, ends.Bitbucket, starts.Bitbucket}, {SourcePipeline, ends.Pipeline, starts.Pipeline},
		{SourceArgoCD, ends.ArgoCD, starts.ArgoCD}, {SourceNoergler, ends.Noergler, starts.Noergler},
	} {
		// A source never heard of is not a hole: the team has none.
		if s.start != nil && s.start.After(a.p.Previous.From) && s.start.Before(end) {
			a.limits = append(a.limits, Limit{Source: s.name, Kind: LimitStarts, From: *s.start})
		}
		if s.at == nil || end.Sub(*s.at) < SilentDays*24*time.Hour {
			continue
		}
		a.gaps[s.name] = true
		a.limits = append(a.limits, Limit{Source: s.name, Kind: LimitSilentEnd, From: *s.at, To: ptr(end)})
		days := float64(int(end.Sub(*s.at).Hours() / 24))
		a.out = append(a.out, Signal{
			ID: "data_gap:" + s.name, Kind: SignalDataGap, Tone: ToneGate, Source: s.name, Weight: 10,
			Facts: []Fact{plain("data_gap_"+s.name+".silent", "silent_days", UnitDays, &days)},
		})
	}
	// Bitbucket's lost events, in either period: a silence at the boundary
	// is in both datasets once.
	seen := map[[2]time.Time]bool{}
	for _, s := range slices.Concat(a.prev.Meta.Silences, a.ds.Meta.Silences) {
		key := [2]time.Time{s.From, s.To}
		if s.Source != dataset.SourceBitbucket || s.UnseenRuns <= 0 || seen[key] {
			continue
		}
		seen[key] = true
		a.lost = true
		a.limits = append(a.limits, Limit{Source: SourceBitbucket, Kind: LimitSilence, From: s.From, To: ptr(s.To)})
	}
	slices.SortStableFunc(a.limits, func(x, y Limit) int { return x.From.Compare(y.From) })
}

// quietReasons are why the period is not quiet, none when it is: every
// number there, compared against a covered previous period, and nothing
// standing out, a gain or a resolution included (a gain stands out; the
// page must not call a period with one unremarkable).
func (a *assessor) quietReasons(out Assessment) []string {
	reasons := []string{}
	baseline := slices.ContainsFunc(out.Team, func(f Fact) bool { return f.Verdict != ChangeNone })
	starts, hole := false, false
	for _, l := range a.limits {
		switch {
		case l.Kind != LimitStarts:
			hole = true
		case !l.From.Before(a.p.Period.From):
			starts, hole = true, true
		default:
			starts = true
		}
	}
	if !baseline || starts {
		reasons = append(reasons, QuietNoBaseline)
	}
	if hole {
		reasons = append(reasons, QuietSourceGap)
	}
	if a.m.Meta.DataStart.Bitbucket == nil || !a.m.Delivery.Frequency.ArgoReporting || out.Coverage.ToProd.Count == 0 {
		reasons = append(reasons, QuietNotMeasurable)
	}
	findings := out.Question != nil || slices.ContainsFunc(out.Signals, func(s Signal) bool {
		// A hint on fewer than lowSample cases is no finding.
		return s.Tone != ToneGate && (s.Tone != ToneHint || !s.LowSample)
	})
	if findings {
		reasons = append(reasons, QuietFindings)
	}
	return reasons
}

// ---- Open PRs --------------------------------------------------------------

func (a *assessor) jamSignals() {
	prevJammed := map[string]bool{}
	for _, r := range a.pm.OpenPRs.all {
		if r.Jammed {
			prevJammed[r.Repo] = true
		}
	}
	// More jammed repos lift every jam only as news: one more is none.
	var more bool
	for _, t := range a.team {
		if t.metric == "open_jammed" {
			more = t.c.Verdict == ChangeWorse && t.c.Magnitude != nil && *t.c.Magnitude == MagnitudeNotable
		}
	}
	for _, r := range a.m.OpenPRs.Repos {
		if !r.Jammed {
			continue
		}
		id := "pr_jam:" + r.Repo
		// A jam makes its repo a focus candidate alone: the pattern the
		// assessment is there to name.
		tone, w := ToneHint, FocusMinWeight
		if more || prevJammed[r.Repo] {
			tone, w = ToneAttention, FocusMinWeight+1
		}
		// A repo without an open PR at the previous end had none open long;
		// nor one with inactive PRs only, which count in no other number.
		prevLong := ptr(0.0)
		for _, p := range a.pm.OpenPRs.all {
			if p.Repo == r.Repo {
				prevLong = f64(p.Long)
			}
		}
		long := Fact{ID: id + ".long", Label: "open_long", Unit: UnitCount, Value: f64(r.Long), Prev: prevLong,
			Verdict: ChangeNone}
		// Against the previous end only where Bitbucket covers that period:
		// a repo riptide did not hear of yet reads as 0 then.
		if incomplete := before(a.m.Meta.DataStart.Bitbucket, a.p.Previous.From) || a.lost; !incomplete {
			c := openJammedMetric.judge(side{f64(r.Long), true}, side{prevLong, true}, false)
			long.Verdict, long.Magnitude = c.Verdict, c.Magnitude
		} else {
			long.Prev = nil
		}
		w2 := r.LongWaiting
		a.add(Signal{
			ID: id, Kind: SignalPRJam, Tone: tone, Source: SourceBitbucket, Repo: ptr(r.Repo), Weight: w,
			Facts: []Fact{long, plain(id+".open", "open", UnitCount, f64(r.Open)),
				plain(id+".age", "age_p50", UnitSeconds, r.Age.P50)},
			Waiting: &w2,
		})
	}
}

// ---- Builds ----------------------------------------------------------------

func (a *assessor) buildSignals() {
	for _, k := range a.m.Build.Hotspots.Kinds {
		kind := k.Kind
		for _, h := range k.Repos {
			// Slower than its own previous period: a change, not a habit.
			if h.Slow {
				id := "slower_build:" + kind + ":" + h.Repo
				a.add(Signal{
					ID: id, Kind: SignalSlower, Tone: ToneAttention, Source: SourcePipeline, Repo: ptr(h.Repo),
					RunKind: ptr(kind), Weight: 3,
					Facts: []Fact{fact(id+".p50", "build_p50", UnitSeconds, h.Duration.P50, h.DurationChange),
						plain(id+".wait", "build_wait_p50", UnitSeconds, h.Waiting.P50),
						plain(id+".work", "build_work_p50", UnitSeconds, h.Building.P50)},
				})
			}
			if !h.FailingMore {
				continue
			}
			id := SignalFailingMore + ":" + kind + ":" + h.Repo
			a.add(Signal{
				ID: id, Kind: SignalFailingMore, Tone: ToneAttention, Source: SourcePipeline, Repo: ptr(h.Repo),
				RunKind: ptr(kind), Weight: 3,
				Facts: []Fact{fact(id+".failure_rate", "failure_rate", UnitShare, h.FailureRate, h.FailureChange),
					plain(id+".wasted", "wasted", UnitSeconds, ptr(h.WastedSeconds)),
					plain(id+".runs", "runs", UnitCount, f64(h.Runs))},
			})
		}
	}
	// Agents: a hint, the platform's capacity and not the team's builds.
	for _, b := range a.m.Build.Kinds {
		if b.Waiting.N < lowSample || b.Waiting.P50 == nil || b.Duration.P50 == nil || *b.Duration.P50 <= 0 {
			continue
		}
		if *b.Waiting.P50 / *b.Duration.P50 + eps < AgentWaitShare || *b.Waiting.P50+eps < AgentWaitMinS {
			continue
		}
		id := "agent_wait:" + b.Kind
		a.add(Signal{
			ID: id, Kind: SignalAgentWait, Tone: ToneHint, Source: SourcePipeline, RunKind: ptr(b.Kind),
			Weight: 2,
			Facts: []Fact{plain(id+".wait", "build_wait_p50", UnitSeconds, b.Waiting.P50),
				plain(id+".p50", "build_p50", UnitSeconds, b.Duration.P50)},
		})
	}
}

// ---- Team numbers ----------------------------------------------------------

// teamChange is one compared team number.
type teamChange struct {
	metric, label, unit, source string
	value                       *float64
	c                           Change
}

// teamMetrics are the team numbers' definitions, by metric, for a
// resolution's judgement against its value before (build_success:<kind>
// aside, in teamMetric).
var teamMetrics = map[string]metricID{
	"deploy_frequency": deployFreqMetric, "deploy_success": deploySuccessMetric, "lead_time": leadTimeMetric,
	"review_time": reviewTimeMetric, "within_day": withinDayMetric, "open_jammed": openJammedMetric,
	"merged_per_week": mergedPerWeekMetric, "batch_size": batchSizeMetric, "wip_weeks": wipWeeksMetric,
}

func teamMetric(name string) (metric, bool) {
	if strings.HasPrefix(name, "build_success:") {
		return buildSuccessMetric.def(), true
	}
	id, ok := teamMetrics[name]
	return id.def(), ok
}

// bitbucketCounted are the team numbers read off Bitbucket's events: lost
// events (lost) leave them uncompared. Lead time is Argo CD's: a commit
// whose push was lost is missing from it, the others' times are right.
var bitbucketCounted = map[string]bool{
	"review_time": true, "within_day": true, "open_jammed": true, "merged_per_week": true, "batch_size": true,
	"wip_weeks": true, "feature_share": true, "untracked": true, "open_prs": true, "first_touch": true,
}

// unlost is c, or no verdict for a number of Bitbucket's when its events
// were lost (lost).
func (a *assessor) unlost(metric string, c Change) Change {
	if !a.lost || !bitbucketCounted[metric] {
		return c
	}
	return Change{Verdict: ChangeNone, Reason: ptr(ReasonIncomplete), Directed: c.Directed}
}

func (a *assessor) teamChanges() []teamChange {
	m, c := a.m, a.m.Comparison
	out := []teamChange{
		{"deploy_frequency", "deploy_per_week", UnitPerWeek, SourceArgoCD, ptr(m.Delivery.Frequency.PerWeek), c.DeployFreq},
		{"deploy_success", "deploy_success", UnitShare, SourceArgoCD, m.Delivery.Frequency.SuccessRate, c.DeploySuccess},
		{"lead_time", "lead_time_p50", UnitSeconds, SourceArgoCD, m.Delivery.LeadTime.Human.P50, c.LeadTime},
		{"review_time", "review_p50", UnitSeconds, SourceBitbucket, m.Review.Review.P50, c.ReviewTime},
		{"within_day", "within_day", UnitShare, SourceBitbucket, m.Review.WithinDay.Rate, c.WithinDay},
		{"open_jammed", "jammed_repos", UnitCount, SourceBitbucket, f64(m.OpenPRs.Jammed), c.OpenJammed},
	}
	for _, b := range m.Build.Kinds {
		for _, kc := range c.Build {
			if kc.Kind == b.Kind {
				out = append(out, teamChange{"build_success:" + b.Kind, "build_success", UnitShare, SourcePipeline,
					b.SuccessRate, kc.Success})
			}
		}
	}
	bb := before(m.Meta.DataStart.Bitbucket, a.p.Previous.From)
	// Flow numbers: throughput, work in progress against it.
	merged, pmerged := a.m.Throughput.MergedPerWeek, a.pm.Throughput.MergedPerWeek
	ok, pok := a.m.Review.Merged >= lowSample, a.pm.Review.Merged >= lowSample
	out = append(out, teamChange{"merged_per_week", "merged_per_week", UnitPerWeek, SourceBitbucket, merged, c.MergedPerWeek})
	out = append(out, teamChange{"batch_size", "batch_size", UnitCount, SourceNoergler, m.Throughput.BatchSize.P50, c.BatchSize})
	wip, pwip := wipWeeks(a.m, merged), wipWeeks(a.pm, pmerged)
	out = append(out, teamChange{"wip_weeks", "wip_weeks", UnitWeeks, SourceBitbucket, wip,
		wipWeeksMetric.judge(side{wip, ok}, side{pwip, pok}, bb)})
	for i := range out {
		out[i].c = a.unlost(out[i].metric, out[i].c)
	}
	return out
}

// wipWeeks is the open PRs at the period's end over the PRs merged per
// week: how many weeks of work stand open (Little's law).
func wipWeeks(m Metrics, merged *float64) *float64 {
	if merged == nil || *merged <= 0 {
		return nil
	}
	return ptr(float64(m.OpenPRs.Open) / *merged)
}

// changeSignals are the notable moves of the directed team numbers, better
// and worse alike (owner, 2026-11: a gain counts from where a worsening
// does, 25 % or 5 points; a small one was noise read as success). The worse
// ones carry the repos that drive them where riptide can tell.
func (a *assessor) changeSignals() {
	for _, t := range a.team {
		c := t.c
		if c.Magnitude == nil || !c.Directed || *c.Magnitude != MagnitudeNotable ||
			(c.Verdict != ChangeBetter && c.Verdict != ChangeWorse) {
			continue
		}
		id := "change:" + t.metric
		s := Signal{
			ID: id, Kind: SignalChange, Tone: ToneGood, Source: t.source, Metric: ptr(t.metric), Weight: 2,
			Facts: []Fact{fact(id+".value", t.label, t.unit, t.value, c)},
		}
		if kind, ok := strings.CutPrefix(t.metric, "build_success:"); ok {
			s.RunKind = ptr(kind)
		}
		s.Series = a.series(t.metric)
		if c.Verdict == ChangeWorse {
			s.Tone = ToneAttention
			if t.metric == "review_time" {
				s.Drivers = a.reviewDrivers()
			}
		} else if c.Score != nil {
			s.score = math.Abs(*c.Score)
		}
		a.add(s)
	}
}

// series is a team number's weeks in both periods, for the numbers the
// page already counts per week: releases to production, a kind's success
// rate. Nil for the rest; no weekly numbers are computed for a card alone.
func (a *assessor) series(metric string) *Series {
	// A week before Argo CD reported is no week of zero releases: null. The
	// week it started in counts, as the per-week rate counts it.
	start := a.m.Meta.DataStart.ArgoCD
	releases := func(f DeployFrequency, p Period) []*float64 {
		out := make([]*float64, len(f.Weeks))
		for i, w := range f.Weeks {
			if start != nil && i < len(p.Weeks) && p.Weeks[i].End.After(*start) {
				out[i] = f64(w.Release)
			}
		}
		return out
	}
	success := func(m Metrics, kind string) []*float64 {
		for _, b := range m.Build.Kinds {
			if b.Kind == kind && b.Weekly != nil {
				return slices.Clone(b.Weekly)
			}
		}
		return []*float64{}
	}
	if metric == "deploy_frequency" {
		return &Series{Prev: releases(a.pm.Delivery.Frequency, a.p.Previous), Cur: releases(a.m.Delivery.Frequency, a.p.Period)}
	}
	if kind, ok := strings.CutPrefix(metric, "build_success:"); ok {
		return &Series{Prev: success(a.pm, kind), Cur: success(a.m, kind)}
	}
	return nil
}

// reviewDrivers split a worsened review time by repo: each repo's PRs times
// the rise of its median, over all rises. A rough split, enough to point
// at the repo that carries it; repos without PRs in both periods left out.
func (a *assessor) reviewDrivers() []Driver {
	cur, prev := a.reviewByRepo(a.ds, a.p.Period), a.reviewByRepo(a.prev, a.p.Previous)
	var out []Driver
	total := 0.0
	for repo, ws := range cur {
		pw := prev[repo]
		if len(ws) == 0 || len(pw) == 0 {
			continue
		}
		c, p := Median(ws), Median(pw)
		rise := float64(len(ws)) * (*c - *p)
		if rise <= 0 {
			continue
		}
		total += rise
		out = append(out, Driver{Repo: repo, Share: rise, Value: c, Prev: p})
	}
	if total <= 0 {
		return nil
	}
	for i := range out {
		out[i].Share /= total
	}
	slices.SortFunc(out, func(x, y Driver) int { return cmp.Or(cmp.Compare(y.Share, x.Share), cmp.Compare(x.Repo, y.Repo)) })
	return out[:min(len(out), 3)]
}

// reviewByRepo is the review stage's waits per repo, ready to approval, as
// the breakdown counts them.
func (a *assessor) reviewByRepo(ds *dataset.Dataset, p Period) map[string][]float64 {
	out := map[string][]float64{}
	for _, pr := range mergedPRs(ds, p, a.f) {
		if pr.ReadyAt == nil {
			continue
		}
		if w := wait(pr.ReadyAt, pr.ApprovedAt); w != nil {
			out[pr.Repo] = append(out[pr.Repo], *w)
		}
	}
	return out
}

// ---- Resolved --------------------------------------------------------------

// resolvable are the kinds a resolution can follow: a problem with a
// number that can come back.
var resolvable = []string{SignalPRJam, SignalFailingMore, SignalSlower, SignalChange}

// backAt reports whether v is back at before or better: no worse than it
// beyond the tolerance, in the number's own direction.
func backAt(m metric, v, before *float64) bool {
	return v != nil && before != nil && m.d != undirected && changeVerdict(*v, *before, m.d, m.s) != ChangeWorse
}

// resolvedSignals: a problem an earlier assessment of the window raised,
// not resolved since, whose number is back now: at or better than before
// it was raised (Problem.Before), not merely no worse than a previous
// period that drifted to the bad level; a jam clearly below the jam
// (JamResolvedMax). Today's number must be judged against the previous
// period too, and no worse there. A resolution names no step (owner: no
// recommendation without a finding behind it).
func (a *assessor) resolvedSignals() {
	// A worsening that turned into a gain is resolved, told once: as the
	// resolution, not as a good change beside it.
	now, gains := map[string]bool{}, map[string]bool{}
	for _, s := range a.out {
		if s.Tone == ToneGood {
			gains[s.ID] = true
		} else {
			now[s.ID] = true
		}
	}
	resolved := map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(a.h.Open)) {
		pb := a.h.Open[id]
		kind, rest, _ := strings.Cut(id, ":")
		if now[id] || !slices.Contains(resolvable, kind) {
			continue
		}
		s := Signal{ID: "resolved:" + id, Kind: SignalResolved, Tone: ToneGood, Weight: 2, Metric: ptr(id)}
		switch kind {
		case SignalPRJam:
			long := a.jamHeld(rest)
			if long > JamResolvedMax {
				continue
			}
			s.Source, s.Repo = SourceBitbucket, ptr(rest)
			s.Facts = []Fact{plain(s.ID+".long", "open_long", UnitCount, f64(long))}
		case SignalFailingMore, SignalSlower:
			runKind, repo, _ := strings.Cut(rest, ":")
			s.Source, s.Repo, s.RunKind = SourcePipeline, ptr(repo), ptr(runKind)
			for _, k := range a.m.Build.Hotspots.Kinds {
				if k.Kind != runKind {
					continue
				}
				for _, h := range k.all {
					if h.Repo != repo {
						continue
					}
					// Judged (enough builds in both periods), no worse than
					// the previous period, and back where it was.
					f, def := fact(s.ID+".p50", "build_p50", UnitSeconds, h.Duration.P50, h.DurationChange), repoDurationMetric
					if kind == SignalFailingMore {
						f = fact(s.ID+".failure_rate", "failure_rate", UnitShare, h.FailureRate, h.FailureChange)
						def = repoFailureMetric
					}
					if f.Prev == nil || f.Verdict == ChangeWorse || !backAt(def.def(), f.Value, pb.Before) {
						continue
					}
					s.Facts = []Fact{f}
				}
			}
		case SignalChange:
			// Only a worsening is a problem to resolve; a gone improvement is
			// no news.
			if pb.Tone != ToneAttention {
				continue
			}
			def, ok := teamMetric(rest)
			for _, t := range a.team {
				// A number gone unknown, or not judged against the previous
				// period, resolves nothing; nor one still worse than that.
				if !ok || t.metric != rest || t.c.Prev == nil || t.c.Verdict == ChangeWorse || !backAt(def, t.value, pb.Before) {
					continue
				}
				s.Source = t.source
				s.Facts = []Fact{fact(s.ID+".value", t.label, t.unit, t.value, t.c)}
				s.Series = a.series(rest)
			}
		}
		if s.Facts != nil && !a.gaps[s.Source] {
			a.add(s)
			resolved[id] = true
		}
	}
	a.out = slices.DeleteFunc(a.out, func(s Signal) bool { return gains[s.ID] && resolved[s.ID] })
}

// jamHeld is the repo's open PRs still holding its jam at the period's
// end: the long-open ones, and those every open number leaves out since,
// unknown (last heard of before Bitbucket's events were lost) or inactive
// for OpenInactiveDays. A jam whose PRs only fell silent is not resolved.
func (a *assessor) jamHeld(repo string) int {
	at, n := a.m.OpenPRs.At, 0
	for _, pr := range openPRs(a.ds, a.f) {
		if pr.Repo == repo && !pr.Draft && (pr.Unknown || openInactive(pr, at) || openLong(pr, at)) {
			n++
		}
	}
	return n
}

// ---- Question --------------------------------------------------------------

// question is the first undirected number in a fixed order that moved
// notably; none when none did. No fixed question beside it: a question
// judged against nothing is no finding (owner, 2026-11).
func (a *assessor) question() *Signal {
	m, c := a.m, a.m.Comparison
	feature, pfeature := m.Review.MixShare.Feature, a.pm.Review.MixShare.Feature
	mixN := func(x Metrics) int {
		t := x.Review.MixTotal
		return t.Feature + t.Bugfix + t.Hotfix + t.Other
	}
	bb := before(m.Meta.DataStart.Bitbucket, a.p.Previous.From)
	cands := []teamChange{
		{"feature_share", "feature_share", UnitShare, SourceBitbucket, feature,
			caseGate(featureShareMetric.judge(side{feature, mixN(m) >= lowSample}, side{pfeature, mixN(a.pm) >= lowSample}, bb),
				feature, pfeature, mixN(m), mixN(a.pm))},
		{"untracked", "untracked", UnitShare, SourceBitbucket, m.Review.UntrackedRate, c.Untracked},
		{"wasted_share", "wasted_share", UnitShare, SourceNoergler, m.Costs.WastedShare, c.WastedShare},
		{"cost_per_kloc", "cost_per_kloc", UnitUSD, SourceNoergler, m.Costs.PerKloc, c.CostPerKloc},
		{"open_prs", "open_prs", UnitCount, SourceBitbucket, f64(m.OpenPRs.Open), c.OpenPRs},
		{"first_touch", "first_touch", UnitShare, SourceBitbucket, m.Review.FirstTouchRate, c.FirstTouch},
	}
	for _, t := range cands {
		t.c = a.unlost(t.metric, t.c)
		if a.gaps[t.source] || t.c.Magnitude == nil || *t.c.Magnitude != MagnitudeNotable {
			continue
		}
		return a.questionOf(t)
	}
	return nil
}

func (a *assessor) questionOf(t teamChange) *Signal {
	id := "question:" + t.metric
	return &Signal{
		ID: id, Kind: SignalQuestion, Tone: ToneTalk, Source: t.source, Metric: ptr(t.metric),
		Facts: []Fact{fact(id+".value", t.label, t.unit, t.value, t.c)}, Drivers: []Driver{},
	}
}

// ---- Focus -----------------------------------------------------------------

// focus ranks the repos by the weight of their signals, a driver of a
// notable worsening counting as an attention signal; candidates need
// FocusMinWeight. It counts out's repos: active, calm, unjudged, idle.
func (a *assessor) focus(out *Assessment) []FocusRepo {
	by := map[string]*FocusRepo{}
	get := func(repo string) *FocusRepo {
		if by[repo] == nil {
			by[repo] = &FocusRepo{Repo: repo, Signals: []string{}, Facts: []Fact{}}
		}
		return by[repo]
	}
	for _, s := range a.out {
		if s.Repo != nil && s.Kind != SignalResolved {
			r := get(*s.Repo)
			r.Weight += s.Weight + min(s.Seen, 1)
			r.Seen = max(r.Seen, s.Seen)
			r.Signals = append(r.Signals, s.ID)
			if len(r.Facts) < 3 {
				r.Facts = append(r.Facts, s.Facts[0])
			}
			if s.Waiting != nil && r.Waiting == nil {
				r.Waiting = s.Waiting
			}
		}
		for _, d := range s.Drivers {
			if d.Share+eps >= DriverShare {
				r := get(d.Repo)
				r.Weight += 3
				r.Signals = append(r.Signals, s.ID)
				if len(r.Facts) < 3 {
					r.Facts = append(r.Facts, Fact{
						ID: s.ID + ".driver." + d.Repo, Label: "review_p50", Unit: UnitSeconds,
						Value: d.Value, Prev: d.Prev, Verdict: ChangeWorse,
					})
				}
			}
		}
	}
	var focus []FocusRepo
	for _, r := range by {
		if r.Weight >= FocusMinWeight {
			focus = append(focus, *r)
		}
	}
	slices.SortFunc(focus, func(x, y FocusRepo) int { return cmp.Or(cmp.Compare(y.Weight, x.Weight), cmp.Compare(x.Repo, y.Repo)) })
	focus = focus[:min(len(focus), FocusCandidates)]
	if focus == nil {
		focus = []FocusRepo{}
	}
	act, merged := activeRepos(a.ds, a.p.Period, a.m, a.f)
	pact, pmerged := activeRepos(a.prev, a.p.Previous, a.pm, a.f)
	out.Active = len(act)
	for repo := range act {
		switch {
		case by[repo] != nil:
		case a.judgeable(repo, merged[repo], pmerged[repo]):
			out.Calm++
		default:
			out.Unjudged++
		}
	}
	for repo := range pact {
		if !act[repo] {
			out.Idle++
		}
	}
	out.Repos = out.Active + out.Idle
	return focus
}

// judgeable reports whether a repo rule had the data to raise something
// on repo: a build kind of it compared with the previous period (the
// hotspots' HotspotMinRuns in both), OpenJamMin open PRs or more at the
// period's end (a jam could be there), or lowSample merged PRs in both
// periods (a review worsening could name it a driver). A repo with less is
// no repo without a finding, only one riptide cannot judge.
func (a *assessor) judgeable(repo string, merged, pmerged int) bool {
	for _, k := range a.m.Build.Hotspots.Kinds {
		if slices.ContainsFunc(k.all, func(h RepoHotspot) bool { return h.Repo == repo && h.Compared }) {
			return true
		}
	}
	if slices.ContainsFunc(a.m.OpenPRs.all, func(r OpenRepo) bool { return r.Repo == repo && r.Open >= OpenJamMin }) {
		return true
	}
	return merged >= lowSample && pmerged >= lowSample
}

// activeRepos are the repos with a merged PR, a build or an open PR in
// the period, in the product, and each one's merged PRs. An open PR
// inactive for OpenInactiveDays makes no repo active: it counts in no
// number but its own.
func activeRepos(ds *dataset.Dataset, p Period, m Metrics, f filter) (map[string]bool, map[string]int) {
	out, merged := map[string]bool{}, map[string]int{}
	for _, pr := range mergedPRs(ds, p, f) {
		out[pr.Repo] = true
		merged[pr.Repo]++
	}
	for _, r := range m.OpenPRs.all {
		out[r.Repo] = true
	}
	for _, repos := range m.Build.repos {
		for repo := range repos {
			out[repo] = true
		}
	}
	return out, merged
}
