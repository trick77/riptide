package metrics

import (
	"cmp"
	"maps"
	"math"
	"slices"
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
	// FocusMinWeight is the weight a repo's signals need for it to be a
	// focus candidate: one attention signal, or a hint and a notable change.
	FocusMinWeight = 3
	// FocusCandidates are the repos offered for the focus, FocusMax those
	// shown: two or three repos with the most movement, never all of them
	// watered down to a sentence each.
	FocusCandidates = 5
	FocusMax        = 3
	// SteadySlowTop: a repo among the SteadySlowTop slowest of its kind in
	// both periods, and slower than the kind's median, builds slowly as a
	// habit, not as a change. No factor against the median: a kind mixes
	// toolchains, and a factor would claim a slowness a different toolchain
	// explains.
	SteadySlowTop = 3
	// AgentWaitShare: when the median wait for a build agent is this much of
	// the median run or more, the time is the platform's, not the build's.
	AgentWaitShare = 0.5
	// ResyncShare: re-syncs past this part of the syncs, on enough syncs,
	// are deploys that ship nothing.
	ResyncShare = 0.5
	// SilentDays: a source that reported before and is silent this long at
	// the period's end has a gap; its numbers say nothing about the work.
	SilentDays = 7
	// DriverShare: a repo carrying this part of a notable worsening or more
	// is its driver, and counts toward the focus.
	DriverShare = 0.5
	// StageAttention: a stage that takes this part of the time to
	// production or more needs attention; below it the longest stage is
	// only a hint, as some stage always takes longest. A starting value.
	StageAttention = 0.5
)

// Signal kinds.
const (
	SignalDataGap     = "data_gap"
	SignalPRJam       = "pr_jam"
	SignalStage       = "slow_stage"
	SignalSlower      = "slower_build"
	SignalSteadySlow  = "steady_slow"
	SignalFailingMore = "failing_more"
	SignalChange      = "change"
	SignalBottleneck  = "bottleneck_moved"
	SignalTradeoff    = "throughput_instability"
	SignalAgentWait   = "agent_wait"
	SignalSharedStall = "shared_stall"
	SignalInactive    = "inactive_prs"
	SignalResync      = "resync_share"
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
	Stage  *string `json:"stage"`
	// PrevStage is the bottleneck's previous stage.
	PrevStage *string `json:"prev_stage"`
	// LowSample: the signal rests on fewer than 10 cases (a stage's PRs);
	// shown, never a headline.
	LowSample bool `json:"low_sample"`
	// Sample is the cases the signal rests on, where counted (a stage's PRs):
	// the page says how few, not only that LowSample.
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

// History is what earlier assessments of the same window raised: how many
// times in a row each signal ID, and the latest one's signals by ID with
// their tone.
type History struct {
	Seen   map[string]int
	Latest map[string]string
	// Runs are the earlier assessments read; none: the view's first.
	Runs int
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
	// Active are the repos with a PR, a run or an open PR in the period;
	// Calm those of them without a signal.
	Active int `json:"active"`
	Calm   int `json:"calm"`
	// Team are the compared team numbers, every one, for the trend.
	Team []Fact `json:"team"`
	// Question is the one number to talk about.
	Question *Signal `json:"question"`
	// Compared: an assessment of the view for an earlier week was there to
	// tell what is resolved; without one, nothing can be.
	Compared bool `json:"compared"`
}

// WeekStart is the Monday 00:00 of t's week in loc: where ClosedPeriod
// ends, and the schedule's week.
func WeekStart(t time.Time, loc *time.Location) time.Time { return weekStart(t, loc) }

// ClosedPeriod is the given number of whole calendar weeks ending at the
// start of now's week: an assessment never judges a week still running.
func ClosedPeriod(weeks int, now time.Time, loc *time.Location) Period {
	end := weekStart(now, loc)
	return periodFrom(end.AddDate(0, 0, -7*weeks), weeks, end)
}

// ClosedPrevious is the period ClosedPeriod compares with: the weeks right
// before it.
func ClosedPrevious(weeks int, now time.Time, loc *time.Location) Period {
	end := weekStart(now, loc).AddDate(0, 0, -7*weeks)
	return periodFrom(end.AddDate(0, 0, -7*weeks), weeks, end)
}

// extra compared numbers, the assessment's own: the page shows them
// without a verdict.
const (
	mergedPerWeekMetric metricID = iota + 100
	wipWeeksMetric
	waitShareMetric
	featureShareMetric
)

func (id metricID) judgeX(cur, prev side, incomplete bool) Change {
	def := map[metricID]metric{
		mergedPerWeekMetric: {up, relative},
		wipWeeksMetric:      {down, relative},
		waitShareMetric:     {down, points},
		featureShareMetric:  {undirected, points},
	}[id]
	return def.judge(cur, prev, incomplete)
}

// Assess is the assessment of ds over p.Period against prev over
// p.Previous, both closed weeks (ClosedPeriod), with what earlier ones
// raised.
func Assess(ds, prev *dataset.Dataset, p Params, h History) Assessment {
	m, pm := computeBoth(ds, prev, nil, p)
	if prev == nil {
		prev = &dataset.Dataset{Meta: ds.Meta}
	}
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
	out      []Signal
	// team are the compared team numbers, computed once.
	team []teamChange
	// held are the gains a trade-off holds back: no signal, still there.
	held map[string]bool
}

func (a *assessor) run() Assessment {
	a.team = a.teamChanges()
	a.gapSignals()
	a.jamSignals()
	a.buildSignals()
	a.changeSignals()
	a.flowSignals()
	a.deploySignals()
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
		Signals: a.out, Question: a.question(), Compared: a.h.Runs > 0,
	}
	out.Focus, out.Active, out.Calm = a.focus()
	out.Team = []Fact{}
	for _, t := range a.team {
		if !a.gaps[t.source] {
			out.Team = append(out.Team, fact("team."+t.metric, t.label, t.unit, t.value, t.c))
		}
	}
	if out.Signals == nil {
		out.Signals = []Signal{}
	}
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

// ---- Gaps ------------------------------------------------------------------

// gapSignals silence a source that reported before and went quiet for
// SilentDays at the period's end: a dead webhook must not read as a team
// that stopped deploying.
func (a *assessor) gapSignals() {
	a.gaps = map[string]bool{}
	end := a.p.Period.To
	ends := a.m.Meta.DataEnd
	for _, s := range []struct {
		name string
		at   *time.Time
	}{
		{SourceBitbucket, ends.Bitbucket}, {SourcePipeline, ends.Pipeline},
		{SourceArgoCD, ends.ArgoCD}, {SourceNoergler, ends.Noergler},
	} {
		if s.at == nil || end.Sub(*s.at) < SilentDays*24*time.Hour {
			continue
		}
		a.gaps[s.name] = true
		days := float64(int(end.Sub(*s.at).Hours() / 24))
		a.out = append(a.out, Signal{
			ID: "data_gap:" + s.name, Kind: SignalDataGap, Tone: ToneGate, Source: s.name, Weight: 10,
			Facts: []Fact{plain("data_gap_"+s.name+".silent", "silent_days", UnitDays, &days)},
		})
	}
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
	oj := a.m.Comparison.OpenJammed
	more := oj.Verdict == ChangeWorse && oj.Magnitude != nil && *oj.Magnitude == MagnitudeNotable
	for _, r := range a.m.OpenPRs.Repos {
		if !r.Jammed {
			continue
		}
		id := "pr_jam:" + r.Repo
		// A jam and a steady slow build make their repo a focus candidate
		// alone: the two patterns the assessment is there to name.
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
		if incomplete := before(a.m.Meta.DataStart.Bitbucket, a.p.Previous.From); !incomplete {
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
	if n := a.m.OpenPRs.Inactive; n > 0 {
		a.add(Signal{
			ID: "inactive_prs", Kind: SignalInactive, Tone: ToneHint, Source: SourceBitbucket, Weight: 1,
			Facts: []Fact{plain("inactive_prs.count", "inactive", UnitCount, f64(n))},
		})
	}
}

// ---- Builds ----------------------------------------------------------------

func (a *assessor) buildSignals() {
	// Among the slowest and slower than the kind's median: with three repos
	// in a kind, every one is among the top three.
	top := func(k KindHotspots) []RepoHotspot {
		var out []RepoHotspot
		for _, h := range k.Slowest[:min(len(k.Slowest), SteadySlowTop)] {
			if k.Duration.P50 != nil && *h.Duration.P50 > *k.Duration.P50+eps {
				out = append(out, h)
			}
		}
		return out
	}
	prevTop := map[string]map[string]bool{}
	for _, k := range a.pm.Build.Hotspots.Kinds {
		prevTop[k.Kind] = map[string]bool{}
		for _, h := range top(k) {
			prevTop[k.Kind][h.Repo] = true
		}
	}
	for _, k := range a.m.Build.Hotspots.Kinds {
		kind := k.Kind
		steady := map[string]bool{}
		for _, h := range top(k) {
			steady[h.Repo] = prevTop[kind][h.Repo]
		}
		for _, h := range k.Repos {
			// Slower than its own previous period: a change, not a habit.
			// One also slow as a habit is the steady signal's, attention.
			if h.Slow && !steady[h.Repo] {
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
		for _, h := range top(k) {
			if !prevTop[kind][h.Repo] {
				continue
			}
			id := "steady_slow:" + kind + ":" + h.Repo
			tone, w := ToneHint, FocusMinWeight
			if h.Slow {
				tone, w = ToneAttention, FocusMinWeight+1
			}
			a.add(Signal{
				ID: id, Kind: SignalSteadySlow, Tone: tone, Source: SourcePipeline, Repo: ptr(h.Repo),
				RunKind: ptr(kind), Weight: w,
				Facts: []Fact{fact(id+".p50", "build_p50", UnitSeconds, h.Duration.P50, h.DurationChange),
					plain(id+".kind_p50", "kind_p50", UnitSeconds, k.Duration.P50),
					plain(id+".wait", "build_wait_p50", UnitSeconds, h.Waiting.P50),
					plain(id+".work", "build_work_p50", UnitSeconds, h.Building.P50)},
			})
		}
	}
	for _, b := range a.m.Build.Kinds {
		if b.Waiting.N < lowSample || b.Waiting.P50 == nil || b.Duration.P50 == nil || *b.Duration.P50 <= 0 {
			continue
		}
		if *b.Waiting.P50 / *b.Duration.P50 + eps < AgentWaitShare {
			continue
		}
		id := "agent_wait:" + b.Kind
		a.add(Signal{
			ID: id, Kind: SignalAgentWait, Tone: ToneAttention, Source: SourcePipeline, RunKind: ptr(b.Kind),
			Weight: 2,
			Facts: []Fact{plain(id+".wait", "build_wait_p50", UnitSeconds, b.Waiting.P50),
				plain(id+".p50", "build_p50", UnitSeconds, b.Duration.P50)},
		})
	}
	shared := 0
	for _, g := range a.m.Build.Hotspots.OutlierGroups {
		if g.Shared {
			shared++
		}
	}
	if shared > 0 {
		a.add(Signal{
			ID: "shared_stall", Kind: SignalSharedStall, Tone: ToneHint, Source: SourcePipeline, Weight: 1,
			Facts: []Fact{plain("shared_stall.groups", "stall_groups", UnitCount, f64(shared))},
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
	// Flow numbers: throughput, work in progress against it, waiting.
	merged, pmerged := a.mergedPerWeek(a.m), a.mergedPerWeek(a.pm)
	ok, pok := a.m.Review.Cycle.N >= lowSample, a.pm.Review.Cycle.N >= lowSample
	out = append(out, teamChange{"merged_per_week", "merged_per_week", UnitPerWeek, SourceBitbucket, merged,
		mergedPerWeekMetric.judgeX(side{merged, ok}, side{pmerged, pok}, bb)})
	wip, pwip := wipWeeks(a.m, merged), wipWeeks(a.pm, pmerged)
	out = append(out, teamChange{"wip_weeks", "wip_weeks", UnitWeeks, SourceBitbucket, wip,
		wipWeeksMetric.judgeX(side{wip, ok}, side{pwip, pok}, bb)})
	ws, wok := waitShare(a.m.LeadChain)
	pws, pwok := waitShare(a.pm.LeadChain)
	out = append(out, teamChange{"wait_share", "wait_share", UnitShare, SourceBitbucket, ws,
		waitShareMetric.judgeX(side{ws, wok}, side{pws, pwok}, bb)})
	return out
}

func (a *assessor) mergedPerWeek(m Metrics) *float64 {
	if m.Period.Span <= 0 {
		return nil
	}
	return ptr(float64(m.Review.Cycle.N) / m.Period.Span)
}

// wipWeeks is the open PRs at the period's end over the PRs merged per
// week: how many weeks of work stand open (Little's law).
func wipWeeks(m Metrics, merged *float64) *float64 {
	if merged == nil || *merged <= 0 {
		return nil
	}
	return ptr(float64(m.OpenPRs.Open) / *merged)
}

// waitShare is the part of the summed stage medians that is not coding:
// waiting for review, merge and deploys. Nil when the chain does not reach
// production: without the production wait it would be a part of the time
// to merge.
func waitShare(c Chain) (*float64, bool) {
	for _, s := range c.Stages {
		if s.Key == StageCoding && s.Share != nil {
			return ptr(1 - *s.Share), !s.LowSample
		}
	}
	return nil, false
}

// changeSignals are the moves of the directed team numbers: every better
// one past the tolerance, as what runs needs evidence and a small gain is
// one; a worse one only when notable, or the stuck side fills with noise
// (user, 2026-11-18). The worse ones carry the repos that drive them where
// riptide can tell.
func (a *assessor) changeSignals() {
	a.held = map[string]bool{}
	m := a.m
	cf, ds := m.Comparison.DeployFreq, m.Comparison.DeploySuccess
	tradeoff := (cf.Verdict == ChangeBetter && ds.Verdict == ChangeWorse) ||
		(cf.Verdict == ChangeWorse && ds.Verdict == ChangeBetter)
	for _, t := range a.team {
		c := t.c
		if c.Magnitude == nil || !c.Directed {
			continue
		}
		good := c.Verdict == ChangeBetter && *c.Magnitude != MagnitudeSame
		bad := c.Verdict == ChangeWorse && *c.Magnitude == MagnitudeNotable
		if !good && !bad {
			continue
		}
		id := "change:" + t.metric
		// The better half of a throughput / instability pair is no progress
		// (the trade-off's own claim): never a good card beside it, and no
		// resolution of its earlier worsening either.
		if good && tradeoff && (t.metric == "deploy_frequency" || t.metric == "deploy_success") {
			a.held[id] = true
			continue
		}
		s := Signal{
			ID: id, Kind: SignalChange, Tone: ToneGood, Source: t.source, Metric: ptr(t.metric), Weight: 2,
			Facts: []Fact{fact(id+".value", t.label, t.unit, t.value, c)},
		}
		if kind, ok := strings.CutPrefix(t.metric, "build_success:"); ok {
			s.RunKind = ptr(kind)
		}
		s.Series = a.series(t.metric)
		if bad {
			s.Tone = ToneAttention
			if t.metric == "review_time" {
				s.Drivers = a.reviewDrivers()
			}
		} else if c.Score != nil {
			s.score = math.Abs(*c.Score)
		}
		a.add(s)
	}
	if tradeoff {
		a.add(Signal{
			ID: "throughput_instability", Kind: SignalTradeoff, Tone: ToneHint, Source: SourceArgoCD, Weight: 2,
			Facts: []Fact{fact("throughput_instability.freq", "deploy_per_week", UnitPerWeek, ptr(m.Delivery.Frequency.PerWeek), cf),
				fact("throughput_instability.success", "deploy_success", UnitShare, m.Delivery.Frequency.SuccessRate, ds)},
		})
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

// ---- Flow and deploys ------------------------------------------------------

func hotStage(c Chain) *Stage {
	for i := range c.Stages {
		if c.Stages[i].Hot {
			return &c.Stages[i]
		}
	}
	return nil
}

// flowSignals: the stage that takes longest, attention past StageAttention
// of the time to production on enough PRs (some stage always takes
// longest, so below that a hint); and another one than before.
func (a *assessor) flowSignals() {
	cur, prev := hotStage(a.m.LeadChain), hotStage(a.pm.LeadChain)
	if cur != nil && cur.Share != nil {
		tone, w := ToneHint, 1
		if *cur.Share+eps >= StageAttention && !cur.LowSample {
			tone, w = ToneAttention, 3
		}
		a.add(Signal{
			ID: "slow_stage:" + cur.Key, Kind: SignalStage, Tone: tone, Source: SourceBitbucket, Weight: w,
			Stage: ptr(cur.Key), LowSample: cur.LowSample, Sample: ptr(cur.N),
			Facts: []Fact{plain("slow_stage.share", "stage_share", UnitShare, cur.Share),
				plain("slow_stage.p50", "stage_p50", UnitSeconds, cur.P50),
				plain("slow_stage.p90", "stage_p90", UnitSeconds, cur.P90)},
		})
	}
	if cur == nil || prev == nil || cur.LowSample || prev.LowSample || cur.Key == prev.Key {
		return
	}
	a.add(Signal{
		ID: "bottleneck_moved", Kind: SignalBottleneck, Tone: ToneHint, Source: SourceBitbucket, Weight: 2,
		Stage: ptr(cur.Key), PrevStage: ptr(prev.Key),
		Facts: []Fact{plain("bottleneck_moved.share", "stage_share", UnitShare, cur.Share),
			plain("bottleneck_moved.p50", "stage_p50", UnitSeconds, cur.P50)},
	})
}

// deploySignals: syncs that ship nothing.
func (a *assessor) deploySignals() {
	f := a.m.Delivery.Frequency
	if f.Syncs < lowSample {
		return
	}
	share := ratio(f.Resyncs, f.Syncs)
	if share == nil || *share+eps < ResyncShare {
		return
	}
	a.add(Signal{
		ID: "resync_share", Kind: SignalResync, Tone: ToneHint, Source: SourceArgoCD, Weight: 1,
		Facts: []Fact{plain("resync_share.share", "resync_share", UnitShare, share),
			plain("resync_share.syncs", "syncs", UnitCount, f64(f.Syncs))},
	})
}

// resolvedSignals: what the latest assessment of the window raised as a
// problem and is gone now, with today's number where there is one.
func (a *assessor) resolvedSignals() {
	// A worsening that turned into a gain is resolved, told once: as the
	// resolution, not as a good change beside it.
	now, gains := map[string]bool{}, map[string]bool{}
	maps.Copy(now, a.held)
	for _, s := range a.out {
		if s.Tone == ToneGood {
			gains[s.ID] = true
		} else {
			now[s.ID] = true
		}
	}
	resolved := map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(a.h.Latest)) {
		kind, rest, _ := strings.Cut(id, ":")
		if now[id] || !slices.Contains([]string{SignalPRJam, SignalFailingMore, SignalSteadySlow, SignalSlower, SignalChange}, kind) {
			continue
		}
		// Slower and steady slow are one problem under two names: a repo
		// that got slower and stays slow moves from one to the other, and is
		// not resolved.
		if (kind == SignalSlower && now[SignalSteadySlow+":"+rest]) || (kind == SignalSteadySlow && now[SignalSlower+":"+rest]) {
			continue
		}
		s := Signal{ID: "resolved:" + id, Kind: SignalResolved, Tone: ToneGood, Weight: 2, Metric: ptr(id)}
		switch kind {
		case SignalPRJam:
			s.Source, s.Repo = SourceBitbucket, ptr(rest)
			for _, r := range a.m.OpenPRs.all {
				if r.Repo == rest {
					s.Facts = []Fact{plain(s.ID+".long", "open_long", UnitCount, f64(r.Long))}
				}
			}
			if s.Facts == nil {
				s.Facts = []Fact{plain(s.ID+".long", "open_long", UnitCount, ptr(0.0))}
			}
		case SignalFailingMore, SignalSteadySlow, SignalSlower:
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
					// Resolved only on a number judged and no worse than
					// before: a low sample or a repo still worse is not done,
					// nor a steady slow build with too few runs to rank.
					f := fact(s.ID+".p50", "build_p50", UnitSeconds, h.Duration.P50, h.DurationChange)
					if kind == SignalFailingMore {
						f = fact(s.ID+".failure_rate", "failure_rate", UnitShare, h.FailureRate, h.FailureChange)
					}
					if f.Prev == nil || f.Verdict == ChangeWorse || kind == SignalSteadySlow && h.Duration.N < SlowestMinRuns {
						continue
					}
					s.Facts = []Fact{f}
				}
			}
		case SignalChange:
			// Only a worsening is a problem to resolve; a gone improvement is
			// no news.
			if a.h.Latest[id] != ToneAttention {
				continue
			}
			// A number gone unknown (no PR reached production: no wait
			// share) is no resolution: say nothing. Nor is one still worse,
			// only no longer notably: it is not done.
			for _, t := range a.team {
				if t.metric == rest && t.value != nil && t.c.Verdict != ChangeWorse {
					s.Source = t.source
					s.Facts = []Fact{fact(s.ID+".value", t.label, t.unit, t.value, t.c)}
					s.Series = a.series(rest)
				}
			}
		}
		if s.Facts != nil && !a.gaps[s.Source] {
			a.add(s)
			resolved[id] = true
		}
	}
	a.out = slices.DeleteFunc(a.out, func(s Signal) bool { return gains[s.ID] && resolved[s.ID] })
}

// ---- Question --------------------------------------------------------------

// question is the undirected number that moved most, notable only; else
// the approvals without a comment, the page's fixed one.
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
			featureShareMetric.judgeX(side{feature, mixN(m) >= lowSample}, side{pfeature, mixN(a.pm) >= lowSample}, bb)},
		{"untracked", "untracked", UnitShare, SourceBitbucket, m.Review.UntrackedRate, c.Untracked},
		{"wasted_share", "wasted_share", UnitShare, SourceNoergler, m.Costs.WastedShare, c.WastedShare},
		{"cost_per_kloc", "cost_per_kloc", UnitUSD, SourceNoergler, m.Costs.PerKloc, c.CostPerKloc},
		{"open_prs", "open_prs", UnitCount, SourceBitbucket, f64(m.OpenPRs.Open), c.OpenPRs},
		{"first_touch", "first_touch", UnitShare, SourceBitbucket, m.Review.FirstTouchRate, c.FirstTouch},
	}
	for _, t := range cands {
		if a.gaps[t.source] || t.c.Magnitude == nil || *t.c.Magnitude != MagnitudeNotable {
			continue
		}
		return a.questionOf(t)
	}
	if m.Review.Approved > 0 && !a.gaps[SourceBitbucket] {
		return a.questionOf(cands[len(cands)-1])
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
// FocusMinWeight. Active counts the repos with any activity, Calm those
// of them without a signal.
func (a *assessor) focus() (out []FocusRepo, active, calm int) {
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
	for _, r := range by {
		if r.Weight >= FocusMinWeight {
			out = append(out, *r)
		}
	}
	slices.SortFunc(out, func(x, y FocusRepo) int { return cmp.Or(cmp.Compare(y.Weight, x.Weight), cmp.Compare(x.Repo, y.Repo)) })
	out = out[:min(len(out), FocusCandidates)]
	if out == nil {
		out = []FocusRepo{}
	}
	act := a.activeRepos()
	active = len(act)
	for repo := range act {
		if by[repo] == nil {
			calm++
		}
	}
	return out, active, calm
}

// activeRepos are the repos with a merged PR, a run or an open PR in the
// period, in the product. An open PR inactive for OpenInactiveDays makes no
// repo active: it counts in no number but its own.
func (a *assessor) activeRepos() map[string]bool {
	out := map[string]bool{}
	for _, pr := range mergedPRs(a.ds, a.p.Period, a.f) {
		out[pr.Repo] = true
	}
	for _, r := range a.m.OpenPRs.all {
		out[r.Repo] = true
	}
	for _, repos := range a.m.Build.repos {
		for repo := range repos {
			out[repo] = true
		}
	}
	return out
}
