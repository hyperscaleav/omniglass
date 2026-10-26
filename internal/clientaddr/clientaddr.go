// Package clientaddr decides who a request's client is: the connection's peer,
// or, only when that peer is a trusted proxy, the client the proxy forwarded
// (#1185). The session and token stamps and the webhook receiver's limits read
// the one answer, so they agree on who the client is.
package clientaddr

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Trust is the set of networks a forwarded header is believed from. A peer
// outside it is the client itself, and a forwarded header it sends is only its
// own claim. An empty Trust believes no forwarded header.
type Trust []netip.Prefix

// Default is the trust an unset OMNIGLASS_TRUSTED_PROXIES gives: the loopback
// and the private ranges, where the chart's ingress (Traefik, which overwrites
// the header from an untrusted client) and a node on the same host sit.
var Default = Trust{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
}

// Parse reads OMNIGLASS_TRUSTED_PROXIES: a comma-separated list of CIDRs or
// bare addresses (one host), "none" to believe no proxy, or empty (or
// "default") for Default. A list replaces the default whole. Any entry that is
// not an address or a prefix is an error, so a typo fails the boot rather than
// silently trusting less, or more, than was meant.
func Parse(s string) (Trust, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "", "default":
		return Default, nil
	case "none":
		return Trust{}, nil
	}
	var t Trust
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		p, err := parseEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("OMNIGLASS_TRUSTED_PROXIES: %q is not an address or a CIDR prefix (list them comma-separated, or say none)", entry)
		}
		t = append(t, p)
	}
	return t, nil
}

func parseEntry(entry string) (netip.Prefix, error) {
	if strings.Contains(entry, "/") {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, err
		}
		// A peer is unmapped before it is matched, so a mapped prefix is
		// read as the IPv4 range it maps, or it would never match.
		if p.Addr().Is4In6() && p.Bits() >= 96 {
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-96)
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// String is the list in Parse's own grammar, so it reads back: comma-separated,
// or "none" for an empty list.
func (t Trust) String() string {
	if len(t) == 0 {
		return "none"
	}
	parts := make([]string, len(t))
	for i, p := range t {
		parts[i] = p.String()
	}
	return strings.Join(parts, ",")
}

func (t Trust) trusts(a netip.Addr) bool {
	for _, p := range t {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// JoinForwarded joins every X-Forwarded-For line of a request in order, so a
// proxy that appends its own line (rather than extending the client's) is read
// from the right like one that extends it.
func JoinForwarded(lines []string) string { return strings.Join(lines, ", ") }

// maxForwarded bounds how much of a forwarded header is read: its rightmost
// hops are the ones a trusted proxy appended.
const maxForwarded = 512

// Client is the request's client address, given the connection's peer
// (host:port, or a bare host) and its joined X-Forwarded-For. It is the peer,
// unless the peer is trusted, in which case the forwarded header is walked from
// the right past every trusted hop to the first untrusted one (a malformed hop
// stops the walk at the last good address). A mapped IPv4 reads as IPv4. False
// when the peer does not parse.
func (t Trust) Client(remoteAddr, forwarded string) (netip.Addr, bool) {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	addr = addr.Unmap()
	if !t.trusts(addr) || forwarded == "" {
		return addr, true
	}
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
		if !t.trusts(addr) {
			break
		}
	}
	return addr, true
}
