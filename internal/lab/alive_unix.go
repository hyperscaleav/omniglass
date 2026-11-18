//go:build !windows

package lab

import (
	"errors"
	"os"
	"syscall"
)

// processAlive reports whether the process with the pid and start time runs
// on this host. A pid held by a process that started at another time is a
// reused one, so it reads as gone; an empty start asks about the pid alone,
// and so does a start the platform cannot read for a process that exists.
func processAlive(pid int, start string) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	if start == "" {
		return true
	}
	got, ok := processStart(pid)
	return !ok || got == start
}

// currentStart is this process's start time, empty where the platform has
// none to read.
func currentStart() string {
	start, _ := processStart(os.Getpid())
	return start
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
