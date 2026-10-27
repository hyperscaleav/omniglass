//go:build windows

package lab

import "syscall"

// processAlive reports whether a process with the pid exists on this host.
func processAlive(pid int) bool {
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
	return code == stillActive
}

// currentPIDNS is empty on Windows: there are no PID namespaces to tell apart.
func currentPIDNS() string { return "" }
