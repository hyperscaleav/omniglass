package sim

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The scheduling side reads the lab calendar's booked entries (#2765): the
// booking service, the scheduling service and the slate panel say what the
// calendar says of a room, at every instant, on the device's clock. A no-show
// reads booked, a walk-in reads free, on every one of them.

var bookingRoom = calendar.Room{Site: "hq", Building: "tower", Room: "r101"}

// bookingDay finds a seed whose day for the room holds a booked meeting that
// ran, a no-show and a walk-in, and returns the calendar, the day's entries
// and one of each kind.
func bookingDay(t *testing.T) (*calendar.Calendar, time.Time, []calendar.Entry, calendar.Entry, calendar.Entry, calendar.Entry) {
	t.Helper()
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	for seed := int64(1); seed < 200; seed++ {
		cal := calendar.New(seed, func(calendar.Room) (calendar.Knobs, bool) { return calTestKnobs(), true })
		es := cal.Entries(bookingRoom, day, day.Add(24*time.Hour), calendar.MeetingKinds)
		var held, noShow, walkIn calendar.Entry
		for _, e := range es {
			switch {
			case caltest.M(e).Origin == calendar.OriginWalkIn && !caltest.IsMeeting(walkIn):
				walkIn = e
			case caltest.M(e).Outcome == calendar.OutcomeNoShow && !caltest.IsMeeting(noShow):
				noShow = e
			case caltest.M(e).Origin == calendar.OriginBooked && caltest.M(e).Outcome != calendar.OutcomeNoShow && !caltest.IsMeeting(held):
				held = e
			}
		}
		if caltest.IsMeeting(held) && caltest.IsMeeting(noShow) && caltest.IsMeeting(walkIn) {
			return cal, day, es, held, noShow, walkIn
		}
	}
	t.Fatal("no seed holds a held meeting, a no-show and a walk-in in one day")
	return nil, day, nil, calendar.Entry{}, calendar.Entry{}, calendar.Entry{}
}

// span is the time a booked entry holds its room: its slot and its own run,
// together.
func span(e calendar.Entry) (time.Time, time.Time) {
	from, to := e.Start, e.End
	if caltest.M(e).SlotStart.Before(from) {
		from = caltest.M(e).SlotStart
	}
	if caltest.M(e).SlotEnd.After(to) {
		to = caltest.M(e).SlotEnd
	}
	return from, to
}

// Over a simulated day, sampled every minute, the room's booking reading
// agrees with the calendar's booked entries: booked exactly while one holds
// the room, the count the booked meetings not yet over, and the next the
// first booked one to begin.
func TestRoomBookingsAgreeWithTheCalendarAtEveryInstant(t *testing.T) {
	cal, day, es, _, _, _ := bookingDay(t)
	for at := day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		var booked bool
		var today, remaining int
		var next *calendar.Entry
		for i := range es {
			e := es[i]
			if caltest.M(e).Origin != calendar.OriginBooked {
				continue
			}
			from, to := span(e)
			today++
			if to.After(at) {
				remaining++
			}
			if !at.Before(from) && at.Before(to) {
				booked = true
			}
			if from.After(at) && next == nil {
				next = &es[i]
			}
		}
		got := RoomBookingsAt(cal, bookingRoom, at)
		if got["booked"] != booked || got["today"] != today || got["remaining"] != remaining {
			t.Fatalf("at %s: booked %v today %v remaining %v, the calendar's booked entries say %v %d %d", at.Format("15:04"), got["booked"], got["today"], got["remaining"], booked, today, remaining)
		}
		if want := map[bool]string{true: "booked", false: "free"}[booked]; got["state"] != want {
			t.Fatalf("at %s: state %v, want %s", at.Format("15:04"), got["state"], want)
		}
		if next == nil {
			// Past the day's last booking the next one is another day's.
			if g, ok := got["next"].(map[string]any); ok && g["slot_start"].(int64) < day.Add(24*time.Hour).Unix() {
				t.Fatalf("at %s: next %v, the day holds none", at.Format("15:04"), g)
			}
			continue
		}
		if got["next"] == nil || got["next"].(map[string]any)["organizer"] != caltest.M(*next).Organizer {
			t.Fatalf("at %s: next is %v, want %s", at.Format("15:04"), got["next"], caltest.M(*next).Organizer)
		}
	}
}

// A no-show holds its slot, so the room reads booked through it; a walk-in
// is nobody's booking, so the room reads free through it, and it counts for
// nothing the day holds.
func TestANoShowReadsBookedAndAWalkInReadsFree(t *testing.T) {
	cal, _, _, _, noShow, walkIn := bookingDay(t)
	mid := func(e calendar.Entry) time.Time { return e.Start.Add(e.End.Sub(e.Start) / 2) }

	got := RoomBookingsAt(cal, bookingRoom, mid(noShow))
	if got["state"] != "booked" || got["now"] == nil || got["now"].(map[string]any)["outcome"] != "no-show" {
		t.Fatalf("mid no-show the room reads %v, want booked on the no-show", got)
	}
	got = RoomBookingsAt(cal, bookingRoom, mid(walkIn))
	if got["state"] != "free" || got["now"] != nil {
		t.Fatalf("mid walk-in the room reads %v, want free", got)
	}
	if n := got["today"].(int); n == 0 {
		t.Fatalf("a day with booked meetings counts %d", n)
	}
}

// With no calendar a room reads free and empty, every key present.
func TestRoomBookingsWithNoCalendarReadFree(t *testing.T) {
	got := RoomBookingsAt(nil, bookingRoom, time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC))
	if got["state"] != "free" || got["today"] != 0 || got["remaining"] != 0 || got["now"] != nil || got["next"] != nil || got["booked"] != false {
		t.Fatalf("no calendar reads %v", got)
	}
}

// bookingEngine is an engine over the shipped catalog and the test calendar,
// holding one device of the profile in the room, its clock pinned, with the
// world of two rooms and (when service) the device covering both.
func bookingEngine(t *testing.T, cal *calendar.Calendar, profileName string, service bool, at *time.Time) *Device {
	t.Helper()
	return bookingEngineIn(t, cal, profileName, service, !service, at)
}

// bookingEngineIn is bookingEngine with the choice of placing the device in a room.
func bookingEngineIn(t *testing.T, cal *calendar.Calendar, profileName string, service, placed bool, at *time.Time) *Device {
	t.Helper()
	e := shippedEngine(t)
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	e.SetCalendar(cal)
	w := &world.World{Version: world.Version, Sites: []world.Site{{Name: "hq", Rooms: []world.Room{
		{Name: "r101", Label: "Curie", Building: "tower", System: "sys-101"}, {Name: "r102", Label: "Hopper", Building: "tower", System: "sys-102"}}}}}
	if service {
		w.Services = []world.Service{{Name: "svc-1", Profile: profileName, Covers: []world.Cover{
			{System: "sys-101", Site: "hq", Building: "tower", Room: "r101", Label: "Curie"}, {System: "sys-102", Site: "hq", Building: "tower", Room: "r102", Label: "Hopper"}}}}
	}
	e.SetWorld(w)
	snap := DeviceSnapshot{DeviceID: "svc-1", Hostname: "svc-1", LoopbackIP: "127.0.0.1", ProfileName: profileName, Version: 1}
	if placed {
		snap.SiteCode, snap.Building, snap.Room = "hq", "tower", "r101"
	}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.StopBehaviors()
	d.SetClock(pinned(at))
	return d
}

// The booking service pushes each system's booked count and state from the
// calendar: the retired fixed day is gone, a no-show pushes booked, a
// walk-in pushes free, and a system the world does not name pushes free.
func TestBookingServicePushesTheCalendarsBookings(t *testing.T) {
	cal, _, _, held, noShow, walkIn := bookingDay(t)
	at := held.Start.Add(time.Minute)
	d := bookingEngine(t, cal, "booking-service", true, &at)
	push := func(owner string) (float64, string) {
		t.Helper()
		body, err := d.RenderPush(owner)
		if err != nil {
			t.Fatal(err)
		}
		var b struct {
			Metrics    []struct{ Value float64 }
			Properties []struct{ Value string }
		}
		if err := json.Unmarshal(body, &b); err != nil {
			t.Fatalf("%s\n%v", body, err)
		}
		return b.Metrics[0].Value, b.Properties[0].Value
	}
	check := func(label string, when time.Time, owner string) {
		t.Helper()
		at = when
		want := RoomBookingsAt(cal, bookingRoom, when)
		n, state := push(owner)
		if n != float64(want["remaining"].(int)) || state != want["state"] {
			t.Fatalf("%s: pushed %v %s, the calendar says %v %v", label, n, state, want["remaining"], want["state"])
		}
	}
	check("held meeting", held.Start.Add(time.Minute), "sys-101")
	if _, s := push("sys-101"); s != "booked" {
		t.Fatalf("mid held meeting pushes %s", s)
	}
	at = noShow.Start.Add(time.Minute)
	if _, s := push("sys-101"); s != "booked" {
		t.Fatalf("a no-show pushes %s, want booked", s)
	}
	at = walkIn.Start.Add(time.Minute)
	if _, s := push("sys-101"); s != "free" {
		t.Fatalf("a walk-in pushes %s, want free", s)
	}
	check("walk-in", walkIn.Start.Add(time.Minute), "sys-101")
	// A system the world does not place has no calendar: the service keeps
	// its previous fixed meeting day for it, exactly.
	at = time.Date(2026, 10, 29, 13, 30, 0, 0, time.UTC)
	if n, s := push("sys-404"); n != 2 || s != "booked" {
		t.Fatalf("a system with no calendar pushes %v %s, want the fixed day's 2 booked at 13:30", n, s)
	}
	// A day's end: nothing left, free.
	at = time.Date(2026, 11, 3, 23, 59, 0, 0, time.UTC)
	if n, s := push("sys-101"); n != 0 || s != "free" {
		t.Fatalf("at day's end the room pushes %v %s", n, s)
	}
}

// The scheduling service counts the booked entries of the rooms it serves,
// across a simulated day, and serves those rooms.
func TestSchedulingServiceCountsTheServedRoomsBookings(t *testing.T) {
	cal, day, _, _, noShow, walkIn := bookingDay(t)
	at := day
	d := bookingEngine(t, cal, "scheduling-service", true, &at)
	rooms := []calendar.Room{bookingRoom, {Site: "hq", Building: "tower", Room: "r102"}}
	samples := []time.Time{day, noShow.Start.Add(time.Minute), walkIn.Start.Add(time.Minute), day.Add(12 * time.Hour), day.Add(23*time.Hour + 59*time.Minute)}
	for _, when := range samples {
		at = when
		d.TickBehavior("bookings_sync")
		var today, remaining, inProgress int
		for _, r := range rooms {
			b := RoomBookingsAt(cal, r, when)
			today += b["today"].(int)
			remaining += b["remaining"].(int)
			if b["booked"] == true {
				inProgress++
			}
		}
		for path, want := range map[string]int{"status.bookings.today": today, "status.bookings.remaining": remaining, "status.bookings.in_progress": inProgress, "configuration.rooms_served": 2} {
			if got := fmt.Sprint(d.ReadPath(path)); got != fmt.Sprint(want) {
				t.Fatalf("at %s: %s = %s, the calendar says %d", when.Format("15:04"), path, got, want)
			}
		}
	}
	// A no-show's room is in progress, a walk-in's is not.
	at = noShow.Start.Add(time.Minute)
	d.TickBehavior("bookings_sync")
	if n := fmt.Sprint(d.ReadPath("status.bookings.in_progress")); n == "0" {
		t.Fatal("a no-show's room reads no booking in progress")
	}
	// A service the world covers no room for has no calendar and keeps the
	// previous fixed 8, whatever the calendar holds.
	at = day
	e := bookingEngineIn(t, cal, "scheduling-service", false, false, &at)
	e.TickBehavior("bookings_sync")
	if got := fmt.Sprint(e.ReadPath("status.bookings.today")); got != "8" {
		t.Fatalf("a service covering no room counts %s bookings, want the previous 8", got)
	}
}

// The slate panel shows its own room's booked meeting now and next and its
// booked or free state; a no-show reads booked, a walk-in free; a panel
// button holds only until the calendar's reading changes.
func TestSlatePanelShowsItsRoomsBookedMeetings(t *testing.T) {
	cal, day, _, held, noShow, walkIn := bookingDay(t)
	at := day
	d := bookingEngine(t, cal, "newtron-slate-5", false, &at)
	read := func(when time.Time) (state, current, next string) {
		t.Helper()
		at = when
		d.TickBehavior("room_bookings")
		return fmt.Sprint(d.ReadPath("status.room.booking")), fmt.Sprint(d.ReadPath("status.room.current_meeting")), fmt.Sprint(d.ReadPath("status.room.next_meeting"))
	}
	fields := func(m map[string]any) string {
		if m == nil {
			return "none"
		}
		return fmt.Sprintf("%d %d %s", m["slot_start"], m["slot_end"], m["organizer"])
	}
	for _, when := range []time.Time{day, held.Start.Add(time.Minute), noShow.Start.Add(time.Minute), walkIn.Start.Add(time.Minute), day.Add(23 * time.Hour)} {
		b := RoomBookingsAt(cal, bookingRoom, when)
		state, current, next := read(when)
		now, _ := b["now"].(map[string]any)
		nx, _ := b["next"].(map[string]any)
		if state != b["state"] || current != fields(now) || next != fields(nx) {
			t.Fatalf("at %s the panel reads %s | %s | %s, the calendar says %v | %s | %s", when.Format("15:04"), state, current, next, b["state"], fields(now), fields(nx))
		}
	}
	if s, _, _ := read(noShow.Start.Add(time.Minute)); s != "booked" {
		t.Fatalf("a no-show reads %s on the panel, want booked", s)
	}
	if s, c, _ := read(walkIn.Start.Add(time.Minute)); s != "free" || c != "none" {
		t.Fatalf("a walk-in reads %s %s on the panel, want free and none", s, c)
	}

	// A panel button is a momentary override: free in a free room until the
	// calendar's reading changes, then the calendar takes over again.
	at = walkIn.Start.Add(time.Minute)
	d.TickBehavior("room_bookings")
	d.SetState(map[string]any{"status": map[string]any{"room": map[string]any{
		"booking": "booked", "cal_booking": d.ReadPath("status.room.cal_booking"),
		"current_meeting": "none", "next_meeting": "none"}}}, 3)
	if s, _, _ := read(walkIn.Start.Add(2 * time.Minute)); s != "booked" {
		t.Fatalf("a booked button press did not hold across an unchanged reading: %s", s)
	}
	if s, _, _ := read(held.Start.Add(time.Minute)); s != "booked" {
		t.Fatalf("mid held meeting the panel reads %s", s)
	}
	if s, _, _ := read(walkIn.Start.Add(time.Minute)); s != "free" {
		t.Fatalf("after the calendar moved on the panel still reads %s", s)
	}
}

// A pusher the world covers pushes for the system names the world gives it,
// whatever uuids its grant names (a grant carries the server's ids, which the
// simulator does not hold), so the owner resolves to a room; with no world it
// pushes for its grant.
func TestBookingServicePushesForTheSystemsTheWorldNames(t *testing.T) {
	cal, _, _, held, _, _ := bookingDay(t)
	srv := &pushServer{grants: []map[string]string{{"scope_kind": "system", "scope_id": "0b6f3c3e-uuid-1"}, {"scope_kind": "system", "scope_id": "0b6f3c3e-uuid-2"}}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	at := held.Start.Add(time.Minute)
	d := bookingEngine(t, cal, "booking-service", true, &at)
	d.SetPushCredential(ts.URL, "tok")
	if err := d.PushOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(srv.refs(), ","); got != "sys-101,sys-102" {
		t.Fatalf("pushed for %s, want the world's names sys-101,sys-102", got)
	}
	var b struct {
		Properties []struct{ Value string }
	}
	srv.mu.Lock()
	raw, _ := json.Marshal(srv.pushes[0])
	srv.mu.Unlock()
	_ = json.Unmarshal(raw, &b)
	if len(b.Properties) == 0 || b.Properties[0].Value != "booked" {
		t.Fatalf("the first push says %s, want booked mid meeting", raw)
	}
}

// A panel whose room has no calendar behaves as it always did: free at rest,
// BOOK and RELEASE hold, and no behavior tick moves them.
func TestSlatePanelWithNoCalendarKeepsItsButtons(t *testing.T) {
	at := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	d := bookingEngine(t, nil, "newtron-slate-5", false, &at)
	d.TickBehavior("room_bookings")
	if got := fmt.Sprint(d.ReadPath("status.room.booking")); got != "free" {
		t.Fatalf("a panel with no calendar reads %s", got)
	}
	d.SetState(map[string]any{"status": map[string]any{"room": map[string]any{"booking": "booked", "cal_booking": "free", "current_meeting": "none", "next_meeting": "none"}}}, 3)
	d.TickBehavior("room_bookings")
	at = at.Add(time.Hour)
	d.TickBehavior("room_bookings")
	if got := fmt.Sprint(d.ReadPath("status.room.booking")); got != "booked" {
		t.Fatalf("a BOOK on a panel with no calendar did not hold: %s", got)
	}
}

// A scheduling service placed in a room serves that room.
func TestSchedulingServiceInARoomServesIt(t *testing.T) {
	cal, _, _, held, _, _ := bookingDay(t)
	at := held.Start.Add(time.Minute)
	d := bookingEngine(t, cal, "scheduling-service", false, &at)
	d.TickBehavior("bookings_sync")
	b := RoomBookingsAt(cal, bookingRoom, at)
	if got := fmt.Sprint(d.ReadPath("status.bookings.today")); got != fmt.Sprint(b["today"]) {
		t.Fatalf("today = %s, the room's calendar says %v", got, b["today"])
	}
	if got := fmt.Sprint(d.ReadPath("configuration.rooms_served")); got != "1" {
		t.Fatalf("rooms_served = %s, want 1", got)
	}
	if got := fmt.Sprint(d.ReadPath("status.bookings.in_progress")); got != "1" {
		t.Fatalf("in_progress = %s, want 1 mid held meeting", got)
	}
}

// The scheduling service lists each room it covers as a record named by its
// system, with the room's bookings off the calendar, and one room nobody
// bound; a service covering no room lists none.
func TestSchedulingServiceListsItsRoomsAsRecords(t *testing.T) {
	cal, _, _, held, noShow, walkIn := bookingDay(t)
	at := held.Start.Add(time.Minute)
	d := bookingEngine(t, cal, "scheduling-service", true, &at)
	rooms := func(when time.Time) map[string]any {
		at = when
		d.TickBehavior("bookings_sync")
		m, _ := d.ReadPath("status.rooms").(map[string]any)
		return m
	}
	for _, c := range []struct {
		when time.Time
		want string
	}{{held.Start.Add(time.Minute), "booked"}, {noShow.Start.Add(time.Minute), "booked"}, {walkIn.Start.Add(time.Minute), "free"}} {
		got := rooms(c.when)
		r, ok := got["sys-101"].(map[string]any)
		if !ok || r["state"] != c.want {
			t.Fatalf("at %s sys-101 reads %v, want %s", c.when.Format("15:04"), got["sys-101"], c.want)
		}
		if b := RoomBookingsAt(cal, bookingRoom, c.when); fmt.Sprint(r["remaining"]) != fmt.Sprint(b["remaining"]) {
			t.Fatalf("remaining %v, the calendar says %v", r["remaining"], b["remaining"])
		}
		if _, ok := got["sys-102"]; !ok {
			t.Fatal("the second covered room is not listed")
		}
		if u, ok := got["unassigned-room"].(map[string]any); !ok || u["state"] != "free" {
			t.Fatalf("the unbound room reads %v", got["unassigned-room"])
		}
	}
	at = held.Start
	e := bookingEngineIn(t, cal, "scheduling-service", false, false, &at)
	e.TickBehavior("bookings_sync")
	if m, _ := e.ReadPath("status.rooms").(map[string]any); len(m) != 0 {
		t.Fatalf("a service covering no room lists %v", m)
	}
}

// A scheduling platform lists each room by its name, as a calendar does, the
// address beside it (#2757): the record is keyed by the system, its name the
// label the world gave the room and its address the room's own.
func TestSchedulingServiceListsEachRoomByNameWithItsAddress(t *testing.T) {
	cal, _, _, held, _, _ := bookingDay(t)
	at := held.Start.Add(time.Minute)
	d := bookingEngine(t, cal, "scheduling-service", true, &at)
	d.TickBehavior("bookings_sync")
	var doc struct {
		Rooms []struct{ ID, Name, Address string }
	}
	body := d.Operate(Operation{Kind: "http", Method: "GET", Path: "/api/rooms"})
	if err := json.Unmarshal([]byte(body.Body), &doc); err != nil {
		t.Fatalf("GET /api/rooms answered %q: %v", body.Body, err)
	}
	got := map[string][2]string{}
	for _, r := range doc.Rooms {
		got[r.ID] = [2]string{r.Name, r.Address}
	}
	for id, want := range map[string][2]string{"sys-101": {"Curie", "r101"}, "sys-102": {"Hopper", "r102"}} {
		if got[id] != want {
			t.Errorf("room %s lists as %v, want name %q and address %q", id, got[id], want[0], want[1])
		}
	}
}
