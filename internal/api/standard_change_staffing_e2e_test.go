package api_test

import (
	"net/http"
	"testing"
)

// TestChangingAStandardKeepsTheComponentStaffable is #506: repointing a system
// to a different standard carries each assignment onto the slot of the same
// name the new standard declares, when the component is still a typed match for
// it and the slot has room, and releases the rest. A released component stays a
// member and can be unassigned, unbound and deleted; nothing is left pointing at
// the old standard's slots, so moving back reactivates nothing.
func TestChangingAStandardKeepsTheComponentStaffable(t *testing.T) {
	t.Parallel()
	f := newStaffingFixture(t)
	c, owner := f.c, f.owner

	declare := func(std, slot string, body map[string]any) {
		c.do(owner, http.MethodPatch, "/standards/"+std+"/slots/"+slot, body, http.StatusOK)
	}
	for _, std := range []string{"std-old", "std-new", "std-strict"} {
		c.do(owner, http.MethodPost, "/standards", map[string]any{"name": std, "label": std}, http.StatusCreated)
	}
	declare("std-old", "table-mic", map[string]any{"accepted_types": []string{"video-bar"}})
	declare("std-old", "room-display", map[string]any{"accepted_types": []string{"display"}})
	declare("std-new", "table-mic", map[string]any{"accepted_types": []string{"video-bar"}})
	declare("std-new", "ceiling-mic", map[string]any{"accepted_types": []string{"video-bar"}})
	// The same name, a different requirement: the bar is no longer a match.
	declare("std-strict", "table-mic", map[string]any{"accepted_types": []string{"display"}})

	c.do(owner, http.MethodPost, "/systems", map[string]any{"name": "swap-room", "standard_id": "std-old"}, http.StatusCreated)
	f.component("swap-bar", f.bar)
	f.component("swap-display", f.display)
	c.do(owner, http.MethodPut, "/systems/swap-room/slots/table-mic/assignments/swap-bar", nil, http.StatusNoContent)
	c.do(owner, http.MethodPut, "/systems/swap-room/slots/room-display/assignments/swap-display", nil, http.StatusNoContent)

	c.do(owner, http.MethodPatch, "/systems/swap-room", map[string]any{"standard_id": "std-new"}, http.StatusOK)
	got := f.slots(owner, "swap-room")
	if mic := got.find(t, "table-mic"); len(mic.AssignedTo) != 1 || mic.AssignedTo[0] != "swap-bar" || !mic.FromStandard {
		t.Fatalf("table-mic after the move = %+v, want the bar carried onto std-new's table-mic", mic)
	}
	if len(got.Slots) != 2 {
		t.Fatalf("slots after the move = %+v, want std-new's two", got.Slots)
	}
	// The display's slot does not exist on std-new: it is released, still a
	// member, and nothing holds it.
	if n := f.count(`select count(*) from system_slot_assignment ra join component c on c.id = ra.component_id where c.name = 'swap-display'`); n != 0 {
		t.Fatalf("the display still holds %d assignment(s) after its slot left with the standard", n)
	}
	if members := f.memberNames("swap-room"); len(members) != 2 {
		t.Fatalf("members after the move = %v, want both still bound", members)
	}
	c.do(owner, http.MethodDelete, "/systems/swap-room/members/swap-display", nil, http.StatusNoContent)
	c.do(owner, http.MethodDelete, "/components/swap-display", nil, http.StatusNoContent)

	// A slot of the same name the bar no longer satisfies releases it rather
	// than staffing a mismatch.
	c.do(owner, http.MethodPatch, "/systems/swap-room", map[string]any{"standard_id": "std-strict"}, http.StatusOK)
	if mic := f.slots(owner, "swap-room").find(t, "table-mic"); len(mic.AssignedTo) != 0 {
		t.Fatalf("std-strict's table-mic = %+v, want the bar released (it wants a display)", mic)
	}
	// Moving back finds nothing to reactivate: the guard runs on every staffing.
	c.do(owner, http.MethodPatch, "/systems/swap-room", map[string]any{"standard_id": "std-old"}, http.StatusOK)
	if mic := f.slots(owner, "swap-room").find(t, "table-mic"); len(mic.AssignedTo) != 0 {
		t.Fatalf("std-old's table-mic after moving back = %+v, want nothing reactivated", mic)
	}
	c.do(owner, http.MethodPut, "/systems/swap-room/slots/table-mic/assignments/swap-bar", nil, http.StatusNoContent)

	// Clearing the standard makes it a one-off: every inherited assignment is
	// released, and the bar is unbound and deleted like any other member.
	c.do(owner, http.MethodPatch, "/systems/swap-room", map[string]any{"standard_id": ""}, http.StatusOK)
	if got := f.slots(owner, "swap-room"); len(got.Slots) != 0 {
		t.Fatalf("slots of the one-off = %+v, want none", got.Slots)
	}
	c.do(owner, http.MethodDelete, "/systems/swap-room/members/swap-bar", nil, http.StatusNoContent)
	c.do(owner, http.MethodDelete, "/components/swap-bar", nil, http.StatusNoContent)
}
