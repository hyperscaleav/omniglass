package sim

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// uccWorld is a meeting platform account over a world of three rooms, each a
// system, and the lab calendar of the seed, its clock pinned to *at. Each room
// is added to the account as a lab's seed does it, through the provision route.
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
	for _, n := range []string{"r1", "r2", "r3"} {
		sys := "hq-" + n
		site.Rooms = append(site.Rooms, world.Room{Name: n, Building: "tower", System: sys})
		rooms[sys] = calendar.Room{Site: "hq", Building: "tower", Room: n}
	}
	w.Sites = []world.Site{site}
	e.SetWorld(w)
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: "ucc-hq", Hostname: "ucc-hq", LoopbackIP: "127.0.0.1", ProfileName: "ucc-platform", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.SetClock(func() time.Time { return *at })
	for sys := range rooms {
		uccCall(t, d, http.MethodPost, "/api/rooms", fmt.Sprintf(`{"id":%q}`, sys))
	}
	return d, cal, rooms
}

type uccRoom struct {
	State, Meeting string
	Participants   int
}

func readRoomDocs(t *testing.T, d *Device) map[string]uccRoom {
	t.Helper()
	var doc struct {
		Rooms []struct {
			ID, State, Meeting string
			Participants       int
		}
	}
	if err := json.Unmarshal(uccCall(t, d, http.MethodGet, "/api/rooms", ""), &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]uccRoom{}
	for _, r := range doc.Rooms {
		out[r.ID] = uccRoom{r.State, r.Meeting, r.Participants}
	}
	return out
}

// The platform's room state is the lab calendar's (#2767): over a simulated
// day on a pinned clock, after every tick each room the world places reads
// in-call exactly when somebody is in it (a no-show reads idle), names the
// meeting by its start and the room, and counts who came; a room the world
// does not name keeps the state its routes gave it.
func TestTheUCCPlatformsRoomsFollowTheLabCalendar(t *testing.T) {
	day := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	at := day
	d, cal, rooms := uccWorld(t, &at)
	var inCall, noShow, walkIn int
	for i := range 24 * 60 * 4 {
		at = day.Add(time.Duration(i) * 15 * time.Second)
		tickByName(t, d, "rooms_calendar")
		if i%4 != 0 {
			continue // read the room list each minute: one read is a full render
		}
		got := readRoomDocs(t, d)
		for sys, room := range rooms {
			e, in := held(cal, room, at)
			r := got[sys]
			want := uccRoom{State: "idle"}
			if in {
				want = uccRoom{"in-call", fmt.Sprintf("m-%d-%s", e.Start.Unix(), sys), e.Meeting.Actual}
				inCall++
				if e.Meeting.Origin == calendar.OriginWalkIn {
					walkIn++
				}
			} else if now, ok := cal.Now(room, at); ok && now.Meeting.Outcome == calendar.OutcomeNoShow {
				noShow++
			}
			if r != want {
				t.Fatalf("at %s %s reads %+v, want %+v", at.Format("15:04:05"), sys, r, want)
			}
		}
		if z := got["zr-huddle-north"]; z.State != "in-call" || z.Meeting != "m-1001" || z.Participants != 3 {
			t.Fatalf("at %s a room the world does not name reads %+v, want its own state kept", at.Format("15:04"), z)
		}
	}
	if inCall == 0 || noShow == 0 || walkIn == 0 {
		t.Fatalf("the day sampled %d in-call, %d no-show and %d walk-in room-minutes; want all", inCall, noShow, walkIn)
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
