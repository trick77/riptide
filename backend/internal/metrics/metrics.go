package metrics

import (
	"cmp"
	"maps"
	"math"
	"math/big"
	"slices"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/dataset"
)

// lowSample is the sample size below which a number is flagged as weak.
const lowSample = 10

// Params selects what the page shows of a dataset.
type Params struct {
	Period Period
	// Previous is the period Period is compared with: the same number of
	// weeks right before it (ClosedPrevious).
	Previous Period
	// Running is the week running now (RunningPeriod), counted apart.
	Running Period
	// Product is a Bitbucket project key; empty, or one the dataset does
	// not know, is all products.
	Product string
	// JiraURL is the config's jira.base_url: Jira keys link there.
	JiraURL string
	// BitbucketURL is BACKEND_BITBUCKET_URL: a PR link elsewhere is dropped.
	// Empty: any http(s) link.
	BitbucketURL string
	// Thresholds are the team's (config's `thresholds`) the assessment's
	// states measure against; nil: the starting values.
	Thresholds *config.Thresholds
	// Earlier is the dataset over StatePeriod, before Previous, that the
	// assessment's states look back into (a project's last work, a PR
	// still before production, a typical week); nil: none read.
	Earlier *dataset.Dataset
}

// filter is the product selection, resolved against the dataset. Bots'
// PRs are never counted in (user, 2026-11-16: the numbers mirror the
// team's own work; a switch for them only distorted review time and
// throughput).
type filter struct {
	product string
}

func (f filter) in(project *string) bool {
	return f.product == "" || (project != nil && *project == f.product)
}

func (f filter) inKey(project string) bool { return f.product == "" || project == f.product }

func newFilter(ds *dataset.Dataset, p Params) filter {
	var f filter
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
	Product   *string  `json:"product"`
	Delivery  Delivery `json:"delivery"`
	LeadChain Chain    `json:"lead_chain"`
	// Coverage is how the period's merged PRs reached production by its
	// end: placed, not yet, not placeable and why, per repo too.
	Coverage Coverage `json:"coverage"`
	Review   Review   `json:"review"`
	// Throughput is the merged PRs per week and their size.
	Throughput Throughput `json:"throughput"`
	OpenPRs    OpenPRs    `json:"open_prs"`
	Build      Build      `json:"build"`
	Costs      Costs      `json:"costs"`
	// Comparison is the numbers against the previous period.
	Comparison Comparison `json:"comparison"`
	// RunningWeek is the week running now, counted in no number above.
	RunningWeek RunningWeek `json:"running_week"`
}

// RunningWeek is the week running now so far, counted as the page counts
// the period: the product's releases to production, the people's merged
// PRs, the team's finished runs.
type RunningWeek struct {
	Week Week `json:"week"`
	// To is now, where the counts end.
	To       time.Time `json:"to"`
	Releases int       `json:"releases"`
	Merged   int       `json:"merged"`
	Runs     int       `json:"runs"`
}

// runningWeek counts ds over p, the week running now. A release counts
// once, in the week of its first deploy: a revision that reached production
// before Monday 00:00 and reaches another App after it is an earlier week's
// release, however many weeks back, whatever period the page reads.
func runningWeek(ds *dataset.Dataset, p Period, f filter) RunningWeek {
	runs := 0
	for _, rs := range teamRuns(ds, f, gitopsRun(ds), nil) {
		runs += len(rs)
	}
	prod := ds.Meta.ProductionStage
	fresh := *ds
	fresh.Deploys = slices.DeleteFunc(slices.Clone(ds.Deploys), func(d dataset.Deploy) bool {
		return d.RevisionFirstAt != nil && d.RevisionFirstAt.Before(p.From)
	})
	return RunningWeek{
		Week: p.Weeks[0], To: p.To,
		Releases: deployFrequency(&fresh, p, f, prod).Releases,
		Merged:   len(mergedPRs(ds, p, f)),
		Runs:     runs,
	}
}

// Compute is every metric of the page, ds over p.Period, prev the dataset
// over p.Previous it is compared with, running the dataset over p.Running:
// the open PRs as they are now and the running week's counts. prev is never
// nil: no previous data is an empty dataset under ds's Meta. A nil running,
// or no p.Running, keeps the open PRs at the period's end and counts no
// running week.
func Compute(ds, prev, running *dataset.Dataset, p Params) Metrics {
	ds, prev = classified(ds, prev)
	m, _ := computeBoth(ds, prev, running, p)
	return m
}

// classified are the period's and the previous period's datasets with
// every App classed by the period's end (ds.Apps), as the assessment
// classes them: an App is infrastructure, or a product's, in both periods
// alike, so the comparison never moves for an App classed apart, and the
// page's closed period says what its stored assessment says
// (dataset.Dataset.Classified). The running week keeps its own, as of now.
func classified(ds, prev *dataset.Dataset) (*dataset.Dataset, *dataset.Dataset) {
	return ds.Classified(ds.Apps), prev.Classified(ds.Apps)
}

// computeBoth is Compute and the previous period's sections it compared
// with, for the assessment: ds, prev and running classified alike.
func computeBoth(ds, prev, running *dataset.Dataset, p Params) (m, pm Metrics) {
	// The product as this period's data resolves it, for both periods: a
	// product the previous one lacks is that product without data, not all.
	f := newFilter(ds, p)
	// The previous period first: every repo's build flags compare with it,
	// its runs named as this period's.
	names := newRepoNamer(slices.Concat(ds.Runs, prev.Runs))
	pm = compute(prev, p.Previous, f, &baseline{repos: names, previous: true})
	// In a product view a build reaches its product through Bitbucket's
	// commits: with its events lost, a repo's builds compare with nothing, as
	// the kinds' numbers do (compare).
	lost := lostEvents(ds, prev)
	base := &baseline{repos: names, kinds: pm.Build.repos,
		incomplete: buildsIncomplete(ds.Meta, p.Previous.From) || (lost && f.product != "")}
	m = compute(ds, p.Period, f, base)
	m.Delivery.Frequency.fillCalendar(p.Period, ds.Meta.DataStart.ArgoCD)
	// The open PRs are the one number of now, not of the period's end: what
	// waits is worth knowing today. Before the comparison, which judges them
	// against the previous period's end.
	if running != nil && len(p.Running.Weeks) == 1 {
		m.OpenPRs = openSection(running, f)
		m.RunningWeek = runningWeek(running, p.Running, f)
		// How fresh each source is, now: the closed period's read ends its
		// sources at Monday 00:00, the running week's reads them all.
		m.Meta.DataEnd = running.Meta.DataEnd
	}
	m.Comparison = compare(m, pm, p.Previous, lost, f.product != "")
	return m, pm
}

// lostEvents reports whether Bitbucket's events were lost in either period
// (dataset.Silence.Lost): its counts read low there, a false worsening now
// and a false gain once the hole lies in the previous period.
func lostEvents(ds, prev *dataset.Dataset) bool {
	lost := func(s dataset.Silence) bool { return s.Lost }
	return slices.ContainsFunc(ds.Meta.Silences, lost) || slices.ContainsFunc(prev.Meta.Silences, lost)
}

// compute is the page's sections over one period; base as build takes it.
func compute(ds *dataset.Dataset, period Period, f filter, base *baseline) Metrics {
	humans := mergedPRs(ds, period, f)
	ch := chain(ds, humans, period.To)
	m := Metrics{
		Meta:       ds.Meta,
		Period:     period,
		Products:   ds.Products,
		Delivery:   delivery(ds, humans, period, f, ch),
		LeadChain:  leadChain(ch),
		Coverage:   coverage(ds, humans, period),
		Review:     review(humans, period, ch),
		Throughput: throughput(ds, humans, period, f),
		OpenPRs:    openSection(ds, f),
		Build:      build(ds, period, f, base),
		Costs:      costs(ds, f),
	}
	if f.product != "" {
		m.Product = ptr(f.product)
	}
	return m
}

// ---- Deploys ---------------------------------------------------------------

// Revision classes, ranked: a revision is a release if it brought new
// images to any App, else a first release if it was some App's first
// deploy counted as one (dataset.Deploy.FirstRelease), else a first deploy
// if it was some App's first, else a re-sync. A first release counts as a
// release everywhere one counts.
const (
	classSync = iota
	classFirstSeen
	classFirstRelease
	classRelease
)

func revisionClass(d dataset.Deploy) int {
	switch {
	case d.Kind != nil && *d.Kind == "release":
		return classRelease
	case d.Kind != nil && *d.Kind == "first_seen" && d.FirstRelease:
		return classFirstRelease
	case d.Kind != nil && *d.Kind == "first_seen":
		return classFirstSeen
	}
	return classSync
}

// failedPhase: a sync that deployed nothing. Argo CD's notification sends
// Error as well as Failed for one.
func failedPhase(phase string) bool { return phase == "Failed" || phase == "Error" }

// DeployWeek is one week's revisions by class.
type DeployWeek struct {
	Release int `json:"release"`
	// FirstRelease are the Releases that were an App's first deploy.
	FirstRelease int `json:"first_release"`
	Sync         int `json:"sync"`
	FirstSeen    int `json:"first_seen"`
}

// DeployFrequency is the production deploy frequency over the period.
// Infrastructure Apps (dataset.Deploy.Infra) are in none of its numbers
// but Infra and InfraApps: no product's, nothing the team is judged on.
type DeployFrequency struct {
	// Releases, Syncs, FirstSeen count distinct GitOps revisions: a release
	// reconciles every App sharing its revision, so it counts once.
	Releases int `json:"releases"`
	// FirstReleases are the Releases that were an App's first deploy, more
	// than dataset.FirstDeployGrace after Argo CD first reported: an App
	// going live.
	FirstReleases int `json:"first_releases"`
	// ReleasesUnresolved are the Releases tracing resolved no stretch of
	// any repo for, and that changed an image of one (dataset.Deploy.Traced):
	// what they shipped no one can name ("n Releases ohne zuordenbare PRs").
	ReleasesUnresolved int `json:"releases_unresolved"`
	Syncs              int `json:"syncs"`
	// FirstSeen are the App's first deploys within dataset.FirstDeployGrace
	// of Argo CD's first event: release or re-sync, unknown.
	FirstSeen int `json:"first_seen"`
	// GraceDays is dataset.FirstDeployGrace in days, for the page's words.
	GraceDays int `json:"grace_days"`
	// Resyncs are the revisions that brought no new images.
	Resyncs int `json:"resyncs"`
	// Unattributed are the period's successful deploys, distinct
	// revisions, of Apps with no known product (neither the revision nor
	// the App's other deploys name one: dataset.App.Project), infrastructure
	// Apps aside; team-wide whatever product the view shows. In the team
	// view's numbers, in no product's: a product without deploys may have
	// its releases here (a new App's GitOps commit Bitbucket's events
	// missed).
	Unattributed int `json:"unattributed"`
	// MaybeUnattributed: a product view without a deploy of its own while
	// Unattributed are there: they may be its, so "no deploys" would be a
	// claim the data does not carry.
	MaybeUnattributed bool `json:"maybe_unattributed"`
	// Infra are the period's successful deploys, distinct revisions, of
	// infrastructure Apps only, InfraApps those Apps by name; team-wide.
	Infra     int      `json:"infra"`
	InfraApps []string `json:"infra_apps"`
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
	// MedianWeek is the median of the releases per week over the whole weeks
	// Argo CD reported all of: a week Argo CD started in, or one still
	// running, would read low. Null without such a week.
	MedianWeek *float64 `json:"median_week"`
	// Days are the releases per day, the period's weeks Monday first, 7 per
	// week: a release train or a gap shows where the weekly bars stay even.
	Days []DeployDay `json:"days"`
	// NoDataDays counts the Days without data.
	NoDataDays int `json:"no_data_days"`
	// Weekdays are the releases per weekday over the whole days with data,
	// Monday first: a weekday the running week has not reached yet, or one
	// before Argo CD reported, is not a day without a release.
	Weekdays [7]DeployWeekday `json:"weekdays"`

	// releaseAt is each release's first deploy in the period, for the days.
	releaseAt []time.Time
}

// DeployWeekday is one weekday's releases over the period's whole days with
// data (fillCalendar).
type DeployWeekday struct {
	Releases int `json:"releases"`
	// Days are the weekday's whole days with data.
	Days int `json:"days"`
	// PerDay is Releases over Days; null without a day with data.
	PerDay *float64 `json:"per_day"`
}

// DeployDay is one calendar day's releases in the viewer's zone, each in
// the day of its first deploy in the period, as Weeks counts them.
type DeployDay struct {
	// Date is the local calendar date, YYYY-MM-DD.
	Date     string `json:"date"`
	Releases int    `json:"releases"`
	// Step is the day's shade: 0 none, 1 to 3, 4 for four or more.
	Step int `json:"step"`
	// NoData: the day is after the period's end or before Argo CD reported,
	// so no release is no news, not a gap.
	NoData bool `json:"no_data"`
}

// maxStep is the darkest shade a day gets: four releases or more.
const maxStep = 4

// fillCalendar sets Days, NoDataDays and Weekdays from the releases
// deployFrequency found, argoStart being when Argo CD's data starts. The
// page's period only: the previous one is compared by its totals.
//
// A day has data from Argo CD's first event on to the period's end. Argo
// CD's data end says nothing here: an outage and a team that did not deploy
// both send no events. The weekday rates count whole days with data only:
// today is not over yet, and on Argo CD's first day the hours before its
// first event are unknown; both cells still show their releases.
func (d *DeployFrequency) fillCalendar(p Period, argoStart *time.Time) {
	days := p.calendar()
	d.Days = make([]DeployDay, len(days))
	whole := make([]bool, len(days))
	for i, c := range days {
		noData := !c.start.Before(p.To) || argoStart == nil || !c.end.After(*argoStart)
		d.Days[i] = DeployDay{Date: c.start.Format(time.DateOnly), NoData: noData}
		whole[i] = !noData && !c.end.After(p.To) && !c.start.Before(*argoStart)
	}
	for _, at := range d.releaseAt {
		i := dayIndex(days, p.WeekIndex(at), at)
		if i < 0 {
			// Weeks and days cut in different zones: never in a period
			// NewPeriod made, but no request fails over it.
			continue
		}
		d.Days[i].Releases++
	}
	for i, day := range d.Days {
		d.Days[i].Step = min(day.Releases, maxStep)
		if day.NoData {
			d.NoDataDays++
		}
		if whole[i] {
			wd := &d.Weekdays[i%7]
			wd.Releases += day.Releases
			wd.Days++
		}
	}
	for i := range d.Weekdays {
		wd := &d.Weekdays[i]
		if wd.Days > 0 {
			wd.PerDay = ptr(float64(wd.Releases) / float64(wd.Days))
		}
	}
}

func deployFrequency(ds *dataset.Dataset, p Period, f filter, env string) DeployFrequency {
	var ok []dataset.Deploy
	failed := 0
	// Team-wide, before the product filter: what no product shows.
	unattributed, infra, infraApps := map[string]bool{}, map[string]bool{}, map[string]bool{}
	// Revisions some App not infrastructure deployed: theirs, counted where
	// that App counts, never as infrastructure's too.
	work := map[string]bool{}
	for _, d := range ds.Deploys {
		if d.Environment == nil || *d.Environment != env || !p.In(&d.At) {
			continue
		}
		switch {
		case d.Infra:
			if d.Phase == "Succeeded" {
				infra[d.Revision], infraApps[d.App] = true, true
			}
			continue
		case d.Phase == "Succeeded":
			work[d.Revision] = true
			if d.Project == nil {
				unattributed[d.Revision] = true
			}
		}
		if !f.in(d.Project) {
			continue
		}
		switch {
		case d.Phase == "Succeeded":
			ok = append(ok, d)
		case failedPhase(d.Phase):
			failed++
		}
	}
	class := map[string]int{}
	firstAt := map[string]time.Time{}
	traced := map[string]bool{}
	var order []string
	for _, d := range ok {
		traced[d.Revision] = traced[d.Revision] || d.Traced
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
	out := DeployFrequency{Weeks: make([]DeployWeek, len(p.Weeks)), Unattributed: len(unattributed),
		InfraApps: slices.Sorted(maps.Keys(infraApps)), GraceDays: int(dataset.FirstDeployGrace / (24 * time.Hour))}
	if out.InfraApps == nil {
		out.InfraApps = []string{}
	}
	// A revision some other App deployed too is that App's.
	for rev := range infra {
		if !work[rev] {
			out.Infra++
		}
	}
	// Each revision in the week of its first deploy in the period, with its
	// class over the period: the bars add up to the totals, a release rolled
	// out across a week boundary counts once.
	for _, rev := range order {
		w := &out.Weeks[p.WeekIndex(firstAt[rev])]
		switch class[rev] {
		case classRelease, classFirstRelease:
			w.Release++
			out.Releases++
			out.releaseAt = append(out.releaseAt, firstAt[rev])
			if !traced[rev] {
				out.ReleasesUnresolved++
			}
			if class[rev] == classFirstRelease {
				w.FirstRelease++
				out.FirstReleases++
			}
		case classFirstSeen:
			w.FirstSeen++
			out.FirstSeen++
		default:
			w.Sync++
		}
	}
	out.Syncs = len(order)
	out.Resyncs = out.Syncs - out.Releases - out.FirstSeen
	out.MaybeUnattributed = f.product != "" && out.Syncs == 0 && out.Unattributed > 0
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
	var whole []float64
	for i, w := range p.Weeks {
		if start != nil && !start.After(w.Start) && !w.End.After(p.To) {
			whole = append(whole, float64(out.Weeks[i].Release))
		}
	}
	out.MedianWeek = Median(whole)
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

// LeadTimes is the lead time to one environment, per PR
// (docs/dora-lead-time.md): the people's PRs merged in the period
// (mergedPRs) that reached the environment by its end, each from its first
// commit (leadStart) to its first deploy there. The unit is the PR, never
// the commit (owner's decision, 2026-11); bots' PRs are in no number.
type LeadTimes struct {
	Environment string `json:"environment"`
	// Human is over those PRs; N is how many ("aus n PRs").
	Human Spread `json:"human"`
	// LowerBound are Human's PRs placed by a stretch whose start was a lower
	// bound: they may have gone out earlier, their time reads long.
	LowerBound int `json:"lower_bound"`
	// Unmeasured reached the environment by the end too, but have no start
	// (no pushed commit, no opening: opened before Bitbucket reported, or in
	// a lost silence): in no time. Human.N + Unmeasured are the PRs traced
	// there (for production Coverage.OnProd).
	Unmeasured int `json:"unmeasured"`
	// LowSample: fewer than 10 PRs, but some.
	LowSample bool `json:"low_sample"`
}

// leadTime is per PR of humans (mergedPRs), first commit to the first
// deploy to env before cutoff, the period's end; a library's PR is never
// rolled out.
func leadTime(humans []dataset.PR, env string, cutoff time.Time) LeadTimes {
	var waits []float64
	lower, unmeasured := 0, 0
	for _, pr := range humans {
		if pr.Library {
			continue
		}
		e := firstDeployBefore(pr, env, cutoff)
		if e == nil {
			continue
		}
		w := wait(leadStart(pr), &e.FirstDeployedAt)
		if w == nil {
			unmeasured++
			continue
		}
		waits = append(waits, *w)
		if e.LowerBound {
			lower++
		}
	}
	h := spread(waits)
	return LeadTimes{Environment: env, Human: h, LowerBound: lower, Unmeasured: unmeasured,
		LowSample: h.N > 0 && h.N < lowSample}
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

func delivery(ds *dataset.Dataset, humans []dataset.PR, p Period, f filter, ch rawChain) Delivery {
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
		LeadTime:        leadTime(humans, prod, p.To),
	}
	if pre != nil {
		d.LeadTimePre = ptr(leadTime(humans, *pre, p.To))
	}
	return d
}

// ---- PRs -------------------------------------------------------------------

// mergedPRs are the people's PRs merged in the period, the merged work every
// PR number counts: GitOps promotions, bots and the PRs notCounted gives a
// reason for left out.
func mergedPRs(ds *dataset.Dataset, p Period, f filter) []dataset.PR {
	var out []dataset.PR
	for _, pr := range peopleMerged(ds, p, f) {
		if notCounted(pr) == "" {
			out = append(out, pr)
		}
	}
	return out
}

// peopleMerged are the people's PRs merged in the period, the ones
// notCounted leaves out included.
func peopleMerged(ds *dataset.Dataset, p Period, f filter) []dataset.PR {
	var out []dataset.PR
	for _, pr := range ds.PRs {
		if !pr.GitOps && !pr.Bot && pr.State == "merged" && f.inKey(pr.Project) && p.In(pr.MergedAt) {
			out = append(out, pr)
		}
	}
	return out
}

// Why a person's merged PR is no merged work, the first that applies. A PR
// is a PR wherever it went (owner, 2026-11): one into a feature branch, or
// marked merged when an umbrella brought it in, is merged work.
const (
	// NotCountedUmbrella: the vehicle of other people's merged PRs, which
	// count themselves (dataset.PR.Umbrella).
	NotCountedUmbrella = "umbrella"
	// NotCountedHousekeeping: a tool's change opened across repos at once,
	// unapproved, without a ticket (dataset.PR.Housekeeping).
	NotCountedHousekeeping = "housekeeping"
	// NotCountedTwice: merged implicitly from a branch another merged PR,
	// counted, came from (dataset.PR.Twice): the same work.
	NotCountedTwice = "twice"
)

// notCounted is why pr is no merged work, "" when it is.
func notCounted(pr dataset.PR) string {
	switch {
	case pr.Umbrella:
		return NotCountedUmbrella
	case pr.Housekeeping:
		return NotCountedHousekeeping
	case pr.Twice:
		return NotCountedTwice
	}
	return ""
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

// firstDeploy is the PR's first deploy to env, whenever: the facts' (they
// read to now).
func firstDeploy(pr dataset.PR, env string) *time.Time {
	if e := firstDeployBefore(pr, env, time.Time{}); e != nil {
		return ptr(e.FirstDeployedAt)
	}
	return nil
}

// firstDeployBefore is the PR's first deploy to env before cutoff (zero:
// any time), nil without one: a closed period reads none after its end.
func firstDeployBefore(pr dataset.PR, env string, cutoff time.Time) *dataset.EnvironmentDeploy {
	for _, e := range pr.Environments {
		if e.Environment == env && (cutoff.IsZero() || e.FirstDeployedAt.Before(cutoff)) {
			return &e
		}
	}
	return nil
}

// chain is the lead-time breakdown query, per human PR merged in the period
// (humans, from mergedPRs), its deploys those before cutoff, the period's
// end: a closed period does not move when a later deploy arrives.
func chain(ds *dataset.Dataset, humans []dataset.PR, cutoff time.Time) rawChain {
	prod := ds.Meta.ProductionStage
	var prs []dataset.PR
	for _, pr := range humans {
		if pr.ReadyAt != nil {
			prs = append(prs, pr)
		}
	}
	// The stage before production: the one the PRs reached first most
	// often, ties by name.
	pre := map[string]int{}
	for _, pr := range prs {
		for _, e := range pr.Environments {
			if e.Environment != prod && e.FirstDeployedAt.Before(cutoff) {
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
		var preAt, prodAt *time.Time
		if preEnv != nil {
			if e := firstDeployBefore(pr, *preEnv, cutoff); e != nil {
				preAt = &e.FirstDeployedAt
			}
		}
		if e := firstDeployBefore(pr, prod, cutoff); e != nil {
			prodAt = &e.FirstDeployedAt
		}
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
	// Share is the stage's part of the summed medians; null without data or
	// when the chain does not reach production (Chain.ReachesProd).
	// All medians zero (one PR through in a second): equal shares.
	Share *float64 `json:"share"`
	// Hot is the stage with the largest median, only in a chain that
	// reaches production.
	Hot bool `json:"hot"`
	// Histogram is the stage's PRs per StageBounds bucket; null when
	// LowSample, which sends DurationsS instead.
	Histogram []int `json:"histogram"`
	// DurationsS are the stage's waits in seconds, ascending, only when
	// LowSample: a curve from a handful of PRs would be invented, the UI
	// draws them one by one. Null otherwise.
	DurationsS []float64 `json:"durations_s"`
}

// stageSteps is the number of stage histogram buckets.
const stageSteps = 22

// stageBounds are the stage histograms' bucket edges in seconds, 5 min to
// 28 days in 22 log-spaced steps (each ~1.5 times the one before). A
// production wait of two weeks is common, so 14 days would pile the slow
// half on the last edge. The first bucket also holds every shorter wait,
// the last every longer one: a merge in the approval's second still counts.
// An array: nothing outside can change it.
var stageBounds = func() (out [stageSteps + 1]float64) {
	const lo, hi = 300.0, 28 * 86400.0
	for i := range out {
		out[i] = math.Round(lo * math.Pow(hi/lo, float64(i)/stageSteps))
	}
	return out
}()

// StageBounds is a copy of the stage histograms' bucket edges.
func StageBounds() []float64 { return slices.Clone(stageBounds[:]) }

// stageHistogram counts seconds into stageBounds' buckets, out-of-range
// waits into the first or last one.
func stageHistogram(seconds []float64) []int {
	out := make([]int, stageSteps)
	for _, s := range seconds {
		// The first edge above s, less one: the bucket s falls in.
		i, _ := slices.BinarySearchFunc(stageBounds[:], s, edgeAbove)
		out[min(max(i-1, 0), len(out)-1)]++
	}
	return out
}

// edgeAbove orders a bucket edge before s unless it lies above it: never a
// match, so slices.BinarySearchFunc lands on the first edge above s.
func edgeAbove(edge, s float64) int {
	if edge <= s {
		return -1
	}
	return 1
}

// Chain is where the time to production goes.
type Chain struct {
	// Stages in order; to_pre only when there is a stage before production.
	Stages         []Stage `json:"stages"`
	PreEnvironment *string `json:"pre_environment"`
	// MinPRs is the fewest PRs a stage needs for its histogram; fewer are
	// LowSample.
	MinPRs int `json:"min_prs"`
	// BeforeProd is the sum of the medians before the last stage.
	BeforeProd float64 `json:"before_prod"`
	// ReachesProd: some PR has a production wait. Without one, no stage has
	// a share or is hot: the medians would add up to the time to merge, and
	// coding read as most of a time to production nothing reached (a product
	// whose prod deploys predate its CI reports).
	ReachesProd bool `json:"reaches_prod"`
	// BucketBoundsS are StageBounds, every stage's histogram edges.
	BucketBoundsS []float64 `json:"bucket_bounds_s"`
}

func leadChain(c rawChain) Chain {
	out := Chain{PreEnvironment: c.preEnvironment, MinPRs: lowSample,
		BucketBoundsS: StageBounds()}
	var total float64
	withData := 0
	for _, k := range stageKeys {
		if k == StageToPre && c.preEnvironment == nil {
			continue
		}
		s := Stage{Key: k, Spread: spread(c.waits[k])}
		s.LowSample = s.N < lowSample
		if s.LowSample {
			s.DurationsS = slices.Sorted(slices.Values(c.waits[k]))
			if s.DurationsS == nil {
				s.DurationsS = []float64{}
			}
		} else {
			s.Histogram = stageHistogram(c.waits[k])
		}
		if s.P50 != nil {
			total += *s.P50
			withData++
		}
		out.Stages = append(out.Stages, s)
	}
	out.ReachesProd = out.Stages[len(out.Stages)-1].P50 != nil
	if withData == 0 {
		return out
	}
	hot := -1
	for i := range out.Stages {
		s := &out.Stages[i]
		if s.P50 == nil {
			continue
		}
		if i < len(out.Stages)-1 {
			out.BeforeProd += *s.P50
		}
		if !out.ReachesProd {
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
	}
	if hot >= 0 {
		out.Stages[hot].Hot = true
	}
	return out
}

// CycleBuckets are the cycle-time histogram's upper bounds in seconds, the
// last one open.
var CycleBuckets = []float64{15 * 60, 3600, 4 * 3600, 86400, 3 * 86400}

func histogram(seconds []float64) []int {
	out := make([]int, len(CycleBuckets)+1)
	for _, s := range seconds {
		i, _ := slices.BinarySearchFunc(CycleBuckets, s, edgeAbove)
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
	// Merged are the people's merged PRs; Untracked those without a Jira
	// key.
	Merged         int        `json:"merged"`
	Untracked      int        `json:"untracked"`
	UntrackedRate  *float64   `json:"untracked_rate"`
	CycleHistogram Histograms `json:"cycle_histogram"`
	// WithinDay are the human PRs merged within a day of opening, of those
	// in Cycle.
	WithinDay Share     `json:"within_day"`
	ChangeMix []MixWeek `json:"change_mix"`
	// MixTotal is ChangeMix over the period, MixShare each prefix's part of
	// it: the mix reads off the whole period, a week of few PRs swings.
	MixTotal MixWeek  `json:"mix_total"`
	MixShare MixShare `json:"mix_share"`
}

// Share is count of n; Rate null when n is 0.
type Share struct {
	Count int      `json:"count"`
	N     int      `json:"n"`
	Rate  *float64 `json:"rate"`
}

// MixShare is each branch prefix's part of the merged PRs; null without any.
type MixShare struct {
	Feature *float64 `json:"feature"`
	Bugfix  *float64 `json:"bugfix"`
	Hotfix  *float64 `json:"hotfix"`
	Other   *float64 `json:"other"`
}

// Day is the cycle time a PR is merged under to count in WithinDay:
// exclusive, as the histogram's bounds, so the two agree at 24 h.
const Day = 86400.0

// review is the section over the human PRs merged in the period, from
// mergedPRs.
func review(humans []dataset.PR, p Period, ch rawChain) Review {
	untracked := 0
	for _, pr := range humans {
		if len(pr.JiraKeys) == 0 {
			untracked++
		}
	}
	cycle := cycleTimes(humans)
	within := 0
	for _, s := range cycle {
		if s < Day {
			within++
		}
	}
	mix := changeMix(humans, p)
	var total MixWeek
	for _, w := range mix {
		total.Feature += w.Feature
		total.Bugfix += w.Bugfix
		total.Hotfix += w.Hotfix
		total.Other += w.Other
	}
	all := total.Feature + total.Bugfix + total.Hotfix + total.Other
	return Review{
		Cycle:                spread(cycle),
		Review:               spread(ch.waits[StageReview]),
		Approved:             ch.approved,
		ApprovedOnFirstTouch: ch.approvedOnFirstTouch,
		FirstTouchRate:       ratio(ch.approvedOnFirstTouch, ch.approved),
		Merged:               len(humans),
		Untracked:            untracked,
		UntrackedRate:        ratio(untracked, len(humans)),
		CycleHistogram:       Histograms{UpperBoundsS: CycleBuckets, Human: histogram(cycle)},
		WithinDay:            Share{Count: within, N: len(cycle), Rate: ratio(within, len(cycle))},
		ChangeMix:            mix,
		MixTotal:             total,
		MixShare: MixShare{
			Feature: ratio(total.Feature, all), Bugfix: ratio(total.Bugfix, all),
			Hotfix: ratio(total.Hotfix, all), Other: ratio(total.Other, all),
		},
	}
}

// ---- Throughput ------------------------------------------------------------

// Throughput is how much the team finishes and in what portions: the
// people's merged PRs (mergedPRs) per week and their size. Flow velocity
// and batch size; stage promotions and bots are in neither.
type Throughput struct {
	// Merged are the people's merged PRs, as Review.Merged.
	Merged int `json:"merged"`
	// MergedPerWeek is Merged over the period's weeks, or over the part of
	// it since Bitbucket started reporting (Since); null without any, and
	// when that part is under a week: three PRs in six hours are no 84 a
	// week.
	MergedPerWeek *float64 `json:"merged_per_week"`
	// Since is when Bitbucket's data starts inside the period; null when it
	// covers it.
	Since *time.Time `json:"since"`
	// Umbrella, Housekeeping and Twice are the people's PRs merged in the
	// period left out of Merged and every PR number, by the first reason
	// that applies (notCounted).
	Umbrella     int `json:"umbrella"`
	Housekeeping int `json:"housekeeping"`
	Twice        int `json:"twice"`
	// HousekeepingRepos and HousekeepingWindowS are the housekeeping rule's
	// numbers (dataset.HousekeepingMinRepos, dataset.HousekeepingWindow in
	// seconds), for the page's words.
	HousekeepingRepos   int     `json:"housekeeping_repos"`
	HousekeepingWindowS float64 `json:"housekeeping_window_s"`
	// Library are Merged's PRs in library repos (dataset.PR.Library): work,
	// never rolled out, so out of Coverage.ToProd's base.
	Library int `json:"library"`
	// Coverage is how many of Merged each PR number rests on.
	Coverage PRCoverage `json:"coverage"`
	// BatchSize is the changed lines per merged PR, added plus removed, over
	// the PRs with a size (dataset.PR.LinesAdded): the median, not the mean,
	// which one large PR decides.
	BatchSize Spread `json:"batch_size"`
	// BatchFiles is the changed files per PR, over the same PRs.
	BatchFiles Spread `json:"batch_files"`
	// Weeks are both per week: a week of few PRs swings, so each carries
	// its counts.
	Weeks []ThroughputWeek `json:"weeks"`
}

// PRCoverage is, of the people's merged PRs (Count of N each), those a
// number can read: a PR missing here is missing from that number, not a
// PR that took no time.
type PRCoverage struct {
	// ToProd: a production deploy before the period's end shipped it
	// (Coverage.OnProd), so it is in the time to production unless it has
	// no start (Delivery.LeadTime.Unmeasured). Of Merged less
	// Throughput.Library (Coverage.Merged): a library's PR is never rolled
	// out, so never one not traced.
	ToProd Share `json:"to_prod"`
	// Sized: noergler's rollup gives its size (prSize), so it is in the
	// batch size.
	Sized Share `json:"sized"`
	// Opened: its pr:opened was seen, so it is in the cycle time.
	Opened Share `json:"opened"`
}

func share(count, n int) Share { return Share{Count: count, N: n, Rate: ratio(count, n)} }

// ThroughputWeek is one week's merged PRs and their size.
type ThroughputWeek struct {
	Merged int `json:"merged"`
	// Sized are the week's PRs with a size, BatchP50 their median changed
	// lines; null without one.
	Sized    int      `json:"sized"`
	BatchP50 *float64 `json:"batch_p50"`
}

// prSize is a merged PR's changed lines and files as noergler's final
// rollup counts them. No size without a rollup, and none for one without a
// line: a close whose diff noergler could not fetch reports 0, which is no
// empty PR.
func prSize(pr dataset.PR) (lines, files float64, ok bool) {
	if pr.LinesAdded == nil || pr.LinesRemoved == nil {
		return 0, 0, false
	}
	n := *pr.LinesAdded + *pr.LinesRemoved
	if n <= 0 {
		return 0, 0, false
	}
	if pr.FilesChanged != nil {
		files = float64(*pr.FilesChanged)
	}
	return float64(n), files, true
}

// throughput is the section over the human PRs merged in the period, from
// mergedPRs, of ds over p with the product filter f.
func throughput(ds *dataset.Dataset, humans []dataset.PR, p Period, f filter) Throughput {
	out := Throughput{Merged: len(humans), Weeks: make([]ThroughputWeek, len(p.Weeks)),
		HousekeepingRepos: dataset.HousekeepingMinRepos, HousekeepingWindowS: dataset.HousekeepingWindow.Seconds()}
	// Bitbucket started reporting inside the period: the rate is over the
	// weeks it reported, not the empty ones before, as the deploy rate.
	span := p.Span
	if start := ds.Meta.DataStart.Bitbucket; start != nil && start.After(p.From) {
		out.Since = start
		span = float64(p.To.Sub(*start)) / float64(7*24*time.Hour)
	}
	if span >= 1 {
		out.MergedPerWeek = ptr(float64(len(humans)) / span)
	}
	for _, pr := range peopleMerged(ds, p, f) {
		switch notCounted(pr) {
		case NotCountedUmbrella:
			out.Umbrella++
		case NotCountedHousekeeping:
			out.Housekeeping++
		case NotCountedTwice:
			out.Twice++
		}
	}
	toProd, opened := 0, 0
	for _, pr := range humans {
		switch {
		case pr.Library:
			out.Library++
		case firstDeployBefore(pr, ds.Meta.ProductionStage, p.To) != nil:
			toProd++
		}
		if pr.OpenedAt != nil {
			opened++
		}
	}
	var lines, files []float64
	weekly := make([][]float64, len(p.Weeks))
	for _, pr := range humans {
		i := -1
		if pr.MergedAt != nil {
			i = p.WeekIndex(*pr.MergedAt)
		}
		if i >= 0 {
			out.Weeks[i].Merged++
		}
		l, fc, ok := prSize(pr)
		if !ok {
			continue
		}
		lines, files = append(lines, l), append(files, fc)
		if i >= 0 {
			weekly[i] = append(weekly[i], l)
		}
	}
	out.BatchSize, out.BatchFiles = spread(lines), spread(files)
	for i, w := range weekly {
		out.Weeks[i].Sized, out.Weeks[i].BatchP50 = len(w), Median(w)
	}
	n := len(humans)
	out.Coverage = PRCoverage{ToProd: share(toProd, n-out.Library), Sized: share(len(lines), n), Opened: share(opened, n)}
	return out
}

// ---- Costs -----------------------------------------------------------------

// CostRow is the spend of one product or repo.
type CostRow struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// Costs is the noergler spend.
type Costs struct {
	// Total is all rollups closed in the window, whatever the outcome,
	// stage promotions included: the spend is real.
	Total float64 `json:"total"`
	// Merged, MergedReviews, PerKloc and Wasted leave stage promotions out:
	// a version bump is a few lines at a full review's price.
	Merged        float64 `json:"merged"`
	MergedReviews int     `json:"merged_reviews"`
	// PerKloc is per 1000 changed lines of merged PRs; null without line
	// counts.
	PerKloc *float64 `json:"per_kloc"`
	// Wasted is spent on PRs declined or deleted; WastedShare its part of
	// Total less Promotion, null when that is 0: both sides without
	// promotions, so the share does not move with promotion spend.
	Wasted      float64  `json:"wasted"`
	WastedShare *float64 `json:"wasted_share"`
	// Promotion is spent on stage-promotion PRs (GitOps repos), whatever
	// the outcome, over PromotionReviews of them.
	Promotion        float64 `json:"promotion"`
	PromotionReviews int     `json:"promotion_reviews"`
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
	total, merged, wasted, promotion := new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)
	var lines int64
	mergedN, promotionN := 0, 0
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
		switch {
		case r.GitOps:
			promotion.Add(promotion, v)
			promotionN++
		case outcome == "merged":
			merged.Add(merged, v)
			mergedN++
			if r.LinesAdded != nil {
				lines += *r.LinesAdded
			}
			if r.LinesRemoved != nil {
				lines += *r.LinesRemoved
			}
		case outcome == "declined" || outcome == "deleted":
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
		Total:            float(total),
		Merged:           float(merged),
		MergedReviews:    mergedN,
		Wasted:           float(wasted),
		Promotion:        float(promotion),
		PromotionReviews: promotionN,
		Rows:             []CostRow{},
		ReviewedRepos:    len(repos),
	}
	if work := new(big.Rat).Sub(total, promotion); work.Sign() > 0 {
		out.WastedShare = ptr(float(new(big.Rat).Quo(wasted, work)))
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
