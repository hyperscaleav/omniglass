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

// The NVP room surface reads the device's own input and scene, and a command
// reports the change it made for the engine to apply: the surface writes
// nothing itself.
func TestNVPRoomReadsTheDevicesStateAndReportsItsChanges(t *testing.T) {
	f := spectest.NewFake(spec.Spec{Name: "room", State: map[string]any{"status": map[string]any{
		"video": map[string]any{"input": "dp1"}, "room": map[string]any{"scene": "vc"},
	}}}, "")
	room := bind.NVPRoom(f)
	if r, ok := room.Handle("GET INPUT"); !ok || r.Text != "INPUT dp1" {
		t.Fatalf("GET INPUT = %+v %v", r, ok)
	}
	if r, ok := room.Handle("GET SCENE"); !ok || r.Text != "SCENE vc" {
		t.Fatalf("GET SCENE = %+v %v", r, ok)
	}
	r, ok := room.Handle("SET INPUT usbc")
	if !ok || r.Text != "INPUT usbc" || len(r.Changes) != 1 || r.Changes[0].Path != "status.video.input" || r.Changes[0].Value != "usbc" {
		t.Fatalf("SET INPUT usbc = %+v %v", r, ok)
	}
	if len(f.Writes) != 0 || spec.String(f, "status.video.input") != "dp1" {
		t.Fatalf("the surface wrote to the device itself: %+v", f.Writes)
	}
}
