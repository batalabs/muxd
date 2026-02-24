//go:build windows

package daemon

import "syscall"

const processQueryLimitedInformation = 0x1000

// IsProcessAlive checks whether a process with the given PID is running.
func IsProcessAlive(pid int) bool {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(h)
	return true
}
