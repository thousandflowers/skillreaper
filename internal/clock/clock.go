// Package clock supplies the one "now" that every view deciding what to print
// is measured against.
//
// It exists so a capture can be regenerated at another moment and still
// compare. reap windows its evidence on the wall clock and renders ages
// against it, so docs/renders and the README's fixture blocks used to carry a
// value that changed with the day they were produced, and the only way to keep
// the check green was to exclude those fields from it.
//
// SOURCE_DATE_EPOCH is the reproducible-builds convention for exactly this:
// seconds since the Unix epoch, UTC. It is read once, and only the views are
// affected. Timestamps that record something that actually happened - a
// quarantine manifest, a lock file, the nudge state - keep the real clock, so
// setting this can never write a false time into a file.
package clock

import (
	"os"
	"strconv"
	"sync"
	"time"
)

// EnvVar names the override. Empty, absent, or unparseable means the real clock.
const EnvVar = "SOURCE_DATE_EPOCH"

var (
	once   sync.Once
	pinned time.Time // zero when there is no override
)

// Now returns the pinned instant when SOURCE_DATE_EPOCH is set to an integer,
// and the real time otherwise. The environment is read once: a process that
// answered one question against a given clock must answer the rest the same
// way, or a report can disagree with the gap view printed beside it.
func Now() time.Time {
	once.Do(func() {
		s, ok := os.LookupEnv(EnvVar)
		if !ok {
			return
		}
		secs, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			// A malformed value is ignored rather than fatal: this is a
			// reproducibility aid, and refusing to run because an unrelated
			// build tool exported something odd would be worse than the drift.
			return
		}
		pinned = time.Unix(secs, 0).UTC()
	})
	if pinned.IsZero() {
		return time.Now()
	}
	return pinned
}
