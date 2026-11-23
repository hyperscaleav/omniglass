package health_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/health"
)

// toAttach is a component placed in a slot whose product is collected through a
// driver but which has no endpoint yet: started from a template, wired later.
func toAttach(name string) health.Component {
	return health.Component{Name: name, Value: health.Healthy, ToAttach: true}
}

// A member outside every slot moves its system to degraded on any open alarm,
// whatever the alarm's severity, and never further (#2459): slots alone decide
// outage, because only a slot says what losing a component costs the room.
func TestOutsideMemberDegradesItsSystem(t *testing.T) {
	staffed := health.Slot{Name: "display", Quorum: 1, Impact: "outage", Assigned: []health.Component{healthy("d")}}

	cases := []struct {
		name    string
		slots   []health.Slot
		outside []health.Component
		want    health.Value
	}{
		{"a hand-built system with a member in outage reads degraded", nil, []health.Component{down("amp", health.Outage)}, health.Degraded},
		{"a degraded outside member reads degraded", nil, []health.Component{down("amp", health.Degraded)}, health.Degraded},
		{"a clean outside member leaves the system healthy", nil, []health.Component{healthy("amp")}, health.Healthy},
		{"no members and no slots is healthy", nil, nil, health.Healthy},
		{"a staffed system with an alarmed outside member reads degraded", []health.Slot{staffed}, []health.Component{down("amp", health.Outage)}, health.Degraded},
		{"a slotted outage still decides outage over an outside alarm", []health.Slot{
			{Name: "display", Quorum: 1, Impact: "outage", Assigned: []health.Component{down("d", health.Outage)}},
		}, []health.Component{down("amp", health.Outage)}, health.Outage},
		{"an outside alarm outranks an incomplete slot", []health.Slot{unstaffed(1, "outage")}, []health.Component{down("amp", health.Degraded)}, health.Degraded},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := health.SystemVerdict(c.slots, nil, c.outside); got != c.want {
				t.Fatalf("verdict = %v, want %v", got, c.want)
			}
		})
	}
}

// An occupant still to attach counts toward quorum (it is installed, nothing is
// alarming on it) but not as finished commissioning: its slot reads incomplete,
// the reading a slot nobody staffed already gives (#2459).
func TestOccupantToAttachReadsIncomplete(t *testing.T) {
	t.Run("a slot whose only occupant is still to attach reads incomplete", func(t *testing.T) {
		r := health.Slot{Name: "display", Quorum: 1, Impact: "outage", Assigned: []health.Component{toAttach("d")}}
		if r.Impaired() {
			t.Fatal("an occupant still to attach occupies: nothing has failed")
		}
		if got := r.Contributes(); got != health.Incomplete {
			t.Fatalf("contributes = %v, want incomplete", got)
		}
	})
	t.Run("a spare attached occupant covers one still to attach", func(t *testing.T) {
		r := health.Slot{Name: "mic", Quorum: 1, Impact: "degraded", Assigned: []health.Component{healthy("a"), toAttach("b")}}
		if got := r.Contributes(); got != health.Healthy {
			t.Fatalf("contributes = %v, want healthy: quorum is met by an attached occupant", got)
		}
	})
	t.Run("a slot declared harmless stays healthy", func(t *testing.T) {
		r := health.Slot{Name: "confidence", Quorum: 1, Impact: "none", Assigned: []health.Component{toAttach("c")}}
		if got := r.Contributes(); got != health.Healthy {
			t.Fatalf("contributes = %v, want healthy", got)
		}
	})
	t.Run("a live failure still outranks the gap", func(t *testing.T) {
		r := health.Slot{Name: "display", Quorum: 2, Impact: "outage", Assigned: []health.Component{toAttach("a"), down("b", health.Outage)}}
		if got := r.Contributes(); got != health.Outage {
			t.Fatalf("contributes = %v, want outage", got)
		}
	})
}

// The reason is what the verdict line says after the verdict: every contributor
// that is not healthy, worst first, naming the slot or the members.
func TestSystemReason(t *testing.T) {
	cases := []struct {
		name    string
		slots   []health.Slot
		outside []health.Component
		want    string
	}{
		{"healthy says nothing is wrong", []health.Slot{{Name: "display", Quorum: 1, Impact: "outage", Assigned: []health.Component{healthy("d")}}}, []health.Component{healthy("amp")},
			"no slot is impaired and no member has an open alarm"},
		{"one outside member", nil, []health.Component{down("amp-1", health.Outage), healthy("amp-2")},
			"1 member outside a slot has an open alarm (amp-1)"},
		{"two outside members", nil, []health.Component{down("amp-1", health.Outage), down("amp-2", health.Degraded)},
			"2 members outside a slot have an open alarm (amp-1, amp-2)"},
		{"impaired slot first, then outside", []health.Slot{{Name: "display", Quorum: 1, Impact: "outage", Assigned: []health.Component{down("d", health.Outage)}}}, []health.Component{down("amp", health.Degraded)},
			"slot display is impaired (0 of 1 occupying); 1 member outside a slot has an open alarm (amp)"},
		{"unstaffed slot", []health.Slot{unstaffed(2, "outage")}, nil,
			"slot r is short 2 (never installed)"},
		{"to attach", []health.Slot{{Name: "display", Quorum: 1, Impact: "outage", Assigned: []health.Component{toAttach("d")}}}, nil,
			"slot display has 1 component still to attach (d)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := health.SystemReason(c.slots, nil, c.outside); got != c.want {
				t.Fatalf("reason = %q, want %q", got, c.want)
			}
		})
	}
}
