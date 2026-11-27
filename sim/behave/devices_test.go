package behave_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/sim/behave"
)

func TestMicActivityHearsTheRoomAndFlipsItsMuteOnlyWhileSomeoneIsIn(t *testing.T) {
	for _, tc := range []struct {
		name       string
		hms        string
		prev       behave.MicState
		rng        behave.Source
		active, mu bool
	}{
		{"booked: active", "08:30:00", behave.MicState{}, &stub{chance: false}, true, false},
		{"booked: the mute flips on its draw", "08:30:00", behave.MicState{}, &stub{chance: true}, true, true},
		{"booked: and back", "08:30:00", behave.MicState{Mute: true}, &stub{chance: true}, true, false},
		{"sharing: still active", "08:45:00", behave.MicState{}, &stub{chance: false}, true, false},
		{"impaired: still hears the room", "08:45:00", behave.MicState{Active: true}, &stub{chance: false}, true, false},
		{"walk-in of one", "09:34:00", behave.MicState{}, &stub{chance: false}, true, false},
		{"waking: nobody yet, no draw", "08:27:00", behave.MicState{Active: true, Mute: true}, never{t}, false, true},
		{"cooldown: nobody, mute held, no draw", "09:03:00", behave.MicState{Active: true, Mute: true}, never{t}, false, true},
		{"no-show: nobody", "13:50:00", behave.MicState{Active: true}, never{t}, false, false},
	} {
		got := behave.MicActivity(tc.prev, roomAt(tc.hms), behave.DefaultMic, tc.rng)
		if got.Active != tc.active || got.Mute != tc.mu {
			t.Errorf("%s: %+v, want active %v mute %v", tc.name, got, tc.active, tc.mu)
		}
	}
	held := behave.MicState{Active: true, Mute: true, LevelDB: -30}
	if got := behave.MicActivity(held, noCalendar, behave.DefaultMic, never{t}); got != held {
		t.Errorf("no calendar moved the mic: %+v", got)
	}
}

func TestMicLevelRidesSpeechWhereItHearsAndSitsAtTheFloorOtherwise(t *testing.T) {
	still := &stub{frac: 0.5}
	for _, tc := range []struct {
		name string
		hms  string
		prev behave.MicState
		want float64
	}{
		{"open in a meeting: stays where it was", "08:30:00", behave.MicState{LevelDB: -35}, -35},
		{"a quiet level rises to the opening level", "08:30:00", behave.MicState{LevelDB: -60}, -40},
		{"muted: the floor", "08:30:00", behave.MicState{Mute: true, LevelDB: -35}, -69},
		{"nobody: the floor", "09:20:00", behave.MicState{LevelDB: -35}, -69},
		{"no-show: the floor", "13:50:00", behave.MicState{LevelDB: -35}, -69},
		{"no calendar: its own walk", "", behave.MicState{LevelDB: -42}, -42},
	} {
		rs := noCalendar
		if tc.hms != "" {
			rs = roomAt(tc.hms)
		}
		if got := behave.MicLevel(tc.prev, rs, behave.DefaultMic, still); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := behave.MicLevel(behave.MicState{LevelDB: -35}, roomAt("08:30:00"), behave.DefaultMic, &stub{frac: 1}); got != -31 {
		t.Errorf("a full step up: %v, want -31", got)
	}
	if got := behave.MicLevel(behave.MicState{LevelDB: -21}, roomAt("08:30:00"), behave.DefaultMic, &stub{frac: 1}); got != -20 {
		t.Errorf("a step past the band: %v, want -20", got)
	}
}

func TestGatesOpenOneForEveryTwoAttendees(t *testing.T) {
	strips := func(muted ...bool) behave.GateState {
		var s behave.GateState
		for _, m := range muted {
			s.Strips = append(s.Strips, behave.Strip{Muted: m, PeakDB: -86})
		}
		return s
	}
	still := &stub{frac: 0.5}
	for _, tc := range []struct {
		name   string
		hms    string
		prev   behave.GateState
		gates  []bool
		active int
		peak0  float64
	}{
		{"3 attend: two strips", "08:30:00", strips(false, false, false, false), []bool{true, true, false, false}, 2, -46},
		{"6 attend: three", "10:20:00", strips(false, false, false, false), []bool{true, true, true, false}, 3, -40},
		{"1 attends: one", "09:34:00", strips(false, false, false, false), []bool{true, false, false, false}, 1, -50},
		{"a muted strip stays shut", "10:20:00", strips(true, false, false, false), []bool{false, true, true, false}, 2, -86},
		{"impaired: the room is still heard", "08:45:00", strips(false, false, false, false), []bool{true, true, false, false}, 2, -46},
		{"nobody: all shut", "09:20:00", strips(false, false, false, false), []bool{false, false, false, false}, 0, -86},
		{"no-show: all shut", "13:50:00", strips(false, false, false, false), []bool{false, false, false, false}, 0, -86},
	} {
		got := behave.Gates(tc.prev, roomAt(tc.hms), behave.DefaultGates, still)
		if got.Active != tc.active || got.Strips[0].PeakDB != tc.peak0 {
			t.Errorf("%s: active %d peak %v, want %d and %v", tc.name, got.Active, got.Strips[0].PeakDB, tc.active, tc.peak0)
		}
		for i, g := range tc.gates {
			if got.Strips[i].Gate != g || got.Strips[i].Muted != tc.prev.Strips[i].Muted {
				t.Errorf("%s: strip %d gate %v muted %v, want gate %v", tc.name, i, got.Strips[i].Gate, got.Strips[i].Muted, g)
			}
		}
	}
	held := strips(false, true)
	held.Active = 1
	if got := behave.Gates(held, noCalendar, behave.DefaultGates, never{t}); got.Active != 1 || len(got.Strips) != 2 || got.Strips[1] != held.Strips[1] {
		t.Errorf("no calendar moved the strips: %+v", got)
	}
}

func TestAmpSignalAndTemperatureFollowTheRoomItDrives(t *testing.T) {
	still := &stub{frac: 0.5}
	for _, tc := range []struct {
		name    string
		hms     string
		on      bool
		present bool
	}{
		{"booked, on", "08:30:00", true, true},
		{"booked, off", "08:30:00", false, false},
		{"impaired, on", "08:45:00", true, true},
		{"cooldown: nobody", "09:03:00", true, false},
		{"no-show", "13:50:00", true, false},
	} {
		if got := behave.AmpSignalPresent(!tc.present, roomAt(tc.hms), tc.on); got != tc.present {
			t.Errorf("%s: signal %v, want %v", tc.name, got, tc.present)
		}
	}
	if !behave.AmpSignalPresent(true, noCalendar, false) || behave.AmpSignalPresent(false, noCalendar, true) {
		t.Error("no calendar moved the signal")
	}
	// toward 46 while driving, 36 at rest, a tenth-ish of the gap a tick
	if got := behave.AmpTemperature(40, roomAt("08:30:00"), true, behave.DefaultAmp, still); got != 40.9 {
		t.Errorf("warming: %v, want 40.9", got)
	}
	if got := behave.AmpTemperature(40, roomAt("08:30:00"), false, behave.DefaultAmp, still); got != 39.4 {
		t.Errorf("on standby in a meeting: %v, want 39.4", got)
	}
	if got := behave.AmpTemperature(40, roomAt("09:20:00"), true, behave.DefaultAmp, still); got != 39.4 {
		t.Errorf("idle: %v, want 39.4", got)
	}
	if got := behave.AmpTemperature(60, roomAt("08:30:00"), true, behave.DefaultAmp, &stub{frac: 1}); got != 52 {
		t.Errorf("clamped: %v, want 52", got)
	}
	if got := behave.AmpTemperature(40, noCalendar, true, behave.DefaultAmp, still); got != 40 {
		t.Errorf("no calendar: %v, want its still walk", got)
	}
}

func TestAmpOutputLevelRidesAudioWhileDrivenAndSitsAtTheFloorOtherwise(t *testing.T) {
	still := &stub{frac: 0.5}
	for _, tc := range []struct {
		name string
		hms  string
		on   bool
		prev float64
		want float64
	}{
		{"driving: stays", "08:30:00", true, -20, -20},
		{"driving from the floor: wakes at its level", "08:30:00", true, -90, -24},
		{"off: the floor", "08:30:00", false, -20, -90},
		{"nobody: the floor", "09:20:00", true, -20, -90},
		{"no-show: the floor", "13:50:00", true, -20, -90},
		{"no calendar: its own walk", "", true, -30, -30},
	} {
		rs := noCalendar
		if tc.hms != "" {
			rs = roomAt(tc.hms)
		}
		if got := behave.AmpOutputLevel(tc.prev, rs, tc.on, -24, behave.DefaultAmp, still); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if got := behave.AmpOutputLevel(-90, roomAt("08:30:00"), true, -27, behave.DefaultAmp, still); got != -27 {
		t.Errorf("another channel's wake level: %v", got)
	}
}

func TestOutletCurrentIsTheLevelOfTheGearForTheRoom(t *testing.T) {
	video := behave.OutletParams{Idle: 150, InUse: 700, InCall: 950, Walk: behave.DefaultOutletWalk}
	control := behave.OutletParams{Idle: 180, InUse: 300, Walk: behave.DefaultOutletWalk}
	spare := behave.OutletParams{CalendarOff: true, Walk: behave.DefaultOutletWalk}
	for _, tc := range []struct {
		name string
		hms  string
		on   bool
		p    behave.OutletParams
		want int
	}{
		{"video, idle", "09:20:00", true, video, 150},
		{"video, waking: in use", "08:27:00", true, video, 700},
		{"video, in a call with people", "08:30:00", true, video, 950},
		{"video, cooling down: in use, no call", "09:03:00", true, video, 700},
		{"video, no-show: idle", "13:50:00", true, video, 150},
		{"video, impaired: the meeting beneath", "08:45:00", true, video, 950},
		{"video, switched off", "08:30:00", false, video, 0},
		{"control, in a call: no call level", "08:30:00", true, control, 300},
		{"control, idle", "09:20:00", true, control, 180},
		{"spare, on: nothing plugged in", "08:30:00", true, spare, 0},
	} {
		if got := behave.OutletCurrent(0, tc.on, roomAt(tc.hms), tc.p, never{t}); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
	if got := behave.OutletCurrent(500, true, noCalendar, video, &stub{frac: 0.5}); got != 500 {
		t.Errorf("no calendar, a still walk: %d", got)
	}
	if got := behave.OutletCurrent(1190, true, noCalendar, spare, &stub{frac: 1}); got != 1200 {
		t.Errorf("no calendar, the walk caps: %d", got)
	}
	if got := behave.OutletCurrent(500, false, noCalendar, video, never{t}); got != 0 {
		t.Errorf("no calendar, off: %d", got)
	}
}
