//go:build windows

package lab

import (
	"os"
	"strconv"
	"syscall"
)

// processAlive reports whether the process with the pid and creation time
// runs on this host. A pid held by a process created at another time is a
// reused one, so it reads as gone; an empty start asks about the pid alone.
func processAlive(pid int, start string) bool {
	if pid <= 0 {
		return false
	}
	const processQueryLimitedInformation = 0x1000
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	const stillActive = 259
	if code != stillActive {
		return false
	}
	if start == "" {
		return true
	}
	got, ok := handleStart(h)
	return !ok || got == start
}

// handleStart is the creation time of the process the handle opens, as the
// 100ns ticks since 1601 that GetProcessTimes reports.
func handleStart(h syscall.Handle) (string, bool) {
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return "", false
	}
	return strconv.FormatUint(uint64(created.HighDateTime)<<32|uint64(created.LowDateTime), 10), true
}

// currentStart is this process's creation time.
func currentStart() string {
	h, err := syscall.OpenProcess(0x1000, false, uint32(os.Getpid()))
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	start, _ := handleStart(h)
	return start
}

// currentPIDNS is empty on Windows: there are no PID namespaces to tell apart.
func currentPIDNS() string { return "" }
