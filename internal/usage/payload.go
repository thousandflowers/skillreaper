package usage

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Payload-quality scoring — a second utilization axis.
//
// Load utilization ("reap gap") asks "does a tool fire?". Payload utilization
// asks the orthogonal question "when it fires, does it return signal or noise?".
// A fetch/read tool can fire 50×/day (so it reads KEEP/green) while every result
// is mostly navigation chrome, boilerplate, or a base64 blob — context burned on
// every call. This file classifies a tool_result body into useful-vs-noise chars
// with a deliberately simple, provider-agnostic shape heuristic.
//
// The heuristic is approximate by design. It marks bytes that match well-known
// noise shapes (long base64 runs, data URIs, HTML tag soup, and lines repeated
// verbatim) onto a single mask, so overlapping matches are never double-counted
// and the noise count is naturally bounded by the payload length. It is a
// content-shape signal, not a semantic judgement, and is computed entirely from
// transcript data skillreaper already parses.

// payloadNoiseMinRun is the shortest base64 run treated as a blob. Short
// alphanumeric tokens (hashes, IDs) are common in legitimate output, so the
// threshold is high enough to avoid flagging them.
const payloadNoiseMinRun = 80

// payloadRepeatedLineMin is the shortest trimmed line length eligible for the
// repeated-line noise rule. Short repeats (table borders, "  ", "}") are normal.
const payloadRepeatedLineMin = 8

var (
	// A contiguous base64 run: payloadNoiseMinRun+ base64 chars with optional padding.
	reBase64 = regexp.MustCompile(`[A-Za-z0-9+/]{80,}={0,2}`)
	// A data: URI carrying inline base64 (images, fonts, etc.).
	reDataURI = regexp.MustCompile(`data:[^,\s]+;base64,[A-Za-z0-9+/=]+`)
	// An HTML/XML tag. Bounded length so a stray "<" in prose is not greedily
	// matched across the rest of the payload.
	reHTMLTag = regexp.MustCompile(`<[^>]{1,200}>`)
)

// resultContentBlock is the subset of a tool_result content array element we
// score. Claude tool results are either a JSON string or an array of blocks.
type resultContentBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Source struct {
		Data string `json:"data"`
	} `json:"source"`
}

// payloadParts unwraps tool_result content into the human-readable text it
// carries and the number of inline base64 bytes (image/binary blocks). It
// tolerates a bare JSON string, an array of typed blocks, or unknown shapes
// (returned verbatim as text). Empty or malformed input yields ("", 0).
func payloadParts(raw json.RawMessage) (text string, base64Bytes int) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return "", 0
	}
	// Case 1: a JSON string.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, 0
	}
	// Case 2: an array of content blocks.
	var blocks []resultContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Text != "" {
				sb.WriteString(b.Text)
			}
			// image / binary blocks are inline base64 — pure noise to a model
			// that asked for content, scored fully against the tool.
			base64Bytes += len(b.Source.Data)
		}
		return sb.String(), base64Bytes
	}
	// Case 3: unknown shape (object, number, …). Score the raw bytes as text so
	// a noisy unrecognised payload is not silently treated as clean.
	return string(raw), 0
}

// classifyPayload returns the total and noise byte counts for one tool_result.
// Inline base64 bytes count fully as noise; text bytes are scored by noiseChars.
func classifyPayload(raw json.RawMessage) (total, noise int) {
	text, b64 := payloadParts(raw)
	total = len(text) + b64
	noise = noiseChars(text) + b64
	if noise > total {
		noise = total
	}
	return total, noise
}

// noiseChars counts the bytes of s that match a known noise shape. It builds a
// single boolean mask so overlapping matches (e.g. a data URI that also matches
// the base64 rule) are counted once.
func noiseChars(s string) int {
	if s == "" {
		return 0
	}
	mask := make([]bool, len(s))
	markRanges(mask, reDataURI.FindAllStringIndex(s, -1))
	markRanges(mask, reBase64.FindAllStringIndex(s, -1))
	markRanges(mask, reHTMLTag.FindAllStringIndex(s, -1))
	markRepeatedLines(mask, s)

	n := 0
	for _, m := range mask {
		if m {
			n++
		}
	}
	return n
}

// markRanges flips mask[lo:hi] true for every [lo,hi] match index pair.
func markRanges(mask []bool, ranges [][]int) {
	for _, r := range ranges {
		lo, hi := r[0], r[1]
		if lo < 0 {
			lo = 0
		}
		if hi > len(mask) {
			hi = len(mask)
		}
		for i := lo; i < hi; i++ {
			mask[i] = true
		}
	}
}

// markRepeatedLines marks every occurrence after the first of any non-empty,
// sufficiently long line that appears verbatim more than once — the signature
// of nav bars, footers, and boilerplate repeated across a payload.
func markRepeatedLines(mask []bool, s string) {
	seen := map[string]bool{}
	for _, line := range splitKeepOffset(s) {
		trimmed := strings.TrimSpace(line.text)
		if len(trimmed) < payloadRepeatedLineMin {
			continue
		}
		if seen[trimmed] {
			end := line.start + len(line.text)
			if end > len(mask) {
				end = len(mask)
			}
			for i := line.start; i < end; i++ {
				mask[i] = true
			}
		} else {
			seen[trimmed] = true
		}
	}
}

// lineSpan is one line with its byte offset into the original string.
type lineSpan struct {
	text  string
	start int
}

// splitKeepOffset splits s on '\n', preserving each line's start offset so
// callers can mask the original bytes. The trailing newline is not part of a line.
func splitKeepOffset(s string) []lineSpan {
	var out []lineSpan
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, lineSpan{text: s[start:i], start: start})
			start = i + 1
		}
	}
	out = append(out, lineSpan{text: s[start:], start: start})
	return out
}
