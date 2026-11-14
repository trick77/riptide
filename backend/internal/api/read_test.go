package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/dataset"
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

// The period is whole calendar weeks in the viewer's zone, to now: the
// dataset is read for exactly that window.
func TestReadParams(t *testing.T) {
	h := newHarness(t, nil)
	now := fixedNow.Truncate(time.Second) // Wednesday 2030-01-02 03:04:05 UTC
	utc := func(y int, m time.Month, d, hh int) time.Time { return time.Date(y, m, d, hh, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		query string
		from  time.Time
		weeks int
	}{
		{"team=checkout", utc(2029, 12, 10, 0), 4},
		{"team=%20checkout%20&bots=1", utc(2029, 12, 10, 0), 4},
		{"team=checkout&weeks=12&tz=UTC", utc(2029, 10, 15, 0), 12},
		// Monday 00:00 in Zurich, CET then; twelve weeks back it was CEST.
		{"team=checkout&tz=Europe/Zurich", utc(2029, 12, 9, 23), 4},
		{"team=checkout&weeks=12&tz=Europe/Zurich", utc(2029, 10, 14, 22), 12},
		{"team=checkout&weeks=26&product=nope", utc(2029, 7, 9, 0), 26},
	} {
		for _, path := range []string{"/api/metrics?", "/api/facts?"} {
			h.mem.params = nil
			w := h.serveUI(newRequest(http.MethodGet, path+c.query, nil))
			expectStatus(t, w, http.StatusOK)
			p := h.mem.params[0]
			// The facts read no lead times or reviews.
			if p.Team != "checkout" || !p.From.Equal(c.from) || !p.To.Equal(now) || p.ProductionStage != "prod" ||
				!p.Now.Equal(now) || p.FactsOnly != (path == "/api/facts?") {
				t.Errorf("%s%s: params = %+v", path, c.query, p)
			}
			// The ingest-time detector: configured authors (login or display
			// name), the bot-shaped name heuristic; a person is not a bot.
			if p.Automated == nil || !p.Automated("renovate-bot", "") || !p.Automated("svc-x", "Dependabot[bot]") ||
				!p.Automated("deploy-bot", "") || p.Automated("dev-anna", "Anna Example") {
				t.Errorf("%s%s: automation detection wrong", path, c.query)
			}
		}
		w := h.serveUI(newRequest(http.MethodGet, "/api/metrics?"+c.query, nil))
		var body struct {
			Period struct {
				Weeks []struct {
					KW int `json:"kw"`
				} `json:"weeks"`
			} `json:"period"`
			Product *string `json:"product"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Period.Weeks) != c.weeks || body.Product != nil {
			t.Errorf("%s: body = %s", c.query, w.Body)
		}
		if last := body.Period.Weeks[len(body.Period.Weeks)-1].KW; last != 1 {
			t.Errorf("%s: current week KW %d, want 1", c.query, last)
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
		{"team=checkout&bots=yes", http.StatusUnprocessableEntity, "bots must be 0 or 1"},
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
			DataEnd:   dataset.SourceTimes{Bitbucket: &at, Pipeline: &at, ArgoCD: &at, Noergler: &at}, BuildDataFrom: &before, BuildTypeFrom: &before},
		Products: []dataset.Product{{Key: "kasse", Repos: []string{"kasse/kasse-api"}, Apps: []string{"kasse-prod"}}},
		PRs: []dataset.PR{{Repo: "kasse/kasse-api", Project: "kasse", PRID: 7, Title: s("Rabattregeln"), ChangeType: s("feature"),
			JiraKeys: []string{"KAS-7"}, State: "merged", OpenedAt: &before, ReadyAt: &before, FirstReviewAt: &before, ApprovedAt: &before,
			ApprovedOnFirstTouch: true, MergedAt: &at, FirstCommitAt: &before, MergeCommit: s("abc1234"),
			Environments: []dataset.EnvironmentDeploy{{Environment: "intg", FirstDeployedAt: at}, {Environment: "prod", FirstDeployedAt: at}}}},
		Deploys: []dataset.Deploy{{App: "kasse-prod", Project: s("kasse"), Environment: s("prod"), Revision: "abc1234",
			At: at, Phase: "Succeeded", DurationS: n(45), Images: []string{"reg/a:1"}, Kind: s("release"),
			NewImages: []string{"reg/a:1"}}},
		LeadTimes: []dataset.LeadTime{{Repo: "kasse/kasse-api", Project: "kasse", Environment: "prod", CommitSHA: "abc1234",
			AuthoredAt: before, FirstDeployedAt: at, LeadTimeS: 3600}},
		Reviews: []dataset.Review{{PRKey: "kasse/kasse-api#7", Repo: s("kasse/kasse-api"), Project: s("kasse"),
			Outcome: s("merged"), CostUSD: s("0.123456"), LinesAdded: n(1), LinesRemoved: n(1), FilesChanged: n(1),
			FindingsCount: n(1), TotalRuns: n(1), ClosedAt: at}},
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
		for _, q := range []string{"team=checkout&tz=Europe/Zurich", "team=checkout&product=kasse&bots=1", "", "team=nobody", "team=checkout&weeks=3"} {
			validateResponse(t, router, http.MethodGet, path, h.serveUI(newRequest(http.MethodGet, path+"?"+q, nil)))
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

// Over the real store: the handlers compute from what the dataset package
// reads, and the responses match the contract.
func TestE2ERead(t *testing.T) {
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
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if m.Delivery.Frequency.Syncs != 1 || m.Delivery.Frequency.FirstSeen != 1 || len(m.Build.Kinds) != 1 ||
		m.Build.Kinds[0].Kind != "release" || m.Build.Kinds[0].Runs != 1 || m.Costs.Total != 1.5 {
		t.Errorf("metrics = %s", w.Body)
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

	h.store.(*store.Store).Close()
	for _, path := range []string{"/api/metrics", "/api/facts"} {
		w = h.serveUI(newRequest(http.MethodGet, path+"?team=checkout", nil))
		expectStatus(t, w, http.StatusInternalServerError)
		validateResponse(t, router, http.MethodGet, path, w)
	}
}
