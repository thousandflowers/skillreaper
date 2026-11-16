// Package flagcheck holds a command line to what a person means by it where
// the flag package reads something else.
package flagcheck

import (
	"flag"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var baseTen = regexp.MustCompile(`^[+-]?(0|[1-9][0-9]*)$`)

// Decimal refuses an integer flag whose value is a number in another base.
// The flag package reads integers in base 0: "010" is octal 8, "0x10" is 16,
// "1_000" is 1000. To whoever typed it, 010 was ten, and `--days 010` reported
// on eight days without a word. What is not a number at all is left to the
// flag package, which says so itself.
func Decimal(fs *flag.FlagSet, args []string) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return nil
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") || !isInt(fs.Lookup(name)) {
			continue
		}
		if !hasValue {
			if i+1 == len(args) {
				return nil
			}
			i++
			value = args[i]
		}
		if _, err := strconv.ParseInt(value, 0, 64); err == nil && !baseTen.MatchString(value) {
			return fmt.Errorf("invalid value %q for flag -%s: write the number in base ten", value, name)
		}
	}
	return nil
}

func isInt(f *flag.Flag) bool {
	if f == nil {
		return false
	}
	getter, ok := f.Value.(flag.Getter)
	if !ok {
		return false
	}
	_, isInt := getter.Get().(int)
	return isInt
}
