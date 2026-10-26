package ingress

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// TrustedProxies is the set of networks a forwarded header is believed from
// (OMNIGLASS_TRUSTED_PROXIES, #1185). It decides who a request's client is,
// for the address a session or token is stamped with and the source a pushed
// message is limited and recorded by, so the two always agree. A peer outside
// it is the client itself, and a forwarded header it sends is only its own
// claim. An empty set believes no forwarded header.
type TrustedProxies []netip.Prefix

// DefaultTrustedProxies is the set an unset OMNIGLASS_TRUSTED_PROXIES gives:
// the loopback and the private ranges, where the chart's ingress (Traefik,
// which overwrites the header from an untrusted client) and a node on the
// same host sit. Fresh on every call, so no caller can change another's.
func DefaultTrustedProxies() TrustedProxies {
	return TrustedProxies{
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("fc00::/7"),
	}
}

// ParseTrustedProxies reads OMNIGLASS_TRUSTED_PROXIES: a comma-separated list
// of CIDRs or bare addresses (one host), "none" to believe no proxy, or empty
// (or "default") for DefaultTrustedProxies. A list replaces the default
// whole. Any entry that is not an address or a prefix is an error, so a typo
// fails the boot rather than silently trusting less, or more, than was meant.
func ParseTrustedProxies(s string) (TrustedProxies, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "", "default":
		return DefaultTrustedProxies(), nil
	case "none":
		return TrustedProxies{}, nil
	}
	var t TrustedProxies
	for _, entry := range strings.Split(s, ",") {
		entry = strings.TrimSpace(entry)
		p, err := parseProxyEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("OMNIGLASS_TRUSTED_PROXIES: %q is not an address or a CIDR prefix (list them comma-separated, or say none)", entry)
		}
		t = append(t, p)
	}
	return t, nil
}

func parseProxyEntry(entry string) (netip.Prefix, error) {
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

// String is the list in ParseTrustedProxies' own grammar, so it reads back:
// comma-separated, or "none" for an empty list.
func (t TrustedProxies) String() string {
	if len(t) == 0 {
		return "none"
	}
	parts := make([]string, len(t))
	for i, p := range t {
		parts[i] = p.String()
	}
	return strings.Join(parts, ",")
}

func (t TrustedProxies) trusts(a netip.Addr) bool {
	for _, p := range t {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// maxForwarded bounds how much of a forwarded header is read: its rightmost
// hops are the ones a trusted proxy appended.
const maxForwarded = 512

// Client is the request's client address, given the connection's peer
// (host:port, or a bare host) and every X-Forwarded-For line of the request,
// in order. It is the peer, unless the peer is trusted, in which case the
// lines are read as one list (so a proxy that appends its own line, rather
// than extending the client's, is read like one that extends it) and walked
// from the right past every trusted hop to the first untrusted one. Only the
// list's last 512 bytes are read, and a malformed hop stops the walk at the
// last good address. A mapped IPv4 reads as IPv4. False when the peer does
// not parse.
func (t TrustedProxies) Client(remoteAddr string, forwarded []string) (netip.Addr, bool) {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	addr = addr.Unmap()
	list := strings.Join(forwarded, ",")
	if !t.trusts(addr) || strings.TrimSpace(list) == "" {
		return addr, true
	}
	if len(list) > maxForwarded {
		list = list[len(list)-maxForwarded:]
	}
	hops := strings.Split(list, ",")
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
