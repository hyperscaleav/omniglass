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
// recorded.
type Transport struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Held        bool   `json:"held"`
	Built       bool   `json:"built"`
}

// registry is the one authoritative list, in the order surfaces render it:
// the stateless wires first, the held ones after, each pair cheapest first.
var registry = []Transport{
	{Name: "icmp", Description: "ICMP echo / ping reachability and RTT.", Held: false, Built: true},
	{Name: "tcp", Description: "Raw TCP connect / port-open check.", Held: false, Built: true},
	{Name: "udp", Description: "UDP datagram transport; probing arrives with its driver.", Held: false, Built: false},
	{Name: "ssh", Description: "SSH transport; the probe runs a real key exchange (responds), and tries the endpoint credential when one is set (auth).", Held: true, Built: true},
	{Name: "http", Description: "HTTP transport; the probe draws a real response off the API (responds), any status counting as an answer.", Held: true, Built: true},
	{Name: "snmp", Description: "SNMP transport; the v2c scalar driver arrives with its slice.", Held: true, Built: false},
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
