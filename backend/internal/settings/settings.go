// Package settings reads the BACKEND_* environment. A `.env` file in the
// working directory fills in variables the environment leaves unset, so
// `go run` from a checkout works without exporting anything.
package settings

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/trick77/ship/kit/logging"
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
	// ListenAddr is where serve binds the collector: webhooks, /auth/ping
	// and the probes.
	ListenAddr string
	// UIAddr is where serve binds the UI listener: the read API under /api.
	// Loopback by default, so in the pod only the auth proxy sidecar
	// reaches it.
	UIAddr string
}

// Lookup reads one variable; os.LookupEnv in production, a map in tests.
type Lookup func(string) (string, bool)

// Defaults point at the in-repo dev samples, for a run from the repo root.
const (
	defaultDBURL        = "postgres://riptide:riptide@localhost:5432/riptide" //nolint:gosec // the compose dev database
	defaultConfigPath   = "config/riptide.json"
	defaultTeamKeysPath = "config/team-keys.json"
)

// legacyNames are the variables riptide read before the BACKEND_* rename.
// Senders' own RIPTIDE_* variables (RIPTIDE_TOKEN, RIPTIDE_URL, ...) were
// never read here and are not listed.
var legacyNames = []string{
	"RIPTIDE_DB_URL", "RIPTIDE_CONFIG_PATH", "RIPTIDE_TEAM_KEYS_PATH", "RIPTIDE_LOG_LEVEL",
	"RIPTIDE_ENV", "RIPTIDE_LISTEN_ADDR", "RIPTIDE_CONFIG_RELOAD_SECONDS",
}

// CheckLegacy fails when any of names is set (blank counts as unset): a
// deployment still on the old names would otherwise start on the defaults,
// the dev database URL among them.
func CheckLegacy(lookup Lookup, names ...string) error {
	var set []string
	for _, n := range names {
		if v, ok := lookup(n); ok && strings.TrimSpace(v) != "" {
			set = append(set, n)
		}
	}
	if len(set) == 0 {
		return nil
	}
	return fmt.Errorf("%s set: riptide's RIPTIDE_* variables were renamed to BACKEND_*, suffix unchanged (see docs/change-notes.md)",
		strings.Join(set, ", "))
}

// Load resolves the settings from lookup. A legacy RIPTIDE_* name is an error.
func Load(lookup Lookup) (Settings, error) {
	if err := CheckLegacy(lookup, legacyNames...); err != nil {
		return Settings{}, err
	}
	get := func(key, def string) string {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return def
	}
	s := Settings{
		DBURL:        NormalizeDBURL(get("BACKEND_DB_URL", defaultDBURL)),
		ConfigPath:   get("BACKEND_CONFIG_PATH", defaultConfigPath),
		TeamKeysPath: get("BACKEND_TEAM_KEYS_PATH", defaultTeamKeysPath),
		LogLevel:     get("BACKEND_LOG_LEVEL", "INFO"),
		Env:          get("BACKEND_ENV", "dev"),
		ListenAddr:   get("BACKEND_LISTEN_ADDR", ":8000"),
		UIAddr:       get("BACKEND_UI_ADDR", "127.0.0.1:8080"),
	}
	if err := checkListenAddr(s.ListenAddr); err != nil {
		return Settings{}, err
	}
	allowRemote, err := strconv.ParseBool(get("BACKEND_UI_ALLOW_REMOTE", "false"))
	if err != nil {
		return Settings{}, fmt.Errorf("BACKEND_UI_ALLOW_REMOTE must be true or false: %w", err)
	}
	if err := checkUIAddr(s.UIAddr, s.ListenAddr, allowRemote); err != nil {
		return Settings{}, err
	}
	raw := get("BACKEND_CONFIG_RELOAD_SECONDS", "30")
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || secs <= 0 || secs > 86400 {
		return Settings{}, fmt.Errorf("BACKEND_CONFIG_RELOAD_SECONDS must be a number of seconds in (0, 86400], got %q", raw)
	}
	s.ReloadInterval = time.Duration(secs * float64(time.Second))
	return s, nil
}

// checkListenAddr refuses a bind with no loopback listener: the exec probes
// (-healthcheck, -readycheck) dial loopback, the only peer kit's probes
// answer, so on a concrete non-loopback address they could never pass.
func checkListenAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("BACKEND_LISTEN_ADDR %q: %w", addr, err)
	}
	if host == "" || host == "localhost" {
		return nil
	}
	if ip, err := netip.ParseAddr(host); err == nil && (ip.IsUnspecified() || ip.Unmap().IsLoopback()) {
		return nil
	}
	return fmt.Errorf("BACKEND_LISTEN_ADDR %q: bind a wildcard (:8000, 0.0.0.0:8000) or a loopback address; the probes dial loopback", addr)
}

// checkUIAddr refuses a UI listener anyone but the pod could reach: the read
// API does no authentication of its own, the auth proxy sidecar in front of
// it on loopback does. allowRemote opts out for compose, where the
// container binds every interface and the host publishes the port on its
// own loopback. A port the collector already binds on an overlapping host is
// refused here, with both names, rather than as a bind error at startup.
func checkUIAddr(addr, listenAddr string, allowRemote bool) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("BACKEND_UI_ADDR %q: %w", addr, err)
	}
	if !allowRemote && !isLoopback(host) {
		return fmt.Errorf("BACKEND_UI_ADDR %q: bind a loopback address (127.0.0.1:8080); the read API has no authentication "+
			"of its own. Set BACKEND_UI_ALLOW_REMOTE=true only where something else keeps it private (compose)", addr)
	}
	lhost, lport, err := net.SplitHostPort(listenAddr)
	if err == nil && port == lport && overlaps(host, lhost) {
		return fmt.Errorf("BACKEND_UI_ADDR %q and BACKEND_LISTEN_ADDR %q bind the same port", addr, listenAddr)
	}
	return nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

// overlaps reports whether two bind hosts can claim the same socket: equal,
// or either one every interface. localhost counts as 127.0.0.1.
func overlaps(a, b string) bool {
	norm := func(h string) string {
		if h == "localhost" {
			return "127.0.0.1"
		}
		if ip, err := netip.ParseAddr(h); err == nil {
			if ip.IsUnspecified() {
				return ""
			}
			return ip.Unmap().String()
		}
		return h
	}
	a, b = norm(a), norm(b)
	return a == "" || b == "" || a == b
}

// SlogLevel is LogLevel as kit/logging reads it. The Python collector's
// CRITICAL is error, and an unknown value stays info, as it always has: kit
// refuses one at startup, which would turn a typo in a deployed manifest
// into a crash loop.
func (s Settings) SlogLevel() string {
	if strings.EqualFold(strings.TrimSpace(s.LogLevel), "critical") {
		return "error"
	}
	if _, err := logging.ParseLevel(s.LogLevel); err != nil {
		return "info"
	}
	return s.LogLevel
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
