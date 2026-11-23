package health

import (
	"fmt"
	"sort"
	"strings"
)

// SystemReason is the verdict's explanation in one line: every slot that
// contributes something other than healthy, worst first (ties in the order
// given), or a plain statement that no slot is impaired. It reads the same
// inputs as SystemValueWith, so the two cannot disagree. A member outside every
// slot is never named (ADR-2459): the slot and its impact are the system-level
// health rule, and such a member's alarms are shown beside the verdict, not
// folded into it.
func SystemReason(unconditional []Slot, choices []Choice) string {
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
	if len(parts) == 0 {
		return "no slot is impaired"
	}
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].v > parts[j].v })
	texts := make([]string, len(parts))
	for i, p := range parts {
		texts[i] = p.text
	}
	return strings.Join(texts, "; ")
}

// slotReason words one non-healthy slot by the branch of Contributes that
// produced it: a live failure, or a gap nobody installed.
func slotReason(r Slot) string {
	if r.Down() > 0 && r.Impaired() {
		return fmt.Sprintf("slot %s is impaired (%d of %d occupying)", r.Name, r.Satisfying(), r.want())
	}
	return fmt.Sprintf("slot %s is short %d (never installed)", r.Name, r.want()-len(r.Assigned))
}
