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
	// Impaired by a DOWN occupant, not by an empty slot: since #631 impact
	// describes what a failure means, and an uninstalled box has not failed
	// (it reads incomplete instead, tested separately).
	impaired := func(impact string) health.Verdict {
		return health.Role{Name: "r", Quorum: 1, Impact: impact,
			Assigned: []health.Component{down("a", health.Outage)}}.Contributes()
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
	// deg and out are staffed and DOWN, so they contribute their declared
	// impact. An unstaffed role would contribute incomplete instead (#631),
	// which is a different claim, tested separately.
	deg := health.Role{Name: "deg", Quorum: 1, Impact: "degraded", Assigned: []health.Component{down("d", health.Outage)}}
	out := health.Role{Name: "out", Quorum: 1, Impact: "outage", Assigned: []health.Component{down("o", health.Outage)}}
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
	// Incomplete rather than the camera role's declared degraded: the camera
	// was never installed, so this is a commissioning gap in the alternate the
	// room WAS built to (#631). The claim under test is unchanged, that the
	// winning alternate's own short role is not immune to itself.
	if got := c.Contributes(); got != health.Incomplete {
		t.Fatalf("contributes = %v, want incomplete: the winning alternate's own short role still counts", got)
	}

	// The same alternate, short because its camera is ALARMING rather than
	// missing, contributes what the role declared instead.
	winner.Roles[1].Assigned = []health.Component{down("cam-1", health.Outage)}
	broken := health.Choice{Name: "conferencing", Alternates: []health.Alternate{winner, loser}}
	if got := broken.Contributes(); got != health.Degraded {
		t.Fatalf("contributes = %v, want degraded: the camera is installed and down, which is a failure, not a gap", got)
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
	// Incomplete, not its declared degraded: the screen was never installed
	// (#631). The claim under test is unchanged, that the choice grouping does
	// not swallow an unconditional role beside it.
	mandatory := health.Role{Name: "screen", Quorum: 1, Impact: "degraded"}
	if got := health.SystemVerdictWith([]health.Role{mandatory}, []health.Choice{choice}); got != health.Incomplete {
		t.Fatalf("an unstaffed mandatory role beside a satisfied choice = %v, want incomplete", got)
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
// a misread would silently change an fleet's history.
func TestVerdictRoundTrip(t *testing.T) {
	for _, v := range []health.Verdict{health.Healthy, health.Degraded, health.Outage} {
		if got := health.ParseVerdict(v.String()); got != v {
			t.Fatalf("round trip %v -> %q -> %v", v, v.String(), got)
		}
	}
	if got := health.ParseVerdict("garbage"); got != health.Healthy {
		t.Fatalf("unrecognized recorded value = %v, want healthy (a stray row cannot break an fleet)", got)
	}
}

// unstaffed is a role nobody ever assigned a component to: the commissioning
// gap the Incomplete verdict exists to name.
func unstaffed(quorum int, impact string) health.Role {
	return health.Role{Name: "r", Quorum: quorum, Impact: impact}
}

// A role short of quorum because nobody installed the hardware is a
// COMMISSIONING GAP, not a failure. No alarm will ever fire for it, and
// collapsing it into the role's declared impact paints a half-commissioned
// fleet entirely red, which is the state most real fleets are in while they
// are being built. Incomplete is what tells the two apart.
func TestIncompleteIsACommissioningGapNotAFailure(t *testing.T) {
	t.Run("a role nobody staffed reads incomplete, not its impact", func(t *testing.T) {
		r := unstaffed(1, "outage")
		if got := r.Contributes(); got != health.Incomplete {
			t.Fatalf("contributes = %v, want incomplete: nothing was ever installed, so nothing can be alarming", got)
		}
	})

	t.Run("a fully staffed role with one occupant down reads its impact", func(t *testing.T) {
		r := health.Role{Name: "r", Quorum: 3, Impact: "outage",
			Assigned: []health.Component{healthy("a"), healthy("b"), down("c", health.Outage)}}
		if got := r.Contributes(); got != health.Outage {
			t.Fatalf("contributes = %v, want outage: the hardware is installed and one of it is alarming", got)
		}
	})

	t.Run("a part-staffed role with nothing down reads incomplete", func(t *testing.T) {
		r := health.Role{Name: "r", Quorum: 3, Impact: "outage",
			Assigned: []health.Component{healthy("a"), healthy("b")}}
		if got := r.Contributes(); got != health.Incomplete {
			t.Fatalf("contributes = %v, want incomplete: the third box was never installed", got)
		}
	})

	t.Run("a part-staffed role with an occupant down reads its impact, the worse of the two", func(t *testing.T) {
		r := health.Role{Name: "r", Quorum: 3, Impact: "degraded",
			Assigned: []health.Component{healthy("a"), down("b", health.Outage)}}
		if got := r.Contributes(); got != health.Degraded {
			t.Fatalf("contributes = %v, want degraded: a live failure outranks the missing box", got)
		}
	})

	t.Run("a spare absorbing a failure contributes nothing", func(t *testing.T) {
		r := health.Role{Name: "r", Quorum: 3, Impact: "outage",
			Assigned: []health.Component{healthy("a"), healthy("b"), healthy("c"), down("d", health.Outage)}}
		if got := r.Contributes(); got != health.Healthy {
			t.Fatalf("contributes = %v, want healthy: quorum is still met, which is what a spare is for", got)
		}
	})

	// A role declared harmless is harmless empty as well as broken. Reporting
	// a gap here would mark every confidence monitor nobody ever intends to
	// staff as permanently incomplete, which is the saturation this verdict
	// exists to prevent.
	t.Run("an unstaffed role declared harmless contributes nothing", func(t *testing.T) {
		if got := unstaffed(1, "none").Contributes(); got != health.Healthy {
			t.Fatalf("contributes = %v, want healthy: impact none says an empty slot here is not a gap worth reporting", got)
		}
	})

	// A bad impact value must not become a way to hide a gap, the same reason
	// ImpactVerdict refuses to read one as harmless.
	t.Run("an unstaffed role with an unrecognized impact still reads incomplete", func(t *testing.T) {
		if got := unstaffed(1, "nonsense").Contributes(); got != health.Incomplete {
			t.Fatalf("contributes = %v, want incomplete: only an explicit none opts out", got)
		}
	})
}

// Incomplete ranks between healthy and degraded: a commissioning gap is worth
// surfacing above a clean system and worth burying under anything actually
// broken. Rollup is worst-wins over the same ordering everywhere.
func TestIncompleteRanksBetweenHealthyAndDegraded(t *testing.T) {
	if health.Worse(health.Healthy, health.Incomplete) != health.Incomplete {
		t.Fatal("incomplete must outrank healthy")
	}
	if health.Worse(health.Incomplete, health.Degraded) != health.Degraded {
		t.Fatal("degraded must outrank incomplete: something broken beats something missing")
	}
	if health.Worse(health.Incomplete, health.Outage) != health.Outage {
		t.Fatal("outage must outrank incomplete")
	}
	if got := health.RollUp([]health.Verdict{health.Incomplete, health.Degraded}); got != health.Degraded {
		t.Fatalf("rollup = %v, want degraded: a location holding one incomplete and one degraded system reads degraded", got)
	}
	if got := health.RollUp([]health.Verdict{health.Healthy, health.Incomplete}); got != health.Incomplete {
		t.Fatalf("rollup = %v, want incomplete", got)
	}
}

// An unstaffed role inside a LOSING alternate contributes nothing at all, not
// even incomplete. The whole point of a choice is that the alternate the room
// was not built to is not outstanding work: an all-in-one room must not read
// incomplete for the five component-built roles it deliberately never filled.
func TestLosingAlternateContributesNoIncomplete(t *testing.T) {
	allInOne := health.Alternate{Name: "all-in-one", Roles: []health.Role{
		{Name: "video-bar", Quorum: 1, Impact: "outage", Assigned: []health.Component{healthy("bar")}},
	}}
	componentBuilt := health.Alternate{Name: "component-system", Roles: []health.Role{
		unstaffed(1, "outage"), unstaffed(1, "outage"), unstaffed(3, "degraded"),
	}}
	c := health.Choice{Name: "conferencing", Alternates: []health.Alternate{allInOne, componentBuilt}}

	if got := c.Contributes(); got != health.Healthy {
		t.Fatalf("choice contributes = %v, want healthy: the room is built all-in-one, so the roles it never filled are not a gap", got)
	}
	if got := health.SystemVerdictWith(nil, []health.Choice{c}); got != health.Healthy {
		t.Fatalf("system verdict = %v, want healthy", got)
	}
}

// The verdict a system reads when its only shortfall is uninstalled hardware.
func TestSystemVerdictIncomplete(t *testing.T) {
	got := health.SystemVerdict([]health.Role{
		{Name: "display", Quorum: 1, Impact: "outage", Assigned: []health.Component{healthy("d")}},
		unstaffed(2, "outage"),
	})
	if got != health.Incomplete {
		t.Fatalf("system verdict = %v, want incomplete: one role is staffed and the other was never installed", got)
	}
}
