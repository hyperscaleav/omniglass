//go:build !linux && !darwin

package node

import "time"

// threadClockReads is whether this platform has a per-thread CPU clock: not
// one the node reads here, so its runs land a duration and no CPU time.
const threadClockReads = false

func threadCPU() (time.Duration, bool) { return 0, false }
