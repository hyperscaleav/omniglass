package sim

import (
	"net"
	"testing"
	"time"
)

// newTestDevice constructs a Device with state seeded from defaults.
// Behaviors mutate this state via the same applyMutations pipeline
// routes use, so test setup mirrors a real simulator load.
func newTestDevice(t *testing.T, deviceID string, state map[string]any) *Device {
	t.Helper()
	if state == nil {
		state = map[string]any{}
	}
	return NewDevice(deviceID, deviceID+".lab", net.IPv4(127, 0, 0, 1), "test-profile", 1, state)
}

// TestStopBehaviors_IdempotentWithoutStart calling stop on a device
// that never started behaviors must not block or panic.
func TestStopBehaviors_IdempotentWithoutStart(t *testing.T) {
	d := newTestDevice(t, "d-1", nil)
	// Never call Start; Stop should be a no-op.
	done := make(chan struct{})
	go func() {
		d.StopBehaviors()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("StopBehaviors blocked when no behaviors were started")
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
