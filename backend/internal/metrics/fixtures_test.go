package metrics

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
)

// Builders for datasets in tests: a row with sensible defaults, changed
// field by field. Synthetic names only.

var zurich = func() *time.Location {
	l, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		panic(err)
	}
	return l
}()

// now is a Thursday, KW 46.
var now = time.Date(2026, 11, 12, 12, 0, 0, 0, zurich)

// ago is the instant days (fractions allowed) before now.
func ago(days float64) time.Time {
	return now.Add(-time.Duration(days * float64(24*time.Hour))).UTC()
}

func agoP(days float64) *time.Time { return ptr(ago(days)) }

func s(v string) *string { return &v }

func n(v int64) *int64 { return &v }

func p4() Period { return NewPeriod(4, now, zurich) }

func all() Params { return Params{Period: p4()} }

// noPrevious is a previous period without data, under ds's meta.
func noPrevious(ds *dataset.Dataset) *dataset.Dataset { return &dataset.Dataset{Meta: ds.Meta} }

// buildAlone is build with no previous period: ds names the repos, nothing
// compared.
func buildAlone(ds *dataset.Dataset, p Period, f filter) Build {
	return build(ds, p, f, &baseline{repos: newRepoNamer(ds.Runs)})
}

func product(key string) Params { return Params{Period: p4(), Product: key} }

func pr(mod ...func(*dataset.PR)) dataset.PR {
	p := dataset.PR{
		Repo: "kasse/kasse-api", Project: "kasse", PRID: 1, Title: s("Pay by card"), ChangeType: s("feature"),
		JiraKeys: []string{"KAS-1"}, State: "merged", OpenedAt: agoP(2), ReadyAt: agoP(2), FirstReviewAt: agoP(1.9),
		ApprovedAt: agoP(1.9), ApprovedOnFirstTouch: true, MergedAt: agoP(1.8), FirstCommitAt: agoP(3),
		MergeCommit: s(strings.Repeat("a", 40)), TrunkAt: agoP(1.8), TrunkBranch: s("master"),
		Environments: []dataset.EnvironmentDeploy{},
	}
	for _, m := range mod {
		m(&p)
	}
	return p
}

func deploy(mod ...func(*dataset.Deploy)) dataset.Deploy {
	d := dataset.Deploy{
		App: "kasse-prod", Project: s("kasse"), Environment: s("prod"), Revision: "r1", At: ago(1), Phase: "Succeeded",
		DurationS: n(40), Images: []string{"reg/kasse-api:1.1"}, Kind: s("release"), NewImages: []string{"reg/kasse-api:1.1"},
	}
	for _, m := range mod {
		m(&d)
	}
	return d
}

// shipped is a merged PR on production: deployed there an hour ago, its
// first commit seconds before.
func shipped(seconds float64, mod ...func(*dataset.PR)) dataset.PR {
	at := ago(1.0 / 24)
	return pr(append([]func(*dataset.PR){func(p *dataset.PR) {
		p.FirstCommitAt = ptr(at.Add(-time.Duration(seconds * float64(time.Second))))
		p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: at, Route: dataset.RouteCompare}}
	}}, mod...)...)
}

func run(mod ...func(*dataset.Run)) dataset.Run {
	r := dataset.Run{
		Source: "jenkins", PipelineName: "kasse-api/master", Branch: "master", BranchKind: dataset.BranchMain,
		BuildType: s("build"), RunID: "1", CommitSHA: s(strings.Repeat("c", 40)), Status: s("SUCCESS"),
		StartedAt: agoP(1), FinishedAt: agoP(0.99), OccurredAt: ago(0.99), DurationS: n(600), Repo: s("kasse/kasse-api"), Project: s("kasse"),
		ImageRefs: []string{},
	}
	for _, m := range mod {
		m(&r)
	}
	return r
}

func reviewRow(mod ...func(*dataset.Review)) dataset.Review {
	r := dataset.Review{
		Repo: s("kasse/kasse-api"), Project: s("kasse"), Outcome: s("merged"),
		CostUSD: s("0.500000"), LinesAdded: n(400), LinesRemoved: n(100),
	}
	for _, m := range mod {
		m(&r)
	}
	return r
}

func data(mod ...func(*dataset.Dataset)) *dataset.Dataset {
	ds := &dataset.Dataset{
		Meta: dataset.Meta{
			Team: "team-a", From: ago(25), To: now.UTC(), GeneratedAt: now.UTC(), ProductionStage: "prod",
			DataStart:     dataset.SourceTimes{Bitbucket: agoP(60), Pipeline: agoP(10), ArgoCD: agoP(60), Noergler: agoP(60)},
			DataEnd:       dataset.SourceTimes{Bitbucket: agoP(0.1), Pipeline: agoP(0.2), ArgoCD: agoP(0.3), Noergler: agoP(0.4)},
			BuildDataFrom: agoP(10), BuildTypeFrom: agoP(10),
		},
		Products: []dataset.Product{
			{Key: "kasse", Repos: []string{"kasse/kasse-api", "kasse/kasse-gitops"}, Apps: []string{"kasse-prod"}},
			{Key: "portal", Repos: []string{"portal/portal-web"}, Apps: []string{}},
		},
		PRs: []dataset.PR{}, Deploys: []dataset.Deploy{}, Runs: []dataset.Run{}, Reviews: []dataset.Review{},
		Traces: map[string]dataset.RepoTrace{},
	}
	for _, m := range mod {
		m(ds)
	}
	return ds
}

// chainOf and reviewOf are the sections as Compute builds them.
func chainOf(ds *dataset.Dataset, p Period, f filter) rawChain {
	return chain(ds, mergedPRs(ds, p, f), p.To)
}

func reviewOf(ds *dataset.Dataset, p Period, f filter) Review {
	return review(mergedPRs(ds, p, f), p, chainOf(ds, p, f))
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6*math.Max(1, math.Abs(b)) }

func expectNear(t *testing.T, what string, got *float64, want float64) {
	t.Helper()
	if got == nil || !near(*got, want) {
		t.Errorf("%s = %v, want %v", what, show(got), want)
	}
}

func expectNil(t *testing.T, what string, got *float64) {
	t.Helper()
	if got != nil {
		t.Errorf("%s = %v, want null", what, *got)
	}
}

func show(v *float64) any {
	if v == nil {
		return "null"
	}
	return *v
}
