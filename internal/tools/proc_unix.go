//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
)

// setProcGroup puts the command in its own process group so that
// killing the parent also kills all children.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Kill the entire process group (negative PID).
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
