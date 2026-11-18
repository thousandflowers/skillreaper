package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// keep reads the list, adds to it and writes it back, as prune and mute do with
// their state. Without the lock they share, two keeps at once lost one of the
// two: a skill marked "never prune" was no longer on the list, and nothing said so.
func TestKeepsRunAtOnceAllLand(t *testing.T) {
	home := deleteHome(t)
	const count = 10
	names := make([]string, count)
	for i := range names {
		names[i] = fmt.Sprintf("kept-%02d", i)
		putSkill(t, home, names[i])
	}

	var wg sync.WaitGroup
	for _, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code, out := reapRun("keep", name); code != 0 {
				t.Errorf("keep %s: %d %s", name, code, out)
			}
		}()
	}
	wg.Wait()

	_, list := reapRun("keep", "--list")
	for _, name := range names {
		if !strings.Contains(list, "skill:"+name+"\n") {
			t.Errorf("%s was marked as keep and is not on the list:\n%s", name, list)
		}
	}

	for _, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code, out := reapRun("keep", "--remove", name); code != 0 {
				t.Errorf("keep --remove %s: %d %s", name, code, out)
			}
		}()
	}
	wg.Wait()
	if _, list := reapRun("keep", "--list"); strings.Contains(list, "kept-") {
		t.Errorf("removed keeps are still on the list:\n%s", list)
	}
}
