//go:build linux || darwin

package node

import (
	"time"

	"golang.org/x/sys/unix"
)

// threadClockReads is whether this platform has a per-thread CPU clock.
const threadClockReads = true

// threadCPU reads the CPU time the kernel has charged the calling thread
// (CLOCK_THREAD_CPUTIME_ID): meaningful only as a difference on one thread.
func threadCPU() (time.Duration, bool) {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts); err != nil {
		return 0, false
	}
	return time.Duration(ts.Nano()), true
}
