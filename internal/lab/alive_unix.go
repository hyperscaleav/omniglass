//go:build !windows

package lab

import (
	"errors"
	"syscall"
)

// processAlive reports whether a process with the pid exists on this host.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
