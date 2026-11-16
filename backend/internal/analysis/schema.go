package analysis

import (
	_ "embed"
	"maps"
	"slices"

	"github.com/trick77/toolkit/llmwire"
)

//go:embed prompt.txt
var promptText string

// Length caps, enforced by the validator, not trusted to the model: short
// is the point of the page (user: "knackig, keine Textwand").
const (
	MaxHeadline = 70
	MaxTrend    = 160
	MaxTake     = 140
	MaxRetro    = 100
	MaxCI       = 140
	MaxQuestion = 120
	MaxFocus    = 3
	MaxCards    = 4
)

// SchemaName names the response format.
const SchemaName = "team_assessment"

// The caps stay out of the schema: strict schemas take no maxLength on
// every vendor, and the validator enforces them either way.
func str(int) map[string]any { return map[string]any{"type": "string"} }

func nullableStr(int) map[string]any { return map[string]any{"type": []any{"string", "null"}} }

// object requires every property, as a strict schema must.
func object(props map[string]any) map[string]any {
	req := make([]any, 0, len(props))
	for _, k := range slices.Sorted(maps.Keys(props)) {
		req = append(req, k)
	}
	return map[string]any{"type": "object", "additionalProperties": false, "required": req, "properties": props}
}

func textSchema() map[string]any {
	return object(map[string]any{
		"headline": str(MaxHeadline),
		"trend":    str(MaxTrend),
		"focus": map[string]any{"type": "array", "maxItems": MaxFocus, "items": object(map[string]any{
			"repo": map[string]any{"type": "string"}, "take": str(MaxTake), "retro": nullableStr(MaxRetro),
			"lever": map[string]any{"type": "string"},
		})},
		"cards": map[string]any{"type": "array", "maxItems": MaxCards, "items": object(map[string]any{
			"signal": map[string]any{"type": "string"}, "take": str(MaxTake), "retro": nullableStr(MaxRetro),
			"lever": map[string]any{"type": "string"},
		})},
		"ci":       str(MaxCI),
		"question": str(MaxQuestion),
	})
}

// responseFormat is the strict schema of Output.
func responseFormat() llmwire.ResponseFormat {
	return llmwire.ResponseFormat{
		Kind: llmwire.FormatJSONSchema, Name: SchemaName, Strict: true,
		Schema: object(map[string]any{"de": textSchema(), "en": textSchema()}),
	}
}
