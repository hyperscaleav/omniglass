//go:build !windows

package lab

import (
	"errors"
	"os"
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

// currentPIDNS names this process's PID namespace (on Linux, the target of
// /proc/self/ns/pid); empty where there is none to read.
func currentPIDNS() string {
	ns, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		return ""
	}
	return ns
}
