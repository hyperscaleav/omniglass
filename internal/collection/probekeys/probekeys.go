// Package probekeys declares the sample keys the native collection probes
// write (tcp, icmp, http, ssh, the endpoint verdicts they roll up into, and
// a held session's state), and who may write each.
// A leaf, so the probes (internal/collection), the storage gateway's calc rule
// check (a calc rule may not write a key a collection writes) and the push
// lane (which refuses a key only a node may write) read this one declaration.
// A driver's keys are declared in its spec instead.
package probekeys

import (
	"maps"
	"slices"
	"strings"
)

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

// Every constant above is ruled exactly once, in nodeOnly or in pushable
// (TestEveryKeyRuled reads them from this file and fails on a key in neither),
// so a new key cannot ship pushable by omission.

// nodeOnly are the native keys whose value only a node's own probe can know
// (#1080): the reachability verdicts and their rungs, the probe metrics and
// their timings, and the node's account of its own connection (a held
// session's state and reason, the host key it trusted, the gate it
// publishes). The console reads each as the node's observation, and the gate
// and the host key steer what the node attempts (ADR-0158), so the push lane
// refuses every one by name: a push may not forge a verdict or a timing, plant
// or lift a gate, or read a rejected session up.
var nodeOnly = []string{
	TCPOpen, TCPConnectTime, ICMPReachable, ICMPRTTAvg, HTTPResponseTime, SSHHandshakeTime, TelnetResponseTime,
	EndpointReachable, EndpointResponsive, EndpointAuthenticated, SessionState, SessionReason, SSHHostKey, EndpointRejection,
}

// pushable are the native keys a push may write too, each mapped to the
// one-line reason it may. None today: every native value is one only the
// probing node observes. A key moves here from nodeOnly only with its reason,
// and TestTelemetryPushRefusesNativeKeys then proves the push lane accepts it.
var pushable = map[string]string{}

// native is every key a native probe writes, whoever else may write it.
var native = slices.Concat(nodeOnly, slices.Collect(maps.Keys(pushable)))

// IsNodeOnly reports a native key only a node's own collection may write,
// which the push lane refuses. It matches case-insensitively and ignores
// surrounding space, failing closed: a variant the registry would refuse as
// unregistered today is refused as node-only should that lookup ever loosen.
func IsNodeOnly(key string) bool {
	key = strings.TrimSpace(key)
	return slices.ContainsFunc(nodeOnly, func(k string) bool { return strings.EqualFold(k, key) })
}

// Native is every key a native probe writes, sorted, a fresh slice each call.
func Native() []string {
	out := slices.Clone(native)
	slices.Sort(out)
	return out
}

// IsNative reports whether a native probe writes key.
func IsNative(key string) bool { return slices.Contains(native, key) }
