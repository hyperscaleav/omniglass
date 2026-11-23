package health

import (
	"fmt"
	"sort"
	"strings"
)

// Members outside a slot (#2459, ADR-2459). A system's members are the
// components bound into it; a slot is what a member fills when the system's
// standard (or the system itself) says what losing it costs. A member that
// fills no slot used to contribute nothing, so a hand-built system (no
// standard, so no slots) read healthy forever, whatever was wrong inside it.
//
// The ruled rule: any open alarm on a member outside every slot makes its
// system Degraded, and never more. Slots alone decide Outage, because only a
// slot declares what the room loses; a member nobody declared is known to be in
// the room, and known to be wrong, and that is all.

// OutsideContributes is what the members outside every slot hand their system:
// Degraded once any of them carries an open alarm (its own value is not
// Healthy), whatever that alarm's severity, and Healthy otherwise.
func OutsideContributes(outside []Component) Value {
	for _, c := range outside {
		if c.Value != Healthy {
			return Degraded
		}
	}
	return Healthy
}

// SystemVerdict is a system's health from every input it has: its
// unconditional slots, its choices, and its members outside every slot,
// worst-wins. SystemValueWith is the case with no member outside a slot.
func SystemVerdict(unconditional []Slot, choices []Choice, outside []Component) Value {
	return Worse(SystemValueWith(unconditional, choices), OutsideContributes(outside))
}

// SystemReason is the verdict's explanation in one line: every contributor that
// is not healthy, worst first (slots in the order given, then the members
// outside a slot), or a plain statement that nothing is wrong. It is computed
// from the same inputs as SystemVerdict, so the two cannot disagree.
func SystemReason(unconditional []Slot, choices []Choice, outside []Component) string {
	type part struct {
		v    Value
		text string
	}
	var parts []part
	slots := append([]Slot(nil), unconditional...)
	for _, c := range choices {
		if active, ok := c.Active(); ok {
			slots = append(slots, active.Slots...)
		}
	}
	for _, r := range slots {
		if v := r.Contributes(); v != Healthy {
			parts = append(parts, part{v, slotReason(r)})
		}
	}
	var alarmed []string
	for _, c := range outside {
		if c.Value != Healthy {
			alarmed = append(alarmed, c.Name)
		}
	}
	if len(alarmed) > 0 {
		verb := "has"
		noun := "member"
		if len(alarmed) > 1 {
			verb, noun = "have", "members"
		}
		parts = append(parts, part{Degraded, fmt.Sprintf("%d %s outside a slot %s an open alarm (%s)", len(alarmed), noun, verb, strings.Join(alarmed, ", "))})
	}
	if len(parts) == 0 {
		return "no slot is impaired and no member has an open alarm"
	}
	// Worst first; equal contributors keep their order.
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].v > parts[j].v })
	texts := make([]string, len(parts))
	for i, p := range parts {
		texts[i] = p.text
	}
	return strings.Join(texts, "; ")
}

// slotReason words one non-healthy slot by the branch of Contributes that
// produced it: a live failure, a component still to attach, or a gap nobody
// installed.
func slotReason(r Slot) string {
	if r.Down() > 0 && r.Impaired() {
		return fmt.Sprintf("slot %s is impaired (%d of %d occupying)", r.Name, r.Satisfying(), r.want())
	}
	var pending []string
	for _, c := range r.Assigned {
		if c.ToAttach {
			pending = append(pending, c.Name)
		}
	}
	if len(r.Assigned) >= r.want() && len(pending) > 0 {
		noun := "component"
		if len(pending) > 1 {
			noun = "components"
		}
		return fmt.Sprintf("slot %s has %d %s still to attach (%s)", r.Name, len(pending), noun, strings.Join(pending, ", "))
	}
	return fmt.Sprintf("slot %s is short %d (never installed)", r.Name, r.want()-len(r.Assigned))
}
