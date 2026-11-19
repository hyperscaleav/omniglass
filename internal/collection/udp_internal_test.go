package collection

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestPreferIPv4UnmapsAndPrefersTheIPv4Address(t *testing.T) {
	for _, tc := range []struct {
		name string
		ips  []string
		want string
	}{
		// A dual-stack localhost can answer ::1 first and 127.0.0.1 mapped.
		{"mapped ipv4 after ipv6", []string{"::1", "::ffff:127.0.0.1"}, "127.0.0.1"},
		{"plain ipv4 after ipv6", []string{"::1", "127.0.0.1"}, "127.0.0.1"},
		{"mapped ipv4 alone", []string{"::ffff:10.0.0.5"}, "10.0.0.5"},
		{"ipv6 only", []string{"2001:db8::1", "::1"}, "2001:db8::1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ips := make([]netip.Addr, len(tc.ips))
			for i, s := range tc.ips {
				ips[i] = netip.MustParseAddr(s)
			}
			if got := preferIPv4(ips).String(); got != tc.want {
				t.Fatalf("preferIPv4(%v) = %s, want %s", tc.ips, got, tc.want)
			}
		})
	}
}

func TestRetargetDropsTheOldAddressesGhost(t *testing.T) {
	if testing.Short() {
		t.Skip("real-socket integration test")
	}
	x, _ := net.ResolveUDPAddr("udp4", "127.0.0.1:5000")
	y, _ := net.ResolveUDPAddr("udp4", "127.0.0.2:5000")
	peer := x
	c, err := ListenDatagram(DatagramConfig{
		Target:  "component.test:5000",
		Resolve: func(context.Context, string, string) (*net.UDPAddr, error) { return peer, nil },
	})
	if err != nil {
		t.Fatalf("listen datagram: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	c.mu.Lock()
	c.ghost = &datagramGhost{answer: "INPUT hdmi1", remaining: 1, until: time.Now().Add(time.Minute)}
	c.mu.Unlock()

	// Unmoved: the ghost still guards against the same address's surplus.
	if _, _, moved, err := c.Retarget(context.Background()); moved || err != nil {
		t.Fatalf("retarget of an unmoved name: moved=%v err=%v", moved, err)
	}
	c.mu.Lock()
	kept := c.ghost != nil
	c.mu.Unlock()
	if !kept {
		t.Fatal("a retarget that did not move dropped the ghost")
	}

	peer = y
	if _, _, moved, err := c.Retarget(context.Background()); !moved || err != nil {
		t.Fatalf("retarget of a moved name: moved=%v err=%v", moved, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ghost != nil {
		t.Fatal("a moved name kept the old address's ghost, which could swallow the new address's reply")
	}
}
