package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/thousandflowers/skillreaper/internal/usage"
)

// Payload-quality view — the second utilization axis.
//
// "reap gap" scores LOAD utilization (does a tool fire?). This scores PAYLOAD
// utilization (when it fires, is the result signal or noise?). A tool that fires
// often reads as healthy/KEEP under load utilization while quietly returning
// mostly base64, nav chrome, or boilerplate on every call. The evidence comes
// from the same transcripts: usage.Stats.MCPPayload, populated during parsing.

const (
	// payloadNoisyMinCalls is the minimum measured calls before a tool can be
	// flagged noisy. A single junk result is anecdote; a pattern needs repeats.
	payloadNoisyMinCalls = 3
	// payloadNoisyMaxQualityPct is the quality ceiling (percent useful content)
	// at or below which a frequently-firing tool is flagged "mostly noise".
	payloadNoisyMaxQualityPct = 40
)

// PayloadRow is one MCP tool's payload-quality score over the window.
type PayloadRow struct {
	Tool       string `json:"tool"`        // full "mcp__server__tool" key
	Server     string `json:"server"`      // the server segment of the key
	Calls      int    `json:"calls"`       // tool_result payloads measured
	TotalChars int    `json:"total_chars"` // summed payload bytes
	NoiseChars int    `json:"noise_chars"` // summed bytes classified as noise
	QualityPct int    `json:"quality_pct"` // useful*100/total (100 = all signal)
	Noisy      bool   `json:"noisy"`       // fires often AND mostly noise
}

// computePayload turns the per-tool payload accumulators into sorted, scored
// rows. Tools with no measured bytes are skipped (no evidence). Flagged-noisy
// tools lead (they fire often AND waste context, the actionable cases); then by
// worst quality, most calls, and tool name — so a single 0%-quality anecdote
// never outranks a genuine high-volume offender.
func computePayload(st *usage.Stats) []PayloadRow {
	if st == nil || len(st.MCPPayload) == 0 {
		return nil
	}
	rows := make([]PayloadRow, 0, len(st.MCPPayload))
	for tool, p := range st.MCPPayload {
		if p.TotalChars == 0 {
			continue
		}
		useful := p.TotalChars - p.NoiseChars
		if useful < 0 {
			useful = 0
		}
		quality := useful * 100 / p.TotalChars
		rows = append(rows, PayloadRow{
			Tool:       tool,
			Server:     payloadServer(tool),
			Calls:      p.Calls,
			TotalChars: p.TotalChars,
			NoiseChars: p.NoiseChars,
			QualityPct: quality,
			Noisy:      p.Calls >= payloadNoisyMinCalls && quality <= payloadNoisyMaxQualityPct,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Noisy != rows[j].Noisy {
			return rows[i].Noisy // flagged-noisy tools lead
		}
		if rows[i].QualityPct != rows[j].QualityPct {
			return rows[i].QualityPct < rows[j].QualityPct
		}
		if rows[i].Calls != rows[j].Calls {
			return rows[i].Calls > rows[j].Calls
		}
		return rows[i].Tool < rows[j].Tool
	})
	return rows
}

// payloadServer extracts the server segment of an "mcp__server__tool" key.
func payloadServer(tool string) string {
	const p = "mcp__"
	if len(tool) <= len(p) || tool[:len(p)] != p {
		return tool
	}
	rest := tool[len(p):]
	for i := 0; i+1 < len(rest); i++ {
		if rest[i] == '_' && rest[i+1] == '_' {
			return rest[:i]
		}
	}
	return rest
}

// avgChars returns the mean payload size of a row, humanized (e.g. "12.4k").
func avgChars(r PayloadRow) string {
	if r.Calls == 0 {
		return "0"
	}
	return humanChars(r.TotalChars / r.Calls)
}

// humanChars formats a byte count compactly: 1234 → "1.2k", 980 → "980".
func humanChars(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// renderPayloadQuality appends the payload-quality section to the gap report.
// It is a no-op when there is no MCP payload evidence.
func renderPayloadQuality(w io.Writer, r *Report, paint func(code, s string) string) {
	if len(r.MCPPayload) == 0 {
		return
	}
	fmt.Fprintf(w, "\n  %s\n", paint(cBold, "⟡ payload quality — when an MCP tool fires, is the result signal or noise?"))
	fmt.Fprintf(w, "  %s\n\n", paint(cDim, "high firing rate + low quality = context burned every call (not caught by mute)"))

	fmt.Fprintf(w, "  %-40s %6s %7s   %-12s %10s\n", "TOOL", "CALLS", "QUALITY", "", "AVG")
	for _, pr := range r.MCPPayload {
		tag := ""
		if pr.Noisy {
			tag = "  " + paint(cBYell, "⚑ noisy")
		}
		line := fmt.Sprintf("  %-40s %6d %6d%%   %-12s ~%9s",
			truncate(pr.Tool, 40), pr.Calls, pr.QualityPct, utilBar(pr.QualityPct), avgChars(pr))
		fmt.Fprintf(w, "%s%s\n", paint(utilColor(pr.QualityPct), line), tag)
	}
}

// renderPayloadMarkdown appends the payload-quality table to the gap Markdown.
func renderPayloadMarkdown(w io.Writer, r *Report) {
	if len(r.MCPPayload) == 0 {
		return
	}
	fmt.Fprintf(w, "\n## payload quality (MCP)\n\n")
	fmt.Fprintln(w, "| Tool | Calls | Quality | Avg chars | Noisy |")
	fmt.Fprintln(w, "|---|---|---|---|---|")
	for _, pr := range r.MCPPayload {
		noisy := ""
		if pr.Noisy {
			noisy = "yes"
		}
		fmt.Fprintf(w, "| %s | %d | %d%% | ~%s | %s |\n",
			pr.Tool, pr.Calls, pr.QualityPct, avgChars(pr), noisy)
	}
}
