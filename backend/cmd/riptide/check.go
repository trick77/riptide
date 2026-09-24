package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/trick77/riptide/internal/settings"
	"github.com/trick77/riptide/internal/store"
)

// errSourcesMissing makes check-onboarding exit 1 without a startup error.
var errSourcesMissing = errors.New("sources missing")

// checkOnboarding answers "did I wire it up correctly?": which repos,
// pipelines and apps reported recently. It fails when Bitbucket, the CI
// pipelines or Argo CD sent nothing at all in the window; noergler is
// optional and only listed.
func checkOnboarding(s settings.Settings, log *slog.Logger, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("check-onboarding", flag.ContinueOnError)
	team := fs.String("team", "", "only this team's events")
	since := fs.Duration("since", time.Hour, "how far back to look")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ctx := context.Background()
	db, err := store.Open(ctx, s.DBURL, slog.New(slog.DiscardHandler))
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.RecentActivity(ctx, time.Now().Add(-*since), *team)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.Source] = true
		_, _ = fmt.Fprintf(out, "%-10s %-12s %-50s %d events\n", r.Source, r.Team, r.Identifier, r.Events)
	}
	var missing []string
	for _, src := range []string{"bitbucket", "pipeline", "argocd"} {
		if !seen[src] {
			missing = append(missing, src)
		}
	}
	if len(missing) > 0 {
		_, _ = fmt.Fprintf(out, "\nNo events in the last %s from: %v\n", *since, missing)
		log.Debug("check_onboarding_missing", "sources", missing)
		return errSourcesMissing
	}
	_, _ = fmt.Fprintf(out, "\nBitbucket, pipeline and Argo CD all reporting.\n")
	return nil
}
