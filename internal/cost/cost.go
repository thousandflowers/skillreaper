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
	// Claude — verified against the Anthropic pricing documentation, 2026-08-25.
	"claude-fable-5":  10.0,
	"claude-mythos-5": 10.0,
	"claude-opus-5":   5.0,
	"claude-opus-4-8": 5.0,
	"claude-opus-4-7": 5.0,
	"claude-opus-4-6": 5.0,
	"claude-opus-4-5": 5.0,
	// Sonnet 5 lists at $3.00; an introductory $2.00 rate runs to 2026-08-31.
	// The list price is the one that survives, so it is the one recorded here.
	"claude-sonnet-5":   3.0,
	"claude-sonnet-4-6": 3.0,
	"claude-sonnet-4-5": 3.0,
	"claude-haiku-4-5":  1.0,
	"claude-3-5-sonnet": 3.0,

	// OpenAI — reached through Codex and opencode.
	"gpt-5":       1.25,
	"gpt-5-mini":  0.25,
	"gpt-4o":      2.50,
	"gpt-4o-mini": 0.15,
	"o3":          2.0,
	"o4-mini":     1.10,
	"o3-mini":     1.10,

	// Google Gemini — reached through gemini-cli. Base tier.
	"gemini-2.5-pro":         1.25,
	"gemini-2.5-flash":       0.30,
	"gemini-3-pro-preview":   2.0,
	"gemini-3-flash-preview": 0.50,
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

// TokenRatios maps model IDs to their average characters per token (x10 to
// stay integer). Each entry overrides the default 3.7 ratio for models whose
// tokenizer packs characters differently. Models absent from this map use
// the default ratio. Keep this in sync with ModelPricing: when adding a
// model whose tokenizer differs from the default, add its ratio here too.
var TokenRatios = map[string]int{
	// OpenAI's o200k_base tokenizer averages ~4.0 characters per token for
	// English prose, slightly more than the ~3.7 default. OpenAI documents
	// the rule of thumb as "1 token ~= 4 chars in English":
	// https://platform.openai.com/tokenizer
	"gpt-5":       40,
	"gpt-5-mini":  40,
	"gpt-4o":      40,
	"gpt-4o-mini": 40,
	"o3":          40,
	"o4-mini":     40,
	"o3-mini":     40,
}

// Tokens estimates the token count for a number of characters using the
// default ratio, rounding up: ceil(chars / 3.7).
func Tokens(chars int) int {
	return tokensWithRatio(chars, charsPerTokenX10)
}

// TokensFor estimates the token count for a number of characters using the
// ratio for the given model ID, falling back to the default 3.7 ratio when
// the model is empty or has no entry in TokenRatios.
func TokensFor(modelID string, chars int) int {
	ratio, ok := TokenRatios[modelID]
	if !ok {
		ratio = charsPerTokenX10
	}
	return tokensWithRatio(chars, ratio)
}

func tokensWithRatio(chars, ratioX10 int) int {
	if chars <= 0 {
		return 0
	}
	return (chars*10 + ratioX10 - 1) / ratioX10
}

// MoneyPerMonth estimates the monthly dollar cost of dead-weight tokens:
// tokens per session times sessions per month at a given price per
// million input tokens.
func MoneyPerMonth(tokensPerSession, sessionsPerMonth int, pricePerMTok float64) float64 {
	return float64(tokensPerSession) * float64(sessionsPerMonth) * pricePerMTok / 1e6
}
