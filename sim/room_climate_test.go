package sim

import (
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"
)

// The control processor reports the room's temperature (#2757), as a real
// processor polls a room sensor: it warms with the people in a held meeting
// toward the setpoint plus their load, and settles back after. With no
// calendar it reads the setpoint with only noise. Every reading derives from
// the calendar through the device's clock.

const climateSetpoint = 21.5

func readTemp(t *testing.T, d *Device) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(fmt.Sprint(d.ReadPath("status.room.temperature_c")), 64)
	if err != nil {
		t.Fatalf("status.room.temperature_c = %v: %v", d.ReadPath("status.room.temperature_c"), err)
	}
	return v
}

func TestControlProcessorRoomTemperatureWarmsInAMeetingAndSettlesAfter(t *testing.T) {
	k, seed, day, cal := pinnedDay(t)
	at := day
	d := roomEngine(t, seed, k, &at, "newtron-core-3")["newtron-core-3"]
	var inMeeting, quiet []float64
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_climate")
		got := readTemp(t, d)
		if got < climateSetpoint-0.5 || got > climateSetpoint+4.5 {
			t.Fatalf("%s: room temperature %.1f outside the sane band", at.Format("15:04"), got)
		}
		if e, ok := heldAt(cal, at); ok && at.Sub(e.Start) >= 15*time.Minute {
			inMeeting = append(inMeeting, got)
			continue
		}
		// Quiet: nothing held now or in the previous hour and a half.
		settled := true
		for back := time.Duration(0); back <= 90*time.Minute; back += 5 * time.Minute {
			if _, ok := heldAt(cal, at.Add(-back)); ok {
				settled = false
				break
			}
		}
		if settled {
			quiet = append(quiet, got)
		}
	}
	if len(inMeeting) == 0 || len(quiet) == 0 {
		t.Fatalf("the day sampled %d in-meeting and %d quiet minutes; want both", len(inMeeting), len(quiet))
	}
	im, qm := mean(inMeeting), mean(quiet)
	if im < qm+0.5 {
		t.Errorf("a held meeting reads %.2f, quiet %.2f: the room should warm with its people", im, qm)
	}
	if math.Abs(qm-climateSetpoint) > 0.3 {
		t.Errorf("a settled room reads %.2f, want the setpoint %.1f", qm, climateSetpoint)
	}
}

func TestControlProcessorRoomTemperatureIsDeterministicForACalendarAndClock(t *testing.T) {
	k, seed, day, _ := pinnedDay(t)
	run := func() []float64 {
		at := day
		d := roomEngine(t, seed, k, &at, "newtron-core-3")["newtron-core-3"]
		var out []float64
		for at = day.Add(9 * time.Hour); at.Before(day.Add(12 * time.Hour)); at = at.Add(time.Minute) {
			d.TickBehavior("room_climate")
			out = append(out, readTemp(t, d))
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("minute %d reads %.1f then %.1f for the same calendar and clock", i, a[i], b[i])
		}
	}
}

func TestControlProcessorRoomTemperatureIsSteadyWithNoCalendar(t *testing.T) {
	e := shippedEngine(t)
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: "d", Hostname: "d", LoopbackIP: "127.0.0.1", ProfileName: "newtron-core-3", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	d.SetClock(pinned(&at))
	for i := 0; i < 240; i++ {
		d.TickBehavior("room_climate")
		if got := readTemp(t, d); math.Abs(got-climateSetpoint) > 0.2 {
			t.Fatalf("tick %d: a processor with no calendar reads %.1f, want the setpoint %.1f within noise", i, got, climateSetpoint)
		}
		at = at.Add(time.Minute)
	}
}

func mean(v []float64) float64 {
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}
