package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/store"
)

// The read API lives on the UI listener only, and the UI listener serves
// nothing of the collector's.
func TestReadAPIIsOnTheUIListenerOnly(t *testing.T) {
	h := newHarness(t, nil)
	w := h.serve(newRequest(http.MethodGet, "/api/dataset?team=checkout", nil))
	expectStatus(t, w, http.StatusNotFound)
	expectBody(t, w, `{"detail":"Not Found"}`)
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
	if got := h.serveUI(newRequest(http.MethodPost, "/api/dataset", nil)); got.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/dataset = %d", got.Code)
	}
}

func TestDatasetParams(t *testing.T) {
	h := newHarness(t, nil)
	day := 24 * time.Hour
	now := fixedNow.Truncate(time.Second)
	for _, c := range []struct {
		query    string
		from, to time.Time
	}{
		{"team=checkout", now.Add(-28 * day), now},
		{"team=%20checkout%20", now.Add(-28 * day), now},
		{"team=checkout&from=2029-12-01", time.Date(2029, 12, 1, 0, 0, 0, 0, time.UTC), now},
		// A date as `to` includes the day.
		{"team=checkout&from=2029-11-01&to=2029-11-30", time.Date(2029, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2029, 12, 1, 0, 0, 0, 0, time.UTC)},
		{"team=checkout&to=2029-06-01T12:00:00%2B02:00", time.Date(2029, 5, 4, 10, 0, 0, 0, time.UTC), time.Date(2029, 6, 1, 10, 0, 0, 0, time.UTC)},
	} {
		h.mem.params = nil
		w := h.serveUI(newRequest(http.MethodGet, "/api/dataset?"+c.query, nil))
		expectStatus(t, w, http.StatusOK)
		p := h.mem.params[0]
		if p.Team != "checkout" || !p.From.Equal(c.from) || !p.To.Equal(c.to) || p.ProductionStage != "prod" || !p.Now.Equal(now) {
			t.Errorf("%s: params = %+v", c.query, p)
		}
		// The ingest-time detector: configured authors (login or display
		// name), the bot-shaped name heuristic; a person is not a bot.
		if p.Automated == nil || !p.Automated("renovate-bot", "") || !p.Automated("svc-x", "Dependabot[bot]") ||
			!p.Automated("deploy-bot", "") || p.Automated("dev-anna", "Anna Example") {
			t.Errorf("%s: automation detection wrong", c.query)
		}
		var body struct {
			Meta struct {
				Team string `json:"team"`
			} `json:"meta"`
			PRs []any `json:"prs"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Meta.Team != "checkout" || body.PRs == nil {
			t.Errorf("%s: body = %s", c.query, w.Body)
		}
	}

	for _, c := range []struct {
		query  string
		status int
		detail string
	}{
		{"", http.StatusUnprocessableEntity, "query parameter team is required"},
		{"team=nobody", http.StatusNotFound, "unknown team"},
		{"team=checkout&from=yesterday", http.StatusUnprocessableEntity, "from must be an RFC 3339 timestamp or a YYYY-MM-DD date"},
		{"team=checkout&to=2029-13-01", http.StatusUnprocessableEntity, "to must be an RFC 3339 timestamp or a YYYY-MM-DD date"},
		{"team=checkout&from=2029-12-01&to=2029-11-30", http.StatusUnprocessableEntity, "from must be before to"},
		{"team=checkout&from=2028-01-01&to=2029-12-31", http.StatusUnprocessableEntity, "the window spans more than 400 days"},
	} {
		w := h.serveUI(newRequest(http.MethodGet, "/api/dataset?"+c.query, nil))
		expectStatus(t, w, c.status)
		expectBody(t, w, `{"detail":"`+c.detail+`"}`)
	}
}

// Every field of every section set, so each item schema is exercised; the
// e2e test below covers the nulls.
func TestDatasetMatchesOpenAPISpec(t *testing.T) {
	router := specRouter(t)
	h := newHarness(t, nil)
	at := fixedNow
	s, n := func(v string) *string { return &v }, func(v int64) *int64 { return &v }
	h.mem.dataset = &dataset.Dataset{
		Meta: dataset.Meta{Team: "checkout", From: at, To: at, GeneratedAt: at, ProductionStage: "prod",
			DataStart: dataset.DataStart{Bitbucket: &at, Pipeline: &at, ArgoCD: &at, Noergler: &at}, BuildDataFrom: &at, BuildTypeFrom: &at},
		Products: []dataset.Product{{Key: "kasse", Repos: []string{"kasse/kasse-api"}, Apps: []string{"kasse-prod"}}},
		PRs: []dataset.PR{{Repo: "kasse/kasse-api", Project: "kasse", PRID: 7, ChangeType: s("feature"),
			JiraKeys: []string{"KAS-7"}, State: "merged", OpenedAt: &at, ReadyAt: &at, FirstReviewAt: &at, ApprovedAt: &at,
			ApprovedOnFirstTouch: true, MergedAt: &at, FirstCommitAt: &at, MergeCommit: s("abc1234"),
			Environments: []dataset.EnvironmentDeploy{{Environment: "intg", FirstDeployedAt: at}, {Environment: "prod", FirstDeployedAt: at}}}},
		Deploys: []dataset.Deploy{{App: "kasse-prod", Project: s("kasse"), Environment: s("prod"), Revision: "abc1234",
			At: at, Phase: "Succeeded", DurationS: n(45), Images: []string{"reg/a:1"}, Kind: s("release")}},
		LeadTimes: []dataset.LeadTime{{Repo: "kasse/kasse-api", Project: "kasse", Environment: "prod", CommitSHA: "abc1234",
			AuthoredAt: at, FirstDeployedAt: at, LeadTimeS: 3600, IsMerge: true, ServiceAccount: true, Bot: true}},
		Runs: []dataset.Run{{Source: "jenkins", PipelineName: "kasse-api/master", Branch: "master", BranchKind: "main",
			BuildType: s("release"), RunID: "1", Status: s("SUCCESS"), StartedAt: &at, FinishedAt: &at, DurationS: n(600),
			Repo: s("kasse/kasse-api"), Project: s("kasse"), ImageRefs: []string{"reg/a:1"}}},
		Reviews: []dataset.Review{{PRKey: "kasse/kasse-api#7", Repo: s("kasse/kasse-api"), Project: s("kasse"),
			Outcome: s("merged"), CostUSD: s("0.123456"), LinesAdded: n(1), LinesRemoved: n(1), FilesChanged: n(1),
			FindingsCount: n(1), TotalRuns: n(1), ClosedAt: at}},
	}
	for _, q := range []string{"team=checkout", "", "team=nobody", "team=checkout&from=x"} {
		validateResponse(t, router, http.MethodGet, "/api/dataset", h.serveUI(newRequest(http.MethodGet, "/api/dataset?"+q, nil)))
	}
}

func TestDatasetFailureIs500AndLogged(t *testing.T) {
	h := newHarness(t, nil)
	h.mem.fail = errBoom
	w := h.serveUI(newRequest(http.MethodGet, "/api/dataset?team=checkout", nil))
	expectStatus(t, w, http.StatusInternalServerError)
	expectBody(t, w, `{"detail":"Internal Server Error"}`)
	var logged bool
	for _, l := range h.lines(t) {
		if l["msg"] == "dataset_failed" && l["team"] == "checkout" && l["err"] == "boom" {
			logged = true
		}
	}
	if !logged {
		t.Errorf("no dataset_failed line: %s", h.logs)
	}
}

// Over the real store: the handler answers what the dataset package reads,
// and the response matches the contract.
func TestE2EDataset(t *testing.T) {
	h, pool := dbHarness(t)
	ctx := context.Background()
	at := fixedNow.Add(-24 * time.Hour)
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
	w := h.serveUI(newRequest(http.MethodGet, "/api/dataset?team=checkout", nil))
	expectStatus(t, w, http.StatusOK)
	validateResponse(t, specRouter(t), http.MethodGet, "/api/dataset", w)
	var body struct {
		PRs []struct {
			Repo string `json:"repo"`
		} `json:"prs"`
		Runs []struct {
			BuildType  string `json:"build_type"`
			BranchKind string `json:"branch_kind"`
		} `json:"runs"`
		Deploys []struct {
			Kind string `json:"kind"`
		} `json:"deploys"`
		Reviews []struct {
			CostUSD string `json:"cost_usd"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.PRs) != 1 || body.PRs[0].Repo != "kasse/kasse-api" || len(body.Runs) != 1 || body.Runs[0].BuildType != "release" ||
		body.Runs[0].BranchKind != "main" || len(body.Deploys) != 1 || body.Deploys[0].Kind != "first_seen" ||
		len(body.Reviews) != 1 || body.Reviews[0].CostUSD != "1.500000" {
		t.Errorf("body = %s", w.Body)
	}

	h.store.(*store.Store).Close()
	w = h.serveUI(newRequest(http.MethodGet, "/api/dataset?team=checkout", nil))
	expectStatus(t, w, http.StatusInternalServerError)
	validateResponse(t, specRouter(t), http.MethodGet, "/api/dataset", w)
}
