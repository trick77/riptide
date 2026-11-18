package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/trick77/ship/kit/logging"

	"github.com/trick77/riptide/internal/httpapi"
)

// fakeProjectHooks is a Bitbucket with one empty project: it lists no
// webhooks and no repos, and takes a POST of a webhook.
type fakeProjectHooks struct {
	mu      sync.Mutex
	created []map[string]any
	tokens  []string
}

func (f *fakeProjectHooks) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = append(f.tokens, r.Header.Get("Authorization"))
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/rest/api/1.0/projects/PLAT/webhooks" ||
		r.URL.Path == "/rest/api/1.0/projects/PLAT/repos"):
		_, _ = w.Write([]byte(`{"values":[],"isLastPage":true}`))
	case r.Method == http.MethodPost && r.URL.Path == "/rest/api/1.0/projects/PLAT/webhooks":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.created = append(f.created, body)
		_, _ = w.Write([]byte(`{"id":7}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type onboardHarness struct {
	srv  *httpapi.Server
	bb   *fakeProjectHooks
	logs *bytes.Buffer
}

func newOnboardHarness(t *testing.T, on bool) *onboardHarness {
	t.Helper()
	h := newHarness(t, nil)
	bb := &fakeProjectHooks{}
	bbSrv := httptest.NewServer(bb)
	t.Cleanup(bbSrv.Close)
	logs := &bytes.Buffer{}
	log := slog.New(logging.NewSplunkHandler(&lockedWriter{w: logs}, slog.LevelDebug, "riptide", "test"))
	deps := Deps{Store: h.mem, Runtime: h.runtime, Log: log}
	if on {
		deps.Onboarding = Onboarding{PublicURL: "https://riptide.example.com", BitbucketURL: bbSrv.URL, HTTP: bbSrv.Client()}
	}
	srv := httpapi.New(log)
	Register(srv, deps)
	return &onboardHarness{srv: srv, bb: bb, logs: logs}
}

func (h *onboardHarness) post(team, key, bbToken, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/onboard/"+team, strings.NewReader(body))
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	if bbToken != "" {
		r.Header.Set("X-Bitbucket-Token", bbToken)
	}
	w := httptest.NewRecorder()
	h.srv.Handler().ServeHTTP(w, r)
	return w
}

func TestOnboardCreatesTheProjectWebhook(t *testing.T) {
	h := newOnboardHarness(t, true)
	w := h.post("platform", platformBitbucket, "caller-bb-token", `{"action":"onboard","projects":[{"key":"PLAT"}]}`)
	expectStatus(t, w, http.StatusOK)
	var got onboardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := "https://riptide.example.com/webhooks/bitbucket/platform"
	if !got.Healthy || got.WebhookURL != want || len(got.Rows) != 1 || got.Rows[0].Status != "ok" ||
		got.Rows[0].Level != "project" || !strings.Contains(got.Text, "PLAT (project)") {
		t.Fatalf("response = %+v", got)
	}
	if len(h.bb.created) != 1 {
		t.Fatalf("created = %v", h.bb.created)
	}
	hook := h.bb.created[0]
	cfg, _ := hook["configuration"].(map[string]any)
	// The team's own key signs, whoever called.
	if hook["url"] != want || cfg["secret"] != platformBitbucket {
		t.Fatalf("hook = %v", hook)
	}
	for _, tok := range h.bb.tokens {
		if tok != "Bearer caller-bb-token" {
			t.Fatalf("Bitbucket saw %q", tok)
		}
	}
	// The caller's token is never logged; the request is, once.
	if strings.Contains(h.logs.String(), "caller-bb-token") || strings.Count(h.logs.String(), `"onboard_processed"`) != 1 {
		t.Fatalf("logs:\n%s", h.logs)
	}
}

func TestOnboardRefuses(t *testing.T) {
	h := newOnboardHarness(t, true)
	const ok = `{"projects":[{"key":"PLAT"}]}`
	for _, c := range []struct {
		name, team, key, token, body string
		want                         int
	}{
		{"no key", "platform", "", "t", ok, http.StatusUnauthorized},
		{"another source's key", "platform", platformArgoCD, "t", ok, http.StatusUnauthorized},
		{"another team's key", "platform", checkoutBitbucket, "t", ok, http.StatusUnauthorized},
		{"no Bitbucket token", "platform", platformBitbucket, "", ok, http.StatusBadRequest},
		{"unknown field", "platform", platformBitbucket, "t", `{"projects":[{"key":"PLAT"}],"dryrun":true}`, http.StatusUnprocessableEntity},
		{"no projects", "platform", platformBitbucket, "t", `{"action":"onboard"}`, http.StatusUnprocessableEntity},
		{"bad action", "platform", platformBitbucket, "t", `{"action":"grant","projects":[{"key":"PLAT"}]}`, http.StatusUnprocessableEntity},
		{"empty repos", "platform", platformBitbucket, "t", `{"projects":[{"key":"PLAT","repos":[]}]}`, http.StatusUnprocessableEntity},
		{"twice", "platform", platformBitbucket, "t", `{"projects":[{"key":"PLAT"},{"key":" PLAT "}]}`, http.StatusUnprocessableEntity},
		{"two objects", "platform", platformBitbucket, "t", ok + ok, http.StatusUnprocessableEntity},
	} {
		t.Run(c.name, func(t *testing.T) {
			expectStatus(t, h.post(c.team, c.key, c.token, c.body), c.want)
		})
	}
	if len(h.bb.tokens) != 0 {
		t.Fatalf("Bitbucket was called: %v", h.bb.tokens)
	}
}

func TestOnboardOffWithoutURLs(t *testing.T) {
	h := newOnboardHarness(t, false)
	expectStatus(t, h.post("platform", platformBitbucket, "t", `{"projects":[{"key":"PLAT"}]}`), http.StatusServiceUnavailable)
}

func TestOnboardResponsesMatchTheSpec(t *testing.T) {
	router := specRouter(t)
	h := newOnboardHarness(t, true)
	check := func(w *httptest.ResponseRecorder) {
		t.Helper()
		validateResponse(t, router, http.MethodPost, "/onboard/platform", w)
	}
	check(h.post("platform", platformBitbucket, "t", `{"projects":[{"key":"PLAT"},{"key":"~ME","repos":["tool"]}]}`))
	check(h.post("platform", platformBitbucket, "t", `{"action":"onboard","dry_run":true,"projects":[{"key":"PLAT"}]}`))
	check(h.post("platform", "nope", "t", `{}`))
	check(h.post("platform", platformBitbucket, "", `{}`))
	check(h.post("platform", platformBitbucket, "t", `{"projects":[]}`))
	check(newOnboardHarness(t, false).post("platform", platformBitbucket, "t", `{"projects":[{"key":"PLAT"}]}`))
}
