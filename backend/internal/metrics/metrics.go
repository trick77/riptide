package metrics

import (
	"cmp"
	"math/big"
	"slices"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// lowSample is the sample size below which a number is flagged as weak.
const lowSample = 10

// Params selects what the page shows of a dataset.
type Params struct {
	Period Period
	// Product is a Bitbucket project key; empty, or one the dataset does
	// not know, is all products.
	Product string
	// Bots counts bots' PRs in (on their own series where a chart has one).
	Bots bool
}

// filter is the product and bot selection, the product resolved against
// the dataset.
type filter struct {
	product string
	bots    bool
}

func (f filter) in(project *string) bool {
	return f.product == "" || (project != nil && *project == f.product)
}

func (f filter) inKey(project string) bool { return f.product == "" || project == f.product }

func newFilter(ds *dataset.Dataset, p Params) filter {
	f := filter{bots: p.Bots}
	if slices.ContainsFunc(ds.Products, func(x dataset.Product) bool { return x.Key == p.Product }) {
		f.product = p.Product
	}
	return f
}

// Metrics is the /api/metrics body: every number of the page.
type Metrics struct {
	Meta     dataset.Meta      `json:"meta"`
	Period   Period            `json:"period"`
	Products []dataset.Product `json:"products"`
	// Product is the product shown; null for all, also when the one asked
	// for has no data in the window.
	Product *string `json:"product"`
	// Bots is whether bots' PRs are counted in, as asked.
	Bots      bool     `json:"bots"`
	Delivery  Delivery `json:"delivery"`
	LeadChain Chain    `json:"lead_chain"`
	Review    Review   `json:"review"`
	Build     Build    `json:"build"`
	Costs     Costs    `json:"costs"`
}

// Compute is every metric of the page.
func Compute(ds *dataset.Dataset, p Params) Metrics {
	f := newFilter(ds, p)
	humans := mergedPRs(ds, p.Period, f, false)
	ch := chain(ds, humans)
	m := Metrics{
		Meta:      ds.Meta,
		Period:    p.Period,
		Products:  ds.Products,
		Bots:      f.bots,
		Delivery:  delivery(ds, p.Period, f, ch),
		LeadChain: leadChain(ch),
		Review:    review(humans, mergedPRs(ds, p.Period, f, f.bots), p.Period, ch),
		Build:     build(ds, p.Period, f),
		Costs:     costs(ds, f),
	}
	if f.product != "" {
		m.Product = ptr(f.product)
	}
	return m
}

// ---- Deploys ---------------------------------------------------------------

// Revision classes, ranked: a revision is a release if it brought new
// images to any App, else a first deploy if it was some App's first, else
// a re-sync.
const (
	classSync = iota
	classFirstSeen
	classRelease
)

func revisionClass(d dataset.Deploy) int {
	switch {
	case d.Kind != nil && *d.Kind == "release":
		return classRelease
	case d.Kind != nil && *d.Kind == "first_seen":
		return classFirstSeen
	}
	return classSync
}

// DeployWeek is one week's revisions by class.
type DeployWeek struct {
	Release   int `json:"release"`
	Sync      int `json:"sync"`
	FirstSeen int `json:"first_seen"`
}

// DeployFrequency is the production deploy frequency over the period.
type DeployFrequency struct {
	// Releases, Syncs, FirstSeen count distinct GitOps revisions: a release
	// reconciles every App sharing its revision, so it counts once.
	Releases  int `json:"releases"`
	Syncs     int `json:"syncs"`
	FirstSeen int `json:"first_seen"`
	// Resyncs are the revisions that brought no new images.
	Resyncs int `json:"resyncs"`
	// PerWeek is releases per week over the period, or over the part of it
	// since Argo CD started reporting.
	PerWeek float64 `json:"per_week"`
	// LowSample: the rate rests on less than a week of Argo CD data, so one
	// release reads as many per week.
	LowSample bool `json:"low_sample"`
	// Since is when Argo CD's data starts inside the period; null when it
	// covers it.
	Since       *time.Time   `json:"since"`
	Weeks       []DeployWeek `json:"weeks"`
	Succeeded   int          `json:"succeeded"`
	Failed      int          `json:"failed"`
	Finished    int          `json:"finished"`
	SuccessRate *float64     `json:"success_rate"`
	// ArgoReporting: Argo CD reported for the team before the period's end.
	ArgoReporting bool `json:"argo_reporting"`
}

func deployFrequency(ds *dataset.Dataset, p Period, f filter, env string) DeployFrequency {
	var ok []dataset.Deploy
	failed := 0
	for _, d := range ds.Deploys {
		if d.Environment == nil || *d.Environment != env || !f.in(d.Project) || !p.In(&d.At) {
			continue
		}
		switch d.Phase {
		case "Succeeded":
			ok = append(ok, d)
		case "Failed":
			failed++
		}
	}
	class := map[string]int{}
	firstAt := map[string]time.Time{}
	var order []string
	for _, d := range ok {
		c := revisionClass(d)
		prev, seen := class[d.Revision]
		if !seen {
			order = append(order, d.Revision)
		}
		if !seen || c > prev {
			class[d.Revision] = c
		}
		if t, seen := firstAt[d.Revision]; !seen || d.At.Before(t) {
			firstAt[d.Revision] = d.At
		}
	}
	out := DeployFrequency{Weeks: make([]DeployWeek, len(p.Weeks))}
	// Each revision in the week of its first deploy in the period, with its
	// class over the period: the bars add up to the totals, a release rolled
	// out across a week boundary counts once.
	for _, rev := range order {
		w := &out.Weeks[p.WeekIndex(firstAt[rev])]
		switch class[rev] {
		case classRelease:
			w.Release++
			out.Releases++
		case classFirstSeen:
			w.FirstSeen++
			out.FirstSeen++
		default:
			w.Sync++
		}
	}
	out.Syncs = len(order)
	out.Resyncs = out.Syncs - out.Releases - out.FirstSeen
	// Argo CD started reporting inside the period: the rate is over the
	// weeks it reported, not the empty ones before.
	span := p.Span
	if start := ds.Meta.DataStart.ArgoCD; start != nil && start.After(p.From) {
		out.Since = start
		span = float64(p.To.Sub(*start)) / float64(7*24*time.Hour)
	}
	if span > 0 {
		out.PerWeek = float64(out.Releases) / span
	}
	out.LowSample = span < 1
	out.Succeeded = len(ok)
	out.Failed = failed
	out.Finished = out.Succeeded + out.Failed
	out.SuccessRate = ratio(out.Succeeded, out.Finished)
	start := ds.Meta.DataStart.ArgoCD
	out.ArgoReporting = start != nil && start.Before(p.To)
	return out
}

// environments are the environments the product's deploys went to, over
// the whole dataset, the production stage last, the rest by how often they
// were deployed to.
func environments(ds *dataset.Dataset, f filter) []string {
	n := map[string]int{}
	for _, d := range ds.Deploys {
		if d.Environment != nil && *d.Environment != "" && f.in(d.Project) {
			n[*d.Environment]++
		}
	}
	prod := ds.Meta.ProductionStage
	var out []string
	for e := range n {
		if e != prod {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		return cmp.Or(cmp.Compare(n[b], n[a]), cmp.Compare(a, b))
	})
	if n[prod] > 0 {
		out = append(out, prod)
	}
	return out
}

// LeadTimes is the per-commit lead time to one environment.
type LeadTimes struct {
	Environment string `json:"environment"`
	Human       Spread `json:"human"`
	Bot         Spread `json:"bot"`
	// LowSample: fewer than 10 human commits, but some.
	LowSample bool `json:"low_sample"`
}

// leadTime is per commit, first deploy to the environment in the window:
// merge commits and release tooling left out, bots on their own line
// (docs/dora-lead-time.md). The dataset's window is the period.
func leadTime(ds *dataset.Dataset, f filter, env string) LeadTimes {
	var human, bot []float64
	for _, l := range ds.LeadTimes {
		if l.Environment != env || !f.inKey(l.Project) || l.IsMerge || l.ServiceAccount {
			continue
		}
		if l.Bot {
			bot = append(bot, float64(l.LeadTimeS))
		} else {
			human = append(human, float64(l.LeadTimeS))
		}
	}
	h := spread(human)
	return LeadTimes{Environment: env, Human: h, Bot: spread(bot), LowSample: h.N > 0 && h.N < lowSample}
}

// Delivery is the DORA section.
type Delivery struct {
	ProductionStage string `json:"production_stage"`
	// PreEnvironment is the stage before production the lead-time breakdown
	// names, else the product's most deployed one; null when there is none.
	PreEnvironment *string         `json:"pre_environment"`
	Frequency      DeployFrequency `json:"frequency"`
	LeadTime       LeadTimes       `json:"lead_time"`
	LeadTimePre    *LeadTimes      `json:"lead_time_pre"`
}

func delivery(ds *dataset.Dataset, p Period, f filter, ch rawChain) Delivery {
	prod := ds.Meta.ProductionStage
	pre := ch.preEnvironment
	if pre == nil {
		for _, e := range environments(ds, f) {
			if e != prod {
				pre = ptr(e)
				break
			}
		}
	}
	d := Delivery{
		ProductionStage: prod,
		PreEnvironment:  pre,
		Frequency:       deployFrequency(ds, p, f, prod),
		LeadTime:        leadTime(ds, f, prod),
	}
	if pre != nil {
		d.LeadTimePre = ptr(leadTime(ds, f, *pre))
	}
	return d
}

// ---- PRs -------------------------------------------------------------------

// mergedPRs are the PRs merged in the period, GitOps promotions left out,
// bots only if asked.
func mergedPRs(ds *dataset.Dataset, p Period, f filter, bots bool) []dataset.PR {
	var out []dataset.PR
	for _, pr := range ds.PRs {
		if !pr.GitOps && (bots || !pr.Bot) && pr.State == "merged" && f.inKey(pr.Project) && p.In(pr.MergedAt) {
			out = append(out, pr)
		}
	}
	return out
}

// Stage keys of the lead-time breakdown, in order.
const (
	StageCoding = "coding"
	StageReview = "review"
	StageMerge  = "merge"
	StageToPre  = "to_pre"
	StageToProd = "to_prod"
)

var stageKeys = []string{StageCoding, StageReview, StageMerge, StageToPre, StageToProd}

type rawChain struct {
	waits                          map[string][]float64
	preEnvironment                 *string
	approved, approvedOnFirstTouch int
}

func firstDeploy(pr dataset.PR, env string) *time.Time {
	for _, e := range pr.Environments {
		if e.Environment == env {
			return ptr(e.FirstDeployedAt)
		}
	}
	return nil
}

// chain is the lead-time breakdown query, per human PR merged in the period
// (humans, from mergedPRs).
func chain(ds *dataset.Dataset, humans []dataset.PR) rawChain {
	prod := ds.Meta.ProductionStage
	var prs []dataset.PR
	for _, pr := range humans {
		if pr.ReadyAt != nil {
			prs = append(prs, pr)
		}
	}
	// The stage before production: the one the merge commits reached first
	// most often, ties by name.
	pre := map[string]int{}
	for _, pr := range prs {
		for _, e := range pr.Environments {
			if e.Environment != prod {
				pre[e.Environment]++
				break
			}
		}
	}
	var preEnv *string
	for e, n := range pre {
		if preEnv == nil || n > pre[*preEnv] || (n == pre[*preEnv] && e < *preEnv) {
			preEnv = ptr(e)
		}
	}
	c := rawChain{waits: map[string][]float64{}, preEnvironment: preEnv}
	// The waits the facts table shows per PR: unknown or negative ones out.
	add := func(k string, from, to *time.Time) {
		if w := wait(from, to); w != nil {
			c.waits[k] = append(c.waits[k], *w)
		}
	}
	for _, pr := range prs {
		add(StageCoding, pr.FirstCommitAt, pr.ReadyAt)
		add(StageReview, pr.ReadyAt, pr.ApprovedAt)
		add(StageMerge, pr.ApprovedAt, pr.MergedAt)
		var preAt *time.Time
		if preEnv != nil {
			preAt = firstDeploy(pr, *preEnv)
		}
		prodAt := firstDeploy(pr, prod)
		// The stage before production counts only when it came before
		// production: a hotfix deployed to production first and carried to
		// the earlier stage by a later release went past it.
		viaPre := preAt != nil && (prodAt == nil || !preAt.After(*prodAt))
		if viaPre {
			add(StageToPre, pr.MergedAt, preAt)
		}
		// From the stage before production; a PR that went to production
		// without it (no such stage, or a hotfix past it) from its merge, so
		// its deploy time is not lost.
		from := pr.MergedAt
		if viaPre {
			from = preAt
		}
		add(StageToProd, from, prodAt)
		if pr.ApprovedAt != nil {
			c.approved++
			if pr.ApprovedOnFirstTouch {
				c.approvedOnFirstTouch++
			}
		}
	}
	return c
}

// Stage is one stage of the lead-time breakdown.
type Stage struct {
	Key string `json:"key"`
	Spread
	// LowSample: fewer than 10 PRs.
	LowSample bool `json:"low_sample"`
	// Share is the stage's part of the summed medians; null without data.
	// All medians zero (one PR through in a second): equal shares.
	Share *float64 `json:"share"`
	// Hot is the stage with the largest median.
	Hot bool `json:"hot"`
}

// Chain is where the time to production goes.
type Chain struct {
	// Stages in order; to_pre only when there is a stage before production.
	Stages         []Stage `json:"stages"`
	PreEnvironment *string `json:"pre_environment"`
	// SumP50 is the sum of the stage medians; null when no stage has data.
	SumP50 *float64 `json:"sum_p50"`
	// BeforeProd is the sum of the medians before the last stage.
	BeforeProd float64 `json:"before_prod"`
}

func leadChain(c rawChain) Chain {
	out := Chain{PreEnvironment: c.preEnvironment}
	var total float64
	withData := 0
	for _, k := range stageKeys {
		if k == StageToPre && c.preEnvironment == nil {
			continue
		}
		s := Stage{Key: k, Spread: spread(c.waits[k])}
		s.LowSample = s.N < lowSample
		if s.P50 != nil {
			total += *s.P50
			withData++
		}
		out.Stages = append(out.Stages, s)
	}
	if withData == 0 {
		return out
	}
	out.SumP50 = ptr(total)
	hot := -1
	for i := range out.Stages {
		s := &out.Stages[i]
		if s.P50 == nil {
			continue
		}
		if total > 0 {
			s.Share = ptr(*s.P50 / total)
		} else {
			s.Share = ptr(1 / float64(withData))
		}
		if hot < 0 || *s.P50 > *out.Stages[hot].P50 {
			hot = i
		}
		if i < len(out.Stages)-1 {
			out.BeforeProd += *s.P50
		}
	}
	out.Stages[hot].Hot = true
	return out
}

// CycleBuckets are the cycle-time histogram's upper bounds in seconds, the
// last one open.
var CycleBuckets = []float64{15 * 60, 3600, 4 * 3600, 86400, 3 * 86400}

func histogram(seconds []float64) []int {
	out := make([]int, len(CycleBuckets)+1)
	for _, s := range seconds {
		i, _ := slices.BinarySearchFunc(CycleBuckets, s, func(b, s float64) int {
			if b <= s {
				return -1
			}
			return 1
		})
		out[i]++
	}
	return out
}

// cycleTimes are opened to merged, per PR with a known opening.
func cycleTimes(prs []dataset.PR) []float64 {
	var out []float64
	for _, pr := range prs {
		if s := wait(pr.OpenedAt, pr.MergedAt); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

// MixWeek is one week's merged PRs by branch prefix; every other prefix and
// none fold into other.
type MixWeek struct {
	Feature int `json:"feature"`
	Bugfix  int `json:"bugfix"`
	Hotfix  int `json:"hotfix"`
	Other   int `json:"other"`
}

func changeMix(prs []dataset.PR, p Period) []MixWeek {
	out := make([]MixWeek, len(p.Weeks))
	for _, pr := range prs {
		if pr.MergedAt == nil {
			continue
		}
		i := p.WeekIndex(*pr.MergedAt)
		if i < 0 {
			continue
		}
		t := ""
		if pr.ChangeType != nil {
			t = *pr.ChangeType
		}
		switch t {
		case "feature":
			out[i].Feature++
		case "bugfix":
			out[i].Bugfix++
		case "hotfix":
			out[i].Hotfix++
		default:
			out[i].Other++
		}
	}
	return out
}

// Histograms are cycle-time counts per bucket.
type Histograms struct {
	// UpperBoundsS are the buckets' upper bounds in seconds, exclusive; the
	// last bucket, past the last bound, is open.
	UpperBoundsS []float64 `json:"upper_bounds_s"`
	Human        []int     `json:"human"`
	Bot          []int     `json:"bot"`
}

// Review is the code-review section.
type Review struct {
	// Cycle is opened to merged, human PRs.
	Cycle Spread `json:"cycle"`
	// Review is ready to approval, the breakdown's review stage.
	Review               Spread   `json:"review"`
	Approved             int      `json:"approved"`
	ApprovedOnFirstTouch int      `json:"approved_on_first_touch"`
	FirstTouchRate       *float64 `json:"first_touch_rate"`
	// Merged are the merged PRs, bots per the filter; Untracked those
	// without a Jira key.
	Merged         int        `json:"merged"`
	Untracked      int        `json:"untracked"`
	UntrackedRate  *float64   `json:"untracked_rate"`
	CycleHistogram Histograms `json:"cycle_histogram"`
	ChangeMix      []MixWeek  `json:"change_mix"`
}

// review is the section over the human PRs merged in the period and those
// counted per the bot filter, both from mergedPRs.
func review(humans, withBots []dataset.PR, p Period, ch rawChain) Review {
	var bots []dataset.PR
	untracked := 0
	for _, pr := range withBots {
		if pr.Bot {
			bots = append(bots, pr)
		}
		if len(pr.JiraKeys) == 0 {
			untracked++
		}
	}
	cycle := cycleTimes(humans)
	return Review{
		Cycle:                spread(cycle),
		Review:               spread(ch.waits[StageReview]),
		Approved:             ch.approved,
		ApprovedOnFirstTouch: ch.approvedOnFirstTouch,
		FirstTouchRate:       ratio(ch.approvedOnFirstTouch, ch.approved),
		Merged:               len(withBots),
		Untracked:            untracked,
		UntrackedRate:        ratio(untracked, len(withBots)),
		CycleHistogram: Histograms{
			UpperBoundsS: CycleBuckets,
			Human:        histogram(cycle),
			Bot:          histogram(cycleTimes(bots)),
		},
		ChangeMix: changeMix(withBots, p),
	}
}

// ---- Costs -----------------------------------------------------------------

// CostRow is the spend of one product or repo.
type CostRow struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// Costs is the noergler spend.
type Costs struct {
	// Total is all rollups closed in the window, whatever the outcome.
	Total         float64 `json:"total"`
	Merged        float64 `json:"merged"`
	MergedReviews int     `json:"merged_reviews"`
	// PerKloc is per 1000 changed lines of merged PRs; null without line
	// counts.
	PerKloc *float64 `json:"per_kloc"`
	// Wasted is spent on PRs declined or deleted.
	Wasted float64 `json:"wasted"`
	// Rows are by product (all products) or by repo (one product), largest
	// first.
	Rows          []CostRow `json:"rows"`
	ReviewedRepos int       `json:"reviewed_repos"`
}

// usd is the stored decimal, summed exactly; a malformed one counts 0.
func usd(r dataset.Review) *big.Rat {
	v := new(big.Rat)
	if r.CostUSD != nil {
		if _, ok := v.SetString(*r.CostUSD); !ok {
			v.SetInt64(0)
		}
	}
	return v
}

func float(r *big.Rat) float64 {
	f, _ := r.Float64()
	return f
}

func costs(ds *dataset.Dataset, f filter) Costs {
	total, merged, wasted := new(big.Rat), new(big.Rat), new(big.Rat)
	var lines int64
	mergedN := 0
	by := map[string]*big.Rat{}
	repos := map[string]bool{}
	for _, r := range ds.Reviews {
		if !f.in(r.Project) {
			continue
		}
		v := usd(r)
		total.Add(total, v)
		outcome := ""
		if r.Outcome != nil {
			outcome = *r.Outcome
		}
		switch outcome {
		case "merged":
			merged.Add(merged, v)
			mergedN++
			if r.LinesAdded != nil {
				lines += *r.LinesAdded
			}
			if r.LinesRemoved != nil {
				lines += *r.LinesRemoved
			}
		case "declined", "deleted":
			wasted.Add(wasted, v)
		}
		key := r.Project
		if f.product != "" {
			key = r.Repo
		}
		label := "?"
		if key != nil {
			label = *key
		}
		if by[label] == nil {
			by[label] = new(big.Rat)
		}
		by[label].Add(by[label], v)
		if r.Repo != nil && *r.Repo != "" {
			repos[*r.Repo] = true
		}
	}
	out := Costs{
		Total:         float(total),
		Merged:        float(merged),
		MergedReviews: mergedN,
		Wasted:        float(wasted),
		Rows:          []CostRow{},
		ReviewedRepos: len(repos),
	}
	if lines > 0 {
		k := new(big.Rat).Quo(merged, new(big.Rat).SetInt64(lines))
		out.PerKloc = ptr(float(k.Mul(k, big.NewRat(1000, 1))))
	}
	for label, v := range by {
		out.Rows = append(out.Rows, CostRow{Label: label, Value: float(v)})
	}
	slices.SortFunc(out.Rows, func(a, b CostRow) int {
		return cmp.Or(cmp.Compare(b.Value, a.Value), cmp.Compare(a.Label, b.Label))
	})
	return out
}
