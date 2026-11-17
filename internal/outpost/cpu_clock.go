package node

import (
	"runtime"
	"time"
)

// runCost is what one task run cost the node (#1870): its wall time, and the
// CPU the kernel charged the run's thread where the platform has a
// per-thread clock (cpuRead). Wall time is mostly waiting on the device; the
// CPU time is the node's own work.
type runCost struct {
	took    time.Duration
	cpu     time.Duration
	cpuRead bool
}

// measureRun runs fn with the calling goroutine locked to its OS thread and
// reads the kernel's CPU clock for that thread before and after: user plus
// system time the thread ran, so a run parked on the network is charged
// nothing while it waits. The lock is what makes the thread the run's alone
// for its length; a parked locked goroutine parks its thread, so a node holds
// at most one extra thread per lane. What the clock counts: fn's own
// syscalls and any GC assist fn pays. What it misses: work fn hands to
// another goroutine (a held session's reader, the HTTP client's connection
// goroutines, a driver's helper) runs on another thread, and the garbage
// collector's background workers are charged to no run.
func measureRun(fn func()) runCost {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	start := time.Now()
	before, ok := threadCPU()
	fn()
	after, okAfter := threadCPU()
	cost := runCost{took: time.Since(start)}
	if ok && okAfter && after >= before {
		cost.cpu, cost.cpuRead = after-before, true
	}
	return cost
}
