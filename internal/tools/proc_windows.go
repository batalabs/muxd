package tools

import (
	"os/exec"
	"syscall"
)

// setProcGroup creates the process in a new job/group so that killing
// the parent also terminates all children on Windows.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}
