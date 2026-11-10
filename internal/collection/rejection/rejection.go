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
//
// The gate is a set of reasons (#1650): the refusal above is one, and every
// other reason the gate holds (an endpoint unreachable on its own probe's
// verdict) is an entry `<kind> <since> <source>` beside it, since an RFC 3339
// UTC instant and source the task whose verdict added it. Entries are joined
// by ";", the refusal first. Parse reads the refusal alone, so a :retry and a
// restarted node read exactly what they read before; ParseSet reads them all.
package rejection

import (
	"strings"
	"time"
)

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

// Reason is a gate reason other than a refusal: its kind, since when it has
// held, and the task whose observation added it.
type Reason struct {
	Kind   string
	Since  time.Time
	Source string
}

// String renders the reason as its entry in the value.
func (r Reason) String() string {
	return r.Kind + " " + r.Since.UTC().Format(time.RFC3339) + " " + r.Source
}

// Join renders a gate's reasons as its endpoint-rejection value: the refusal
// first when one holds, then each other reason, None when the set is empty.
func Join(refused *Gate, others []Reason) string {
	var entries []string
	if refused != nil {
		entries = append(entries, refused.String())
	}
	for _, r := range others {
		entries = append(entries, r.String())
	}
	if len(entries) == 0 {
		return None
	}
	return strings.Join(entries, ";")
}

// ParseSet reads every entry of an endpoint-rejection value: the refusal, if
// one holds, and the other reasons in order. A malformed entry (a since that
// is no instant, a field missing) is skipped, not the whole value.
func ParseSet(v string) (Gate, bool, []Reason) {
	var (
		refused Gate
		ok      bool
		others  []Reason
	)
	for _, entry := range strings.Split(v, ";") {
		if g, isGate := parseGate(entry); isGate {
			if !ok {
				refused, ok = g, true
			}
			continue
		}
		parts := strings.Fields(entry)
		if len(parts) != 3 {
			continue
		}
		since, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			continue
		}
		others = append(others, Reason{Kind: parts[0], Since: since.UTC(), Source: parts[2]})
	}
	return refused, ok, others
}

// Parse reads the refusal out of an endpoint-rejection value. None, an empty
// or malformed value, a rung the ladder does not have, and a value holding no
// refusal are no gate.
func Parse(v string) (Gate, bool) {
	g, ok, _ := ParseSet(v)
	return g, ok
}

// parseGate reads one refusal entry.
func parseGate(entry string) (Gate, bool) {
	parts := strings.Fields(entry)
	if (len(parts) != 3 && len(parts) != 5) || !rungs[parts[0]] {
		return Gate{}, false
	}
	g := Gate{Rung: parts[0], Class: parts[1], Fingerprint: parts[2]}
	if len(parts) == 5 {
		g.PinnedKey, g.OfferedKey = parts[3], parts[4]
	}
	return g, true
}
