package lab

import (
	"fmt"
	"math"
)

// OutpostCPUs is every lab outpost's CPU cap (#1892): one CPU, an edge box's share,
// so an outpost sizes its lane pool to the same figure on a laptop and on a large
// host, and a run's parallelism is one known number rather than the host's.
const OutpostCPUs = 1.0

// outpostMemoryFloorMiB holds a small site's collector peaks: the cost model's
// steady figure for a branch sits far under it.
const outpostMemoryFloorMiB = 512

// outpostMemoryHeadroom is the cap's multiple of the cost model's steady figure,
// room for a burst over the measured resident set.
const outpostMemoryHeadroom = 2

// OutpostMemoryLimit is a lab outpost's memory cap in bytes for the components its
// site serves: twice what the cost model says an outpost of that size holds (its
// own share plus each component's, the simulator in its process), never under
// the floor. The soak's resident sets sat on the model's line, so twice the
// line is a ceiling a healthy outpost does not meet and a leaking one does.
func OutpostMemoryLimit(components int) int64 {
	mib := outpostMemoryHeadroom * (MeasuredCost.Outpost.MemMiB + MeasuredCost.Component.MemMiB*float64(components))
	// The model's decimal fractions are not exact in binary; trim the error
	// before rounding up so a whole figure stays whole.
	limit := int64(math.Ceil(mib - 1e-9))
	return max(limit, outpostMemoryFloorMiB) << 20
}

// GoMemLimitEnv is the outpost's Go soft memory limit, a tenth under its
// container's cap: the runtime reads the cgroup's CPU limit but not its
// memory limit, so without it the
// collector paces to the heap's growth and the kernel kills the outpost before
// the collector tightens.
func GoMemLimitEnv(limitBytes int64) string {
	return fmt.Sprintf("GOMEMLIMIT=%dMiB", limitBytes*9/10>>20)
}
