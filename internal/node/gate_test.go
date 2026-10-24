package node

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/collection/rejection"
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

// A refusal of a changed key carries both keys as data (#1078): the gate
// publishes them with itself, so the console names the pinned and the offered
// key without parsing the reason, and a restarted node that adopts the gate
// adopts its evidence too, republishing the same value rather than one that
// drops the keys.
func TestAKeyRefusalPublishesBothKeysAndARestartKeepsThem(t *testing.T) {
	const pinned, offered = "SHA256:" + "pinnedpinnedpinnedpinnedpinnedpinnedpinned1", "SHA256:" + "offeredofferedofferedofferedofferedoffered1"
	g := newGates()
	g.observe("ep-1", "fp-a", "")
	g.record("ep-1", "fp-a", collection.HostKeyRefusal(pinned, offered))
	published := g.rejectionOf("ep-1")
	gt, ok := rejection.Parse(published)
	if !ok || gt.Class != driver.ClassHostKey || gt.Rung != "responsive" || gt.PinnedKey != pinned || gt.OfferedKey != offered {
		t.Fatalf("published %q, want the host-key gate with both keys", published)
	}
	if held, _ := g.blocked("ep-1", rungResponsive); held.refusal().Offered != offered || held.refusal().Pinned != pinned {
		t.Fatalf("a gated attempt's refusal lost the keys: %+v", held.refusal())
	}

	restarted := newGates()
	restarted.observe("ep-1", "fp-a", published)
	if got := restarted.rejectionOf("ep-1"); got != published {
		t.Fatalf("a restarted node republishes %q, want the adopted %q", got, published)
	}

	// A refusal that is not about a key carries none.
	plain := newGates()
	plain.observe("ep-2", "fp-b", "")
	plain.record("ep-2", "fp-b", &collection.TerminalError{Class: driver.ClassAuth, Reason: "auth rejected"})
	if gt, _ := rejection.Parse(plain.rejectionOf("ep-2")); gt.HasKeys() {
		t.Fatalf("an auth refusal published keys: %+v", gt)
	}
}
