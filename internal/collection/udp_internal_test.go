package collection

import (
	"net/netip"
	"testing"
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
