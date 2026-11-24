// Package health computes a fleet's health from resolved inputs. The
// rollup is a pure function on purpose: the subtle cases (quorum boundaries, a
// slot nobody staffed, a component an alarm has taken down) are where this
// gets quietly wrong, and they are far easier to pin down in a unit test than
// in SQL. Storage resolves the inputs and records the transitions; the
// judgement lives here.
package health

// Value is an entity's health. The zero value is Healthy, so an entity with
// nothing wrong needs no special casing.
type Value int

// The order IS the severity ranking: Worse and RollUp compare these values
// directly, so a new value is inserted at its rank rather than appended.
// Incomplete sits between Healthy and Degraded because a commissioning gap is
// worth surfacing above a clean system and worth burying under anything
// actually broken.
const (
	Healthy Value = iota
	// Incomplete is a slot short of quorum because the hardware was never
	// installed, rather than because installed hardware is failing. No alarm
	// will ever fire for it: nothing exists yet to alarm. It is deliberately
	// not a failure, because most of a real fleet is mid-commissioning for
	// months and folding that into Outage paints the whole canvas red and
	// teaches an operator to ignore it.
	Incomplete
	Degraded
	Outage
)

func (v Value) String() string {
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

// ParseValue reads a recorded health back. An unrecognized value is Healthy,
// so a stray row cannot make a fleet look broken.
func ParseValue(s string) Value {
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

// Worse returns the more severe of two health values. Rollup is worst-wins at
// every level: one slot in outage puts its system in outage regardless of how
// many other slots are fine.
func Worse(a, b Value) Value {
	if a > b {
		return a
	}
	return b
}

// ImpactValue maps a slot's declared impact to the value an impaired slot
// contributes. An unrecognized impact is Degraded rather than Healthy: an
// impaired slot should never be silently harmless because of a bad value.
func ImpactValue(impact string) Value {
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
// health, from its active alarms. An alarm impairs a component wholesale
// now (#626): there is no per-capability degradation left to resolve, so a
// component's own state is the whole of what a slot it occupies can see.
type Component struct {
	Name  string
	Value Value
}

// Occupies reports whether this component currently satisfies a slot it
// fills: a component whose own health is Outage (down) cannot be counted
// toward a slot's quorum, regardless of which slot asked or which alarm is
// behind it. A Degraded component still occupies: severity is how loudly to
// page somebody, not a second threshold for staffing, so an info or warning
// alarm never shorts a slot by itself. This is the mechanism by which a
// high-severity alarm reaches a system: impair the component down to Outage, and
// every slot it occupies loses one occupant.
func (c Component) Occupies() bool {
	return c.Value != Outage
}

// Slot is a slot as the rollup sees it: how many occupants it needs, what its
// failure means, and who was assigned to it.
type Slot struct {
	Name     string
	Quorum   int
	Impact   string
	Assigned []Component
}

// Satisfying counts the assigned components that currently occupy the slot
// (their own health is not Outage).
func (r Slot) Satisfying() int {
	n := 0
	for _, c := range r.Assigned {
		if c.Occupies() {
			n++
		}
	}
	return n
}

// want is the quorum floor Impaired, Short, and Spare all measure against. A
// quorum of zero or less is treated as one: a slot that wants nobody is not
// a slot.
func (r Slot) want() int {
	if r.Quorum < 1 {
		return 1
	}
	return r.Quorum
}

// Impaired reports whether the slot has fallen below its quorum.
func (r Slot) Impaired() bool {
	return r.Satisfying() < r.want()
}

// Short is how many more occupants the slot needs to reach quorum; zero
// once it does.
func (r Slot) Short() int {
	if n := r.want() - r.Satisfying(); n > 0 {
		return n
	}
	return 0
}

// Spare is how many occupants the slot has beyond quorum, the depth it has
// left before losing one would impair it; zero at or below quorum.
func (r Slot) Spare() int {
	if n := r.Satisfying() - r.want(); n > 0 {
		return n
	}
	return 0
}

// Down counts the assigned components that are not currently occupying the
// slot, which after #626 is exactly those whose own health is Outage.
func (r Slot) Down() int {
	return len(r.Assigned) - r.Satisfying()
}

// Contributes is the health this slot hands its system, and it distinguishes
// two different ways of being short.
//
// A slot whose assigned hardware is FAILING contributes what the slot declared
// that failure means: its Impact. A slot that is short because the hardware was
// never installed contributes Incomplete instead. Impact answers "what does it
// mean when this breaks", and an empty slot has not broken; treating the two
// alike would let a room that was never commissioned page somebody.
//
// The failing case is tested first, so a slot that is BOTH under-installed and
// partly alarming reports the live failure. That is the worse of the two by
// rank, and the actionable one: somebody is already on their way to the missing
// box, and nobody is on their way to the dead one.
// A slot that declares its own failure harmless (impact none) declares its own
// absence harmless too: reporting Incomplete for one would put a permanent
// commissioning gap on every confidence monitor nobody ever intends to staff,
// which is the same saturation Incomplete exists to prevent. An unrecognized
// impact still reads as a gap rather than harmless, matching ImpactValue.
func (r Slot) Contributes() Value {
	if r.Down() > 0 && r.Impaired() {
		return ImpactValue(r.Impact)
	}
	if len(r.Assigned) < r.want() && ImpactValue(r.Impact) != Healthy {
		return Incomplete
	}
	return Healthy
}

// Alternate is one way a choice can be satisfied: a named group of slots that
// either all count toward the choice together or not at all (#626). An
// all-in-one room's single video-bar slot and a component-built room's five
// separate slots are two alternates of the same choice: staffing either one
// answers the choice, and the other's unstaffed slots must not read as
// impairments of a system that chose the other way.
type Alternate struct {
	Name  string
	Slots []Slot
}

// Fraction is the share of this alternate's slots that are at quorum (not
// Impaired). An alternate with no slots is 0, never vacuously satisfied: a
// choice cannot be answered by a group that declares nothing, so a naive
// "count satisfied == count total" (0 == 0) must not read as fully satisfied.
func (a Alternate) Fraction() float64 {
	if len(a.Slots) == 0 {
		return 0
	}
	n := 0
	for _, r := range a.Slots {
		if !r.Impaired() {
			n++
		}
	}
	return float64(n) / float64(len(a.Slots))
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
// slots is skipped entirely, never eligible to be active: see Fraction.
// False when every alternate is empty, so the choice has nothing to be
// active about.
func (c Choice) Active() (Alternate, bool) {
	best := -1.0
	var active Alternate
	found := false
	for _, alt := range c.Alternates {
		if len(alt.Slots) == 0 {
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

// Contributes is the health this choice hands its system: the worst-wins
// fold of its active alternate's own slots, the same per-slot impact fold an
// unconditional slot set gets. Winning the choice does not make an
// alternate's own slots immune to their own impact: an active alternate that
// is itself short still contributes what its short slots declare. A choice
// with no non-empty alternate (nothing declared on either side) contributes
// nothing, the same as a system with no slots.
func (c Choice) Contributes() Value {
	active, ok := c.Active()
	if !ok {
		return Healthy
	}
	v := Healthy
	for _, r := range active.Slots {
		v = Worse(v, r.Contributes())
	}
	return v
}

// SystemValueWith rolls a system's slots into one health value, worst-wins,
// across two kinds of input: unconditional slots that always count, and
// choices where only the best-satisfied alternate's slots count (#626).
// SystemValue is the special case with no choices.
func SystemValueWith(unconditional []Slot, choices []Choice) Value {
	v := Healthy
	for _, r := range unconditional {
		v = Worse(v, r.Contributes())
	}
	for _, c := range choices {
		v = Worse(v, c.Contributes())
	}
	return v
}

// SystemValue rolls a system's slots into one health value, worst-wins. A
// system with no slots is Healthy: nothing has been claimed about it, and
// claiming otherwise would paint every unmodelled system as broken.
func SystemValue(slots []Slot) Value {
	return SystemValueWith(slots, nil)
}

// RollUp folds already-computed child values into a parent, worst-wins. It is
// how a location takes the worst of the systems placed in it and of the
// locations beneath it.
func RollUp(children []Value) Value {
	v := Healthy
	for _, c := range children {
		v = Worse(v, c)
	}
	return v
}

// The three health effects a severity level declares (#2558): what an open
// alarm at the level does to its own component. The effect is declared on the
// level, never implied by an alarm merely existing.
const (
	EffectNone     = "none"
	EffectDegraded = "degraded"
	EffectOutage   = "outage"
)

// ValidEffect reports whether s is one of the three declared effects.
func ValidEffect(s string) bool {
	return s == EffectNone || s == EffectDegraded || s == EffectOutage
}

// The ranks the pre-#2558 rule split on, kept only to derive the effect of a
// custom level created without one: below warning is none, below high is
// degraded, else outage.
const (
	warningRank = 20
	highRank    = 40
)

// DefaultEffect derives a level's health effect from its rank for a custom
// level that did not declare one, by the old floors.
func DefaultEffect(rank int) string {
	switch {
	case rank < warningRank:
		return EffectNone
	case rank < highRank:
		return EffectDegraded
	default:
		return EffectOutage
	}
}

// EffectValue maps a declared effect to the value an alarm contributes to its
// component. An unrecognized effect is Degraded rather than Healthy: an open
// alarm should never be silently harmless because of a bad value.
func EffectValue(effect string) Value {
	switch effect {
	case EffectNone:
		return Healthy
	case EffectOutage:
		return Outage
	default:
		return Degraded
	}
}

// ComponentValue is a component's own health, independent of any slot it
// fills, from the declared health effects of its open alarms' severity
// levels: an outage when any alarm declares outage, degraded when any
// declares degraded, and healthy when every open alarm declares none (or
// there are none). A component that fills no slot still shows what is wrong
// with it.
func ComponentValue(effects []string) Value {
	v := Healthy
	for _, e := range effects {
		v = Worse(v, EffectValue(e))
	}
	return v
}
