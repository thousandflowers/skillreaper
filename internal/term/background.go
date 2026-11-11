package term

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

const (
	backgroundQuery = "\x1b]11;?\a"
	lightFrom       = 0.5 // share of full brightness
)

var backgroundReply = regexp.MustCompile(`rgb:([0-9a-fA-F]+)/([0-9a-fA-F]+)/([0-9a-fA-F]+)`)

// parseBackground reads a terminal's answer to the background query. ok is false
// when reply is no such answer.
func parseBackground(reply []byte) (light, ok bool) {
	found := backgroundReply.FindSubmatch(reply)
	if found == nil {
		return false, false
	}
	var share [3]float64
	for i, part := range found[1:] {
		if len(part) > 8 {
			return false, false
		}
		v, err := strconv.ParseUint(string(part), 16, 64)
		if err != nil {
			return false, false
		}
		share[i] = float64(v) / float64(uint64(1)<<(4*len(part))-1)
	}
	return 0.299*share[0]+0.587*share[1]+0.114*share[2] >= lightFrom, true
}

// toldBackground is what COLORFGBG says of the background, when it says it.
func toldBackground() (light, ok bool) {
	told := os.Getenv("COLORFGBG")
	n, err := strconv.Atoi(told[strings.LastIndexByte(told, ';')+1:])
	if err != nil || n < 0 {
		return false, false
	}
	return n == 7 || n == 15, true // the background numbers that are light
}

// colorCount is how many colours this terminal is asked to draw.
func colorCount() int {
	switch {
	case os.Getenv("NO_COLOR") != "":
		return 0
	case strings.Contains(os.Getenv("TERM"), "256color"), os.Getenv("COLORTERM") != "":
		return 256
	}
	return 8
}
