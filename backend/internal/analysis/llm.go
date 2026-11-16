package analysis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/trick77/toolkit/llmwire"
)

// Call is what one model call cost and returned.
type Call struct {
	Model        string
	Content      string
	FinishReason string
	InputTokens  *int64
	OutputTokens *int64
	// NanoUSD is nil when the call went unpriced: the gateway's figure
	// behind LiteLLM, llmwire's list price for a vendor called directly.
	NanoUSD *int64
}

// LLM answers one assessment prompt.
type LLM interface {
	Model() string
	Complete(ctx context.Context, system, user string) (Call, error)
}

// maxAnswerTokens caps the answer: two languages, a few short sentences
// each, JSON around them, about 2000 tokens, plus the thinking, which on
// MiMo varies from none to 7000 between calls on the same input (measured
// 2026-11-16: done at 1946..7016 output tokens, cut at 5024 and 9024
// under caps of 4000 and 8000). A cut answer is paid and lost; a longer
// cap costs only what is written, at most ~0.015 USD on MiMo, ~0.16 on
// gpt-6.1-sol.
const maxAnswerTokens = 16000

// callTimeout bounds one call: a reasoning model on a small prompt answers
// in a minute or two.
const callTimeout = 5 * time.Minute

// Wire is the llmwire LLM.
type Wire struct {
	client    *llmwire.Client
	model     string
	reasoning string
}

// NewWire builds the client for model from the environment (lookup reads
// LLMWIRE_* through the same .env as the settings), checks at boot that the
// model takes a strict JSON schema, and logs through log.
func NewWire(model, reasoning string, lookup func(string) (string, bool), log *slog.Logger,
	registry *llmwire.Registry) (*Wire, error) {
	cfg := llmwire.Config{Lookup: lookup, Logger: log, CallTimeout: callTimeout, Registry: registry}
	c, err := llmwire.FromEnv(model, cfg)
	if err != nil {
		return nil, fmt.Errorf("BACKEND_ANALYSIS_MODEL %q: %w", model, err)
	}
	if _, err := c.Registry().Require(model, llmwire.Needs{JSONSchema: true, Streaming: true}); err != nil {
		return nil, fmt.Errorf("BACKEND_ANALYSIS_MODEL %q: %w", model, err)
	}
	return &Wire{client: c, model: model, reasoning: reasoning}, nil
}

// Model is the profile id asked.
func (w *Wire) Model() string { return w.model }

// Complete streams one call to its end: a gateway cuts a call that sends no
// byte for minutes, and a reasoning model writes nothing until it has
// thought. Never retried (llmwire's rule): the scheduler tries next hour.
func (w *Wire) Complete(ctx context.Context, system, user string) (Call, error) {
	answer := maxAnswerTokens
	reasoning := llmwire.ReasoningBalanced()
	if w.reasoning != "" {
		reasoning = llmwire.ReasoningEffort(w.reasoning)
	}
	stream, _, err := w.client.ChatStream(ctx, llmwire.ChatRequest{
		Model:           w.model,
		Messages:        []llmwire.Message{llmwire.System(system), llmwire.User(user)},
		Reasoning:       reasoning,
		MaxAnswerTokens: &answer,
		ResponseFormat:  responseFormat(),
	})
	if err != nil {
		return Call{Model: w.model}, err
	}
	defer func() { _ = stream.Close() }()
	res, err := stream.Collect(nil)
	call := Call{
		Model: w.model, Content: res.Content, FinishReason: res.FinishReason,
		InputTokens: res.Usage.Input.Total, OutputTokens: res.Usage.Output.Total,
	}
	if res.Usage.Cost.Provenance != llmwire.Unpriced {
		n := res.Usage.Cost.NanoUSD
		call.NanoUSD = &n
	}
	return call, err
}
