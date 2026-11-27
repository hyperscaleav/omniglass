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

// uccDeviceWorld is a meeting platform account over a world of three rooms and
// the devices in them: a bar, a codec, a controller and a display each, the
// display the one a meeting platform does not list. The ids are the world's.
func uccDeviceWorld(t *testing.T, at *time.Time) (*Device, []world.Listing) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	k := calTestKnobs()
	e := NewEngine(store, profiles.Templates())
	e.SetCalendar(calendar.New(7, func(calendar.Room) (calendar.Knobs, bool) { return k, true }))
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
			r.Devices = append(r.Devices, world.Device{Name: name, ID: "dev-" + name, Product: dv.slot, Platform: dv.platform, Firmware: dv.fw})
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
