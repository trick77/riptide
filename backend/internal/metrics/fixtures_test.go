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

func product(key string) Params { return Params{Period: p4(), Product: key} }

func pr(mod ...func(*dataset.PR)) dataset.PR {
	p := dataset.PR{
		Repo: "kasse/kasse-api", Project: "kasse", PRID: 1, Title: s("Pay by card"), ChangeType: s("feature"),
		JiraKeys: []string{"KAS-1"}, State: "merged", OpenedAt: agoP(2), ReadyAt: agoP(2), FirstReviewAt: agoP(1.9),
		ApprovedAt: agoP(1.9), ApprovedOnFirstTouch: true, MergedAt: agoP(1.8), FirstCommitAt: agoP(3),
		MergeCommit: s(strings.Repeat("a", 40)), Environments: []dataset.EnvironmentDeploy{},
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

func leadTimeRow(mod ...func(*dataset.LeadTime)) dataset.LeadTime {
	l := dataset.LeadTime{
		Repo: "kasse/kasse-api", Project: "kasse", Environment: "prod", CommitSHA: strings.Repeat("b", 40),
		AuthoredAt: ago(3), FirstDeployedAt: ago(1), LeadTimeS: 2 * 86400,
	}
	for _, m := range mod {
		m(&l)
	}
	return l
}

func run(mod ...func(*dataset.Run)) dataset.Run {
	r := dataset.Run{
		Source: "jenkins", PipelineName: "kasse-api/master", Branch: "master", BranchKind: dataset.BranchMain,
		BuildType: s("build"), RunID: "1", CommitSHA: s(strings.Repeat("c", 40)), Status: s("SUCCESS"),
		StartedAt: agoP(1), FinishedAt: agoP(0.99), DurationS: n(600), Repo: s("kasse/kasse-api"), Project: s("kasse"),
		ImageRefs: []string{},
	}
	for _, m := range mod {
		m(&r)
	}
	return r
}

func reviewRow(mod ...func(*dataset.Review)) dataset.Review {
	r := dataset.Review{
		PRKey: "kasse/kasse-api#1", Repo: s("kasse/kasse-api"), Project: s("kasse"), Outcome: s("merged"),
		CostUSD: s("0.500000"), LinesAdded: n(400), LinesRemoved: n(100), FilesChanged: n(5), FindingsCount: n(2),
		TotalRuns: n(1), ClosedAt: ago(1),
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
		PRs: []dataset.PR{}, Deploys: []dataset.Deploy{}, LeadTimes: []dataset.LeadTime{},
		Runs: []dataset.Run{}, Reviews: []dataset.Review{},
	}
	for _, m := range mod {
		m(ds)
	}
	return ds
}

// chainOf and reviewOf are the sections as Compute builds them.
func chainOf(ds *dataset.Dataset, p Period, f filter) rawChain {
	return chain(ds, mergedPRs(ds, p, f, false))
}

func reviewOf(ds *dataset.Dataset, p Period, f filter) Review {
	return review(mergedPRs(ds, p, f, false), mergedPRs(ds, p, f, f.bots), p, chainOf(ds, p, f))
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
