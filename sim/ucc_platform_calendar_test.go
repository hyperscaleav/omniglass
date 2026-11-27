package sim

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// uccWorld is a meeting platform account over a world of three rooms, each a
// system the account covers, and the lab calendar of the seed, its clock pinned
// to *at.
func uccWorld(t *testing.T, at *time.Time) (*Device, *calendar.Calendar, map[string]calendar.Room) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	k := calTestKnobs()
	cal := calendar.New(7, func(calendar.Room) (calendar.Knobs, bool) { return k, true })
	e := NewEngine(store, profiles.Templates())
	e.SetCalendar(cal)
	rooms := map[string]calendar.Room{}
	w := &world.World{Version: world.Version}
	site := world.Site{Name: "hq"}
	svc := world.Service{Name: "ucc-hq", Profile: "ucc-platform", Webhook: true}
	for _, n := range []string{"r1", "r2", "r3"} {
		sys := "hq-" + n
		label := uccRoomLabels[n]
		site.Rooms = append(site.Rooms, world.Room{Name: n, Label: label, Building: "tower", System: sys})
		rooms[sys] = calendar.Room{Site: "hq", Building: "tower", Room: n}
		svc.Covers = append(svc.Covers, world.Cover{System: sys, Site: "hq", Building: "tower", Room: n, Label: label})
	}
	w.Sites = []world.Site{site}
	w.Services = []world.Service{svc}
	e.SetWorld(w)
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: "ucc-hq", Hostname: "ucc-hq", LoopbackIP: "127.0.0.1", ProfileName: "ucc-platform", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.SetClock(func() time.Time { return *at })
	tickByName(t, d, "rooms_calendar") // the account learns its rooms from the world
	return d, cal, rooms
}

// uccRoomLabels are the names the world gives the three rooms (#2757).
var uccRoomLabels = map[string]string{"r1": "Curie", "r2": "Hopper", "r3": "Huddle 3"}

type uccRoom struct {
	Status, Meeting, Topic string
	Participants           int
	Start                  int64
	Booking                struct {
		Topic      string
		Start, End int64
	}
	Next struct {
		Topic string
		Start int64
	}
}

func readRoomDocs(t *testing.T, d *Device) map[string]uccRoom {
	t.Helper()
	out := map[string]uccRoom{}
	token := ""
	for pages := 0; ; pages++ {
		var doc struct {
			Rooms []struct {
				ID, Status string
				Meeting    struct {
					ID, Topic    string
					Start        int64
					Participants int
				}
				Booking struct {
					Topic      string
					Start, End int64
				}
				Next struct {
					Topic string
					Start int64
				}
			}
			NextPageToken string `json:"next_page_token"`
		}
		path := "/api/rooms?page_size=2"
		if token != "" {
			path += "&next_page_token=" + token
		}
		if err := json.Unmarshal(uccCall(t, d, http.MethodGet, path, ""), &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Rooms) > 2 {
			t.Fatalf("a page of two holds %d rooms", len(doc.Rooms))
		}
		for _, r := range doc.Rooms {
			if _, dup := out[r.ID]; dup {
				t.Fatalf("room %s is on two pages", r.ID)
			}
			out[r.ID] = uccRoom{Status: r.Status, Meeting: r.Meeting.ID, Topic: r.Meeting.Topic, Participants: r.Meeting.Participants,
				Start: r.Meeting.Start, Booking: r.Booking, Next: r.Next}
		}
		if token = doc.NextPageToken; token == "" {
			return out
		}
		if pages > 50 {
			t.Fatal("the room list never ends")
		}
	}
}

// The platform's room list is the lab calendar's (#2767, #2790): over a
// simulated day on a pinned clock, after every tick each room the world places
// reads in-meeting exactly when somebody is in it (a no-show reads available,
// but its booking still holds the room), names the meeting by its start and the
// room, counts who came, and says which booking holds it now and which is next,
// all read through pages of two; a room the world does not name keeps the state
// its routes gave it.
func TestTheUCCPlatformsRoomsFollowTheLabCalendar(t *testing.T) {
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	at := day
	d, cal, rooms := uccWorld(t, &at)
	var inCall, noShow, walkIn, booked int
	for i := range 24 * 60 * 4 {
		at = day.Add(time.Duration(i) * 15 * time.Second)
		tickByName(t, d, "rooms_calendar")
		if i%4 != 0 {
			continue // read the room list each minute: one read is a full walk
		}
		got := readRoomDocs(t, d)
		for sys, room := range rooms {
			e, in := held(cal, room, at)
			r := got[sys]
			bk := RoomBookingsAt(cal, room, at)
			if r.Status != map[bool]string{true: "in-meeting", false: "available"}[in] {
				t.Fatalf("at %s %s reads %q, want in-meeting %v", at.Format("15:04:05"), sys, r.Status, in)
			}
			wantMeeting := ""
			if in {
				wantMeeting = fmt.Sprintf("m-%d-%s", e.Start.Unix(), sys)
				inCall++
				if caltest.M(e).Origin == calendar.OriginWalkIn {
					walkIn++
				}
				if r.Participants != caltest.M(e).Actual || r.Start != e.Start.Unix() || r.Topic == "" {
					t.Fatalf("at %s %s holds %+v, want %d people from %d with a topic", at.Format("15:04:05"), sys, r, caltest.M(e).Actual, e.Start.Unix())
				}
			} else if r.Participants != 0 || r.Topic != "" || r.Start != 0 {
				t.Fatalf("at %s %s reads %+v with nobody in it", at.Format("15:04:05"), sys, r)
			}
			if r.Meeting != wantMeeting {
				t.Fatalf("at %s %s names meeting %q, want %q", at.Format("15:04:05"), sys, r.Meeting, wantMeeting)
			}
			if now, ok := cal.Now(room, at, calendar.MeetingKinds); ok && caltest.M(now).Outcome == calendar.OutcomeNoShow {
				noShow++
				if caltest.M(now).Origin == calendar.OriginBooked && (r.Status != "available" || r.Booking.Start == 0) {
					t.Fatalf("at %s %s holds a booked no-show and reads %+v, want available with its booking", at.Format("15:04:05"), sys, r)
				}
			}
			// The booking is the booked slot holding the room now, the next one
			// the first booked meeting to begin after: a booked no-show leaves
			// the room available with its booking.
			if m, ok := bk["now"].(map[string]any); ok {
				booked++
				if r.Booking.Start != m["slot_start"] || r.Booking.End != m["slot_end"] || r.Booking.Topic == "" {
					t.Fatalf("at %s %s booking %+v, want the slot %v to %v", at.Format("15:04:05"), sys, r.Booking, m["slot_start"], m["slot_end"])
				}
			} else if r.Booking.Start != 0 || r.Booking.End != 0 {
				t.Fatalf("at %s %s reads a booking %+v the calendar does not hold", at.Format("15:04:05"), sys, r.Booking)
			}
			if n, ok := bk["next"].(map[string]any); ok {
				if r.Next.Start != n["slot_start"] {
					t.Fatalf("at %s %s next %+v, want the slot at %v", at.Format("15:04:05"), sys, r.Next, n["slot_start"])
				}
			} else if r.Next.Start != 0 {
				t.Fatalf("at %s %s reads a next booking %+v the calendar does not hold", at.Format("15:04:05"), sys, r.Next)
			}
		}
		if z := got["zr-huddle-north"]; z.Status != "in-meeting" || z.Meeting != "m-1001" || z.Participants != 3 {
			t.Fatalf("at %s a room the world does not name reads %+v, want its own state kept", at.Format("15:04"), z)
		}
	}
	if inCall == 0 || noShow == 0 || walkIn == 0 || booked == 0 {
		t.Fatalf("the day sampled %d in-meeting, %d no-show, %d walk-in and %d booked room-minutes; want all", inCall, noShow, walkIn, booked)
	}
}

// The platform tells the server when a meeting starts and ends (#2767): a
// signed room.meeting.started when a room's meeting begins and a
// room.meeting.ended when it ends, one per boundary and in order per room, a
// meeting starting as the last one ends told as the end and then the start, and
// none for the meetings already running when the account was subscribed.
func TestTheUCCPlatformPostsAMeetingsBoundaries(t *testing.T) {
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	at := day.Add(8 * time.Hour)
	d, cal, rooms := uccWorld(t, &at)
	var c caught
	sub, _ := json.Marshal(map[string]string{"url": c.server(t).URL, "scheme": "hmac", "secret": "ucc-lab-signing-key"})
	uccCall(t, d, http.MethodPost, "/api/webhooks", string(sub))

	type told struct{ event, meeting string }
	want := map[string][]told{}
	last := map[string]string{} // each room's meeting at the last tick, "" for none
	sent := 0
	step := func(i int) {
		at = day.Add(8*time.Hour + time.Duration(i)*15*time.Second)
		tickByName(t, d, "rooms_calendar")
		for sys, room := range rooms {
			cur := ""
			if e, in := held(cal, room, at); in {
				cur = fmt.Sprintf("m-%d-%s", e.Start.Unix(), sys)
			}
			if i > 0 && cur != last[sys] {
				// Told one boundary a tick: the ended before the started.
				if last[sys] != "" {
					want[sys] = append(want[sys], told{"room.meeting.ended", last[sys]})
				}
				if cur != "" {
					want[sys] = append(want[sys], told{"room.meeting.started", cur})
				}
			}
			last[sys] = cur
		}
		if kind, _ := d.ReadPath("status.sim.announce.kind").(string); kind != "" {
			sent++
			c.wait(t, sent)
		}
	}
	for i := 0; i < 10*60*4+8; i++ { // 08:00 to 18:00, and a few ticks for a queued boundary
		step(i)
	}
	total := 0
	for _, w := range want {
		total += len(w)
	}
	if total < 6 {
		t.Fatalf("the calendar holds only %d boundaries in the window", total)
	}
	posts := c.wait(t, total)
	got := map[string][]told{}
	for _, p := range posts {
		var b struct{ Event, RoomID, Meeting string }
		var raw struct {
			Event   string `json:"event"`
			RoomID  string `json:"room_id"`
			Meeting string `json:"meeting"`
		}
		if err := json.Unmarshal(p.Body, &raw); err != nil {
			t.Fatalf("a post is not JSON: %v: %s", err, p.Body)
		}
		b.Event, b.RoomID, b.Meeting = raw.Event, raw.RoomID, raw.Meeting
		if p.Header.Get("X-UCC-Event") != b.Event {
			t.Errorf("post %s carries event header %q", p.Body, p.Header.Get("X-UCC-Event"))
		}
		if b.Event == "rooms.state" {
			continue
		}
		if p.Header.Get("X-UCC-Signature") == "" {
			t.Errorf("post %s is unsigned", p.Body)
		}
		got[b.RoomID] = append(got[b.RoomID], told{b.Event, b.Meeting})
	}
	for sys, w := range want {
		if fmt.Sprint(got[sys]) != fmt.Sprint(w) {
			t.Errorf("%s was told\n%v\nwant\n%v", sys, got[sys], w)
		}
	}
	for sys := range got {
		if _, ok := want[sys]; !ok {
			t.Errorf("%s was told %v with no boundary on the calendar", sys, got[sys])
		}
	}
}

// The platform lists each room by the name a company gave it, the address
// beside it as a field of its own, as a rooms API lists a room and where it is
// (#2757); the id stays the system.
func TestTheUCCPlatformListsEachRoomByNameWithItsAddress(t *testing.T) {
	at := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	d, _, _ := uccWorld(t, &at)
	var doc struct {
		Rooms []struct{ ID, Name, Address string }
	}
	if err := json.Unmarshal(uccCall(t, d, http.MethodGet, "/api/rooms", ""), &doc); err != nil {
		t.Fatal(err)
	}
	got := map[string][2]string{}
	for _, r := range doc.Rooms {
		got[r.ID] = [2]string{r.Name, r.Address}
	}
	for n, label := range uccRoomLabels {
		if want := [2]string{label, n}; got["hq-"+n] != want {
			t.Errorf("room hq-%s lists as %v, want name %q and address %q", n, got["hq-"+n], want[0], want[1])
		}
	}
}
