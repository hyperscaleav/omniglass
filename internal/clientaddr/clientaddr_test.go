package clientaddr_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/clientaddr"
)

// The client is the connection's peer, or, only when that peer is a trusted
// proxy, the rightmost forwarded hop that is not one. A forwarded header from
// any other peer is that peer's own claim and changes nothing.
func TestTheClientIsThePeerUnlessATrustedProxyForwardsIt(t *testing.T) {
	cases := []struct {
		name, remote, xff, want string
	}{
		{"a direct peer", "203.0.113.9:51234", "", "203.0.113.9"},
		{"a public peer's forwarded header is its own claim, ignored", "203.0.113.9:51234", "198.51.100.1", "203.0.113.9"},
		{"a private proxy forwards its client", "10.42.0.7:8080", "198.51.100.1", "198.51.100.1"},
		{"the rightmost untrusted hop, never a spoofed leftmost one", "10.42.0.7:8080", "1.2.3.4, 198.51.100.1", "198.51.100.1"},
		{"proxies chained inside the network are skipped", "127.0.0.1:9", "198.51.100.1, 10.0.0.5", "198.51.100.1"},
		{"a malformed hop stops the walk at the last good address", "10.42.0.7:8080", "198.51.100.1, not-an-ip", "10.42.0.7"},
		{"a peer with no port", "203.0.113.9", "", "203.0.113.9"},
		{"a mapped IPv4 reads as IPv4", "[::ffff:203.0.113.9]:443", "", "203.0.113.9"},
		{"an IPv6 peer keeps its whole address", "[2001:db8:1:2:aaaa::1]:443", "", "2001:db8:1:2:aaaa::1"},
	}
	for _, c := range cases {
		got, ok := clientaddr.Default.Client(c.remote, c.xff)
		if !ok || got.String() != c.want {
			t.Errorf("%s: Client(%q, %q) = %v, %v; want %q", c.name, c.remote, c.xff, got, ok, c.want)
		}
	}
	if _, ok := clientaddr.Default.Client("garbage", "198.51.100.1"); ok {
		t.Error("an unparseable peer resolved; want no client")
	}
	// A proxy that appends its own header line, after the client's forged
	// one, is read from the right all the same.
	two := clientaddr.JoinForwarded([]string{"203.0.113.66", "198.51.100.1"})
	if got, _ := clientaddr.Default.Client("10.0.0.1:1", two); got.String() != "198.51.100.1" {
		t.Errorf("two forwarded lines = %v, want the proxy-appended rightmost one", got)
	}
	// Only the header's tail is read, however large it is.
	huge := strings.Repeat("\xff", 1<<16) + ", 198.51.100.1"
	if got, _ := clientaddr.Default.Client("10.0.0.1:1", huge); got.String() != "198.51.100.1" {
		t.Errorf("a huge forwarded header = %v, want the rightmost address", got)
	}
}

// The configured list replaces the default whole: a proxy on a public range
// is believed once listed, a private peer is not once the list leaves it
// out, and "none" believes no forwarded header at all.
func TestAConfiguredListDecidesWhichPeerIsBelieved(t *testing.T) {
	public, err := clientaddr.Parse("203.0.113.0/24, ::1")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cases := []struct {
		name   string
		trust  clientaddr.Trust
		remote string
		want   string
	}{
		{"a listed public proxy forwards its client", public, "203.0.113.5:443", "198.51.100.1"},
		{"a bare address is one host", public, "[::1]:443", "198.51.100.1"},
		{"a private peer the list leaves out is the client itself", public, "10.42.0.7:8080", "10.42.0.7"},
		{"none believes no peer, not even loopback", clientaddr.Trust{}, "127.0.0.1:9", "127.0.0.1"},
	}
	for _, c := range cases {
		if got, _ := c.trust.Client(c.remote, "1.2.3.4, 198.51.100.1"); got.String() != c.want {
			t.Errorf("%s: Client = %v, want %s", c.name, got, c.want)
		}
	}
}

func TestParseReadsAListAndRefusesAnythingElse(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "127.0.0.0/8,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,::1/128,fc00::/7",
		"default":           "127.0.0.0/8,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,::1/128,fc00::/7",
		"none":              "none",
		" 10.1.2.0/24 ,::1": "10.1.2.0/24,::1/128",
		"10.1.2.3/24":       "10.1.2.0/24",
		"::ffff:10.0.0.1":   "10.0.0.1/32",
		// A mapped prefix is the IPv4 range it maps, since a peer is
		// unmapped before it is matched.
		"::ffff:10.0.0.0/104": "10.0.0.0/8",
	} {
		got, err := clientaddr.Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got.String() != want {
			t.Errorf("Parse(%q) = %q, want %q", in, got.String(), want)
		}
	}
	for _, in := range []string{"10.0.0.0/33", "proxy.example", "10.0.0.1,,10.0.0.2", "none,10.0.0.1"} {
		if _, err := clientaddr.Parse(in); err == nil {
			t.Errorf("Parse(%q) accepted; want an error naming the entry", in)
		} else if !strings.Contains(err.Error(), "OMNIGLASS_TRUSTED_PROXIES") {
			t.Errorf("Parse(%q) error %q does not name the variable", in, err)
		}
	}
}
