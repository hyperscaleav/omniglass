//go:build linux && netadmin

// The real applier against the kernel: run only where the test holds
// NET_ADMIN in a network namespace of its own (make test-netadmin runs it in
// a container), never on a host whose firewall it would rewrite.
package impair

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

func listen(t *testing.T, addr string) {
	t.Helper()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
}

// dial reports how long a TCP connect took, and whether it timed out rather
// than being refused.
func dial(addr string) (time.Duration, error) {
	start := time.Now()
	c, err := net.DialTimeout("tcp", addr, 1500*time.Millisecond)
	if err == nil {
		c.Close()
	}
	return time.Since(start), err
}

// pings sends one unprivileged echo, as the collector's pinger does.
func pings(t *testing.T, target string) bool {
	t.Helper()
	p, err := probing.NewPinger(target)
	if err != nil {
		t.Fatalf("ping %s: %v", target, err)
	}
	p.Count, p.Timeout = 1, time.Second
	p.SetPrivileged(false)
	if err := p.RunWithContext(context.Background()); err != nil {
		t.Fatalf("ping %s: %v", target, err)
	}
	return p.Statistics().PacketsRecv > 0
}

func TestNetlinkApplierBreaksAndRestoresDevices(t *testing.T) {
	if os.Getenv("OMNIGLASS_NETADMIN") != "1" {
		t.Fatal("this test rewrites the network it runs in; run it through make test-netadmin")
	}
	a, b := netip.MustParseAddr("127.61.0.1"), netip.MustParseAddr("127.61.0.2")
	listen(t, "127.61.0.1:8080")
	listen(t, "127.61.0.2:8080")
	ap := NewNetlink()
	t.Cleanup(func() { _ = ap.Clear() })

	// Before: everything answers, including an address with no device.
	if !pings(t, "127.61.0.99") {
		t.Fatal("precondition: loopback answers ping for any address")
	}

	if err := ap.Apply(PlanFor([]Device{
		{Addr: a, Impairment: Impairment{LatencyMs: 200}},
		{Addr: b, Impairment: Impairment{Unplugged: true}},
	})); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Unplugged: dark on TCP (a timeout, not a refusal) and on ICMP.
	if took, err := dial("127.61.0.2:8080"); err == nil || !isTimeout(err) {
		t.Errorf("unplugged device: connect took %v with %v, want a timeout", took, err)
	}
	if pings(t, "127.61.0.2") {
		t.Error("an unplugged device answered ping")
	}
	// Ping tells the truth: a device answers, an empty address does not.
	if !pings(t, "127.61.0.1") {
		t.Error("a served device stopped answering ping")
	}
	if pings(t, "127.61.0.99") {
		t.Error("an address with no device answered ping")
	}
	// Latency: the connect pays the device's delay.
	if took, err := dial("127.61.0.1:8080"); err != nil || took < 180*time.Millisecond {
		t.Errorf("delayed device: connect took %v (%v), want at least the 200ms delay", took, err)
	}

	// Total loss cuts the device off; a re-apply replaces the old state.
	if err := ap.Apply(PlanFor([]Device{{Addr: a, Impairment: Impairment{LossPct: 100}}, {Addr: b}})); err != nil {
		t.Fatalf("re-apply: %v", err)
	}
	if _, err := dial("127.61.0.1:8080"); err == nil {
		t.Error("a device at 100% loss accepted a connection")
	}
	if took, err := dial("127.61.0.2:8080"); err != nil || took > 100*time.Millisecond {
		t.Errorf("a replugged device: connect took %v (%v)", took, err)
	}

	// Clear restores the network as it was.
	if err := ap.Clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := dial("127.61.0.1:8080"); err != nil {
		t.Errorf("after clear: %v", err)
	}
	if !pings(t, "127.61.0.99") {
		t.Error("after clear, loopback no longer answers for any address")
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
