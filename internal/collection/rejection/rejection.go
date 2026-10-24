// Package rejection declares the endpoint-rejection value (ADR-0158): the gate
// a terminal refusal holds on an endpoint, as the node publishes it and as
// the node (a restart adopts it), the server (a :retry trusting a key checks
// it) and the reachability read (the console names its keys) read it back.
// A leaf, like probekeys, so all three read the one shape.
//
// The value is `<rung> <class> <fingerprint>`, or None when no gate holds. A
// refusal of a changed key (an ssh host key, a tls certificate's public key)
// appends its evidence, `<pinned-key> <offered-key>` (#1078): the key the
// endpoint pins and the key the component offered instead, carried as data
// beside the gate so no surface parses them out of a reason, republished and
// adopted with the gate itself, so the evidence can never describe a
// different refusal from the one the gate holds.
package rejection

import "strings"

// None is the value published when no gate holds.
const None = "none"

// Gate is one endpoint's held refusal: the ladder rung it holds at, its class,
// the connection fingerprint it holds for, and, for a refusal of a changed
// key, both keys.
type Gate struct {
	Rung        string
	Class       string
	Fingerprint string
	// PinnedKey and OfferedKey are a key refusal's evidence, both or
	// neither: the key the endpoint pins and the key the component offered.
	PinnedKey  string
	OfferedKey string
}

// rungs are the ladder's rungs a gate can hold at.
var rungs = map[string]bool{"reachable": true, "responsive": true, "authenticated": true}

// HasKeys reports whether the gate carries a key refusal's evidence.
func (g Gate) HasKeys() bool { return g.PinnedKey != "" && g.OfferedKey != "" }

// String renders the gate as its endpoint-rejection value. Half a key pair is
// no evidence and is left out.
func (g Gate) String() string {
	v := g.Rung + " " + g.Class + " " + g.Fingerprint
	if g.HasKeys() {
		v += " " + g.PinnedKey + " " + g.OfferedKey
	}
	return v
}

// Parse reads an endpoint-rejection value. None, an empty or malformed value,
// and a rung the ladder does not have are no gate.
func Parse(v string) (Gate, bool) {
	parts := strings.Fields(v)
	if (len(parts) != 3 && len(parts) != 5) || !rungs[parts[0]] {
		return Gate{}, false
	}
	g := Gate{Rung: parts[0], Class: parts[1], Fingerprint: parts[2]}
	if len(parts) == 5 {
		g.PinnedKey, g.OfferedKey = parts[3], parts[4]
	}
	return g, true
}
