package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"

	"github.com/trick77/riptide/internal/analysis"
	"github.com/trick77/riptide/internal/api"
	"github.com/trick77/riptide/internal/config"
	"github.com/trick77/riptide/internal/metrics"
	"github.com/trick77/riptide/internal/settings"
	"github.com/trick77/riptide/internal/store"
)

// analyse words one view from the shell, for tuning the prompt against a
// real import: -dry-run prints the model's input and calls nothing; else
// it runs a manual analysis, stores it as the button would, and prints it.
func analyse(s settings.Settings, log *slog.Logger, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("analyse", flag.ContinueOnError)
	team := fs.String("team", "", "the team (required)")
	weeks := fs.Int("weeks", 4, "the window: 4, 12 or 26")
	product := fs.String("product", "", "a product key; empty is all")
	dry := fs.Bool("dry-run", false, "print the model's input, call nothing")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *team == "" || !metrics.ValidWindow(*weeks) {
		return errors.New("analyse: -team is required, -weeks one of 4, 12, 26")
	}
	rt, err := config.Load(s.ConfigPath, s.TeamKeysPath, log)
	if err != nil {
		return err
	}
	ctx := context.Background()
	db, err := store.Open(ctx, s.DBURL, log)
	if err != nil {
		return err
	}
	defer db.Close()
	deps := api.Deps{Store: db, Reader: db, Runtime: rt, Log: log}
	svc := &analysis.Service{Store: db, Load: deps.Datasets, Loc: s.AnalysisLoc, Log: log, Thresholds: deps.Thresholds}
	v := analysis.View{Team: *team, Weeks: *weeks, Product: *product}
	if *dry {
		digest, err := svc.Digest(ctx, v)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(digest))
		return err
	}
	if s.AnalysisModel == "" {
		return errors.New("analyse: BACKEND_ANALYSIS_MODEL is not set")
	}
	if svc.LLM, err = analysis.NewWire(s.AnalysisModel, s.AnalysisReasoning, s.Lookup, log, nil); err != nil {
		return err
	}
	svc.Start(ctx)
	if err := svc.Run(ctx, v, analysis.TriggerManual); err != nil {
		return err
	}
	st, err := svc.Status(ctx, v)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(struct {
		State    string          `json:"state"`
		Analysis *analysis.Shown `json:"analysis"`
	}{st.State, st.Analysis}, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(b))
	return err
}
