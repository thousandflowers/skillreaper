package prune

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Runs that all find a lock left by a dead run, at once: at most one may hold
// it at any moment. The takeover used to remove whatever file was there, so a
// late remover could delete the lock an earlier one had just taken.
func TestTakingOverALeftLockAdmitsOneAtATime(t *testing.T) {
	const rounds, runs = 200, 8
	for round := 0; round < rounds; round++ {
		dir := t.TempDir()
		path := filepath.Join(dir, ".reap-prune.lock")
		if err := os.WriteFile(path, []byte("pid 999999 at then\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var holding, most atomic.Int32
		var start, wg sync.WaitGroup
		start.Add(1)
		for range runs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				start.Wait()
				l, err := AcquireLock(dir)
				if err != nil {
					return
				}
				n := holding.Add(1)
				for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
				}
				time.Sleep(2 * time.Millisecond) // the item in hand
				holding.Add(-1)
				l.Release()
			}()
		}
		start.Done()
		wg.Wait()
		if most.Load() > 1 {
			t.Fatalf("round %d: %d runs held the lock at once", round, most.Load())
		}
	}
}
