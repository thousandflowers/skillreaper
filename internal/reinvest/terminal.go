package reinvest

import (
	"errors"
	"io"
	"os"
)

var errNoPicker = errors.New("the arrow-key picker is not available")

// filer is a stream standing in front of a file, as reap's own stdout is.
type filer interface{ File() *os.File }

// fileOf returns the file behind a stream, if there is one.
func fileOf(stream any) (*os.File, bool) {
	switch v := stream.(type) {
	case *os.File:
		return v, v != nil
	case filer:
		f := v.File()
		return f, f != nil
	}
	return nil, false
}

// isTerminal reports whether a stream is a person's terminal.
func isTerminal(stream any) bool {
	f, ok := fileOf(stream)
	return ok && isTerminalFile(f)
}

// readLine reads one line, a byte at a time so nothing after it is taken.
func readLine(r io.Reader) (string, error) {
	var line []byte
	var buf [1]byte
	for {
		n, err := r.Read(buf[:])
		if n == 1 {
			if buf[0] == '\n' {
				return string(line), nil
			}
			line = append(line, buf[0])
		}
		if err != nil {
			if err == io.EOF && len(line) > 0 {
				return string(line), nil
			}
			return string(line), err
		}
	}
}
