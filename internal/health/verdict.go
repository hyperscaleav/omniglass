// Package health computes an estate's health verdict from resolved inputs. The
// rollup is a pure function on purpose: the subtle cases (quorum boundaries, a
// role nobody staffed, a component an alarm has taken down) are where this
// gets quietly wrong, and they are far easier to pin down in a unit test than
// in SQL. Storage resolves the inputs and records the transitions; the
// judgement lives here.
package health

// Verdict is an entity's health. The zero value is Healthy, so an entity with
// nothing wrong needs no special casing.
type Verdict int

const (
	Healthy Verdict = iota
	Degraded
	Outage
)

func (v Verdict) String() string {
	switch v {
	case Outage:
		return "outage"
	case Degraded:
		return "degraded"
	default:
		return "healthy"
	}
}

// ParseVerdict reads a recorded verdict back. An unrecognized value is Healthy,
// so a stray row cannot make an estate look broken.
func ParseVerdict(s string) Verdict {
	switch s {
	case "outage":
		return Outage
	case "degraded":
		return Degraded
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

// Impaired reports whether the role has fallen below its quorum. A quorum of
// zero or less is treated as one: a role that wants nobody is not a role.
func (r Role) Impaired() bool {
	want := r.Quorum
	if want < 1 {
		want = 1
	}
	return r.Satisfying() < want
}

// Contributes is the verdict this role hands its system: its declared impact
// when impaired, nothing when satisfied.
func (r Role) Contributes() Verdict {
	if !r.Impaired() {
		return Healthy
	}
	return ImpactVerdict(r.Impact)
}

// SystemVerdict rolls a system's roles into one verdict, worst-wins. A system
// with no roles is Healthy: nothing has been claimed about it, and claiming
// otherwise would paint every unmodelled system as broken.
func SystemVerdict(roles []Role) Verdict {
	v := Healthy
	for _, r := range roles {
		v = Worse(v, r.Contributes())
	}
	return v
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
