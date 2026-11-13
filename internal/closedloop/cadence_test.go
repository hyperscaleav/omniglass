package closedloop

import (
	"testing"
	"time"
)

// A target's interval is the slower of its own and the tick it waits on; a
// probe with no probe interval runs every tick.
func TestNodeCadenceInterval(t *testing.T) {
	c := NodeCadence{Heartbeat: 2 * time.Second, ProbeEvery: 10 * time.Second}
	for _, x := range []struct {
		every string
		want  time.Duration
	}{{"", 10 * time.Second}, {"1m", time.Minute}, {"500ms", 2 * time.Second}, {"nonsense", 2 * time.Second}} {
		if got := c.Interval(x.every); got != x.want {
			t.Errorf("Interval(%q) = %v, want %v", x.every, got, x.want)
		}
	}
	if got := (NodeCadence{Heartbeat: 2 * time.Second}).Interval(""); got != 2*time.Second {
		t.Errorf("a probe with no probe interval = %v, want the heartbeat", got)
	}
}
