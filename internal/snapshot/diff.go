package snapshot

import (
	"sort"
	"time"

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
	Category string `json:"category"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	From     string `json:"from,omitempty"` // verdict, for VerdictMoved
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

type key struct {
	category scan.Category
	name     string
}

func (k key) String() string { return string(k.category) + ":" + k.name }

func index(r *report.Report) map[key]report.Row {
	m := make(map[key]report.Row, len(r.Rows))
	for _, row := range r.Rows {
		m[key{row.Category, row.Name}] = row
	}
	return m
}

func counts(r *report.Report) (loaded, fired int) {
	loaded = len(r.Rows)
	for _, row := range r.Rows {
		if row.Uses > 0 {
			fired++
		}
	}
	return loaded, fired
}

// PrunedKeys reads the prune manifest and returns the items this tool has
// quarantined and not restored. A restored entry is deliberately excluded:
// restoring is the user putting it back on purpose, and reporting that as a
// surprise return would train them to ignore the line that matters.
func PrunedKeys(claudeDir string) map[string]bool {
	out := map[string]bool{}
	entries, err := prune.LoadManifest(claudeDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.Restored {
			out[e.Category+":"+e.Name] = true
		}
	}
	return out
}

// Compare diffs two snapshots. pruned may be nil.
func Compare(from, to *report.Report, fromPath, toPath string, pruned map[string]bool) Diff {
	a, b := index(from), index(to)
	d := Diff{
		FromPath:       fromPath,
		ToPath:         toPath,
		FromTaken:      from.GeneratedAt,
		ToTaken:        to.GeneratedAt,
		DeadTokensFrom: from.DeadTokensPerSession,
		DeadTokensTo:   to.DeadTokensPerSession,
	}
	d.LoadedFrom, d.FiredFrom = counts(from)
	d.LoadedTo, d.FiredTo = counts(to)

	for k, rowB := range b {
		rowA, ok := a[k]
		if !ok {
			d.Changes = append(d.Changes, Change{
				Category: string(k.category), Name: k.name, Kind: Appeared,
				To:       rowB.Verdict,
				Returned: pruned[k.String()],
			})
			continue
		}
		if rowA.Verdict != rowB.Verdict {
			d.Changes = append(d.Changes, Change{
				Category: string(k.category), Name: k.name, Kind: VerdictMoved,
				From: rowA.Verdict, To: rowB.Verdict,
			})
		}
	}
	for k, rowA := range a {
		if _, ok := b[k]; !ok {
			d.Changes = append(d.Changes, Change{
				Category: string(k.category), Name: k.name, Kind: Disappeared,
				From: rowA.Verdict,
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
		return x.Name < y.Name
	})
	return d
}
