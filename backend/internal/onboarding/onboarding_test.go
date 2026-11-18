package onboarding

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeBitbucket is just enough Bitbucket Data Center REST for the onboarder:
// project and repo webhooks, a project's repos, paging.
type fakeBitbucket struct {
	mu       sync.Mutex
	repos    map[string][]string         // project -> slugs
	hooks    map[string][]map[string]any // "PROJ" or "PROJ/repo" -> webhooks
	nextID   int
	writes   []string
	reads    map[string]int // "PROJ[/repo]" -> webhook listings
	deny     map[string]int // "METHOD PROJ[/repo]" -> status
	pageSize int
}

func newFake() *fakeBitbucket {
	return &fakeBitbucket{repos: map[string][]string{}, hooks: map[string][]map[string]any{}, nextID: 100,
		reads: map[string]int{}, deny: map[string]int{}, pageSize: 100}
}

func (f *fakeBitbucket) hook(key, name, url string, events ...string) {
	if events == nil {
		events = RequiredEvents
	}
	f.hooks[key] = append(f.hooks[key], map[string]any{"id": f.nextID, "name": name, "url": url,
		"events": events, "active": true})
	f.nextID++
}

func (f *fakeBitbucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer bb-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/rest/api/1.0/projects/"), "/")
	key, rest := parts[0], parts[1:]
	if len(rest) >= 2 && rest[0] == "repos" {
		key, rest = key+"/"+rest[1], rest[2:]
	} else if len(rest) == 1 && rest[0] == "repos" {
		var values []map[string]any
		for _, s := range f.repos[key] {
			values = append(values, map[string]any{"slug": s})
		}
		f.page(w, r, values)
		return
	}
	if code := f.deny[r.Method+" "+key]; code != 0 {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, `{"errors":[{"message":"denied"}]}`)
		return
	}
	if len(rest) == 0 || rest[0] != "webhooks" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		f.writes = append(f.writes, r.Method+" "+key)
	} else {
		f.reads[key]++
	}
	switch {
	case len(rest) == 1 && r.Method == http.MethodGet:
		f.page(w, r, f.hooks[key])
	case len(rest) == 1 && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["id"] = f.nextID
		f.nextID++
		f.hooks[key] = append(f.hooks[key], body)
		_ = json.NewEncoder(w).Encode(body)
	case len(rest) == 2:
		id, _ := strconv.Atoi(rest[1])
		for i, h := range f.hooks[key] {
			if toInt(h["id"]) != id {
				continue
			}
			if r.Method == http.MethodDelete {
				f.hooks[key] = slices.Delete(f.hooks[key], i, i+1)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			body["id"] = id
			f.hooks[key][i] = body
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	default:
		w.WriteHeader(http.StatusNotFound)
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

func (f *fakeBitbucket) page(w http.ResponseWriter, r *http.Request, values []map[string]any) {
	start, _ := strconv.Atoi(r.URL.Query().Get("start"))
	start = min(start, len(values))
	end := min(start+f.pageSize, len(values))
	resp := map[string]any{"values": values[start:end], "isLastPage": end >= len(values)}
	if end < len(values) {
		resp["nextPageStart"] = end
	}
	_ = json.NewEncoder(w).Encode(resp)
}

const (
	wantURL  = "https://riptide.example.com/webhooks/bitbucket/platform"
	oldURL   = "https://riptide-collector.example.com/webhooks/bitbucket/platform"
	otherURL = "https://riptide.example.com/webhooks/bitbucket/checkout"
)

func setup(t *testing.T) (*fakeBitbucket, *Onboarder) {
	t.Helper()
	f := newFake()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, &Onboarder{Client: NewClient(srv.URL, "bb-token", srv.Client()), Team: "platform",
		WebhookURL: wantURL, Secret: "hmac-secret"}
}

var (
	proj = Target{Project: "PLAT"}
	repo = Target{Project: "~ME", Repo: "tool"}
)

func TestOnboardProjectCreatesThenRewrites(t *testing.T) {
	f, o := setup(t)
	r := o.Onboard(t.Context(), proj)
	if r.Status != "ok" || !slices.Equal(r.Diff, []string{"create"}) {
		t.Fatalf("create: %+v", r)
	}
	h := f.hooks["PLAT"][0]
	cfg, _ := h["configuration"].(map[string]any)
	if h["url"] != wantURL || h["name"] != "riptide" || cfg["secret"] != "hmac-secret" || h["sslVerificationRequired"] != true {
		t.Fatalf("hook = %v", h)
	}
	if len(h["events"].([]any)) != len(RequiredEvents) {
		t.Fatalf("events = %v", h["events"])
	}
	// Up to date, and still written: the secret cannot be compared.
	r = o.Onboard(t.Context(), proj)
	if r.Status != "ok" || len(r.Diff) != 0 || !strings.Contains(r.Detail, "secret rewritten") {
		t.Fatalf("rerun: %+v", r)
	}
	if !slices.Equal(f.writes, []string{"POST PLAT", "PUT PLAT"}) {
		t.Fatalf("writes = %v", f.writes)
	}
}

// The case it was built for: hooks left on a host riptide moved away from.
func TestOnboardMovesTheHostAndPrunesRepoHooks(t *testing.T) {
	f, o := setup(t)
	f.repos["PLAT"] = []string{"a", "b", "c", "d"}
	f.hook("PLAT/a", "riptide", oldURL)
	f.hook("PLAT/b", "riptide", otherURL)
	f.hook("PLAT/c", "noergler", "https://noergler.example.com/webhook/platform")
	f.hook("PLAT/d", "riptide", oldURL, "pr:opened")

	st := o.Status(t.Context(), proj)
	if st.Status != "missing" || !slices.Equal(st.RepoHooks, []string{"a", "d"}) || len(st.Foreign) != 1 {
		t.Fatalf("status: %+v", st)
	}

	r := o.Onboard(t.Context(), proj)
	if r.Status != "ok" || !slices.Equal(r.RepoHooks, []string{"a", "d"}) ||
		!slices.Equal(r.Foreign, []string{"PLAT/b -> " + otherURL}) {
		t.Fatalf("onboard: %+v", r)
	}
	if len(f.hooks["PLAT/a"]) != 0 || len(f.hooks["PLAT/d"]) != 0 || len(f.hooks["PLAT/b"]) != 1 || len(f.hooks["PLAT/c"]) != 1 {
		t.Fatalf("hooks = %v", f.hooks)
	}
	if st := o.Status(t.Context(), proj); st.Status != "ok" || len(st.RepoHooks) != 0 {
		t.Fatalf("status after: %+v", st)
	}
}

func TestOnboardUpdatesAStaleHook(t *testing.T) {
	f, o := setup(t)
	f.hook("PLAT", "riptide", oldURL, "pr:opened", "repo:forked")
	st := o.Status(t.Context(), proj)
	if st.Status != "stale" || len(st.Diff) != 3 || !strings.HasPrefix(st.Diff[0], "url: "+oldURL) {
		t.Fatalf("status: %+v", st)
	}
	r := o.Onboard(t.Context(), proj)
	if r.Status != "ok" || len(r.Diff) != 3 || f.hooks["PLAT"][0]["url"] != wantURL {
		t.Fatalf("onboard: %+v %v", r, f.hooks["PLAT"])
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	f, o := setup(t)
	o.DryRun = true
	f.repos["PLAT"] = []string{"a"}
	f.hook("PLAT/a", "riptide", oldURL)
	if r := o.Onboard(t.Context(), proj); r.Status != "ok" || !strings.HasPrefix(r.Detail, "dry run") ||
		!slices.Equal(r.RepoHooks, []string{"a"}) {
		t.Fatalf("project: %+v", r)
	}
	f.hook("PLAT", "riptide", wantURL)
	if r := o.Remove(t.Context(), proj); r.Status != "ok" || !strings.Contains(r.Detail, "would remove") {
		t.Fatalf("remove: %+v", r)
	}
	if len(f.writes) != 0 {
		t.Fatalf("writes = %v", f.writes)
	}
}

// A shared project: the other team's hook stays, ours goes next to it, and
// ours is found whichever comes first.
func TestAnotherTeamsHookIsLeftAlone(t *testing.T) {
	f, o := setup(t)
	f.hook("PLAT", "riptide", otherURL)
	if r := o.Status(t.Context(), proj); r.Status != "foreign" {
		t.Fatalf("status: %+v", r)
	}
	if r := o.Remove(t.Context(), proj); r.Status != "skipped" || len(f.writes) != 0 {
		t.Fatalf("remove: %+v %v", r, f.writes)
	}
	r := o.Onboard(t.Context(), proj)
	if r.Status != "ok" || !slices.Equal(r.Diff, []string{"create"}) || !slices.Equal(r.Foreign, []string{"PLAT -> " + otherURL}) {
		t.Fatalf("onboard: %+v", r)
	}
	if st := o.Status(t.Context(), proj); st.Status != "ok" {
		t.Fatalf("status after: %+v", st)
	}
	if r := o.Remove(t.Context(), proj); r.Status != "ok" || len(f.hooks["PLAT"]) != 1 || f.hooks["PLAT"][0]["url"] != otherURL {
		t.Fatalf("remove: %+v %v", r, f.hooks["PLAT"])
	}
}

// A public URL with a prefix: the hooks riptide wrote are still its own.
func TestPrefixedPublicURL(t *testing.T) {
	f, o := setup(t)
	o.WebhookURL = "https://gw.example.com/riptide/webhooks/bitbucket/platform"
	o.Onboard(t.Context(), proj)
	if r := o.Status(t.Context(), proj); r.Status != "ok" {
		t.Fatalf("status: %+v", r)
	}
	if r := o.Remove(t.Context(), proj); r.Status != "ok" || len(f.hooks["PLAT"]) != 0 {
		t.Fatalf("remove: %+v", r)
	}
}

func TestDryRunSaysWouldPrune(t *testing.T) {
	f, o := setup(t)
	o.DryRun = true
	f.repos["PLAT"] = []string{"a"}
	f.hook("PLAT/a", "riptide", oldURL)
	if r := o.Onboard(t.Context(), proj); !strings.Contains(r.Detail, "would prune 1") {
		t.Fatalf("onboard: %+v", r)
	}
}

// A repo gone between the repo list and its hook list has none; the rest
// are still pruned.
func TestRepoGoneMidwayIsNoFailure(t *testing.T) {
	f, o := setup(t)
	f.repos["PLAT"] = []string{"gone", "a"}
	f.hook("PLAT/a", "riptide", oldURL)
	f.deny["GET PLAT/gone"] = http.StatusNotFound
	if r := o.Onboard(t.Context(), proj); r.Status != "ok" || !slices.Equal(r.RepoHooks, []string{"a"}) {
		t.Fatalf("onboard: %+v", r)
	}
	f.deny["GET PLAT/gone"] = http.StatusInternalServerError
	if r := o.Status(t.Context(), proj); r.Status != "failed" || !strings.Contains(r.Detail, "HTTP 500") {
		t.Fatalf("status: %+v", r)
	}
}

func TestRepoTargets(t *testing.T) {
	f, o := setup(t)
	if r := o.Onboard(t.Context(), repo); r.Status != "ok" || len(f.hooks["~ME/tool"]) != 1 {
		t.Fatalf("personal repo: %+v", r)
	}
	// No project admin on ~ME, or no project hooks there: the project check
	// is skipped, not failed.
	for _, code := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusBadRequest} {
		o.projectHooks = nil
		f.deny["GET ~ME"] = code
		if r := o.Status(t.Context(), repo); r.Status != "ok" {
			t.Fatalf("status with %d: %+v", code, r)
		}
	}
	delete(f.deny, "GET ~ME")
	o.projectHooks = nil

	// Under a project hook: skipped, and a leftover repo hook is double.
	// Onboarding the project in the same run is seen by the repo targets.
	if r := o.Onboard(t.Context(), Target{Project: "~ME"}); r.Status != "ok" {
		t.Fatalf("project: %+v", r)
	}
	if r := o.Onboard(t.Context(), repo); r.Status != "skipped" {
		t.Fatalf("under a project hook: %+v", r)
	}
	if r := o.Status(t.Context(), repo); r.Status != "double" {
		t.Fatalf("double: %+v", r)
	}
	f.hooks["~ME/tool"] = nil
	if r := o.Status(t.Context(), repo); r.Status != "ok" || !strings.Contains(r.Detail, "covered") {
		t.Fatalf("covered: %+v", r)
	}
}

// Two hooks of the team's on one target: status says double, onboard keeps
// the first, remove takes both.
func TestDuplicateHooks(t *testing.T) {
	f, o := setup(t)
	f.hook("PLAT", "riptide", oldURL)
	f.hook("PLAT", "riptide", wantURL)
	if r := o.Status(t.Context(), proj); r.Status != "double" {
		t.Fatalf("status: %+v", r)
	}
	r := o.Onboard(t.Context(), proj)
	if r.Status != "ok" || len(f.hooks["PLAT"]) != 1 || f.hooks["PLAT"][0]["url"] != wantURL ||
		!slices.Contains(r.Diff, "duplicate id=101 deleted") {
		t.Fatalf("onboard: %+v %v", r, f.hooks["PLAT"])
	}
	f.hook("PLAT", "riptide", oldURL)
	if r := o.Remove(t.Context(), proj); r.Status != "ok" || len(f.hooks["PLAT"]) != 0 {
		t.Fatalf("remove: %+v %v", r, f.hooks["PLAT"])
	}
}

// Without project admin a repo hook could sit next to a project hook
// unseen: onboard refuses, status cannot tell and says nothing more.
func TestRepoWithoutProjectAdminIsRefused(t *testing.T) {
	f, o := setup(t)
	f.deny["GET PLAT"] = http.StatusForbidden
	r := o.Onboard(t.Context(), Target{Project: "PLAT", Repo: "x"})
	if r.Status != "failed" || !strings.Contains(r.Detail, "no project admin") || len(f.writes) != 0 {
		t.Fatalf("onboard: %+v %v", r, f.writes)
	}
}

// Another team's hook on a shared repo under our project hook: covered.
func TestForeignRepoHookUnderOurProjectHook(t *testing.T) {
	f, o := setup(t)
	f.hook("PLAT", "riptide", wantURL)
	f.hook("PLAT/x", "riptide", otherURL)
	if r := o.Status(t.Context(), Target{Project: "PLAT", Repo: "x"}); r.Status != "ok" || !strings.Contains(r.Detail, "covered") {
		t.Fatalf("status: %+v", r)
	}
}

// A dry run plans like the real run: a repo after its project is skipped.
func TestDryRunPlansTheProjectHook(t *testing.T) {
	_, o := setup(t)
	o.DryRun = true
	rows, _ := Run(t.Context(), o, ActionOnboard, []Target{proj, {Project: "PLAT", Repo: "x"}})
	if rows[0].Status != "ok" || rows[1].Status != "skipped" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestUnknownActionFails(t *testing.T) {
	f, o := setup(t)
	rows, healthy := Run(t.Context(), o, Action("grant"), []Target{proj})
	if healthy || rows[0].Status != "failed" || len(f.writes) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

// Repo targets of one project read its webhooks once per run.
func TestProjectHookReadOncePerProject(t *testing.T) {
	f, o := setup(t)
	targets := []Target{{Project: "PLAT", Repo: "a"}, {Project: "PLAT", Repo: "b"}, {Project: "PLAT", Repo: "c"}}
	rows, healthy := Run(t.Context(), o, ActionOnboard, targets)
	if !healthy || len(rows) != 3 || f.reads["PLAT"] != 1 {
		t.Fatalf("rows = %+v, project reads = %d", rows, f.reads["PLAT"])
	}
}

func TestFailuresAreRows(t *testing.T) {
	f, o := setup(t)
	f.deny["POST PLAT"] = http.StatusForbidden
	r := o.Onboard(t.Context(), proj)
	if r.Status != "failed" || !strings.Contains(r.Detail, "create webhook: HTTP 403") {
		t.Fatalf("onboard: %+v", r)
	}
	f.deny["GET OTHER"] = http.StatusNotFound
	rows, healthy := Run(t.Context(), o, ActionOnboard, []Target{{Project: "OTHER"}, proj})
	if healthy || len(rows) != 2 || rows[0].Status != "failed" || rows[1].Status != "failed" {
		t.Fatalf("run: %v %+v", healthy, rows)
	}
}

func TestRemove(t *testing.T) {
	f, o := setup(t)
	f.hook("PLAT", "riptide", oldURL)
	if r := o.Remove(t.Context(), proj); r.Status != "ok" || len(f.hooks["PLAT"]) != 0 {
		t.Fatalf("remove: %+v", r)
	}
	if r := o.Remove(t.Context(), proj); r.Status != "skipped" {
		t.Fatalf("again: %+v", r)
	}
}

func TestRunHealthAndRender(t *testing.T) {
	f, o := setup(t)
	f.hook("PLAT", "riptide", wantURL)
	rows, healthy := Run(t.Context(), o, ActionStatus, []Target{proj})
	if !healthy {
		t.Fatalf("rows = %+v", rows)
	}
	rows, healthy = Run(t.Context(), o, ActionStatus, []Target{proj, repo})
	if healthy || rows[1].Status != "missing" {
		t.Fatalf("rows = %+v", rows)
	}
	text := Render(rows)
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "PLAT (project)   ok") || !strings.HasPrefix(lines[1], "~ME/tool (repo)  missing") {
		t.Fatalf("text:\n%s", text)
	}
}

func TestPaginationFollowsNextPageStart(t *testing.T) {
	f, o := setup(t)
	f.pageSize = 2
	for i := range 5 {
		f.repos["PLAT"] = append(f.repos["PLAT"], "r"+strconv.Itoa(i))
	}
	slugs, err := o.Client.ListRepos(t.Context(), "PLAT")
	if err != nil || len(slugs) != 5 {
		t.Fatalf("slugs = %v, %v", slugs, err)
	}
}
