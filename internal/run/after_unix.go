//go:build unix

package run

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// afterExitScript waits for the process named in $0 to exit, then becomes the
// command given in "$@". The script is fixed: the command reaches it only as
// arguments, never as shell text, so no value in it can become shell syntax.
const afterExitScript = `while kill -0 "$0" 2>/dev/null; do sleep 0.05; done; exec "$@"`

// afterExitPID is the process the waiter waits for: this one; tests replace it.
var afterExitPID = os.Getpid

// startAfterExit starts a waiter in a session of its own, so it outlives the
// panel's terminal closing (which signals the panel's whole session), and
// returns once it has started.
func startAfterExit(req Request) error {
	args := append([]string{"-c", afterExitScript, strconv.Itoa(afterExitPID())}, req.Argv...)
	cmd := exec.Command("/bin/sh", args...) //nolint:noctx // it must outlive every context of this process
	cmd.Env = append(os.Environ(), req.Env...)
	cmd.Dir = req.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s after the panel closes: %w", req.Argv[0], err)
	}
	go func() { _ = cmd.Wait() }() // reap it if this process outlives it
	return nil
}
