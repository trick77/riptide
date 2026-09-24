// Command riptide is the riptide collector.
//
//	riptide serve              run the collector (default)
//	riptide migrate            apply pending database migrations and exit
//	riptide version            print the version and exit
//	riptide onboard-bitbucket  create, update or remove the riptide webhook on Bitbucket repos
//	riptide check-onboarding   list which repos, pipelines and apps reported in the last hour
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/trick77/riptide/internal/api"
	"github.com/trick77/riptide/internal/buildinfo"
	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/httpapi"
	"github.com/trick77/riptide/internal/logging"
	"github.com/trick77/riptide/internal/onboarding"
	"github.com/trick77/riptide/internal/settings"
	"github.com/trick77/riptide/internal/store"
)

const usage = "usage: riptide [serve|migrate|version|onboard-bitbucket|check-onboarding]"

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
		fmt.Println(buildinfo.Version())
	case "onboard-bitbucket":
		os.Exit(onboarding.Main(args, os.Stdout, os.Stderr))
	case "check-onboarding":
		err = withSettings(func(s settings.Settings, log *slog.Logger) error {
			return checkOnboarding(s, log, os.Stdout)
		})
	case "-h", "--help", "help":
		fmt.Fprintln(os.Stderr, usage)
	default:
		fmt.Fprintf(os.Stderr, "riptide: unknown command %q\n%s\n", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("startup_aborted", "error", err.Error())
		os.Exit(1)
	}
}

// withSettings loads the environment (plus ./.env) and sets up logging
// before running fn: settings decide the log level and env.
func withSettings(fn func(settings.Settings, *slog.Logger) error) error {
	lookup, err := settings.EnvLookup(".env")
	if err != nil {
		logging.Setup("INFO", "dev")
		return err
	}
	s, err := settings.Load(lookup)
	if err != nil {
		logging.Setup("INFO", "dev")
		return err
	}
	return fn(s, logging.Setup(s.LogLevel, s.Env))
}

// migrate applies pending migrations and exits: the init container's job.
// Only the database URL is needed; the config files are not read.
func migrate(s settings.Settings, log *slog.Logger) error {
	log.Info("riptide-collector version: " + buildinfo.Version())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
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
	log.Info("riptide-collector version: " + buildinfo.Version())
	rt, err := config.Load(s.ConfigPath, s.TeamKeysPath, log)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
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

	srv := httpapi.New(log)
	api.Register(srv, api.Deps{Store: db, Runtime: rt, Log: log})

	cfg := rt.Config()
	log.Info("riptide_collector_starting",
		"teams", len(cfg.Teams),
		"keys", len(rt.Keys().TeamNames()),
		"production_stage", cfg.Environments.ProductionStage)
	if err := httpapi.Run(ctx, s.ListenAddr, srv.Handler(), log); err != nil {
		return err
	}
	log.Info("riptide_collector_stopped")
	return nil
}
