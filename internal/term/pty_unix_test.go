//go:build darwin || linux

package term

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/thousandflowers/skillreaper/internal/ptytest"
)

// patience bounds every wait on a terminal or a child, so a broken test fails
// instead of hanging.
const patience = 10 * time.Second

// ptyTerm is a pseudo-terminal whose far end the test holds, as a terminal
// window would: what the program writes is collected, keys are typed into it.
type ptyTerm struct {
	t      *testing.T
	master int
	slave  *os.File
	fd     int // the slave's descriptor
	done   chan struct{}

	mu      sync.Mutex
	out     []byte
	reply   string // written back once the background query is seen
	hangUp  bool   // close the far end instead of answering the query
	hungUp  bool
	replied bool
}

func newPTY(t *testing.T, rows, cols int) *ptyTerm {
	t.Helper()
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Fatalf("no pseudo-terminal: %v", err)
	}
	p := &ptyTerm{t: t, master: master, slave: slave, fd: int(slave.Fd()), done: make(chan struct{})}
	p.setSize(rows, cols)
	go p.collect()
	t.Cleanup(p.close)
	return p
}

// collect reads what the program writes until every slave end is closed.
func (p *ptyTerm) collect() {
	defer close(p.done)
	var buf [4096]byte
	for {
		n, err := syscall.Read(p.master, buf[:])
		if err == syscall.EINTR {
			continue
		}
		if n <= 0 {
			return
		}
		p.mu.Lock()
		p.out = append(p.out, buf[:n]...)
		asked := !p.replied && bytes.Contains(p.out, []byte(backgroundQuery))
		if asked && p.hangUp {
			p.hungUp = true
			_ = syscall.Close(p.master)
			p.mu.Unlock()
			return
		}
		if asked && p.reply != "" {
			p.replied = true
			_, _ = syscall.Write(p.master, []byte(p.reply))
		}
		p.mu.Unlock()
	}
}

func (p *ptyTerm) close() {
	_ = p.slave.Close()
	select {
	case <-p.done:
	case <-time.After(patience):
		p.t.Error("the pseudo-terminal's far end never saw its last reader go")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.hungUp {
		_ = syscall.Close(p.master)
	}
}

func (p *ptyTerm) output() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(p.out)
}

// waitFor waits until the program has written s, from byte from on.
func (p *ptyTerm) waitFor(from int, s string) bool {
	for end := time.Now().Add(patience); time.Now().Before(end); time.Sleep(2 * time.Millisecond) {
		if out := p.output(); len(out) >= from && strings.Contains(out[from:], s) {
			return true
		}
	}
	return false
}

// typeIn types s at the terminal, and waits until the program can read it.
func (p *ptyTerm) typeIn(s string) {
	p.t.Helper()
	if _, err := syscall.Write(p.master, []byte(s)); err != nil {
		p.t.Fatalf("typing %q: %v", s, err)
	}
}

// waitTyped waits until what was typed has reached the program's side.
func (p *ptyTerm) waitTyped() {
	p.t.Helper()
	if ready, _, err := waitReadable(p.fd, -1, patience); err != nil || !ready {
		p.t.Fatalf("what was typed never arrived: ready %v, %v", ready, err)
	}
}

func (p *ptyTerm) termios() syscall.Termios {
	p.t.Helper()
	var tio syscall.Termios
	if err := ioctl(p.fd, ioctlGetTermios, unsafe.Pointer(&tio)); err != nil {
		p.t.Fatalf("reading the terminal's settings: %v", err)
	}
	return tio
}

func (p *ptyTerm) setSize(rows, cols int) {
	p.t.Helper()
	ws := struct{ rows, cols, xpixel, ypixel uint16 }{uint16(rows), uint16(cols), 0, 0}
	if err := ioctl(p.fd, syscall.TIOCSWINSZ, unsafe.Pointer(&ws)); err != nil {
		p.t.Fatalf("setting the window's size: %v", err)
	}
}

// isRaw says whether keys arrive one at a time, unechoed, with Ctrl-C still a
// signal and Ctrl-S no freeze.
func isRaw(tio syscall.Termios) bool {
	return tio.Lflag&(syscall.ICANON|syscall.ECHO|syscall.IEXTEN) == 0 && tio.Lflag&syscall.ISIG != 0 &&
		tio.Iflag&syscall.IXON == 0 && tio.Cc[syscall.VMIN] == 1 && tio.Cc[syscall.VTIME] == 0 && suspendKeyOff(tio)
}

// localEnv is a local terminal with its background already told, so nothing is
// asked of it and no keyboard helper starts.
var localEnv = map[string]string{
	"TERM": "xterm-256color", "COLORFGBG": "15;0", "COLORTERM": "", "NO_COLOR": "",
	"SSH_CONNECTION": "", "SSH_TTY": "",
}

// onPTY makes p the program's stdin and stdout, as a shell would, until the
// test ends. The keyboard helper is kept off: SSH_CONNECTION says the keyboard
// is elsewhere, and SSH_TTY stays unset so keys are timed as local ones.
func onPTY(t *testing.T, p *ptyTerm, env map[string]string) {
	t.Helper()
	for name, value := range localEnv {
		t.Setenv(name, value)
	}
	t.Setenv("SSH_CONNECTION", "test")
	for name, value := range env {
		t.Setenv(name, value)
	}
	stdin, stdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = p.slave, p.slave
	t.Cleanup(func() { os.Stdin, os.Stdout = stdin, stdout })
}

// openOn opens the menu's terminal on p, and closes it when the test ends.
func openOn(t *testing.T, p *ptyTerm, env map[string]string) *Terminal {
	t.Helper()
	onPTY(t, p, env)
	tm, err := Open()
	if err != nil {
		t.Fatalf("Open on a pseudo-terminal: %v", err)
	}
	t.Cleanup(tm.Close)
	return tm
}

// pipeFDs is a pipe as raw descriptors, closed when the test ends.
func pipeFDs(t *testing.T) (r, w int) {
	t.Helper()
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Close(fds[0])
		_ = syscall.Close(fds[1])
	})
	return fds[0], fds[1]
}

// drain is everything waiting in the pipe that r reads.
func drain(t *testing.T, r int) string {
	t.Helper()
	var got []byte
	var buf [65536]byte
	for {
		ready, _, err := waitReadable(r, -1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !ready {
			return string(got)
		}
		n, err := syscall.Read(r, buf[:])
		if n <= 0 || err != nil {
			return string(got)
		}
		got = append(got, buf[:n]...)
	}
}

// closedFD is a descriptor number nothing has open.
func closedFD(t *testing.T) int {
	t.Helper()
	for fd := 1000; fd > 900; fd-- {
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) == syscall.EBADF {
			return fd
		}
	}
	t.Skip("every descriptor from 900 to 1000 is open")
	return -1
}

// bareTerminal is a Terminal on descriptors that need not be a terminal, for
// what it does with them once open: the fields are those Open sets.
func bareTerminal(t *testing.T, in, out, rows, cols int) *Terminal {
	t.Helper()
	wakeR, wakeW := pipeFDs(t)
	tm := &Terminal{in: in, out: out, mouse: true, full: true, gap: math.MaxInt64, colors: 256,
		wakeR: wakeR, wakeW: wakeW, done: make(chan struct{}), signals: make(chan os.Signal, 1), fr: newFrame(rows, cols)}
	tm.src = &fdSource{fd: in}
	tm.dec = decoder{src: tm.src, escWait: escWaitLocal}
	return tm
}

// The child: the test binary run again on a pseudo-terminal, for what ends or
// stops a process. It reports through descriptor 3.

const childScenarioVar = "SKILLREAPER_TERM_CHILD"

func TestTerminalChild(t *testing.T) {
	name := os.Getenv(childScenarioVar)
	if name == "" {
		t.Skip("runs only as the child of a terminal test")
	}
	run, ok := childScenarios[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "no child scenario %q\n", name)
		os.Exit(3)
	}
	os.Exit(run(os.NewFile(3, "report")))
}

type childProc struct {
	t      *testing.T
	cmd    *exec.Cmd
	lines  chan string
	exited chan struct{}
	stderr bytes.Buffer
}

// childEnvDropped are the variables a child decides for itself.
var childEnvDropped = []string{"LANG", "LC_ALL", "LC_CTYPE", "GOCOVERDIR", childScenarioVar}

// startChild runs scenario with p as its stdin and stdout. Its coverage goes
// where this test's goes, so what it runs counts.
func startChild(t *testing.T, p *ptyTerm, scenario string, env map[string]string, attr *syscall.SysProcAttr) *childProc {
	t.Helper()
	vars := map[string]string{childScenarioVar: scenario, "SSH_CONNECTION": "test"}
	for name, value := range localEnv {
		if name != "SSH_CONNECTION" {
			vars[name] = value
		}
	}
	for name, value := range env {
		vars[name] = value
	}
	if f := flag.Lookup("test.gocoverdir"); f != nil && f.Value.String() != "" {
		dir, err := filepath.Abs(f.Value.String())
		if err != nil {
			t.Fatal(err)
		}
		vars["GOCOVERDIR"] = dir
	}
	var environ []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, mine := vars[name]; !mine && !slices.Contains(childEnvDropped, name) {
			environ = append(environ, kv)
		}
	}
	for name, value := range vars {
		environ = append(environ, name+"="+value)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	c := &childProc{t: t, lines: make(chan string, 16), exited: make(chan struct{})}
	c.cmd = exec.Command(os.Args[0], "-test.run=^TestTerminalChild$")
	c.cmd.Env = environ
	c.cmd.Stdin, c.cmd.Stdout, c.cmd.Stderr = p.slave, p.slave, &c.stderr
	c.cmd.ExtraFiles = []*os.File{w}
	c.cmd.SysProcAttr = attr
	if err := c.cmd.Start(); err != nil {
		t.Fatalf("starting the child: %v", err)
	}
	_ = w.Close()
	go func() {
		defer close(c.lines)
		defer r.Close()
		scan := bufio.NewScanner(r)
		for scan.Scan() {
			c.lines <- scan.Text()
		}
	}()
	go func() {
		_ = c.cmd.Wait()
		close(c.exited)
	}()
	t.Cleanup(func() {
		select {
		case <-c.exited:
		default:
			_ = c.cmd.Process.Kill()
			<-c.exited
		}
	})
	return c
}

// next is the child's next report line.
func (c *childProc) next() string {
	c.t.Helper()
	select {
	case line, ok := <-c.lines:
		if ok {
			return line
		}
		<-c.exited
		c.t.Fatalf("the child ended without reporting; stderr:\n%s", c.stderr.String())
	case <-time.After(patience):
		c.t.Fatalf("the child reported nothing for %v", patience)
	}
	return ""
}

func (c *childProc) expect(want string) {
	c.t.Helper()
	if got := c.next(); got != want {
		c.t.Fatalf("the child reported %q, want %q", got, want)
	}
}

func (c *childProc) signal(sig syscall.Signal) {
	c.t.Helper()
	if err := c.cmd.Process.Signal(sig); err != nil {
		c.t.Fatalf("signalling the child: %v", err)
	}
}

// wait is how the child ended: its exit code, or the signal that killed it.
func (c *childProc) wait() (code int, killedBy syscall.Signal) {
	c.t.Helper()
	select {
	case <-c.exited:
	case <-time.After(patience):
		_ = c.cmd.Process.Kill()
		<-c.exited
		c.t.Fatalf("the child did not end within %v; stderr:\n%s", patience, c.stderr.String())
	}
	status := c.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if status.Signaled() {
		return -1, status.Signal()
	}
	return status.ExitStatus(), 0
}

// waitStopped waits until the child is stopped, as by a shell's Ctrl-Z.
func (c *childProc) waitStopped() {
	c.t.Helper()
	got := make(chan syscall.WaitStatus, 1)
	go func() {
		var status syscall.WaitStatus
		for {
			_, err := syscall.Wait4(c.cmd.Process.Pid, &status, syscall.WUNTRACED, nil)
			if err != syscall.EINTR {
				break
			}
		}
		got <- status
	}()
	select {
	case status := <-got:
		if status&0xff != 0x7f { // stopped, by any signal: darwin's Stopped leaves out SIGSTOP
			c.t.Fatalf("the child did not stop: %v", status)
		}
	case <-time.After(patience):
		c.t.Fatalf("the child did not stop within %v", patience)
	}
}
