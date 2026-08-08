package health_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/health"
)

// healthy is a component with no active alarms.
func healthy(name string) health.Component {
	return health.Component{Name: name, Verdict: health.Healthy}
}

// down is a component an alarm has taken to the given verdict (Degraded or
// Outage). Either one is "down": it stops occupying every slot it fills,
// which is the whole mechanism by which an alarm reaches a system now.
func down(name string, v health.Verdict) health.Component {
	return health.Component{Name: name, Verdict: v}
}

func TestOccupies(t *testing.T) {
	t.Run("a healthy component occupies its slot", func(t *testing.T) {
		if !healthy("a").Occupies() {
			t.Fatal("a healthy component should occupy its slot")
		}
	})

	t.Run("a degraded component does not occupy its slot", func(t *testing.T) {
		if down("a", health.Degraded).Occupies() {
			t.Fatal("a degraded component must not count: this is how an alarm reaches a system")
		}
	})

	t.Run("an outage component does not occupy its slot", func(t *testing.T) {
		if down("a", health.Outage).Occupies() {
			t.Fatal("an outage component must not count")
		}
	})
}

func TestRoleQuorumBoundary(t *testing.T) {
	cases := []struct {
		name     string
		quorum   int
		assigned []health.Component
		impaired bool
	}{
		{"exactly at quorum is satisfied", 2, []health.Component{healthy("a"), healthy("b")}, false},
		{"one below quorum is impaired", 2, []health.Component{healthy("a")}, true},
		{"above quorum is satisfied", 2, []health.Component{healthy("a"), healthy("b"), healthy("c")}, false},
		{"nobody assigned is impaired", 1, nil, true},
		{"quorum zero is treated as one", 0, nil, true},
		{"quorum zero with one satisfying is fine", 0, []health.Component{healthy("a")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := health.Role{Name: "mic", Quorum: tc.quorum, Assigned: tc.assigned}
			if got := r.Impaired(); got != tc.impaired {
				t.Fatalf("impaired = %v, want %v (satisfying %d of %d)", got, tc.impaired, r.Satisfying(), tc.quorum)
			}
		})
	}
}

// A down assignee stops counting toward quorum, which is the path from an
// alarm on one component to a verdict on the system that depends on it.
func TestDownAssigneeDropsBelowQuorum(t *testing.T) {
	a, b := healthy("a"), healthy("b")
	r := health.Role{Name: "mic", Quorum: 2, Impact: "degraded", Assigned: []health.Component{a, b}}
	if r.Impaired() {
		t.Fatal("two healthy assignees meet a quorum of two")
	}
	b = down("b", health.Degraded)
	r.Assigned = []health.Component{a, b}
	if !r.Impaired() {
		t.Fatal("one down assignee should drop the role below quorum")
	}
	if got := r.Contributes(); got != health.Degraded {
		t.Fatalf("contributes = %v, want degraded", got)
	}
}

// A spare occupant absorbs the loss: quorum still met by the others means the
// role, and the system above it, never move.
func TestSpareOccupantAbsorbsOneDown(t *testing.T) {
	r := health.Role{Name: "mic", Quorum: 1, Impact: "degraded",
		Assigned: []health.Component{down("a", health.Degraded), healthy("b")}}
	if r.Impaired() {
		t.Fatal("a spare occupant still meets a quorum of one")
	}
	if got := r.Contributes(); got != health.Healthy {
		t.Fatalf("contributes = %v, want healthy: quorum is met by the spare", got)
	}
}

func TestImpactMapping(t *testing.T) {
	impaired := func(impact string) health.Verdict {
		return health.Role{Name: "r", Quorum: 1, Impact: impact}.Contributes()
	}
	if got := impaired("outage"); got != health.Outage {
		t.Fatalf("outage impact = %v", got)
	}
	if got := impaired("degraded"); got != health.Degraded {
		t.Fatalf("degraded impact = %v", got)
	}
	// An impaired role declared harmless stays harmless: that is the point of
	// impact being per-role, so a confidence monitor does not page anyone.
	if got := impaired("none"); got != health.Healthy {
		t.Fatalf("none impact = %v, want healthy", got)
	}
	// A bad value must not read as harmless.
	if got := impaired("nonsense"); got != health.Degraded {
		t.Fatalf("unknown impact = %v, want degraded (never silently harmless)", got)
	}
	// A satisfied role contributes nothing no matter how severe its impact.
	satisfied := health.Role{Name: "r", Quorum: 1, Impact: "outage",
		Assigned: []health.Component{healthy("a")}}
	if got := satisfied.Contributes(); got != health.Healthy {
		t.Fatalf("a satisfied outage-impact role contributes %v, want healthy", got)
	}
}

func TestSystemVerdictWorstWins(t *testing.T) {
	fine := health.Role{Name: "ok", Quorum: 1, Impact: "outage", Assigned: []health.Component{healthy("a")}}
	deg := health.Role{Name: "deg", Quorum: 1, Impact: "degraded"}
	out := health.Role{Name: "out", Quorum: 1, Impact: "outage"}
	harmless := health.Role{Name: "meh", Quorum: 1, Impact: "none"}

	if got := health.SystemVerdict(nil); got != health.Healthy {
		t.Fatalf("a system with no roles = %v, want healthy (nothing has been claimed about it)", got)
	}
	if got := health.SystemVerdict([]health.Role{fine}); got != health.Healthy {
		t.Fatalf("all roles satisfied = %v, want healthy", got)
	}
	if got := health.SystemVerdict([]health.Role{fine, harmless}); got != health.Healthy {
		t.Fatalf("an impaired none-impact role = %v, want healthy", got)
	}
	if got := health.SystemVerdict([]health.Role{fine, deg}); got != health.Degraded {
		t.Fatalf("one impaired degraded role = %v, want degraded", got)
	}
	// Worst wins: an outage is not softened by everything else being fine.
	if got := health.SystemVerdict([]health.Role{fine, deg, out, harmless}); got != health.Outage {
		t.Fatalf("one impaired outage role among many = %v, want outage", got)
	}
}

func TestRollUpAndComponentVerdict(t *testing.T) {
	if got := health.RollUp(nil); got != health.Healthy {
		t.Fatalf("no children = %v, want healthy", got)
	}
	if got := health.RollUp([]health.Verdict{health.Healthy, health.Degraded, health.Healthy}); got != health.Degraded {
		t.Fatalf("rollup = %v, want degraded", got)
	}
	if got := health.RollUp([]health.Verdict{health.Degraded, health.Outage}); got != health.Outage {
		t.Fatalf("rollup = %v, want outage", got)
	}

	if got := health.ComponentVerdict(nil); got != health.Healthy {
		t.Fatalf("no alarms = %v, want healthy", got)
	}
	if got := health.ComponentVerdict([]string{"warning"}); got != health.Degraded {
		t.Fatalf("a warning = %v, want degraded", got)
	}
	if got := health.ComponentVerdict([]string{"warning", "critical"}); got != health.Outage {
		t.Fatalf("a critical among others = %v, want outage", got)
	}
}

// The recorded string round-trips, since the transition log stores it as text and
// a misread would silently change an estate's history.
func TestVerdictRoundTrip(t *testing.T) {
	for _, v := range []health.Verdict{health.Healthy, health.Degraded, health.Outage} {
		if got := health.ParseVerdict(v.String()); got != v {
			t.Fatalf("round trip %v -> %q -> %v", v, v.String(), got)
		}
	}
	if got := health.ParseVerdict("garbage"); got != health.Healthy {
		t.Fatalf("unrecognized recorded value = %v, want healthy (a stray row cannot break an estate)", got)
	}
}
