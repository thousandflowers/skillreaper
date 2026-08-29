package report

import "fmt"

// Headline is the one place the report's opening figures are named.
//
// They were named in four: the boxed figures in the terminal report, the
// Markdown export, the agent block, and the README generator each wrote their
// own wording over the same two fields. That is how the same defect shipped in
// all four at once - every one of them printed DeadCount, which counts REAP
// verdicts, under the words "items never used", and the terminal report went
// further and appended ", of N loaded", so the page read "367 items never used,
// of 382 loaded" while the line under it read "13/382 items fired". A reader who
// subtracts finds 369, not 367, and concludes the tool cannot count.
//
// It can. The two numbers answer different questions: 369 items never fired,
// and of those, 367 are ones reap is willing to condemn. The other two it holds
// back - one on a platform that publishes no transcript, where absence of a use
// is not evidence of no use, and one referred to from CLAUDE.md. Refusing to
// condemn what it cannot see is the product's argument, so the figure that
// names it belongs on the front page rather than in a footnote.
//
// Everything above is derived here and only here, so the wording can be wrong
// in one place instead of four.
type Headline struct {
	Loaded     int
	Fired      int
	NeverFired int  // Loaded - Fired: the count "never fired" actually means
	Reaped     int  // DeadCount: the subset reap will condemn
	HeldBack   int  // never fired, not condemned
	KnowsFired bool // false when no gap was measured; then only Reaped is safe to state
}

// NewHeadline derives the opening figures from a finished report.
func NewHeadline(r *Report) Headline {
	h := Headline{Reaped: r.DeadCount}
	if r.Gap == nil || r.Gap.Loaded <= 0 {
		return h
	}
	h.Loaded, h.Fired = r.Gap.Loaded, r.Gap.Fired
	h.NeverFired = h.Loaded - h.Fired
	h.KnowsFired = true
	// Clamped, not trusted: the two counts come from different passes, and a
	// negative "held back" would be printed as a fact.
	if held := h.NeverFired - h.Reaped; held > 0 {
		h.HeldBack = held
	}
	return h
}

// Figure returns the count and its unit for the stacked figures at the top of
// the report. With no gap measured there is no honest "never fired" to state,
// so the verdict count is named as what it is instead.
func (h Headline) Figure() (val, unit string) {
	if !h.KnowsFired {
		return humanNum(h.Reaped), "items marked REAP"
	}
	return humanNum(h.NeverFired), fmt.Sprintf("items never fired, of %s loaded", humanNum(h.Loaded))
}

// Verdict is the line under the figures: how many of the never-fired reap will
// condemn, and how many it holds back.
func (h Headline) Verdict() string {
	if !h.KnowsFired {
		return ""
	}
	s := fmt.Sprintf("%s of them marked REAP", humanNum(h.Reaped))
	if h.HeldBack > 0 {
		s += fmt.Sprintf(", %d held back rather than condemned", h.HeldBack)
	}
	return s
}

// Line is the same two counts on one line, for the renderers with no room to
// stack them.
func (h Headline) Line() string {
	if !h.KnowsFired {
		return fmt.Sprintf("%d marked REAP", h.Reaped)
	}
	return fmt.Sprintf("%d never fired · %d marked REAP", h.NeverFired, h.Reaped)
}
