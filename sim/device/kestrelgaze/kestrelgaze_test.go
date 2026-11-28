package kestrelgaze

import (
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/behave"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/spec/spectest"
)

func TestFamilyConforms(t *testing.T) { spectest.Conform(t, Spec()) }

func call(t *testing.T, f *spectest.Fake, match string, body map[string]any) spec.HTTPResponse {
	t.Helper()
	for _, r := range Spec().HTTP {
		if r.Match == match {
			return r.Handle(f, spec.HTTPRequest{Body: body})
		}
	}
	t.Fatalf("no route %s", match)
	return spec.HTTPResponse{}
}

func tick(t *testing.T, name string) spec.Tick {
	t.Helper()
	for _, tk := range Spec().Ticks {
		if tk.Name == name {
			return tk
		}
	}
	t.Fatalf("no tick %s", name)
	return spec.Tick{}
}

func apply(f *spectest.Fake, tk spec.Tick, c spec.TickContext) {
	for _, ch := range tk.Run(f, c) {
		_ = f.Write(ch.Path, ch.Value)
	}
}

// A preset recall moves the lens to the stored position and takes the camera off
// tracking; a preset it lacks is refused with nothing written.
func TestRecallingAPresetMovesTheLensAndStopsTracking(t *testing.T) {
	for _, tc := range []struct {
		preset    string
		pan, tilt any
		mag       any
	}{
		{"1", int64(-30), int64(-5), int64(1)},
		{"2", int64(30), int64(5), 2.5},
		{"3", int64(0), int64(10), int64(4)},
	} {
		f := spectest.NewFake(Spec(), "kestrel-gaze-p2")
		if res := call(t, f, "POST /api/camera/preset", map[string]any{"preset": tc.preset}); res.Status != 0 {
			t.Fatalf("preset %s: status %d", tc.preset, res.Status)
		}
		n := int64(tc.preset[0] - '0')
		if f.Read(pathPreset) != n || f.Read(pathPan) != tc.pan || f.Read(pathTilt) != tc.tilt || f.Read(pathMag) != tc.mag || f.Read(pathTracking) != manual {
			t.Errorf("preset %s: %v pan %v tilt %v mag %v %v", tc.preset, f.Read(pathPreset), f.Read(pathPan), f.Read(pathTilt), f.Read(pathMag), f.Read(pathTracking))
		}
	}
	f := spectest.NewFake(Spec(), "kestrel-gaze-p2")
	if res := call(t, f, "POST /api/camera/preset", map[string]any{"preset": "4"}); res.Status != 400 || len(f.Writes) != 0 {
		t.Fatalf("a preset the camera lacks: %d, %d writes", res.Status, len(f.Writes))
	}
}

func TestPointingByHandLeavesThePresetAndTracking(t *testing.T) {
	f := spectest.NewFake(Spec(), "kestrel-gaze-p2")
	call(t, f, "POST /api/camera/preset", map[string]any{"preset": "2"})
	call(t, f, "POST /api/camera/position", map[string]any{"pan": -12.5, "tilt": float64(4), "magnification": "1.5"})
	if f.Read(pathPan) != -12.5 || f.Read(pathTilt) != int64(4) || f.Read(pathMag) != 1.5 || f.Read(pathPreset) != 0 || f.Read(pathTracking) != manual {
		t.Errorf("pan %v tilt %v mag %v preset %v %v", f.Read(pathPan), f.Read(pathTilt), f.Read(pathMag), f.Read(pathPreset), f.Read(pathTracking))
	}
	if res := call(t, f, "POST /api/camera/position", map[string]any{"pan": "left", "tilt": float64(1), "magnification": float64(1)}); res.Status != 400 {
		t.Errorf("a pan that is not a number: %d", res.Status)
	}
}

func TestRebootReturnsTheCameraHome(t *testing.T) {
	f := spectest.NewFake(Spec(), "kestrel-gaze-p2")
	call(t, f, "POST /api/camera/preset", map[string]any{"preset": "1"})
	call(t, f, "POST /api/admin/reboot", nil)
	if spec.Float(f, pathPan) != 0 || spec.Float(f, pathMag) != 1 || f.Read(pathPreset) != 0 || f.Read(pathTracking) != tracking || f.Read(pathBootEpoch) != f.Clock.Unix() {
		t.Errorf("after a reboot: pan %v mag %v preset %v %v anchor %v", f.Read(pathPan), f.Read(pathMag), f.Read(pathPreset), f.Read(pathTracking), f.Read(pathBootEpoch))
	}
}

// meeting is a room with a calendar and a meeting of the given attendees running
// from the given start.
func meeting(start time.Time, actual int, outcome calendar.Outcome) calendar.RoomState {
	e := calendar.MeetingEntry(start, start.Add(time.Hour), calendar.Room{Site: "hq", Building: "tower", Room: "r1"}, calendar.Meeting{Actual: actual, Outcome: outcome})
	return calendar.RoomState{HasCalendar: true, HasMeeting: true, Meeting: e, CallActive: outcome != calendar.OutcomeNoShow, Attendees: actual}
}

// The camera frames the room's group when a meeting starts (tighter for fewer
// people), keeps an operator's change inside it, and goes home when it ends.
func TestTheCameraFramesTheGroupAtTheEdgesOfAMeeting(t *testing.T) {
	f := spectest.NewFake(Spec(), "kestrel-gaze-p2")
	room := tick(t, "room_meeting")
	start := f.Clock
	apply(f, room, spec.TickContext{Room: meeting(start, 6, calendar.OutcomeHeld), Now: start, Rand: behave.NewSource("g")})
	if f.Read(pathKnown) != true || spec.Int(f, pathPeople) != 6 || f.Read(pathPresence) != true || spec.Float(f, pathMag) != 2.7 {
		t.Fatalf("at the start: known %v people %v presence %v mag %v", f.Read(pathKnown), f.Read(pathPeople), f.Read(pathPresence), f.Read(pathMag))
	}
	// An operator's change holds until the next edge.
	call(t, f, "POST /api/camera/preset", map[string]any{"preset": "1"})
	apply(f, room, spec.TickContext{Room: meeting(start, 6, calendar.OutcomeHeld), Now: start.Add(time.Minute), Rand: behave.NewSource("g")})
	if f.Read(pathPreset) != int64(1) || f.Read(pathTracking) != manual {
		t.Fatalf("mid-meeting an operator's preset became %v %v", f.Read(pathPreset), f.Read(pathTracking))
	}
	// The meeting ends: nobody in the room, the camera home.
	apply(f, room, spec.TickContext{Room: calendar.RoomState{HasCalendar: true}, Now: start.Add(2 * time.Hour), Rand: behave.NewSource("g")})
	if spec.Int(f, pathPeople) != 0 || f.Read(pathPresence) != false || f.Read(pathPreset) != 0 || f.Read(pathTracking) != tracking || spec.Float(f, pathMag) != 1 || spec.Float(f, pathPan) != 0 {
		t.Fatalf("after the meeting: people %v preset %v %v mag %v pan %v", f.Read(pathPeople), f.Read(pathPreset), f.Read(pathTracking), f.Read(pathMag), f.Read(pathPan))
	}
	// A no-show frames nothing and counts nobody.
	g := spectest.NewFake(Spec(), "kestrel-gaze-p2")
	apply(g, room, spec.TickContext{Room: meeting(start, 0, calendar.OutcomeNoShow), Now: start, Rand: behave.NewSource("g")})
	if spec.Int(g, pathPeople) != 0 || g.Read(pathMeeting) != "none" || spec.Float(g, pathMag) != 1 {
		t.Fatalf("a no-show: people %v meeting %v mag %v", g.Read(pathPeople), g.Read(pathMeeting), g.Read(pathMag))
	}
}

// A camera whose room has no calendar is left alone, and tracking drifts only
// while it tracks and, in a calendar room, only while someone is there.
func TestTrackingWandersInsideTheLensRangeAndOnlyWhileItShould(t *testing.T) {
	f := spectest.NewFake(Spec(), "kestrel-gaze-p2")
	room, drift := tick(t, "room_meeting"), tick(t, "tracking_drift")
	rng := behave.NewSource("wander")
	ctx := spec.TickContext{Room: spectest.IdleRoom(), Now: f.Clock, Rand: rng}
	moved := false
	for i := 0; i < 400; i++ {
		apply(f, room, ctx)
		apply(f, drift, ctx)
		pan, tilt := spec.Float(f, pathPan), spec.Float(f, pathTilt)
		if pan < -60 || pan > 60 || tilt < -20 || tilt > 20 {
			t.Fatalf("tick %d: pan %v tilt %v left the lens range", i, pan, tilt)
		}
		moved = moved || pan != 0
	}
	if !moved || f.Read(pathKnown) != false {
		t.Fatalf("with no calendar it tracks on its own: moved %v known %v", moved, f.Read(pathKnown))
	}
	_ = f.Write(pathTracking, manual)
	before := spec.Float(f, pathPan)
	apply(f, drift, ctx)
	if spec.Float(f, pathPan) != before {
		t.Error("a camera set to manual drifted")
	}
	_ = f.Write(pathTracking, tracking)
	_ = f.Write(pathKnown, true)
	_ = f.Write(pathPresence, false)
	apply(f, drift, ctx)
	if spec.Float(f, pathPan) != before {
		t.Error("an empty calendar room's camera drifted")
	}
}
