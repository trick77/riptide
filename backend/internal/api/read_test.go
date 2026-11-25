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
			Silences:   []dataset.Silence{{Source: dataset.SourceBitbucket, From: before, To: at, UnseenRuns: 2}},
			RepoStarts: []dataset.RepoStart{{Repo: "kasse/kasse-api", Noergler: &before}}},
		Products: []dataset.Product{{Key: "kasse", Repos: []string{"kasse/kasse-api"}, Apps: []string{"kasse-prod"}}},
		PRs: []dataset.PR{{Repo: "kasse/kasse-api", Project: "kasse", PRID: 7, Title: s("Rabattregeln"), ChangeType: s("feature"),
			JiraKeys: []string{"KAS-7"}, State: "merged", OpenedAt: &before, ReadyAt: &before, FirstReviewAt: &before, ApprovedAt: &before,
			ApprovedOnFirstTouch: true, MergedAt: &at, FirstCommitAt: &before, MergeCommit: s("abc1234"),
			Environments: []dataset.EnvironmentDeploy{{Environment: "intg", FirstDeployedAt: at}, {Environment: "prod", FirstDeployedAt: at}}},
			// Merged into a feature branch: listed, in no PR number.
			{Repo: "kasse/kasse-api", Project: "kasse", PRID: 8, Title: s("Teilschritt"), State: "merged", OpenedAt: &before,
				MergedAt: &at, TargetBranch: s("feature/umbau"), OffTrunk: true}},
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
			{App: "monitoring", Environment: s("prod"), Revision: "fed4321", At: at, Phase: "Succeeded",
				Images: []string{"reg/m:1"}, Kind: s("release"), NewImages: []string{"reg/m:1"}, Infra: true}},
		LeadTimes: []dataset.LeadTime{{Repo: "kasse/kasse-api", Project: "kasse", Environment: "prod", LeadTimeS: 3600}},
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
		"/api/metrics": {`"unknown":1`, `"unseen_runs":2`},
		"/api/facts":   {`"not_counted":"off_trunk"`, `"state":"unknown"`, `"phase":"Error"`, `"infra":true`, `"first_deploy":true`},
	} {
		body := h.serveUI(newRequest(http.MethodGet, path+"?team=checkout", nil)).Body.String()
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s has no %s", path, w)
			}
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
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
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
