package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/httpapi"
	"github.com/trick77/riptide/internal/logging"
	"github.com/trick77/riptide/internal/parse"
)

// Team keys used across the tests, one per team and source.
const (
	checkoutBitbucket = "test-checkout-bitbucket-hmac-secret"
	platformBitbucket = "test-platform-bitbucket-hmac-secret"
	checkoutArgoCD    = "test-checkout-argocd-bearer"
	platformArgoCD    = "test-platform-argocd-bearer"
	checkoutJenkins   = "test-checkout-jenkins-bearer"
	platformJenkins   = "test-platform-jenkins-bearer"
	checkoutNoergler  = "test-checkout-noergler-bearer"
	platformNoergler  = "test-platform-noergler-bearer"
)

const teamKeysJSON = `{
  "checkout": {"bitbucket": "` + checkoutBitbucket + `", "argocd": "` + checkoutArgoCD + `", "jenkins": "` + checkoutJenkins + `", "noergler": "` + checkoutNoergler + `"},
  "platform": {"bitbucket": "` + platformBitbucket + `", "argocd": "` + platformArgoCD + `", "jenkins": "` + platformJenkins + `", "noergler": "` + platformNoergler + `"}
}`

const configJSON = `{
  "teams": [
    {"name": "checkout", "group_email": "team-checkout@example.com"},
    {"name": "platform", "group_email": "team-platform@example.com"}
  ],
  "automation": {
    "renovate": {"authors": ["renovate-bot", "renovate[bot]"], "branch_prefixes": ["renovate/"]},
    "dependabot": {"authors": ["dependabot[bot]"], "branch_prefixes": ["dependabot/"]}
  },
  "environments": {"production_stage": "prod", "ignored_stages": ["dev", "entw", "syst", "stage"]}
}`

var fixedNow = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

// memStore is an in-memory Store: it records every row and dedupes on the
// delivery id, like the ON CONFLICT DO NOTHING in the real one.
type memStore struct {
	mu      sync.Mutex
	seen    map[string]bool
	rows    []any
	teams   []string
	sources []string
	fail    error
	pingErr error
}

func newMemStore() *memStore { return &memStore{seen: map[string]bool{}} }

func (m *memStore) Ping(context.Context) error { return m.pingErr }

func (m *memStore) add(id, team string, row any) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return false, m.fail
	}
	if m.seen[id] {
		return false, nil
	}
	m.seen[id] = true
	m.rows = append(m.rows, row)
	m.teams = append(m.teams, team)
	return true, nil
}

func (m *memStore) InsertBitbucket(_ context.Context, d *parse.BitbucketDraft, automation, team string) (bool, error) {
	m.mu.Lock()
	m.sources = append(m.sources, automation)
	m.mu.Unlock()
	return m.add(d.DeliveryID, team, d)
}

func (m *memStore) InsertPipeline(_ context.Context, d *parse.PipelineDraft, team string) (bool, error) {
	return m.add(d.DeliveryID, team, d)
}

func (m *memStore) InsertArgoCD(_ context.Context, d *parse.ArgoCDDraft, team string) (bool, error) {
	return m.add(d.DeliveryID, team, d)
}

func (m *memStore) InsertNoergler(_ context.Context, d *parse.NoerglerDraft, team string) (bool, error) {
	return m.add(d.DeliveryID, team, d)
}

type harness struct {
	srv     *httpapi.Server
	store   Store
	mem     *memStore
	logs    *bytes.Buffer
	runtime *config.Runtime
	cfgPath string
}

// newHarness wires the real mux over st (an in-memory store when nil),
// with logs captured in the production JSON format.
func newHarness(t *testing.T, st Store) *harness {
	t.Helper()
	dir := t.TempDir()
	cfgPath, keysPath := filepath.Join(dir, "riptide.json"), filepath.Join(dir, "team-keys.json")
	if err := os.WriteFile(cfgPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keysPath, []byte(teamKeysJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	log := slog.New(logging.NewHandler(&lockedWriter{w: logs}, slog.LevelDebug, "test"))
	rt, err := config.Load(cfgPath, keysPath, log)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{logs: logs, runtime: rt, cfgPath: cfgPath}
	if st == nil {
		h.mem = newMemStore()
		st = h.mem
	}
	h.store = st
	h.srv = httpapi.New(log)
	Register(h.srv, Deps{Store: st, Runtime: rt, Log: log, Now: func() time.Time { return fixedNow }})
	return h
}

type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func newRequest(method, path string, body []byte) *http.Request {
	return httptest.NewRequest(method, path, bytes.NewReader(body))
}

func (h *harness) serve(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.srv.Handler().ServeHTTP(w, r)
	return w
}

// bearer posts body with a bearer token.
func (h *harness) bearer(t *testing.T, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := newRequest(http.MethodPost, path, jsonBytes(t, body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	return h.serve(r)
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// bitbucket posts a signed Bitbucket delivery.
func (h *harness) bitbucket(t *testing.T, team, secret, eventKey string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	raw := jsonBytes(t, body)
	r := newRequest(http.MethodPost, "/webhooks/bitbucket/"+team, raw)
	r.Header.Set("X-Hub-Signature", sign(secret, raw))
	if eventKey != "" {
		r.Header.Set("X-Event-Key", eventKey)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return h.serve(r)
}

func jsonBytes(t *testing.T, v any) []byte {
	t.Helper()
	switch b := v.(type) {
	case []byte:
		return b
	case string:
		return []byte(b)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "parse", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// lines returns the captured log lines, each as a map.
func (h *harness) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

// processed returns the webhook_processed lines.
func (h *harness) processed(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range h.lines(t) {
		if l["msg"] == "webhook_processed" {
			out = append(out, l)
		}
	}
	return out
}

func expectStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status = %d, want %d; body %s", w.Code, want, w.Body.String())
	}
}

func expectBody(t *testing.T, w *httptest.ResponseRecorder, want string) {
	t.Helper()
	if got := strings.TrimSpace(w.Body.String()); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

var errBoom = errors.New("boom")
