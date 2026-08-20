// Package health computes a fleet's health verdict from resolved inputs. The
// rollup is a pure function on purpose: the subtle cases (quorum boundaries, a
// role nobody staffed, a component an alarm has taken down) are where this
// gets quietly wrong, and they are far easier to pin down in a unit test than
// in SQL. Storage resolves the inputs and records the transitions; the
// judgement lives here.
package health

// Verdict is an entity's health. The zero value is Healthy, so an entity with
// nothing wrong needs no special casing.
type Verdict int

// The order IS the severity ranking: Worse and RollUp compare these values
// directly, so a new verdict is inserted at its rank rather than appended.
// Incomplete sits between Healthy and Degraded because a commissioning gap is
// worth surfacing above a clean system and worth burying under anything
// actually broken.
const (
	Healthy Verdict = iota
	// Incomplete is a role short of quorum because the hardware was never
	// installed, rather than because installed hardware is failing. No alarm
	// will ever fire for it: nothing exists yet to alarm. It is deliberately
	// not a failure, because most of a real fleet is mid-commissioning for
	// months and folding that into Outage paints the whole canvas red and
	// teaches an operator to ignore it.
	Incomplete
	Degraded
	Outage
)

func (v Verdict) String() string {
	switch v {
	case Outage:
		return "outage"
	case Degraded:
		return "degraded"
	case Incomplete:
		return "incomplete"
	default:
		return "healthy"
	}
}

// ParseVerdict reads a recorded verdict back. An unrecognized value is Healthy,
// so a stray row cannot make a fleet look broken.
func ParseVerdict(s string) Verdict {
	switch s {
	case "outage":
		return Outage
	case "degraded":
		return Degraded
	case "incomplete":
		return Incomplete
	default:
		return Healthy
	}
}

// Worse returns the more severe of two verdicts. Rollup is worst-wins at every
// level: one role in outage puts its system in outage regardless of how many
// other roles are fine.
func Worse(a, b Verdict) Verdict {
	if a > b {
		return a
	}
	return b
}

// ImpactVerdict maps a role's declared impact to the verdict an impaired role
// contributes. An unrecognized impact is Degraded rather than Healthy: an
// impaired role should never be silently harmless because of a bad value.
func ImpactVerdict(impact string) Verdict {
	switch impact {
	case "outage":
		return Outage
	case "none":
		return Healthy
	default:
		return Degraded
	}
}

// Component is a component as the rollup sees it: only its own current
// verdict, from its active alarms. An alarm impairs a component wholesale
// now (#626): there is no per-capability degradation left to resolve, so a
// component's condition is the whole of what a slot it occupies can see.
type Component struct {
	Name    string
	Verdict Verdict
}

// Occupies reports whether this component currently satisfies a slot it
// fills: a component whose own verdict is Outage (down) cannot be counted
// toward a role's quorum, regardless of which role asked or which alarm is
// behind it. A Degraded component still occupies: severity is how loudly to
// page somebody, not a second threshold for staffing, so an info or warning
// alarm never shorts a role by itself. This is the mechanism by which a
// critical alarm reaches a system: impair the component down to Outage, and
// every role it occupies loses one occupant.
func (c Component) Occupies() bool {
	return c.Verdict != Outage
}

// Role is a role as the rollup sees it: how many occupants it needs, what its
// failure means, and who was assigned to it.
type Role struct {
	Name     string
	Quorum   int
	Impact   string
	Assigned []Component
}

// Satisfying counts the assigned components that currently occupy the role
// (their own verdict is not Outage).
func (r Role) Satisfying() int {
	n := 0
	for _, c := range r.Assigned {
		if c.Occupies() {
			n++
		}
	}
	return n
}

// want is the quorum floor Impaired, Short, and Spare all measure against. A
// quorum of zero or less is treated as one: a role that wants nobody is not
// a role.
func (r Role) want() int {
	if r.Quorum < 1 {
		return 1
	}
	return r.Quorum
}

// Impaired reports whether the role has fallen below its quorum.
func (r Role) Impaired() bool {
	return r.Satisfying() < r.want()
}

// Short is how many more occupants the role needs to reach quorum; zero
// once it does.
func (r Role) Short() int {
	if n := r.want() - r.Satisfying(); n > 0 {
		return n
	}
	return 0
}

// Spare is how many occupants the role has beyond quorum, the depth it has
// left before losing one would impair it; zero at or below quorum.
func (r Role) Spare() int {
	if n := r.Satisfying() - r.want(); n > 0 {
		return n
	}
	return 0
}

// Down counts the assigned components that are not currently occupying the
// role, which after #626 is exactly those whose own verdict is Outage.
func (r Role) Down() int {
	return len(r.Assigned) - r.Satisfying()
}

// Contributes is the verdict this role hands its system, and it distinguishes
// two different ways of being short.
//
// A role whose assigned hardware is FAILING contributes what the role declared
// that failure means: its Impact. A role that is short because the hardware was
// never installed contributes Incomplete instead. Impact answers "what does it
// mean when this breaks", and an empty slot has not broken; treating the two
// alike would let a room that was never commissioned page somebody.
//
// The failing case is tested first, so a role that is BOTH under-installed and
// partly alarming reports the live failure. That is the worse of the two by
// rank, and the actionable one: somebody is already on their way to the missing
// box, and nobody is on their way to the dead one.
// A role that declares its own failure harmless (impact none) declares its own
// absence harmless too: reporting Incomplete for one would put a permanent
// commissioning gap on every confidence monitor nobody ever intends to staff,
// which is the same saturation Incomplete exists to prevent. An unrecognized
// impact still reads as a gap rather than harmless, matching ImpactVerdict.
func (r Role) Contributes() Verdict {
	if r.Down() > 0 && r.Impaired() {
		return ImpactVerdict(r.Impact)
	}
	if len(r.Assigned) < r.want() && ImpactVerdict(r.Impact) != Healthy {
		return Incomplete
	}
	return Healthy
}

// Alternate is one way a choice can be satisfied: a named group of roles that
// either all count toward the choice together or not at all (#626). An
// all-in-one room's single video-bar role and a component-built room's five
// separate roles are two alternates of the same choice: staffing either one
// answers the choice, and the other's unstaffed roles must not read as
// impairments of a system that chose the other way.
type Alternate struct {
	Name  string
	Roles []Role
}

// Fraction is the share of this alternate's roles that are at quorum (not
// Impaired). An alternate with no roles is 0, never vacuously satisfied: a
// choice cannot be answered by a group that declares nothing, so a naive
// "count satisfied == count total" (0 == 0) must not read as fully satisfied.
func (a Alternate) Fraction() float64 {
	if len(a.Roles) == 0 {
		return 0
	}
	n := 0
	for _, r := range a.Roles {
		if !r.Impaired() {
			n++
		}
	}
	return float64(n) / float64(len(a.Roles))
}

// Choice is an exclusive-or group: a system satisfies it through exactly one
// of its alternates, whichever is furthest along, not through all of them at
// once.
type Choice struct {
	Name string
	// Alternates is in declaration order: the order a tie between two
	// equally-satisfied alternates breaks by, and the order
	// choice_alternate.position assigns at the storage layer.
	Alternates []Alternate
}

// Active returns the best-satisfied alternate (the highest Fraction), ties
// broken by declaration order (the earlier one wins, checked with strict
// greater-than so a later tie never displaces it). An alternate with no
// roles is skipped entirely, never eligible to be active: see Fraction.
// False when every alternate is empty, so the choice has nothing to be
// active about.
func (c Choice) Active() (Alternate, bool) {
	best := -1.0
	var active Alternate
	found := false
	for _, alt := range c.Alternates {
		if len(alt.Roles) == 0 {
			continue
		}
		if f := alt.Fraction(); f > best {
			best = f
			active = alt
			found = true
		}
	}
	return active, found
}

// Contributes is the verdict this choice hands its system: the worst-wins
// fold of its active alternate's own roles, the same per-role impact fold an
// unconditional role set gets. Winning the choice does not make an
// alternate's own roles immune to their own impact: an active alternate that
// is itself short still contributes what its short roles declare. A choice
// with no non-empty alternate (nothing declared on either side) contributes
// nothing, the same as a system with no roles.
func (c Choice) Contributes() Verdict {
	active, ok := c.Active()
	if !ok {
		return Healthy
	}
	v := Healthy
	for _, r := range active.Roles {
		v = Worse(v, r.Contributes())
	}
	return v
}

// SystemVerdictWith rolls a system's roles into one verdict, worst-wins,
// across two kinds of input: unconditional roles that always count, and
// choices where only the best-satisfied alternate's roles count (#626).
// SystemVerdict is the special case with no choices.
func SystemVerdictWith(unconditional []Role, choices []Choice) Verdict {
	v := Healthy
	for _, r := range unconditional {
		v = Worse(v, r.Contributes())
	}
	for _, c := range choices {
		v = Worse(v, c.Contributes())
	}
	return v
}

// SystemVerdict rolls a system's roles into one verdict, worst-wins. A system
// with no roles is Healthy: nothing has been claimed about it, and claiming
// otherwise would paint every unmodelled system as broken.
func SystemVerdict(roles []Role) Verdict {
	return SystemVerdictWith(roles, nil)
}

// RollUp folds already-computed child verdicts into a parent, worst-wins. It is
// how a location takes the worst of the systems placed in it and of the
// locations beneath it.
func RollUp(children []Verdict) Verdict {
	v := Healthy
	for _, c := range children {
		v = Worse(v, c)
	}
	return v
}

// ComponentVerdict is a component's own health, independent of any role it
// fills: degraded once any alarm is active against it, and an outage when an
// active alarm is critical. A component that fills no role still shows what is
// wrong with it.
func ComponentVerdict(severities []string) Verdict {
	v := Healthy
	for _, s := range severities {
		if s == "critical" {
			v = Worse(v, Outage)
			continue
		}
		v = Worse(v, Degraded)
	}
	return v
}
