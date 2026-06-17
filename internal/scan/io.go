package scan

import (
	"fmt"
	"os"
)

// maxFileSize caps how large a config or markdown file the scanner reads into
// memory. The line scanners only bound per-line size, so a crafted
// multi-gigabyte file under the scan root would otherwise be read whole and
// could OOM the process. 10 MiB is far above any real skill/agent/config file.
const maxFileSize = 10 << 20

// readCapped reads path but refuses files larger than maxFileSize. Callers
// treat the error like any other read failure (skip the file, optionally warn).
func readCapped(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > maxFileSize {
		return nil, fmt.Errorf("%s is %d bytes, over the %d-byte scan limit", path, fi.Size(), maxFileSize)
	}
	return os.ReadFile(path)
}
