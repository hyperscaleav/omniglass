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
// Outage). Only Outage is "down" in the staffing sense: it stops occupying
// every slot it fills, which is the whole mechanism by which a critical
// alarm reaches a system now. Degraded still occupies; it exists as a
// verdict a component can carry without costing it any slot.
func down(name string, v health.Verdict) health.Component {
	return health.Component{Name: name, Verdict: v}
}

func TestOccupies(t *testing.T) {
	t.Run("a healthy component occupies its slot", func(t *testing.T) {
		if !healthy("a").Occupies() {
			t.Fatal("a healthy component should occupy its slot")
		}
	})

	t.Run("a degraded component still occupies its slot", func(t *testing.T) {
		if !down("a", health.Degraded).Occupies() {
			t.Fatal("a degraded component must still count: severity is how loudly to page somebody, not a second staffing threshold")
		}
	})

	t.Run("an outage component does not occupy its slot", func(t *testing.T) {
		if down("a", health.Outage).Occupies() {
			t.Fatal("an outage component must not count: this is how a critical alarm reaches a system")
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

// A down (outage) assignee stops counting toward quorum, which is the path
// from a critical alarm on one component to a verdict on the system that
// depends on it.
func TestDownAssigneeDropsBelowQuorum(t *testing.T) {
	a, b := healthy("a"), healthy("b")
	r := health.Role{Name: "mic", Quorum: 2, Impact: "degraded", Assigned: []health.Component{a, b}}
	if r.Impaired() {
		t.Fatal("two healthy assignees meet a quorum of two")
	}
	b = down("b", health.Outage)
	r.Assigned = []health.Component{a, b}
	if !r.Impaired() {
		t.Fatal("one outage assignee should drop the role below quorum")
	}
	if got := r.Contributes(); got != health.Degraded {
		t.Fatalf("contributes = %v, want degraded", got)
	}
}

// TestDegradedAssigneeStaysAboveQuorum pins the boundary: a merely degraded
// assignee (an info or warning alarm, never outage) is not "down" in the
// staffing sense. It still occupies its slot, so a role staffed exactly to
// quorum by degraded-but-not-outage components is not impaired.
func TestDegradedAssigneeStaysAboveQuorum(t *testing.T) {
	a, b := down("a", health.Degraded), healthy("b")
	r := health.Role{Name: "mic", Quorum: 2, Impact: "degraded", Assigned: []health.Component{a, b}}
	if r.Impaired() {
		t.Fatal("a degraded assignee still occupies its slot: quorum of two is still met")
	}
	if got := r.Contributes(); got != health.Healthy {
		t.Fatalf("contributes = %v, want healthy: nobody dropped out", got)
	}
}

// A spare occupant absorbs the loss: quorum still met by the others means the
// role, and the system above it, never move.
func TestSpareOccupantAbsorbsOneDown(t *testing.T) {
	r := health.Role{Name: "mic", Quorum: 1, Impact: "degraded",
		Assigned: []health.Component{down("a", health.Outage), healthy("b")}}
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

// TestAlternateFraction pins Fraction's contract, especially the trap a naive
// "satisfied count == total count" comparison falls into at zero: 0 == 0
// reads as vacuously true, which would make an alternate nobody declared
// anything for read as fully satisfied.
func TestAlternateFraction(t *testing.T) {
	if got := (health.Alternate{Name: "empty"}).Fraction(); got != 0 {
		t.Fatalf("empty alternate fraction = %v, want 0 (never vacuously satisfied)", got)
	}
	satisfied := health.Role{Name: "a", Quorum: 1, Assigned: []health.Component{healthy("a")}}
	short := health.Role{Name: "b", Quorum: 1}
	if got := (health.Alternate{Name: "half", Roles: []health.Role{satisfied, short}}).Fraction(); got != 0.5 {
		t.Fatalf("one of two satisfied = %v, want 0.5", got)
	}
	if got := (health.Alternate{Name: "full", Roles: []health.Role{satisfied}}).Fraction(); got != 1 {
		t.Fatalf("all satisfied = %v, want 1", got)
	}
}

// TestEmptyAlternateNeverSatisfies is the choice-level mirror of
// TestAlternateFraction's zero case: Active must never pick an alternate
// nobody declared any roles for, even when it is the only alternate a choice
// has, because an empty group answering the choice would make any choice
// that ships with an unbuilt alternate permanently satisfied.
func TestEmptyAlternateNeverSatisfies(t *testing.T) {
	c := health.Choice{Name: "conferencing", Alternates: []health.Alternate{{Name: "unbuilt"}}}
	if _, ok := c.Active(); ok {
		t.Fatal("a choice with only an empty alternate must have no active alternate")
	}
	if got := c.Contributes(); got != health.Healthy {
		t.Fatalf("contributes = %v, want healthy: nothing declared, nothing to be impaired by", got)
	}

	// An empty alternate loses to a real one even at fraction zero: a
	// declared-but-unstaffed alternate is still a candidate, an
	// undeclared one never is.
	real := health.Alternate{Name: "component-system", Roles: []health.Role{{Name: "codec", Quorum: 1}}}
	c2 := health.Choice{Name: "conferencing", Alternates: []health.Alternate{{Name: "unbuilt"}, real}}
	active, ok := c2.Active()
	if !ok || active.Name != "component-system" {
		t.Fatalf("active = %+v, ok=%v; want component-system active over the empty alternate", active, ok)
	}
}

// TestTieBreaksByDeclarationOrder pins the tie-break rule the storage layer
// leans on to make choice_alternate.position load-bearing: two alternates
// equally satisfied resolve to the earlier-declared one, and repeated calls
// (this is a pure function, so nothing about the input changes between them)
// must never flip which one explains the verdict.
func TestTieBreaksByDeclarationOrder(t *testing.T) {
	oneOfTwo := func(name string) health.Alternate {
		return health.Alternate{Name: name, Roles: []health.Role{
			{Name: name + "-a", Quorum: 1, Assigned: []health.Component{healthy("x")}},
			{Name: name + "-b", Quorum: 1},
		}}
	}
	c := health.Choice{Name: "conferencing", Alternates: []health.Alternate{oneOfTwo("first"), oneOfTwo("second")}}
	for i := 0; i < 5; i++ {
		active, ok := c.Active()
		if !ok || active.Name != "first" {
			t.Fatalf("read %d: active = %+v, ok=%v; want the position-1 alternate on every read", i, active, ok)
		}
	}
}

// TestChoiceContributesActiveAlternateOwnImpact pins that winning a choice
// does not immunize the winning alternate's own roles from their own
// impact: an active alternate that is itself short still contributes what
// its short roles declare, the same per-role fold an unconditional role set
// gets.
func TestChoiceContributesActiveAlternateOwnImpact(t *testing.T) {
	winner := health.Alternate{Name: "component-system", Roles: []health.Role{
		{Name: "codec", Quorum: 1, Impact: "outage", Assigned: []health.Component{healthy("c")}},
		{Name: "camera", Quorum: 1, Impact: "degraded"}, // unstaffed, so the winner is still short one
	}}
	loser := health.Alternate{Name: "all-in-one", Roles: []health.Role{{Name: "bar", Quorum: 1, Impact: "outage"}}}
	c := health.Choice{Name: "conferencing", Alternates: []health.Alternate{winner, loser}}
	if got := c.Contributes(); got != health.Degraded {
		t.Fatalf("contributes = %v, want degraded: the winning alternate's own short role still counts", got)
	}
}

// TestSystemVerdictWithChoices is the integration point: an unbuilt
// alternate of a satisfied choice must not impair the system (the defect
// #626 exists to close), and SystemVerdict is confirmed to still be exactly
// SystemVerdictWith(roles, nil) so every pre-existing caller keeps compiling
// and behaving the same.
func TestSystemVerdictWithChoices(t *testing.T) {
	allInOne := health.Alternate{Name: "all-in-one", Roles: []health.Role{
		{Name: "bar", Quorum: 1, Impact: "outage", Assigned: []health.Component{healthy("bar-1")}},
	}}
	componentSystem := health.Alternate{Name: "component-system", Roles: []health.Role{
		{Name: "codec", Quorum: 1, Impact: "outage"},
		{Name: "camera", Quorum: 1, Impact: "outage"},
		{Name: "dsp", Quorum: 1, Impact: "outage"},
		{Name: "amp", Quorum: 1, Impact: "outage"},
		{Name: "mic", Quorum: 1, Impact: "outage"},
	}}
	choice := health.Choice{Name: "conferencing", Alternates: []health.Alternate{allInOne, componentSystem}}

	if got := health.SystemVerdictWith(nil, []health.Choice{choice}); got != health.Healthy {
		t.Fatalf("a satisfied all-in-one beside five unbuilt component-system roles = %v, want healthy", got)
	}

	// An unconditional role beside the same satisfied choice still counts on
	// its own: the choice grouping does not swallow every role in the system.
	mandatory := health.Role{Name: "screen", Quorum: 1, Impact: "degraded"}
	if got := health.SystemVerdictWith([]health.Role{mandatory}, []health.Choice{choice}); got != health.Degraded {
		t.Fatalf("an unstaffed mandatory role beside a satisfied choice = %v, want degraded", got)
	}

	roles := []health.Role{
		{Name: "ok", Quorum: 1, Impact: "outage", Assigned: []health.Component{healthy("a")}},
		{Name: "deg", Quorum: 1, Impact: "degraded"},
	}
	if got, want := health.SystemVerdict(roles), health.SystemVerdictWith(roles, nil); got != want {
		t.Fatalf("SystemVerdict(roles) = %v, want it to equal SystemVerdictWith(roles, nil) = %v", got, want)
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
