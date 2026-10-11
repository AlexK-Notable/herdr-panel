package main

import (
	"os"

	"github.com/charmbracelet/x/term"
)

// makeRaw puts stdin in raw mode for the hold screen and returns the restore.
func makeRaw() (func(), error) {
	fd := os.Stdin.Fd()
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() { _ = term.Restore(fd, state) }, nil
}
