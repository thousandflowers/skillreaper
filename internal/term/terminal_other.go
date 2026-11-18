//go:build !darwin && !linux

package term

import (
	"errors"
	"runtime"
	"time"
)

// Terminal cannot be opened on this system: the menu needs a terminal it can
// put in raw mode without a dependency, and that is written for macOS and Linux.
type Terminal struct{}

// Problem says why this cannot run as a full-screen menu.
func Problem() string {
	return "the interactive menu is not available on " + runtime.GOOS + " yet"
}

// Open always fails here, with the reason alone.
func Open() (*Terminal, error) { return nil, errors.New(Problem()) }

func (*Terminal) Close()                      {}
func (*Terminal) Hold() (release func())      { return func() {} }
func (*Terminal) Size() (rows, cols int)      { return 0, 0 }
func (*Terminal) Erase()                      {}
func (*Terminal) Put(int, int, string, Style) {}
func (*Terminal) Refresh()                    {}
func (*Terminal) ReadKey(time.Duration) Key   { return KeyEOF }
func (*Terminal) Mouse() Mouse                { return Mouse{} }
func (*Terminal) KeyGap() time.Duration       { return 0 }
func (*Terminal) FlushInput()                 {}
func (*Terminal) Beep()                       {}
func (*Terminal) SetMouse(bool) (was bool)    { return false }
func (*Terminal) ShiftHeld() bool             { return false }
func (*Terminal) SHeld() bool                 { return false }
func (*Terminal) ShiftReadable() bool         { return false }
func (*Terminal) Light() bool                 { return false }
func (*Terminal) Colors() int                 { return 0 }
