package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// AnalysisView is the page view an analysis words: team, window and
// product, as the read API takes them.
type AnalysisView struct {
	Team    string
	Weeks   int
	Product string
}

// AnalysisClaim starts an attempt.
type AnalysisClaim struct {
	View     AnalysisView
	From, To time.Time
	Trigger  string
	Model    string
	// At is the attempt's start by the caller's clock, the one its cooldown
	// and staleness are judged by.
	At time.Time
	// MaxAttempts and StaleAfter let a scheduled run be claimed again, up
	// to MaxAttempts tries in all: a failed one, or a running one abandoned
	// past StaleAfter (its pod died, maybe of the run itself).
	MaxAttempts int
	StaleAfter  time.Duration
}

// AnalysisResult ends an attempt, done or failed; what of it is known.
type AnalysisResult struct {
	InputHash    *string
	Assessment   json.RawMessage
	Output       json.RawMessage
	Report       json.RawMessage
	InputTokens  *int64
	OutputTokens *int64
	CostNanoUSD  *int64
	Error        *string
	// At is the end by the caller's clock.
	At time.Time
}

const analysisColumns = `id, team, weeks, product, period_from, period_to, trigger, status, model,
	input_hash, assessment, output, report, input_tokens, output_tokens, cost_nano_usd, error, attempts,
	created_at, finished_at`

func scanAnalysis(r pgx.CollectableRow) (Analysis, error) {
	var a Analysis
	err := r.Scan(&a.ID, &a.Team, &a.Weeks, &a.Product, &a.PeriodFrom, &a.PeriodTo, &a.Trigger,
		&a.Status, &a.Model, &a.InputHash, &a.Assessment, &a.Output, &a.Report, &a.InputTokens, &a.OutputTokens,
		&a.CostNanoUSD, &a.Error, &a.Attempts, &a.CreatedAt, &a.FinishedAt)
	return a, err
}

// ClaimAnalysis starts an attempt and returns its id. A scheduled one is
// one per view and closed week, whatever the replicas: ok is false when
// another has it, is done, or failed MaxAttempts times.
func (s *Store) ClaimAnalysis(ctx context.Context, c AnalysisClaim) (id int64, ok bool, err error) {
	v := c.View
	if c.Trigger != "scheduled" {
		err = s.pool.QueryRow(ctx, `INSERT INTO team_analyses
			(team, weeks, product, period_from, period_to, trigger, status, model, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, 'running', $7, $8) RETURNING id`,
			v.Team, v.Weeks, v.Product, c.From, c.To, c.Trigger, c.Model, c.At).Scan(&id)
		return id, err == nil, err
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO team_analyses
		(team, weeks, product, period_from, period_to, trigger, status, model, created_at)
		VALUES ($1, $2, $3, $4, $5, 'scheduled', 'running', $6, $9)
		ON CONFLICT (team, weeks, product, period_to) WHERE trigger = 'scheduled'
		DO UPDATE SET status = 'running', attempts = team_analyses.attempts + 1, model = EXCLUDED.model,
			created_at = EXCLUDED.created_at, finished_at = NULL, error = NULL
		WHERE team_analyses.attempts < $7 AND (team_analyses.status = 'failed'
		   OR (team_analyses.status = 'running' AND team_analyses.created_at < $9 - $8 * interval '1 second'))
		RETURNING id`,
		v.Team, v.Weeks, v.Product, c.From, c.To, c.Model, c.MaxAttempts, c.StaleAfter.Seconds(), c.At).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// FinishAnalysis ends attempt id as done.
func (s *Store) FinishAnalysis(ctx context.Context, id int64, r AnalysisResult) error {
	return s.endAnalysis(ctx, id, "done", r)
}

// FailAnalysis ends attempt id as failed, with what it got before failing.
func (s *Store) FailAnalysis(ctx context.Context, id int64, r AnalysisResult) error {
	return s.endAnalysis(ctx, id, "failed", r)
}

func (s *Store) endAnalysis(ctx context.Context, id int64, status string, r AnalysisResult) error {
	raw := func(m json.RawMessage) any {
		if len(m) == 0 {
			return nil
		}
		return string(m)
	}
	_, err := s.pool.Exec(ctx, `UPDATE team_analyses SET status = $2, input_hash = $3, assessment = $4::jsonb,
		output = $5::jsonb, report = $6::jsonb, input_tokens = $7, output_tokens = $8, cost_nano_usd = $9,
		error = $10, finished_at = $11 WHERE id = $1`,
		id, status, r.InputHash, raw(r.Assessment), raw(r.Output), raw(r.Report), r.InputTokens, r.OutputTokens,
		r.CostNanoUSD, r.Error, r.At)
	return err
}

// DoneAnalyses are the view's done analyses, the latest closed weeks first,
// at most limit.
func (s *Store) DoneAnalyses(ctx context.Context, v AnalysisView, limit int) ([]Analysis, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+analysisColumns+` FROM team_analyses
		WHERE team = $1 AND weeks = $2 AND product = $3 AND status = 'done'
		ORDER BY period_to DESC, finished_at DESC LIMIT $4`, v.Team, v.Weeks, v.Product, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAnalysis)
}

// ScheduledAttempt is the view's scheduled attempt for the closed weeks
// ending at to; nil without one.
func (s *Store) ScheduledAttempt(ctx context.Context, v AnalysisView, to time.Time) (*Analysis, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+analysisColumns+` FROM team_analyses
		WHERE team = $1 AND weeks = $2 AND product = $3 AND period_to = $4 AND trigger = 'scheduled'`,
		v.Team, v.Weeks, v.Product, to)
	if err != nil {
		return nil, err
	}
	return oneAnalysis(rows)
}

func oneAnalysis(rows pgx.Rows) (*Analysis, error) {
	a, err := pgx.CollectExactlyOneRow(rows, scanAnalysis)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// LatestAttempt is the view's newest attempt in any state; nil without one.
func (s *Store) LatestAttempt(ctx context.Context, v AnalysisView) (*Analysis, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+analysisColumns+` FROM team_analyses
		WHERE team = $1 AND weeks = $2 AND product = $3
		ORDER BY created_at DESC LIMIT 1`, v.Team, v.Weeks, v.Product)
	if err != nil {
		return nil, err
	}
	return oneAnalysis(rows)
}
