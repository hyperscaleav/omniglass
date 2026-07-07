package collection_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
)

// TestICMPPingerReal is the capability-primitive closing gate: the REAL
// NewICMPPinger against a real ICMP socket. A fake-green seam does not close the
// increment; the environment risk (opening an unprivileged SOCK_DGRAM ICMP
// socket) is the point of the probe. Loopback must echo (received > 0, an rtt,
// nil err); a TEST-NET-1 address must NOT echo, and that non-answer is DATA
// (received == 0 with a down reason and a nil error), never an error.
func TestICMPPingerReal(t *testing.T) {
	if testing.Short() {
		t.Skip("capability integration test opens a real ICMP socket")
	}
	p := collection.NewICMPPinger()
	ctx := context.Background()

	// Loopback: the node's own stack must answer its own echo.
	res, err := p.Ping(ctx, "127.0.0.1", 1, 2*time.Second)
	if err != nil {
		t.Fatalf("icmp loopback: this environment cannot open an unprivileged ICMP socket: %v", err)
	}
	if res.Received == 0 {
		t.Fatalf("icmp loopback: want at least one echo, got Received=0 (reason %q)", res.Reason)
	}
	if !res.Reason.Up() {
		t.Fatalf("icmp loopback: want a responded reason, got %q", res.Reason)
	}
	if res.AvgRTT <= 0 {
		t.Fatalf("icmp loopback: want a positive avg rtt, got %v", res.AvgRTT)
	}

	// Down-is-data: 192.0.2.1 is TEST-NET-1 (RFC 5737), reserved for documentation
	// and guaranteed not to route to a live host. The probe does not answer, and
	// that verdict is DATA: received == 0, a down reason, and a nil error.
	res, err = p.Ping(ctx, "192.0.2.1", 1, time.Second)
	if err != nil {
		t.Fatalf("icmp unreachable: a target that does not answer is data, want nil error, got %v", err)
	}
	if res.Received != 0 {
		t.Fatalf("icmp unreachable: an unroutable target must not echo, got Received=%d", res.Received)
	}
	if res.Reason == "" || res.Reason.Up() {
		t.Fatalf("icmp unreachable: want a non-empty down reason (data, not no-data), got %q", res.Reason)
	}
}
