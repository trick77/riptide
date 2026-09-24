package config

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Runtime holds the live config and team keys and hot-reloads both. Readers
// get an immutable snapshot through an atomic pointer and never block on a
// reload.
type Runtime struct {
	configPath, keysPath string
	log                  *slog.Logger

	cfg  atomic.Pointer[Config]
	keys atomic.Pointer[Keys]

	mu              sync.Mutex // serialises Reload, guards the fields below
	cfgSum, keysSum [sha256.Size]byte
	// lastFailure remembers the failure last reported per file, so one bad
	// file version, or one config waiting for keys the kubelet has not
	// mounted yet, is logged and counted once rather than on every tick.
	lastFailure  map[string]string
	cfgFailures  atomic.Int64
	keysFailures atomic.Int64
}

// Load reads and cross-validates both files. Any fault is fatal: a
// collector that starts without its teams would reject every sender.
func Load(configPath, keysPath string, log *slog.Logger) (*Runtime, error) {
	r := &Runtime{configPath: configPath, keysPath: keysPath, log: log, lastFailure: map[string]string{}}
	cfgData, err := os.ReadFile(configPath) //nolint:gosec // operator-controlled path
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := Parse(cfgData)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", configPath, err)
	}
	keysData, err := os.ReadFile(keysPath) //nolint:gosec // operator-controlled path
	if err != nil {
		return nil, fmt.Errorf("read team keys: %w", err)
	}
	keys, err := ParseKeys(keysData)
	if err != nil {
		return nil, fmt.Errorf("team keys %s: %w", keysPath, err)
	}
	extra, err := CrossValidate(cfg, keys)
	if err != nil {
		return nil, err
	}
	if len(extra) > 0 {
		log.Warn("team_keys_extra_entries", "extra", extra)
	}
	r.cfg.Store(cfg)
	r.keys.Store(keys)
	r.cfgSum = sha256.Sum256(cfgData)
	r.keysSum = sha256.Sum256(keysData)
	return r, nil
}

// Config is the current config snapshot.
func (r *Runtime) Config() *Config { return r.cfg.Load() }

// Keys is the current team-keys snapshot.
func (r *Runtime) Keys() *Keys { return r.keys.Load() }

// ConfigReloadFailures counts failed config reload attempts since start.
func (r *Runtime) ConfigReloadFailures() int64 { return r.cfgFailures.Load() }

// KeysReloadFailures counts failed team-keys reload attempts since start.
func (r *Runtime) KeysReloadFailures() int64 { return r.keysFailures.Load() }

// Run reloads every interval until ctx is done. Polling on a timer rather
// than per request means every route sees a change, whichever route
// happens to be called, and no request pays for the file reads.
func (r *Runtime) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.Reload()
		}
	}
}

// Reload re-reads both files and applies whichever changed. Change detection
// is by content hash, so a same-second rewrite is not missed and a
// Kubernetes symlink swap that leaves the content alone is a no-op. A file
// that fails to read, parse or cross-validate against the other keeps the
// previous version live and is retried on the next tick; each distinct
// failure is logged and counted once.
func (r *Runtime) Reload() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg, keys := r.cfg.Load(), r.keys.Load()
	var newCfgSum, newKeysSum [sha256.Size]byte
	cfgChanged, keysChanged := false, false

	if data, err := os.ReadFile(r.configPath); err != nil {
		r.fail("config", r.configPath, "", err, &r.cfgFailures)
	} else if sum := sha256.Sum256(data); sum != r.cfgSum {
		newCfgSum = sum
		if parsed, err := Parse(data); err != nil {
			r.fail("config", r.configPath, short(sum), err, &r.cfgFailures)
		} else {
			cfg, cfgChanged = parsed, true
		}
	} else {
		delete(r.lastFailure, "config")
	}
	if data, err := os.ReadFile(r.keysPath); err != nil {
		r.fail("team_keys", r.keysPath, "", err, &r.keysFailures)
	} else if sum := sha256.Sum256(data); sum != r.keysSum {
		newKeysSum = sum
		if parsed, err := ParseKeys(data); err != nil {
			r.fail("team_keys", r.keysPath, short(sum), err, &r.keysFailures)
		} else {
			keys, keysChanged = parsed, true
		}
	} else {
		delete(r.lastFailure, "team_keys")
	}
	if !cfgChanged && !keysChanged {
		return
	}

	extra, err := CrossValidate(cfg, keys)
	if err != nil {
		// The pair is what failed, so both versions are in the key: the
		// missing keys arriving changes it, and the retry is reported.
		pair := short(newCfgSum) + "+" + short(newKeysSum)
		if cfgChanged {
			r.fail("config", r.configPath, pair, err, &r.cfgFailures)
		}
		if keysChanged {
			r.fail("team_keys", r.keysPath, pair, err, &r.keysFailures)
		}
		return
	}
	if cfgChanged {
		r.cfg.Store(cfg)
		r.cfgSum = newCfgSum
		delete(r.lastFailure, "config")
		r.log.Info("config_reloaded", "teams", len(cfg.Teams))
	}
	if keysChanged {
		r.keys.Store(keys)
		r.keysSum = newKeysSum
		delete(r.lastFailure, "team_keys")
		r.log.Info("team_keys_reloaded", "teams", len(keys.TeamNames()))
	}
	if len(extra) > 0 {
		r.log.Warn("team_keys_extra_entries", "extra", extra)
	}
}

func short(sum [sha256.Size]byte) string { return fmt.Sprintf("%x", sum[:8]) }

// fail logs `<what>_stat_failed` when the file could not be read and
// `<what>_reload_failed` when its content was rejected, and counts it,
// unless this same failure of this same version was already reported.
func (r *Runtime) fail(what, path, version string, err error, counter *atomic.Int64) {
	key := version + "|" + err.Error()
	if r.lastFailure[what] == key {
		return
	}
	r.lastFailure[what] = key
	counter.Add(1)
	msg := what + "_reload_failed"
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		msg = what + "_stat_failed"
	}
	r.log.Error(msg, "path", path, "error", err.Error())
}
