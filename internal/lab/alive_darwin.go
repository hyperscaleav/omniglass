//go:build darwin

package lab

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// processStart is the pid's creation time, from the kernel's process table.
func processStart(pid int) (string, bool) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", false
	}
	t := k.Proc.P_starttime
	return fmt.Sprintf("%d.%06d", t.Sec, t.Usec), true
}
