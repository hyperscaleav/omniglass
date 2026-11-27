package seed

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/profiles"
)

// shippedSpec is a seeded driver's spec, parsed as the write path parses it.
func shippedSpec(t *testing.T, id string) *driver.Spec {
	t.Helper()
	sp, err := ShippedDriverSpec(id)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// bake carries a function whole with each emit's lane, as attach does.
func bake(fn string, records string, id *driver.IdentitySpec, emits []driver.Emit) *driver.BakedFunction {
	lanes := map[string]string{"call-state": driver.LaneProperty, "participant-count": driver.LaneMetric, "call-started": driver.LaneEvent, "call-ended": driver.LaneEvent,
		"platform-state": driver.LaneProperty, "firmware-version": driver.LaneProperty}
	b := &driver.BakedFunction{Driver: "test", Function: fn, Records: records, Identity: id}
	for _, e := range emits {
		b.Emits = append(b.Emits, driver.BakedEmit{Name: e.Name, Lane: lanes[e.Name], Extract: e.Extract, Transform: e.Transform, Message: e.Message, Attributes: e.Attributes})
	}
	return b
}

// TestTheShippedUCCDriversReadThePlatform pins the shipped drivers to what
// the simulated UCC platform actually answers (#1282): ucc-rooms reads its
// room list as one identified record per room, each a call state the
// catalog admits and a numeric participant count, and ucc-webhook's
// batched-report listener reads every room of a report the same way.
func TestTheShippedUCCDriversReadThePlatform(t *testing.T) {
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := sim.NewEngine(store, profiles.Templates())
	if err := e.LoadDevices([]sim.DeviceSnapshot{{DeviceID: "p", Hostname: "p", LoopbackIP: "127.0.0.1", ProfileName: "ucc-platform", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/rooms", bytes.NewBuffer(nil))
	req.Header.Set("X-UCC-Token", "ucc-lab-token")
	e.Devices()[0].ServeHTTP(rec, req)
	body := rec.Body.Bytes()

	check := func(what string, fn *driver.BakedFunction, p driver.Payload) {
		t.Helper()
		got, faults := fn.Interpret(p, time.Now())
		if len(faults) > 0 {
			t.Fatalf("%s faulted: %v", what, faults)
		}
		rooms := map[string]bool{}
		for _, s := range got {
			if s.Identity == nil || s.Identity.Key != "ucc-room-id" {
				t.Fatalf("%s: a sample with no ucc-room-id identity: %+v", what, s)
			}
			rooms[s.Identity.Value] = true
			if s.Name == "call-state" && s.Text != "active" && s.Text != "idle" {
				t.Errorf("%s: call-state %q is outside the catalog's enum", what, s.Text)
			}
		}
		if len(rooms) != 5 || !rooms["zr-huddle-north"] || !rooms["zr-boardroom-b"] {
			t.Errorf("%s read rooms %v, want the platform's five", what, rooms)
		}
	}

	poll := shippedSpec(t, "ucc-rooms").Polls[0]
	check("ucc-rooms", bake(poll.Name, poll.Records, poll.Identity, poll.Emits), driver.HTTPPayload(body, &poll.Request))

	var rooms struct {
		Rooms []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(body, &rooms); err != nil {
		t.Fatal(err)
	}
	type rec2 struct {
		RoomID string `json:"room_id"`
		State  string `json:"state"`
	}
	report := struct {
		Event string `json:"event"`
		Rooms []rec2 `json:"rooms"`
	}{Event: "rooms.state"}
	for _, r := range rooms.Rooms {
		// The batched report speaks in the call vocabulary, not the list's.
		report.Rooms = append(report.Rooms, rec2{r.ID, map[string]string{"in-meeting": "in-call"}[r.Status] + map[string]string{"available": "idle", "offline": "idle"}[r.Status]})
	}
	post, _ := json.Marshal(report)
	for _, l := range shippedSpec(t, "ucc-webhook").Listeners {
		if l.Name == "rooms-state" {
			check("ucc-webhook rooms-state", bake(l.Name, l.Records, l.Identity, l.Emits), driver.HTTPPayload(post, nil))
			return
		}
	}
	t.Fatal("ucc-webhook has no rooms-state listener")
}

// TestTheShippedRoomListDeclaresTokenPaging pins ucc-rooms to the platform's
// paging (#2789): its rule names the platform's own cursor field and
// parameter, and the platform answers a page_size and hands back the
// next_page_token of the room that opens the next page, empty on the last.
func TestTheShippedRoomListDeclaresTokenPaging(t *testing.T) {
	pg := shippedSpec(t, "ucc-rooms").Polls[0].Request.Paginate
	if pg == nil || pg.Style != driver.PageCursor || pg.CursorFrom != "next_page_token" || pg.CursorParam != "next_page_token" || pg.SizeParam != "page_size" {
		t.Fatalf("ucc-rooms paginate = %+v", pg)
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := sim.NewEngine(store, profiles.Templates())
	if err := e.LoadDevices([]sim.DeviceSnapshot{{DeviceID: "p", Hostname: "p", LoopbackIP: "127.0.0.1", ProfileName: "ucc-platform", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	token := ""
	for pages := 0; pages < 10; pages++ {
		target := "/api/rooms?page_size=2"
		if token != "" {
			target += "&next_page_token=" + token
		}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("X-UCC-Token", "ucc-lab-token")
		e.Devices()[0].ServeHTTP(rec, req)
		var page struct {
			Rooms []struct {
				ID string `json:"id"`
			} `json:"rooms"`
			Next string `json:"next_page_token"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("%s: %v\n%s", target, err, rec.Body)
		}
		for _, r := range page.Rooms {
			ids = append(ids, r.ID)
		}
		if token = page.Next; token == "" {
			break
		}
	}
	if len(ids) != 5 {
		t.Errorf("paged rooms %v, want the five, each once", ids)
	}
}

// TestTheShippedDriverReadsThePlatformsDevices pins ucc-rooms's second poll to
// what the platform answers at /api/devices (#2790): one identified record per
// device, each by the platform's device id, a platform status the catalog
// admits and the firmware the device reports; and its paging rule names the
// platform's own cursor.
func TestTheShippedDriverReadsThePlatformsDevices(t *testing.T) {
	sp := shippedSpec(t, "ucc-rooms")
	if len(sp.Polls) != 2 || sp.Polls[1].Name != "devices" {
		t.Fatalf("ucc-rooms polls = %d, want its rooms and its devices", len(sp.Polls))
	}
	poll := sp.Polls[1]
	if pg := poll.Request.Paginate; pg == nil || pg.Style != driver.PageCursor || pg.CursorFrom != "next_page_token" || pg.CursorParam != "next_page_token" || pg.SizeParam != "page_size" {
		t.Fatalf("the devices poll paginate = %+v", pg)
	}
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	e := sim.NewEngine(store, profiles.Templates())
	if err := e.LoadDevices([]sim.DeviceSnapshot{{DeviceID: "p", Hostname: "p", LoopbackIP: "127.0.0.1", ProfileName: "ucc-platform", Version: 1}}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	req.Header.Set("X-UCC-Token", "ucc-lab-token")
	e.Devices()[0].ServeHTTP(rec, req)
	fn := bake(poll.Name, poll.Records, poll.Identity, poll.Emits)
	got, faults := fn.Interpret(driver.HTTPPayload(rec.Body.Bytes(), &poll.Request), time.Now())
	if len(faults) > 0 {
		t.Fatalf("the devices poll faulted: %v", faults)
	}
	state := map[string]string{}
	firmware := map[string]string{}
	for _, s := range got {
		if s.Identity == nil || s.Identity.Key != "ucc-device-id" {
			t.Fatalf("a sample with no ucc-device-id identity: %+v", s)
		}
		switch s.Name {
		case "platform-state":
			state[s.Identity.Value] = s.Text
		case "firmware-version":
			firmware[s.Identity.Value] = s.Text
		}
	}
	if len(state) != 2 || state["zd-spare-bar"] != "online" || state["zd-annex-codec"] != "offline" || firmware["zd-spare-bar"] == "" {
		t.Errorf("the devices poll read state %v and firmware %v, want the platform's two held devices", state, firmware)
	}
}
