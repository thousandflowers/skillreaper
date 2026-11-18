package reinvest

import (
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// bytesToTokens is reap's weight: bytes / 3.7, rounded up.
func bytesToTokens(n int) int { return (n*10 + 36) / 37 }

// reapTokens is what reap will report for this skill once installed: reap's own
// reading of the header (internal/scan), / 3.7 rounded up, so the two agree on
// every quirk of a header too.
func reapTokens(slug, doc string) int {
	_, chars := scan.SkillListing(slug, []byte(doc))
	return bytesToTokens(chars)
}
