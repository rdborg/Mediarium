//go:build unix

package api

import (
	"os/exec"
	"syscall"
)

// ownProcessGroup starts a script in a process group of its own, and makes
// stopping it (on its time limit) stop everything it started too.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		killProcessGroup(cmd)
		return nil
	}
}

// killProcessGroup stops whatever is left of a script's process group.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil && cmd.Process.Pid > 0 {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
