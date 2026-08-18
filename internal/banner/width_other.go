//go:build !unix

package banner

import "os"

// terminalWidth has no dependency-free implementation on this platform, so the
// width stays unknown and the banner relies on the terminal check alone.
func terminalWidth(*os.File) (int, bool) { return 0, false }
