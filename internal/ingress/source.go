package ingress

import "github.com/hyperscaleav/omniglass/internal/clientaddr"

// Source is the sender a host limits, counts and records a message by
// (#1024): the client address the configured trust resolves (#1185), never a
// header's raw text. An IPv6 sender is its /64. An unparseable peer is
// "unknown".
func Source(trust clientaddr.Trust, remoteAddr, forwarded string) string {
	addr, ok := trust.Client(remoteAddr, forwarded)
	if !ok {
		return "unknown"
	}
	if addr.Is6() {
		p, _ := addr.Prefix(64)
		return p.String()
	}
	return addr.String()
}
