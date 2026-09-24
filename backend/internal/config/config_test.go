package config

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validConfig = `{
  "teams": [
    {"name": "checkout", "group_email": "team-checkout@example.com"},
    {"name": "platform", "group_email": "Platform <team-platform@example.com>"}
  ],
  "automation": {
    "renovate": {"authors": ["renovate-bot", "renovate[bot]"], "branch_prefixes": ["Renovate/"]},
    "dependabot": {"authors": ["dependabot[bot]"], "branch_prefixes": ["dependabot/"]},
    "noergler": {"authors": ["noergler"], "branch_prefixes": []}
  }
}`

func mustParse(t *testing.T, s string) *Config {
	t.Helper()
	c, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestParseValid(t *testing.T) {
	c := mustParse(t, validConfig)
	if !reflect.DeepEqual(c.TeamNames(), []string{"checkout", "platform"}) {
		t.Errorf("teams = %v", c.TeamNames())
	}
	if c.Teams["checkout"].GroupEmail != "team-checkout@example.com" {
		t.Errorf("email = %q", c.Teams["checkout"].GroupEmail)
	}
	var names []string
	for _, a := range c.Automation {
		names = append(names, a.Name)
	}
	if !reflect.DeepEqual(names, []string{"renovate", "dependabot", "noergler"}) {
		t.Errorf("automation order = %v", names)
	}
	if c.Automation[0].BranchPrefixes[0] != "renovate/" {
		t.Errorf("prefix not lowercased: %v", c.Automation[0].BranchPrefixes)
	}
	if c.Environments.ProductionStage != DefaultProductionStage || len(c.Environments.IgnoredStages) != 0 {
		t.Errorf("environments = %+v", c.Environments)
	}
}

func TestParseEmptyAndNullSections(t *testing.T) {
	c := mustParse(t, `{"teams": null, "automation": null, "environments": null, "unrelated": 1}`)
	if len(c.Teams) != 0 || len(c.Automation) != 0 || c.Environments.ProductionStage != "prod" {
		t.Errorf("config = %+v", c)
	}
}

func TestParseRejects(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"non-object root", `[]`, "JSON object"},
		{"null root", `null`, "JSON object"},
		{"invalid json", `{`, "invalid JSON"},
		{"teams not a list", `{"teams": {}}`, "`teams` must be a list"},
		{"team not an object", `{"teams": ["x"]}`, "teams[0] must be an object"},
		{"invalid email", `{"teams": [{"name": "x", "group_email": "not-an-email"}]}`, "group_email"},
		{"email without dot in domain", `{"teams": [{"name": "x", "group_email": "a@localhost"}]}`, "group_email"},
		{"missing email", `{"teams": [{"name": "x"}]}`, "group_email"},
		{"duplicate team", `{"teams": [{"name": "x", "group_email": "a@b.c"}, {"name": "x", "group_email": "a@b.c"}]}`, "duplicate team"},
		{"missing name", `{"teams": [{"group_email": "a@b.c"}]}`, "missing `name`"},
		{"numeric name", `{"teams": [{"name": 3, "group_email": "a@b.c"}]}`, "missing `name`"},
		{"automation not an object", `{"automation": []}`, "`automation` must be an object"},
		{"automation source not an object", `{"automation": {"x": []}}`, "automation.x must be an object"},
		{"authors a string", `{"automation": {"x": {"authors": "bot"}}}`, "automation.x.authors must be a list of strings"},
		{"authors with a number", `{"automation": {"x": {"authors": [1]}}}`, "must be a list of strings"},
		{"empty prefix", `{"automation": {"x": {"branch_prefixes": [""]}}}`, "non-empty strings"},
		{"duplicate source", `{"automation": {"x": {}, "x": {}}}`, "duplicate automation source"},
		{"environments not an object", `{"environments": []}`, "`environments` must be an object"},
		{"empty production stage", `{"environments": {"production_stage": " "}}`, "production_stage"},
		{"ignored stages not a list", `{"environments": {"ignored_stages": "dev"}}`, "ignored_stages"},
		{"ignored stages null", `{"environments": {"ignored_stages": null}}`, "ignored_stages"},
		{"empty ignored stage", `{"environments": {"ignored_stages": [""]}}`, "ignored_stages"},
		{"prod in ignored", `{"environments": {"production_stage": "Prod", "ignored_stages": ["PROD"]}}`, "production_stage"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestEnvironments(t *testing.T) {
	c := mustParse(t, `{"environments": {"production_stage": " PROD ", "ignored_stages": ["DEV", " entw", "syst"]}}`)
	if c.Environments.ProductionStage != "prod" {
		t.Errorf("stage = %q", c.Environments.ProductionStage)
	}
	if !reflect.DeepEqual(c.Environments.IgnoredStages, map[string]bool{"dev": true, "entw": true, "syst": true}) {
		t.Errorf("ignored = %v", c.Environments.IgnoredStages)
	}
}

func TestDetectAutomationSource(t *testing.T) {
	c := mustParse(t, validConfig)
	for _, tc := range []struct {
		name                    string
		author, display, branch string
		service                 bool
		want                    string
	}{
		{"author match", "renovate-bot", "", "", false, "renovate"},
		{"author match is case-insensitive", "Renovate-Bot", "", "", false, "renovate"},
		{"branch prefix", "alice", "", "renovate/something", false, "renovate"},
		{"configured prefix lowercased", "alice", "", "renovate/x", false, "renovate"},
		{"bot-shaped fallback", "some-bot", "", "feature/x", false, "other-bot"},
		{"bracket bot", "thing[bot]", "", "", false, "other-bot"},
		{"bot- prefix", "bot-deploy", "", "", false, "other-bot"},
		{"human", "alice", "", "feature/x", false, ""},
		{"review bot by login", "noergler", "", "", false, "noergler"},
		{"host service account", "bitbucket.system-user", "", "", true, "service-account"},
		{"configured source beats service flag", "renovate-bot", "", "", true, "renovate"},
		{"service flag beats bot shape", "ci-bot", "", "", true, "service-account"},
		{"human stays human", "alice", "Alice", "feature/x", false, ""},
		{"display name match", "rop", "noergler", "", false, "noergler"},
		{"display name case-insensitive", "rop", "Noergler", "", false, "noergler"},
		{"bot-shaped display name", "svc01", "release-bot", "", false, "other-bot"},
		{"human display name", "alice", "Alice Example", "feature/x", false, ""},
		{"nothing at all", "", "", "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.DetectAutomationSource(tc.author, tc.display, tc.branch, tc.service); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// --- keys --------------------------------------------------------------------

func TestParseKeys(t *testing.T) {
	k, err := ParseKeys([]byte(`{"checkout": {"bitbucket": "b", "argocd": "a"}, "platform": {"jenkins": "j"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(k.TeamNames(), []string{"checkout", "platform"}) {
		t.Errorf("teams = %v", k.TeamNames())
	}
	if s, ok := k.Secret("checkout", "bitbucket"); !ok || s != "b" {
		t.Errorf("secret = %q %v", s, ok)
	}
	if _, ok := k.Secret("checkout", "jenkins"); ok {
		t.Error("absent source has a secret")
	}
	if _, ok := k.Secret("ghost", "bitbucket"); ok {
		t.Error("unknown team has a secret")
	}
	if !k.Has("platform") || k.Has("ghost") {
		t.Error("Has is wrong")
	}
}

func TestParseKeysRejects(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"non-object root", `[]`, "JSON object"},
		{"null root", `null`, "JSON object"},
		{"invalid json", `{`, "invalid JSON"},
		{"flat string value", `{"checkout": "raw"}`, "object of source"},
		{"empty inner object", `{"checkout": {}}`, "non-empty object"},
		{"unknown source", `{"checkout": {"github": "x"}}`, "unknown source"},
		{"empty token", `{"checkout": {"argocd": ""}}`, "non-empty string"},
		{"non-string token", `{"checkout": {"argocd": 1}}`, "non-empty string"},
		{"empty team", `{"": {"argocd": "a"}}`, "non-empty string"},
		{"token shared by two teams", `{"a": {"argocd": "same"}, "b": {"jenkins": "same"}}`, "share a token"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseKeys([]byte(c.body))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
	// One team reusing a token across its own sources is its own business.
	if _, err := ParseKeys([]byte(`{"a": {"argocd": "same", "jenkins": "same"}}`)); err != nil {
		t.Errorf("same-team reuse rejected: %v", err)
	}
}

func TestLookup(t *testing.T) {
	k, _ := ParseKeys([]byte(`{"checkout": {"argocd": "a-raw", "jenkins": "j"}, "platform": {"argocd": "p-raw", "bitbucket": "bb"}}`))
	for _, c := range []struct {
		token, source, want string
	}{
		{"a-raw", SourceArgoCD, "checkout"},
		{"p-raw", SourceArgoCD, "platform"},
		{"a-raw", SourceBitbucket, ""},
		{"a-raw", "github", ""},
		{"wrong", SourceArgoCD, ""},
		{"", SourceArgoCD, ""},
		{"a-rawé", SourceArgoCD, ""},
	} {
		got, ok := k.Lookup(c.token, c.source)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("Lookup(%q, %q) = %q %v, want %q", c.token, c.source, got, ok, c.want)
		}
	}
	for token, want := range map[string]string{"a-raw": "checkout", "j": "checkout", "bb": "platform", "nope": "", "": ""} {
		if got, _ := k.LookupAnySource(token); got != want {
			t.Errorf("LookupAnySource(%q) = %q, want %q", token, got, want)
		}
	}
}

func TestCrossValidate(t *testing.T) {
	cfg := mustParse(t, validConfig)
	keys, _ := ParseKeys([]byte(`{"checkout": {"argocd": "a"}}`))
	if _, err := CrossValidate(cfg, keys); err == nil || !strings.Contains(err.Error(), "platform") {
		t.Fatalf("missing keys not reported: %v", err)
	}
	keys, _ = ParseKeys([]byte(`{"checkout": {"argocd": "a"}, "platform": {"argocd": "p"}, "extra": {"argocd": "e"}}`))
	extra, err := CrossValidate(cfg, keys)
	if err != nil || !reflect.DeepEqual(extra, []string{"extra"}) {
		t.Fatalf("extra = %v, %v", extra, err)
	}
}

// --- runtime -----------------------------------------------------------------

type logCapture struct{ bytes.Buffer }

func (l *logCapture) messages() []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(l.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(line), &m)
		out = append(out, m["msg"].(string))
	}
	return out
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const twoTeamKeys = `{"checkout": {"argocd": "a"}, "platform": {"argocd": "p"}}`

func newRuntime(t *testing.T) (*Runtime, string, string, *logCapture) {
	t.Helper()
	dir := t.TempDir()
	cfgPath, keysPath := filepath.Join(dir, "riptide.json"), filepath.Join(dir, "team-keys.json")
	write(t, cfgPath, validConfig)
	write(t, keysPath, twoTeamKeys)
	logs := &logCapture{}
	rt, err := Load(cfgPath, keysPath, slog.New(slog.NewJSONHandler(logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return rt, cfgPath, keysPath, logs
}

func TestLoadFailures(t *testing.T) {
	dir := t.TempDir()
	cfgPath, keysPath := filepath.Join(dir, "c.json"), filepath.Join(dir, "k.json")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if _, err := Load(cfgPath, keysPath, log); err == nil {
		t.Error("missing config accepted")
	}
	write(t, cfgPath, `[]`)
	if _, err := Load(cfgPath, keysPath, log); err == nil {
		t.Error("bad config accepted")
	}
	write(t, cfgPath, validConfig)
	if _, err := Load(cfgPath, keysPath, log); err == nil {
		t.Error("missing keys accepted")
	}
	write(t, keysPath, `{"checkout": "x"}`)
	if _, err := Load(cfgPath, keysPath, log); err == nil {
		t.Error("bad keys accepted")
	}
	write(t, keysPath, `{"checkout": {"argocd": "a"}}`)
	if _, err := Load(cfgPath, keysPath, log); err == nil || !strings.Contains(err.Error(), "platform") {
		t.Errorf("team without keys accepted: %v", err)
	}
	logs := &logCapture{}
	write(t, keysPath, `{"checkout": {"argocd": "a"}, "platform": {"argocd": "p"}, "ghost": {"argocd": "g"}}`)
	if _, err := Load(cfgPath, keysPath, slog.New(slog.NewJSONHandler(logs, nil))); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "team_keys_extra_entries") || !strings.Contains(logs.String(), "ghost") {
		t.Errorf("extra keys not warned: %s", logs.String())
	}
}

func TestReloadPicksUpChanges(t *testing.T) {
	rt, cfgPath, keysPath, logs := newRuntime(t)
	rt.Reload()
	if len(logs.messages()) != 0 {
		t.Fatalf("unchanged reload logged: %v", logs.messages())
	}
	write(t, keysPath, `{"checkout": {"argocd": "a"}, "platform": {"argocd": "p"}, "team-y": {"argocd": "y"}}`)
	write(t, cfgPath, strings.Replace(validConfig, `"teams": [`, `"teams": [{"name": "team-y", "group_email": "y@example.com"},`, 1))
	rt.Reload()
	if _, ok := rt.Config().Teams["team-y"]; !ok {
		t.Error("config change not picked up")
	}
	if team, _ := rt.Keys().Lookup("y", SourceArgoCD); team != "team-y" {
		t.Error("key change not picked up")
	}
	got := strings.Join(logs.messages(), ",")
	if got != "config_reloaded,team_keys_reloaded" {
		t.Errorf("logs = %s", got)
	}
}

func TestReloadKeepsOldOnFailure(t *testing.T) {
	rt, cfgPath, keysPath, logs := newRuntime(t)
	write(t, cfgPath, `{"teams": [{"name": "x"}]}`)
	write(t, keysPath, `not json`)
	rt.Reload()
	if rt.ConfigReloadFailures() != 1 || rt.KeysReloadFailures() != 1 {
		t.Errorf("failures = %d/%d", rt.ConfigReloadFailures(), rt.KeysReloadFailures())
	}
	if _, ok := rt.Config().Teams["checkout"]; !ok {
		t.Error("old config dropped")
	}
	if team, _ := rt.Keys().Lookup("a", SourceArgoCD); team != "checkout" {
		t.Error("old keys dropped")
	}
	if got := strings.Join(logs.messages(), ","); got != "config_reload_failed,team_keys_reload_failed" {
		t.Errorf("logs = %s", got)
	}
}

func TestReloadRejectsInconsistentPair(t *testing.T) {
	rt, cfgPath, keysPath, logs := newRuntime(t)
	// A config team without keys: rejected, the old config stays.
	write(t, cfgPath, strings.Replace(validConfig, `"teams": [`, `"teams": [{"name": "team-y", "group_email": "y@example.com"},`, 1))
	rt.Reload()
	if _, ok := rt.Config().Teams["team-y"]; ok || rt.ConfigReloadFailures() != 1 {
		t.Fatal("inconsistent config applied")
	}
	// Removing a configured team's keys: rejected too.
	write(t, keysPath, `{"checkout": {"argocd": "a"}}`)
	rt.Reload()
	if !rt.Keys().Has("platform") || rt.KeysReloadFailures() != 1 {
		t.Fatal("keys for a configured team were dropped")
	}
	// The keys arrive: both files now agree and both apply.
	write(t, keysPath, `{"checkout": {"argocd": "a"}, "platform": {"argocd": "p"}, "team-y": {"argocd": "y"}}`)
	rt.Reload()
	if _, ok := rt.Config().Teams["team-y"]; !ok || !rt.Keys().Has("team-y") {
		t.Fatal("consistent pair not applied")
	}
	if !strings.Contains(logs.String(), "missing entries for teams in the config") {
		t.Errorf("reason not logged: %s", logs.String())
	}
}

func TestReloadStatFailure(t *testing.T) {
	rt, cfgPath, _, logs := newRuntime(t)
	if err := os.Remove(cfgPath); err != nil {
		t.Fatal(err)
	}
	rt.Reload()
	if rt.ConfigReloadFailures() != 1 || !strings.Contains(logs.String(), "config_stat_failed") {
		t.Errorf("failures = %d, logs = %s", rt.ConfigReloadFailures(), logs.String())
	}
	if _, ok := rt.Config().Teams["checkout"]; !ok {
		t.Error("config dropped")
	}
}

func TestReloadExtraKeysWarn(t *testing.T) {
	rt, _, keysPath, logs := newRuntime(t)
	write(t, keysPath, `{"checkout": {"argocd": "a"}, "platform": {"argocd": "p"}, "ghost": {"argocd": "g"}}`)
	rt.Reload()
	if !strings.Contains(logs.String(), "team_keys_extra_entries") {
		t.Errorf("logs = %s", logs.String())
	}
}

// A config waiting for keys the kubelet has not mounted yet is the normal
// rollout order: it is reported once, not on every tick, and applies as soon
// as the keys arrive.
func TestReloadReportsEachFailureOnce(t *testing.T) {
	rt, cfgPath, keysPath, logs := newRuntime(t)
	write(t, cfgPath, strings.Replace(validConfig, `"teams": [`, `"teams": [{"name": "team-y", "group_email": "y@example.com"},`, 1))
	for i := 0; i < 3; i++ {
		rt.Reload()
	}
	if rt.ConfigReloadFailures() != 1 || strings.Count(logs.String(), "config_reload_failed") != 1 {
		t.Fatalf("failures = %d, logs = %s", rt.ConfigReloadFailures(), logs.String())
	}
	write(t, keysPath, `{"checkout": {"argocd": "a"}, "platform": {"argocd": "p"}, "team-y": {"argocd": "y"}}`)
	rt.Reload()
	if _, ok := rt.Config().Teams["team-y"]; !ok {
		t.Fatal("config not applied once its keys arrived")
	}
	// A broken file, then a different broken file: two reports.
	write(t, keysPath, `nope`)
	rt.Reload()
	rt.Reload()
	write(t, keysPath, `still nope`)
	rt.Reload()
	if rt.KeysReloadFailures() != 2 {
		t.Errorf("keys failures = %d", rt.KeysReloadFailures())
	}
	// Reverting to the live version clears it, so the same breakage later is
	// reported again.
	write(t, keysPath, `{"checkout": {"argocd": "a"}, "platform": {"argocd": "p"}, "team-y": {"argocd": "y"}}`)
	rt.Reload()
	write(t, keysPath, `still nope`)
	rt.Reload()
	if rt.KeysReloadFailures() != 3 {
		t.Errorf("keys failures after revert = %d", rt.KeysReloadFailures())
	}
	// A missing file is a stat failure, also reported once.
	if err := os.Remove(cfgPath); err != nil {
		t.Fatal(err)
	}
	rt.Reload()
	rt.Reload()
	if strings.Count(logs.String(), "config_stat_failed") != 1 {
		t.Errorf("logs = %s", logs.String())
	}
}
