// Package cost estimates token counts and money from character counts.
//
// The token estimate is intentionally simple: English prose averages
// ~3.7 characters per token across modern BPE tokenizers. This tool
// compares relative weights, so a documented approximation beats a
// tokenizer dependency.
package cost

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
