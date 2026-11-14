package platformmetric

import "time"

// BusyFraction is the share of capacity spent busy, busy over capacity held
// to [0, 1]: a worker pool's busy worker time over its worker time, or a
// single lane's busy time over the window's wall time. 0 for no capacity, so
// an empty window never divides by zero. Pure.
func BusyFraction(busy, capacity time.Duration) float64 {
	if capacity <= 0 || busy <= 0 {
		return 0
	}
	if busy >= capacity {
		return 1
	}
	return float64(busy) / float64(capacity)
}
