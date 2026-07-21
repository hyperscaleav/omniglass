package health_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/health"
)

// mic is a component that provides microphone and speaker with nothing degraded.
func mic(name string) health.Component {
	return health.Component{Name: name, Provides: []string{"microphone", "speaker"}}
}

func TestSatisfies(t *testing.T) {
	required := []string{"microphone", "speaker"}

	t.Run("provides everything required", func(t *testing.T) {
		if !mic("a").Satisfies(required) {
			t.Fatal("a component providing both should satisfy")
		}
	})

	t.Run("missing a required capability", func(t *testing.T) {
		c := health.Component{Name: "panel", Provides: []string{"flat-panel-display"}}
		if c.Satisfies(required) {
			t.Fatal("a display should not satisfy a microphone role")
		}
	})

	t.Run("provides it but an alarm degraded it", func(t *testing.T) {
		c := mic("a")
		c.Degraded = []string{"microphone"}
		if c.Satisfies(required) {
			t.Fatal("a degraded capability must not count: this is how an alarm reaches a system")
		}
	})

	t.Run("degraded capability the role does not require changes nothing", func(t *testing.T) {
		c := mic("a")
		c.Degraded = []string{"camera"}
		if !c.Satisfies(required) {
			t.Fatal("an alarm on an unrelated capability must not impair this role")
		}
	})

	t.Run("a role requiring nothing is satisfied by anything", func(t *testing.T) {
		empty := health.Component{Name: "x"}
		if !empty.Satisfies(nil) {
			t.Fatal("no requirements means nothing to fail")
		}
	})
}

func TestRoleQuorumBoundary(t *testing.T) {
	required := []string{"microphone", "speaker"}

	cases := []struct {
		name     string
		quorum   int
		assigned []health.Component
		impaired bool
	}{
		{"exactly at quorum is satisfied", 2, []health.Component{mic("a"), mic("b")}, false},
		{"one below quorum is impaired", 2, []health.Component{mic("a")}, true},
		{"above quorum is satisfied", 2, []health.Component{mic("a"), mic("b"), mic("c")}, false},
		{"nobody assigned is impaired", 1, nil, true},
		{"quorum zero is treated as one", 0, nil, true},
		{"quorum zero with one satisfying is fine", 0, []health.Component{mic("a")}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := health.Role{Name: "mic", Required: required, Quorum: tc.quorum, Assigned: tc.assigned}
			if got := r.Impaired(); got != tc.impaired {
				t.Fatalf("impaired = %v, want %v (satisfying %d of %d)", got, tc.impaired, r.Satisfying(), tc.quorum)
			}
		})
	}
}

// A degraded assignee stops counting toward quorum, which is the path from an
// alarm on one component to a verdict on the system that depends on it.
func TestDegradedAssigneeDropsBelowQuorum(t *testing.T) {
	a, b := mic("a"), mic("b")
	r := health.Role{Name: "mic", Required: []string{"microphone", "speaker"}, Quorum: 2,
		Impact: "degraded", Assigned: []health.Component{a, b}}
	if r.Impaired() {
		t.Fatal("two healthy assignees meet a quorum of two")
	}
	b.Degraded = []string{"speaker"}
	r.Assigned = []health.Component{a, b}
	if !r.Impaired() {
		t.Fatal("one degraded assignee should drop the role below quorum")
	}
	if got := r.Contributes(); got != health.Degraded {
		t.Fatalf("contributes = %v, want degraded", got)
	}
}

func TestImpactMapping(t *testing.T) {
	impaired := func(impact string) health.Verdict {
		return health.Role{Name: "r", Required: []string{"microphone"}, Quorum: 1, Impact: impact}.Contributes()
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
	satisfied := health.Role{Name: "r", Required: []string{"microphone"}, Quorum: 1, Impact: "outage",
		Assigned: []health.Component{mic("a")}}
	if got := satisfied.Contributes(); got != health.Healthy {
		t.Fatalf("a satisfied outage-impact role contributes %v, want healthy", got)
	}
}

func TestSystemVerdictWorstWins(t *testing.T) {
	fine := health.Role{Name: "ok", Required: []string{"microphone"}, Quorum: 1, Impact: "outage",
		Assigned: []health.Component{mic("a")}}
	deg := health.Role{Name: "deg", Required: []string{"camera"}, Quorum: 1, Impact: "degraded"}
	out := health.Role{Name: "out", Required: []string{"codec"}, Quorum: 1, Impact: "outage"}
	harmless := health.Role{Name: "meh", Required: []string{"touch-panel"}, Quorum: 1, Impact: "none"}

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
