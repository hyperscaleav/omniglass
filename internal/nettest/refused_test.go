//go:build linux

package nettest

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

// A dial to a refused address is refused, not timed out and not answered.
func TestARefusedAddrRefusesADial(t *testing.T) {
	addr := RefusedAddr(t)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("RefusedAddr = %q, want a loopback address", addr)
	}
	c, err := net.Dial("tcp", addr)
	if err == nil {
		_ = c.Close()
		t.Fatalf("a dial to %s connected", addr)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("dial %s = %v, want connection refused", addr, err)
	}
}

// The race the helper closes (#1089), with the competing process injected
// into the gap: a port picked by listening on :0 and closing is free the
// moment it closes, so another listener takes it and the "refused" dial is
// answered.
func TestAReleasedPortCanBeTakenInTheGap(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	released := ln.Addr().String()
	_ = ln.Close()
	squatter, err := net.Listen("tcp", released)
	if errors.Is(err, syscall.EADDRINUSE) {
		return // a concurrent process took it first, which is the race itself
	}
	if err != nil {
		t.Fatalf("take the released %s: %v", released, err)
	}
	t.Cleanup(func() { _ = squatter.Close() })
	c, err := net.Dial("tcp", released)
	if err != nil {
		t.Fatalf("a dial to the taken %s = %v, want it answered by the squatter", released, err)
	}
	_ = c.Close()
}

// Nothing on the host can listen where a dial to a refused address would
// land, whatever it binds and however it sets its socket options, so the dial
// stays refused for the whole test.
func TestNoListenerCanTakeARefusedAddr(t *testing.T) {
	addr := RefusedAddr(t)
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, network, addr string
		reuse               bool
	}{
		{"the address itself", "tcp", addr, false},
		{"the address over tcp4", "tcp4", addr, false},
		{"the IPv4 wildcard", "tcp4", net.JoinHostPort("0.0.0.0", port), false},
		{"the dual-stack wildcard", "tcp", ":" + port, false},
		{"the address with SO_REUSEPORT", "tcp", addr, true},
		{"the dual-stack wildcard with SO_REUSEPORT", "tcp", ":" + port, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			lc := net.ListenConfig{}
			if c.reuse {
				lc.Control = reusePort
			}
			ln, err := lc.Listen(context.Background(), c.network, c.addr)
			if err == nil {
				_ = ln.Close()
				t.Fatalf("listen %s %s succeeded beside the held %s", c.network, c.addr, addr)
			}
			if !errors.Is(err, syscall.EADDRINUSE) {
				t.Fatalf("listen %s %s = %v, want address in use", c.network, c.addr, err)
			}
		})
	}
	if c, err := net.Dial("tcp", addr); err == nil {
		_ = c.Close()
		t.Fatalf("a dial to %s connected after the takeover attempts", addr)
	}
}

// reusePort sets SO_REUSEPORT (and Go's default SO_REUSEADDR stays on), the
// options a listener sets to share a port.
func reusePort(_, _ string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
	}); err != nil {
		return err
	}
	return serr
}

// soReusePort is SO_REUSEPORT on Linux, which package syscall does not name.
const soReusePort = 0xf
