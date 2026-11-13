package node

import (
	"encoding/json"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The endpoint lane (#1249, ADR-1249) is the lane pool's unit of exclusion:
// one discrete request in flight per endpoint, an operator's work (a
// command, a run now, a preview that resolves to the endpoint) ahead of the
// scheduled tasks queued there, nothing preempted. A lane holds no state of
// its own about the endpoint: each job asks the endpoint's gate as it is
// taken off the lane (consult), so work queued behind a request that closed
// the gate (a reboot) answers with the gate's reason and sends nothing.

// closure is a closed gate as a lane job reads it: the most causal reason,
// and the refusal when that reason is one.
type closure struct {
	reason  reason
	refusal *collection.TerminalError
}

// consult is the endpoint's gate as a job about to send at rung at reads
// it. Every reason that holds the whole endpoint counts, a path upstream's
// (#1652) in either phase included; the refused reason
// counts only when the job sends its own request (own) at or above the
// refused rung, since a job riding a held session is answered by that
// session's own state. false when nothing holds the job.
func (g *gates) consult(endpointID string, at rung, own bool) (closure, bool) {
	if g == nil || endpointID == "" {
		return closure{}, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var rs []reason
	var refusal *collection.TerminalError
	if gt, ok := g.held[endpointID]; ok && own && gt.fingerprint == g.current[endpointID] && at.rank() >= gt.rung.rank() {
		rs = append(rs, reason{kind: reasonRefused, since: gt.since, text: gt.reason})
		refusal = gt.refusal()
	}
	for _, r := range g.others[endpointID] {
		rs = append(rs, r)
	}
	if r, ok := g.upstreamReasonLocked(endpointID); ok {
		rs = append(rs, r)
	}
	if len(rs) == 0 {
		return closure{}, false
	}
	c := closure{reason: mostCausal(rs)}
	if c.reason.kind == reasonRefused {
		c.refusal = refusal
	}
	return c, true
}

// consult is the endpoint's gate as a lane job reads it (nil-safe).
func (m *sessionManager) consult(endpointID string, at rung, own bool) (closure, bool) {
	if m == nil {
		return closure{}, false
	}
	return m.gates.consult(endpointID, at, own)
}

// sendsOwnRequest reports a task that makes its own request rather than
// riding a held session: no session held for its endpoint, or http, whose
// every poll is its own request (#1038).
func sendsOwnRequest(m *sessionManager, t collection.TaskSpec) bool {
	return m.lookup(t.EndpointID) == nil || driver.IsHTTP(t.Transport)
}

// runReply is the answer to a run now its endpoint's gate holds: the
// refusal itself when that is the cause, else the reason and its release.
func runReply(c closure) collection.RunReply {
	if c.refusal != nil {
		return collection.RunReply{Gated: true, GateReason: string(reasonRefused), Error: c.refusal.Error()}
	}
	return heldRun(c.reason)
}

// commandRefusal is the failure story of a command its endpoint's gate
// holds: nothing was sent.
func commandRefusal(c closure) string {
	if c.refusal != nil {
		return "node: the endpoint is gated: " + c.refusal.Error()
	}
	return "node: the endpoint is " + c.reason.text + ", so the command was not sent"
}

// previewReply is the answer to a preview that resolved to an endpoint whose
// gate is closed, naming the target it was for.
func previewReply(c closure, target string) collection.RunReply {
	text := c.reason.text
	if c.refusal != nil {
		text = "gated: " + c.refusal.Error()
	}
	return collection.RunReply{Gated: true, GateReason: string(c.reason.kind),
		Error: "node: the endpoint at " + target + " is " + text + ", so the preview was not sent; " + reasonRules[c.reason.kind].release}
}

// routeKey is what resolves a preview to an endpoint: the transport it
// speaks and the target it names.
func routeKey(transport, target string) string {
	return transport + " " + strings.ToLower(strings.TrimSpace(target))
}

func taskTarget(t collection.TaskSpec) string {
	var p endpointParams
	_ = json.Unmarshal(t.EndpointParams, &p)
	return p.Target
}

// route rebuilds the preview routes from the worklist: each transport and
// target an endpoint's tasks speak, to the endpoint (the lowest id where two
// endpoints name the same).
func (g *gates) route(tasks []collection.TaskSpec) {
	routes := map[string]string{}
	for _, t := range tasks {
		target := taskTarget(t)
		if t.EndpointID == "" || target == "" {
			continue
		}
		k := routeKey(t.Transport, target)
		if cur, ok := routes[k]; !ok || t.EndpointID < cur {
			routes[k] = t.EndpointID
		}
	}
	g.mu.Lock()
	g.routes = routes
	g.mu.Unlock()
}

// resolve is the endpoint a preview's tasks all speak to by transport and
// target, or empty when any matches none or they name different endpoints:
// such a preview runs off-lane.
func (g *gates) resolve(tasks []collection.TaskSpec) string {
	if g == nil || len(tasks) == 0 {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	ep := ""
	for i, t := range tasks {
		got := g.routes[routeKey(t.Transport, taskTarget(t))]
		if got == "" || (i > 0 && got != ep) {
			return ""
		}
		ep = got
	}
	return ep
}

// previewClosed is the closed gate of the endpoint a preview resolves to,
// with the target it names; false when it resolves to none or the gate is
// open. A preview sends its own request, so a refusal holds it.
func (g *gates) previewClosed(tasks []collection.TaskSpec) (closure, string, bool) {
	ep := g.resolve(tasks)
	if ep == "" {
		return closure{}, "", false
	}
	c, closed := g.consult(ep, statelessRung(tasks[0]), true)
	return c, taskTarget(tasks[0]), closed
}

// gatesOf is the manager's gates (nil-safe: a node run with no manager has
// none, and a nil gates holds nothing).
func (m *sessionManager) gatesOf() *gates {
	if m == nil {
		return nil
	}
	return m.gates
}
