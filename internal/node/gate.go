package node

import (
	"strings"
	"sync"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/rejection"
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

// rungIn is the rung a terminal class refuses at over a transport: over udp
// (#1037) nothing authenticates and there is no session rung, so every
// refusal holds the transport rung; elsewhere, snmp's community included
// (#821), a credential's refusal holds the session rung.
func rungIn(transportName, class string) rung {
	if transportName == "udp" {
		return rungResponsive
	}
	return rungOf(class)
}

// statelessRung is the rung a stateless poll sends at: the transport's,
// except where its request carries the credential (an snmp get carries the
// community, #821; an http request carries its session's scheme, #902), so
// an auth gate holds the poll too.
func statelessRung(task collection.TaskSpec) rung {
	if task.Transport == "snmp" || (driver.IsHTTP(task.Transport) && task.Session != nil && task.Session.Credential != "") {
		return rungAuthenticated
	}
	return rungResponsive
}

// rejectionNone is the endpoint-rejection value when no gate holds.
const rejectionNone = rejection.None

// rejectionOf renders a gate as its endpoint-rejection value.
func rejectionOf(r rung, class, fingerprint string) string {
	return rejection.Gate{Rung: string(r), Class: class, Fingerprint: fingerprint}.String()
}

// gate is one endpoint's held refusal. pinned and offered are a key
// refusal's evidence (#1078), published with the gate and adopted with it.
// adopted marks a gate taken from the worklist after a restart: its reason is
// the node's placeholder, not the refusal's, so it is never published (#1110).
type gate struct {
	fingerprint string
	rung        rung
	class       string
	reason      string
	pinned      string
	offered     string
	adopted     bool
}

// value renders the gate as its endpoint-rejection value.
func (gt gate) value() string {
	return rejection.Gate{Rung: string(gt.rung), Class: gt.class, Fingerprint: gt.fingerprint, PinnedKey: gt.pinned, OfferedKey: gt.offered}.String()
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
func (g *gates) observe(endpointID, fingerprint, published string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.current[endpointID] = fingerprint
	if gt, ok := g.held[endpointID]; ok && gt.fingerprint != fingerprint {
		delete(g.held, endpointID)
	}
	if _, ok := g.held[endpointID]; ok || published == "" || published == rejectionNone {
		return
	}
	if gt, ok := rejection.Parse(published); ok && gt.Fingerprint == fingerprint && fingerprint != "" {
		g.held[endpointID] = gate{fingerprint: fingerprint, rung: rung(gt.Rung), class: gt.Class,
			reason:  gt.Class + ": refused under this connection fingerprint before the node restarted",
			pinned:  gt.PinnedKey,
			offered: gt.OfferedKey,
			adopted: true}
	}
}

// record gates the endpoint at the refusal's rung under the fingerprint the
// refused attempt was made under. A refusal from an attempt under a
// fingerprint that is no longer current (a late report from a session being
// recycled onto a fixed secret) gates nothing: it is about inputs that are
// gone. false when dropped.
func (g *gates) record(endpointID, fingerprint string, terr *collection.TerminalError) bool {
	return g.recordAt(endpointID, fingerprint, terr, rungOf(terr.Class))
}

// recordAt is record at a rung the caller names.
func (g *gates) recordAt(endpointID, fingerprint string, terr *collection.TerminalError, at rung) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if fingerprint != g.current[endpointID] {
		return false
	}
	g.held[endpointID] = gate{fingerprint: fingerprint, rung: at, class: terr.Class, reason: terr.Error(),
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

// rejectionOf is the endpoint-rejection value the endpoint publishes now,
// and the reason beside it (#1110): the refusal's own, for a gate this node
// recorded; empty for a gate it adopted from the worklist, whose refusal it
// never saw, and when no gate holds.
func (g *gates) rejectionOf(endpointID string) (value, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	gt, ok := g.held[endpointID]
	if !ok {
		return rejectionNone, ""
	}
	if gt.adopted {
		return gt.value(), ""
	}
	return gt.value(), gt.reason
}

// refusal renders a held gate as the terminal error a gated attempt fails
// with.
func (gt gate) refusal() *collection.TerminalError {
	reason := strings.TrimPrefix(gt.reason, gt.class+": ")
	return &collection.TerminalError{Class: gt.class, Reason: reason, Pinned: gt.pinned, Offered: gt.offered}
}
