package reinvest

import (
	_ "embed"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// str.lower() the way Python does it. Two letters need more than Go's
// unicode.ToLower: İ becomes "i" plus a combining dot, and a capital sigma
// becomes ς at the end of a word and σ elsewhere. Which characters end a
// word is Unicode's "cased" and "case-ignorable", which the standard library
// does not carry: data/gen.py reads both off the prototype's own Python.

//go:embed data/casing.txt
var casingSource string

type runeRange struct{ lo, hi rune }

type casingTables struct {
	ignorable []runeRange // skipped when looking for the letters around a sigma
	cased     []runeRange // letters with a case, among the rest
}

var (
	casingOnce sync.Once
	casingData casingTables
)

func loadCasing() casingTables {
	casingOnce.Do(func() {
		for _, line := range strings.Split(casingSource, "\n") {
			fields := strings.Fields(line)
			if len(fields) != 3 {
				continue
			}
			span := runeRange{hexRune(fields[1]), hexRune(fields[2])}
			switch fields[0] {
			case "I":
				casingData.ignorable = append(casingData.ignorable, span)
			case "K":
				casingData.cased = append(casingData.cased, span)
			}
		}
	})
	return casingData
}

func inRanges(ranges []runeRange, r rune) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].hi >= r })
	return i < len(ranges) && ranges[i].lo <= r
}

const (
	dottedCapitalI = 0x130 // the one letter whose lower case is two code points
	capitalSigma   = 0x3a3
	smallSigma     = 0x3c3
	finalSigma     = 0x3c2
)

// sigmaAt picks the small sigma for the capital one at runes[at]: the final
// form when a cased letter comes before it and none comes after, ignorable
// characters aside.
func sigmaAt(runes []rune, at int) rune {
	t := loadCasing()
	before := at - 1
	for before >= 0 && inRanges(t.ignorable, runes[before]) {
		before--
	}
	if before < 0 || !inRanges(t.cased, runes[before]) {
		return smallSigma
	}
	after := at + 1
	for after < len(runes) && inRanges(t.ignorable, runes[after]) {
		after++
	}
	if after < len(runes) && inRanges(t.cased, runes[after]) {
		return smallSigma
	}
	return finalSigma
}

// pyLower is str.lower().
func pyLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var runes []rune // only made when a capital sigma needs its neighbours
	at := 0
	for _, r := range s {
		switch r {
		case dottedCapitalI:
			b.WriteString("i̇")
		case capitalSigma:
			if runes == nil {
				runes = []rune(s)
			}
			b.WriteRune(sigmaAt(runes, at))
		default:
			b.WriteRune(unicode.ToLower(r))
		}
		at++
	}
	return b.String()
}
