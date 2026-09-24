package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeBitbucket is just enough Bitbucket Data Center REST for the onboarder.
type fakeBitbucket struct {
	mu        sync.Mutex
	hooks     map[string][]map[string]any // "PROJ/repo" -> webhooks
	nextID    int
	calls     []string
	denyRead  map[string]int // repo key -> status for GET repo
	denyWrite int
	pageSize  int
	adminRepo [][2]string
}

func newFake() *fakeBitbucket {
	return &fakeBitbucket{hooks: map[string][]map[string]any{}, nextID: 1, denyRead: map[string]int{}, pageSize: 100}
}

func (f *fakeBitbucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	if r.Header.Get("Authorization") != "Bearer bb-token" {
		w.WriteHeader(401)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rest/api/1.0/"), "/")
	if len(parts) == 1 && parts[0] == "repos" {
		f.page(w, r, f.adminRepoValues())
		return
	}
	if len(parts) < 4 || parts[0] != "projects" || parts[2] != "repos" {
		w.WriteHeader(404)
		return
	}
	key := parts[1] + "/" + parts[3]
	if code := f.denyRead[key]; code != 0 {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, `{"errors":[{"message":"no"}]}`)
		return
	}
	switch {
	case len(parts) == 4:
		_, _ = io.WriteString(w, `{"slug":"`+parts[3]+`"}`)
	case parts[4] == "pull-requests":
		_, _ = io.WriteString(w, `{"values":[]}`)
	case parts[4] == "webhooks" && len(parts) == 5 && r.Method == http.MethodGet:
		f.page(w, r, f.hooks[key])
	case parts[4] == "webhooks" && len(parts) == 5 && r.Method == http.MethodPost:
		if f.denyWrite != 0 {
			w.WriteHeader(f.denyWrite)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["id"] = f.nextID
		f.nextID++
		f.hooks[key] = append(f.hooks[key], body)
		_ = json.NewEncoder(w).Encode(body)
	case parts[4] == "webhooks" && len(parts) == 6:
		id, _ := strconv.Atoi(parts[5])
		for i, h := range f.hooks[key] {
			if toInt(h["id"]) != id {
				continue
			}
			if r.Method == http.MethodDelete {
				f.hooks[key] = append(f.hooks[key][:i], f.hooks[key][i+1:]...)
				w.WriteHeader(204)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			body["id"] = id
			f.hooks[key][i] = body
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		w.WriteHeader(404)
	default:
		w.WriteHeader(404)
	}
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	}
	return -1
}

func (f *fakeBitbucket) adminRepoValues() []map[string]any {
	var out []map[string]any
	for _, r := range f.adminRepo {
		out = append(out, map[string]any{"slug": r[1], "project": map[string]any{"key": r[0]}})
	}
	return out
}

func (f *fakeBitbucket) page(w http.ResponseWriter, r *http.Request, values []map[string]any) {
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))
	end := min(start+f.pageSize, len(values))
	if start > len(values) {
		start = len(values)
	}
	resp := map[string]any{"values": values[start:end], "isLastPage": end >= len(values)}
	if end < len(values) {
		resp["nextPageStart"] = end
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func onboarder(_ *fakeBitbucket, srv *httptest.Server) *Onboarder {
	return &Onboarder{Client: NewClient(srv.URL, "bb-token"), WebhookURL: "https://riptide.example.com/webhooks/bitbucket/",
		Team: "platform", TeamKey: "hmac-secret", Name: DefaultWebhookName}
}

func TestOnboardCreatesThenUpdates(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f)
	defer srv.Close()
	o := onboarder(f, srv)
	var logs []string
	o.Log = func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }
	p := RepoSpec{Project: "PLAT", Repo: "riptide"}

	res := o.Onboard(context.Background(), p)
	if res.Status != "ok" || res.Detail != "webhook configured" || res.Diff[0] != "create" {
		t.Fatalf("create: %+v", res)
	}
	hook := f.hooks["PLAT/riptide"][0]
	if hook["url"] != "https://riptide.example.com/webhooks/bitbucket/platform" || hook["active"] != true ||
		hook["configuration"].(map[string]any)["secret"] != "hmac-secret" || len(hook["events"].([]any)) != len(RequiredEvents) {
		t.Errorf("hook = %v", hook)
	}

	// Drift: wrong URL, missing and extra events, inactive.
	hook["url"] = "https://old.example.com/hook"
	hook["events"] = []any{"pr:opened", "repo:comment:added"}
	hook["active"] = false
	res = o.Onboard(context.Background(), p)
	if res.Status != "ok" || len(res.Diff) != 4 || !strings.Contains(res.Diff[1], "extra=[repo:comment:added]") || res.Diff[2] != "active: false -> true" {
		t.Fatalf("update: %+v", res)
	}
	if len(f.hooks["PLAT/riptide"]) != 1 || f.hooks["PLAT/riptide"][0]["url"] != "https://riptide.example.com/webhooks/bitbucket/platform" {
		t.Errorf("hooks = %v", f.hooks["PLAT/riptide"])
	}
	// Up to date apart from the secret, which is always rewritten.
	res = o.Onboard(context.Background(), p)
	if len(res.Diff) != 1 || !strings.HasPrefix(res.Diff[0], "configuration.secret") {
		t.Errorf("diff = %v", res.Diff)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "read permissions OK") {
		t.Errorf("logs = %v", logs)
	}
}

func TestOnboardDryRunWritesNothing(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f)
	defer srv.Close()
	o := onboarder(f, srv)
	o.DryRun = true
	var logs []string
	o.Log = func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }
	res := o.Onboard(context.Background(), RepoSpec{"PLAT", "riptide"})
	if res.Status != "ok" || res.Detail != "dry-run" || len(f.hooks) != 0 {
		t.Fatalf("res = %+v, hooks = %v", res, f.hooks)
	}
	if joined := strings.Join(logs, "\n"); strings.Contains(joined, "hmac-secret") || !strings.Contains(joined, `"secret":"***"`) {
		t.Errorf("secret not redacted: %s", joined)
	}
	f.hooks["PLAT/riptide"] = []map[string]any{{"id": 7, "name": "riptide", "url": "x"}}
	res = o.Onboard(context.Background(), RepoSpec{"PLAT", "riptide"})
	if res.Detail != "dry-run" || f.hooks["PLAT/riptide"][0]["url"] != "x" {
		t.Errorf("dry-run update wrote: %+v", res)
	}
	res = o.Remove(context.Background(), RepoSpec{"PLAT", "riptide"})
	if res.Detail != "dry-run: would remove webhook id=7" || len(f.hooks["PLAT/riptide"]) != 1 {
		t.Errorf("dry-run remove: %+v", res)
	}
}

func TestOnboardFailures(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f)
	defer srv.Close()
	o := onboarder(f, srv)
	f.denyRead["PLAT/secret"] = 403
	res := o.Onboard(context.Background(), RepoSpec{"PLAT", "secret"})
	if res.Status != "failed" || !strings.HasPrefix(res.Detail, "permission check HTTP 403") {
		t.Errorf("read denied: %+v", res)
	}
	f.denyWrite = 403
	res = o.Onboard(context.Background(), RepoSpec{"PLAT", "riptide"})
	if res.Status != "failed" || !strings.HasPrefix(res.Detail, "upsert webhook HTTP 403") {
		t.Errorf("write denied: %+v", res)
	}
	res = o.Remove(context.Background(), RepoSpec{"PLAT", "secret"})
	if res.Status != "failed" || !strings.HasPrefix(res.Detail, "list webhooks HTTP 403") {
		t.Errorf("remove denied: %+v", res)
	}
	dead := &Onboarder{Client: NewClient("http://127.0.0.1:1", "t"), Name: "riptide"}
	if res := dead.Onboard(context.Background(), RepoSpec{"A", "b"}); res.Status != "failed" || !strings.HasPrefix(res.Detail, "permission check: ") {
		t.Errorf("unreachable: %+v", res)
	}
	if !strings.Contains((&HTTPError{Status: 500, Body: strings.Repeat("x", 300), URL: "u"}).Error(), "HTTP 500 for u") {
		t.Error("HTTPError message")
	}
}

func TestRemove(t *testing.T) {
	f := newFake()
	srv := httptest.NewServer(f)
	defer srv.Close()
	o := onboarder(f, srv)
	p := RepoSpec{"PLAT", "riptide"}
	if res := o.Remove(context.Background(), p); res.Status != "skipped" {
		t.Errorf("absent: %+v", res)
	}
	o.Onboard(context.Background(), p)
	f.hooks["PLAT/riptide"] = append(f.hooks["PLAT/riptide"], map[string]any{"id": 99, "name": "someone-else"})
	res := o.Remove(context.Background(), p)
	if res.Status != "ok" || res.Detail != "webhook removed: id=1" || len(f.hooks["PLAT/riptide"]) != 1 {
		t.Errorf("remove: %+v, hooks %v", res, f.hooks)
	}
}

func TestPaginationFollowsNextPageStart(t *testing.T) {
	f := newFake()
	f.pageSize = 2
	for i := 0; i < 5; i++ {
		f.hooks["PLAT/r"] = append(f.hooks["PLAT/r"], map[string]any{"id": i, "name": fmt.Sprintf("h%d", i)})
	}
	srv := httptest.NewServer(f)
	defer srv.Close()
	hooks, err := NewClient(srv.URL, "bb-token").ListWebhooks(context.Background(), RepoSpec{"PLAT", "r"})
	if err != nil || len(hooks) != 5 || hooks[4].Name != "h4" {
		t.Errorf("hooks = %v, %v", hooks, err)
	}
}

func TestParseInput(t *testing.T) {
	good := `{"bitbucket_url": "https://bb.example.com/", "webhook_url": "http://riptide/webhooks/bitbucket", "team": " platform ",
		"projects": [{"project": "PLAT", "repos": ["riptide", " noergler "]}, {"project": "APP", "repos": ["foo"]}]}`
	in, err := ParseInput([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if in.BitbucketURL != "https://bb.example.com" || in.Team != "platform" || len(in.Repos) != 3 || in.Repos[1].Key() != "PLAT/noergler" {
		t.Errorf("input = %+v", in)
	}
	for _, c := range []struct{ body, want string }{
		{`[]`, "JSON object"},
		{`{"webhook_url": "https://r", "team": "t", "projects": []}`, "'bitbucket_url' must be a non-empty string"},
		{`{"bitbucket_url": "http://bb", "webhook_url": "https://r", "team": "t"}`, "'bitbucket_url' must be an https URL"},
		{`{"bitbucket_url": "https://bb", "webhook_url": "ftp://r", "team": "t"}`, "'webhook_url' must be an http(s) URL"},
		{`{"bitbucket_url": "https://bb", "webhook_url": "https://r", "team": " "}`, "'team'"},
		{`{"bitbucket_url": "https://bb", "webhook_url": "https://r", "team": "t", "projects": []}`, "'projects'"},
		{`{"bitbucket_url": "https://bb", "webhook_url": "https://r", "team": "t", "projects": ["x"]}`, "projects[0] must be an object"},
		{`{"bitbucket_url": "https://bb", "webhook_url": "https://r", "team": "t", "projects": [{"repos": ["a"]}]}`, "projects[0].project"},
		{`{"bitbucket_url": "https://bb", "webhook_url": "https://r", "team": "t", "projects": [{"project": "P"}]}`, "projects[0].repos must be"},
		{`{"bitbucket_url": "https://bb", "webhook_url": "https://r", "team": "t", "projects": [{"project": "P", "repos": [1]}]}`, "projects[0].repos[0]"},
		{`{"bitbucket_url": "https://bb", "webhook_url": "https://r", "team": "t", "projects": [{"project": "P", "repos": ["a", "a"]}]}`, "duplicate repo entry: P/a"},
	} {
		if _, err := ParseInput([]byte(c.body)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.body, err, c.want)
		}
	}
	if _, err := LoadInput(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("missing file accepted")
	}
}

// --- CLI -----------------------------------------------------------------------

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func runMain(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Main(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestMainOnboardsAndRemoves(t *testing.T) {
	f := newFake()
	dir := t.TempDir()
	t.Chdir(dir)
	env := writeFile(t, dir, "secrets.env", "BITBUCKET_TOKEN=bb-token\nRIPTIDE_TEAM_KEY=\"hmac-secret\"\n")
	// The config insists on https, so the fake runs behind TLS.
	tls := httptest.NewTLSServer(f)
	defer tls.Close()
	cfgPath := writeFile(t, dir, "onboard.json", `{"bitbucket_url": "`+tls.URL+`", "webhook_url": "http://riptide/webhooks/bitbucket",
		"team": "platform", "projects": [{"project": "PLAT", "repos": ["a", "b"]}]}`)
	saved := defaultHTTP
	defaultHTTP = tls.Client()
	defer func() { defaultHTTP = saved }()

	code, out, errOut := runMain(cfgPath, "--env-file", env, "--dry-run")
	if code != 0 || !strings.Contains(out, "PLAT/a") || !strings.Contains(out, "dry-run") || len(f.hooks) != 0 {
		t.Fatalf("dry run: %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "bb-t-****") || strings.Contains(errOut, "hmac-secret") || !strings.Contains(errOut, "travel unencrypted") {
		t.Errorf("stderr = %s", errOut)
	}
	code, out, _ = runMain(cfgPath, "--env-file", env)
	if code != 0 || len(f.hooks["PLAT/a"]) != 1 || len(f.hooks["PLAT/b"]) != 1 || !strings.Contains(out, "webhook configured") {
		t.Fatalf("onboard: %d %s", code, out)
	}
	code, out, _ = runMain("--remove", cfgPath, "--env-file", env, "--name", "riptide")
	if code != 0 || len(f.hooks["PLAT/a"]) != 0 || !strings.Contains(out, "webhook removed") {
		t.Fatalf("remove: %d %s", code, out)
	}
	// A failing repo stops the run and names what was not processed.
	f.denyRead["PLAT/a"] = 403
	code, out, errOut = runMain(cfgPath, "--env-file", env)
	if code != 1 || !strings.Contains(out, "failed") || !strings.Contains(errOut, "not processed: PLAT/b") {
		t.Fatalf("failure: %d\n%s\n%s", code, out, errOut)
	}

	// Discover lists admin repos grouped by project, JSON on stdout only.
	f.adminRepo = [][2]string{{"PLAT", "b"}, {"APP", "x"}, {"PLAT", "a"}}
	code, out, _ = runMain("--discover", "--bitbucket-url", tls.URL, "--env-file", env)
	var projects []struct {
		Project string   `json:"project"`
		Repos   []string `json:"repos"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &projects) != nil || len(projects) != 2 || projects[1].Project != "PLAT" ||
		strings.Join(projects[1].Repos, ",") != "a,b" {
		t.Fatalf("discover: %d %s", code, out)
	}
}

func TestMainUsageErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("RIPTIDE_TEAM_KEY", "")
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{nil, 2, "config path is required"},
		{[]string{"--bogus"}, 2, "flag provided but not defined"},
		{[]string{"a.json", "b.json"}, 2, ""},
		{[]string{"cfg.json"}, 2, "missing required environment variable(s): BITBUCKET_TOKEN, RIPTIDE_TEAM_KEY"},
		{[]string{"cfg.json", "--env-file", "nope.env"}, 2, "nope.env"},
		{[]string{"--discover"}, 2, "--discover requires --bitbucket-url"},
		{[]string{"--discover", "--bitbucket-url", "http://bb"}, 2, "must be an https URL"},
		{[]string{"--discover", "--bitbucket-url", "https://bb"}, 2, "BITBUCKET_TOKEN"},
		{[]string{"-h"}, 0, "usage: riptide onboard-bitbucket"},
	} {
		code, _, errOut := runMain(c.args...)
		if code != c.code || !strings.Contains(errOut, c.want) {
			t.Errorf("%v: %d %q, want %d %q", c.args, code, errOut, c.code, c.want)
		}
	}
	// Secrets resolved, but the config is bad.
	writeFile(t, dir, ".env", "BITBUCKET_TOKEN=x\nRIPTIDE_TEAM_KEY=y\n")
	writeFile(t, dir, "cfg.json", `[]`)
	if code, _, errOut := runMain("cfg.json"); code != 2 || !strings.Contains(errOut, "JSON object") {
		t.Errorf("bad config: %d %s", code, errOut)
	}
	// Discover against an unreachable host fails with 1.
	if code, _, _ := runMain("--discover", "--bitbucket-url", "https://127.0.0.1:1"); code != 1 {
		t.Errorf("unreachable discover = %d", code)
	}
}

func TestResolveEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("BITBUCKET_TOKEN", "from-env")
	t.Setenv("RIPTIDE_TEAM_KEY", "key-from-env")
	writeFile(t, dir, ".env", "# c\nBITBUCKET_TOKEN=from-dotenv\nnot a pair\n=x\n")
	explicit := writeFile(t, dir, "x.env", "BITBUCKET_TOKEN='from-file'\n")
	got, err := resolveEnv(explicit, "BITBUCKET_TOKEN", "RIPTIDE_TEAM_KEY")
	if err != nil || got["BITBUCKET_TOKEN"] != "from-file" || got["RIPTIDE_TEAM_KEY"] != "key-from-env" {
		t.Errorf("explicit file: %v %v", got, err)
	}
	got, _ = resolveEnv("", "BITBUCKET_TOKEN")
	if got["BITBUCKET_TOKEN"] != "from-dotenv" {
		t.Errorf("dotenv over environment: %v", got)
	}
	if mask("abc") != "****" || mask("abcdef") != "abcd-****" {
		t.Error("mask")
	}
}
