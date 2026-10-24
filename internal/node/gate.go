package node

import (
	"strings"
	"sync"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The layered gate (#1056 ruling, ADR-0158). Every connection to an endpoint
// is verified bottom up on the existing ladder: reachable (ICMP, gating only
// where a ping is expected), responsive (the transport: a TCP connect, or the
// protocol's handshake: ssh key exchange, tls), authenticated (the session's
// login). A terminal refusal gates its rung and every rung above it, for that
// endpoint, while the connection fingerprint is unchanged; the rungs below
// keep running, so reachability stays observed while auth is rejected. A
// changed fingerprint lifts the gate (an operator's :retry moves it too), and
// so does the rung passing. The gate is published as endpoint-rejection and
// carried back on the worklist, so a restarted node honors it.

// rung is a ladder rung a gate holds at.
type rung string

const (
	rungReachable     rung = "reachable"
	rungResponsive    rung = "responsive"
	rungAuthenticated rung = "authenticated"
)

func (r rung) rank() int {
	switch r {
	case rungReachable:
		return 0
	case rungResponsive:
		return 1
	default:
		return 2
	}
}

// rungOf is the rung a terminal class refuses at: a credential at the login,
// everything else (a host key, a certificate, a protocol) at the transport.
func rungOf(class string) rung {
	if class == driver.ClassAuth {
		return rungAuthenticated
	}
	return rungResponsive
}

// rejectionNone is the endpoint-rejection value when no gate holds.
const rejectionNone = "none"

// rejectionOf renders a gate as its endpoint-rejection value: `<rung>
// <class> <fingerprint>`, then, for a refusal of a changed key (#1035), the
// key the endpoint pins and the key the component offered, so a surface that
// offers to trust the new key reads both as data.
func rejectionOf(r rung, class, fingerprint string, keys ...string) string {
	v := string(r) + " " + class + " " + fingerprint
	if len(keys) == 2 && keys[0] != "" && keys[1] != "" {
		v += " " + keys[0] + " " + keys[1]
	}
	return v
}

// gate is one endpoint's held refusal.
type gate struct {
	fingerprint string
	rung        rung
	class       string
	reason      string
	// pinned and offered are a changed key's refusal's two keys.
	pinned, offered string
}

// gates is the node's gate per endpoint id, with the fingerprint each
// endpoint is connected with now.
type gates struct {
	mu      sync.Mutex
	held    map[string]gate
	current map[string]string
}

func newGates() *gates { return &gates{held: map[string]gate{}, current: map[string]string{}} }

// observe records the endpoint's fingerprint from the worklist and the
// rejection published for it. A gate under another fingerprint lifts; a
// published rejection under this one (a node restart) is adopted.
func (g *gates) observe(endpointID, fingerprint, rejection string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current[endpointID] = fingerprint
	if gt, ok := g.held[endpointID]; ok && gt.fingerprint != fingerprint {
		delete(g.held, endpointID)
	}
	if _, ok := g.held[endpointID]; ok || rejection == "" || rejection == rejectionNone {
		return
	}
	parts := strings.Fields(rejection)
	if (len(parts) == 3 || len(parts) == 5) && parts[2] == fingerprint && fingerprint != "" {
		gt := gate{fingerprint: fingerprint, rung: rung(parts[0]), class: parts[1],
			reason: parts[1] + ": refused under this connection fingerprint before the node restarted"}
		if len(parts) == 5 {
			gt.pinned, gt.offered = parts[3], parts[4]
		}
		g.held[endpointID] = gt
	}
}

// record gates the endpoint at the refusal's rung under the fingerprint the
// refused attempt was made under. A refusal from an attempt under a
// fingerprint that is no longer current (a late report from a session being
// recycled onto a fixed secret) gates nothing: it is about inputs that are
// gone. false when dropped.
func (g *gates) record(endpointID, fingerprint string, terr *collection.TerminalError) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if fingerprint != g.current[endpointID] {
		return false
	}
	g.held[endpointID] = gate{fingerprint: fingerprint, rung: rungOf(terr.Class), class: terr.Class, reason: terr.Error(),
		pinned: terr.Pinned, offered: terr.Offered}
	return true
}

// fingerprintOf is the endpoint's current fingerprint.
func (g *gates) fingerprintOf(endpointID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.current[endpointID]
}

// clear lifts the endpoint's gate (its rung passed); true when one held.
func (g *gates) clear(endpointID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.held[endpointID]
	delete(g.held, endpointID)
	return ok
}

// blocked reports the gate that blocks rung r for the endpoint, if any.
func (g *gates) blocked(endpointID string, r rung) (gate, bool) {
	if g == nil {
		return gate{}, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	gt, ok := g.held[endpointID]
	if !ok || gt.fingerprint != g.current[endpointID] || r.rank() < gt.rung.rank() {
		return gate{}, false
	}
	return gt, true
}

// rejectionOf is the endpoint-rejection value the endpoint publishes now.
func (g *gates) rejectionOf(endpointID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	gt, ok := g.held[endpointID]
	if !ok {
		return rejectionNone
	}
	return rejectionOf(gt.rung, gt.class, gt.fingerprint, gt.pinned, gt.offered)
}

// refusal renders a held gate as the terminal error a gated attempt fails
// with.
func (gt gate) refusal() *collection.TerminalError {
	reason := strings.TrimPrefix(gt.reason, gt.class+": ")
	return &collection.TerminalError{Class: gt.class, Reason: reason, Pinned: gt.pinned, Offered: gt.offered}
}
