package platformmetric

import (
	"testing"
	"time"
)

// #1686: a busy fraction is busy over capacity, held to [0, 1]: a window busy
// throughout reads exactly 1 whatever its length, never a rounding past it,
// and an empty window reads 0, never a division by zero.
func TestBusyFractionIsBusyOverCapacityHeldToOne(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		busy, capacity time.Duration
		want           float64
	}{
		{3 * time.Second, 10 * time.Second, 0.3},
		{30 * time.Second, 120 * time.Second, 0.25},
		{7*time.Second + 123*time.Nanosecond, 7*time.Second + 123*time.Nanosecond, 1},
		{11 * time.Second, 10 * time.Second, 1},
		{time.Second, 0, 0},
		{-time.Second, 10 * time.Second, 0},
		{0, 0, 0},
	} {
		if got := BusyFraction(c.busy, c.capacity); got != c.want {
			t.Errorf("BusyFraction(%v, %v) = %v, want %v", c.busy, c.capacity, got, c.want)
		}
	}
}
