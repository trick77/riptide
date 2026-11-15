package metrics

import (
	"math"
	"time"
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
)

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
)

// Change is a number of the page against the previous period.
type Change struct {
	// Prev is the previous period's value; null when the periods do not
	// compare (low_sample, incomplete) or it has none.
	Prev    *float64 `json:"prev"`
	Verdict string   `json:"verdict"`
	// Reason is why the verdict is none; null otherwise.
	Reason *string `json:"reason"`
}

// KindChange is a kind of run's success rate and median duration against
// the previous period's runs of the same kind.
type KindChange struct {
	Kind     string `json:"kind"`
	Success  Change `json:"success"`
	Duration Change `json:"duration"`
}

// Comparison is the page's numbers against the previous period.
type Comparison struct {
	From          time.Time    `json:"from"`
	To            time.Time    `json:"to"`
	DeployFreq    Change       `json:"deploy_frequency"`
	DeploySuccess Change       `json:"deploy_success"`
	LeadTime      Change       `json:"lead_time"`
	LeadTimePre   *Change      `json:"lead_time_pre"`
	ReviewTime    Change       `json:"review_time"`
	WithinDay     Change       `json:"within_day"`
	FirstTouch    Change       `json:"first_touch"`
	Untracked     Change       `json:"untracked"`
	CostPerKloc   Change       `json:"cost_per_kloc"`
	WastedShare   Change       `json:"wasted_share"`
	Build         []KindChange `json:"build"`
}

// direction is which way a number gets better.
type direction int

const (
	up direction = iota
	down
	undirected
)

// scale is how a change is measured: relative for durations and rates,
// in points for shares.
type scale int

const (
	relative scale = iota
	points
)

// side is one period's value and whether it rests on enough data.
type side struct {
	v  *float64
	ok bool
}

func none(reason string) Change { return Change{Verdict: ChangeNone, Reason: ptr(reason)} }

// judge compares cur with prev. incomplete is the previous period's data
// not covering it.
func judge(cur, prev side, d direction, s scale, incomplete bool) Change {
	switch {
	case incomplete:
		return none(ReasonIncomplete)
	case !cur.ok || !prev.ok:
		return none(ReasonLowSample)
	case cur.v == nil || prev.v == nil:
		return none(ReasonNoValue)
	}
	c := Change{Prev: prev.v}
	if d == undirected {
		c.Verdict, c.Reason = ChangeNone, ptr(ReasonUndirected)
		return c
	}
	delta := *cur.v - *prev.v
	switch s {
	case relative:
		// From 0 there is no relative change, but there is a direction: a
		// team that starts deploying moves, one that stays at 0 does not.
		if *prev.v == 0 && *cur.v == 0 || *prev.v != 0 && math.Abs(delta / *prev.v) <= Tolerance+1e-9 {
			c.Verdict = ChangeSame
			return c
		}
	case points:
		if math.Abs(delta) <= ShareTolerance+1e-9 {
			c.Verdict = ChangeSame
			return c
		}
	}
	if (delta > 0) == (d == up) {
		c.Verdict = ChangeBetter
	} else {
		c.Verdict = ChangeWorse
	}
	return c
}

// before reports whether a source's data starts after from, or not at all:
// a period from then on is not covered by it.
func before(start *time.Time, from time.Time) bool { return start == nil || start.After(from) }

// compare is cur against prev, prev computed over period.
func compare(cur, prev Metrics, period Period) Comparison {
	meta := cur.Meta
	from := period.From
	argo := before(meta.DataStart.ArgoCD, from)
	bitbucket := before(meta.DataStart.Bitbucket, from)
	// A build type first reported inside the previous period splits its
	// master runs into unknown and known kinds; one never reported keeps
	// both periods unknown alike.
	t := meta.BuildTypeFrom
	builds := before(meta.BuildDataFrom, from) || (t != nil && t.After(from))
	noergler := before(meta.DataStart.Noergler, from)

	cf, pf := cur.Delivery.Frequency, prev.Delivery.Frequency
	out := Comparison{
		From: period.From, To: period.To,
		// A first deploy is no release. Right after Argo CD started, most
		// revisions are first deploys and the rate reads low; one new App
		// later on costs a single release and still compares.
		DeployFreq: judge(side{ptr(cf.PerWeek), !cf.LowSample && cf.ArgoReporting},
			side{ptr(pf.PerWeek), !pf.LowSample && pf.ArgoReporting}, up, relative,
			argo || pf.FirstSeen > pf.Releases || cf.FirstSeen > cf.Releases),
		DeploySuccess: judge(side{cf.SuccessRate, cf.Finished >= lowSample},
			side{pf.SuccessRate, pf.Finished >= lowSample}, up, points, argo),
		LeadTime: leadChange(cur.Delivery.LeadTime, prev.Delivery.LeadTime, argo || bitbucket),
	}
	if c, p := cur.Delivery.LeadTimePre, prev.Delivery.LeadTimePre; c != nil {
		ch := none(ReasonNoValue)
		if p != nil && p.Environment == c.Environment {
			ch = leadChange(*c, *p, argo || bitbucket)
		}
		out.LeadTimePre = &ch
	}
	cr, pr := cur.Review, prev.Review
	out.ReviewTime = judge(side{cr.Review.P50, cr.Review.N >= lowSample},
		side{pr.Review.P50, pr.Review.N >= lowSample}, down, relative, bitbucket)
	out.WithinDay = judge(side{cr.WithinDay.Rate, cr.WithinDay.N >= lowSample},
		side{pr.WithinDay.Rate, pr.WithinDay.N >= lowSample}, up, points, bitbucket)
	out.FirstTouch = judge(side{cr.FirstTouchRate, cr.Approved >= lowSample},
		side{pr.FirstTouchRate, pr.Approved >= lowSample}, undirected, points, bitbucket)
	out.Untracked = judge(side{cr.UntrackedRate, cr.Merged >= lowSample},
		side{pr.UntrackedRate, pr.Merged >= lowSample}, undirected, points, bitbucket)
	cc, pc := cur.Costs, prev.Costs
	out.CostPerKloc = judge(side{cc.PerKloc, cc.MergedReviews >= lowSample},
		side{pc.PerKloc, pc.MergedReviews >= lowSample}, undirected, relative, noergler)
	out.WastedShare = judge(side{cc.WastedShare, cc.MergedReviews >= lowSample},
		side{pc.WastedShare, pc.MergedReviews >= lowSample}, undirected, points, noergler)

	out.Build = []KindChange{}
	for _, b := range cur.Build.Kinds {
		var p BuildStats
		for _, x := range prev.Build.Kinds {
			if x.Kind == b.Kind {
				p = x
			}
		}
		out.Build = append(out.Build, KindChange{
			Kind: b.Kind,
			Success: judge(side{b.SuccessRate, b.Runs >= lowSample},
				side{p.SuccessRate, p.Runs >= lowSample}, up, points, builds),
			Duration: judge(side{b.Duration.P50, b.Duration.N >= lowSample},
				side{p.Duration.P50, p.Duration.N >= lowSample}, down, relative, builds),
		})
	}
	return out
}

func leadChange(c, p LeadTimes, incomplete bool) Change {
	return judge(side{c.Human.P50, c.Human.N >= lowSample}, side{p.Human.P50, p.Human.N >= lowSample},
		down, relative, incomplete)
}
