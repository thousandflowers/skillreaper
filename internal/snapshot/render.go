package snapshot

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// RenderJSON writes the diff as JSON.
func RenderJSON(w io.Writer, d Diff) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(d)
}

func signed(n int) string {
	if n > 0 {
		return fmt.Sprintf("+%d", n)
	}
	return fmt.Sprintf("%d", n)
}

// RenderText writes the human-readable diff.
//
// Returns lead, always, and are stated even when nothing else changed: an item
// coming back after a prune is the one finding here that is acted on rather
// than read, and it is invisible in the report a run prints today.
func RenderText(w io.Writer, d Diff) {
	fmt.Fprintf(w, "\n  %s → %s\n\n",
		strings.TrimSuffix(filepath.Base(d.FromPath), ".json"),
		strings.TrimSuffix(filepath.Base(d.ToPath), ".json"))

	if back := d.Returned(); len(back) > 0 {
		fmt.Fprintf(w, "  !  %d %s back after a prune\n", len(back), plural(len(back), "item"))
		for _, c := range back {
			fmt.Fprintf(w, "       %-6s %s\n", c.Category, c.Name)
		}
		fmt.Fprintln(w)
	}

	var appeared, disappeared, moved []Change
	for _, c := range d.Changes {
		switch c.Kind {
		case Appeared:
			appeared = append(appeared, c)
		case Disappeared:
			disappeared = append(disappeared, c)
		case VerdictMoved:
			moved = append(moved, c)
		}
	}
	section(w, "appeared", appeared, func(c Change) string { return c.To })
	section(w, "disappeared", disappeared, func(c Change) string { return c.From })
	section(w, "verdict changed", moved, func(c Change) string { return c.From + " → " + c.To })

	if len(d.Changes) == 0 {
		fmt.Fprintf(w, "  no items appeared, left, or changed verdict\n\n")
	}

	fmt.Fprintf(w, "  dead tokens   %d → %d  (%s)\n",
		d.DeadTokensFrom, d.DeadTokensTo, signed(d.DeadTokenDelta()))
	fmt.Fprintf(w, "  utilization   %.0f%% → %.0f%%  (%d/%d → %d/%d fired)\n\n",
		d.UtilizationFrom()*100, d.UtilizationTo()*100,
		d.FiredFrom, d.LoadedFrom, d.FiredTo, d.LoadedTo)
}

func section(w io.Writer, title string, cs []Change, detail func(Change) string) {
	if len(cs) == 0 {
		return
	}
	fmt.Fprintf(w, "  %s (%d)\n", title, len(cs))
	for _, c := range cs {
		fmt.Fprintf(w, "       %-6s %-28s %s\n", c.Category, c.Name, detail(c))
	}
	fmt.Fprintln(w)
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
