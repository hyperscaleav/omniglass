//go:build linux

package sim

import "testing"

// The slate panel answers its room's booked meeting now and next over NVP
// (#2765): "none" with no calendar, and what its behavior read once it has.
func TestSlatePanelAnswersCurrentAndNextOverNVP(t *testing.T) {
	target, d := startNVP(t, "newtron-slate-5")
	ask := nvpAsk(t, target)
	if got := ask("GET CURRENT"); got != "CURRENT none" {
		t.Errorf("GET CURRENT = %q, want none at rest", got)
	}
	if got := ask("GET NEXT"); got != "NEXT none" {
		t.Errorf("GET NEXT = %q, want none at rest", got)
	}
	d.SetState(map[string]any{"status": map[string]any{"room": map[string]any{
		"booking": "booked", "cal_booking": "booked",
		"current_meeting": "1793754000 1793757600 Indira Rao", "next_meeting": "1793761200 1793763000 Kai Abara"}}}, 3)
	if got := ask("GET CURRENT"); got != "CURRENT 1793754000 1793757600 Indira Rao" {
		t.Errorf("GET CURRENT = %q", got)
	}
	if got := ask("GET NEXT"); got != "NEXT 1793761200 1793763000 Kai Abara" {
		t.Errorf("GET NEXT = %q", got)
	}
}
