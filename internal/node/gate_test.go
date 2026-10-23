package node

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The layered gate (#1056 ruling): a terminal refusal gates its ladder rung
// and every rung above it, for the endpoint, while the connection fingerprint
// is unchanged. Lower rungs keep running. A changed fingerprint (a :retry
// moves it too) lifts the gate, and a gate published as endpoint-rejection
// and carried back on the worklist holds across a node restart.

func TestGateBlocksItsRungAndAboveForItsFingerprint(t *testing.T) {
	g := newGates()
	g.observe("ep-1", "fp-a", "")
	g.record("ep-1", "fp-a", &collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"})
	if _, ok := g.blocked("ep-1", rungAuthenticated); !ok {
		t.Fatal("an auth refusal did not gate the authenticated rung")
	}
	if _, ok := g.blocked("ep-1", rungResponsive); ok {
		t.Fatal("an auth refusal gated the rung below it")
	}
	g.observe("ep-1", "fp-b", "")
	if _, ok := g.blocked("ep-1", rungAuthenticated); ok {
		t.Fatal("a changed fingerprint left the gate up")
	}

	g.observe("ep-2", "fp-c", "")
	g.record("ep-2", "fp-c", &collection.TerminalError{Class: driver.ClassHostKey, Reason: "the host key changed"})
	for _, r := range []rung{rungResponsive, rungAuthenticated} {
		if _, ok := g.blocked("ep-2", r); !ok {
			t.Fatalf("a host-key refusal did not gate %s", r)
		}
	}
}

func TestGateHoldsAcrossARestartFromItsPublishedRejection(t *testing.T) {
	g := newGates()
	g.observe("ep-1", "fp-a", "")
	g.record("ep-1", "fp-a", &collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"})
	published := g.rejectionOf("ep-1")
	if published == "" || published == rejectionNone {
		t.Fatalf("published rejection = %q", published)
	}

	restarted := newGates()
	restarted.observe("ep-1", "fp-a", published)
	if gt, ok := restarted.blocked("ep-1", rungAuthenticated); !ok || gt.class != driver.ClassAuth {
		t.Fatalf("a restarted node lost the gate: %+v, %v", gt, ok)
	}
	other := newGates()
	other.observe("ep-1", "fp-new", published)
	if _, ok := other.blocked("ep-1", rungAuthenticated); ok {
		t.Fatal("a rejection published under another fingerprint gated this one")
	}
	if got := rejectionOf(rungAuthenticated, "auth", "fp-a"); got != published {
		t.Fatalf("rejection rendering drifted: %q vs %q", got, published)
	}
}

func TestGateClearsWhenTheRungPasses(t *testing.T) {
	g := newGates()
	g.observe("ep-1", "fp-a", "")
	g.record("ep-1", "fp-a", &collection.TerminalError{Class: driver.ClassAuth, Reason: "x"})
	if !g.clear("ep-1") {
		t.Fatal("clearing a gate reported nothing cleared")
	}
	if _, ok := g.blocked("ep-1", rungAuthenticated); ok {
		t.Fatal("a cleared gate still blocks")
	}
	if g.clear("ep-1") {
		t.Fatal("clearing twice reported a change")
	}
}
