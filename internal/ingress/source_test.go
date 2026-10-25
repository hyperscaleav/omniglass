package ingress_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/ingress"
)

// A sender's source is a parsed address, never a header's raw text: the
// connection's peer, or, only when that peer is a proxy on a private or
// loopback network (the chart's ingress), the rightmost forwarded hop that
// is not one. An IPv6 sender is grouped by its /64, the block one host
// holds, so rotating addresses inside it buys nothing.
func TestASourceIsAParsedAddressHonouringOnlyATrustedProxy(t *testing.T) {
	cases := []struct {
		name, remote, xff, want string
	}{
		{"a direct peer", "203.0.113.9:51234", "", "203.0.113.9"},
		{"a public peer's forwarded header is its own claim, ignored", "203.0.113.9:51234", "198.51.100.1", "203.0.113.9"},
		{"a private proxy forwards its client", "10.42.0.7:8080", "198.51.100.1", "198.51.100.1"},
		{"the rightmost untrusted hop, never a spoofed leftmost one", "10.42.0.7:8080", "1.2.3.4, 198.51.100.1", "198.51.100.1"},
		{"proxies chained inside the network are skipped", "127.0.0.1:9", "198.51.100.1, 10.0.0.5", "198.51.100.1"},
		{"a malformed hop stops the walk at the last good address", "10.42.0.7:8080", "198.51.100.1, not-an-ip", "10.42.0.7"},
		{"an IPv6 sender groups by its /64", "[2001:db8:1:2:aaaa::1]:443", "", "2001:db8:1:2::/64"},
		{"a mapped IPv4 reads as IPv4", "[::ffff:203.0.113.9]:443", "", "203.0.113.9"},
		{"an unparseable peer", "garbage", "198.51.100.1", "unknown"},
	}
	for _, c := range cases {
		if got := ingress.Source(c.remote, c.xff); got != c.want {
			t.Errorf("%s: Source(%q, %q) = %q, want %q", c.name, c.remote, c.xff, got, c.want)
		}
	}
	// A proxy that appends its own header line, after the client's forged
	// one, is read from the right all the same.
	if got := ingress.Source("10.0.0.1:1", ingress.JoinForwarded([]string{"203.0.113.66", "198.51.100.1"})); got != "198.51.100.1" {
		t.Fatalf("two forwarded lines = %q, want the proxy-appended rightmost one", got)
	}
	// A header of any size is never kept or stored: the source stays an
	// address.
	huge := strings.Repeat("\xff", 1<<16) + ", 198.51.100.1"
	if got := ingress.Source("10.0.0.1:1", huge); got != "198.51.100.1" {
		t.Fatalf("a huge forwarded header = %q, want the rightmost address", got)
	}
}
