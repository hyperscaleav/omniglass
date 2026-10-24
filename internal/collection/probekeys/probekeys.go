// Package probekeys declares the sample keys the native collection probes
// write (tcp, icmp, http, ssh, tls, the endpoint verdicts they roll up into,
// and a held session's state).
// A leaf, so both the probes (internal/collection) and the storage gateway's
// calc rule check (a calc rule may not write a key a collection writes) read
// this one declaration. A driver's keys are declared in its spec instead.
package probekeys

import "slices"

// The probe metrics and verdict properties. Each is a seeded metric_type or
// property_type (internal/seed); the ingest drops a name the registries lack.
const (
	TCPOpen               = "tcp-open"
	TCPConnectTime        = "tcp-connect-time"
	ICMPReachable         = "icmp-reachable"
	ICMPRTTAvg            = "icmp-rtt-avg"
	HTTPResponseTime      = "http-response-time"
	SSHHandshakeTime      = "ssh-handshake-time"
	TelnetResponseTime    = "telnet-response-time"
	EndpointReachable     = "endpoint-reachable"
	EndpointResponsive    = "endpoint-responsive"
	EndpointAuthenticated = "endpoint-authenticated"
	// SessionState is a held session's state per endpoint (#816), written
	// by the node's session machine on each transition.
	SessionState = "session-state"
	// SessionReason is why a held session is in its state (#1049), the
	// reason of its latest transition: `<class>: <device text>` when
	// rejected, so an event rule can alarm on it.
	SessionReason = "session-reason"
	// SSHHostKey is the host key an ssh session trusted (#1032): the key
	// trusted on first use and every matching key after, so the series is
	// the pin's history and a change shows as a rejection beside it.
	SSHHostKey = "ssh-host-key"
	// TLSHandshakeTime is the tls probe's handshake timing (#1035), one
	// observation per answered probe.
	TLSHandshakeTime = "tls-handshake-time"
	// TLSCertificate is the public key a tls endpoint pins (#1035): the key
	// a pin-mode connection trusted on first use (published again while the
	// worklist does not deliver it back), or the operator's declared key.
	TLSCertificate = "tls-certificate"
	// TLSCertificateExpiry is the not-after of the certificate a tls probe
	// accepted (RFC 3339), transition-only.
	TLSCertificateExpiry = "tls-certificate-expiry"
	// TLSCertificateDaysLeft is the days from the probe to that not-after,
	// one observation per probe, the number a threshold rule reads.
	TLSCertificateDaysLeft = "tls-certificate-days-left"
	// EndpointRejection is an endpoint's gate (#1056 ruling, ADR-0158): the
	// ladder rung a terminal refusal holds, its class, and the connection
	// fingerprint it holds for ("authenticated auth <fingerprint>"), or
	// "none". The worklist carries it back, so a restarted node honours it.
	EndpointRejection = "endpoint-rejection"
)

var native = []string{
	TCPOpen, TCPConnectTime, ICMPReachable, ICMPRTTAvg, HTTPResponseTime, SSHHandshakeTime, TelnetResponseTime,
	EndpointReachable, EndpointResponsive, EndpointAuthenticated, SessionState, SessionReason, SSHHostKey, EndpointRejection,
	TLSHandshakeTime, TLSCertificate, TLSCertificateExpiry, TLSCertificateDaysLeft,
}

// nodeControl are the keys only a node's own collection writes; the push
// lane refuses them. The gate it publishes and the key it trusted (an ssh
// host key, a tls certificate's public key) steer what the node attempts
// (ADR-0158); a held session's state and reason are
// the node's account of its own connection, which the console reads as such
// (#1059), so a push may neither read a rejected session up, hiding the
// refusal and its Retry, nor plant a reason the node never scrubbed.
var nodeControl = []string{EndpointRejection, SSHHostKey, TLSCertificate, SessionState, SessionReason}

// IsNodeControl reports a key only a node's own collection may write.
func IsNodeControl(key string) bool { return slices.Contains(nodeControl, key) }

// Native is every key a native probe writes, sorted, a fresh slice each call.
func Native() []string {
	out := slices.Clone(native)
	slices.Sort(out)
	return out
}

// IsNative reports whether a native probe writes key.
func IsNative(key string) bool { return slices.Contains(native, key) }
