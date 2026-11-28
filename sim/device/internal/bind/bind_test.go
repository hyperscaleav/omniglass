package bind_test

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/device/internal/bind"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

const input = "status.display.input"

func family(ticks ...spec.Tick) spec.Spec {
	return spec.Spec{Name: "bound", Ticks: ticks, State: map[string]any{
		"status": map[string]any{
			"device":  map[string]any{"power": "standby"},
			"display": map[string]any{"input": "usbc", "temperature": 40, "hours": 10.0},
			"sim":     bind.RoomDefaults(),
		},
	}}
}

func apply(f *spectest.Fake, t spec.Tick, c spec.TickContext) {
	for _, ch := range t.Run(f, c) {
		_ = f.Write(ch.Path, ch.Value)
	}
}

var room = calendar.Room{Site: "hq", Building: "tower", Room: "tower-1-01"}

// The bound tick is the primitive over the state paths: the device wakes on the
// edge of the room going busy with the conferencing input, and the bookkeeping
// it keeps is what the primitive returns, step for step.
func TestPowerFollowsRoomIsThePrimitiveOverTheStatePaths(t *testing.T) {
	tick := bind.PowerFollowsRoom(input)
	if tick.Name != "room_meeting" || tick.Interval != 30*time.Second || !tick.OnStart {
		t.Fatalf("tick = %s every %v on start %v", tick.Name, tick.Interval, tick.OnStart)
	}
	f := spectest.NewFake(family(tick), "")
	cal := calendar.New(24, func(calendar.Room) (calendar.Knobs, bool) {
		return calendar.Knobs{BookingRate: 0.5, SlotMix: map[int]float64{15: 1, 30: 2, 60: 2}, NoShowRate: 0.2, EndEarlyRate: 0.2, EndLateRate: 0.15,
			Attendance: calendar.Range{Min: 2, Max: 6}, WalkInRate: 0.25, Hours: calendar.Hours{Start: 8, End: 14}}, true
	})
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	prev := behave.PowerState{Power: "standby"}
	var woke int
	for at := day.Add(7 * time.Hour); at.Before(day.Add(15 * time.Hour)); at = at.Add(30 * time.Second) {
		rs := cal.StateAt(room, at)
		next, edge := behave.PowerFollowsRoom(prev, rs, at, behave.DefaultPower)
		wantInput := behave.InputFollowsMeeting(spec.String(f, input), edge, behave.DefaultPower)
		apply(f, tick, spec.TickContext{Room: rs, Now: at})
		got := behave.PowerState{Power: spec.String(f, bind.PathPower), Busy: spec.Bool(f, bind.PathBusy), Known: spec.Bool(f, bind.PathKnown),
			Awake: spec.Bool(f, bind.PathAwake), Until: spec.Int64(f, bind.PathUntil)}
		if got != next || spec.String(f, input) != wantInput {
			t.Fatalf("%s: state %+v input %q, the primitive says %+v input %q", at.Format("15:04:05"), got, spec.String(f, input), next, wantInput)
		}
		if edge {
			woke++
		}
		prev = next
	}
	if woke == 0 {
		t.Fatal("the pinned day never woke the device")
	}
}

func TestPowerFollowsRoomWithNoCalendarLeavesWhatTheOperatorSet(t *testing.T) {
	tick := bind.PowerFollowsRoom(input)
	f := spectest.NewFake(family(tick), "")
	apply(f, tick, spec.TickContext{Room: spectest.IdleRoom(), Now: f.Clock})
	if f.Read(bind.PathPower) != "standby" || f.Read(input) != "usbc" {
		t.Fatalf("power %v input %v, want the operator's standby and usbc held", f.Read(bind.PathPower), f.Read(input))
	}
}

func TestDriftWritesOnePathWithinItsBandFromTheDevicesSource(t *testing.T) {
	tick := bind.Drift("temperature_drift", bind.Seconds(60), "status.display.temperature", behave.Band{Lo: 34, Hi: 54, Step: 1}, behave.SigFigs(3))
	if len(tick.Writes) != 1 || tick.Writes[0] != "status.display.temperature" || tick.Interval != time.Minute {
		t.Fatalf("tick = %+v", tick)
	}
	f := spectest.NewFake(family(tick), "")
	rng, twin := behave.NewSource("d"), behave.NewSource("d")
	v := 40.0
	for i := 0; i < 50; i++ {
		apply(f, tick, spec.TickContext{Rand: rng})
		v = behave.Drift(v, behave.Band{Lo: 34, Hi: 54, Step: 1}, twin, behave.SigFigs(3))
		if got := spec.Float(f, "status.display.temperature"); got != v {
			t.Fatalf("step %d: %v, the primitive says %v", i, got, v)
		}
	}
}

func TestWearAddsOnlyWhileThePowerReadsOn(t *testing.T) {
	tick := bind.Wear("aging", bind.Seconds(60), "status.display.hours", behave.DefaultLightSource)
	f := spectest.NewFake(family(tick), "")
	apply(f, tick, spec.TickContext{})
	if got := spec.Float(f, "status.display.hours"); got != 10 {
		t.Fatalf("in standby the hours moved to %v", got)
	}
	_ = f.Write(bind.PathPower, "on")
	apply(f, tick, spec.TickContext{})
	if got := spec.Float(f, "status.display.hours"); got != 10.0167 {
		t.Fatalf("lit for a minute the hours are %v, want 10.0167", got)
	}
}

// A whole-number drift is the primitive over the path, written as the integer it
// rounds to, held in the band and drawn from the device's own source.
func TestDriftWholeWritesTheIntegerTheBandKeeps(t *testing.T) {
	const path = "status.mains.voltage_mv"
	band := behave.Band{Lo: 226000, Hi: 234000, Step: 600}
	tick := bind.DriftWhole("mains_drift", 30*time.Second, path, band)
	if tick.Name != "mains_drift" || tick.Interval != 30*time.Second || len(tick.Writes) != 1 || tick.Writes[0] != path {
		t.Fatalf("tick = %+v", tick)
	}
	f := spectest.NewFake(spec.Spec{Name: "bound", State: map[string]any{"status": map[string]any{"mains": map[string]any{"voltage_mv": 230100}}}}, "")
	src, twin := behave.NewSource("d-1"), behave.NewSource("d-1")
	prev := 230100.0
	for i := 0; i < 50; i++ {
		want := behave.Drift(prev, band, twin, behave.Places(0))
		got := tick.Run(f, spec.TickContext{Rand: src})
		if len(got) != 1 || got[0].Path != path || got[0].Value != int64(want) {
			t.Fatalf("tick %d wrote %+v (%T), the primitive says %v", i, got, got[0].Value, int64(want))
		}
		_ = f.Write(path, got[0].Value)
		prev = want
	}
}
