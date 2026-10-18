//go:build linux && netadmin

// The real applier against the kernel: run only where the test holds
// NET_ADMIN in a network namespace of its own (make test-netadmin runs it in
// a container), never on a host whose firewall it would rewrite.
package impair

import (
	"context"
	"errors"
	"io"
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

// serveBulk answers each connection by reading what the client sends, then
// writing n bytes back: a device a collector downloads from and uploads to.
func serveBulk(t *testing.T, addr string, n int) {
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
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c)
				_, _ = c.Write(make([]byte, n))
			}(c)
		}
	}()
}

// transfer sends up bytes, then reads until the device closes, and reports
// how long the whole exchange took: the device answers only after it has
// read everything sent.
func transfer(t *testing.T, addr string, up int) time.Duration {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	start := time.Now()
	if _, err := c.Write(make([]byte, up)); err != nil {
		t.Fatal(err)
	}
	_ = c.(*net.TCPConn).CloseWrite()
	if _, err := io.Copy(io.Discard, c); err != nil {
		t.Fatal(err)
	}
	return time.Since(start)
}

// A rate limit holds in both directions (what the collector downloads as
// well as what it sends), while the latency still counts once per round
// trip: the return path is rate limited but not delayed.
func TestNetlinkRateLimitsBothDirections(t *testing.T) {
	if os.Getenv("OMNIGLASS_NETADMIN") != "1" {
		t.Fatal("this test rewrites the network it runs in; run it through make test-netadmin")
	}
	const dev = "127.63.0.5"
	serveBulk(t, dev+":9090", 64<<10) // the device sends 64 KiB
	serveBulk(t, dev+":9091", 0)      // the device only takes
	ap := NewNetlink()
	t.Cleanup(func() { _ = ap.Clear() })
	if err := ap.Apply(PlanFor([]Device{{Addr: netip.MustParseAddr(dev), Impairment: Impairment{LatencyMs: 200, RateKbit: 256}}})); err != nil {
		t.Fatal(err)
	}
	// 64 KiB at 256 kbit/s is about two seconds each way.
	if down := transfer(t, dev+":9090", 0); down < 1500*time.Millisecond {
		t.Errorf("downloaded 64 KiB from a 256 kbit/s device in %v; the rate does not hold toward the collector", down)
	}
	if up := transfer(t, dev+":9091", 64<<10); up < 1500*time.Millisecond {
		t.Errorf("uploaded 64 KiB to a 256 kbit/s device in %v; the rate does not hold toward the device", up)
	}
	took, err := dial(dev + ":9090")
	if err != nil {
		t.Fatal(err)
	}
	if took < 190*time.Millisecond || took > 350*time.Millisecond {
		t.Errorf("a connect took %v with 200 ms of latency; want the latency once per round trip", took)
	}
}
