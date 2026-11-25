package store

import (
	"encoding/json"
	"time"
)

// Analysis is one attempt at wording a team's assessment with the LLM: the
// view and closed weeks it covers, the assessment worded, the answer.
//
// Table: team_analyses
type Analysis struct {
	ID         int64
	Team       string
	Weeks      int
	Product    string // empty: all products
	PeriodFrom time.Time
	PeriodTo   time.Time
	Trigger    string // scheduled | manual
	Status     string // running | done | failed
	Model      string
	InputHash  *string
	Assessment json.RawMessage
	Output     json.RawMessage
	Report     json.RawMessage
	// InputTokens and OutputTokens as the endpoint counted them; nil when
	// it reported none.
	InputTokens  *int64
	OutputTokens *int64
	CostNanoUSD  *int64
	Error        *string
	Attempts     int
	CreatedAt    time.Time
	FinishedAt   *time.Time
	// RulesVersion is the assessment rules the row was assessed under
	// (metrics.RulesVersion); nil for a row from before the column, older
	// than any. A row of older rules is not shown as the assessment's words.
	RulesVersion *int
}

// Activity is how many events one sender identifier produced: a count per
// source, identifier and team over the four event tables.
//
// Table: -
type Activity struct {
	Source     string // bitbucket | pipeline | argocd | noergler
	Identifier string // repo_full_name / pipeline_name / app_name / repo
	Team       string
	Events     int64
}
