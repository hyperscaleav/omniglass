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
	// The built-in data its endpoints always carry (#1371): what Omniglass
	// knows about an endpoint itself, which every endpoint gets whatever its
	// driver. BuiltIn is the probe's, on every endpoint of the transport;
	// the rest is what an attach adds: SessionBuiltIn where a driver holds a
	// session over it, CredentialBuiltIn where the endpoint carries a
	// credential, PollBuiltIn where its driver polls, ListenBuiltIn where it
	// listens. A key that lands only when nonzero, only on a failure, or only
	// when something answers in a way it need not (a silent telnet port) is
	// none of these. Expected is the union an endpoint is held to.
	BuiltIn           []string `json:"built_in,omitempty"`
	SessionBuiltIn    []string `json:"session_built_in,omitempty"`
	CredentialBuiltIn []string `json:"credential_built_in,omitempty"`
	PollBuiltIn       []string `json:"poll_built_in,omitempty"`
	ListenBuiltIn     []string `json:"listen_built_in,omitempty"`
}

// Attach is what an endpoint's attach adds to its built-in data: a driver
// (which holds a session over a held line transport), a credential (a
// secret input), polls and listeners.
type Attach struct {
	Driver, Credential, Polls, Listeners bool
}

// Expected is the built-in keys an endpoint of this transport with this
// attach always carries once it has run: the transport's own, and what the
// attach adds. Sorted, no duplicates.
func (t Transport) Expected(a Attach) []string {
	keys := append([]string(nil), t.BuiltIn...)
	if a.Driver {
		keys = append(keys, t.SessionBuiltIn...)
	}
	if a.Credential {
		keys = append(keys, t.CredentialBuiltIn...)
	}
	if a.Driver && a.Polls {
		keys = append(keys, t.PollBuiltIn...)
	}
	if a.Driver && a.Listeners {
		keys = append(keys, t.ListenBuiltIn...)
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// The built-in key sets the registry composes (#1371).
var (
	reached   = []string{probekeys.EndpointReachable}
	connected = []string{probekeys.TCPOpen, probekeys.TCPConnectTime}
	answered  = []string{probekeys.EndpointResponsive}
	tlsKeys   = []string{probekeys.TLSHandshakeTime, probekeys.TLSCertificate, probekeys.TLSCertificateExpiry, probekeys.TLSCertificateDaysLeft}
	httpKeys  = []string{probekeys.HTTPResponseTime}
	session   = []string{probekeys.SessionState, probekeys.SessionReason}
	authed    = []string{probekeys.EndpointAuthenticated}
	polled    = []string{probekeys.PollDuration}
	pushed    = []string{probekeys.PushMessageCount}
)

func keys(sets ...[]string) []string { return slices.Concat(sets...) }

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
	{Name: "icmp", Description: "ICMP echo / ping reachability and RTT.", Held: false, Built: true, BuiltIn: keys(reached, []string{probekeys.ICMPReachable, probekeys.ICMPRTTAvg})},
	{Name: "tcp", Description: "Raw TCP: the connect / port-open check, and the line protocol, which holds a session over it (polls, commands, and listeners share one connection).", Held: true, Built: true, Line: true, Rung: "connect", BuiltIn: keys(reached, connected), SessionBuiltIn: session, CredentialBuiltIn: authed, PollBuiltIn: polled, ListenBuiltIn: pushed},
	{Name: "tls", Description: "TLS: the line protocol over a TLS connection, held exactly as it is over tcp; the probe runs the handshake (responds) and reports the certificate's expiry, verified against the node's roots, pinned to the public key on first use, or (by the operator's explicit choice) not verified.", Held: true, Built: true, Line: true, Rung: "handshake", TLS: true, BuiltIn: keys(reached, answered, connected, tlsKeys), SessionBuiltIn: session, CredentialBuiltIn: authed, PollBuiltIn: polled, ListenBuiltIn: pushed},
	{Name: "udp", Description: "UDP: the line protocol as datagrams, one line each, stateless: the probe sends the driver's named request and an answer is the rung (an open udp port cannot be detected), a refused port reads refused, and a lost datagram is retried; each endpoint binds its own local port (its driver's listen-port input, else one the node picks), which its requests, replies and pushes share.", Held: false, Built: true, Rung: "request", BuiltIn: keys(reached, answered, []string{probekeys.UDPResponseTime}), CredentialBuiltIn: authed, PollBuiltIn: polled, ListenBuiltIn: pushed},
	{Name: "telnet", Description: "Telnet: the line protocol over TCP with option negotiation (every option refused), its login the session's auth step; the probe connects and waits for the first negotiation or banner bytes (responds).", Held: true, Built: true, Line: true, Rung: "connect", BuiltIn: keys(reached, answered, []string{probekeys.TCPOpen}), SessionBuiltIn: session, CredentialBuiltIn: authed, PollBuiltIn: polled, ListenBuiltIn: pushed},
	{Name: "ssh", Description: "SSH: the probe runs a real key exchange (responds) and tries the endpoint credential when one is set (auth); the line protocol holds a shell or exec channel over it, its userauth the session's auth step and its host key pinned on first use.", Held: true, Built: true, Line: true, Rung: "handshake", BuiltIn: keys(reached, answered, connected, []string{probekeys.SSHHandshakeTime}), SessionBuiltIn: keys(session, []string{probekeys.SSHHostKey}), CredentialBuiltIn: authed, PollBuiltIn: polled, ListenBuiltIn: pushed},
	{Name: "http", Description: "HTTP: a driver's polls and commands send real requests (method, path, query, headers, body) and read JSON, XML, HTML or text answers, authenticated by the endpoint's declared scheme (basic, bearer, an API key, digest) or a login whose session the node keeps; a declared server-sent events stream is held as a session, its events landing through the listeners; the probe draws a real response off the API without a credential (responds), any status counting as an answer.", Held: true, Built: true, Rung: "connect", BuiltIn: keys(reached, answered, connected, httpKeys), CredentialBuiltIn: authed, PollBuiltIn: polled, ListenBuiltIn: keys(pushed, session)},
	{Name: "https", Description: "HTTPS: http over a TLS connection, its own entry so a component can serve a plain web port beside a TLS one, a server-sent events stream held over it as over http; the probe runs the handshake (responds), reports the certificate's expiry, then draws a response, the certificate verified against the node's roots, pinned to the public key on first use, or (by the operator's explicit choice) not verified.", Held: true, Built: true, Rung: "handshake", TLS: true, BuiltIn: keys(reached, answered, connected, httpKeys, tlsKeys), CredentialBuiltIn: authed, PollBuiltIn: polled, ListenBuiltIn: keys(pushed, session)},
	{Name: "websocket", Description: "WebSocket: a held session whose messages are text frames, routed as a line session's are: a frame sent to arm a subscription on every connection, pushes landing through the listeners, and a request answered by the frame carrying back an id the node mints into it, whatever pushes interleave; the upgrade rides the endpoint's scheme (basic, bearer, an API key) or a login whose cookie the node keeps, and the session is kept alive by ping and pong; the probe is http's, a credential-free GET / whose answer, any status, is the rung (responds), so reachability stays observed beneath a refused upgrade.", Held: true, Built: true, Line: true, Rung: "connect", BuiltIn: keys(reached, answered, connected, httpKeys), SessionBuiltIn: session, CredentialBuiltIn: authed, PollBuiltIn: polled, ListenBuiltIn: pushed},
	{Name: "wss", Description: "WSS: websocket over a TLS connection, its own entry so a component can serve a plain socket beside a TLS one; the probe is https's: it runs the handshake (responds), reports the certificate's expiry, then draws a credential-free response, the certificate verified against the node's roots, pinned to the public key on first use, or (by the operator's explicit choice) not verified.", Held: true, Built: true, Line: true, Rung: "handshake", TLS: true, BuiltIn: keys(reached, answered, connected, httpKeys, tlsKeys), SessionBuiltIn: session, CredentialBuiltIn: authed, PollBuiltIn: polled, ListenBuiltIn: pushed},
	{Name: "snmp", Description: "SNMP v2c, stateless: the probe gets a standard OID (sysUpTime.0 unless the driver names another) with the endpoint's community, and an answer is the rung; a wrong community is dropped silently and reads as no answer, while an authorizationError reads as an auth refusal.", Held: false, Built: true, Rung: "request", BuiltIn: keys(reached, answered, []string{probekeys.SNMPResponseTime}), CredentialBuiltIn: authed, PollBuiltIn: polled},
	{Name: "webhook", Description: "Webhook: a push transport the server hosts, not one a node dials: a component or a cloud service posts each message to the endpoint's own URL (/api/v1/webhooks/ and a token the server mints, held as a secret), authenticated by the scheme its driver declares (an hmac signature over a signed timestamp, a token, basic, or none, which reads unsigned) and mapped by its listeners onto any lane; nothing is dialed, so it has no reachability ladder and reads last heard.", Held: false, Built: true, Push: true, ListenBuiltIn: []string{writerclass.WebhookPostCount}},
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
