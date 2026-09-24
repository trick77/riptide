// Package settings reads the RIPTIDE_* environment. A `.env` file in the
// working directory fills in variables the environment leaves unset, so
// `go run` from a checkout works without exporting anything.
package settings

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Settings is the resolved process configuration.
type Settings struct {
	// DBURL is a libpq/pgx URL. A SQLAlchemy driver suffix
	// (`postgresql+asyncpg://`) is accepted and stripped, so secrets written
	// for the Python collector keep working.
	DBURL        string
	ConfigPath   string
	TeamKeysPath string
	LogLevel     string
	// Env is stamped on every log line as `env` (dev / intg / prod).
	Env string
	// ReloadInterval is how often riptide.json and team-keys.json are
	// re-read for hot reload.
	ReloadInterval time.Duration
	// ListenAddr is where serve binds.
	ListenAddr string
}

// Lookup reads one variable; os.LookupEnv in production, a map in tests.
type Lookup func(string) (string, bool)

// Defaults point at the in-repo dev samples, for a run from the repo root.
const (
	defaultDBURL        = "postgres://riptide:riptide@localhost:5432/riptide" //nolint:gosec // the compose dev database
	defaultConfigPath   = "openshift/collector/riptide.json"
	defaultTeamKeysPath = "openshift/collector/team-keys.json"
)

// Load resolves the settings from lookup.
func Load(lookup Lookup) (Settings, error) {
	get := func(key, def string) string {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	s := Settings{
		DBURL:        NormalizeDBURL(get("RIPTIDE_DB_URL", defaultDBURL)),
		ConfigPath:   get("RIPTIDE_CONFIG_PATH", defaultConfigPath),
		TeamKeysPath: get("RIPTIDE_TEAM_KEYS_PATH", defaultTeamKeysPath),
		LogLevel:     get("RIPTIDE_LOG_LEVEL", "INFO"),
		Env:          get("RIPTIDE_ENV", "dev"),
		ListenAddr:   get("RIPTIDE_LISTEN_ADDR", ":8000"),
	}
	raw := get("RIPTIDE_CONFIG_RELOAD_SECONDS", "30")
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || secs <= 0 || secs > 86400 {
		return Settings{}, fmt.Errorf("RIPTIDE_CONFIG_RELOAD_SECONDS must be a number of seconds in (0, 86400], got %q", raw)
	}
	s.ReloadInterval = time.Duration(secs * float64(time.Second))
	return s, nil
}

// NormalizeDBURL drops a SQLAlchemy `+driver` suffix from the scheme.
func NormalizeDBURL(u string) string {
	scheme, rest, ok := strings.Cut(u, "://")
	if !ok {
		return u
	}
	if base, _, found := strings.Cut(scheme, "+"); found {
		return base + "://" + rest
	}
	return u
}

// EnvLookup is os.LookupEnv backed by a `.env` file: the process
// environment wins, the file fills gaps. A missing file is not an error.
func EnvLookup(path string) (Lookup, error) {
	file, err := readDotEnv(path)
	if err != nil {
		return nil, err
	}
	return func(key string) (string, bool) {
		if v, ok := os.LookupEnv(key); ok {
			return v, true
		}
		v, ok := file[key]
		return v, ok
	}, nil
}

// readDotEnv parses KEY=VALUE lines. `#` comments, blank lines, an optional
// `export ` prefix and one level of matching quotes are understood; nothing
// is interpolated.
func readDotEnv(path string) (map[string]string, error) {
	out := map[string]string{}
	f, err := os.Open(path) //nolint:gosec // operator-controlled path
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		out[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}
