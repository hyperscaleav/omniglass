package sim

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
)

// A room's audio chain shows its meeting (#2774): the ceiling and wireless
// microphones, the DSP and the amplifier each read the room's entry off the
// lab calendar through the device clock, so a reading is a function of the
// calendar and the time, never of a draw the device kept. The tests below
// run a simulated day, and a week, on a pinned clock and hold every sampled
// instant to the calendar.

var audioRoom = calendar.Room{Site: "hq", Building: "tower", Room: "r101"}

// audioKnobs fills the working day with meetings of every outcome the
// readers must tell apart, with a night either side of it.
func audioKnobs() calendar.Knobs {
	return calendar.Knobs{
		BookingRate: 0.7, SlotMix: map[int]float64{30: 1, 60: 1}, NoShowRate: 0.25,
		EndEarlyRate: 0.2, EndLateRate: 0.1, Attendance: calendar.Range{Min: 2, Max: 12}, WalkInRate: 0.3,
		Hours: calendar.Hours{Start: 8, End: 18},
	}
}

// audioRig is an engine of one room's calendar holding one device of the
// profile, its clock the rig's own.
type audioRig struct {
	cal *calendar.Calendar
	dev *Device
	at  time.Time
}

func newAudioRig(t *testing.T, profileName string, seed int64, k calendar.Knobs) *audioRig {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	cal := calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true })
	e.SetCalendar(cal)
	id := profileName + "-1"
	snap := DeviceSnapshot{DeviceID: id, Hostname: id, LoopbackIP: "127.0.0.1", ProfileName: profileName, Version: 1,
		SiteCode: audioRoom.Site, Building: audioRoom.Building, Room: audioRoom.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	r := &audioRig{cal: cal, dev: e.Devices()[0]}
	r.dev.SetClock(func() time.Time { return r.at })
	return r
}

// tick pins the clock to at and fires each named behavior once.
func (r *audioRig) tick(t *testing.T, at time.Time, names ...string) {
	t.Helper()
	r.at = at
	for _, n := range names {
		if !r.dev.TickBehavior(n) {
			t.Fatalf("%s has no behavior %q; have %v", r.dev.ProfileName, n, r.dev.Behaviors())
		}
	}
}

func (r *audioRig) f(t *testing.T, path string) float64 {
	t.Helper()
	f, _, err := toFloat(r.dev.ReadPath(path))
	if err != nil {
		t.Fatalf("%s: %v (%v)", path, err, r.dev.ReadPath(path))
	}
	return f
}

func (r *audioRig) is(path string) bool { return fmt.Sprint(r.dev.ReadPath(path)) == "true" }

// occupied is the calendar's own answer: someone is in the room at at.
func occupied(c *calendar.Calendar, at time.Time) (calendar.Entry, bool) {
	e, ok := c.Now(audioRoom, at)
	return e, ok && e.Meeting.Actual > 0
}

// audioDay finds a seed whose day holds what the tests need to tell apart: a
// no-show, a walk-in and at least three held meetings of differing sizes.
func audioDay(t *testing.T, k calendar.Knobs) (int64, time.Time) {
	t.Helper()
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for seed := int64(1); seed < 400; seed++ {
		var noShow, walkIn int
		sizes := map[int]bool{}
		for _, e := range calendar.Generate(audioRoom, k, seed, day, day.Add(24*time.Hour)) {
			switch {
			case e.Meeting.Outcome == calendar.OutcomeNoShow:
				noShow++
			case e.Meeting.Origin == calendar.OriginWalkIn:
				walkIn++
			default:
				sizes[e.Meeting.Actual] = true
			}
		}
		if noShow > 0 && walkIn > 0 && len(sizes) >= 3 {
			return seed, day
		}
	}
	t.Fatal("no seed holds a no-show, a walk-in and three sizes of meeting in a day")
	return 0, day
}

// The ceiling array is active in a held meeting with its level up and mute
// moving inside the call, and idle (its mute left alone, at its floor) otherwise, at
// every minute of a day with a no-show and a walk-in in it. Mute is left as
// it is between meetings (an operator's mute holds), and moves only inside one.
func TestTheCeilingMicFollowsTheLabCalendarThroughADay(t *testing.T) {
	k := audioKnobs()
	seed, day := audioDay(t, k)
	r := newAudioRig(t, "lyra-arc-a2", seed, k)
	var inUse, toggles int
	var wasMuted bool
	var sawNoShow, sawWalkIn bool
	for m := 0; m < 24*60; m++ {
		at := day.Add(time.Duration(m) * time.Minute)
		r.tick(t, at, "call_state", "level_drift")
		e, use := occupied(r.cal, at)
		if got := r.is("status.mic.active"); got != use {
			t.Fatalf("%s: active = %v, the calendar says in use = %v (%+v)", at.Format("15:04"), got, use, e.Meeting)
		}
		level := r.f(t, "status.mic.level_db")
		muted := r.is("status.mic.mute")
		switch {
		case use:
			inUse++
			if m > 0 && muted != wasMuted {
				toggles++
			}
			if !muted && level < -55 {
				t.Errorf("%s: an open mic in a meeting reads %v dBFS, at its floor", at.Format("15:04"), level)
			}
			if muted && level > -66 {
				t.Errorf("%s: a muted mic reads %v dBFS, above the floor", at.Format("15:04"), level)
			}
			sawWalkIn = sawWalkIn || e.Meeting.Origin == calendar.OriginWalkIn
		default:
			if level > -66 || (m > 0 && muted != wasMuted) {
				t.Fatalf("%s: idle but mute moved to %v or level=%v", at.Format("15:04"), muted, level)
			}
			if ns, ok := r.cal.Now(audioRoom, at); ok && ns.Meeting.Outcome == calendar.OutcomeNoShow {
				sawNoShow = true
			}
		}
		wasMuted = muted
	}
	if inUse < 60 {
		t.Fatalf("the day holds %d minutes of use, too few to test", inUse)
	}
	if toggles == 0 {
		t.Errorf("mute never moved inside a meeting in %d minutes of use", inUse)
	}
	if !sawNoShow || !sawWalkIn {
		t.Errorf("the day did not exercise a no-show (%v) and a walk-in (%v)", sawNoShow, sawWalkIn)
	}
}

// The HX-1's pack transmits only while the room is in use, and its battery is
// the same function of the calendar at an instant on every run.
func TestTheWirelessMicTransmitsInMeetingsAndItsBatteryFollowsTheCalendar(t *testing.T) {
	k := audioKnobs()
	seed, day := audioDay(t, k)
	r := newAudioRig(t, "lyra-hx-1", seed, k)
	for m := 0; m < 24*60; m++ {
		at := day.Add(time.Duration(m) * time.Minute)
		r.tick(t, at, "call_state", "level_drift", "battery_charge")
		if _, use := occupied(r.cal, at); r.is("status.mic.transmitting") != use {
			t.Fatalf("%s: transmitting = %v, in use = %v", at.Format("15:04"), !use, use)
		}
		if m%10 != 0 {
			continue
		}
		want := batteryOracle(r.cal, at)
		if got := r.f(t, "status.mic.battery_percent"); math.Abs(got-want) > 0.06 {
			t.Fatalf("%s: battery %v, the calendar's charge is %v", at.Format("15:04"), got, want)
		}
		// A second device, fresh, at the same instant reads the same charge:
		// nothing accumulated in the first.
		if m%120 == 0 {
			r2 := newAudioRig(t, "lyra-hx-1", seed, k)
			r2.tick(t, at, "battery_charge")
			if a, b := r.f(t, "status.mic.battery_percent"), r2.f(t, "status.mic.battery_percent"); a != b {
				t.Fatalf("%s: a fresh pack reads %v, the running one %v", at.Format("15:04"), b, a)
			}
		}
	}
}

// batteryOracle is the pack's charge by walking the calendar a second at a
// time over the 48 hours before at: full at the start, draining in use,
// recharging on the dock, the way the profile states it in rates.
func batteryOracle(c *calendar.Calendar, at time.Time) float64 {
	const drain, charge = 0.4 / 60, 0.8 / 60 // percent per second
	from := at.Add(-48 * time.Hour)
	entries := c.Entries(audioRoom, from, at)
	level := 100.0
	i := 0
	for s := from; s.Before(at); s = s.Add(time.Second) {
		for i < len(entries) && !entries[i].End.After(s) {
			i++
		}
		use := false
		for j := i; j < len(entries) && !entries[j].Start.After(s); j++ {
			use = use || (entries[j].Meeting.Actual > 0 && entries[j].End.After(s))
		}
		if use {
			level -= drain
		} else {
			level += charge
		}
		level = math.Max(0, math.Min(100, level))
	}
	return math.Round(level*10) / 10
}

// Over a simulated week the charge falls minute on minute while a meeting is
// held, never falls through a no-show or an empty room, and is back to full
// in the nights between.
func TestTheWirelessBatteryFallsInUseAndRecoversBetweenMeetingsOverAWeek(t *testing.T) {
	k := audioKnobs()
	seed, day := audioDay(t, k)
	r := newAudioRig(t, "lyra-hx-1", seed, k)
	var prev float64
	var prevUse, fell, rose, full bool
	lowest := 100.0
	for m := 0; m < 7*24*60; m++ {
		at := day.Add(time.Duration(m) * time.Minute)
		r.tick(t, at, "battery_charge")
		b := r.f(t, "status.mic.battery_percent")
		_, use := occupied(r.cal, at)
		if m > 0 {
			switch {
			case use && prevUse && b >= prev && prev > 0:
				t.Fatalf("%s: charge %v after %v through a meeting", at.Format("Mon 15:04"), b, prev)
			case !use && !prevUse && b < prev-0.0001:
				t.Fatalf("%s: charge fell %v to %v with the room empty (a no-show drains nothing)", at.Format("Mon 15:04"), prev, b)
			}
			fell = fell || b < prev-0.05
			rose = rose || b > prev+0.05
		}
		lowest = math.Min(lowest, b)
		full = full || (b == 100 && m > 24*60)
		prev, prevUse = b, use
	}
	if !fell || !rose || lowest >= 100 || !full {
		t.Fatalf("week: fell=%v rose=%v lowest=%v full again=%v", fell, rose, lowest, full)
	}
}

// A room whose every booking is a no-show never lights its audio: the packs
// stay charged, the array idle, the DSP at its floor, the amplifier silent.
func TestANoShowRoomsAudioStaysIdle(t *testing.T) {
	k := audioKnobs()
	k.NoShowRate, k.WalkInRate = 1, 0
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	hx := newAudioRig(t, "lyra-hx-1", 5, k)
	arc := newAudioRig(t, "lyra-arc-a2", 5, k)
	dsp := newAudioRig(t, "polaris-dsp-16", 5, k)
	amp := newAudioRig(t, "polaris-amp-4100", 5, k)
	if len(calendar.Generate(audioRoom, k, 5, day, day.Add(24*time.Hour))) == 0 {
		t.Fatal("the day holds no no-show to test")
	}
	for m := 0; m < 24*60; m += 5 {
		at := day.Add(time.Duration(m) * time.Minute)
		hx.tick(t, at, "call_state", "battery_charge")
		arc.tick(t, at, "call_state", "level_drift")
		dsp.tick(t, at, "room_signal")
		amp.tick(t, at, "output_drift", "temperature_drift")
		if hx.is("status.mic.transmitting") || hx.f(t, "status.mic.battery_percent") != 100 {
			t.Fatalf("%s: a pack in a no-show room transmits or drains", at.Format("15:04"))
		}
		if arc.is("status.mic.active") {
			t.Fatalf("%s: the array is active in a no-show room", at.Format("15:04"))
		}
		if n := dsp.f(t, "status.dsp.active_channels"); n != 0 {
			t.Fatalf("%s: the DSP opens %v gates in a no-show room", at.Format("15:04"), n)
		}
		if amp.is("status.amp.signal_present") {
			t.Fatalf("%s: the amplifier carries signal in a no-show room", at.Format("15:04"))
		}
	}
}

// The DSP opens a gate per two attendees (to its four table and ceiling
// strips) and raises their level with the count, a meeting of eleven louder
// than a meeting of two; with the room empty every gate is shut and every
// strip at the noise floor.
func TestTheDSPsGatesAndLevelsScaleWithActualAttendees(t *testing.T) {
	k := audioKnobs()
	seed, day := audioDay(t, k)
	r := newAudioRig(t, "polaris-dsp-16", seed, k)
	byActual := map[int][]float64{}
	for m := 0; m < 24*60; m++ {
		at := day.Add(time.Duration(m) * time.Minute)
		r.tick(t, at, "room_signal")
		e, use := occupied(r.cal, at)
		open := 0
		var sum float64
		for i := 0; i < 4; i++ {
			gate := r.is(fmt.Sprintf("status.channels.%d.gate", i))
			peak := r.f(t, fmt.Sprintf("status.channels.%d.peak_db", i))
			if gate {
				open++
				sum += peak
			}
			if !use && (gate || peak > -78) {
				t.Fatalf("%s: idle room, channel %d gate=%v peak=%v", at.Format("15:04"), i, gate, peak)
			}
			if use && !gate && peak > -78 {
				t.Fatalf("%s: a shut gate carries %v dBFS", at.Format("15:04"), peak)
			}
		}
		active := int(r.f(t, "status.dsp.active_channels"))
		if active != open {
			t.Fatalf("%s: active_channels %d, gates open %d", at.Format("15:04"), active, open)
		}
		if !use {
			continue
		}
		want := min(4, (e.Meeting.Actual+1)/2)
		if open != want {
			t.Fatalf("%s: %d attendees open %d gates, want %d", at.Format("15:04"), e.Meeting.Actual, open, want)
		}
		byActual[e.Meeting.Actual] = append(byActual[e.Meeting.Actual], sum/float64(open))
	}
	mean := func(xs []float64) float64 {
		var s float64
		for _, x := range xs {
			s += x
		}
		return s / float64(len(xs))
	}
	lo, hi := 1<<30, 0
	for a := range byActual {
		lo, hi = min(lo, a), max(hi, a)
	}
	if hi-lo < 2 {
		t.Fatalf("the day's meetings span %d to %d attendees, too narrow to compare", lo, hi)
	}
	if a, b := mean(byActual[lo]), mean(byActual[hi]); b <= a+1 {
		t.Errorf("a meeting of %d reads %.1f dBFS, one of %d reads %.1f: no louder", hi, b, lo, a)
	}
}

// The amplifier carries signal on every channel while the room is in use and
// sits at its floor with signal absent otherwise; its heatsink follows the
// load, warmer in use than idle, within its band.
func TestTheAmplifierCarriesSignalAndWarmsWithTheMeetings(t *testing.T) {
	k := audioKnobs()
	seed, day := audioDay(t, k)
	r := newAudioRig(t, "polaris-amp-4100", seed, k)
	var inUse, idle []float64
	for m := 0; m < 24*60; m++ {
		at := day.Add(time.Duration(m) * time.Minute)
		r.tick(t, at, "output_drift", "temperature_drift")
		_, use := occupied(r.cal, at)
		if r.is("status.amp.signal_present") != use {
			t.Fatalf("%s: signal_present = %v, in use = %v", at.Format("15:04"), !use, use)
		}
		for i := 0; i < 4; i++ {
			o := r.f(t, fmt.Sprintf("status.channels.%d.output_db", i))
			switch {
			case use && (o < -40 || o > -6):
				t.Fatalf("%s: channel %d carries %v dBFS in a meeting", at.Format("15:04"), i, o)
			case !use && o > -85:
				t.Fatalf("%s: channel %d carries %v dBFS in an empty room", at.Format("15:04"), i, o)
			}
		}
		temp := r.f(t, "status.amp.temperature")
		if temp < 34 || temp > 52 {
			t.Fatalf("%s: temperature %v outside its band", at.Format("15:04"), temp)
		}
		if m >= 12*60 { // after the morning's warm-up from the default
			if use {
				inUse = append(inUse, temp)
			} else {
				idle = append(idle, temp)
			}
		}
	}
	avg := func(xs []float64) float64 {
		var s float64
		for _, x := range xs {
			s += x
		}
		return s / float64(len(xs))
	}
	if len(inUse) < 20 || len(idle) < 20 {
		t.Fatalf("the afternoon holds %d minutes in use and %d idle", len(inUse), len(idle))
	}
	if a, b := avg(inUse), avg(idle); a < b+2 {
		t.Errorf("the amplifier averages %.1f C in use and %.1f C idle: it does not follow the load", a, b)
	}
}

// A standby amplifier carries nothing even while the room is in use.
func TestAStandbyAmplifierCarriesNoSignalInAMeeting(t *testing.T) {
	k := audioKnobs()
	seed, day := audioDay(t, k)
	r := newAudioRig(t, "polaris-amp-4100", seed, k)
	var at time.Time
	for m := 0; m < 24*60; m++ {
		at = day.Add(time.Duration(m) * time.Minute)
		if _, use := occupied(r.cal, at); use {
			break
		}
	}
	if err := r.dev.AddBehaviorFromProfile(&profile.Behavior{Name: "standby", Schedule: profile.Schedule{Interval: "60s"},
		Mutations: []profile.Mutation{{Path: "status.amp.power", Value: "standby"}}}); err != nil {
		t.Fatal(err)
	}
	r.tick(t, at, "standby")
	for range 3 {
		r.tick(t, at, "output_drift")
	}
	if r.is("status.amp.signal_present") {
		t.Fatal("a standby amplifier reports signal present in a meeting")
	}
}

// meetingsBetween hands a template the room's meetings over a window, the
// same ones the calendar derives, and none without a calendar.
func TestMeetingsBetweenReadsTheRoomsWindowOffTheCalendar(t *testing.T) {
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	at := day.Add(12 * time.Hour)
	e, d := calEngine(t, 11, audioRoom, &at)
	want := len(e.Calendar().Entries(audioRoom, day, day.Add(24*time.Hour)))
	if want == 0 {
		t.Fatal("the day holds no meeting")
	}
	expr := fmt.Sprintf("{{ len (meetingsBetween %d %d) }}", day.Unix(), day.Add(24*time.Hour).Unix())
	if got := stampBehavior(t, d, expr); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("meetingsBetween holds %v meetings, the calendar %d", got, want)
	}
	e.SetCalendar(nil)
	if got := stampBehavior(t, d, expr); fmt.Sprint(got) != "0" {
		t.Fatalf("with no calendar meetingsBetween holds %v", got)
	}
}
