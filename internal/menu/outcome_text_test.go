package menu

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// On a line the screen cuts, what went well comes first and a failure names
// its cause, not two full paths that push the cause off the edge.
func TestAFailedApplyShowsWhatWentWellAndWhy(t *testing.T) {
	long := "/private/tmp/claude-501/" + strings.Repeat("x", 80)
	moved := &os.LinkError{Op: "rename", Old: long + "/a", New: long + "/b", Err: fs.ErrPermission}
	opened := &fs.PathError{Op: "open", Path: long, Err: errors.New("disk full")}
	for err, cause := range map[error]string{moved: "permission denied", opened: "disk full", errors.New("it is locked: unlock it first"): "it is locked: unlock it first"} {
		if got := causeOf(err); got != cause {
			t.Errorf("causeOf(%v) = %q, want %q", err, got, cause)
		}
	}
}
