//go:build !darwin && !linux

package reinvest

import (
	"fmt"
	"os"
	"runtime"
)

// isTerminalFile has no dependency-free exact form here: a character device
// is the closest answer.
func isTerminalFile(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// pickSwaps refuses: the picker drives the terminal through ioctls this
// platform does not have.
func (a *app) pickSwaps([]swap) ([]swap, error) {
	return nil, fmt.Errorf("%w on %s", errNoPicker, runtime.GOOS)
}
