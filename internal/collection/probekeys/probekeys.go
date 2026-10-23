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
	// EndpointRejection is an endpoint's gate (#1056 ruling, ADR-0158): the
	// ladder rung a terminal refusal holds, its class, and the connection
	// fingerprint it holds for ("authenticated auth <fingerprint>"), or
	// "none". The worklist carries it back, so a restarted node honours it.
	EndpointRejection = "endpoint-rejection"
)

var native = []string{
	TCPOpen, TCPConnectTime, ICMPReachable, ICMPRTTAvg, HTTPResponseTime, SSHHandshakeTime, TelnetResponseTime,
	EndpointReachable, EndpointResponsive, EndpointAuthenticated, SessionState, SessionReason, SSHHostKey, EndpointRejection,
}

// nodeControl are the keys whose values steer what a node attempts: the
// gate it publishes and the host key it trusted (ADR-0158). Only a node's
// own collection writes them; the push lane refuses them.
var nodeControl = []string{EndpointRejection, SSHHostKey}

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
