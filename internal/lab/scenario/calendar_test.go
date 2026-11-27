package scenario

import (
	"strings"
	"testing"
)

// #2764: a scenario declares the meetings its rooms hold as knobs per
// standard; a bad block is refused at load naming the field.
func TestValidateChecksTheCalendarBlock(t *testing.T) {
	base := `name: t
standards:
  - name: sign-55
    system_type: sign
    products:
      panel: t-panel-55
      player: t-player
` + oneArchetype
	block := func(knobs string) string {
		return "calendar:\n  meetings:\n    sign-55:\n" + knobs
	}
	good := "      booking_rate: 0.4\n      slot_mix: {15: 1, 30: 3, 60: 2}\n      no_show_rate: 0.1\n      ended_early_rate: 0.2\n      ended_late_rate: 0.1\n      attendance: {min: 2, max: 6}\n      walk_in_rate: 0.1\n      hours: {start: 8, end: 18}\n      recording_rate: 0.25\n"
	m, err := Parse("t.yaml", []byte(base+block(good)))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(m, fakeCatalog()); err != nil {
		t.Fatalf("a calendar block is refused: %v", err)
	}
	k, ok := m.MeetingKnobs("sign-55")
	if !ok || k.BookingRate != 0.4 || k.SlotMix[30] != 3 || k.Attendance.Max != 6 || k.Hours.End != 18 || k.RecordingRate != 0.25 {
		t.Fatalf("parsed knobs = %+v, %v", k, ok)
	}
	if _, ok := m.MeetingKnobs("mr65"); ok {
		t.Fatal("knobs for a standard the block does not name")
	}
	for _, tc := range []struct{ name, yaml, want string }{
		{"booking rate past 1", base + block("      booking_rate: 1.5\n      slot_mix: {30: 1}\n      attendance: {min: 1, max: 2}\n"), "calendar.meetings.sign-55.booking_rate"},
		{"recording rate past 1", base + block("      booking_rate: 0.5\n      slot_mix: {30: 1}\n      attendance: {min: 1, max: 2}\n      recording_rate: 1.5\n"), "calendar.meetings.sign-55.recording_rate"},
		{"a 45-minute slot", base + block("      booking_rate: 0.5\n      slot_mix: {45: 1}\n      attendance: {min: 1, max: 2}\n"), "calendar.meetings.sign-55.slot_mix"},
		{"inverted attendance", base + block("      booking_rate: 0.5\n      slot_mix: {30: 1}\n      attendance: {min: 5, max: 2}\n"), "calendar.meetings.sign-55.attendance"},
		{"not a standard", base + "calendar:\n  meetings:\n    mr99:\n      booking_rate: 0.5\n", `calendar.meetings.mr99: "mr99" is no standard`},
		{"unknown knob", base + block("      booking_rat: 0.5\n"), `unknown field "booking_rat"`},
		{"unknown key", base + "calendar:\n  problems: {}\n", `unknown field "problems"`},
		{"not a mapping", base + "calendar:\n  meetings: [a]\n", "calendar.meetings wants a mapping"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := Parse("t.yaml", []byte(tc.yaml))
			if err == nil {
				err = Validate(m, fakeCatalog())
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// The shipped corporate scenario gives every standard that has a room (not
// the signage) its meeting knobs.
func TestShippedCorporateDeclaresMeetingKnobs(t *testing.T) {
	m, cat := corporate(t)
	if err := Validate(m, cat); err != nil {
		t.Fatal(err)
	}
	for _, s := range m.Standards {
		_, ok := m.MeetingKnobs(s.Name.Name)
		if s.SystemType.Name == "sign" {
			if ok {
				t.Errorf("signage %s holds meetings", s.Name.Name)
			}
			continue
		}
		if !ok {
			t.Errorf("standard %s has no meeting knobs", s.Name.Name)
		}
	}
	// A share of every room's meetings is recorded (#2775).
	for _, s := range m.Standards {
		if k, ok := m.MeetingKnobs(s.Name.Name); ok && k.RecordingRate <= 0 {
			t.Errorf("standard %s records no meetings", s.Name.Name)
		}
	}
}
