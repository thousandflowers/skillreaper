//go:build darwin || linux

package tty

import (
	"bufio"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/ptytest"
)

// settle is how long a typed line gets to reach the terminal's input queue.
const settle = 100 * time.Millisecond

type filer struct{ f *os.File }

func (w filer) File() *os.File             { return w.f }
func (w filer) Read(p []byte) (int, error) { return w.f.Read(p) }

func TestFlushInput_drops_keys_typed_before_the_question(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(master)
	defer slave.Close()
	if _, err := syscall.Write(master, []byte("typed ahead\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(settle)
	FlushInput(filer{slave}) // through a writer standing in front of the file
	if _, err := syscall.Write(master, []byte("the answer\n")); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(slave).ReadString('\n')
	if err != nil || line != "the answer\n" {
		t.Errorf("read %q, %v; want only the line typed after the flush", line, err)
	}
}

func TestFlushInput_keeps_a_pipe_and_ignores_what_has_no_file(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := w.WriteString("y\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	FlushInput(r)
	FlushInput(filer{nil}) // a writer with no file behind it
	FlushInput(nil)
	line, _ := bufio.NewReader(r).ReadString('\n')
	if line != "y\n" {
		t.Errorf("a piped answer became %q", line)
	}
	if f, ok := FileOf(filer{nil}); ok || f != nil {
		t.Errorf("FileOf a writer without a file gave %v, %v", f, ok)
	}
}
