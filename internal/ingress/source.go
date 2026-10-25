package ingress

import (
	"net"
	"net/netip"
	"strings"
)

// trustedProxies are the networks a forwarded header is believed from: the
// loopback and the private ranges, where the chart's ingress (Traefik, which
// overwrites the header from an untrusted client) and a node on the same
// host sit. A peer outside them is the sender itself, and a forwarded header
// it sends is only its own claim.
var trustedProxies = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
}

func trusted(a netip.Addr) bool {
	for _, p := range trustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// JoinForwarded joins every X-Forwarded-For line of a request in order, so
// a proxy that appends its own line (rather than extending the client's)
// is read from the right like one that extends it.
func JoinForwarded(lines []string) string { return strings.Join(lines, ", ") }

// maxForwarded bounds how much of a forwarded header is read: its rightmost
// hops are the ones a trusted proxy appended.
const maxForwarded = 512

// Source is the sender a host limits, counts and records a message by
// (#1024): a parsed address, never a header's raw text. It is the
// connection's peer, unless the peer is a trusted proxy, in which case the
// forwarded header is walked from the right past every trusted hop to the
// first untrusted one (a malformed hop stops the walk at the last good
// address). An IPv6 sender is its /64. An unparseable peer is "unknown".
func Source(remoteAddr, forwarded string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown"
	}
	addr = addr.Unmap()
	if trusted(addr) && forwarded != "" {
		if len(forwarded) > maxForwarded {
			forwarded = forwarded[len(forwarded)-maxForwarded:]
		}
		hops := strings.Split(forwarded, ",")
		for i := len(hops) - 1; i >= 0; i-- {
			a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				break
			}
			addr = a.Unmap()
			if !trusted(addr) {
				break
			}
		}
	}
	if addr.Is6() {
		p, _ := addr.Prefix(64)
		return p.String()
	}
	return addr.String()
}
