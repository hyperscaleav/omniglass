// Package transport is the code registry of the wires the platform can speak
// over (ADR-0073): the set is a build-time fact of the binary, so it lives in
// code rather than a table, and the retired interface_type table's rows became
// these entries. The registry carries the facts every consumer reads: the
// storage gateway validates an endpoint's transport against it at write, the
// API serves it to the console picker, and the node's probe dispatch binds
// behavior to its names. The probe and dial implementations themselves live in
// internal/collection and internal/node, bound by name: this package stays a
// leaf so the storage gateway can import it without a cycle (collection
// imports storage).
package transport

import (
	"slices"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// Transport is one wire the platform can speak over. Held marks a transport
// whose driver holds a session open (establish, operate, recover, teardown)
// rather than dialing per poll; Built marks that a node-side reachability
// probe ships today, which is what the retired interface_type.built column
// recorded. Line marks a transport the line protocol rides over a held
// session: a driver's polls, commands and listeners speak lines over it, and
// its session may declare a login and terminal replies (#1049).
type Transport struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Held        bool   `json:"held"`
	Built       bool   `json:"built"`
	Line        bool   `json:"line"`
	// Rung is the transport rung of the reachability ladder (ADR-0158): a
	// connect on the transport's port, the protocol's handshake, or (where an
	// open port cannot be detected, udp) a named request answered. It is the
	// gate: a session rung is attempted only when it passes.
	Rung string `json:"rung,omitempty"`
	// TLS marks a transport whose every connection runs the TLS handshake
	// over the shared primitive (#1035, #902): its endpoint takes the
	// tls-mode input, pins the certificate's public key on first use, and
	// accepts a changed key through :retry with certificate.
	TLS bool `json:"tls,omitempty"`
	// Push marks a transport a sender pushes over rather than one a node
	// dials (#1024, ADR-1024): nothing is dialed, so it has no rung, no
	// reachability task and no ladder, and its endpoint reads last heard
	// instead of a verdict. A webhook is one, hosted by the server.
	Push bool `json:"push,omitempty"`
	// BuiltIn is the node-probe data an endpoint of this transport carries
	// once its probe has run, whether or not the device answered: the rung
	// the probe tried and the verdict it rolls up into (#1371). A push
	// transport has none, nothing being dialed.
	BuiltIn []string `json:"-"`
	// Answered is the data that lands only from an answer: a timing, or the
	// platform's count of a webhook's posts. An endpoint that is down carries
	// none of it. The lab-level check generates its expected built-in set
	// from BuiltIn, Answered and the driver's shape (ExpectedBuiltIn), so no
	// list of keys is kept beside the registry.
	Answered []string `json:"-"`
}

// registry is the one authoritative list, in the order surfaces render it:
// cheapest first. tcp is both: its reachability probe dials per poll, and the
// line protocol a driver speaks over it holds a session (#817). tls is the
// same line protocol over a TLS connection (#1035): its own entry rather than
// an option on tcp, since an endpoint is named by its transport (so a
// component can hold a raw port and a TLS one) and its rung is the handshake,
// over the TLS primitive in internal/collection that other carriers consume.
// https is the same placement for http (#902, ADR-0160's rule): a component
// may serve a plain web port beside a TLS one, and its rung is the handshake.
var registry = []Transport{
	{Name: "icmp", Description: "ICMP echo / ping reachability and RTT.", Held: false, Built: true, BuiltIn: []string{probekeys.ICMPReachable, probekeys.EndpointReachable}, Answered: []string{probekeys.ICMPRTTAvg}},
	{Name: "tcp", Description: "Raw TCP: the connect / port-open check, and the line protocol, which holds a session over it (polls, commands, and listeners share one connection).", Held: true, Built: true, Line: true, Rung: "connect", BuiltIn: []string{probekeys.TCPOpen, probekeys.EndpointReachable}, Answered: []string{probekeys.TCPConnectTime}},
	{Name: "tls", Description: "TLS: the line protocol over a TLS connection, held exactly as it is over tcp; the probe runs the handshake (responds) and reports the certificate's expiry, verified against the node's roots, pinned to the public key on first use, or (by the operator's explicit choice) not verified.", Held: true, Built: true, Line: true, Rung: "handshake", TLS: true, BuiltIn: []string{probekeys.TCPOpen, probekeys.EndpointResponsive, probekeys.EndpointReachable}, Answered: []string{probekeys.TCPConnectTime, probekeys.TLSHandshakeTime}},
	{Name: "udp", Description: "UDP: the line protocol as datagrams, one line each, stateless: the probe sends the driver's named request and an answer is the rung (an open udp port cannot be detected), a refused port reads refused, and a lost datagram is retried; each endpoint binds its own local port (its driver's listen-port input, else one the node picks), which its requests, replies and pushes share.", Held: false, Built: true, Rung: "request", BuiltIn: []string{probekeys.EndpointResponsive, probekeys.EndpointReachable}, Answered: []string{probekeys.UDPResponseTime}},
	{Name: "telnet", Description: "Telnet: the line protocol over TCP with option negotiation (every option refused), its login the session's auth step; the probe connects and waits for the first negotiation or banner bytes (responds).", Held: true, Built: true, Line: true, Rung: "connect", BuiltIn: []string{probekeys.TCPOpen, probekeys.EndpointResponsive, probekeys.EndpointReachable}, Answered: []string{probekeys.TCPConnectTime, probekeys.TelnetResponseTime}},
	{Name: "ssh", Description: "SSH: the probe runs a real key exchange (responds) and tries the endpoint credential when one is set (auth); the line protocol holds a shell or exec channel over it, its userauth the session's auth step and its host key pinned on first use.", Held: true, Built: true, Line: true, Rung: "handshake", BuiltIn: []string{probekeys.TCPOpen, probekeys.EndpointResponsive, probekeys.EndpointReachable}, Answered: []string{probekeys.TCPConnectTime, probekeys.SSHHandshakeTime}},
	{Name: "http", Description: "HTTP: a driver's polls and commands send real requests (method, path, query, headers, body) and read JSON, XML, HTML or text answers, authenticated by the endpoint's declared scheme (basic, bearer, an API key, digest) or a login whose session the node keeps; a declared server-sent events stream is held as a session, its events landing through the listeners; the probe draws a real response off the API without a credential (responds), any status counting as an answer.", Held: true, Built: true, Rung: "connect", BuiltIn: []string{probekeys.TCPOpen, probekeys.EndpointResponsive, probekeys.EndpointReachable}, Answered: []string{probekeys.TCPConnectTime, probekeys.HTTPResponseTime}},
	{Name: "https", Description: "HTTPS: http over a TLS connection, its own entry so a component can serve a plain web port beside a TLS one, a server-sent events stream held over it as over http; the probe runs the handshake (responds), reports the certificate's expiry, then draws a response, the certificate verified against the node's roots, pinned to the public key on first use, or (by the operator's explicit choice) not verified.", Held: true, Built: true, Rung: "handshake", TLS: true, BuiltIn: []string{probekeys.TCPOpen, probekeys.EndpointResponsive, probekeys.EndpointReachable}, Answered: []string{probekeys.TCPConnectTime, probekeys.TLSHandshakeTime, probekeys.HTTPResponseTime}},
	{Name: "websocket", Description: "WebSocket: a held session whose messages are text frames, routed as a line session's are: a frame sent to arm a subscription on every connection, pushes landing through the listeners, and a request answered by the frame carrying back an id the node mints into it, whatever pushes interleave; the upgrade rides the endpoint's scheme (basic, bearer, an API key) or a login whose cookie the node keeps, and the session is kept alive by ping and pong; the probe is http's, a credential-free GET / whose answer, any status, is the rung (responds), so reachability stays observed beneath a refused upgrade.", Held: true, Built: true, Line: true, Rung: "connect", BuiltIn: []string{probekeys.TCPOpen, probekeys.EndpointResponsive, probekeys.EndpointReachable}, Answered: []string{probekeys.TCPConnectTime, probekeys.HTTPResponseTime}},
	{Name: "wss", Description: "WSS: websocket over a TLS connection, its own entry so a component can serve a plain socket beside a TLS one; the probe is https's: it runs the handshake (responds), reports the certificate's expiry, then draws a credential-free response, the certificate verified against the node's roots, pinned to the public key on first use, or (by the operator's explicit choice) not verified.", Held: true, Built: true, Line: true, Rung: "handshake", TLS: true, BuiltIn: []string{probekeys.TCPOpen, probekeys.EndpointResponsive, probekeys.EndpointReachable}, Answered: []string{probekeys.TCPConnectTime, probekeys.TLSHandshakeTime, probekeys.HTTPResponseTime}},
	{Name: "snmp", Description: "SNMP v2c, stateless: the probe gets a standard OID (sysUpTime.0 unless the driver names another) with the endpoint's community, and an answer is the rung; a wrong community is dropped silently and reads as no answer, while an authorizationError reads as an auth refusal.", Held: false, Built: true, Rung: "request", BuiltIn: []string{probekeys.EndpointResponsive, probekeys.EndpointReachable}, Answered: []string{probekeys.SNMPResponseTime}},
	{Name: "webhook", Description: "Webhook: a push transport the server hosts, not one a node dials: a component or a cloud service posts each message to the endpoint's own URL (/api/v1/webhooks/ and a token the server mints, held as a secret), authenticated by the scheme its driver declares (an hmac signature over a signed timestamp, a token, basic, or none, which reads unsigned) and mapped by its listeners onto any lane; nothing is dialed, so it has no reachability ladder and reads last heard.", Held: false, Built: true, Push: true, Answered: []string{writerclass.WebhookPostCount}},
}

// byName indexes the registry; built once at init since the registry is
// immutable for the life of the binary.
var byName = func() map[string]Transport {
	m := make(map[string]Transport, len(registry))
	for _, t := range registry {
		m[t.Name] = t
	}
	return m
}()

// All returns every transport in render order. The slice is a copy: callers
// may sort or mutate it without corrupting the registry.
func All() []Transport {
	out := make([]Transport, len(registry))
	copy(out, registry)
	return out
}

// UsesTLS reports whether a transport's connections run the TLS handshake
// (tls, https): false for an unknown name.
func UsesTLS(name string) bool {
	t, ok := byName[name]
	return ok && t.TLS
}

// IsPush reports a push transport (#1024): false for an unknown name.
func IsPush(name string) bool {
	t, ok := byName[name]
	return ok && t.Push
}

// ByName resolves one transport; ok is false for a name the binary does not
// speak, which is what makes an unknown transport a 422 at write rather than
// a silent row.
func ByName(name string) (Transport, bool) {
	t, ok := byName[name]
	return t, ok
}

// DriverShape is what a driver declares that decides which collection
// telemetry (#1369) its endpoint reports: its polls, its listeners, and
// whether its spec carries a session block.
type DriverShape struct {
	Polls     bool
	Listeners bool
	Session   bool
}

// DriverBuiltIn is the collection telemetry a driver of this shape adds to an
// endpoint over the transport: a poll duration for a driver that polls, a push
// count for a held or datagram endpoint whose driver listens, and a session
// state for an endpoint whose session is held. The session rule mirrors the
// node's (a baked driver over a held transport other than http and https, and
// over tcp only with a session block); webhook listeners run on the server, so
// it adds none.
func DriverBuiltIn(name string, shape DriverShape) []string {
	t, ok := byName[name]
	if !ok || t.Push {
		return nil
	}
	var out []string
	if shape.Polls {
		out = append(out, probekeys.PollDuration)
	}
	holds := t.Held && name != "http" && name != "https" && (shape.Session || name != "tcp")
	if holds && (shape.Polls || shape.Listeners || shape.Session) {
		out = append(out, probekeys.SessionState)
	}
	if shape.Listeners && (t.Held || name == "udp") {
		out = append(out, probekeys.PushMessageCount)
	}
	return out
}

// ExpectedBuiltIn is every built-in key an endpoint of this transport and
// driver shape carries: the transport's BuiltIn, and, once the endpoint
// answers, its Answered set and the driver's telemetry; a down endpoint polls
// nothing, so it owes no poll duration. An unknown transport expects none.
func ExpectedBuiltIn(name string, shape DriverShape, answered bool) []string {
	t, ok := byName[name]
	if !ok {
		return nil
	}
	out := slices.Clone(t.BuiltIn)
	if answered {
		out = append(out, t.Answered...)
		out = append(out, DriverBuiltIn(name, shape)...)
	}
	return out
}
