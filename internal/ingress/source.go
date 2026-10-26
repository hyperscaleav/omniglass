package ingress

import "net/netip"

// Source is the sender a host limits, counts and records a message by
// (#1024): the client address its TrustedProxies resolved (#1185), never a
// header's raw text. An IPv6 sender is its /64, the block one host holds, so
// rotating addresses inside it buys nothing. An address that did not resolve
// is "unknown".
func Source(client netip.Addr) string {
	if !client.IsValid() {
		return "unknown"
	}
	if client.Is6() {
		p, _ := client.Prefix(64)
		return p.String()
	}
	return client.String()
}
