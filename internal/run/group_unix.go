//go:build unix

package run

import (
	"os/exec"
	"syscall"
)

// setGroup starts the command in its own process group and makes
// cancellation kill the whole group, so a command's children die with it
// instead of outliving the timeout.
func setGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
