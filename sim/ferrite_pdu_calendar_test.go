package sim

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
)

// The Ferrite PDU's outlets follow the room's day (#2777): what a rack draws
// is a pure function of which of the room's devices the lab calendar says are
// awake, read through the device's clock, never from the devices themselves.
// The levels below are the profile's fixed outlet-to-device map, in mA.
const (
	videoStandby, videoAwake, videoCall = 150, 700, 950 // outlet 1: displays, video bar, codec
	procStandby, procAwake              = 180, 300      // outlet 2: DSP and the control processor
	ampStandby, ampAwake, ampCall       = 120, 500, 800 // outlet 3: amplifier
)

// wantOutlets is the calendar's own answer for the four outlets at t,
// computed with no device: the room is awake by the displays' rule (wantAwake)
// and in a call when a meeting someone attends is running.
func wantOutlets(cal *calendar.Calendar, room calendar.Room, t time.Time) [4]int {
	call := false
	if e, ok := cal.Now(room, t); ok && e.Meeting.Outcome != calendar.OutcomeNoShow && e.Meeting.Actual > 0 {
		call = true
	}
	v, p, a := videoStandby, procStandby, ampStandby
	if wantAwake(cal, room, t) {
		v, p, a = videoAwake, procAwake, ampAwake
	}
	if call {
		v, a = videoCall, ampCall
	}
	return [4]int{v, p, a, 0}
}

func outlets(t *testing.T, d *Device) (o [4]int, total int) {
	t.Helper()
	for i := range o {
		f, _, err := toFloat(d.ReadPath(fmt.Sprintf("status.outlets.%d.current_ma", i)))
		if err != nil {
			t.Fatal(err)
		}
		o[i] = int(f)
	}
	f, _, err := toFloat(d.ReadPath("status.load.total_current_ma"))
	if err != nil {
		t.Fatal(err)
	}
	return o, int(f)
}

// Over a simulated day on a pinned clock, each outlet agrees with the room's
// calendar at every sampled minute, and the day holds a booked meeting, a
// walk-in and a no-show.
func TestFerritePDUOutletsFollowTheRoomsDay(t *testing.T) {
	k := dayKnobs()
	const seed = 21
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	cal := calOf(seed, k)
	var booked, walkIn, noShow int
	for _, e := range cal.Entries(roomHQ, day, day.Add(24*time.Hour)) {
		switch {
		case e.Meeting.Outcome == calendar.OutcomeNoShow:
			noShow++
		case e.Meeting.Origin == calendar.OriginWalkIn:
			walkIn++
		default:
			booked++
		}
	}
	if booked == 0 || walkIn == 0 || noShow == 0 {
		t.Fatalf("the pinned day holds %d booked, %d walk-in, %d no-show; pick a seed with all three", booked, walkIn, noShow)
	}

	at := day
	d := borealEngine(t, "ferrite-pdu-4c", seed, k, &at)
	var sawCall, sawStandby bool
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("load_drift")
		want := wantOutlets(cal, roomHQ, at)
		got, total := outlets(t, d)
		if got != want {
			t.Fatalf("%s: outlets %v mA, the calendar says %v", at.Format("15:04"), got, want)
		}
		if sum := want[0] + want[1] + want[2] + want[3]; total != sum {
			t.Fatalf("%s: total %d mA, the outlets draw %d", at.Format("15:04"), total, sum)
		}
		sawCall = sawCall || want[0] == videoCall
		sawStandby = sawStandby || want[0] == videoStandby
	}
	if !sawCall || !sawStandby {
		t.Fatalf("the day never showed both a call (%v) and a standby rack (%v)", sawCall, sawStandby)
	}
}

// An outlet an operator switched off draws nothing; the others follow the day.
func TestFerritePDUSwitchedOffOutletDrawsNothingUnderACalendar(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	d := borealEngine(t, "ferrite-pdu-4c", 21, dayKnobs(), &at)
	st := d.State()
	list := st["status"].(map[string]any)["outlets"].([]any)
	list[0].(map[string]any)["state"] = "off"
	d.SetState(st, 99)
	d.TickBehavior("load_drift")
	got, _ := outlets(t, d)
	if got[0] != 0 || got[1] == 0 || got[2] == 0 {
		t.Fatalf("outlets %v mA with outlet 1 switched off", got)
	}
}

// energyOver ticks the profile's load_drift every 30 s (its schedule) over a
// day in a room with the knobs, and returns the energy the rack accrued, mWh.
func energyOver(t *testing.T, k calendar.Knobs) int {
	t.Helper()
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	at := day
	d := borealEngine(t, "ferrite-pdu-4c", 21, k, &at)
	prev := 0
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(30 * time.Second) {
		d.TickBehavior("load_drift")
		f, _, err := toFloat(d.ReadPath("status.load.energy_mwh"))
		if err != nil {
			t.Fatal(err)
		}
		if int(f) < prev {
			t.Fatalf("%s: energy fell from %d to %d", at.Format("15:04:05"), prev, int(f))
		}
		prev = int(f)
	}
	return prev
}

// A busy room's rack uses more energy over a day than an idle room's with the
// same gear, and an idle rack still uses some (its gear is on standby).
func TestFerritePDUEnergyIsHigherForABusyRoom(t *testing.T) {
	busy := dayKnobs()
	idle := dayKnobs()
	idle.BookingRate, idle.WalkInRate = 0, 0
	eb, ei := energyOver(t, busy), energyOver(t, idle)
	if ei <= 0 || eb <= ei {
		t.Fatalf("energy over a day: busy %d mWh, idle %d mWh; want busy above idle above zero", eb, ei)
	}
}

// A PDU in a room with no calendar keeps the drift it had: no meeting reaches
// it, an outlet that is on walks inside its old bounds, and the spare stays off.
func TestFerritePDUWithoutACalendarKeepsItsDrift(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	d := borealEngine(t, "ferrite-pdu-4c", 21, dayKnobs(), &at)
	d.calSrc = func() *calendar.Calendar { return nil }
	moved := false
	first, _ := outlets(t, d)
	for i := 0; i < 50; i++ {
		d.TickBehavior("load_drift")
		got, total := outlets(t, d)
		for n := 0; n < 3; n++ {
			if got[n] < 100 || got[n] > 1200 {
				t.Fatalf("outlet %d drew %d mA, outside the old 100 to 1200 walk", n+1, got[n])
			}
		}
		if got[3] != 0 || total != got[0]+got[1]+got[2] {
			t.Fatalf("spare %d mA, total %d, outlets %v", got[3], total, got)
		}
		moved = moved || got != first
	}
	if !moved {
		t.Fatal("the outlets never moved: the random walk is gone")
	}
}
