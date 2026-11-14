package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lookup(m map[string]string) Lookup {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestDefaults(t *testing.T) {
	s, err := Load(lookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := Settings{
		DBURL: "postgres://riptide:riptide@localhost:5432/riptide", ConfigPath: "config/riptide.json",
		TeamKeysPath: "config/team-keys.json", LogLevel: "INFO", Env: "dev",
		ReloadInterval: 30 * time.Second, ListenAddr: ":8000", UIAddr: "127.0.0.1:8080",
	}
	if s != want {
		t.Errorf("settings = %+v", s)
	}
}

func TestOverrides(t *testing.T) {
	s, err := Load(lookup(map[string]string{
		"BACKEND_DB_URL":                "postgresql+asyncpg://u:p@db:5432/r",
		"BACKEND_CONFIG_PATH":           "/etc/c.json",
		"BACKEND_TEAM_KEYS_PATH":        "/etc/k.json",
		"BACKEND_LOG_LEVEL":             "DEBUG",
		"BACKEND_ENV":                   "prod",
		"BACKEND_CONFIG_RELOAD_SECONDS": "2.5",
		"BACKEND_LISTEN_ADDR":           ":9000",
		"BACKEND_UI_ADDR":               "10.1.2.3:9090",
		"BACKEND_UI_ALLOW_REMOTE":       "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if s.DBURL != "postgresql://u:p@db:5432/r" || s.ConfigPath != "/etc/c.json" || s.TeamKeysPath != "/etc/k.json" ||
		s.LogLevel != "DEBUG" || s.Env != "prod" || s.ReloadInterval != 2500*time.Millisecond || s.ListenAddr != ":9000" ||
		s.UIAddr != "10.1.2.3:9090" {
		t.Errorf("settings = %+v", s)
	}
	blank, _ := Load(lookup(map[string]string{"BACKEND_ENV": "  "}))
	if blank.Env != "dev" {
		t.Errorf("blank value not defaulted: %q", blank.Env)
	}
}

func TestReloadSecondsValidated(t *testing.T) {
	for _, v := range []string{"0", "-1", "abc", "86401"} {
		if _, err := Load(lookup(map[string]string{"BACKEND_CONFIG_RELOAD_SECONDS": v})); err == nil {
			t.Errorf("%q accepted", v)
		}
	}
}

// A deployment still on the pre-rename names fails at startup instead of
// running on the defaults (the dev database URL).
func TestLegacyNamesRefused(t *testing.T) {
	_, err := Load(lookup(map[string]string{"RIPTIDE_DB_URL": "postgres://prod/r", "BACKEND_ENV": "prod"}))
	if err == nil || !strings.Contains(err.Error(), "RIPTIDE_DB_URL") || !strings.Contains(err.Error(), "BACKEND_*") {
		t.Fatalf("err = %v", err)
	}
	// Senders' names were never riptide's, and a blank old name is unset.
	if _, err := Load(lookup(map[string]string{"RIPTIDE_TOKEN": "t", "RIPTIDE_URL": "u", "RIPTIDE_ENV": " "})); err != nil {
		t.Fatalf("err = %v", err)
	}
	// From ./.env too: the check reads through the same lookup.
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("RIPTIDE_LISTEN_ADDR=:9000\nRIPTIDE_LOG_LEVEL=DEBUG\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := EnvLookup(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(l); err == nil || !strings.Contains(err.Error(), "RIPTIDE_LOG_LEVEL, RIPTIDE_LISTEN_ADDR set") {
		t.Fatalf("err = %v", err)
	}
}

// The exec probes dial loopback: a bind without a loopback listener is
// refused at load, not discovered as a failing probe.
func TestListenAddrNeedsALoopbackListener(t *testing.T) {
	for _, ok := range []string{":8000", "0.0.0.0:8000", "[::]:8000", "127.0.0.1:8000", "127.0.0.2:8000", "[::1]:8000", "localhost:8000"} {
		if _, err := Load(lookup(map[string]string{"BACKEND_LISTEN_ADDR": ok})); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"10.1.2.3:8000", "[2001:db8::1]:8000", "myhost:8000", "8000"} {
		if _, err := Load(lookup(map[string]string{"BACKEND_LISTEN_ADDR": bad})); err == nil || !strings.Contains(err.Error(), "BACKEND_LISTEN_ADDR") {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
}

// The UI address must be host:port on loopback: the read API has no
// authentication of its own. BACKEND_UI_ALLOW_REMOTE opts out (compose).
func TestUIAddr(t *testing.T) {
	for _, ok := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080", "127.0.0.2:8000"} {
		if _, err := Load(lookup(map[string]string{"BACKEND_UI_ADDR": ok, "BACKEND_LISTEN_ADDR": "127.0.0.1:8000"})); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"8080", "127.0.0.1", "[::1", ":8080", "0.0.0.0:8080", "10.1.2.3:8080", "myhost:8080"} {
		if _, err := Load(lookup(map[string]string{"BACKEND_UI_ADDR": bad})); err == nil || !strings.Contains(err.Error(), "BACKEND_UI_ADDR") {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
	for _, remote := range []string{":8080", "0.0.0.0:8080", "10.1.2.3:8080"} {
		if _, err := Load(lookup(map[string]string{"BACKEND_UI_ADDR": remote, "BACKEND_UI_ALLOW_REMOTE": "true"})); err != nil {
			t.Errorf("%s with the opt-in refused: %v", remote, err)
		}
	}
	if _, err := Load(lookup(map[string]string{"BACKEND_UI_ALLOW_REMOTE": "maybe"})); err == nil ||
		!strings.Contains(err.Error(), "BACKEND_UI_ALLOW_REMOTE") {
		t.Errorf("bad opt-in: err = %v", err)
	}
}

// The collector's port on an overlapping host is a clear error at load, not
// a bind failure at startup.
func TestUIAddrMustNotShareTheCollectorsPort(t *testing.T) {
	for _, c := range []struct{ ui, listen string }{
		{"127.0.0.1:8000", ":8000"},
		{"localhost:8000", "127.0.0.1:8000"},
		{"[::1]:9000", "[::]:9000"},
		{":8000", "127.0.0.1:8000"},
	} {
		_, err := Load(lookup(map[string]string{"BACKEND_UI_ADDR": c.ui, "BACKEND_LISTEN_ADDR": c.listen, "BACKEND_UI_ALLOW_REMOTE": "true"}))
		if err == nil || !strings.Contains(err.Error(), "same port") {
			t.Errorf("%s vs %s: err = %v", c.ui, c.listen, err)
		}
	}
	if _, err := Load(lookup(map[string]string{"BACKEND_UI_ADDR": "127.0.0.2:8000", "BACKEND_LISTEN_ADDR": "127.0.0.1:8000"})); err != nil {
		t.Errorf("distinct hosts refused: %v", err)
	}
}

func TestSlogLevel(t *testing.T) {
	for in, want := range map[string]string{
		"DEBUG": "DEBUG", "warning": "warning", "CRITICAL": "error", " critical ": "error", "LOUD": "info", "": "",
	} {
		if got := (Settings{LogLevel: in}).SlogLevel(); got != want {
			t.Errorf("SlogLevel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeDBURL(t *testing.T) {
	for in, want := range map[string]string{
		"postgresql+asyncpg://a@b/c": "postgresql://a@b/c",
		"postgres://a@b/c":           "postgres://a@b/c",
		"host=x dbname=y":            "host=x dbname=y",
	} {
		if got := NormalizeDBURL(in); got != want {
			t.Errorf("NormalizeDBURL(%q) = %q", in, got)
		}
	}
}

func TestEnvLookup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\n\nexport BACKEND_ENV=\"intg\"\nBACKEND_LOG_LEVEL='DEBUG'\nno-equals-sign\nBACKEND_TEST_ONLY_KEY = spaced \n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BACKEND_LOG_LEVEL", "ERROR")
	l, err := EnvLookup(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := l("BACKEND_ENV"); v != "intg" {
		t.Errorf("file value = %q", v)
	}
	if v, _ := l("BACKEND_LOG_LEVEL"); v != "ERROR" {
		t.Errorf("environment should win: %q", v)
	}
	if v, _ := l("BACKEND_TEST_ONLY_KEY"); v != "spaced" {
		t.Errorf("spaced = %q", v)
	}
	if _, ok := l("BACKEND_NOT_SET_ANYWHERE"); ok {
		t.Error("unset key found")
	}
	if _, err := EnvLookup(filepath.Join(dir, "missing")); err != nil {
		t.Errorf("missing file: %v", err)
	}
	if _, err := EnvLookup(dir); err == nil {
		t.Error("directory accepted as a file")
	}
}
