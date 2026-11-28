package newtroncore

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

// The model table is the line: each row a product that reports itself, the
// catalog and the row agreeing.
func TestEveryRowIsAProductThatReportsItself(t *testing.T) {
	cat, err := spec.NewCatalog(Spec())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Rows {
		res, ok := cat.ForProduct(r.Product)
		if !ok || res.Definition.ID() != Name {
			t.Fatalf("%s resolves to %+v, want the %s family", r.Product, res.Definition, Name)
		}
		if res.Identity.Model != r.Model || res.Identity.Manufacturer != "Newtron" || res.Identity.DeviceType != "control-processor" || res.Identity.Summary != r.Summary() {
			t.Errorf("%s: identity = %+v", r.Product, res.Identity)
		}
		f := spectest.NewFake(Spec(), r.Product)
		if spec.String(f, pathProgram) != running || spec.String(f, pathPorts) != r.Ports || spec.Int(f, pathPanels) != r.Panels || spec.String(f, pathFirmware) != r.Firmware {
			t.Errorf("%s: state does not carry its row", r.Product)
		}
	}
}

const pathFirmware = "configuration.firmware.version"

func ask(t *testing.T, f *spectest.Fake, line string) string {
	t.Helper()
	for _, c := range surfaceOf(f).Commanders {
		if r, ok := c.Handle(line); ok {
			for _, ch := range r.Changes {
				_ = f.Write(ch.Path, ch.Value)
			}
			return r.Text
		}
	}
	return "-ERR " + line
}

// The processor answers the panel's room, and its own program, ports, panels,
// room power, temperature and macro, each as the NVP driver reads them.
func TestTheProcessorAnswersItsOwnLinesBesideThePanelsRoom(t *testing.T) {
	f := spectest.NewFake(Spec(), "newtron-core-3")
	for _, tc := range [][2]string{
		{"GET INPUT", "INPUT hdmi1"}, {"GET SCENE", "SCENE idle"}, {"GET PROGRAM", "PROGRAM running"},
		{"GET PORTS", "PORTS com1=connected com2=connected ir1=idle relay1=open"}, {"GET PANELS", "PANELS 3"},
		{"GET ROOMPOWER", "ROOMPOWER standby"}, {"GET ROOMTEMP", "ROOMTEMP 21.5"}, {"GET MACRO", "MACRO none 0"},
		{"SET PROGRAM stopped", "PROGRAM stopped"}, {"SET INPUT dp1", "INPUT dp1"}, {"SET SCENE vc", "SCENE vc"},
	} {
		if got := ask(t, f, tc[0]); got != tc[1] {
			t.Errorf("%s = %q, want %q", tc[0], got, tc[1])
		}
	}
	if got := ask(t, f, "GET PROGRAM"); got != "PROGRAM stopped" {
		t.Errorf("the program after the stop reads %q", got)
	}
	for _, line := range []string{"SET PROGRAM paused", "SET PROGRAM", "get program", "GET BOOKING"} {
		if got := ask(t, f, line); got != "-ERR "+line {
			t.Errorf("%q = %q, want it refused as no command", line, got)
		}
	}
}

// A processor lists its own lines after the panel's, the order a shell's help
// names them in, and subscribes to the program beside the panel's subjects.
func TestTheProcessorListsAndPublishesWhatItAnswers(t *testing.T) {
	f := spectest.NewFake(Spec(), "newtron-core-3")
	sf := surfaceOf(f)
	var lines []string
	for _, c := range sf.Commanders {
		lines = append(lines, c.Commands()...)
	}
	if len(lines) != 24 || lines[0] != "GET INPUT" || lines[14] != "GET PROGRAM" || lines[len(lines)-1] != "UNSUBSCRIBE PROGRAM" {
		t.Fatalf("help lists %d lines, from %q to %q", len(lines), lines[0], lines[len(lines)-1])
	}
	var subjects []string
	for _, s := range sf.Subjects["nvp"] {
		subjects = append(subjects, s.Match)
	}
	if fmt.Sprint(subjects) != "[status.control.program status.video.input status.diagnostics.log status.room.scene]" {
		t.Errorf("subjects = %v", subjects)
	}
	for _, s := range sf.Subjects["nvp"] {
		if s.Path == pathProgram && s.Push("stopped") != "EVT PROGRAM stopped" {
			t.Errorf("a program change is pushed as %q", s.Push("stopped"))
		}
	}
}

func run(f *spectest.Fake, tick spec.Tick, room calendar.RoomState, now time.Time) {
	for _, ch := range tick.Run(f, spec.TickContext{Room: room, Now: now, Rand: behave.NewSource(f.ID)}) {
		_ = f.Write(ch.Path, ch.Value)
	}
}

func tickNamed(t *testing.T, name string) spec.Tick {
	t.Helper()
	for _, tk := range Spec().Ticks {
		if tk.Name == name {
			return tk
		}
	}
	t.Fatalf("no tick %s", name)
	return spec.Tick{}
}

// The room starts for a meeting and ends after it: the macro that ran as it
// went on or to standby is held beside the second it ran, and a room with no
// calendar runs none and sits at its setpoint.
func TestTheMeetingStartsAndEndsTheRoom(t *testing.T) {
	cal := calendar.New(24, func(calendar.Room) (calendar.Knobs, bool) {
		return calendar.Knobs{BookingRate: 0.5, SlotMix: map[int]float64{15: 1, 30: 2, 60: 2}, NoShowRate: 0.2, EndEarlyRate: 0.2, EndLateRate: 0.15,
			Attendance: calendar.Range{Min: 2, Max: 6}, WalkInRate: 0.25, Hours: calendar.Hours{Start: 8, End: 14}}, true
	})
	room := calendar.Room{Site: "hq", Building: "tower", Room: "tower-1-01"}
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)

	f := spectest.NewFake(Spec(), "newtron-core-3")
	meeting, climate := tickNamed(t, "room_meeting"), tickNamed(t, "room_climate")
	var starts, ends int
	var warmest float64
	for at := day.Add(7 * time.Hour); at.Before(day.Add(15 * time.Hour)); at = at.Add(30 * time.Second) {
		before := spec.String(f, pathMacro) + spec.String(f, pathMacroAt)
		rs := cal.StateAt(room, at)
		run(f, meeting, rs, at)
		run(f, climate, rs, at)
		if spec.String(f, pathMacro)+spec.String(f, pathMacroAt) != before {
			switch spec.String(f, pathMacro) {
			case "start-meeting":
				starts++
			case "end-meeting":
				ends++
			}
			if spec.Int64(f, pathMacroAt) != at.Unix() {
				t.Fatalf("%s: the macro ran at %d", at.Format("15:04:05"), spec.Int64(f, pathMacroAt))
			}
		}
		if (spec.String(f, pathRoomPower) == "on") != rs.InUse {
			t.Fatalf("%s: room power %s with the room in use %v", at.Format("15:04:05"), spec.String(f, pathRoomPower), rs.InUse)
		}
		warmest = max(warmest, spec.Float(f, pathClimate))
	}
	if starts == 0 || starts != ends || warmest <= 21.5 {
		t.Fatalf("%d starts, %d ends, warmest %v: want every meeting started and ended and the room warmed", starts, ends, warmest)
	}

	idle := spectest.NewFake(Spec(), "newtron-core-3")
	for i := 0; i < 60; i++ {
		at := day.Add(10*time.Hour + time.Duration(i)*30*time.Second)
		run(idle, meeting, spectest.IdleRoom(), at)
		run(idle, climate, spectest.IdleRoom(), at)
		if idle.Read(pathMacro) != "none" || spec.String(idle, pathRoomPower) != "standby" || spec.Float(idle, pathClimate) != 21.5 {
			t.Fatalf("no calendar, tick %d: macro %v power %v climate %v", i, idle.Read(pathMacro), idle.Read(pathRoomPower), idle.Read(pathClimate))
		}
	}
}
