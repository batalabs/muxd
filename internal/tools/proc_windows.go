package tools

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// setProcGroup creates the process in a new process group and sets
// Cancel to kill the entire process tree so child processes don't
// survive and hold stdout/stderr pipes open.
func setProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
	cmd.Cancel = func() error {
		// Use taskkill /T /F to kill the entire process tree.
		// TerminateProcess alone only kills the parent.
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		kill.Stderr = os.Stderr
		if err := kill.Run(); err != nil {
			// Fallback to direct kill if taskkill fails.
			return cmd.Process.Kill()
		}
		return nil
	}
}
