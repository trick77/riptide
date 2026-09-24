package config

import (
	"context"
	"crypto/sha256"
	"fmt"
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

	mu              sync.Mutex        // serialises Reload, guards the fields below
	cfgSum, keysSum [sha256.Size]byte // the versions that are live
	// seenCfg and seenKeys are the file states the last Reload acted on (a
	// content hash, or the read error). While both are unchanged a tick does
	// nothing, so a bad file is parsed, logged and counted once, not every
	// 30 seconds.
	seenCfg, seenKeys string
	// lastCross is the config+keys pair whose cross-validation last failed,
	// so a config waiting for keys the kubelet has not mounted yet is
	// reported once.
	lastCross    string
	cfgFailures  atomic.Int64
	keysFailures atomic.Int64
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
	r.seenCfg, r.seenKeys = fileState(cfgData, nil), fileState(keysData, nil)
	return r, nil
}

// Config is the current config snapshot.
func (r *Runtime) Config() *Config { return r.cfg.Load() }

// Keys is the current team-keys snapshot.
func (r *Runtime) Keys() *Keys { return r.keys.Load() }

// ConfigReloadFailures counts the distinct config versions rejected since
// start: a bad file is counted once, however long it stays mounted.
func (r *Runtime) ConfigReloadFailures() int64 { return r.cfgFailures.Load() }

// KeysReloadFailures counts the distinct team-keys versions rejected since
// start.
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
// previous version live. Each failure is logged and counted once: the next
// ticks see the same file state and do nothing until a file changes.
func (r *Runtime) Reload() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cfgData, cfgErr := os.ReadFile(r.configPath)
	keysData, keysErr := os.ReadFile(r.keysPath)
	cfgState, keysState := fileState(cfgData, cfgErr), fileState(keysData, keysErr)
	if cfgState == r.seenCfg && keysState == r.seenKeys {
		return
	}
	cfgNew, keysNew := cfgState != r.seenCfg, keysState != r.seenKeys
	r.seenCfg, r.seenKeys = cfgState, keysState

	cfg, keys := r.cfg.Load(), r.keys.Load()
	cfgSum, keysSum := r.cfgSum, r.keysSum
	cfgChanged, keysChanged := false, false
	switch {
	case cfgErr != nil:
		if cfgNew {
			r.fail("config_stat_failed", r.configPath, cfgErr, &r.cfgFailures)
		}
	case sha256.Sum256(cfgData) != r.cfgSum:
		if parsed, err := Parse(cfgData); err != nil {
			if cfgNew {
				r.fail("config_reload_failed", r.configPath, err, &r.cfgFailures)
			}
		} else {
			cfg, cfgSum, cfgChanged = parsed, sha256.Sum256(cfgData), true
		}
	}
	switch {
	case keysErr != nil:
		if keysNew {
			r.fail("team_keys_stat_failed", r.keysPath, keysErr, &r.keysFailures)
		}
	case sha256.Sum256(keysData) != r.keysSum:
		if parsed, err := ParseKeys(keysData); err != nil {
			if keysNew {
				r.fail("team_keys_reload_failed", r.keysPath, err, &r.keysFailures)
			}
		} else {
			keys, keysSum, keysChanged = parsed, sha256.Sum256(keysData), true
		}
	}
	if !cfgChanged && !keysChanged {
		return
	}

	extra, err := CrossValidate(cfg, keys)
	if err != nil {
		pair := fmt.Sprintf("%x+%x", cfgSum[:8], keysSum[:8])
		if pair != r.lastCross {
			r.lastCross = pair
			if cfgChanged {
				r.fail("config_reload_failed", r.configPath, err, &r.cfgFailures)
			}
			if keysChanged {
				r.fail("team_keys_reload_failed", r.keysPath, err, &r.keysFailures)
			}
		}
		return
	}
	r.lastCross = ""
	if cfgChanged {
		r.cfg.Store(cfg)
		r.cfgSum = cfgSum
		r.log.Info("config_reloaded", "teams", len(cfg.Teams))
	}
	if keysChanged {
		r.keys.Store(keys)
		r.keysSum = keysSum
		r.log.Info("team_keys_reloaded", "teams", len(keys.TeamNames()))
	}
	if len(extra) > 0 {
		r.log.Warn("team_keys_extra_entries", "extra", extra)
	}
}

// fileState is what a read returned: the content hash, or the error.
func fileState(data []byte, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// fail logs msg and counts it.
func (r *Runtime) fail(msg, path string, err error, counter *atomic.Int64) {
	counter.Add(1)
	r.log.Error(msg, "path", path, "error", err.Error())
}
