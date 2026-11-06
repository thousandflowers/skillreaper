package usage

import "errors"

func mkfifo(string) error { return errors.New("no fifos on windows") }
