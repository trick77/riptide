package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/trick77/riptide/internal/analysis"
	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/metrics"
	"github.com/trick77/riptide/internal/store"
)

// The read API lives on the UI listener only, and the UI listener serves
// nothing of the collector's.
func TestReadAPIIsOnTheUIListenerOnly(t *testing.T) {
	h := newHarness(t, nil)
	for _, path := range []string{"/api/metrics?team=checkout", "/api/facts?team=checkout"} {
		w := h.serve(newRequest(http.MethodGet, path, nil))
		expectStatus(t, w, http.StatusNotFound)
		expectBody(t, w, `{"detail":"Not Found"}`)
	}
	if len(h.mem.params) != 0 {
		t.Error("the collector listener reached the reader")
	}
	for _, path := range []string{"/webhooks/pipeline", "/auth/ping", "/openapi.yaml"} {
		if got := h.serveUI(newRequest(http.MethodGet, path, nil)); got.Code != http.StatusNotFound {
			t.Errorf("UI listener %s = %d", path, got.Code)
		}
	}
	r := newRequest(http.MethodGet, "/healthz", nil)
	r.RemoteAddr = "127.0.0.1:40000"
	if got := h.serveUI(r); got.Code != http.StatusNotFound {
		t.Errorf("UI listener /healthz = %d", got.Code)
	}
	if got := h.serveUI(newRequest(http.MethodPost, "/api/metrics", nil)); got.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/metrics = %d", got.Code)
	}
}

// The team picker's list: the config's teams, sorted, per the spec.
func TestTeams(t *testing.T) {
	h := newHarness(t, nil)
	w := h.serveUI(newRequest(http.MethodGet, "/api/teams", nil))
	expectStatus(t, w, http.StatusOK)
	var body Teams
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !slices.IsSorted(body.Teams) || !slices.Contains(body.Teams, "checkout") {
		t.Errorf("teams = %v", body.Teams)
	}
	// The picker's and the schedule's teams: the harness config's, sorted.
	want := []string{"checkout", "platform"}
	if !slices.Equal(body.Teams, want) {
		t.Errorf("teams = %v, want %v", body.Teams, want)
	}
	if got := (Deps{Runtime: h.runtime}).TeamNames(); !slices.Equal(got, want) {
		t.Errorf("TeamNames = %v, want %v", got, want)
	}
	validateResponse(t, specRouter(t), http.MethodGet, "/api/teams", w)
}

// The SPA's catch-all must not swallow /api: an unknown API path is the
// JSON 404 every other error is, and a client route that does not exist is
// a 404 too, never the shell.
func TestUIListenerRoutes(t *testing.T) {
	h := newHarness(t, nil)
	// The raw-rows endpoint is gone: nothing reads rows to compute from.
	for _, path := range []string{"/api/nope", "/api/dataset?team=checkout"} {
		w := h.serveUI(newRequest(http.MethodGet, path, nil))
		expectStatus(t, w, http.StatusNotFound)
		expectBody(t, w, `{"detail":"Not Found"}`)
	}
	// A method the SPA's or the API's GET patterns do not take: the mux's
	// 405, in the {"detail": ...} shape like every other error.
	for _, path := range []string{"/api/teams", "/api/nope", "/anything"} {
		w := h.serveUI(newRequest(http.MethodPost, path, nil))
		expectStatus(t, w, http.StatusMethodNotAllowed)
		expectBody(t, w, `{"detail":"Method Not Allowed"}`)
	}
	// Built or not (the placeholder answers 503), the shell's route is served.
	if got := h.serveUI(newRequest(http.MethodGet, "/", nil)); got.Code != http.StatusOK && got.Code != http.StatusServiceUnavailable {
		t.Errorf("GET / = %d", got.Code)
	}
	if got := h.serveUI(newRequest(http.MethodGet, "/nope", nil)); got.Code != http.StatusNotFound {
		t.Errorf("GET /nope = %d", got.Code)
	}
}

// The period is closed calendar weeks in the viewer's zone, ending with the
// last one before the week running now; the metrics read the weeks before
// to compare and the running week apart, the facts read on to now.
func TestReadParams(t *testing.T) {
	h := newHarness(t, nil)
	now := fixedNow.Truncate(time.Second) // Wednesday 2030-01-02 03:04:05 UTC
	utc := func(y int, m time.Month, d, hh int) time.Time { return time.Date(y, m, d, hh, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		query string
		// from is the period's first Monday, monday the running week's.
		from, monday time.Time
		weeks        int
	}{
		{"team=checkout", utc(2029, 12, 3, 0), utc(2029, 12, 31, 0), 4},
		{"team=%20checkout%20", utc(2029, 12, 3, 0), utc(2029, 12, 31, 0), 4},
		{"team=checkout&weeks=12&tz=UTC", utc(2029, 10, 8, 0), utc(2029, 12, 31, 0), 12},
		// Monday 00:00 in Zurich, CET then; twelve weeks back it was CEST.
		{"team=checkout&tz=Europe/Zurich", utc(2029, 12, 2, 23), utc(2029, 12, 30, 23), 4},
		{"team=checkout&weeks=12&tz=Europe/Zurich", utc(2029, 10, 7, 22), utc(2029, 12, 30, 23), 12},
		{"team=checkout&weeks=26&product=nope", utc(2029, 7, 2, 0), utc(2029, 12, 31, 0), 26},
	} {
		check := func(path string, p dataset.Params, from, to time.Time, factsOnly bool) {
			t.Helper()
			if p.Team != "checkout" || !p.From.Equal(from) || !p.To.Equal(to) || p.ProductionStage != "prod" ||
				!p.Now.Equal(now) || p.FactsOnly != factsOnly {
				t.Errorf("%s%s: params = %+v, want %v to %v", path, c.query, p, from, to)
			}
			// The ingest-time detector: configured authors (login or display
			// name), the bot-shaped name heuristic; a person is not a bot.
			if p.Automated == nil || !p.Automated("renovate-bot", "") || !p.Automated("svc-x", "Dependabot[bot]") ||
				!p.Automated("deploy-bot", "") || p.Automated("dev-anna", "Anna Example") {
				t.Errorf("%s%s: automation detection wrong", path, c.query)
			}
		}
		// The metrics: the period, the weeks right before, the running week
		// (no lead times or reviews), side by side in any order.
		h.mem.params = nil
		expectStatus(t, h.serveUI(newRequest(http.MethodGet, "/api/metrics?"+c.query, nil)), http.StatusOK)
		reads := map[time.Time]dataset.Params{}
		for _, x := range h.mem.params {
			reads[x.To] = x
		}
		if len(h.mem.params) != 3 || len(reads) != 3 {
			t.Fatalf("/api/metrics?%s: reads = %+v", c.query, h.mem.params)
		}
		check("/api/metrics?", reads[c.monday], c.from, c.monday, false)
		prev := reads[c.from]
		if !prev.To.Equal(c.from) || prev.From.Before(c.from.AddDate(0, 0, -7*c.weeks-1)) || prev.FactsOnly {
			t.Errorf("/api/metrics?%s: previous = %+v", c.query, prev)
		}
		check("/api/metrics?", reads[now], c.monday, now, true)
		// The facts: the period's weeks on to now, the running week marked.
		h.mem.params = nil
		expectStatus(t, h.serveUI(newRequest(http.MethodGet, "/api/facts?"+c.query, nil)), http.StatusOK)
		if len(h.mem.params) != 1 {
			t.Fatalf("/api/facts?%s: reads = %d", c.query, len(h.mem.params))
		}
		check("/api/facts?", h.mem.params[0], c.from, now, true)

		w := h.serveUI(newRequest(http.MethodGet, "/api/metrics?"+c.query, nil))
		var body struct {
			Period struct {
				To    time.Time `json:"to"`
				Weeks []struct {
					KW int `json:"kw"`
				} `json:"weeks"`
			} `json:"period"`
			RunningWeek struct {
				Week struct {
					KW int `json:"kw"`
				} `json:"week"`
				To time.Time `json:"to"`
			} `json:"running_week"`
			Product *string `json:"product"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Period.Weeks) != c.weeks || body.Product != nil {
			t.Errorf("%s: body = %s", c.query, w.Body)
		}
		// The last closed week is 2029's KW 52, the running one 2030's KW 1.
		if last := body.Period.Weeks[len(body.Period.Weeks)-1].KW; last != 52 || !body.Period.To.Equal(c.monday) {
			t.Errorf("%s: last week KW %d to %v, want 52 to %v", c.query, last, body.Period.To, c.monday)
		}
		if body.RunningWeek.Week.KW != 1 || !body.RunningWeek.To.Equal(now) {
			t.Errorf("%s: running week = %+v", c.query, body.RunningWeek)
		}
	}

	for _, c := range []struct {
		query  string
		status int
		detail string
	}{
		{"", http.StatusUnprocessableEntity, "query parameter team is required"},
		{"team=nobody", http.StatusNotFound, "unknown team"},
		{"team=checkout&weeks=5", http.StatusUnprocessableEntity, "weeks must be one of 4, 12, 26"},
		{"team=checkout&weeks=x", http.StatusUnprocessableEntity, "weeks must be one of 4, 12, 26"},
		{"team=checkout&tz=Mars/Olympus", http.StatusUnprocessableEntity, "tz must be an IANA time zone"},
	} {
		for _, path := range []string{"/api/metrics?", "/api/facts?"} {
			w := h.serveUI(newRequest(http.MethodGet, path+c.query, nil))
			expectStatus(t, w, c.status)
			expectBody(t, w, `{"detail":"`+c.detail+`"}`)
		}
	}
}

// Every field of every section set, so each item schema is exercised; the
// e2e test below covers the empty sections.
func TestReadMatchesOpenAPISpec(t *testing.T) {
	router := specRouter(t)
	h := newHarness(t, nil)
	at := fixedNow.Add(-time.Hour)
	before := at.Add(-30 * time.Hour)
	s, n := func(v string) *string { return &v }, func(v int64) *int64 { return &v }
	ds := &dataset.Dataset{
		Meta: dataset.Meta{Team: "checkout", From: at, To: at, GeneratedAt: at, ProductionStage: "prod",
			DataStart: dataset.SourceTimes{Bitbucket: &before, Pipeline: &before, ArgoCD: &before, Noergler: &before},
			DataEnd:   dataset.SourceTimes{Bitbucket: &at, Pipeline: &at, ArgoCD: &at, Noergler: &at}, BuildDataFrom: &before, BuildTypeFrom: &before,
			Silences: []dataset.Silence{{Source: dataset.SourceBitbucket, From: before, To: at, UnseenRuns: 2, Lost: true},
				{Source: dataset.SourcePipeline, From: before, To: fixedNow, Ongoing: true}},
			Lost:       []dataset.Silence{{Source: dataset.SourceBitbucket, From: before, To: at, UnseenRuns: 2, Lost: true}},
			RepoStarts: []dataset.RepoStart{{Repo: "kasse/kasse-api", Noergler: &before}}},
		Products: []dataset.Product{{Key: "kasse", Repos: []string{"kasse/kasse-api"}, Apps: []string{"kasse-prod"}}},
		PRs: []dataset.PR{{Repo: "kasse/kasse-api", Project: "kasse", PRID: 7, Title: s("Rabattregeln"), ChangeType: s("feature"),
			JiraKeys: []string{"KAS-7"}, State: "merged", OpenedAt: &before, ReadyAt: &before, FirstReviewAt: &before, ApprovedAt: &before,
			ApprovedOnFirstTouch: true, MergedAt: &at, FirstCommitAt: &before, MergeCommit: s("abc1234"), TrunkAt: &at,
			Environments: []dataset.EnvironmentDeploy{{Environment: "intg", FirstDeployedAt: at, Route: dataset.RouteImage},
				{Environment: "prod", FirstDeployedAt: at, Route: dataset.RouteCompare, LowerBound: true}}},
			// Merged into a feature branch: counted, marked.
			{Repo: "kasse/kasse-api", Project: "kasse", PRID: 8, Title: s("Teilschritt"), State: "merged", OpenedAt: &before,
				MergedAt: &at, TargetBranch: s("feature/umbau"), OffTrunk: true},
			// The umbrella that carried it: listed, in no PR number.
			{Repo: "kasse/kasse-api", Project: "kasse", PRID: 10, Title: s("Umbau"), State: "merged", OpenedAt: &before,
				MergedAt: &at, TargetBranch: s("master"), Umbrella: true},
			// A library's: never rolled out.
			{Repo: "kasse/kasse-lib", Project: "kasse", PRID: 11, Title: s("Hilfsfunktion"), State: "merged", OpenedAt: &before,
				MergedAt: &at, Library: true}},
		// Last heard of before Bitbucket's events were lost.
		OpenPRs: []dataset.OpenPR{{Repo: "kasse/kasse-api", Project: "kasse", PRID: 9, Title: s("Liegt"), OpenedAt: before,
			LastEventAt: before, Unknown: true}},
		Deploys: []dataset.Deploy{{App: "kasse-prod", Project: s("kasse"), Environment: s("prod"), Revision: "abc1234",
			At: at, Phase: "Succeeded", DurationS: n(45), Images: []string{"reg/a:1"}, Kind: s("release"),
			NewImages: []string{"reg/a:1"}},
			{App: "kasse-neu", Project: s("kasse"), Environment: s("prod"), Revision: "def5678", At: at, Phase: "Succeeded",
				Images: []string{"reg/b:1"}, Kind: s("first_seen"), FirstRelease: true, NewImages: []string{}},
			{App: "kasse-prod", Project: s("kasse"), Environment: s("prod"), Revision: "abc9999", At: at, Phase: "Error",
				NewImages: []string{}},
			{App: "log-shipper", Environment: s("prod"), Revision: "fed4321", At: at, Phase: "Succeeded",
				Images: []string{"reg/m:1"}, Kind: s("release"), NewImages: []string{"reg/m:1"}, Infra: true}},
		Traces: map[string]dataset.RepoTrace{"kasse/kasse-api": {DeployedSince: &before}},
		Reviews: []dataset.Review{{Repo: s("kasse/kasse-api"), Project: s("kasse"),
			Outcome: s("merged"), CostUSD: s("0.123456"), LinesAdded: n(1), LinesRemoved: n(1)}},
	}
	// Enough runs for every hotspot: a slow, failing repo and an outlier.
	for i := range 12 {
		status, d := "SUCCESS", int64(600)
		if i%3 == 0 {
			status = "FAILURE"
		}
		if i == 0 {
			d = 60000
		}
		ds.Runs = append(ds.Runs, dataset.Run{Source: "jenkins", PipelineName: "kasse-api/master", Branch: "master", BranchKind: "main",
			BuildType: s("release"), RunID: "1", CommitSHA: s("abc1234"), Status: s(status), StartedAt: &before, FinishedAt: &at,
			DurationS: n(d), Repo: s("kasse/kasse-api"), Project: s("kasse"), ImageRefs: []string{"reg/a:1"}})
	}
	// A bot's branch: in no build number, counted apart.
	ds.Runs = append(ds.Runs, dataset.Run{Source: "jenkins", PipelineName: "kasse-api/renovate%2Flib", Branch: "renovate/lib",
		BranchKind: "other", RunID: "2", CommitSHA: s("abc4321"), Status: s("FAILURE"), StartedAt: &before, FinishedAt: &at,
		DurationS: n(120), Repo: s("kasse/kasse-api"), Project: s("kasse"), ImageRefs: []string{}, Bot: true})
	h.mem.dataset = ds
	for _, path := range []string{"/api/metrics", "/api/facts"} {
		for _, q := range []string{"team=checkout&tz=Europe/Zurich", "team=checkout&product=kasse", "", "team=nobody", "team=checkout&weeks=3"} {
			validateResponse(t, router, http.MethodGet, path, h.serveUI(newRequest(http.MethodGet, path+"?"+q, nil)))
		}
	}
	// The rows above reach the item schemas: an empty array validates
	// against any. They fall in the running week: the facts list them, the
	// closed period's numbers count none.
	for path, want := range map[string][]string{
		"/api/metrics": {`"unknown":1`, `"unseen_runs":2`, `"lost":true`, `"bot_runs":{`, `"grace_days":14`},
		"/api/facts": {`"not_counted":"umbrella"`, `"via_umbrella":true`, `"library":true`, `"state":"unknown"`,
			`"phase":"Error"`, `"infra":true`, `"first_deploy":true`,
			// #8 off the trunk, no umbrella merged on.
			`"prod_state":"not_placeable"`, `"prod_reason":"off_trunk"`},
	} {
		body := h.serveUI(newRequest(http.MethodGet, path+"?team=checkout", nil)).Body.String()
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s has no %s", path, w)
			}
		}
	}
}

// The per-repo view and the trends of a closed period, every row and note
// kind reached, match the contract.
func TestRepoViewMatchesOpenAPISpec(t *testing.T) {
	h := newHarness(t, nil)
	day := func(d float64) time.Time { return fixedNow.Add(-time.Duration(d * float64(24*time.Hour))) }
	at := func(d float64) *time.Time { return ptrTo(day(d)) }
	s, n := func(v string) *string { return &v }, func(v int64) *int64 { return &v }
	start := day(70)
	ds := &dataset.Dataset{
		Meta: dataset.Meta{Team: "checkout", From: day(30), To: fixedNow, GeneratedAt: fixedNow, ProductionStage: "prod",
			DataStart: dataset.SourceTimes{Bitbucket: &start, Pipeline: &start, ArgoCD: &start, Noergler: &start},
			DataEnd:   dataset.SourceTimes{Bitbucket: &fixedNow, Pipeline: &fixedNow, ArgoCD: &fixedNow, Noergler: &fixedNow},
			// A lost silence over working days of the period: a gap in the
			// trends.
			Lost:     []dataset.Silence{{Source: dataset.SourceBitbucket, From: day(16), To: day(14), UnseenRuns: 1, Lost: true}},
			Silences: []dataset.Silence{}, RepoStarts: []dataset.RepoStart{}},
		Products: []dataset.Product{{Key: "kasse", Repos: []string{"kasse/kasse-api"}, Apps: []string{"kasse-prod"}}},
		Apps:     []dataset.App{{Name: "kasse-prod", Project: s("kasse")}, {Name: "lager-prod", Project: s("lager")}},
		Traces: map[string]dataset.RepoTrace{
			"kasse/kasse-api": {DeployedSince: at(60), Releases: []dataset.TraceRelease{
				{App: "kasse-prod", Environment: "prod", At: day(45), Known: day(60)}}},
			"lager/lager-api": {DeployedSince: at(60), Releases: []dataset.TraceRelease{
				{App: "lager-prod", Environment: "prod", At: day(5), Known: day(60)},
				{App: "lager-prod", Environment: "prod", At: day(12), Known: day(60)}}},
		},
	}
	merged := func(repo string, id int64, d float64, mod func(*dataset.PR)) dataset.PR {
		pr := dataset.PR{Repo: repo, Project: strings.Split(repo, "/")[0], PRID: id, State: "merged",
			OpenedAt: at(d + 2), ReadyAt: at(d + 2), FirstReviewAt: at(d + 1), ApprovedAt: at(d + 1), MergedAt: at(d),
			FirstCommitAt: at(d + 3), MergeCommit: s("abc"), TrunkAt: at(d), LinesAdded: n(40 * id), LinesRemoved: n(id)}
		if mod != nil {
			mod(&pr)
		}
		return pr
	}
	for i := range 6 {
		d := 8 + float64(i)
		ds.PRs = append(ds.PRs, merged("lager/lager-api", int64(i+1), d, func(p *dataset.PR) {
			p.Environments = []dataset.EnvironmentDeploy{{Environment: "prod", FirstDeployedAt: day(d - 3), Route: dataset.RouteCompare}}
		}))
	}
	ds.PRs = append(ds.PRs, merged("kasse/kasse-api", 20, 9, nil), merged("kasse/kasse-lib", 21, 9,
		func(p *dataset.PR) { p.Library = true }), merged("portal/portal-web", 22, 9, nil))
	for i := range 6 {
		ds.Runs = append(ds.Runs, dataset.Run{Source: "jenkins", PipelineName: "lager-api/feature%2Fx", Branch: "feature/x",
			BranchKind: "feature", RunID: string(rune('a' + i)), CommitSHA: s("abc"), Status: s("SUCCESS"), StartedAt: at(9),
			FinishedAt: at(9), OccurredAt: day(9), DurationS: n(1200), Repo: s("lager/lager-api"), Project: s("lager"),
			ImageRefs: []string{}})
	}
	h.mem.dataset = ds
	w := h.serveUI(newRequest(http.MethodGet, "/api/metrics?team=checkout&tz=Europe/Zurich", nil))
	expectStatus(t, w, http.StatusOK)
	validateResponse(t, specRouter(t), http.MethodGet, "/api/metrics", w)
	for _, want := range []string{`"group":"none"`, `"group":"regular"`, `"group":"library"`, `"group":"unjudged"`,
		`"reason":"untraced"`, `"apart":true`, `"key":"no_release"`, `"verdict":"hint"`, `"gaps":[`, `"lost"`,
		`"running":true`, `"key":"build_duration","kind":"feature"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("metrics have no %s", want)
		}
	}
}

// analystMem is the in-memory store with a fixed analysis status: the
// contract's rarer shapes, which the e2e data never reaches.
type analystMem struct {
	*memStore
	st analysis.Status
}

func (a *analystMem) Status(context.Context, analysis.View) (analysis.Status, error) {
	return a.st, nil
}
func (a *analystMem) Ask(context.Context, analysis.View) error { return nil }

// An outdated analysis, the limits with a span (a lost silence closing after
// the period, a source silent at the end) and a not-measurable reason match
// the contract.
func TestAnalysisShapesMatchOpenAPISpec(t *testing.T) {
	cur, prev := metrics.ClosedPeriod(4, fixedNow, time.UTC), metrics.ClosedPrevious(4, fixedNow, time.UTC)
	from, end := cur.To.Add(-60*time.Hour), cur.To.Add(30*time.Hour)
	share := metrics.Share{Count: 0, N: 3}
	a := metrics.Assessment{Period: cur, Previous: prev, Signals: []metrics.Signal{}, Focus: []metrics.FocusRepo{},
		Team: []metrics.Fact{}, QuietReasons: []string{metrics.QuietSourceGap, metrics.QuietNotMeasurable},
		NotMeasurable: ptrTo(metrics.NotMeasurableNoRelease),
		Limits: []metrics.Limit{
			{Source: dataset.SourceBitbucket, Kind: metrics.LimitSilence, From: from, To: &end},
			{Source: dataset.SourceNoergler, Kind: metrics.LimitSilentEnd, From: cur.To.Add(-9 * 24 * time.Hour), To: &cur.To},
			{Source: dataset.SourcePipeline, Kind: metrics.LimitStarts, From: from},
		},
		Coverage: metrics.PRCoverage{ToProd: share, Sized: share, Opened: share}}
	st := analysis.Status{State: analysis.StateNone, Enabled: true, Outdated: true, Assessment: a,
		Layout: analysis.Layout{Focus: []analysis.LayoutFocus{}, Cards: []analysis.LayoutCard{}, Also: []analysis.LayoutCard{}},
		Zone:   "UTC"}
	h := newHarness(t, &analystMem{memStore: newMemStore(), st: st})
	w := h.serveUI(newRequest(http.MethodGet, "/api/analysis?team=checkout", nil))
	expectStatus(t, w, http.StatusOK)
	validateResponse(t, specRouter(t), http.MethodGet, "/api/analysis", w)
	for _, want := range []string{`"outdated":true`, `"kind":"silence"`, `"kind":"silent_end"`, `"not_measurable":"no_release"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("no %s in %s", want, w.Body)
		}
	}
}

func ptrTo[T any](v T) *T { return &v }

// The assessment's read: the period and the previous one in full, the
// states' look-back past them lean (PRs and deploys only); its shared read
// takes the approvals and the branches from StateLookbackWeeks before its
// end, in every view alike.
func TestDatasetsStatesRead(t *testing.T) {
	h := newHarness(t, nil)
	var shared []dataset.SharedParams
	reader := sharedSpy{memStore: h.mem, got: &shared}
	d := Deps{Reader: reader, Runtime: h.runtime, Now: func() time.Time { return fixedNow }}
	cur, prev := metrics.ClosedPeriod(4, fixedNow, time.UTC), metrics.ClosedPrevious(4, fixedNow, time.UTC)
	ps := []metrics.Period{cur, prev, *metrics.StatePeriod(cur, prev)}
	if _, err := d.Datasets(context.Background(), "checkout", ps...); err != nil {
		t.Fatal(err)
	}
	if len(h.mem.params) != 3 {
		t.Fatalf("reads %+v, want 3", h.mem.params)
	}
	for _, p := range h.mem.params {
		want := p.From.Equal(ps[2].From)
		if p.Lean != want || p.FactsOnly != want || !p.ClassAt.Equal(cur.To) {
			t.Errorf("read %v..%v lean %v facts-only %v class %v, want lean only for the look-back", p.From, p.To,
				p.Lean, p.FactsOnly, p.ClassAt)
		}
	}
	if len(shared) != 1 || !shared[0].States || !shared[0].ApprovalsFrom.Equal(metrics.StateLookbackFrom(cur)) ||
		!shared[0].BranchesFrom.Equal(metrics.StateLookbackFrom(cur)) || !shared[0].From.Equal(ps[2].From) {
		t.Errorf("shared %+v, want the states' read", shared)
	}
	// A longer view reads the same approvals: what is judged does not
	// depend on the view's length.
	shared = nil
	cur12, prev12 := metrics.ClosedPeriod(12, fixedNow, time.UTC), metrics.ClosedPrevious(12, fixedNow, time.UTC)
	ps12 := []metrics.Period{cur12, prev12}
	if look := metrics.StatePeriod(cur12, prev12); look != nil {
		ps12 = append(ps12, *look)
	}
	if _, err := d.Datasets(context.Background(), "checkout", ps12...); err != nil {
		t.Fatal(err)
	}
	if len(shared) != 1 || !shared[0].ApprovalsFrom.Equal(metrics.StateLookbackFrom(cur)) {
		t.Errorf("12-week view's approvals from %v, want the 4-week view's %v", shared, metrics.StateLookbackFrom(cur))
	}
}

// sharedSpy records the shared read's parameters.
type sharedSpy struct {
	*memStore
	got *[]dataset.SharedParams
}

func (s sharedSpy) Shared(ctx context.Context, p dataset.SharedParams) (*dataset.Shared, error) {
	*s.got = append(*s.got, p)
	return s.memStore.Shared(ctx, p)
}

// A firing state card, a data-honesty card with its reason and an
// unmeasurable one (no threshold either) match the contract.
func TestStateCardsMatchOpenAPISpec(t *testing.T) {
	cur, prev := metrics.ClosedPeriod(4, fixedNow, time.UTC), metrics.ClosedPrevious(4, fixedNow, time.UTC)
	since := cur.To.Add(-31 * 24 * time.Hour)
	week := cur.Weeks[3]
	unit := func(name string, mod func(*metrics.StateUnit)) metrics.StateUnit {
		u := metrics.StateUnit{Name: name, Project: ptrTo("kasse"), Value: 31, Count: ptrTo(3), Names: []string{}}
		if mod != nil {
			mod(&u)
		}
		return u
	}
	card := func(key, unitKind string, th *metrics.StateThreshold, units ...metrics.StateUnit) *metrics.StateCard {
		return &metrics.StateCard{Key: key, Unit: unitKind, Count: len(units), Judged: len(units) + 1, Units: units,
			Worst: units[:min(3, len(units))], Threshold: th, At: cur.To, Facts: "deploys", ValueUnit: metrics.UnitDays}
	}
	signal := func(id, tone string, c *metrics.StateCard) metrics.Signal {
		return metrics.Signal{ID: id, Kind: metrics.SignalState, Tone: tone, Source: dataset.SourceArgoCD, Weight: 9,
			State: c, Drivers: []metrics.Driver{},
			Facts: []metrics.Fact{{ID: id + ".count", Label: "state_units", Unit: metrics.UnitCount, Value: ptrTo(1.0),
				Verdict: metrics.ChangeNone}}}
	}
	release := card(metrics.StateNoRelease, metrics.UnitProject,
		&metrics.StateThreshold{Key: "release_breach_days", Value: 28, Unit: metrics.ThresholdDays},
		unit("kasse", func(u *metrics.StateUnit) { u.Since, u.LowerBound, u.Query = &since, true, ptrTo("kasse-prod") }))
	release.Since = &since
	pause := card(metrics.StateMergePause, metrics.UnitProject,
		&metrics.StateThreshold{Key: "merge_pause_share", Value: 0.2, Unit: metrics.ThresholdShare,
			MinKey: ptrTo("merge_pause_waiting"), Min: ptrTo(3)},
		unit("kasse", func(u *metrics.StateUnit) { u.Week, u.Typical, u.Held, u.Lost = &week, ptrTo(6.5), true, true }))
	// A lasting state red for the unit that held it two sprints, with a span.
	branches := card(metrics.StateBranchOpen, metrics.UnitRepo,
		&metrics.StateThreshold{Key: "branch_open_days", Value: 14, Unit: metrics.ThresholdDays,
			WithinKey: ptrTo("branch_active_days"), Within: ptrTo(5.0), WithinUnit: ptrTo(metrics.ThresholdWorkingDays)},
		unit("kasse/kasse-api", func(u *metrics.StateUnit) { u.TwoSprints = true }))
	untraced := card(metrics.StateUntraced, metrics.UnitRepo, nil,
		unit("kasse/kasse-api", func(u *metrics.StateUnit) { u.Reason, u.Of = ptrTo(metrics.TraceInSilence), ptrTo(9) }))
	gone := card(metrics.StateUnmeasurable, metrics.UnitRepo, nil, unit("kasse/kasse-api", nil))
	gone.Was = ptrTo(metrics.StateApprovedWaiting)
	share := metrics.Share{Count: 0, N: 3}
	a := metrics.Assessment{Period: cur, Previous: prev, Focus: []metrics.FocusRepo{}, Team: []metrics.Fact{},
		Signals: []metrics.Signal{
			signal("state:no_release", metrics.ToneAttention, release),
			signal("state:merge_pause", metrics.ToneAttention, pause),
			signal("state:branch_open", metrics.ToneAttention, branches),
			signal("state:untraced", metrics.ToneLimit, untraced),
			signal("state:unmeasurable:approved_waiting", metrics.ToneLimit, gone),
		},
		QuietReasons: []string{metrics.QuietFindings}, Limits: []metrics.Limit{},
		Coverage: metrics.PRCoverage{ToProd: share, Sized: share, Opened: share}}
	st := analysis.Status{State: analysis.StateNone, Enabled: true, Assessment: a,
		Layout: analysis.Layout{Focus: []analysis.LayoutFocus{}, Cards: []analysis.LayoutCard{}, Also: []analysis.LayoutCard{}},
		Zone:   "UTC"}
	h := newHarness(t, &analystMem{memStore: newMemStore(), st: st})
	w := h.serveUI(newRequest(http.MethodGet, "/api/analysis?team=checkout", nil))
	expectStatus(t, w, http.StatusOK)
	validateResponse(t, specRouter(t), http.MethodGet, "/api/analysis", w)
	for _, want := range []string{`"key":"no_release"`, `"threshold":null`, `"within":5`, `"within_unit":"working_days"`,
		`"two_sprints":true`, `"lost":true`, `"was":"approved_waiting"`, `"reason":"in_silence"`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("no %s in %s", want, w.Body)
		}
	}
}

func TestReadFailureIs500AndLogged(t *testing.T) {
	h := newHarness(t, nil)
	h.mem.fail = errBoom
	for _, path := range []string{"/api/metrics", "/api/facts"} {
		w := h.serveUI(newRequest(http.MethodGet, path+"?team=checkout", nil))
		expectStatus(t, w, http.StatusInternalServerError)
		expectBody(t, w, `{"detail":"Internal Server Error"}`)
	}
	logged := 0
	for _, l := range h.lines(t) {
		if l["msg"] == "dataset_failed" && l["team"] == "checkout" && l["err"] == "boom" {
			logged++
		}
	}
	if logged != 2 {
		t.Errorf("dataset_failed lines = %d: %s", logged, h.logs)
	}
}

// prevFails fails the previous period's read only; every other read waits
// until that failure cancels it, or gives up so a regression fails, not hangs.
type prevFails struct {
	*memStore
	prev metrics.Period
}

var (
	errPrevious  = errors.New("previous period failed")
	errNotCancel = errors.New("read never cancelled")
)

func (s prevFails) Dataset(ctx context.Context, p dataset.Params) (*dataset.Dataset, error) {
	if p.From.Equal(s.prev.From) && p.To.Equal(s.prev.To) {
		return nil, errPrevious
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, errNotCancel
	}
}

// The read that failed is logged, not one its failure cancelled.
func TestReadFailureLogsTheCauseNotTheCancelled(t *testing.T) {
	h := newHarness(t, prevFails{newMemStore(), metrics.ClosedPrevious(defaultWeeks, fixedNow, time.UTC)})
	w := h.serveUI(newRequest(http.MethodGet, "/api/metrics?team=checkout", nil))
	expectStatus(t, w, http.StatusInternalServerError)
	var errs []any
	for _, l := range h.lines(t) {
		if l["msg"] == "dataset_failed" {
			errs = append(errs, l["err"])
		}
	}
	if len(errs) != 1 || errs[0] != errPrevious.Error() {
		t.Errorf("dataset_failed errors = %v, want [%q]", errs, errPrevious)
	}
}

// viewerGone reads the dataset and the analysis status, or asks. With leave
// it is a viewer leaving while the read waits: the request's context is
// cancelled and the read answers err, or the context's error as pgx would
// when err is nil. Without leave it answers err at once, the viewer still
// there. With inAsk the ask itself is the read that waits.
type viewerGone struct {
	*memStore
	leave context.CancelFunc
	err   error
	inAsk bool
}

func (s viewerGone) wait(ctx context.Context) error {
	if s.leave == nil {
		return s.err
	}
	s.leave()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		return errNotCancel
	}
	if s.err != nil {
		return s.err
	}
	return ctx.Err()
}

func (s viewerGone) Dataset(ctx context.Context, _ dataset.Params) (*dataset.Dataset, error) {
	return nil, s.wait(ctx)
}

func (s viewerGone) Status(ctx context.Context, _ analysis.View) (analysis.Status, error) {
	return analysis.Status{}, s.wait(ctx)
}

// Ask starts the run; unless inAsk, the status read after it is the one
// that waits.
func (s viewerGone) Ask(ctx context.Context, _ analysis.View) error {
	if s.inAsk {
		return s.wait(ctx)
	}
	return nil
}

// analysisFails is an analyst whose ask or status read fails.
type analysisFails struct {
	*memStore
	ask, status error
}

func (s analysisFails) Status(context.Context, analysis.View) (analysis.Status, error) {
	return analysis.Status{}, s.status
}

func (s analysisFails) Ask(context.Context, analysis.View) error { return s.ask }

func TestAnalysisFailureIs500AndLogged(t *testing.T) {
	for _, tc := range []struct {
		name        string
		ask, status error
		failed      string
	}{
		{"ask fails", errBoom, nil, "analysis_ask_failed"},
		{"status after ask fails", nil, errBoom, "analysis_status_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, analysisFails{memStore: newMemStore(), ask: tc.ask, status: tc.status})
			r := newRequest(http.MethodPost, "/api/analysis?team=checkout", nil)
			r.Header.Set(AskHeader, "1")
			w := h.serveUI(r)
			expectStatus(t, w, http.StatusInternalServerError)
			expectBody(t, w, `{"detail":"Internal Server Error"}`)
			validateResponse(t, specRouter(t), http.MethodPost, "/api/analysis", w)
			logged := 0
			for _, l := range h.lines(t) {
				if l["msg"] == tc.failed {
					logged++
					if l["log_level"] != "error" || l["team"] != "checkout" || l["err"] != "boom" {
						t.Errorf("line = %v", l)
					}
				}
			}
			if logged != 1 {
				t.Errorf("%s lines = %d: %s", tc.failed, logged, h.logs)
			}
		})
	}
}

// A tab closed or a filter switched mid-load (the UI aborts the request)
// is no failure: 499 in the access log, nothing logged as an error. A real
// error after the viewer left, or a cancellation the viewer did not cause,
// still is one.
func TestReadViewerGoneIsNoFailure(t *testing.T) {
	queryCanceled := &pgconn.PgError{Code: "57014", Message: "canceling statement due to user request"}
	for _, tc := range []struct {
		name, method, path string
		left, inAsk        bool
		err                error
		status             int
		failed             string
	}{
		{"metrics left", http.MethodGet, "/api/metrics", true, false, nil, statusViewerLeft, ""},
		{"facts left", http.MethodGet, "/api/facts", true, false, nil, statusViewerLeft, ""},
		{"analysis left", http.MethodGet, "/api/analysis", true, false, nil, statusViewerLeft, ""},
		{"ask left", http.MethodPost, "/api/analysis", true, false, nil, statusViewerLeft, ""},
		{"ask left during Ask", http.MethodPost, "/api/analysis", true, true, nil, statusViewerLeft, ""},
		{"facts left, query canceled", http.MethodGet, "/api/facts", true, false, queryCanceled, statusViewerLeft, ""},
		{"metrics left, real error", http.MethodGet, "/api/metrics", true, false, errBoom, http.StatusInternalServerError, "dataset_failed"},
		{"ask left, real error", http.MethodPost, "/api/analysis", true, true, errBoom, http.StatusInternalServerError, "analysis_ask_failed"},
		{"facts there, canceled", http.MethodGet, "/api/facts", false, false, context.Canceled, http.StatusInternalServerError, "dataset_failed"},
		{"facts there, query canceled", http.MethodGet, "/api/facts", false, false, queryCanceled, http.StatusInternalServerError, "dataset_failed"},
		{"analysis there, canceled", http.MethodGet, "/api/analysis", false, false, context.Canceled, http.StatusInternalServerError, "analysis_status_failed"},
		{"ask there, canceled", http.MethodPost, "/api/analysis", false, true, context.Canceled, http.StatusInternalServerError, "analysis_ask_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			st := viewerGone{memStore: newMemStore(), err: tc.err, inAsk: tc.inAsk}
			if tc.left {
				st.leave = cancel
			}
			h := newHarness(t, st)
			r := newRequest(tc.method, tc.path+"?team=checkout", nil).WithContext(ctx)
			r.Header.Set(AskHeader, "1")
			w := h.serveUI(r)
			expectStatus(t, w, tc.status)
			validateResponse(t, specRouter(t), tc.method, tc.path, w)
			failed, access := 0, 0.0
			for _, l := range h.lines(t) {
				switch {
				case tc.failed != "" && l["msg"] == tc.failed:
					failed++
				case l["msg"] == "http_request":
					access, _ = l["status"].(float64)
				case l["log_level"] == "error":
					t.Errorf("logged %v", l)
				}
			}
			if tc.failed != "" && failed != 1 {
				t.Errorf("%s lines = %d: %s", tc.failed, failed, h.logs)
			}
			if int(access) != tc.status {
				t.Errorf("http_request status = %v, want %d", access, tc.status)
			}
			if tc.status == statusViewerLeft && w.Body.Len() != 0 {
				t.Errorf("body = %q, want none", w.Body)
			}
		})
	}
}

// Over the real store: the handlers compute from what the dataset package
// reads, and the responses match the contract.
func TestE2ERead(t *testing.T) {
	h, pool := dbHarness(t)
	ctx := context.Background()
	// In the last closed week: the page counts no week still running.
	at := fixedNow.Add(-7 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO bitbucket_events (delivery_id, event_type, repo_full_name, pr_id, author,
		branch_name, change_type, jira_keys, occurred_at, team, payload) VALUES
		('o', 'pr:opened', 'kasse/kasse-api', 7, 'dev-anna', 'feature/x', 'feature', '{KAS-7}', $1, 'checkout',
		 '{"pullRequest": {"fromRef": {"displayId": "feature/x"}}}')`, at); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pipeline_events (delivery_id, source, pipeline_name, run_id, phase, status,
		commit_sha, build_type, started_at, finished_at, occurred_at, team, payload) VALUES
		('r', 'jenkins', 'kasse-api/master', '1', 'COMPLETED', 'SUCCESS', 'abc1234', 'release', $1, $1, $1, 'checkout', '{}')`, at); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO argocd_events (delivery_id, app_name, revision, operation_phase, environment,
		occurred_at, team, payload) VALUES ('d', 'kasse-prod', 'abc1234', 'Succeeded', 'prod', $1, 'checkout', '{"images": ["reg/a:1"]}')`, at); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO noergler_events (delivery_id, event_type, pr_key, repo, outcome, cost_usd,
		occurred_at, team, payload) VALUES ('n', 'pr_completed', 'kasse/kasse-api#7', 'kasse/kasse-api', 'merged', 1.5, $1, 'checkout', '{}')`, at); err != nil {
		t.Fatal(err)
	}
	router := specRouter(t)
	w := h.serveUI(newRequest(http.MethodGet, "/api/metrics?team=checkout", nil))
	expectStatus(t, w, http.StatusOK)
	validateResponse(t, router, http.MethodGet, "/api/metrics", w)
	var m struct {
		Delivery struct {
			Frequency struct {
				Syncs     int `json:"syncs"`
				FirstSeen int `json:"first_seen"`
			} `json:"frequency"`
		} `json:"delivery"`
		Build struct {
			Kinds []struct {
				Kind string `json:"kind"`
				Runs int    `json:"runs"`
			} `json:"kinds"`
		} `json:"build"`
		Costs struct {
			Total float64 `json:"total"`
		} `json:"costs"`
		OpenPRs struct {
			At   time.Time `json:"at"`
			Open int       `json:"open"`
		} `json:"open_prs"`
		RunningWeek struct {
			Runs int `json:"runs"`
		} `json:"running_week"`
		Trends struct {
			Weeks []struct {
				Running bool `json:"running"`
			} `json:"weeks"`
			Split    int `json:"split"`
			Releases struct {
				Values []*float64 `json:"values"`
			} `json:"releases"`
		} `json:"trends"`
		Repos struct {
			Rows    []json.RawMessage `json:"rows"`
			Spreads []struct {
				Key  string  `json:"key"`
				Kind *string `json:"kind"`
			} `json:"spreads"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	// Eight closed weeks and the running one; the deploy a first deploy, no
	// release; no merged PR, so no repo row, but a strip per number and the
	// release builds' two.
	tr := m.Trends
	if len(tr.Weeks) != 9 || tr.Split != 4 || !tr.Weeks[8].Running || len(tr.Releases.Values) != 9 ||
		len(m.Repos.Rows) != 0 || len(m.Repos.Spreads) != 7 || m.Repos.Spreads[5].Kind == nil ||
		*m.Repos.Spreads[5].Kind != "release" {
		t.Errorf("trends = %+v, repos = %+v", m.Trends, m.Repos)
	}
	if m.Delivery.Frequency.Syncs != 1 || m.Delivery.Frequency.FirstSeen != 1 || len(m.Build.Kinds) != 1 ||
		m.Build.Kinds[0].Kind != "release" || m.Build.Kinds[0].Runs != 1 || m.Costs.Total != 1.5 {
		t.Errorf("metrics = %s", w.Body)
	}
	// The PR is open now, read by the running week's own read; the week has
	// no run of its own.
	if !m.OpenPRs.At.Equal(fixedNow.Truncate(time.Second)) || m.OpenPRs.Open != 1 || m.RunningWeek.Runs != 0 {
		t.Errorf("open PRs = %+v, running week = %+v", m.OpenPRs, m.RunningWeek)
	}

	w = h.serveUI(newRequest(http.MethodGet, "/api/facts?team=checkout&q=KAS-7", nil))
	expectStatus(t, w, http.StatusOK)
	validateResponse(t, router, http.MethodGet, "/api/facts", w)
	var f struct {
		PRs struct {
			Count int `json:"count"`
		} `json:"prs"`
		Deploys struct {
			Count int `json:"count"`
		} `json:"deploys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil || f.PRs.Count != 1 || f.Deploys.Count != 0 {
		t.Errorf("facts = %s", w.Body)
	}

	// The analysis: the assessment alone, then a run asked for, then the
	// run worded and the view on cooldown.
	w = h.serveUI(newRequest(http.MethodGet, "/api/analysis?team=checkout", nil))
	expectStatus(t, w, http.StatusOK)
	validateResponse(t, router, http.MethodGet, "/api/analysis", w)
	var st struct {
		State      string          `json:"state"`
		Running    bool            `json:"running"`
		Analysis   json.RawMessage `json:"analysis"`
		Assessment struct {
			Limits []json.RawMessage `json:"limits"`
		} `json:"assessment"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || st.State != "none" {
		t.Fatalf("analysis before a run = %s", w.Body)
	}
	// Every source started inside the window: limits the spec validated.
	if len(st.Assessment.Limits) == 0 {
		t.Errorf("assessment without limits = %s", w.Body)
	}
	ask := func(query string, header bool) *httptest.ResponseRecorder {
		r := newRequest(http.MethodPost, "/api/analysis?"+query, nil)
		if header {
			r.Header.Set(AskHeader, "1")
		}
		w := h.serveUI(r)
		validateResponse(t, router, http.MethodPost, "/api/analysis", w)
		return w
	}
	// Without the page's header, or for a product the data does not know,
	// nothing is spent.
	expectStatus(t, ask("team=checkout", false), http.StatusForbidden)
	expectStatus(t, ask("team=checkout&product=made-up", true), http.StatusUnprocessableEntity)
	expectStatus(t, ask("team=checkout", true), http.StatusAccepted)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		w = h.serveUI(newRequest(http.MethodGet, "/api/analysis?team=checkout", nil))
		if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
			t.Fatal(err)
		}
		if !st.Running || time.Now().After(deadline) {
			break
		}
	}
	validateResponse(t, router, http.MethodGet, "/api/analysis", w)
	if st.State != "fresh" || string(st.Analysis) == "null" {
		t.Errorf("analysis after a run = %s", w.Body)
	}
	expectStatus(t, ask("team=checkout", true), http.StatusConflict)

	h.store.(*store.Store).Close()
	for _, path := range []string{"/api/metrics", "/api/facts"} {
		w = h.serveUI(newRequest(http.MethodGet, path+"?team=checkout", nil))
		expectStatus(t, w, http.StatusInternalServerError)
		validateResponse(t, router, http.MethodGet, path, w)
	}
}
