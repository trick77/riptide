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

	mu              sync.Mutex // serialises Reload
	cfgSum, keysSum [sha256.Size]byte
	cfgFailures     atomic.Int64
	keysFailures    atomic.Int64
}

// Load reads and cross-validates both files. Any fault is fatal: a
// collector that starts without its teams would reject every sender.
func Load(configPath, keysPath string, log *slog.Logger) (*Runtime, error) {
	r := &Runtime{configPath: configPath, keysPath: keysPath, log: log}
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
// that fails to read, parse or cross-validate against the other is logged
// and counted, and the previous version stays live; it is retried on the
// next tick.
func (r *Runtime) Reload() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfg, keys := r.cfg.Load(), r.keys.Load()
	var newCfgSum, newKeysSum [sha256.Size]byte
	cfgChanged, keysChanged := false, false

	if data, err := os.ReadFile(r.configPath); err != nil {
		r.fail("config", r.configPath, err, &r.cfgFailures)
	} else if sum := sha256.Sum256(data); sum != r.cfgSum {
		if parsed, err := Parse(data); err != nil {
			r.fail("config", r.configPath, err, &r.cfgFailures)
		} else {
			cfg, newCfgSum, cfgChanged = parsed, sum, true
		}
	}
	if data, err := os.ReadFile(r.keysPath); err != nil {
		r.fail("team_keys", r.keysPath, err, &r.keysFailures)
	} else if sum := sha256.Sum256(data); sum != r.keysSum {
		if parsed, err := ParseKeys(data); err != nil {
			r.fail("team_keys", r.keysPath, err, &r.keysFailures)
		} else {
			keys, newKeysSum, keysChanged = parsed, sum, true
		}
	}
	if !cfgChanged && !keysChanged {
		return
	}

	extra, err := CrossValidate(cfg, keys)
	if err != nil {
		if cfgChanged {
			r.fail("config", r.configPath, err, &r.cfgFailures)
		}
		if keysChanged {
			r.fail("team_keys", r.keysPath, err, &r.keysFailures)
		}
		return
	}
	if cfgChanged {
		r.cfg.Store(cfg)
		r.cfgSum = newCfgSum
		r.log.Info("config_reloaded", "teams", len(cfg.Teams))
	}
	if keysChanged {
		r.keys.Store(keys)
		r.keysSum = newKeysSum
		r.log.Info("team_keys_reloaded", "teams", len(keys.TeamNames()))
	}
	if len(extra) > 0 {
		r.log.Warn("team_keys_extra_entries", "extra", extra)
	}
}

// fail logs `<what>_stat_failed` when the file could not be read and
// `<what>_reload_failed` when its content was rejected.
func (r *Runtime) fail(what, path string, err error, counter *atomic.Int64) {
	counter.Add(1)
	msg := what + "_reload_failed"
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		msg = what + "_stat_failed"
	}
	r.log.Error(msg, "path", path, "error", err.Error())
}
