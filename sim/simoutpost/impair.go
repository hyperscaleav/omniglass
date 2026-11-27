package simoutpost

import (
	"net/netip"
	"sort"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/sim/impair"
)

// applier is what realizes an impairment plan: impair.Netlink, or a fake.
type applier interface {
	Apply(impair.Plan) error
	Clear() error
}

// impairer keeps the network matching what the arbiter resolved: on every
// settle it plans the served devices with their requested impairments and applies
// the plan when it changed (and after a failed apply). Without NET_ADMIN it
// applies nothing and says why.
type impairer struct {
	ap       applier
	netAdmin bool
	last     *impair.Plan
	tried    bool // an apply was attempted, so the kernel may hold some of it
	st       collection.SimImpairmentStatus
}

func newImpairer(ap applier, netAdmin bool) *impairer {
	m := &impairer{ap: ap, netAdmin: netAdmin}
	if !netAdmin {
		m.st = collection.SimImpairmentStatus{Reason: "the simulator lacks NET_ADMIN, so it cannot drop or shape traffic"}
	}
	return m
}

// Reconcile takes the served devices (component id to address) and the
// impairments the arbiter resolved, by component id. The arbiter (sim.Arbiter,
// #2799) is its only caller: it is the sim.NetSink.
func (m *impairer) Reconcile(served map[string]netip.Addr, wants map[string]impair.Impairment) {
	if !m.netAdmin {
		return
	}
	devs := make([]impair.Device, 0, len(served))
	var impaired []string
	for id, addr := range served {
		d := impair.Device{Addr: addr}
		if w, ok := wants[id]; ok {
			d.Impairment = w
			if w.Unplugged || d.Impairment.Shaped() {
				impaired = append(impaired, id)
			}
		}
		devs = append(devs, d)
	}
	sort.Strings(impaired)
	p := impair.PlanFor(devs)
	if m.last != nil && m.last.Equal(p) && m.st.Error == "" {
		m.st.Impaired = impaired
		return
	}
	m.tried = true
	if err := m.ap.Apply(p); err != nil {
		m.st = collection.SimImpairmentStatus{Available: true, Error: err.Error()}
		m.last = nil
		return
	}
	m.last = &p
	m.st = collection.SimImpairmentStatus{Available: true, Impaired: impaired}
}

func (m *impairer) status() collection.SimImpairmentStatus { return m.st }

// clear removes what was applied, when the simulator stops. A failed apply
// may have left half its plan behind (the firewall without the shaping), so
// any attempt is cleared, not only a success.
func (m *impairer) clear() {
	if m.netAdmin && m.tried {
		_ = m.ap.Clear()
	}
}
