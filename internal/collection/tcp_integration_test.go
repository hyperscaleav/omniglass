package collection_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/nettest"
)

// TestTCPDialerReal is the capability-primitive closing gate: the REAL
// NewTCPDialer against real sockets. A fake-green seam does not close the
// increment; the environment risk (a real connect) is the point of the probe.
// An open listener must read Responded (open, connect_time > 0); a port nothing
// listens on must read Refused (down), and a failed connect is DATA (nil err),
// not an error.
func TestTCPDialerReal(t *testing.T) {
	if testing.Short() {
		t.Skip("capability integration test opens real sockets")
	}
	d := collection.NewTCPDialer()
	ctx := context.Background()

	// Open: a live listener accepts the connect.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	openAddr := ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	ms, reach, err := d.Dial(ctx, openAddr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial open: unexpected error %v", err)
	}
	if !reach.Up() {
		t.Fatalf("dial open: want Up (responded), got %q", reach)
	}
	if ms < 0 {
		t.Fatalf("dial open: connect_ms should be non-negative, got %v", ms)
	}

	// Refused: a port held for the test that nothing listens on, so the
	// connect is actively refused (a RST) rather than timing out.
	_, reach, err = d.Dial(ctx, nettest.RefusedAddr(t), 2*time.Second)
	if err != nil {
		t.Fatalf("dial refused: a failed connect is data, want nil error, got %v", err)
	}
	if reach.Up() {
		t.Fatalf("dial refused: want down, got Up")
	}
	if reach != collection.Refused {
		t.Fatalf("dial refused: want reason %q, got %q", collection.Refused, reach)
	}
}
