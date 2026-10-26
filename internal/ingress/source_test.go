package ingress_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/clientaddr"
	"github.com/hyperscaleav/omniglass/internal/ingress"
)

// A sender's source is the client address the configured trust resolves
// (clientaddr), never a header's raw text. An IPv6 sender is grouped by its
// /64, the block one host holds, so rotating addresses inside it buys nothing.
func TestASourceIsTheResolvedClientGroupedForLimits(t *testing.T) {
	cases := []struct {
		name, remote, xff, want string
	}{
		{"a direct peer", "203.0.113.9:51234", "", "203.0.113.9"},
		{"a private proxy forwards its client", "10.42.0.7:8080", "1.2.3.4, 198.51.100.1", "198.51.100.1"},
		{"a public peer's forwarded header is its own claim, ignored", "203.0.113.9:51234", "198.51.100.1", "203.0.113.9"},
		{"an IPv6 sender groups by its /64", "[2001:db8:1:2:aaaa::1]:443", "", "2001:db8:1:2::/64"},
		{"an unparseable peer", "garbage", "198.51.100.1", "unknown"},
	}
	for _, c := range cases {
		if got := ingress.Source(clientaddr.Default, c.remote, c.xff); got != c.want {
			t.Errorf("%s: Source(%q, %q) = %q, want %q", c.name, c.remote, c.xff, got, c.want)
		}
	}
	// The configured trust is the one read: with none, even a loopback
	// proxy's forwarded header is ignored.
	if got := ingress.Source(clientaddr.Trust{}, "127.0.0.1:9", "198.51.100.1"); got != "127.0.0.1" {
		t.Fatalf("Source with no trusted proxy = %q, want the peer", got)
	}
}
