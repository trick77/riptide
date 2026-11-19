package api

import (
	"context"
	"net/http"
	"time"

	"github.com/trick77/ship/kit/logging"

	"github.com/trick77/riptide/internal/analysis"
	"github.com/trick77/riptide/internal/dataset"
	"github.com/trick77/riptide/internal/httpapi"
	"github.com/trick77/riptide/internal/metrics"
)

// AskHeader must be "1" on POST /api/analysis.
const AskHeader = "X-Riptide-Ask"

// Analyst serves a view's assessment with its analysis and starts a run.
type Analyst interface {
	Status(ctx context.Context, v analysis.View) (analysis.Status, error)
	Ask(ctx context.Context, v analysis.View) error
}

// Dataset is a team's dataset over a period, the config as it is now: the
// analysis' loader.
func (d Deps) Dataset(ctx context.Context, team string, p metrics.Period) (*dataset.Dataset, error) {
	return d.load(ctx, view{team: team, now: d.now().UTC().Truncate(time.Second), cfg: d.Runtime.Config()}, p.From, p.To, false)
}

// TeamNames are the configured teams, sorted: the schedule's.
func (d Deps) TeamNames() []string { return d.Runtime.Config().TeamNames() }

// analysisView is the read query as the analysis takes it. Its weeks are
// the analysis' calendar (BACKEND_ANALYSIS_TZ), not the viewer's: one
// stored analysis serves every viewer.
func analysisView(v view) analysis.View {
	return analysis.View{Team: v.team, Weeks: len(v.params.Period.Weeks), Product: v.params.Product}
}

// analysisStatus serves a view's assessment over its closed weeks and the
// analysis that words it, if any.
func (d Deps) analysisStatus(w http.ResponseWriter, r *http.Request) {
	if d.Analysis == nil {
		httpapi.WriteDetail(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))
		return
	}
	v, ok := d.readView(w, r)
	if !ok {
		return
	}
	st, err := d.Analysis.Status(r.Context(), analysisView(v))
	if viewerLeft(w, r, err) {
		return
	}
	if err != nil {
		d.Log.ErrorContext(r.Context(), "analysis_status_failed", "team", v.team, logging.Err(err))
		httpapi.WriteDetail(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, st)
}

// analysisAsk starts a manual run of the view and answers with its status;
// 409 when one is under way, the view is within its cooldown, the hour's
// manual runs are taken, or no model is configured; 422 for an unknown
// product; 500 when the store or loader fails.
func (d Deps) analysisAsk(w http.ResponseWriter, r *http.Request) {
	if d.Analysis == nil {
		httpapi.WriteDetail(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))
		return
	}
	// A paid call: only the page asks. A cross-site form or link cannot set
	// a header of its own without a CORS preflight, which this listener
	// never answers.
	if r.Header.Get(AskHeader) != "1" {
		httpapi.WriteDetail(w, http.StatusForbidden, AskHeader+": 1 required")
		return
	}
	v, ok := d.readView(w, r)
	if !ok {
		return
	}
	av := analysisView(v)
	err := d.Analysis.Ask(r.Context(), av)
	switch {
	case analysis.IsUnknownProduct(err):
		httpapi.WriteDetail(w, http.StatusUnprocessableEntity, "unknown product")
		return
	case analysis.IsBusy(err):
		httpapi.WriteDetail(w, http.StatusConflict, "an analysis of this view is under way or was just asked for")
		return
	case analysis.IsOff(err):
		httpapi.WriteDetail(w, http.StatusConflict, "no analysis model configured")
		return
	case err != nil:
		d.Log.ErrorContext(r.Context(), "analysis_ask_failed", "team", v.team, logging.Err(err))
		httpapi.WriteDetail(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	d.Log.InfoContext(r.Context(), "analysis_asked", "team", v.team, "weeks", av.Weeks, "product", av.Product)
	st, err := d.Analysis.Status(r.Context(), av)
	if viewerLeft(w, r, err) {
		return
	}
	if err != nil {
		d.Log.ErrorContext(r.Context(), "analysis_status_failed", "team", v.team, logging.Err(err))
		httpapi.WriteDetail(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	st.Running = true
	httpapi.WriteJSON(w, http.StatusAccepted, st)
}
