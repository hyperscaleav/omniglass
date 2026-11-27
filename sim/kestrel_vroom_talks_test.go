package sim

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
)

// loadVRoom loads the shipped kestrel-vroom as the simulator does, with its
// live feed in place and its behaviors compiled but not running, so a test
// ticks them by hand.
func loadVRoom(t *testing.T, id string) *Device {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(store, profiles.Templates())
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: id, Hostname: id, LoopbackIP: "127.0.0.1", ProfileName: "kestrel-vroom", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	dev := e.Devices()[0]
	for _, iface := range store.Devices["kestrel-vroom"].Interfaces {
		if iface.Events.Path != "" {
			dev.SetEvents(iface.Events)
		}
	}
	return dev
}

// tickByName fires one tick of the named behavior.
func tickByName(t *testing.T, d *Device, name string) {
	t.Helper()
	if !d.TickBehavior(name) {
		t.Fatalf("%s has no behavior %q", d.DeviceID, name)
	}
}

// stamped is one event the feed carried, with the clock it was published at.
type stamped struct {
	at         time.Time
	name, data string
}

// simulateDay pins the bar's clock to from and runs its call cycle a minute a
// tick for minutes, its call stats twice a minute and its status line every
// 15, returning every named event (changes left out) with the minute it came.
// after, when set, is called with each minute once the cycle has ticked.
func simulateDay(t *testing.T, d *Device, from time.Time, minutes int, after func(time.Time)) []stamped {
	t.Helper()
	at := from
	d.SetClock(func() time.Time { return at })
	var out []stamped
	last := d.events.seq
	for i := range minutes {
		at = from.Add(time.Duration(i) * time.Minute)
		tickByName(t, d, "call_cycle")
		tickByName(t, d, "media_stats")
		tickByName(t, d, "media_stats")
		if i%15 == 0 {
			tickByName(t, d, "status_log")
		}
		if after != nil {
			after(at)
		}
		for _, ev := range d.events.replayFrom(strconv.FormatUint(last+1, 10)) {
			if ev.name != "change" {
				out = append(out, stamped{at, ev.name, ev.data})
			}
		}
		last = d.events.seq
	}
	return out
}

// loadVRoomIn loads the shipped kestrel-vroom placed in room, over the lab
// calendar of the seed and the test knobs, as a lab's simulator does: its
// behaviors compiled but not running, so a test ticks them by hand.
func loadVRoomIn(t *testing.T, id string, room calendar.Room, seed int64) (*Device, *calendar.Calendar) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	k := calTestKnobs()
	cal := calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true })
	e := NewEngine(store, profiles.Templates())
	e.SetCalendar(cal)
	snap := DeviceSnapshot{DeviceID: id, Hostname: id, LoopbackIP: "127.0.0.1", ProfileName: "kestrel-vroom", Version: 1,
		SiteCode: room.Site, Building: room.Building, Room: room.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	dev := e.Devices()[0]
	for _, iface := range store.Devices["kestrel-vroom"].Interfaces {
		if iface.Events.Path != "" {
			dev.SetEvents(iface.Events)
		}
	}
	return dev, cal
}

// held is the meeting the calendar has running in room at at, when somebody
// is in it: a no-show holds its slot and nobody joins.
func held(cal *calendar.Calendar, room calendar.Room, at time.Time) (calendar.Entry, bool) {
	e, ok := cal.Now(room, at)
	if !ok || e.Meeting.Outcome == calendar.OutcomeNoShow {
		return calendar.Entry{}, false
	}
	return e, true
}

// busyDay finds a room whose UTC day on the calendar of the seed holds
// every case the bar must act out: a no-show, a walk-in and a meeting that
// starts the moment the one before it ends.
func busyDay(t *testing.T, seed int64, day time.Time) (calendar.Room, []calendar.Entry) {
	t.Helper()
	k := calTestKnobs()
	cal := calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return k, true })
	for i := 1; i <= 60; i++ {
		room := calendar.Room{Site: "hq", Building: "tower", Room: "r" + strconv.Itoa(i)}
		es := cal.Entries(room, day, day.Add(24*time.Hour))
		var noShow, walkIn, backToBack, heldN int
		for j, e := range es {
			if e.Meeting.Outcome == calendar.OutcomeNoShow {
				noShow++
				continue
			}
			heldN++
			if e.Meeting.Origin == calendar.OriginWalkIn {
				walkIn++
			}
			if j > 0 && es[j-1].Meeting.Outcome != calendar.OutcomeNoShow && !es[j-1].End.Before(e.Start.Add(-time.Second)) && es[j-1].End.After(e.Start.Add(-time.Second)) {
				backToBack++
			}
		}
		if noShow > 0 && walkIn > 0 && backToBack > 0 && heldN >= 8 {
			return room, es
		}
	}
	t.Fatal("no room's day on the calendar holds a no-show, a walk-in and a back-to-back pair")
	return calendar.Room{}, nil
}

// tickUp is the first whole minute on or after x: the first tick that sees it.
func tickUp(x time.Time) time.Time {
	t := x.Truncate(time.Minute)
	if t.Before(x) {
		t = t.Add(time.Minute)
	}
	return t
}

func equalTimes(a, b []time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}

func clocks(ts []time.Time) []string {
	out := make([]string, len(ts))
	for i, x := range ts {
		out[i] = x.Format("15:04")
	}
	return out
}

// The VRoom reads its room's meetings off the lab calendar (#2767): over a
// simulated day on a pinned clock, after every tick the call is up exactly
// when the calendar says somebody is in the room (a no-show has no call, a
// walk-in has one), it carries that meeting's id, its participants are never
// more than who came, and a meeting starting the moment the last one ends
// moves the call on in the same tick. Every call starts and ends at the first
// tick on or after its meeting does, each occurrence is also a log line in the
// codec's idiom beside a status line every 15 minutes, and the bar keeps no
// schedule of its own. It logs the day's counts a fleet's cost is read from.
func TestTheVRoomsDayFollowsTheLabCalendar(t *testing.T) {
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	room, entries := busyDay(t, 7, day)
	d, cal := loadVRoomIn(t, "hq-divisible-conference-1-videobar-1", room, 7)

	var wantStarts, wantEnds []time.Time
	var heldEntries int
	for _, e := range entries {
		if e.Meeting.Outcome == calendar.OutcomeNoShow {
			continue
		}
		heldEntries++
		wantStarts = append(wantStarts, tickUp(e.Start))
		if end := tickUp(e.End); end.Before(day.Add(24 * time.Hour)) {
			wantEnds = append(wantEnds, end)
		}
	}

	var noShowTicks, walkInTicks, active, moves int
	evs := simulateDay(t, d, day, 24*60, func(at time.Time) {
		e, inRoom := held(cal, room, at)
		on := d.ReadPath("status.call.active")
		n, _, _ := toFloat(d.ReadPath("status.call.participants"))
		if on != inRoom {
			t.Fatalf("at %s the bar's call is %v, the calendar has the room %v", at.Format("15:04"), on, inRoom)
		}
		if now, ok := cal.Now(room, at); ok && now.Meeting.Outcome == calendar.OutcomeNoShow {
			noShowTicks++
		}
		if !inRoom {
			if n != 0 || d.ReadPath("status.presentation.local_active") != false {
				t.Fatalf("at %s the room is empty, yet the bar reads %v participants and content %v", at.Format("15:04"), n, d.ReadPath("status.presentation.local_active"))
			}
			return
		}
		active++
		if e.Meeting.Origin == calendar.OriginWalkIn {
			walkInTicks++
		}
		if want := "c" + strconv.FormatInt(e.Start.Unix(), 10); d.ReadPath("status.call.id") != want {
			t.Fatalf("at %s the call is %v, want the meeting's %s", at.Format("15:04"), d.ReadPath("status.call.id"), want)
		}
		if int(n) < 1 || int(n) > e.Meeting.Actual {
			t.Fatalf("at %s the call holds %v participants, the meeting had %d come", at.Format("15:04"), n, e.Meeting.Actual)
		}
	})
	if noShowTicks == 0 || walkInTicks == 0 {
		t.Fatalf("the day sampled %d no-show and %d walk-in ticks; want both", noShowTicks, walkInTicks)
	}

	var gotStarts, gotEnds []time.Time
	var inCall bool
	var id string
	perDay := map[string]int{}
	for _, ev := range evs {
		perDay[ev.name]++
		switch ev.name {
		case "call-started":
			var body struct {
				ID string `json:"id"`
				N  int    `json:"participants"`
			}
			if err := json.Unmarshal([]byte(ev.data), &body); err != nil || body.ID == "" || body.N < 1 {
				t.Fatalf("call-started %q", ev.data)
			}
			if inCall {
				t.Fatalf("a call started at %s inside call %s", ev.at.Format("15:04"), id)
			}
			inCall, id = true, body.ID
			gotStarts = append(gotStarts, ev.at)
		case "call-ended":
			if !inCall {
				t.Fatalf("a call ended at %s with none in progress", ev.at.Format("15:04"))
			}
			inCall = false
			gotEnds = append(gotEnds, ev.at)
		case "participant-joined", "participant-left":
			moves++
			if !inCall || !strings.Contains(ev.data, `"call_id":"`+id+`"`) {
				t.Fatalf("%s %q at %s outside call %s", ev.name, ev.data, ev.at.Format("15:04"), id)
			}
		case "log":
			var line struct{ Severity, Facility, Text string }
			if err := json.Unmarshal([]byte(ev.data), &line); err != nil || (line.Severity != "INFO" && line.Severity != "WARN") || !strings.HasPrefix(line.Facility, "kvroom.") || line.Text == "" {
				t.Fatalf("log line %q, want a severity, a kvroom facility and its text", ev.data)
			}
			if line.Facility == "kvroom.net" {
				perDay["net"]++
			}
		}
	}
	if !equalTimes(gotStarts, wantStarts) {
		t.Errorf("calls started at %v, want one at the first tick of each held meeting: %v", clocks(gotStarts), clocks(wantStarts))
	}
	if !equalTimes(gotEnds, wantEnds) {
		t.Errorf("calls ended at %v, want one at the first tick past each meeting's end: %v", clocks(gotEnds), clocks(wantEnds))
	}
	occurrences := perDay["call-started"] + perDay["call-ended"] + perDay["participant-joined"] + perDay["participant-left"]
	if perDay["log"] != occurrences+96+perDay["net"] {
		t.Errorf("%d log lines, want one per occurrence (%d), 96 status lines and the uplink's %d", perDay["log"], occurrences, perDay["net"])
	}
	t.Logf("%d calls (%d held meetings), %d minutes in a call, %d participant moves, %d events, %d log lines in a day",
		perDay["call-started"], heldEntries, active, moves, occurrences, perDay["log"])
}

// A bar placed in no room, or in a lab with no calendar, has nothing to act
// out: it never takes a call by itself.
func TestTheVRoomWithNoCalendarStaysIdle(t *testing.T) {
	d := loadVRoom(t, "bar")
	evs := simulateDay(t, d, time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC), 6*60, nil)
	for _, ev := range evs {
		if ev.name != "log" {
			t.Fatalf("a bar with no calendar reported %s %s", ev.name, ev.data)
		}
	}
	if d.ReadPath("status.call.active") != false {
		t.Fatal("a bar with no calendar is in a call")
	}
}

// A dialed call talks now (#2754): POST /api/call/dial starts it at once
// with its event and log line, the calendar does not fight it (a meeting
// running under it is skipped, and no tick ends it early), a hang-up ends it
// at once, the meeting it overtook is not joined late, and a call nobody hangs
// up ends after the default length.
func TestTheVRoomsDialedCallTalksNowAndTheCalendarYields(t *testing.T) {
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	room, entries := busyDay(t, 7, day)
	d, cal := loadVRoomIn(t, "hq-divisible-conference-1-videobar-1", room, 7)
	var meeting calendar.Entry
	for _, e := range entries {
		if e.Meeting.Outcome != calendar.OutcomeNoShow && e.Start.After(day.Add(time.Hour)) && e.End.Sub(e.Start) >= 25*time.Minute {
			meeting = e
			break
		}
	}
	if meeting.Meeting == nil {
		t.Fatal("no held meeting of 25 minutes in the day")
	}
	from := meeting.Start.Add(-5 * time.Minute).Truncate(time.Minute)
	at := from
	d.SetClock(func() time.Time { return at })
	named := func(since uint64) []string {
		var out []string
		for _, ev := range d.events.replayFrom(strconv.FormatUint(since+1, 10)) {
			if ev.name != "change" {
				out = append(out, ev.name+" "+ev.data)
			}
		}
		return out
	}
	tickByName(t, d, "call_cycle")
	if _, in := held(cal, room, at); in || d.ReadPath("status.call.active") != false {
		t.Fatal("the bar is in a call before the meeting")
	}
	mark := d.events.seq
	if res := d.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/call/dial", Body: `{"id":"c42","participants":3}`}); res.Status != 200 {
		t.Fatalf("dial = %+v", res)
	}
	got := strings.Join(named(mark), "\n")
	if !strings.Contains(got, `call-started {"id":"c42","participants":3}`) || !strings.Contains(got, `"text":"Call started (id=c42`) {
		t.Fatalf("the dial reported\n%s\nwant call-started and its log line at once", got)
	}
	mark = d.events.seq
	for i := 1; i <= 20; i++ {
		at = from.Add(time.Duration(i) * time.Minute)
		tickByName(t, d, "call_cycle")
	}
	if _, running := held(cal, room, at); !running {
		t.Fatalf("the meeting is not running at %s: the dial was not overtaking it", at.Format("15:04"))
	}
	for _, ev := range named(mark) {
		if strings.HasPrefix(ev, "call-started") || strings.HasPrefix(ev, "call-ended") {
			t.Fatalf("twenty minutes into a dialed call the calendar reported %q; it should yield", ev)
		}
	}
	if d.ReadPath("status.call.id") != "c42" || d.ReadPath("status.call.active") != true {
		t.Fatalf("the dialed call is %v/%v, want c42 still up", d.ReadPath("status.call.id"), d.ReadPath("status.call.active"))
	}
	mark = d.events.seq
	if res := d.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/call/disconnect", Body: "{}"}); res.Status != 200 {
		t.Fatalf("disconnect = %+v", res)
	}
	got = strings.Join(named(mark), "\n")
	if !strings.Contains(got, `call-ended {"id":"c42"}`) || !strings.Contains(got, "reason=local-hangup") {
		t.Fatalf("the hang-up reported\n%s\nwant call-ended and its log line at once", got)
	}
	// The meeting it overtook is not joined late once the dialed call is gone.
	mark = d.events.seq
	for _, m := range []int{21, 22} {
		at = from.Add(time.Duration(m) * time.Minute)
		tickByName(t, d, "call_cycle")
	}
	for _, ev := range named(mark) {
		if strings.HasPrefix(ev, "call-started") {
			t.Fatalf("the meeting the dialed call overtook started late: %q", ev)
		}
	}
	if d.ReadPath("status.call.active") != false {
		t.Fatal("the bar rejoined the meeting it hung up on")
	}

	// A dialed call nobody hangs up ends after the default length.
	if res := d.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/call/dial", Body: `{"id":"c43"}`}); res.Status != 200 {
		t.Fatalf("dial = %+v", res)
	}
	length, _, _ := toFloat(d.ReadPath("status.sim.dial_length"))
	mark = d.events.seq
	ended := -1
	for i := 1; i <= int(length)+2 && ended < 0; i++ {
		at = from.Add(time.Duration(22+i) * time.Minute)
		tickByName(t, d, "call_cycle")
		for _, ev := range named(mark) {
			if strings.HasPrefix(ev, `call-ended {"id":"c43"`) {
				ended = i
			}
		}
	}
	if ended != int(length) {
		t.Fatalf("an unanswered dialed call ended after %d ticks, want the default %v", ended, length)
	}
}

// A hang-up on a scheduled call ends it and the bar does not rejoin the same
// meeting, but joins the next.
func TestTheVRoomsHangUpOnAMeetingDoesNotRejoinIt(t *testing.T) {
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	room, entries := busyDay(t, 7, day)
	d, cal := loadVRoomIn(t, "hq-divisible-conference-1-videobar-1", room, 7)
	var meeting, next calendar.Entry
	for i, e := range entries {
		if e.Meeting.Outcome == calendar.OutcomeNoShow || e.End.Sub(e.Start) < 25*time.Minute {
			continue
		}
		for _, n := range entries[i+1:] {
			if n.Meeting.Outcome != calendar.OutcomeNoShow {
				meeting, next = e, n
				break
			}
		}
		if next.Meeting != nil {
			break
		}
	}
	if meeting.Meeting == nil || next.Meeting == nil {
		t.Fatal("no two held meetings in the day")
	}
	at := tickUp(meeting.Start.Add(3 * time.Minute))
	d.SetClock(func() time.Time { return at })
	tickByName(t, d, "call_cycle")
	if d.ReadPath("status.call.active") != true {
		t.Fatalf("the bar is not in the meeting that began at %s", meeting.Start.Format("15:04"))
	}
	if res := d.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/call/disconnect", Body: "{}"}); res.Status != 200 {
		t.Fatalf("disconnect = %+v", res)
	}
	at = at.Add(time.Minute)
	tickByName(t, d, "call_cycle")
	if _, still := held(cal, room, at); !still || d.ReadPath("status.call.active") != false {
		t.Fatalf("after the hang-up the meeting runs (%v) and the bar is in a call (%v); want it out", still, d.ReadPath("status.call.active"))
	}
	at = tickUp(next.Start)
	tickByName(t, d, "call_cycle")
	if _, running := held(cal, room, at); !running || d.ReadPath("status.call.active") != true {
		t.Fatalf("the bar did not join the next meeting (%s)", next.Start.Format("15:04"))
	}
}

// The call stats drift while a call is up and rest at zero when the room is
// idle (#2754): packet loss a ratio (0 to 1), jitter in milliseconds, the
// bitrate in kbps, the frame rate, the speaker level 0 to 100, and the
// microphones' input level in dBFS (a quiet room's -60 when idle). The codec's
// own load and temperature drift whether or not it is in a call.
func TestTheVRoomsCallStatsDriftOnlyInACall(t *testing.T) {
	d := loadVRoom(t, "hq-conf-1-videobar-1")
	num := func(path string) float64 {
		t.Helper()
		f, _, err := toFloat(d.ReadPath(path))
		if err != nil {
			t.Fatalf("%s = %v: %v", path, d.ReadPath(path), err)
		}
		return f
	}
	tickByName(t, d, "media_stats")
	for _, p := range []string{"status.media.packet_loss", "status.media.jitter_ms", "status.media.bitrate_kbps", "status.media.frame_rate", "status.audio.speaker_level"} {
		if v := num(p); v != 0 {
			t.Errorf("idle %s = %v, want 0", p, v)
		}
	}
	if v := num("status.device.cpu_load"); v <= 0 || v > 100 {
		t.Errorf("idle cpu_load = %v, want a percentage", v)
	}
	if v := num("status.audio.mic_level_db"); v != -60 {
		t.Errorf("idle mic_level_db = %v, want the quiet room's -60 dBFS", v)
	}
	if res := d.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/call/dial", Body: `{"id":"c7"}`}); res.Status != 200 {
		t.Fatalf("dial = %+v", res)
	}
	for range 4 {
		tickByName(t, d, "media_stats")
		if v := num("status.media.packet_loss"); v < 0 || v > 1 {
			t.Errorf("packet_loss = %v, want a ratio", v)
		}
		if v := num("status.media.bitrate_kbps"); v < 500 || v > 8000 {
			t.Errorf("bitrate_kbps = %v, want a call's", v)
		}
		if v := num("status.media.frame_rate"); v < 15 || v > 60 {
			t.Errorf("frame_rate = %v, want a call's", v)
		}
		if v := num("status.media.jitter_ms"); v <= 0 || v > 200 {
			t.Errorf("jitter_ms = %v, want a call's", v)
		}
		if v := num("status.audio.speaker_level"); v <= 0 || v > 100 {
			t.Errorf("speaker_level = %v, want a call's", v)
		}
		if v := num("status.audio.mic_level_db"); v < -50 || v > -6 {
			t.Errorf("mic_level_db = %v, want a talking room's", v)
		}
	}
	// A lossy stretch is a WARN line on the network facility, once, as the
	// loss crosses 3%.
	if err := SetPath(d.state, "status.media.packet_loss", 0.05); err != nil {
		t.Fatal(err)
	}
	if err := SetPath(d.state, "status.sim.uplink_degraded", false); err != nil {
		t.Fatal(err)
	}
	mark := d.events.seq
	tickByName(t, d, "media_stats")
	var warned bool
	for _, ev := range d.events.replayFrom(strconv.FormatUint(mark+1, 10)) {
		warned = warned || (ev.name == "log" && strings.Contains(ev.data, `"severity":"WARN","facility":"kvroom.net"`) && strings.Contains(ev.data, "packet loss"))
	}
	if !warned {
		t.Fatalf("a 5%% loss wrote no WARN line: %+v", d.events.replayFrom(strconv.FormatUint(mark+1, 10)))
	}
}

// Every VRoom behavior tick renders clean (#2754): across a simulated day,
// idle and in calls, no mutation or event fails, so the bar's own log is not
// a stream of template warnings.
func TestTheVRoomsTicksRenderClean(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	room, _ := busyDay(t, 7, day)
	d, _ := loadVRoomIn(t, "hq-conf-1-videobar-1", room, 7)
	at := day
	d.SetClock(func() time.Time { return at })
	for i := range 24 * 60 {
		at = day.Add(time.Duration(i) * time.Minute)
		tickByName(t, d, "call_cycle")
		if i%2 == 0 {
			tickByName(t, d, "media_stats")
		}
		if i%15 == 0 {
			tickByName(t, d, "status_log")
		}
	}
	if buf.Len() > 0 {
		t.Fatalf("the ticks warned:\n%.4000s", buf.String())
	}
}

// No VRoom behavior ticks faster than 15 s (#1360's cost rule): richness
// comes from more kinds of signal, not more samples.
func TestNoVRoomBehaviorTicksFasterThan15s(t *testing.T) {
	d := loadVRoom(t, "bar")
	if len(d.behaviors) == 0 {
		t.Fatal("kestrel-vroom has no behaviors")
	}
	for _, b := range d.behaviors {
		if b.Interval < 15*time.Second {
			t.Errorf("behavior %s ticks every %s, want 15 s or slower", b.Name, b.Interval)
		}
	}
}

// A reboot clears a dialed call's hold on the room, so the calendar's meeting
// is joined again by the next tick (the bar rejoins after a restart).
func TestTheVRoomRejoinsTheCalendarAfterARebootDuringADial(t *testing.T) {
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	room, entries := busyDay(t, 7, day)
	d, cal := loadVRoomIn(t, "hq-divisible-conference-1-videobar-1", room, 7)
	var meeting calendar.Entry
	for _, e := range entries {
		if e.Meeting.Outcome != calendar.OutcomeNoShow && e.End.Sub(e.Start) >= 25*time.Minute {
			meeting = e
			break
		}
	}
	at := tickUp(meeting.Start.Add(5 * time.Minute))
	d.SetClock(func() time.Time { return at })
	if res := d.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/call/dial", Body: `{"id":"c50"}`}); res.Status != 200 {
		t.Fatalf("dial = %+v", res)
	}
	if res := d.Operate(Operation{Kind: "http", Method: "POST", Path: "/api/admin/reboot", Body: `{}`}); res.Status != 200 {
		t.Fatalf("reboot = %+v", res)
	}
	tickByName(t, d, "call_cycle")
	// The meeting running when the dial began was never marked skipped (no tick
	// saw the dial), so the rebooted bar joins it.
	if _, running := held(cal, room, at); !running || d.ReadPath("status.call.active") != true {
		t.Fatalf("after a reboot the bar is in a call (%v) with the meeting running (%v); want it back in", d.ReadPath("status.call.active"), running)
	}
}
