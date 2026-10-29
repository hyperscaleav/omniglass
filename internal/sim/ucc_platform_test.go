package sim

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/ingress"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
)

// The account-wide UCC platform speaks for many rooms (#1282): one API lists
// every room with its meeting state, and its webhook posts name the room by
// id, singly at a meeting's start and end and all at once in a batched
// report, each signed as the server checks it. A room id the account has not
// seen joins it, which is how the lab hears an unbound room.
func uccPlatform(t *testing.T) *Device {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	e := NewEngine(store, profiles.Templates())
	if err := e.LoadDevices([]DeviceSnapshot{{DeviceID: "ucc-plat-1", Hostname: "ucc-plat-1", LoopbackIP: "127.0.0.1", ProfileName: "ucc-platform", Version: 1}}); err != nil {
		t.Fatalf("load device: %v", err)
	}
	devs := e.Devices()
	if len(devs) != 1 {
		t.Fatalf("engine built %d devices from the ucc-platform profile, want one", len(devs))
	}
	return devs[0]
}

func uccCall(t *testing.T, d *Device, method, path, body string) []byte {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("X-UCC-Token", "ucc-lab-token")
	req.Header.Set("Content-Type", "application/json")
	d.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body)
	}
	return rec.Body.Bytes()
}

type uccRooms struct {
	Rooms []struct {
		ID, Name, State, Meeting string
		Participants             int
	}
}

func readRooms(t *testing.T, d *Device) map[string]string {
	t.Helper()
	var r uccRooms
	if err := json.Unmarshal(uccCall(t, d, http.MethodGet, "/api/rooms", ""), &r); err != nil {
		t.Fatalf("the room list is not JSON: %v", err)
	}
	out := map[string]string{}
	for _, room := range r.Rooms {
		out[room.ID] = room.State
	}
	return out
}

func TestTheUCCPlatformSpeaksForManyRooms(t *testing.T) {
	d := uccPlatform(t)
	rooms := readRooms(t, d)
	if len(rooms) != 5 || rooms["zr-huddle-north"] != "in-call" || rooms["zr-boardroom-a"] != "idle" || rooms["zr-boardroom-b"] != "in-call" {
		t.Fatalf("rooms = %v, want the five seeded rooms with their state", rooms)
	}

	var c caught
	sub, _ := json.Marshal(map[string]string{"url": c.server(t).URL, "scheme": "hmac", "secret": "ucc-lab-signing-key"})
	uccCall(t, d, http.MethodPost, "/api/webhooks", string(sub))

	// A meeting in a room the account has not seen: it joins the list, and
	// the post names it.
	uccCall(t, d, http.MethodPost, "/api/rooms/meetings/start", `{"room":"zr-annex-2","id":"m-3001","participants":5}`)
	if got := readRooms(t, d); got["zr-annex-2"] != "in-call" || len(got) != 6 {
		t.Fatalf("after the start, rooms = %v, want zr-annex-2 in a call", got)
	}
	uccCall(t, d, http.MethodPost, "/api/rooms/meetings/end", `{"room":"zr-huddle-north"}`)
	uccCall(t, d, http.MethodPost, "/api/rooms/report", `{}`)
	posts := c.wait(t, 3)

	type post struct {
		Event, RoomID, Meeting string
		Rooms                  []struct{ RoomID, State string }
	}
	read := func(p ingress.Post) post {
		var raw struct {
			Event   string `json:"event"`
			RoomID  string `json:"room_id"`
			Meeting string `json:"meeting"`
			Rooms   []struct {
				RoomID string `json:"room_id"`
				State  string `json:"state"`
			} `json:"rooms"`
		}
		if err := json.Unmarshal(p.Body, &raw); err != nil {
			t.Fatalf("a post is not JSON: %v: %s", err, p.Body)
		}
		if err := ingress.Authenticate(driver.WebhookHMAC, uccSpec(), ingress.Credential{Key: "ucc-lab-signing-key"}, p, time.Now()); err != nil {
			t.Fatalf("the server's check refused the post %s: %v", p.Body, err)
		}
		if p.Header.Get("X-UCC-Event") != raw.Event {
			t.Errorf("post %s carries event header %q", p.Body, p.Header.Get("X-UCC-Event"))
		}
		out := post{Event: raw.Event, RoomID: raw.RoomID, Meeting: raw.Meeting}
		for _, r := range raw.Rooms {
			out.Rooms = append(out.Rooms, struct{ RoomID, State string }{r.RoomID, r.State})
		}
		return out
	}
	byEvent := map[string]post{}
	for _, p := range posts {
		r := read(p)
		byEvent[r.Event] = r
	}
	start, end, report := byEvent["room.meeting.started"], byEvent["room.meeting.ended"], byEvent["rooms.state"]
	if start.Event != "room.meeting.started" || start.RoomID != "zr-annex-2" || start.Meeting != "m-3001" {
		t.Errorf("start post = %+v", start)
	}
	if end.Event != "room.meeting.ended" || end.RoomID != "zr-huddle-north" {
		t.Errorf("end post = %+v, want it to name the room", end)
	}
	if report.Event != "rooms.state" || len(report.Rooms) != 6 {
		t.Fatalf("report = %+v, want every room on the account", report)
	}
	states := map[string]string{}
	for _, r := range report.Rooms {
		states[r.RoomID] = r.State
	}
	if states["zr-huddle-north"] != "idle" || states["zr-annex-2"] != "in-call" {
		t.Errorf("report states = %v", states)
	}

	// A room id that is not a name is refused before it touches state.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rooms/meetings/start", bytes.NewBufferString(`{"room":"a.b","id":"m-1"}`))
	req.Header.Set("X-UCC-Token", "ucc-lab-token")
	d.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("a room id with a dot was accepted; it would write into another path")
	}
	if _, ok := readRooms(t, d)["a"]; ok {
		t.Error("a refused room id still wrote state")
	}
}

// TestARefusedStartLeavesTheRoomListReadable: a start whose meeting id or
// participant count is refused writes no room a read cannot render, so the
// room list stays valid JSON and the poll keeps every room.
func TestARefusedStartLeavesTheRoomListReadable(t *testing.T) {
	d := uccPlatform(t)
	for _, body := range []string{`{"room":"zr-new"}`, `{"room":"zr-new","id":"m 1"}`, `{"room":"zr-new","id":"m-1","participants":1000}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/rooms/meetings/start", bytes.NewBufferString(body))
		req.Header.Set("X-UCC-Token", "ucc-lab-token")
		d.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Errorf("start %s was accepted", body)
		}
		raw := uccCall(t, d, http.MethodGet, "/api/rooms", "")
		if !json.Valid(raw) {
			t.Fatalf("after start %s the room list is not JSON: %s", body, raw)
		}
		if readRooms(t, d)["zr-new"] == "in-call" {
			t.Errorf("a refused start %s left the room reading in a call", body)
		}
	}
}
