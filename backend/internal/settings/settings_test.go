package settings

import (
	"os"
	"path/filepath"
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
		ReloadInterval: 30 * time.Second, ListenAddr: ":8000",
	}
	if s != want {
		t.Errorf("settings = %+v", s)
	}
}

func TestOverrides(t *testing.T) {
	s, err := Load(lookup(map[string]string{
		"RIPTIDE_DB_URL":                "postgresql+asyncpg://u:p@db:5432/r",
		"RIPTIDE_CONFIG_PATH":           "/etc/c.json",
		"RIPTIDE_TEAM_KEYS_PATH":        "/etc/k.json",
		"RIPTIDE_LOG_LEVEL":             "DEBUG",
		"RIPTIDE_ENV":                   "prod",
		"RIPTIDE_CONFIG_RELOAD_SECONDS": "2.5",
		"RIPTIDE_LISTEN_ADDR":           ":9000",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if s.DBURL != "postgresql://u:p@db:5432/r" || s.ConfigPath != "/etc/c.json" || s.TeamKeysPath != "/etc/k.json" ||
		s.LogLevel != "DEBUG" || s.Env != "prod" || s.ReloadInterval != 2500*time.Millisecond || s.ListenAddr != ":9000" {
		t.Errorf("settings = %+v", s)
	}
	blank, _ := Load(lookup(map[string]string{"RIPTIDE_ENV": "  "}))
	if blank.Env != "dev" {
		t.Errorf("blank value not defaulted: %q", blank.Env)
	}
}

func TestReloadSecondsValidated(t *testing.T) {
	for _, v := range []string{"0", "-1", "abc", "86401"} {
		if _, err := Load(lookup(map[string]string{"RIPTIDE_CONFIG_RELOAD_SECONDS": v})); err == nil {
			t.Errorf("%q accepted", v)
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
	content := "# comment\n\nexport RIPTIDE_ENV=\"intg\"\nRIPTIDE_LOG_LEVEL='DEBUG'\nno-equals-sign\nRIPTIDE_TEST_ONLY_KEY = spaced \n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RIPTIDE_LOG_LEVEL", "ERROR")
	l, err := EnvLookup(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := l("RIPTIDE_ENV"); v != "intg" {
		t.Errorf("file value = %q", v)
	}
	if v, _ := l("RIPTIDE_LOG_LEVEL"); v != "ERROR" {
		t.Errorf("environment should win: %q", v)
	}
	if v, _ := l("RIPTIDE_TEST_ONLY_KEY"); v != "spaced" {
		t.Errorf("spaced = %q", v)
	}
	if _, ok := l("RIPTIDE_NOT_SET_ANYWHERE"); ok {
		t.Error("unset key found")
	}
	if _, err := EnvLookup(filepath.Join(dir, "missing")); err != nil {
		t.Errorf("missing file: %v", err)
	}
	if _, err := EnvLookup(dir); err == nil {
		t.Error("directory accepted as a file")
	}
}
