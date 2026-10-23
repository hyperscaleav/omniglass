package driver

import (
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/transport"
)

// Ladder is the reachability ladder a driver declares (#1056 ruling,
// ADR-0158), bottom up on the existing rungs: reachable (a ping,
// informational only: an open, answering transport is what counts, so a
// ping never gates), responsive (the transport rung, which is the gate) and
// authenticated (the session rung, attempted only when the transport rung
// passes). Every field is optional: the transport registry supplies the
// defaults, so most drivers declare nothing. A layer-2 presence rung below
// the ping is #1062; it would slot in beneath Ping, informational the same
// way.
type Ladder struct {
	// Ping is the reachable rung: whether to ping the component at all,
	// informational only. A cloud API is never pinged.
	Ping *PingRung `json:"ping,omitempty"`
	// Transport is the transport rung, the gate.
	Transport *TransportRung `json:"transport,omitempty"`
	// Session is the session rung (the login).
	Session *SessionRung `json:"session,omitempty"`
}

// PingRung declares the informational ping. Each rung is an object so the
// per-family measurement toggles and a burst count and interval (#1063, and
// the path rung of #1064) join it without reshaping the ladder.
type PingRung struct {
	// Enabled defaults to true; false never pings.
	Enabled *bool `json:"enabled,omitempty"`
}

// TransportRung declares the transport rung: a connect on the transport's
// port, the protocol's handshake (ssh key exchange, tls), or a named request
// answered, where an open port cannot be detected (udp, snmp).
type TransportRung struct {
	Rung    string   `json:"rung,omitempty"`
	Request *Request `json:"request,omitempty"`
}

// SessionRung declares whether a session rung (a login) exists.
type SessionRung struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// The transport rung kinds.
const (
	RungConnect   = "connect"
	RungHandshake = "handshake"
	RungRequest   = "request"
)

// rungsOf is what each transport's rung can be.
var rungsOf = map[string][]string{
	"tcp":    {RungConnect, RungRequest},
	"telnet": {RungConnect},
	"ssh":    {RungHandshake, RungConnect},
	"http":   {RungConnect, RungRequest},
	"udp":    {RungRequest},
	"snmp":   {RungRequest},
}

// EffectiveLadder is a resolved ladder: every rung decided.
type EffectiveLadder struct {
	Ping      bool     `json:"ping"`
	Transport string   `json:"transport"`
	Request   *Request `json:"request,omitempty"`
	Session   bool     `json:"session"`
}

// ResolveLadder merges a driver's declared ladder (nil for none, or a bare
// endpoint) over the transport registry's defaults.
func ResolveLadder(transportName string, l *Ladder, session *SessionSpec) EffectiveLadder {
	out := EffectiveLadder{Ping: true}
	if t, ok := transport.ByName(transportName); ok {
		out.Transport = t.Rung
	}
	out.Session = transportName == "ssh" || (session != nil && (session.Login != nil || session.Credential != ""))
	if l == nil {
		return out
	}
	if l.Ping != nil && l.Ping.Enabled != nil {
		out.Ping = *l.Ping.Enabled
	}
	if l.Transport != nil {
		if l.Transport.Rung != "" {
			out.Transport = l.Transport.Rung
		}
		out.Request = l.Transport.Request
	}
	if l.Session != nil && l.Session.Enabled != nil {
		out.Session = *l.Session.Enabled
	}
	return out
}

// validateLadder refuses a ladder whose transport rung the transport cannot
// run, a request rung with no request, or a session declared absent beside a
// login the driver declares.
func (s *Spec) validateLadder() error {
	l := s.Ladder
	if l == nil {
		return nil
	}
	var req *Request
	if t := l.Transport; t != nil {
		req = t.Request
		if t.Rung != "" {
			allowed := false
			for _, r := range rungsOf[s.Transport] {
				allowed = allowed || r == t.Rung
			}
			if !allowed {
				return fmt.Errorf("driver: ladder transport rung %q is not one %q can run (it runs %v)", t.Rung, s.Transport, rungsOf[s.Transport])
			}
		}
	}
	rung := ResolveLadder(s.Transport, l, s.Session).Transport
	if rung == RungRequest {
		if req == nil || req.Empty() {
			return fmt.Errorf("driver: a request transport rung names its request (ladder.transport.request)")
		}
		// The node runs a declared request rung as a line over tcp today; an
		// snmp request is #821's, a udp datagram #1037's.
		if s.Transport != "tcp" || req.Line == "" {
			return fmt.Errorf("driver: a request rung runs as a line over tcp today; snmp's own request is #821, udp's #1037")
		}
		if err := validateReply("ladder request", *req); err != nil {
			return err
		}
	} else if req != nil {
		return fmt.Errorf("driver: ladder.transport.request is the request rung's; this ladder's transport rung is %s", rung)
	}
	if l.Session != nil && l.Session.Enabled != nil && !*l.Session.Enabled && (s.Transport == "ssh" || (s.Session != nil && (s.Session.Login != nil || s.Session.Credential != ""))) {
		return fmt.Errorf("driver: the ladder declares no session rung, but the driver's session authenticates")
	}
	return nil
}
