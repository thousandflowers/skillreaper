package reinvest

import (
	_ "embed"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Unicode NFKC, which the risk rules need to see "ｃｕｒｌ" as "curl". The Go
// standard library has no normalization and this module takes no dependency,
// so the tables are written out by data/gen.py from the prototype's own
// Python, and the algorithm is the one in the Unicode standard (UAX #15).

//go:embed data/nfkc.txt
var nfkcSource string

type runePair struct{ first, second rune }

type nfkcTables struct {
	decomposed map[rune][]rune   // full compatibility decomposition
	class      map[rune]uint8    // canonical combining class, when not 0
	composed   map[runePair]rune // primary composite of a pair
}

var (
	nfkcOnce sync.Once
	nfkcData nfkcTables
)

func loadNFKC() nfkcTables {
	nfkcOnce.Do(func() {
		t := nfkcTables{decomposed: map[rune][]rune{}, class: map[rune]uint8{}, composed: map[runePair]rune{}}
		for _, line := range strings.Split(nfkcSource, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 || fields[0] == "#" {
				continue
			}
			switch fields[0] {
			case "D":
				t.decomposed[hexRune(fields[1])] = hexRunes(fields[2:])
			case "C":
				n, _ := strconv.Atoi(fields[2])
				t.class[hexRune(fields[1])] = uint8(n)
			case "P":
				if len(fields) == 4 {
					t.composed[runePair{hexRune(fields[1]), hexRune(fields[2])}] = hexRune(fields[3])
				}
			}
		}
		nfkcData = t
	})
	return nfkcData
}

func hexRune(s string) rune {
	n, _ := strconv.ParseInt(s, 16, 32)
	return rune(n)
}

func hexRunes(fields []string) []rune {
	out := make([]rune, len(fields))
	for i, f := range fields {
		out[i] = hexRune(f)
	}
	return out
}

// Hangul syllables are composed and decomposed by arithmetic (Unicode 3.12).
const (
	hangulBase   = 0xac00
	leadBase     = 0x1100
	vowelBase    = 0x1161
	trailBase    = 0x11a7
	leadCount    = 19
	vowelCount   = 21
	trailCount   = 28
	hangulCount  = leadCount * vowelCount * trailCount
	vowelPerLead = vowelCount * trailCount
)

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// nfkc is unicodedata.normalize("NFKC", s).
func nfkc(s string) string {
	if isASCII(s) {
		return s
	}
	t := loadNFKC()
	return string(compose(t, reorder(t, decompose(t, s))))
}

func decompose(t nfkcTables, s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch index := r - hangulBase; {
		case index >= 0 && index < hangulCount:
			out = append(out, leadBase+index/vowelPerLead, vowelBase+(index%vowelPerLead)/trailCount)
			if trail := index % trailCount; trail != 0 {
				out = append(out, trailBase+trail)
			}
		case t.decomposed[r] != nil:
			out = append(out, t.decomposed[r]...)
		default:
			out = append(out, r)
		}
	}
	return out
}

// reorder sorts each run of combining marks by class, keeping equal ones in place.
func reorder(t nfkcTables, runes []rune) []rune {
	for start := 0; start < len(runes); {
		if t.class[runes[start]] == 0 {
			start++
			continue
		}
		end := start
		for end < len(runes) && t.class[runes[end]] != 0 {
			end++
		}
		run := runes[start:end]
		sort.SliceStable(run, func(i, j int) bool { return t.class[run[i]] < t.class[run[j]] })
		start = end
	}
	return runes
}

func composePair(t nfkcTables, first, second rune) (rune, bool) {
	if lead, vowel := first-leadBase, second-vowelBase; lead >= 0 && lead < leadCount && vowel >= 0 && vowel < vowelCount {
		return hangulBase + (lead*vowelCount+vowel)*trailCount, true
	}
	if index, trail := first-hangulBase, second-trailBase; index >= 0 && index < hangulCount &&
		index%trailCount == 0 && trail > 0 && trail < trailCount {
		return first + trail, true
	}
	r, ok := t.composed[runePair{first, second}]
	return r, ok
}

const blockedClass = 256 // above every real class: nothing composes across it

// compose joins each starter with the marks after it that are not blocked.
func compose(t nfkcTables, runes []rune) []rune {
	if len(runes) == 0 {
		return runes
	}
	starterAt, out := 0, 1
	lastClass := int(t.class[runes[0]])
	if lastClass != 0 {
		lastClass = blockedClass // a text starting with a mark has no starter to join
	}
	for _, r := range runes[1:] {
		class := int(t.class[r])
		joined, ok := composePair(t, runes[starterAt], r)
		if ok && (lastClass < class || lastClass == 0) {
			runes[starterAt] = joined
			continue
		}
		if class == 0 {
			starterAt = out
		}
		lastClass = class
		runes[out] = r
		out++
	}
	return runes[:out]
}
