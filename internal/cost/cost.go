// Package cost estimates token counts and money from character counts.
//
// The token estimate is intentionally simple: English prose averages
// ~3.7 characters per token across modern BPE tokenizers. This tool
// compares relative weights, so a documented approximation beats a
// tokenizer dependency.
package cost

// ModelPricing maps known model IDs to input price per million tokens.
// When a provider releases a new model, just add its pricing here
// instead of changing core logic.
var ModelPricing = map[string]float64{
	// Claude — current (June 2026)
	"claude-opus-4-8":   5.0,
	"claude-opus-4-7":   5.0,
	"claude-opus-4-6":   5.0,
	"claude-opus-4-5":   5.0,
	"claude-sonnet-4-6": 3.0,
	"claude-sonnet-4-5": 3.0,
	"claude-haiku-4-5":  1.0,
	"claude-3-5-sonnet": 3.0,

	// Claude — frontier
	"claude-fable-5":   10.0,
	"claude-mythos-5":  10.0,

	// OpenAI
	"gpt-4o":      2.50,
	"gpt-4o-mini": 0.15,
	"o3-mini":     1.10,
}

// DefaultModel is the pricing fallback when no --price or --model
// flag is given.
const DefaultModel = "claude-sonnet-4-6"

// LookupPrice returns the per-MTok input price for a model ID.
// The second result is false when the model is unknown.
func LookupPrice(modelID string) (float64, bool) {
	p, ok := ModelPricing[modelID]
	return p, ok
}

// CharsPerToken is the documented estimation ratio (x10 to stay integer).
const charsPerTokenX10 = 37

// Tokens estimates the token count for a number of characters,
// rounding up: ceil(chars / 3.7).
func Tokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars*10 + charsPerTokenX10 - 1) / charsPerTokenX10
}

// MoneyPerMonth estimates the monthly dollar cost of dead-weight tokens:
// tokens per session times sessions per month at a given price per
// million input tokens.
func MoneyPerMonth(tokensPerSession, sessionsPerMonth int, pricePerMTok float64) float64 {
	return float64(tokensPerSession) * float64(sessionsPerMonth) * pricePerMTok / 1e6
}
