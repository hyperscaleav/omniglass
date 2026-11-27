package sim

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/impair"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// uccDeviceWorld is a meeting platform account over a world of three rooms and
// the devices in them: a bar, a codec, a controller and a display each, the
// display the one a meeting platform does not list. The ids are the world's.
func uccDeviceWorld(t *testing.T, at *time.Time, problems ...calendar.ProblemSpec) (*Device, []world.Listing) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	k := calTestKnobs()
	e := NewEngine(store, profiles.Templates())
	cal := calendar.New(7, func(calendar.Room) (calendar.Knobs, bool) { return k, true })
	if len(problems) > 0 {
		cal = cal.WithProblems(func(r calendar.Room) []calendar.ProblemSpec {
			if r.Room == "r2" {
				return problems
			}
			return nil
		})
	}
	e.SetCalendar(cal)
	w := &world.World{Version: world.Version}
	site := world.Site{Name: "hq"}
	svc := world.Service{Name: "ucc-hq", Profile: "ucc-platform", Webhook: true}
	for _, n := range []string{"r1", "r2", "r3"} {
		sys := "hq-" + n
		r := world.Room{Name: n, Building: "tower", System: sys}
		for _, dv := range []struct{ slot, platform, fw string }{
			{"video-bar", "bar", "2.7.3"}, {"codec", "codec", "2.7.3"}, {"touch-control", "controller", "4.1.2"}, {"main-display", "", "9.9"},
		} {
			name := sys + "-" + dv.slot + "-1"
			r.Devices = append(r.Devices, world.Device{Name: name, Slot: dv.slot, ID: "dev-" + name, Product: dv.slot, Platform: dv.platform, Firmware: dv.fw})
		}
		site.Rooms = append(site.Rooms, r)
		svc.Covers = append(svc.Covers, world.Cover{System: sys, Site: "hq", Building: "tower", Room: n})
	}
	w.Sites, w.Services = []world.Site{site}, []world.Service{svc}
	e.SetWorld(w)
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: "ucc-hq", Hostname: "ucc-hq", LoopbackIP: "127.0.0.1", ProfileName: "ucc-platform", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	d.SetClock(func() time.Time { return *at })
	return d, w.Listed("ucc-hq")
}

type uccListed struct {
	ID, Name, Type, Status, Firmware string
	RoomID                           string `json:"room_id"`
	LastSeen                         int64  `json:"last_seen"`
}

// walkDevices reads the device list a page of n at a time, following each
// answer's token to the last.
func walkDevices(t *testing.T, d *Device, n int) (out []uccListed, pages int) {
	t.Helper()
	token := ""
	for {
		var doc struct {
			Devices       []uccListed
			NextPageToken string `json:"next_page_token"`
		}
		path := fmt.Sprintf("/api/devices?page_size=%d", n)
		if token != "" {
			path += "&next_page_token=" + token
		}
		if err := json.Unmarshal(uccCall(t, d, http.MethodGet, path, ""), &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Devices) > n {
			t.Fatalf("a page of %d holds %d devices", n, len(doc.Devices))
		}
		out = append(out, doc.Devices...)
		pages++
		if token = doc.NextPageToken; token == "" {
			return out, pages
		}
		if pages > 100 {
			t.Fatal("the device list never ends")
		}
	}
}

// The platform's device list is the world's (#2790): paged, every device of
// the rooms the account covers that a meeting platform manages appears once,
// in id order, carrying the world's id, name, room, kind and firmware, whatever
// the size of the page, beside the devices the platform holds that no room
// places; a display, which a meeting platform does not list, appears nowhere.
func TestTheUCCPlatformListsTheWorldsDevicesPaged(t *testing.T) {
	at := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	d, listed := uccDeviceWorld(t, &at)
	if len(listed) != 9 {
		t.Fatalf("the world lists %d devices, want 3 rooms of 3", len(listed))
	}
	for _, size := range []int{1, 2, 4, 11, 300} {
		got, pages := walkDevices(t, d, size)
		if want := (len(listed) + 2 + size - 1) / size; pages != want {
			t.Errorf("page size %d took %d pages, want %d", size, pages, want)
		}
		if len(got) != len(listed)+2 {
			t.Fatalf("page size %d read %d devices, want the %d the world lists and the 2 the platform holds", size, len(got), len(listed))
		}
		byID := map[string]uccListed{}
		for i, g := range got {
			if _, dup := byID[g.ID]; dup {
				t.Fatalf("device %s is on two pages", g.ID)
			}
			byID[g.ID] = g
			if i > 0 && got[i-1].ID >= g.ID {
				t.Fatalf("devices out of id order: %s then %s", got[i-1].ID, g.ID)
			}
		}
		for _, l := range listed {
			g, ok := byID[l.ID]
			if !ok {
				t.Fatalf("page size %d never lists %s", size, l.ID)
			}
			if g.Name != l.Name || g.RoomID != l.System || g.Type != l.Type || g.Firmware != l.Firmware || g.Status != "online" || g.LastSeen != at.Unix() {
				t.Errorf("%s reads %+v, want the world's %+v online, seen now", l.ID, g, l)
			}
		}
		if _, ok := byID["dev-hq-r1-main-display-1"]; ok {
			t.Error("a display, which a meeting platform does not list, is on the list")
		}
		if h := byID["zd-annex-codec"]; h.Status != "offline" || h.Type != "codec" {
			t.Errorf("the held annex codec reads %+v, want it offline", h)
		}
	}
}

// A device the platform holds goes offline and online by the platform's own
// route, and each change posts device.status.changed naming the room and the
// device by id, signed as every post is.
func TestTheUCCPlatformPostsADeviceStatusChange(t *testing.T) {
	at := time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)
	d, _ := uccDeviceWorld(t, &at)
	var c caught
	sub, _ := json.Marshal(map[string]string{"url": c.server(t).URL, "scheme": "hmac", "secret": "ucc-lab-signing-key"})
	uccCall(t, d, http.MethodPost, "/api/webhooks", string(sub))
	uccCall(t, d, http.MethodPost, "/api/devices/status", `{"device":"zd-spare-bar","room":"zr-boardroom-b","status":"offline"}`)
	uccCall(t, d, http.MethodPost, "/api/rooms/participants/join", `{"room":"zr-huddle-north","participants":4}`)
	uccCall(t, d, http.MethodPost, "/api/rooms/participants/leave", `{"room":"zr-huddle-north","participants":2}`)
	posts := c.wait(t, 3)
	got := map[string]map[string]any{}
	for _, p := range posts {
		var b map[string]any
		if err := json.Unmarshal(p.Body, &b); err != nil {
			t.Fatalf("a post is not JSON: %v: %s", err, p.Body)
		}
		ev, _ := b["event"].(string)
		if p.Header.Get("X-UCC-Event") != ev || p.Header.Get("X-UCC-Signature") == "" {
			t.Errorf("post %s is unsigned or its header %q names another event", p.Body, p.Header.Get("X-UCC-Event"))
		}
		got[ev] = b
	}
	if b := got["device.status.changed"]; b["device_id"] != "zd-spare-bar" || b["room_id"] != "zr-boardroom-b" || b["status"] != "offline" {
		t.Errorf("device post = %v", b)
	}
	if b := got["room.participant.joined"]; b["room_id"] != "zr-huddle-north" || b["participants"] != float64(4) {
		t.Errorf("joined post = %v", b)
	}
	if b := got["room.participant.left"]; b["room_id"] != "zr-huddle-north" || b["participants"] != float64(2) {
		t.Errorf("left post = %v", b)
	}
	devs, _ := walkDevices(t, d, 300)
	for _, g := range devs {
		if g.ID == "zd-spare-bar" && (g.Status != "offline" || time.Now().Unix()-g.LastSeen > 5) {
			t.Errorf("the held bar reads %+v after going offline", g)
		}
	}
	if rooms := readRoomDocs(t, d); rooms["zr-huddle-north"].Participants != 2 {
		t.Errorf("the room reads %d participants, want 2", rooms["zr-huddle-north"].Participants)
	}
}

// #2769: a device a scripted problem has unplugged reads offline on the
// platform's devices list for as long as the problem is on the calendar, last
// seen when it began, and its room reads offline when the dark device is its
// codec or its bar; a pinned or lossy problem, a problem on another role and a
// problem's own end change nothing of the sort. The reading is the calendar's,
// not the unplugged device's own state: the listed device is never served.
func TestAnUnpluggedDeviceReadsOfflineOnThePlatform(t *testing.T) {
	open := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	problem := func(name, role string, imp calendar.Impairment) calendar.ProblemSpec {
		return calendar.ProblemSpec{Name: name, Cadence: "daily", At: "10:00", For: time.Hour,
			Scope: calendar.Scope{Rooms: []string{"hq-r2"}, Role: role}, Impairment: imp}
	}
	statuses := func(d *Device) (map[string]uccListed, map[string]string) {
		devs, _ := walkDevices(t, d, 50)
		byName := map[string]uccListed{}
		for _, g := range devs {
			byName[g.Name] = g
		}
		var rooms struct {
			Rooms []struct{ ID, Status string }
		}
		if err := json.Unmarshal(uccCall(t, d, http.MethodGet, "/api/rooms?page_size=50", ""), &rooms); err != nil {
			t.Fatal(err)
		}
		st := map[string]string{}
		for _, r := range rooms.Rooms {
			st[r.ID] = r.Status
		}
		return byName, st
	}
	at := open.Add(-time.Minute)
	d, _ := uccDeviceWorld(t, &at,
		problem("codec-dark", "codec", calendar.Impairment{Unplugged: true}),
		problem("touch-slow", "touch-control", calendar.Impairment{LossPct: 40}))
	tick := func() { d.TickBehavior("rooms_calendar") }
	tick()
	devs, rooms := statuses(d)
	for _, n := range []string{"hq-r2-codec-1", "hq-r2-video-bar-1", "hq-r2-touch-control-1"} {
		if devs[n].Status != "online" {
			t.Errorf("before the window, %s reads %q", n, devs[n].Status)
		}
	}
	if rooms["hq-r2"] == "offline" {
		t.Error("the room reads offline before the window")
	}

	at = open.Add(5 * time.Minute)
	tick()
	devs, rooms = statuses(d)
	if g := devs["hq-r2-codec-1"]; g.Status != "offline" || g.LastSeen != open.Unix() {
		t.Errorf("the unplugged codec reads %q last seen %d, want offline since %d", g.Status, g.LastSeen, open.Unix())
	}
	for _, n := range []string{"hq-r2-video-bar-1", "hq-r2-touch-control-1", "hq-r1-codec-1", "hq-r3-codec-1"} {
		if devs[n].Status != "online" {
			t.Errorf("during the window, %s reads %q: a lossy problem, another role or another room took it offline", n, devs[n].Status)
		}
	}
	if rooms["hq-r2"] != "offline" || rooms["hq-r1"] == "offline" || rooms["hq-r3"] == "offline" {
		t.Errorf("rooms read %v, want only hq-r2 offline", rooms)
	}

	at = open.Add(time.Hour)
	tick()
	devs, rooms = statuses(d)
	if devs["hq-r2-codec-1"].Status != "online" || rooms["hq-r2"] == "offline" {
		t.Errorf("after the window the codec reads %q and the room %q", devs["hq-r2-codec-1"].Status, rooms["hq-r2"])
	}
}

// #2799: the platform asks the arbiter, not the calendar alone, so a device an
// operator unplugged reads offline on its devices list and takes its room's
// status with it, and plugging it back in clears both.
func TestAnOperatorsUnplugReadsOfflineOnThePlatform(t *testing.T) {
	at := time.Date(2026, 11, 26, 10, 5, 0, 0, time.UTC)
	d, _ := uccDeviceWorld(t, &at)
	d.TickBehavior("rooms_calendar")
	arb := d.arbSrc()
	codec := NewDevice("dev-hq-r2-codec-1", "hq-r2-codec-1", net.IPv4(127, 0, 0, 2), "test-profile", 1, map[string]any{})
	arb.Set("operator", map[string]Want{codec.DeviceID: {Impairment: impair.Impairment{Unplugged: true}}})
	if _, errs := arb.Settle([]*Device{codec}); len(errs) != 0 {
		t.Fatal(errs)
	}
	d.TickBehavior("rooms_calendar")
	devs, _ := walkDevices(t, d, 50)
	status := map[string]string{}
	for _, g := range devs {
		status[g.Name] = g.Status
	}
	if status["hq-r2-codec-1"] != "offline" || status["hq-r1-codec-1"] != "online" {
		t.Errorf("after the operator's unplug the codecs read %v", status)
	}
	if !d.roomOffline("hq-r2") || d.roomOffline("hq-r1") {
		t.Error("the unplugged codec did not take its room offline")
	}
	arb.Release("operator")
	if d.roomOffline("hq-r2") {
		t.Error("the room still reads offline after the operator plugged the codec back in")
	}
}
