// Command riptide is the riptide collector.
//
//	riptide serve              run the collector (default)
//	riptide migrate            apply pending database migrations and exit
//	riptide version            print the version and exit
//	riptide onboard-bitbucket  create, update or remove the riptide webhook on Bitbucket repos
//	riptide check-onboarding   list which repos, pipelines and apps reported in the last hour
//	riptide -healthcheck       probe /healthz, exit 0 on a 2xx (liveness: the container healthcheck)
//	riptide -readycheck        probe /readyz, exit 0 on a 2xx (readiness: the database answers)
//
// Both probes run inside the container: kit's /healthz and /readyz answer a
// loopback caller only and 404 anyone else, so an orchestrator's HTTP probe
// from the node never passes. Deploy them as exec probes.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"time"

	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/logging"
	"github.com/trick77/ship/kit/run"
	"github.com/trick77/ship/kit/version"

	"github.com/trick77/riptide/internal/api"
	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/httpapi"
	"github.com/trick77/riptide/internal/onboarding"
	"github.com/trick77/riptide/internal/settings"
	"github.com/trick77/riptide/internal/store"
)

const usage = "usage: riptide [serve|migrate|version|onboard-bitbucket|check-onboarding|-healthcheck|-readycheck]"

// serviceName is stamped on every log line: the image name, not the repo.
const serviceName = "riptide-collector"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var args []string
	if len(os.Args) > 2 {
		args = os.Args[2:]
	}
	var err error
	switch cmd {
	case "serve":
		err = withSettings(serve)
	case "migrate":
		err = withSettings(migrate)
	case "version":
		fmt.Println(version.String())
	case "onboard-bitbucket":
		os.Exit(onboarding.Main(args, os.Stdout, os.Stderr))
	case "check-onboarding":
		err = withSettings(func(s settings.Settings, log *slog.Logger) error {
			return checkOnboarding(s, log, args, os.Stdout)
		})
		if errors.Is(err, errSourcesMissing) {
			os.Exit(1)
		}
	case "-healthcheck":
		os.Exit(probe("/healthz", os.Stderr))
	case "-readycheck":
		os.Exit(probe("/readyz", os.Stderr))
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, usage)
	default:
		fmt.Fprintf(os.Stderr, "riptide: unknown command %q\n%s\n", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("startup_aborted", logging.Err(err))
		os.Exit(1)
	}
}

// withSettings loads the environment (plus ./.env) and sets up logging
// before running fn: settings decide the log level and env.
func withSettings(fn func(settings.Settings, *slog.Logger) error) error {
	s, err := loadSettings()
	if err != nil {
		setupLog(os.Stdout, settings.Settings{Env: "dev"})
		return err
	}
	return fn(s, setupLog(os.Stdout, s))
}

func loadSettings() (settings.Settings, error) {
	lookup, err := settings.EnvLookup(".env")
	if err != nil {
		return settings.Settings{}, err
	}
	return settings.Load(lookup)
}

// setupLog installs kit's Splunk logger as slog's default, the build's
// version on every line.
func setupLog(w io.Writer, s settings.Settings) *slog.Logger {
	log, err := logging.Setup(logging.Options{Level: s.SlogLevel(), Format: logging.Splunk, Writer: w, Service: serviceName, Env: s.Env})
	if err != nil {
		panic(err) // SlogLevel returns only levels kit knows
	}
	log = log.With("version", version.String())
	slog.SetDefault(log)
	return log
}

// probe asks one of kit's probes on the address serve binds and returns
// the exit code: the container's exec probe in a distroless image.
func probe(path string, stderr io.Writer) int {
	s, err := loadSettings()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "riptide:", err)
		return 1
	}
	return healthcheck(probeURL(s.ListenAddr, path), stderr)
}

// probeTimeout outlasts /readyz's own check budget: kit's Healthcheck gives
// up after 3s while a slow database check may take httpx.CheckTimeout (5s),
// which would fail the probe on a 503 that never got to be sent.
const probeTimeout = httpx.CheckTimeout + 2*time.Second

// healthcheck GETs url and returns the exit code: 0 on a 2xx, 1 otherwise,
// the reason on stderr (the runtime's health log keeps it). Redirects are
// not followed: a probe that redirects is misrouted.
func healthcheck(url string, stderr io.Writer) int {
	c := http.Client{
		Timeout:       probeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := c.Get(url) //nolint:gosec // the process's own listener, from its own config
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "riptide: probe:", err)
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = fmt.Fprintln(stderr, "riptide: probe:", resp.Status)
		return 1
	}
	return 0
}

// probeURL is the loopback URL of serve's listener. kit's probes answer a
// loopback peer only, and httpx.LocalURL rewrites only a wildcard host: a
// concrete BACKEND_LISTEN_ADDR IP would make the probe a remote caller and
// get a 404. A loopback bind ([::1], localhost) is dialled as is, since it
// does not listen on 127.0.0.1. A concrete non-loopback bind has no loopback
// listener at all; settings.Load refuses one.
func probeURL(listenAddr, path string) string {
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return httpx.LocalURL(listenAddr, path)
	}
	if ip, err := netip.ParseAddr(host); (err == nil && ip.Unmap().IsLoopback()) || host == "localhost" {
		return httpx.LocalURL(listenAddr, path)
	}
	return httpx.LocalURL(":"+port, path)
}

// migrate applies pending migrations and exits; it runs before serve.
// Only the database URL is needed; the config files are not read.
func migrate(s settings.Settings, log *slog.Logger) error {
	log.Info("riptide-collector version: " + version.String())
	ctx, stop := run.Context(context.Background())
	defer stop()
	db, err := store.Open(ctx, s.DBURL, log)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	log.Info("migrations_up_to_date")
	return nil
}

func serve(s settings.Settings, log *slog.Logger) error {
	log.Info("riptide-collector version: " + version.String())
	rt, err := config.Load(s.ConfigPath, s.TeamKeysPath, log)
	if err != nil {
		return err
	}

	ctx, stop := run.Context(context.Background())
	defer stop()

	db, err := store.Open(ctx, s.DBURL, log)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.SchemaCurrent(ctx); err != nil {
		return err
	}

	go rt.Run(ctx, s.ReloadInterval)

	deps := api.Deps{Store: db, Reader: db, Runtime: rt, Log: log}
	collector := httpapi.New(log, db.ReadyChecks()...)
	api.Register(collector, deps)
	// The read API gets a listener of its own, never a route on the
	// collector's: in the pod only the auth proxy sidecar reaches
	// 127.0.0.1:8080, so a caller of the webhook route (a Route or Ingress
	// to :8000) cannot reach /api, whatever it sends.
	ui := httpapi.NewUI(log)
	api.RegisterUI(ui, deps)

	cfg := rt.Config()
	log.Info("riptide_collector_starting",
		"teams", len(cfg.Teams),
		"keys", len(rt.Keys().TeamNames()),
		"production_stage", cfg.Environments.ProductionStage)
	hs := httpx.NewServer(s.ListenAddr, collector.Handler())
	// Webhook bodies are small and arrive whole: a sender trickling one is
	// cut off, as before kit. No SSE or downloads here for it to cut.
	hs.ReadTimeout = 60 * time.Second
	us := httpx.NewServer(s.UIAddr, ui.Handler())
	log.Info("listening", "addr", s.ListenAddr, "ui_addr", s.UIAddr)
	if err := serveAll(ctx, hs, us); err != nil {
		return err
	}
	log.Info("riptide_collector_stopped")
	return nil
}

// shutdownTimeout is how long a stopping listener drains in-flight requests.
const shutdownTimeout = 15 * time.Second

// serveAll runs every server until ctx ends, and stops them all as soon as
// one returns: a listener that cannot bind (or dies) takes the process down
// rather than leaving half a service running. The errors are joined.
func serveAll(ctx context.Context, servers ...*http.Server) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, len(servers))
	for _, srv := range servers {
		go func() {
			err := httpx.Serve(ctx, srv, shutdownTimeout)
			cancel()
			errs <- err
		}()
	}
	var all []error
	for range servers {
		if err := <-errs; err != nil {
			all = append(all, err)
		}
	}
	return errors.Join(all...)
}
