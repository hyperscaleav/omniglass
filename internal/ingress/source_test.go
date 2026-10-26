package ingress_test

import (
	"net/netip"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/ingress"
)

// A sender's source is the client address its TrustedProxies resolved, never
// a header's raw text. An IPv6 sender is grouped by its /64, the block one
// host holds, so rotating addresses inside it buys nothing.
func TestASourceIsTheResolvedClientGroupedForLimits(t *testing.T) {
	cases := []struct {
		name string
		addr netip.Addr
		want string
	}{
		{"an IPv4 sender is its address", netip.MustParseAddr("203.0.113.9"), "203.0.113.9"},
		{"an IPv6 sender groups by its /64", netip.MustParseAddr("2001:db8:1:2:aaaa::1"), "2001:db8:1:2::/64"},
		{"an address that did not resolve", netip.Addr{}, "unknown"},
	}
	for _, c := range cases {
		if got := ingress.Source(c.addr); got != c.want {
			t.Errorf("%s: Source(%v) = %q, want %q", c.name, c.addr, got, c.want)
		}
	}
}
