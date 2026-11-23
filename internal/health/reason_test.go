package health_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/health"
)

// The reason is what the verdict line says after the verdict: every slot that
// contributes something other than healthy, worst first, and nothing else. A
// member outside every slot is never a cause (ADR-2459): the slot is the
// system-level health rule.
func TestSystemReason(t *testing.T) {
	cases := []struct {
		name  string
		slots []health.Slot
		want  string
	}{
		{"healthy says no slot is impaired", []health.Slot{{Name: "display", Quorum: 1, Impact: "outage", Assigned: []health.Component{healthy("d")}}},
			"no slot is impaired"},
		{"no slots at all", nil, "no slot is impaired"},
		{"an impaired slot", []health.Slot{{Name: "display", Quorum: 1, Impact: "outage", Assigned: []health.Component{down("d", health.Outage)}}},
			"slot display is impaired (0 of 1 occupying)"},
		{"an unstaffed slot", []health.Slot{unstaffed(2, "outage")},
			"slot r is short 2 (never installed)"},
		{"worst first", []health.Slot{
			unstaffed(1, "outage"),
			{Name: "display", Quorum: 1, Impact: "outage", Assigned: []health.Component{down("d", health.Outage)}},
		}, "slot display is impaired (0 of 1 occupying); slot r is short 1 (never installed)"},
		{"a degraded occupant still occupies, so its slot is no cause", []health.Slot{{Name: "panel", Quorum: 1, Impact: "outage", Assigned: []health.Component{down("p", health.Degraded)}}},
			"no slot is impaired"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := health.SystemReason(c.slots, nil); got != c.want {
				t.Fatalf("reason = %q, want %q", got, c.want)
			}
		})
	}
}

// A losing alternate's slots are no cause either: the reason names only what the
// verdict folded.
func TestSystemReasonSkipsTheBuildNotChosen(t *testing.T) {
	allInOne := health.Alternate{Name: "all-in-one", Slots: []health.Slot{
		{Name: "video-bar", Quorum: 1, Impact: "outage", Assigned: []health.Component{healthy("bar")}},
	}}
	built := health.Alternate{Name: "component-system", Slots: []health.Slot{unstaffed(1, "outage")}}
	c := health.Choice{Name: "conferencing", Alternates: []health.Alternate{allInOne, built}}
	if got := health.SystemReason(nil, []health.Choice{c}); got != "no slot is impaired" {
		t.Fatalf("reason = %q", got)
	}
}
