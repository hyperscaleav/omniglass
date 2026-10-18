package simnode

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim/impair"
)

type fakeApplier struct {
	applied []impair.Plan
	err     error
	cleared int
}

func (f *fakeApplier) Apply(p impair.Plan) error { f.applied = append(f.applied, p); return f.err }
func (f *fakeApplier) Clear() error              { f.cleared++; return nil }

func TestImpairerAppliesWhatTheServerAsksOnChange(t *testing.T) {
	ap := &fakeApplier{}
	m := newImpairer(ap, true)
	served := map[string]netip.Addr{"c1": netip.MustParseAddr("127.60.0.1"), "c2": netip.MustParseAddr("127.60.0.2")}
	wants := map[string]collection.SimImpairment{"c2": {Unplugged: true}}

	m.reconcile(served, wants)
	m.reconcile(served, wants) // unchanged: not re-applied
	if len(ap.applied) != 1 {
		t.Fatalf("applied %d times for one state, want once", len(ap.applied))
	}
	p := ap.applied[0]
	if len(p.Devices) != 2 || len(p.Unplugged) != 1 || p.Unplugged[0] != served["c2"] {
		t.Fatalf("plan = %+v", p)
	}
	if st := m.status(); !st.Available || len(st.Impaired) != 1 || st.Impaired[0] != "c2" {
		t.Fatalf("status = %+v, want available with c2 impaired", st)
	}

	m.reconcile(served, nil) // replugged
	if len(ap.applied) != 2 || len(ap.applied[1].Unplugged) != 0 {
		t.Fatalf("replug not applied: %+v", ap.applied)
	}

	// An impairment the server asks for on a device this simulator does not
	// serve is not applied (it has no address here).
	m.reconcile(served, map[string]collection.SimImpairment{"c9": {Unplugged: true}})
	if last := ap.applied[len(ap.applied)-1]; len(last.Unplugged) != 0 {
		t.Fatalf("an unserved device was impaired: %+v", last)
	}
}

func TestImpairerWithoutNetAdminSaysWhy(t *testing.T) {
	ap := &fakeApplier{}
	m := newImpairer(ap, false)
	m.reconcile(map[string]netip.Addr{"c1": netip.MustParseAddr("127.60.0.1")}, map[string]collection.SimImpairment{"c1": {LatencyMs: 100}})
	if len(ap.applied) != 0 {
		t.Fatal("applied without NET_ADMIN")
	}
	if st := m.status(); st.Available || st.Reason == "" {
		t.Fatalf("status = %+v, want unavailable with a reason", st)
	}
}

func TestImpairerReportsAnApplyFailure(t *testing.T) {
	ap := &fakeApplier{err: errors.New("netlink: operation not permitted")}
	m := newImpairer(ap, true)
	m.reconcile(map[string]netip.Addr{"c1": netip.MustParseAddr("127.60.0.1")}, nil)
	if st := m.status(); st.Error == "" {
		t.Fatalf("status = %+v, want the apply error", st)
	}
	ap.err = nil
	m.reconcile(map[string]netip.Addr{"c1": netip.MustParseAddr("127.60.0.1")}, nil) // retried after a failure
	if st := m.status(); st.Error != "" || len(ap.applied) != 2 {
		t.Fatalf("status %+v after %d applies, want a retry that clears the error", st, len(ap.applied))
	}
}

// A failed apply can leave half a plan in the kernel (the firewall without
// the shaping), so stopping clears whatever was tried, not only what
// succeeded.
func TestImpairerClearsAfterAFailedApply(t *testing.T) {
	ap := &fakeApplier{err: errors.New("root qdisc: file exists")}
	m := newImpairer(ap, true)
	m.reconcile(map[string]netip.Addr{"c1": netip.MustParseAddr("127.60.0.1")}, map[string]collection.SimImpairment{"c1": {Unplugged: true}})
	m.clear()
	if ap.cleared != 1 {
		t.Fatalf("cleared %d times after a failed apply, want once", ap.cleared)
	}
}

// Nothing tried, nothing cleared: a simulator that never impaired leaves
// the network alone on the way out.
func TestImpairerLeavesAnUntouchedNetworkAlone(t *testing.T) {
	ap := &fakeApplier{}
	newImpairer(ap, true).clear()
	if ap.cleared != 0 {
		t.Fatalf("cleared %d times having applied nothing", ap.cleared)
	}
}
