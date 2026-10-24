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
}

// registry is the one authoritative list, in the order surfaces render it:
// cheapest first. tcp is both: its reachability probe dials per poll, and the
// line protocol a driver speaks over it holds a session (#817). tls is the
// same line protocol over a TLS connection (#1035): its own entry rather than
// an option on tcp, since an endpoint is named by its transport (so a
// component can hold a raw port and a TLS one) and its rung is the handshake,
// over the TLS primitive in internal/collection that other carriers consume.
var registry = []Transport{
	{Name: "icmp", Description: "ICMP echo / ping reachability and RTT.", Held: false, Built: true},
	{Name: "tcp", Description: "Raw TCP: the connect / port-open check, and the line protocol, which holds a session over it (polls, commands, and listeners share one connection).", Held: true, Built: true, Line: true, Rung: "connect"},
	{Name: "tls", Description: "TLS: the line protocol over a TLS connection, held exactly as it is over tcp; the probe runs the handshake (responds) and reports the certificate's expiry, verified against the node's roots, pinned to the public key on first use, or (by the operator's explicit choice) not verified.", Held: true, Built: true, Line: true, Rung: "handshake"},
	{Name: "udp", Description: "UDP: the line protocol as datagrams, one line each, stateless: the probe sends the driver's named request and an answer is the rung (an open udp port cannot be detected), a refused port reads refused, and a lost datagram is retried; each endpoint binds its own local port (its driver's listen-port input, else one the node picks), which its requests, replies and pushes share.", Held: false, Built: true, Rung: "request"},
	{Name: "telnet", Description: "Telnet: the line protocol over TCP with option negotiation (every option refused), its login the session's auth step; the probe connects and waits for the first negotiation or banner bytes (responds).", Held: true, Built: true, Line: true, Rung: "connect"},
	{Name: "ssh", Description: "SSH: the probe runs a real key exchange (responds) and tries the endpoint credential when one is set (auth); the line protocol holds a shell or exec channel over it, its userauth the session's auth step and its host key pinned on first use.", Held: true, Built: true, Line: true, Rung: "handshake"},
	{Name: "http", Description: "HTTP transport; the probe draws a real response off the API (responds), any status counting as an answer.", Held: true, Built: true, Rung: "connect"},
	{Name: "snmp", Description: "SNMP transport; the v2c scalar driver arrives with its slice.", Held: true, Built: false, Rung: "request"},
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

// ByName resolves one transport; ok is false for a name the binary does not
// speak, which is what makes an unknown transport a 422 at write rather than
// a silent row.
func ByName(name string) (Transport, bool) {
	t, ok := byName[name]
	return t, ok
}
