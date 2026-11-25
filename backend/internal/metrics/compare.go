package metrics

import (
	"math"
	"slices"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// The page compares a team with itself only: the same number of weeks right
// before. No external benchmark: DORA dropped its performance levels with
// the 2025 report, and its survey distributions are self-reported and
// stale within a year.
const (
	// Tolerance is the relative change a duration or rate may make and
	// still count as the same: noise, not a trend.
	Tolerance = 0.10
	// ShareTolerance is the same for a share, in points of 0..1: relative to
	// a share, 10 % is too coarse (a success rate from 97 % to 88 % has
	// quadrupled its failures).
	ShareTolerance = 0.03
	// SharePointsFull is the change of a share, in points of 0..1, that
	// scores ±1 like a halving or doubling. Shares score linearly: a ratio
	// near 0 % or 100 % explodes (0 % to 3 % failures is infinite) on a change
	// the tolerance calls the same. 25 points keep the tolerance a stub
	// (0.12), as 10 % is on the log scale (0.14).
	SharePointsFull = 0.25
	// CountDamping is added to both sides of a small count before its
	// ratio: one jammed repo more is no doubling. 0 to 1 scores 0.58, 1 to 2
	// 0.42, 10 to 20 still 0.87.
	CountDamping = 2
	// NotableChange is the relative change past which a duration, rate or
	// count is worth naming at all: the tolerance tells noise from movement,
	// this tells movement from news. A starting value, user-chosen
	// 2026-11-16 ("not 1 % less than last period").
	NotableChange = 0.25
	// NotableShareChange is the same for a share, in points of 0..1.
	NotableShareChange = 0.05
	// NotableCountStep is the least a count must move to be news, damped
	// ratio aside: 0 to 1 or 1 to 2 clears NotableChange, but one jammed
	// repo more is no news (user, 2026-11-19).
	NotableCountStep = 2
	// NotableCases: a share over fewer cases than this, in either period,
	// moves notably only by NotableCountStep cases or more. Below it one
	// case is worth NotableShareChange or more: one failed deploy of 12 is
	// 8 points, no news (owner, 2026-11).
	NotableCases = 20
	// NotableReleases: deploy frequency, a rate of releases, on fewer
	// releases than this in either period is news only where the releases
	// moved notably as a count: 1 to 2 releases doubles the rate, and was
	// "notably better" (owner, 2026-11).
	NotableReleases = 10
)

// Magnitudes of a Change with a verdict.
const (
	MagnitudeSame    = "same"
	MagnitudeMinor   = "minor"
	MagnitudeNotable = "notable"
)

// eps absorbs float rounding at a threshold, here and in the build flags:
// 3/20 is no less than 0.1 + 0.05.
const eps = 1e-9

// Verdicts of a Change.
const (
	ChangeBetter = "better"
	ChangeWorse  = "worse"
	ChangeSame   = "same"
	ChangeNone   = "none"
)

// Reasons a Change has no verdict.
const (
	// ReasonLowSample: too few in one of the two periods to tell.
	ReasonLowSample = "low_sample"
	// ReasonIncomplete: the previous period starts before its source's data,
	// or either period has more first deploys than releases (a first deploy
	// is no release), so it reads low.
	ReasonIncomplete = "incomplete"
	// ReasonNoValue: one of the two values is missing.
	ReasonNoValue = "no_value"
	// ReasonUndirected: the number has no better direction; Prev still
	// tells where it was.
	ReasonUndirected = "undirected"
	// ReasonNotReporting: the number's source does not report for the team
	// yet (Argo CD, for the deploy numbers): nothing measured, not too few.
	ReasonNotReporting = "not_reporting"
)

// Change is a number of the page against the previous period.
type Change struct {
	// Prev is the previous period's value; null when the periods do not
	// compare (low_sample, incomplete) or it has none.
	Prev    *float64 `json:"prev"`
	Verdict string   `json:"verdict"`
	// Reason is why the verdict is none; null otherwise.
	Reason *string `json:"reason"`
	// Directed: the number has a better direction, whatever the verdict
	// (an undirected one with too few data says low_sample, not
	// undirected).
	Directed bool `json:"directed"`
	// Score is the change's size, positive better: log2(current /
	// previous), negated where lower is better, clamped to -1..1 (halving,
	// doubling). From 0 to anything is ±1, 0 to 0 is 0; shares score their
	// points per SharePointsFull, counts damped by CountDamping. 0 when the
	// verdict is same, null when it is none.
	Score *float64 `json:"score"`
	// Magnitude is how far it moved: same within the tolerance, notable past
	// NotableChange (NotableShareChange for a share, NotableCountStep on top
	// for a count; a share on few cases and deploy frequency on few releases
	// by such a step too: caseGate, releaseGate), minor between. Null
	// when the verdict is none. Undirected numbers have one too, so a large
	// shift can still be asked about.
	Magnitude *string `json:"magnitude"`
}

// KindChange is a kind of run against the previous period's runs of the
// same kind: its success rate, and its repos each against their own. No
// median duration of the kind: a kind mixes toolchains, and a period with
// more of the slow ones would read as slower builds.
type KindChange struct {
	Kind    string `json:"kind"`
	Success Change `json:"success"`
	// Flaky is BuildStats.FlakyRate against the previous period's, both
	// over lowSample runs with a verdict and a commit or more.
	Flaky Change `json:"flaky"`
	// Slower and FailingMore are the repos flagged slow and failing more.
	Slower      int `json:"slower"`
	FailingMore int `json:"failing_more"`
	// Compared are the repos with enough runs in both periods.
	Compared int `json:"compared"`
}

// Comparison is the page's numbers against the previous period.
type Comparison struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// LastDay is the previous period's last day, as Period's.
	LastDay       string  `json:"last_day"`
	DeployFreq    Change  `json:"deploy_frequency"`
	DeploySuccess Change  `json:"deploy_success"`
	LeadTime      Change  `json:"lead_time"`
	LeadTimePre   *Change `json:"lead_time_pre"`
	ReviewTime    Change  `json:"review_time"`
	WithinDay     Change  `json:"within_day"`
	FirstTouch    Change  `json:"first_touch"`
	Untracked     Change  `json:"untracked"`
	// MergedPerWeek and BatchSize are Throughput's two numbers: more PRs
	// merged and smaller ones are better.
	MergedPerWeek Change       `json:"merged_per_week"`
	BatchSize     Change       `json:"batch_size"`
	CostPerKloc   Change       `json:"cost_per_kloc"`
	WastedShare   Change       `json:"wasted_share"`
	Build         []KindChange `json:"build"`
	// OpenPRs and OpenJammed are the open PRs and the jammed repos now (the
	// page) or at the period's end (the assessment) against those at the
	// previous period's end: more open PRs may
	// be more work under way, more jammed repos are queues.
	OpenPRs    Change `json:"open_prs"`
	OpenJammed Change `json:"open_jammed"`
	// Any: some number above has the previous period's value to stand
	// against (a Prev, verdict or not). Without one nothing was compared:
	// the page names no comparison, the assessment says why.
	Any bool `json:"any"`
}

// anyPrev reports whether some number of c has a previous value.
func (c Comparison) anyPrev() bool {
	cs := []Change{c.DeployFreq, c.DeploySuccess, c.LeadTime, c.ReviewTime, c.WithinDay, c.FirstTouch, c.Untracked,
		c.MergedPerWeek, c.BatchSize, c.CostPerKloc, c.WastedShare, c.OpenPRs, c.OpenJammed}
	if c.LeadTimePre != nil {
		cs = append(cs, *c.LeadTimePre)
	}
	for _, b := range c.Build {
		cs = append(cs, b.Success, b.Flaky)
	}
	return slices.ContainsFunc(cs, func(x Change) bool { return x.Prev != nil })
}

// direction is which way a number gets better.
type direction int

const (
	up direction = iota
	down
	undirected
)

// scale is how a change is measured: relative for durations and rates,
// in points for shares; a count judges as relative, but scores damped.
type scale int

const (
	relative scale = iota
	points
	count
)

// metric is a compared number: which way it gets better and how its change
// is measured. Each number's one definition: its verdict, score, directed
// and no-value fallback all read it.
type metric struct {
	d direction
	s scale
}

// metricID names a compared number; def is its definition, a literal no
// caller can reassign.
type metricID int

const (
	deployFreqMetric metricID = iota
	deploySuccessMetric
	leadTimeMetric
	reviewTimeMetric
	withinDayMetric
	firstTouchMetric
	untrackedMetric
	openPRsMetric
	openJammedMetric
	costPerKlocMetric
	wastedShareMetric
	buildSuccessMetric
	repoDurationMetric
	repoFailureMetric
	buildFlakyMetric
	mergedPerWeekMetric
	batchSizeMetric
	// The assessment's own team numbers: flow and the PR mix.
	wipWeeksMetric
	featureShareMetric
)

func (id metricID) def() metric {
	return [...]metric{
		deployFreqMetric:    {up, relative},
		deploySuccessMetric: {up, points},
		leadTimeMetric:      {down, relative},
		reviewTimeMetric:    {down, relative},
		withinDayMetric:     {up, points},
		firstTouchMetric:    {undirected, points},
		untrackedMetric:     {undirected, points},
		openPRsMetric:       {undirected, count},
		openJammedMetric:    {down, count},
		costPerKlocMetric:   {undirected, relative},
		wastedShareMetric:   {undirected, points},
		buildSuccessMetric:  {up, points},
		repoDurationMetric:  {down, relative},
		repoFailureMetric:   {down, points},
		buildFlakyMetric:    {down, points},
		mergedPerWeekMetric: {up, relative},
		batchSizeMetric:     {down, relative},
		wipWeeksMetric:      {down, relative},
		featureShareMetric:  {undirected, points},
	}[id]
}

func (id metricID) judge(cur, prev side, incomplete bool) Change {
	return id.def().judge(cur, prev, incomplete)
}

func (id metricID) none(reason string) Change { return id.def().none(reason) }

// side is one period's value and whether it rests on enough data.
type side struct {
	v  *float64
	ok bool
}

func (m metric) none(reason string) Change {
	return Change{Verdict: ChangeNone, Reason: ptr(reason), Directed: m.d != undirected}
}

// judge compares cur with prev. incomplete is the previous period's data
// not covering it.
func (m metric) judge(cur, prev side, incomplete bool) Change {
	switch {
	case incomplete:
		return m.none(ReasonIncomplete)
	case !cur.ok || !prev.ok:
		return m.none(ReasonLowSample)
	case cur.v == nil || prev.v == nil:
		return m.none(ReasonNoValue)
	}
	d := m.d
	c := Change{Prev: prev.v, Directed: d != undirected, Magnitude: ptr(magnitude(*cur.v, *prev.v, m.s))}
	if d == undirected {
		c.Verdict, c.Reason = ChangeNone, ptr(ReasonUndirected)
		return c
	}
	c.Verdict = changeVerdict(*cur.v, *prev.v, d, m.s)
	// Within the tolerance the change is noise: no bar on either side.
	s := 0.0
	if c.Verdict != ChangeSame {
		s = score(*cur.v, *prev.v, d, m.s)
	}
	c.Score = &s
	return c
}

// changeVerdict is Change.Verdict of cur against prev, d not undirected.
func changeVerdict(cur, prev float64, d direction, s scale) string {
	delta := cur - prev
	switch s {
	case relative, count:
		// From 0 there is no relative change, but there is a direction: a
		// team that starts deploying moves, one that stays at 0 does not.
		if prev == 0 && cur == 0 || prev != 0 && math.Abs(delta/prev) <= Tolerance+eps {
			return ChangeSame
		}
	case points:
		if math.Abs(delta) <= ShareTolerance+eps {
			return ChangeSame
		}
	}
	if (delta > 0) == (d == up) {
		return ChangeBetter
	}
	return ChangeWorse
}

// magnitude is Change.Magnitude of cur against prev. A count moves
// relative, damped as its score is, and is news only from NotableCountStep:
// one jammed repo more is none.
func magnitude(cur, prev float64, s scale) string {
	delta := math.Abs(cur - prev)
	var moved, notable bool
	switch s {
	case points:
		moved, notable = delta > ShareTolerance+eps, delta+eps >= NotableShareChange
	case count:
		r := math.Abs((cur+CountDamping)/(prev+CountDamping) - 1)
		moved = prev != cur && (prev == 0 || delta/prev > Tolerance+eps)
		notable = r+eps >= NotableChange && delta+eps >= NotableCountStep
	case relative:
		if prev == 0 {
			// From 0 there is no ratio, but any move is news.
			moved, notable = cur != 0, cur != 0
		} else {
			moved, notable = delta/prev > Tolerance+eps, delta/prev+eps >= NotableChange
		}
	}
	switch {
	case notable && moved:
		return MagnitudeNotable
	case moved:
		return MagnitudeMinor
	}
	return MagnitudeSame
}

// caseGate is c with a notable move of a share kept only where it is more
// than a case or so: on fewer than NotableCases cases in either period
// (nCur, nPrev), the move must be NotableCountStep cases of the smaller
// one, else it is minor. Verdict and score stay: the number did move.
func caseGate(c Change, cur, prev *float64, nCur, nPrev int) Change {
	n := min(nCur, nPrev)
	if c.Magnitude == nil || *c.Magnitude != MagnitudeNotable || n >= NotableCases || cur == nil || prev == nil {
		return c
	}
	if math.Abs(*cur-*prev)*float64(n)+eps < NotableCountStep {
		c.Magnitude = ptr(MagnitudeMinor)
	}
	return c
}

// releaseGate is c, deploy frequency, with a notable move kept on fewer
// than NotableReleases releases in either period only where the releases
// themselves moved notably as a count (damped, NotableCountStep or more).
func releaseGate(c Change, cur, prev int) Change {
	if c.Magnitude == nil || *c.Magnitude != MagnitudeNotable || min(cur, prev) >= NotableReleases {
		return c
	}
	if magnitude(float64(cur), float64(prev), count) != MagnitudeNotable {
		c.Magnitude = ptr(MagnitudeMinor)
	}
	return c
}

// score is Change.Score of cur against prev, d not undirected.
func score(cur, prev float64, d direction, s scale) float64 {
	var x float64
	switch {
	case s == points:
		x = (cur - prev) / SharePointsFull
	case s == count:
		x = math.Log2((cur + CountDamping) / (prev + CountDamping))
	case cur == prev:
	case prev == 0 || cur == 0:
		// No ratio, but a direction: past any doubling or halving.
		x = math.Copysign(1, cur-prev)
	default:
		x = math.Log2(cur / prev)
	}
	if d == down {
		x = -x
	}
	// +0 for no change: JSON writes a negated zero as -0.
	return math.Max(-1, math.Min(1, x)) + 0
}

// before reports whether a source's data starts after from, or not at all:
// a period from then on is not covered by it.
func before(start *time.Time, from time.Time) bool { return start == nil || start.After(from) }

// compare is cur against prev, prev computed over period. lost: Bitbucket's
// events were lost in either period (lostEvents), so every number it counts
// reads low there and is not compared; in a product view (product) the
// deploy and build numbers too, which reach a product through Bitbucket's
// commits.
func compare(cur, prev Metrics, period Period, lost, product bool) Comparison {
	meta := cur.Meta
	from := period.From
	argo := before(meta.DataStart.ArgoCD, from)
	bitbucket := before(meta.DataStart.Bitbucket, from)
	builds := buildsIncomplete(meta, from)
	noergler := before(meta.DataStart.Noergler, from)

	cf, pf := cur.Delivery.Frequency, prev.Delivery.Frequency
	out := Comparison{
		From: period.From, To: period.To, LastDay: period.LastDay,
		// A first deploy is no release. Right after Argo CD started, most
		// revisions are first deploys and the rate reads low; one new App
		// later on costs a single release and still compares.
		DeployFreq: deployFreqMetric.judge(side{ptr(cf.PerWeek), !cf.LowSample && cf.ArgoReporting},
			side{ptr(pf.PerWeek), !pf.LowSample && pf.ArgoReporting},
			argo || pf.FirstSeen > pf.Releases || cf.FirstSeen > cf.Releases),
		DeploySuccess: caseGate(deploySuccessMetric.judge(side{cf.SuccessRate, cf.Finished >= lowSample},
			side{pf.SuccessRate, pf.Finished >= lowSample}, argo), cf.SuccessRate, pf.SuccessRate, cf.Finished, pf.Finished),
		LeadTime: leadChange(cur.Delivery.LeadTime, prev.Delivery.LeadTime, argo || bitbucket),
	}
	out.DeployFreq = releaseGate(out.DeployFreq, cf.Releases, pf.Releases)
	if !cf.ArgoReporting {
		out.DeployFreq = deployFreqMetric.none(ReasonNotReporting)
		out.DeploySuccess = deploySuccessMetric.none(ReasonNotReporting)
	}
	if c, p := cur.Delivery.LeadTimePre, prev.Delivery.LeadTimePre; c != nil {
		ch := leadTimeMetric.none(ReasonNoValue)
		if p != nil && p.Environment == c.Environment {
			ch = leadChange(*c, *p, argo || bitbucket)
		}
		out.LeadTimePre = &ch
	}
	cr, pr := cur.Review, prev.Review
	out.ReviewTime = reviewTimeMetric.judge(side{cr.Review.P50, cr.Review.N >= lowSample},
		side{pr.Review.P50, pr.Review.N >= lowSample}, bitbucket)
	out.WithinDay = caseGate(withinDayMetric.judge(side{cr.WithinDay.Rate, cr.WithinDay.N >= lowSample},
		side{pr.WithinDay.Rate, pr.WithinDay.N >= lowSample}, bitbucket),
		cr.WithinDay.Rate, pr.WithinDay.Rate, cr.WithinDay.N, pr.WithinDay.N)
	out.FirstTouch = caseGate(firstTouchMetric.judge(side{cr.FirstTouchRate, cr.Approved >= lowSample},
		side{pr.FirstTouchRate, pr.Approved >= lowSample}, bitbucket), cr.FirstTouchRate, pr.FirstTouchRate,
		cr.Approved, pr.Approved)
	out.Untracked = caseGate(untrackedMetric.judge(side{cr.UntrackedRate, cr.Merged >= lowSample},
		side{pr.UntrackedRate, pr.Merged >= lowSample}, bitbucket), cr.UntrackedRate, pr.UntrackedRate,
		cr.Merged, pr.Merged)
	ct, pt := cur.Throughput, prev.Throughput
	out.MergedPerWeek = mergedPerWeekMetric.judge(side{ct.MergedPerWeek, ct.Merged >= lowSample},
		side{pt.MergedPerWeek, pt.Merged >= lowSample}, bitbucket)
	// The size is noergler's, the PRs Bitbucket's: both must cover it.
	out.BatchSize = batchSizeMetric.judge(side{ct.BatchSize.P50, ct.BatchSize.N >= lowSample},
		side{pt.BatchSize.P50, pt.BatchSize.N >= lowSample}, bitbucket || noergler)
	// A count, no sample: both periods' ends count whatever is open.
	co, po := cur.OpenPRs, prev.OpenPRs
	out.OpenPRs = openPRsMetric.judge(side{ptr(float64(co.Open)), true}, side{ptr(float64(po.Open)), true},
		bitbucket)
	out.OpenJammed = openJammedMetric.judge(side{ptr(float64(co.Jammed)), true},
		side{ptr(float64(po.Jammed)), true}, bitbucket)
	cc, pc := cur.Costs, prev.Costs
	// The wasted share is spend, not cases: one large declined PR may be
	// most of it, and no count of PRs gates it.
	out.CostPerKloc = costPerKlocMetric.judge(side{cc.PerKloc, cc.MergedReviews >= lowSample},
		side{pc.PerKloc, pc.MergedReviews >= lowSample}, noergler)
	out.WastedShare = wastedShareMetric.judge(side{cc.WastedShare, cc.MergedReviews >= lowSample},
		side{pc.WastedShare, pc.MergedReviews >= lowSample}, noergler)

	out.Build = []KindChange{}
	for _, b := range cur.Build.Kinds {
		var p BuildStats
		for _, x := range prev.Build.Kinds {
			if x.Kind == b.Kind {
				p = x
			}
		}
		kc := KindChange{
			Kind: b.Kind,
			Success: caseGate(buildSuccessMetric.judge(side{b.SuccessRate, b.Runs >= lowSample},
				side{p.SuccessRate, p.Runs >= lowSample}, builds), b.SuccessRate, p.SuccessRate, b.Runs, p.Runs),
			Flaky: caseGate(buildFlakyMetric.judge(side{b.FlakyRate, b.judged >= lowSample},
				side{p.FlakyRate, p.judged >= lowSample}, builds), b.FlakyRate, p.FlakyRate, b.judged, p.judged),
			Compared: cur.Build.compared[b.Kind],
		}
		// The hotspots keep every flagged repo past their cut to the top.
		for _, k := range cur.Build.Hotspots.Kinds {
			if k.Kind != b.Kind {
				continue
			}
			for _, h := range k.Repos {
				if h.Slow {
					kc.Slower++
				}
				if h.FailingMore {
					kc.FailingMore++
				}
			}
		}
		out.Build = append(out.Build, kc)
	}
	if lost {
		incomplete := func(cs ...*Change) {
			for _, c := range cs {
				*c = Change{Verdict: ChangeNone, Reason: ptr(ReasonIncomplete), Directed: c.Directed}
			}
		}
		incomplete(&out.ReviewTime, &out.WithinDay, &out.FirstTouch, &out.Untracked, &out.MergedPerWeek, &out.BatchSize,
			&out.OpenPRs, &out.OpenJammed)
		if product {
			incomplete(&out.DeployFreq, &out.DeploySuccess)
			for i := range out.Build {
				incomplete(&out.Build[i].Success, &out.Build[i].Flaky)
			}
		}
	}
	out.Any = out.anyPrev()
	return out
}

// buildsIncomplete: the run data does not cover the previous period from
// from on. A build type first reported inside it also splits its master
// runs into unknown and known kinds; one never reported keeps both periods
// unknown alike.
func buildsIncomplete(meta dataset.Meta, from time.Time) bool {
	t := meta.BuildTypeFrom
	return before(meta.BuildDataFrom, from) || (t != nil && t.After(from))
}

func leadChange(c, p LeadTimes, incomplete bool) Change {
	return leadTimeMetric.judge(side{c.Human.P50, c.Human.N >= lowSample},
		side{p.Human.P50, p.Human.N >= lowSample}, incomplete)
}
