package reinvest

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	toolDirName = "skillreaper"
	subDirName  = "reinvest"
	day         = 24 * time.Hour
)

// app is one run of reinvest: where it reads and writes, and every door to
// the outside (Claude, the network, installers, the clock), so each can be
// replaced in a test.
type app struct {
	cacheDir    string // searches, documents, verdicts, the report: all can be fetched or asked again
	stateDir    string // what cannot: the memory of sessions Claude Code has since deleted
	projectsDir string // Claude Code's transcripts

	stdin   io.Reader
	term    io.Writer // the person's terminal: progress and the result
	termTTY bool
	stderr  io.Writer
	logMu   sync.Mutex
	log     io.Writer // everything else: the run log

	now   func() time.Time
	sleep func(time.Duration)

	// How the verifier is asked. A verdict is cached under all of these, so
	// changing one asks again.
	verifyPrompt string
	model        string
	thinkTokens  string
	verifyEffort string
	docChars     int

	memorySessions int

	lookPath    func(name string) (string, error)
	runProcess  func(spec procSpec) procResult
	ask         func(prompt, body string, opts askOptions) (any, error)
	verify      func(req verifyRequest) (verdict, error)
	groupAll    func(sessions []session) ([]*obj, bool, error)
	httpGet     func(url string, timeout time.Duration) ([]byte, error)
	httpJSON    func(url string, policy retryPolicy) (any, error)
	skillsSh    func(url string) (any, error)
	skillDoc    func(c candidate) string
	searchMCP   func(words []string) []candidate
	runStep     func(cmd []string) error
	isInstalled func(c candidate) bool
	unchecked   func(c candidate) []string
	remember    func(c candidate) error
	reapTokens  func(slug, doc string) int
	reapCommand func() ([]string, error)
	pick        func(swaps []swap) ([]swap, error)
	runPager    func(argv []string, text string) error

	skillsShMu sync.Mutex // one skills.sh request at a time: parallel ones are what trips the rate limit
}

// newApp wires an app to the real world.
func newApp(stdin io.Reader, stdout, stderr io.Writer) *app {
	a := &app{
		cacheDir:       defaultCacheDir(),
		stateDir:       defaultStateDir(),
		projectsDir:    filepath.Join(homeDir(), ".claude", "projects"),
		stdin:          stdin,
		term:           stdout,
		termTTY:        isTerminal(stdout),
		stderr:         stderr,
		log:            io.Discard,
		now:            time.Now,
		sleep:          time.Sleep,
		verifyPrompt:   verifyPrompt,
		model:          model,
		thinkTokens:    thinkTokens,
		verifyEffort:   verifyEffort,
		docChars:       docChars,
		memorySessions: memorySessions,
		lookPath:       exec.LookPath,
		runProcess:     runProcess,
		reapTokens:     reapTokens,
		reapCommand:    reapCommand,
	}
	a.ask = a.askClaude
	a.verify = a.verifyOne
	a.groupAll = a.groupSessions
	a.httpGet = httpGet
	a.httpJSON = a.getJSON
	a.skillsSh = a.skillsShJSON
	a.skillDoc = a.fetchedSkillDoc
	a.searchMCP = a.searchRegistry
	a.runStep = a.runInEmptyDir
	a.isInstalled = isInstalled
	a.unchecked = uncheckedFiles
	a.remember = a.rememberServer
	a.pick = a.pickSwaps
	a.runPager = runPager
	return a
}

// homeDir is Path.home(): the environment first, then the user database.
func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// cacheBase is the user cache directory as reap's own os.UserCacheDir picks
// it, so reap reads what reinvest leaves here.
func cacheBase() string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(homeDir(), "Library", "Caches")
	case "windows":
		return envOr("LocalAppData", filepath.Join(homeDir(), "AppData", "Local"))
	}
	return envOr("XDG_CACHE_HOME", filepath.Join(homeDir(), ".cache"))
}

func defaultCacheDir() string { return filepath.Join(cacheBase(), toolDirName, subDirName) }

// defaultStateDir is where what cannot be fetched again lives. A cache folder
// can be emptied at any time, by the system or a cleaner.
func defaultStateDir() string {
	var base string
	switch runtime.GOOS {
	case "darwin":
		base = filepath.Join(homeDir(), "Library", "Application Support")
	case "windows":
		base = envOr("AppData", filepath.Join(homeDir(), "AppData", "Roaming"))
	default:
		base = envOr("XDG_DATA_HOME", filepath.Join(homeDir(), ".local", "share"))
	}
	return filepath.Join(base, toolDirName, subDirName)
}

func (a *app) cachePath(name string) string { return filepath.Join(a.cacheDir, name) }

const (
	privateFile = 0o600
	sharedFile  = 0o666 // before the umask, as a plain open() leaves it
	sharedDir   = 0o777
)

// writePrivate writes a cache file only this user can read: verdicts,
// reports and written skills quote session text.
func writePrivate(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), sharedDir); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, privateFile)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := f.Chmod(privateFile); err != nil {
			_ = f.Close()
			return err
		}
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// writeShared writes a file with ordinary permissions, creating its folder.
func writeShared(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), sharedDir); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), sharedFile)
}

// readJSONFile reads a JSON file from a cache. A file that is missing,
// damaged or not text gives nil: a cache is ignored, never trusted.
func readJSONFile(path string) any {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	v, err := parseJSON(string(raw))
	if err != nil {
		return nil
	}
	return v
}

// logf adds a line to the run log.
func (a *app) logf(format string, args ...any) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	fmt.Fprintf(a.log, format+"\n", args...)
}

const clearLine = "\r\033[K"

func (a *app) lineStart() string {
	if a.termTTY {
		return clearLine
	}
	return ""
}

// failed tells the person a step did not happen.
func (a *app) failed(msg string) {
	fmt.Fprintf(a.term, "%s  ✗ %s\n", a.lineStart(), msg)
}

// step announces a step (on a terminal) or reports it done.
func (a *app) step(msg string, done bool) {
	switch {
	case done:
		fmt.Fprintf(a.term, "%s  ✓ %s\n", a.lineStart(), msg)
	case a.termTTY:
		fmt.Fprintf(a.term, "  ⠋ %s…", msg)
	}
}

// parallelMap runs fn over items on at most `workers` goroutines and returns
// the results in the order of the items.
func parallelMap[T, R any](items []T, workers int, fn func(T) R) []R {
	out := make([]R, len(items))
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, workers)
	for i, item := range items {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			out[i] = fn(item)
		}()
	}
	wg.Wait()
	return out
}
