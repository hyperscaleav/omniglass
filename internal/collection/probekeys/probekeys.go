// Package probekeys declares the sample keys the native collection probes
// write (tcp, icmp, http, ssh, the endpoint verdicts they roll up into, and
// a held session's state).
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
	EndpointReachable     = "endpoint-reachable"
	EndpointResponsive    = "endpoint-responsive"
	EndpointAuthenticated = "endpoint-authenticated"
	// SessionState is a held session's state per endpoint (#816), written
	// by the node's session machine on each transition.
	SessionState = "session-state"
)

var native = []string{
	TCPOpen, TCPConnectTime, ICMPReachable, ICMPRTTAvg, HTTPResponseTime, SSHHandshakeTime,
	EndpointReachable, EndpointResponsive, EndpointAuthenticated, SessionState,
}

// Native is every key a native probe writes, sorted, a fresh slice each call.
func Native() []string {
	out := slices.Clone(native)
	slices.Sort(out)
	return out
}

// IsNative reports whether a native probe writes key.
func IsNative(key string) bool { return slices.Contains(native, key) }
