package lab

import (
	"fmt"
	"math"
)

// NodeCPUs is every lab node's CPU cap (#1892): one CPU, an edge box's share,
// so a node sizes its lane pool to the same figure on a laptop and on a large
// host, and a run's parallelism is one known number rather than the host's.
const NodeCPUs = 1.0

// nodeMemoryFloorMiB holds a small site's collector peaks: the cost model's
// steady figure for a branch sits far under it.
const nodeMemoryFloorMiB = 512

// nodeMemoryHeadroom is the cap's multiple of the cost model's steady figure,
// room for a burst over the measured resident set.
const nodeMemoryHeadroom = 2

// NodeMemoryLimit is a lab node's memory cap in bytes for the components its
// site serves: twice what the cost model says a node of that size holds (its
// own share plus each component's, the simulator in its process), never under
// the floor. The soak's resident sets sat on the model's line, so twice the
// line is a ceiling a healthy node does not meet and a leaking one does.
func NodeMemoryLimit(components int) int64 {
	mib := nodeMemoryHeadroom * (MeasuredCost.Node.MemMiB + MeasuredCost.Component.MemMiB*float64(components))
	// The model's decimal fractions are not exact in binary; trim the error
	// before rounding up so a whole figure stays whole.
	limit := int64(math.Ceil(mib - 1e-9))
	return max(limit, nodeMemoryFloorMiB) << 20
}

// GoMemLimitEnv is the node's Go soft memory limit, a tenth under its
// container's cap: the runtime does not read the cgroup, so without it the
// collector paces to the heap's growth and the kernel kills the node before
// the collector tightens.
func GoMemLimitEnv(limitBytes int64) string {
	return fmt.Sprintf("GOMEMLIMIT=%dMiB", limitBytes*9/10>>20)
}
