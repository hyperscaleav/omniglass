package sim

import (
	"fmt"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
)

// Room control and switching follow the lab calendar (#2776): the control
// processor starts the room for a meeting and shuts it down after, the touch
// panel wakes and changes page, the switcher routes the source the meeting
// uses and the extender carries it. Every reading is derived from the
// calendar through the device's clock; none consults Omniglass.

const (
	routeConferencing = "hdmi2" // the codec's feed, selected for a call
	routeLaptop       = "hdmi1" // the lectern laptop, selected while content is shared
	routeIdle         = "dp1"   // the input nothing is plugged into, between meetings
)

// roomEngine is an engine over the shipped catalog holding one device of
// each named profile in roomHQ, all on one pinned clock and one calendar.
func roomEngine(t *testing.T, seed int64, k calendar.Knobs, at *time.Time, profileNames ...string) map[string]*Device {
	t.Helper()
	e := shippedEngine(t)
	e.SetCalendar(calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true }))
	var snaps []DeviceSnapshot
	for i, name := range profileNames {
		id := fmt.Sprintf("d-%d", i)
		snaps = append(snaps, DeviceSnapshot{DeviceID: id, Hostname: id, LoopbackIP: "127.0.0.1", ProfileName: name, Version: 1,
			SiteCode: roomHQ.Site, Building: roomHQ.Building, Room: roomHQ.Room})
	}
	if err := e.LoadDevices(snaps); err != nil {
		t.Fatal(err)
	}
	out := map[string]*Device{}
	for _, d := range e.Devices() {
		d.SetClock(pinned(at))
		out[d.ProfileName] = d
	}
	return out
}

// pinnedDay is a day whose room holds a booked meeting, a walk-in and a
// no-show, and a meeting long enough to share content.
func pinnedDay(t *testing.T) (calendar.Knobs, int64, time.Time, *calendar.Calendar) {
	t.Helper()
	k := dayKnobs()
	const seed = 21
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	cal := calOf(seed, k)
	var booked, walkIn, noShow, long int
	for _, e := range cal.Entries(roomHQ, day, day.Add(24*time.Hour), calendar.MeetingKinds) {
		switch {
		case caltest.M(e).Outcome == calendar.OutcomeNoShow:
			noShow++
		case caltest.M(e).Origin == calendar.OriginWalkIn:
			walkIn++
		default:
			booked++
			if e.End.Sub(e.Start) >= 30*time.Minute {
				long++
			}
		}
	}
	if booked == 0 || walkIn == 0 || noShow == 0 || long == 0 {
		t.Fatalf("the pinned day holds %d booked, %d walk-in, %d no-show, %d long; pick a seed with all", booked, walkIn, noShow, long)
	}
	return k, seed, day, cal
}

// heldAt is the meeting a room is in at t that somebody attends, or none.
func heldAt(cal *calendar.Calendar, t time.Time) (calendar.Entry, bool) { return held(cal, roomHQ, t) }

// wantShare is the calendar's own answer, computed with no device: a held
// meeting of half an hour or more shares through its middle third.
func wantShare(e calendar.Entry, t time.Time) bool {
	dur := e.End.Sub(e.Start)
	into := t.Sub(e.Start) * 3
	return dur >= 30*time.Minute && into >= dur && into < 2*dur
}

// Over a simulated day the processor's room power agrees with the calendar at
// every sampled minute, and its last macro is the run that moved it: the start
// macro at the minute the room woke, the end macro at the minute it slept.
func TestControlProcessorStartsAndEndsTheRoomOverADay(t *testing.T) {
	k, seed, day, cal := pinnedDay(t)
	at := day
	d := roomEngine(t, seed, k, &at, "newtron-core-3")["newtron-core-3"]
	if got := d.ReadPath("status.control.macro"); got != "none" {
		t.Fatalf("a processor that has run nothing reads macro %v", got)
	}
	var starts, ends int
	prev := "standby"
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_meeting")
		want := "standby"
		if wantAwake(cal, roomHQ, at) {
			want = "on"
		}
		got := fmt.Sprint(d.ReadPath("status.control.room_power"))
		if got != want {
			t.Fatalf("%s: room power %q, the calendar says %q", at.Format("15:04"), got, want)
		}
		macro, ran := fmt.Sprint(d.ReadPath("status.control.macro")), d.ReadPath("status.control.macro_at")
		if got != prev {
			wantMacro := "end-meeting"
			if got == "on" {
				wantMacro = "start-meeting"
				starts++
			} else {
				ends++
			}
			if macro != wantMacro || ran != at.Unix() {
				t.Fatalf("%s: the room went %s, yet the last macro is %q at %v, want %q at %d", at.Format("15:04"), got, macro, ran, wantMacro, at.Unix())
			}
		}
		prev = got
	}
	if starts == 0 || starts != ends {
		t.Fatalf("a day ran %d start macros and %d end macros", starts, ends)
	}
	if got := d.ReadPath("status.control.macro"); got != "end-meeting" {
		t.Fatalf("the day ends with the last macro %v, want end-meeting", got)
	}
}

// The touch panel is awake in the room's use window, shows the call page in a
// held meeting and the sharing page in its sharing stretch, and idles in a
// no-show; a walk-in and a booked meeting are both covered by the pinned day.
func TestTouchPanelWakesAndChangesPageOverADay(t *testing.T) {
	k, seed, day, cal := pinnedDay(t)
	at := day
	d := roomEngine(t, seed, k, &at, "newtron-panel-7")["newtron-panel-7"]
	var calls, shares, noShowIdle int
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_meeting")
		if got, want := d.ReadPath("status.panel.awake"), wantAwake(cal, roomHQ, at); got != want {
			t.Fatalf("%s: awake %v, the calendar says %v", at.Format("15:04"), got, want)
		}
		want := "idle"
		if e, ok := heldAt(cal, at); ok {
			want = "in-call"
			calls++
			if wantShare(e, at) {
				want = "sharing"
				shares++
			}
		} else if now, ok := cal.Now(roomHQ, at, calendar.MeetingKinds); ok && caltest.M(now).Outcome == calendar.OutcomeNoShow {
			noShowIdle++
		}
		if got := fmt.Sprint(d.ReadPath("status.panel.page")); got != want {
			t.Fatalf("%s: page %q, the calendar says %q", at.Format("15:04"), got, want)
		}
	}
	if calls == 0 || shares == 0 || noShowIdle == 0 {
		t.Fatalf("the day sampled %d call, %d sharing and %d no-show minutes; want all", calls, shares, noShowIdle)
	}
}

// The switcher routes what the meeting uses: the conferencing input for a
// call, the laptop while content is shared, the empty input between meetings
// and through a no-show. It is on a real source exactly when the video bar in
// the same room is in a call, minute by minute.
func TestSwitcherRoutesTheMeetingAndAgreesWithTheBar(t *testing.T) {
	k, seed, day, cal := pinnedDay(t)
	at := day
	room := roomEngine(t, seed, k, &at, "meridian-mx-84", "kestrel-bar")
	sw, bar := room["meridian-mx-84"], room["kestrel-bar"]
	var conf, laptop, idle, noShow, sharedMinutes int
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		sw.TickBehavior("room_meeting")
		sw.TickBehavior("signal_follow")
		bar.TickBehavior("call_cycle")
		want := routeIdle
		if e, ok := heldAt(cal, at); ok {
			want = routeConferencing
			if wantShare(e, at) {
				want = routeLaptop
				laptop++
			} else {
				conf++
			}
		} else if now, ok := cal.Now(roomHQ, at, calendar.MeetingKinds); ok && caltest.M(now).Outcome == calendar.OutcomeNoShow {
			noShow++
		} else {
			idle++
		}
		got := fmt.Sprint(sw.ReadPath("status.switcher.input"))
		if got != want {
			t.Fatalf("%s: routed %q, the calendar says %q", at.Format("15:04"), got, want)
		}
		if barCall := bar.ReadPath("status.call.active") == true; barCall != (got != routeIdle) {
			t.Fatalf("%s: the bar's call is %v and the switcher routes %q", at.Format("15:04"), barCall, got)
		}
		wantSignal := "present"
		if got == routeIdle {
			wantSignal = "absent"
		}
		if sig := fmt.Sprint(sw.ReadPath("status.switcher.signal")); sig != wantSignal {
			t.Fatalf("%s: signal %q on %q, want %q", at.Format("15:04"), sig, got, wantSignal)
		}
		// The bar shares content exactly while the switcher routes the laptop.
		if shared := bar.ReadPath("status.presentation.local_active") == true; shared != (got == routeLaptop) {
			t.Fatalf("%s: the bar shares content %v and the switcher routes %q", at.Format("15:04"), shared, got)
		}
		if got == routeLaptop {
			sharedMinutes++
		}
	}
	if sharedMinutes == 0 || sharedMinutes != laptop {
		t.Fatalf("the bar and switcher agreed on %d sharing minutes of %d", sharedMinutes, laptop)
	}
	if conf == 0 || laptop == 0 || idle == 0 || noShow == 0 {
		t.Fatalf("the day sampled %d conferencing, %d laptop, %d idle, %d no-show minutes; want all", conf, laptop, idle, noShow)
	}
}

// An operator's route inside a meeting holds until the meeting asks for a
// different one: the calendar is an edge, not a level held every tick.
func TestSwitcherHoldsAnOperatorsRouteUntilTheMeetingMoves(t *testing.T) {
	k, seed, day, cal := pinnedDay(t)
	at := day
	d := roomEngine(t, seed, k, &at, "meridian-mx-84")["meridian-mx-84"]
	var set bool
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_meeting")
		e, ok := heldAt(cal, at)
		if !set && ok && !wantShare(e, at) && at.After(e.Start.Add(2*time.Minute)) {
			setPath(t, d, "status.switcher.input", "hdmi3")
			set = true
			continue
		}
		if set {
			if ok && !wantShare(e, at) && at.Before(e.Start.Add(e.End.Sub(e.Start)/3)) {
				if got := d.ReadPath("status.switcher.input"); got != "hdmi3" {
					t.Fatalf("%s: the operator's hdmi3 became %v with the meeting unchanged", at.Format("15:04"), got)
				}
				continue
			}
			if got, want := fmt.Sprint(d.ReadPath("status.switcher.input")), map[bool]string{true: routeLaptop, false: routeIdle}[ok]; got != want {
				t.Fatalf("%s: after the meeting moved the route is %q, want %q", at.Format("15:04"), got, want)
			}
			return
		}
	}
	t.Fatal("the day held no meeting to set a route in")
}

// The extender carries a signal exactly while the switcher routes a source,
// its link stays up through the day, and a pulled cable (link down, set by a
// scenario) is not undone by the calendar.
func TestExtenderCarriesASignalWhileTheSourceIsInUse(t *testing.T) {
	k, seed, day, cal := pinnedDay(t)
	at := day
	d := roomEngine(t, seed, k, &at, "meridian-xl-1")["meridian-xl-1"]
	var present, absent int
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_meeting")
		want := "absent"
		if _, ok := heldAt(cal, at); ok {
			want = "present"
			present++
		} else {
			absent++
		}
		if got := fmt.Sprint(d.ReadPath("status.extender.signal")); got != want {
			t.Fatalf("%s: signal %q, the calendar says %q", at.Format("15:04"), got, want)
		}
		if got := d.ReadPath("status.extender.link"); got != "up" {
			t.Fatalf("%s: link %v, want up", at.Format("15:04"), got)
		}
	}
	if present == 0 || absent == 0 {
		t.Fatalf("%d present and %d absent minutes; want both", present, absent)
	}
	setPath(t, d, "status.extender.link", "down")
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Hour) {
		d.TickBehavior("room_meeting")
	}
	if got := d.ReadPath("status.extender.link"); got != "down" {
		t.Fatalf("the calendar retrained a pulled cable: link %v", got)
	}
}

// A device whose room holds no calendar keeps the state it had before it read
// one: nothing moves on a tick.
func TestRoomControlWithNoCalendarKeepsItsState(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	k := dayKnobs()
	names := []string{"newtron-core-3", "newtron-panel-7", "meridian-mx-84", "meridian-xl-1"}
	room := roomEngine(t, 1, k, &at, names...)
	for _, name := range names {
		d := room[name]
		d.calSrc = func() *calendar.Calendar { return nil }
		before := d.State()
		d.TickBehavior("room_meeting")
		after := d.State()
		for _, path := range []string{"status.control.room_power", "status.control.macro", "status.panel.page", "status.panel.awake",
			"status.switcher.input", "status.switcher.signal", "status.extender.signal", "status.extender.link"} {
			if b, a := readIn(before, path), readIn(after, path); b != a {
				t.Errorf("%s: %s moved from %v to %v with no calendar", name, path, b, a)
			}
		}
	}
}

func readIn(m map[string]any, path string) any {
	var cur any = m
	for _, p := range splitPath(path) {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

// An operator's change to what a room shares holds until the calendar's
// sharing next changes, then the calendar resumes: on the bar (its local
// presentation) and on the codec (its content route), inside a meeting.
func TestAnOperatorsContentSharingHoldsUntilTheCalendarMoves(t *testing.T) {
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	room, _ := busyDay(t, 7, day)
	bar, cal := loadVRoomIn(t, "hq-divisible-conference-1-videobar-1", room, 7)
	relay := loadRelayIn(t, "hq-divisible-conference-1-codec-1", room, 7)
	at := day
	bar.SetClock(func() time.Time { return at })
	relay.SetClock(func() time.Time { return at })
	var forced bool
	var until time.Time
	for i := range 24 * 60 {
		at = day.Add(time.Duration(i) * time.Minute)
		tickByName(t, bar, "call_cycle")
		tickByName(t, relay, "call_cycle")
		e, ok := held(cal, room, at)
		sharing := ok && wantShare(e, at)
		if !forced {
			// Early in a long meeting, before its sharing stretch: stop-sharing is the
			// calendar's answer, so the operator starts it.
			if ok && !sharing && e.End.Sub(e.Start) >= 30*time.Minute && at.Sub(e.Start) >= 2*time.Minute && at.Sub(e.Start) < e.End.Sub(e.Start)/3-2*time.Minute {
				setPath(t, bar, "status.presentation.local_active", true)
				setPath(t, relay, "status.content.sharing", true)
				forced, until = true, e.Start.Add(e.End.Sub(e.Start)/3)
			}
			continue
		}
		if at.Before(until) {
			if bar.ReadPath("status.presentation.local_active") != true || relay.ReadPath("status.content.sharing") != true {
				t.Fatalf("%s: the operator's share dropped before the calendar's sharing moved", at.Format("15:04"))
			}
			continue
		}
		// The calendar's sharing started (or the meeting moved): it resumes.
		if got := bar.ReadPath("status.presentation.local_active") == true; got != sharing {
			t.Fatalf("%s: the bar shares %v, the calendar says %v", at.Format("15:04"), got, sharing)
		}
		if got := relay.ReadPath("status.content.sharing") == true; got != sharing {
			t.Fatalf("%s: the codec shares %v, the calendar says %v", at.Format("15:04"), got, sharing)
		}
		return
	}
	t.Fatal("the day held no long meeting to override")
}
