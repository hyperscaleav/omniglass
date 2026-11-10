package node

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A path dependency (#1652): a component reached through another holds the
// upstream reason while any of its upstream's endpoints has a closed gate,
// names the root of the closure, waits for its own probe once the upstream
// opens, and never derives its reason from a closure rooted in itself.

// pathChain is A, B and C, one endpoint each (a-1, b-1, c-1), with C reached
// through B and B through A, every endpoint probed.
func pathChain() upstreamState {
	return upstreamState{
		componentOf: map[string]string{"a-1": "comp-a", "b-1": "comp-b", "c-1": "comp-c"},
		edges: map[string][]pathUpstream{
			"b-1": {{component: "comp-a", name: "A", endpoints: []string{"a-1"}}},
			"c-1": {{component: "comp-b", name: "B", endpoints: []string{"b-1"}}},
		},
		ownClosed:   map[string]bool{},
		unreachable: map[string]bool{},
		probed:      map[string]bool{"a-1": true, "b-1": true, "c-1": true},
		now:         gateT0,
	}
}

func TestAChainHeldByItsRootNamesTheRootAllTheWayDown(t *testing.T) {
	t.Parallel()
	s := pathChain()
	s.ownClosed["a-1"], s.unreachable["a-1"] = true, true

	got := deriveUpstream(s)
	if _, ok := got["a-1"]; ok {
		t.Fatalf("the root holds an upstream reason of its own: %+v", got["a-1"])
	}
	for _, ep := range []string{"b-1", "c-1"} {
		h, ok := got[ep]
		if !ok || h.wait || h.component != "comp-a" || h.name != "A" || !h.since.Equal(gateT0) {
			t.Fatalf("%s = %+v, %v; want held naming A since %s", ep, h, ok, gateT0)
		}
	}
}

func TestARootThatOpensLeavesItsDependentWaitingAndTheNextOneNamingIt(t *testing.T) {
	t.Parallel()
	g := newGates()
	chainGates(g, true)
	g.verdict("a-1", "probe-a", false, gateT0)
	g.verdict("a-1", "probe-a", false, gateT0.Add(time.Second))
	if r, ok := g.closed("c-1"); !ok || r.kind != reasonUpstream || r.source != "comp-a" {
		t.Fatalf("c-1 = %+v, %v; want held by upstream comp-a", r, ok)
	}

	// A opens: B has a probe, so it waits for its own; C reads upstream B.
	g.verdict("a-1", "probe-a", true, gateT0.Add(2*time.Second))
	if h := g.upstream["b-1"]; !h.wait {
		t.Fatalf("b-1 = %+v; want its wait phase", h)
	}
	if r, ok := g.closed("b-1"); !ok || r.kind != reasonUpstream {
		t.Fatalf("b-1 = %+v, %v; want the upstream reason still, waiting", r, ok)
	}
	if h := g.upstream["c-1"]; h.wait || h.component != "comp-b" || h.name != "B" {
		t.Fatalf("c-1 = %+v; want held naming B", h)
	}
	if !g.upstreamHeld("c-1") || g.upstreamHeld("b-1") {
		t.Fatal("upstreamHeld: want c-1 held and b-1 waiting (its probe runs)")
	}

	// B's own up verdict releases B; C then waits for its own probe.
	g.verdict("b-1", "probe-b", true, gateT0.Add(3*time.Second))
	if r, ok := g.closed("b-1"); ok {
		t.Fatalf("b-1 still closed after its own up verdict: %+v", r)
	}
	if h, ok := g.upstream["c-1"]; !ok || !h.wait {
		t.Fatalf("c-1 = %+v, %v; want its wait phase", h, ok)
	}
	g.verdict("c-1", "probe-c", true, gateT0.Add(4*time.Second))
	if r, ok := g.closed("c-1"); ok {
		t.Fatalf("c-1 still closed after its own up verdict: %+v", r)
	}
}

func TestAPathCycleTerminatesAndNeverHoldsItsRootByWayOfItself(t *testing.T) {
	t.Parallel()
	s := upstreamState{
		componentOf: map[string]string{"a-1": "comp-a", "b-1": "comp-b"},
		edges: map[string][]pathUpstream{
			"a-1": {{component: "comp-b", name: "B", endpoints: []string{"b-1"}}},
			"b-1": {{component: "comp-a", name: "A", endpoints: []string{"a-1"}}},
		},
		ownClosed:   map[string]bool{"a-1": true},
		unreachable: map[string]bool{},
		probed:      map[string]bool{"a-1": true, "b-1": true},
		now:         gateT0,
	}
	got := deriveUpstream(s)
	if h, ok := got["b-1"]; !ok || h.wait || h.component != "comp-a" {
		t.Fatalf("b-1 = %+v, %v; want held naming A", h, ok)
	}
	if h, ok := got["a-1"]; ok {
		t.Fatalf("a-1 = %+v; held by way of its own dependent", h)
	}

	// A's refusal lifts: both wait on their own probes, neither holding the
	// other through the cycle.
	s.ownClosed = map[string]bool{}
	s.prior = got
	got = deriveUpstream(s)
	if h, ok := got["a-1"]; ok {
		t.Fatalf("a-1 = %+v; want no reason once its refusal lifts", h)
	}
	if h, ok := got["b-1"]; !ok || !h.wait {
		t.Fatalf("b-1 = %+v, %v; want its wait phase", h, ok)
	}
}

func TestAnOutOfBandEndpointIsNeverHeld(t *testing.T) {
	t.Parallel()
	s := pathChain()
	// The worklist carries no path upstreams for an out-of-band endpoint.
	s.componentOf["b-oob"] = "comp-b"
	s.probed["b-oob"] = true
	s.ownClosed["a-1"] = true
	got := deriveUpstream(s)
	if h, ok := got["b-oob"]; ok {
		t.Fatalf("b-oob = %+v; an out-of-band endpoint was held", h)
	}
	if _, ok := got["b-1"]; !ok {
		t.Fatal("the in-band endpoint beside it was not held")
	}
}

func TestADependentWithNoProbeIsReleasedTheMomentItsUpstreamOpens(t *testing.T) {
	t.Parallel()
	s := pathChain()
	s.probed["b-1"] = false
	s.ownClosed["a-1"] = true
	s.prior = deriveUpstream(s)
	s.ownClosed = map[string]bool{}
	got := deriveUpstream(s)
	if h, ok := got["b-1"]; ok {
		t.Fatalf("b-1 = %+v; a dependent with no probe waited for one", h)
	}
	// C, probed, waits on its own probe: B released holds nothing.
	if h, ok := got["c-1"]; !ok || !h.wait {
		t.Fatalf("c-1 = %+v, %v; want its wait phase, not held by a released upstream", h, ok)
	}
}

func TestADependentThatEarnsItsOwnUnreachableDropsTheUpstreamReason(t *testing.T) {
	t.Parallel()
	g := newGates()
	chainGates(g, true)
	g.record("a-1", "fp", &collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"})
	if r, ok := g.closed("b-1"); !ok || r.kind != reasonUpstream {
		t.Fatalf("b-1 = %+v, %v; want upstream", r, ok)
	}
	// While held, B's verdicts are not its own to give (its probe is not
	// run; a late one is ignored).
	g.verdict("b-1", "probe-b", false, gateT0)
	g.verdict("b-1", "probe-b", false, gateT0.Add(time.Second))
	if r, _ := g.closed("b-1"); r.kind != reasonUpstream {
		t.Fatalf("b-1 = %+v; a held dependent took a verdict", r)
	}

	g.clear("a-1")
	g.verdict("b-1", "probe-b", false, gateT0.Add(2*time.Second))
	g.verdict("b-1", "probe-b", false, gateT0.Add(3*time.Second))
	r, ok := g.closed("b-1")
	if !ok || r.kind != reasonUnreachable {
		t.Fatalf("b-1 = %+v, %v; want its own unreachable", r, ok)
	}
	if _, ok := g.upstream["b-1"]; ok {
		t.Fatal("b-1 kept the upstream reason beside its own unreachable")
	}
	// C now names B, whose gate is closed by its own reason.
	if r, ok := g.closed("c-1"); !ok || r.kind != reasonUpstream || r.source != "comp-b" {
		t.Fatalf("c-1 = %+v, %v; want upstream comp-b", r, ok)
	}
}

func TestTheUpstreamReasonIsPublishedNamingTheComponentAndAdoptedOnRestart(t *testing.T) {
	t.Parallel()
	g := newGates()
	chainGates(g, true)
	g.verdict("a-1", "probe-a", false, gateT0)
	g.verdict("a-1", "probe-a", false, gateT0.Add(time.Second))
	value, _ := g.rejectionOf("c-1")
	if want := "upstream " + gateT0.Format(time.RFC3339) + " comp-a"; value != want {
		t.Fatalf("c-1 rejection = %q; want %q", value, want)
	}
	r, _ := g.closed("c-1")
	if !strings.Contains(r.text, "upstream A since "+gateT0.Format(time.RFC3339)) {
		t.Fatalf("c-1 text = %q; want it to name upstream A since", r.text)
	}

	// A restarted node adopts the reason off the worklist, then re-derives
	// it live: A reads open (its unreachable is re-earned), so C waits on its
	// own probe rather than dropping the hold unseen.
	restarted := newGates()
	restarted.observe("c-1", "fp", value)
	chainGates(restarted, false)
	if h, ok := restarted.upstream["c-1"]; !ok || !h.wait || !h.since.Equal(r.since) {
		t.Fatalf("restarted c-1 = %+v, %v; want the adopted reason, waiting, since %s", h, ok, r.since)
	}
}

// chainGates places the A-B-C chain on g, every endpoint probed; observe
// first marks each endpoint's fingerprint (a fresh node).
func chainGates(g *gates, observe bool) {
	if observe {
		for _, ep := range []string{"a-1", "b-1", "c-1"} {
			g.observe(ep, "fp", "")
		}
	} else {
		for _, ep := range []string{"a-1", "b-1"} {
			g.observe(ep, "fp", "")
		}
	}
	g.clock = func() time.Time { return gateT0 }
	s := pathChain()
	g.topology(upstreamTopology{componentOf: s.componentOf, edges: s.edges, probed: s.probed}, gateT0)
}
