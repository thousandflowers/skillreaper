package snapshot

import (
	"slices"
	"sort"
	"time"

	"github.com/thousandflowers/skillreaper/internal/platform"
	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// Change kinds. A verdict change is reported only when the item is present on
// both sides: an item that arrived or left says something louder than the
// verdict it happens to carry.
const (
	Appeared     = "appeared"
	Disappeared  = "disappeared"
	VerdictMoved = "verdict"
)

// Change is one difference between two snapshots.
type Change struct {
	Platform string `json:"platform,omitempty"`
	Category string `json:"category"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Tokens   int    `json:"tokens,omitempty"` // the item's weight per session
	From     string `json:"from,omitempty"`   // verdict, for VerdictMoved
	To       string `json:"to,omitempty"`
	// Returned marks an item that is back after having been quarantined. It is
	// read from the prune manifest rather than inferred from the two snapshots:
	// an item can be absent for many reasons, and only the manifest knows this
	// tool is the one that removed it. This is the case the whole comparison
	// exists to catch, because a marketplace update reinstalls silently.
	Returned bool `json:"returned,omitempty"`
}

// Diff is the comparison of two snapshots, oldest first.
type Diff struct {
	FromPath  string    `json:"fromPath"`
	ToPath    string    `json:"toPath"`
	FromTaken time.Time `json:"fromTaken"`
	ToTaken   time.Time `json:"toTaken"`

	Changes []Change `json:"changes"`
	// Warnings says why the two may not be comparable.
	Warnings []string `json:"warnings,omitempty"`
	// MutedCount and MutedTokens are what went from counted to muted: the saving
	// dead tokens cannot show, since a muted item was often never dead.
	MutedCount  int `json:"mutedCount,omitempty"`
	MutedTokens int `json:"mutedTokens,omitempty"`

	DeadTokensFrom int `json:"deadTokensFrom"`
	DeadTokensTo   int `json:"deadTokensTo"`
	LoadedFrom     int `json:"loadedFrom"`
	LoadedTo       int `json:"loadedTo"`
	FiredFrom      int `json:"firedFrom"`
	FiredTo        int `json:"firedTo"`
}

// DeadTokenDelta is negative when the stack got lighter.
func (d Diff) DeadTokenDelta() int { return d.DeadTokensTo - d.DeadTokensFrom }

// Utilization is fired over loaded, the same ratio reap gap prints. Zero
// loaded yields zero rather than a division by zero: an empty stack has no
// utilization to report, and reporting 100% would read as perfect.
func utilization(fired, loaded int) float64 {
	if loaded == 0 {
		return 0
	}
	return float64(fired) / float64(loaded)
}

// percent rounds down, as reap gap does, so the two print one number.
func percent(fired, loaded int) int {
	if loaded == 0 {
		return 0
	}
	return fired * 100 / loaded
}

// UtilizationFrom and UtilizationTo are fractions in [0,1].
func (d Diff) UtilizationFrom() float64 { return utilization(d.FiredFrom, d.LoadedFrom) }
func (d Diff) UtilizationTo() float64   { return utilization(d.FiredTo, d.LoadedTo) }

// Returned lists only the items that came back after a prune.
func (d Diff) Returned() []Change {
	var out []Change
	for _, c := range d.Changes {
		if c.Returned {
			out = append(out, c)
		}
	}
	return out
}

// key is one item. Source is in it because one name can come from two
// marketplaces on the same platform, and the path is not, because a plugin update
// changes the path of the same item.
type key struct {
	platform string
	category scan.Category
	name     string
	source   string
}

// platformOf treats a row or entry with no platform as Claude Code's: those date
// from before anything else was scanned.
func platformOf(p string) string {
	if p == "" {
		return string(platform.ClaudeCode)
	}
	return p
}

func prunedKey(plat, category, name string) string {
	return platformOf(plat) + "\x00" + category + "\x00" + name
}

func index(r *report.Report) map[key]report.Row {
	m := make(map[key]report.Row, len(r.Rows))
	for _, row := range r.Rows {
		m[key{platformOf(row.Platform), row.Category, row.Name, row.Source}] = row
	}
	return m
}

// gapCategories are what fire and what reap gap counts. Hooks and prose have no
// firing data, and counting them would put the two views apart.
var gapCategories = []scan.Category{scan.CatSkill, scan.CatMCP, scan.CatAgent}

func counts(r *report.Report) (loaded, fired int) {
	if r.Gap != nil {
		return r.Gap.Loaded, r.Gap.Fired
	}
	for _, row := range r.Rows {
		if !slices.Contains(gapCategories, row.Category) {
			continue
		}
		loaded++
		if row.Uses > 0 {
			fired++
		}
	}
	return loaded, fired
}

// verdictLabel names the state of a row: a muted item has no verdict of its own.
func verdictLabel(row report.Row) string {
	if row.Reason == report.ReasonMuted {
		return report.ReasonMuted
	}
	return row.Verdict
}

// PrunedAt reads the prune manifest and returns when each item this tool has
// quarantined and not restored was pruned. A restored entry is deliberately
// excluded: restoring is the user putting it back on purpose, and reporting that
// as a surprise return would train them to ignore the line that matters.
func PrunedAt(claudeDir string) map[string]time.Time {
	out := map[string]time.Time{}
	entries, err := prune.LoadManifest(claudeDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.Restored {
			out[prunedKey(e.Platform, e.Category, e.Name)] = e.Timestamp
		}
	}
	return out
}

// Compare diffs two snapshots. pruned may be nil. An item is back after a prune
// only if the prune came before the later snapshot: one pruned since then had
// simply not been pruned yet when that snapshot saw it.
func Compare(from, to *report.Report, fromPath, toPath string, pruned map[string]time.Time) Diff {
	a, b := index(from), index(to)
	d := Diff{
		FromPath:       fromPath,
		ToPath:         toPath,
		FromTaken:      from.GeneratedAt,
		ToTaken:        to.GeneratedAt,
		DeadTokensFrom: from.DeadTokensPerSession,
		DeadTokensTo:   to.DeadTokensPerSession,
		Changes:        []Change{},
	}
	d.LoadedFrom, d.FiredFrom = counts(from)
	d.LoadedTo, d.FiredTo = counts(to)

	for k, rowB := range b {
		base := Change{Platform: k.platform, Category: string(k.category), Name: scan.SafeText(k.name), Tokens: rowB.Tokens}
		rowA, ok := a[k]
		if !ok {
			at, wasPruned := pruned[prunedKey(k.platform, string(k.category), k.name)]
			base.Kind, base.To = Appeared, verdictLabel(rowB)
			base.Returned = wasPruned && at.Before(to.GeneratedAt) && !from.GeneratedAt.After(to.GeneratedAt)
			d.Changes = append(d.Changes, base)
			continue
		}
		if verdictLabel(rowA) != verdictLabel(rowB) {
			base.Kind, base.From, base.To = VerdictMoved, verdictLabel(rowA), verdictLabel(rowB)
			base.Tokens = rowA.Tokens
			if base.To == report.ReasonMuted {
				d.MutedCount++
				d.MutedTokens += report.MuteSaving(rowA)
			}
			d.Changes = append(d.Changes, base)
		}
	}
	for k, rowA := range a {
		if _, ok := b[k]; !ok {
			d.Changes = append(d.Changes, Change{
				Platform: k.platform, Category: string(k.category), Name: scan.SafeText(k.name), Kind: Disappeared,
				From: verdictLabel(rowA), Tokens: rowA.Tokens,
			})
		}
	}

	// Map iteration is random and this output is compared between runs, so the
	// order is fixed here rather than left to whatever the runtime chose:
	// returns first, because that is the finding, then by category and name.
	sort.SliceStable(d.Changes, func(i, j int) bool {
		x, y := d.Changes[i], d.Changes[j]
		if x.Returned != y.Returned {
			return x.Returned
		}
		if x.Category != y.Category {
			return x.Category < y.Category
		}
		if x.Name != y.Name {
			return x.Name < y.Name
		}
		return x.Platform < y.Platform
	})
	return d
}
