package scenario

import (
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
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
		{"unknown key", base + "calendar:\n  incidents: {}\n", `unknown field "incidents"`},
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

// #2769: a scenario scripts recurring problems; a bad one is refused at load
// naming the problem and the field.
func TestValidateChecksTheScriptedProblems(t *testing.T) {
	base := `name: t
standards:
  - name: sign-55
    system_type: sign
    products:
      panel: t-panel-55
      player: t-player
` + oneArchetype
	problem := func(extra string) string {
		return "calendar:\n  problems:\n    - name: dark\n      cadence: weekly\n      weekday: tuesday\n      at: \"14:00\"\n      for: 2h\n" + extra
	}
	good := problem("      scope: { standard: sign-55, role: panel }\n      impairment:\n        pin: { path: status.device.power, value: \"off\" }\n")
	m, err := Parse("t.yaml", []byte(base+good))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(m, fakeCatalog()); err != nil {
		t.Fatalf("a scripted problem is refused: %v", err)
	}
	ps := m.ScriptedProblems()
	if len(ps) != 1 || ps[0].Name != "dark" || ps[0].For != 2*time.Hour || ps[0].Scope.Role != "panel" || ps[0].Impairment.Pin.Value != "off" {
		t.Fatalf("parsed problems = %+v", ps)
	}
	scope := func(s, imp string) string { return problem("      scope: " + s + "\n      impairment: " + imp + "\n") }
	for _, tc := range []struct{ name, yaml, want string }{
		{"a standard the scenario lacks", scope("{ standard: mr99, role: panel }", "{ unplugged: true }"), `calendar.problems.dark: scope.standard "mr99" is no standard`},
		{"a role no slot has", scope("{ standard: sign-55, role: toaster }", "{ unplugged: true }"), `scope.role "toaster" is a slot of no standard`},
		{"a site no archetype has", scope("{ site: nowhere, role: panel }", "{ unplugged: true }"), `scope.site "nowhere"`},
		{"no scope", problem("      impairment: { unplugged: true }\n"), "scope names no standard"},
		{"two effects", scope("{ standard: sign-55, role: panel }", "{ unplugged: true, loss_pct: 5 }"), "exactly one"},
		{"an unknown field", scope("{ standard: sign-55, role: panel, product: x }", "{ unplugged: true }"), `unknown field "product"`},
		{"an unknown impairment field", scope("{ standard: sign-55, role: panel }", "{ latency_ms: 5 }"), `unknown field "latency_ms"`},
		{"an unknown pin field", scope("{ standard: sign-55, role: panel }", "{ pin: { path: a, val: b } }"), `unknown field "val"`},
		{"twice", good + "    - name: dark\n      cadence: daily\n      at: \"01:00\"\n      for: 1h\n      scope: { standard: sign-55, role: panel }\n      impairment: { unplugged: true }\n", "scripted twice"},
		{"not a list", base + "calendar:\n  problems: {}\n", "calendar.problems wants a list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := Parse("t.yaml", []byte(base+tc.yaml))
			if tc.name == "not a list" {
				m, err = Parse("t.yaml", []byte(tc.yaml))
			}
			if err == nil {
				err = Validate(m, fakeCatalog())
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// The shipped corporate scenario scripts its problems with the slots its
// standards have, and its world carries them, each device with the slot it
// fills, so a problem finds it by role.
func TestTheCorporateScenarioScriptsProblemsOnItsRoles(t *testing.T) {
	m, err := Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(m.ScriptedProblems()); n < 2 || n > 3 {
		t.Fatalf("corporate scripts %d problems, want two or three", n)
	}
	plan, err := Resolve(m, cat, 20)
	if err != nil {
		t.Fatal(err)
	}
	w, err := SiteWorld(m, cat, plan, "hq")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Problems) != len(m.ScriptedProblems()) {
		t.Fatalf("the world carries %d problems, the manifest scripts %d", len(w.Problems), len(m.ScriptedProblems()))
	}
	if got := w.SlotOf("hq-divisible-conference-1-wireless-mic-1"); got != "wireless-mic" {
		t.Fatalf("the world gives the wireless mic the slot %q", got)
	}
	cal := w.Calendar()
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC) // a Thursday
	var dead int
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			for _, e := range cal.Entries(calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}, day, day.AddDate(0, 0, 1), calendar.ProblemKinds) {
				if caltest.P(e).Name == "weekly-dead-mic-battery" {
					dead++
					if r.Standard != "divisible-conference" {
						t.Errorf("the dead battery landed in a %s room", r.Standard)
					}
				}
			}
		}
	}
	if dead == 0 {
		t.Error("no conference room has the weekly dead battery on a Thursday")
	}
	// A named room the plan does not have is refused, not silently scripted nowhere.
	m.Calendar.Problems[0].Scope.Rooms = []string{"hq-nowhere-9"}
	if _, err := SiteWorld(m, cat, plan, "hq"); err == nil || !strings.Contains(err.Error(), "hq-nowhere-9") {
		t.Fatalf("a problem naming a room the plan lacks: %v", err)
	}
}
