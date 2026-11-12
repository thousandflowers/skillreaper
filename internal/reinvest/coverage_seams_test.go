//go:build darwin || linux

package reinvest

import (
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

var errSeamCov = errors.New("seam failed")

// seamCovSwap sets *v to to until the test ends.
func seamCovSwap[T any](t *testing.T, v *T, to T) {
	t.Helper()
	old := *v
	*v = to
	t.Cleanup(func() { *v = old })
}

func TestSeamCov_home_falls_back_to_the_user_database_then_to_nothing(t *testing.T) {
	t.Setenv("HOME", "")
	seamCovSwap(t, &currentUser, func() (*user.User, error) { return &user.User{HomeDir: "/from/the/database"}, nil })
	if got := homeDir(); got != "/from/the/database" {
		t.Errorf("with no HOME the home is %q, want the user database's", got)
	}
	seamCovSwap(t, &currentUser, func() (*user.User, error) { return nil, errSeamCov })
	if got := homeDir(); got != "" {
		t.Errorf("with no HOME and no user the home is %q, want none", got)
	}
}

func TestSeamCov_cache_and_state_folders_on_each_system(t *testing.T) {
	home := tempHome(t)
	for _, name := range []string{"LocalAppData", "AppData", "XDG_CACHE_HOME", "XDG_DATA_HOME"} {
		t.Setenv(name, "")
	}
	want := map[string][2]string{
		"darwin":  {filepath.Join(home, "Library", "Caches"), filepath.Join(home, "Library", "Application Support", toolDirName, subDirName)},
		"windows": {filepath.Join(home, "AppData", "Local"), filepath.Join(home, "AppData", "Roaming", toolDirName, subDirName)},
		"linux":   {filepath.Join(home, ".cache"), filepath.Join(home, ".local", "share", toolDirName, subDirName)},
	}
	for goos, w := range want {
		if got := cacheBaseOn(goos); got != w[0] {
			t.Errorf("%s cache = %q, want %q", goos, got, w[0])
		}
		if got := stateDirOn(goos); got != w[1] {
			t.Errorf("%s state = %q, want %q", goos, got, w[1])
		}
	}
	t.Setenv("LocalAppData", `C:\Local`)
	t.Setenv("AppData", `C:\Roaming`)
	if got := cacheBaseOn("windows"); got != `C:\Local` {
		t.Errorf("windows cache with LocalAppData = %q", got)
	}
	if got := stateDirOn("windows"); got != filepath.Join(`C:\Roaming`, toolDirName, subDirName) {
		t.Errorf("windows state with AppData = %q", got)
	}
}

func TestSeamCov_a_written_skill_that_cannot_be_taken_back_says_where_it_is(t *testing.T) {
	c := writtenSwap().cand()
	home := tempHome(t)
	ttyCovDangling(t, filepath.Join(home, ".claude", "skills"))
	seamCovSwap(t, &removeAll, func(string) error { return errSeamCov })
	err := create(c)
	if err == nil || !strings.Contains(err.Error(), "could not be removed: "+errSeamCov.Error()) {
		t.Errorf("a link that failed and a folder that stayed gave %v", err)
	}
}

func TestSeamCov_with_no_REAP_and_no_own_binary_there_is_no_reap(t *testing.T) {
	t.Setenv(reapVariable, "")
	seamCovSwap(t, &executable, func() (string, error) { return "", errSeamCov })
	if cmd, err := reapCommand(); !errors.Is(err, errSeamCov) || cmd != nil {
		t.Errorf("reapCommand = %v, %v; want the error", cmd, err)
	}
}

type seamCovLog struct{ io.Writer }

func (seamCovLog) Close() error { return errSeamCov }

func TestSeamCov_a_run_log_that_cannot_be_closed_fails_the_run(t *testing.T) {
	a, _ := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 4, Reason: "fits", Use: "Cuts your releases."})
	seamCovSwap(t, &createLog, func(string) (io.WriteCloser, error) { return seamCovLog{io.Discard}, nil })
	if err := a.run(options{days: defaultDays, max: defaultSwaps, dryRun: true}); !errors.Is(err, errSeamCov) {
		t.Errorf("run = %v, want the close error", err)
	}
	if _, err := os.Stat(a.cachePath(logFile)); err == nil {
		t.Error("the real log was made although the stand-in was asked for")
	}
}
