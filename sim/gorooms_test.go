//go:build linux

package sim

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/ingress"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/profile"
	"github.com/hyperscaleav/omniglass/sim/spec"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// A Go family that speaks for rooms, sends a signed webhook, names an event on
// its stream and pushes a batch (#2836) is served by the engine as a profile that
// does the same is: the rooms it reads are the world's, read once a second
// however often they are asked for, its post is signed as a sender's is, its event
// reaches the stream, and its push is the body its Go func renders.

const roomsFamily = "wiretest-rooms"

// roomsSpec is the test family: a service that lists its rooms' bookings, posts
// and reports on request, and pushes one number per system.
func roomsSpec(target string) spec.Spec {
	return spec.Spec{
		Name: roomsFamily, Identity: spec.Identity{Manufacturer: "Wire", Model: "Rooms", DeviceType: "cloud-service"},
		Models: []spec.Model{{Product: "wt-rooms-1"}},
		Interfaces: []profile.Interface{{
			Name: "http", Protocol: "http", Port: 80,
			Events: profile.Events{Path: "/events", RetryMS: 1000},
		}},
		State: map[string]any{"status": map[string]any{"device": map[string]any{"boot_epoch": 0}}},
		HTTP: []spec.HTTPRoute{{Match: "POST /go", Handle: func(d spec.Device, r spec.HTTPRequest) spec.HTTPResponse {
			spec.Send(d, spec.Post{URL: target, Body: []byte(`{"event":"go"}`), Headers: map[string]string{"X-Wire-Event": "go"},
				Sign: &spec.Signing{Scheme: "hmac", Secret: "wire-key", Header: "X-UCC-Signature", Prefix: "sha256=", Timestamp: "X-UCC-Timestamp", Delivery: "X-UCC-Delivery"}})
			d.Publish(spec.Event{Name: "went", Data: `{"ok":true}`})
			return spec.JSON(map[string]bool{"ok": true})
		}}},
		Push: &spec.PushFace{Every: 30 * time.Second, Body: func(d spec.Device, owner string) ([]byte, error) {
			room, _ := spec.RoomsOf(d).Place(owner)
			return json.Marshal(map[string]any{"owner": owner, "today": spec.RoomsOf(d).Bookings(room).Today})
		}},
	}
}

func roomsDevice(t *testing.T, target string, w *world.World, cal *calendar.Calendar, room calendar.Room) (*Device, *time.Time) {
	t.Helper()
	e := NewEngineOver(shippedCatalog(t, roomsSpec(target)), nil)
	e.SetPortMap(EphemeralPorts)
	t.Cleanup(e.StopAll)
	e.SetWorld(w)
	e.SetCalendar(cal)
	snap := DeviceSnapshot{DeviceID: "svc-1", Hostname: "svc-1", LoopbackIP: "127.0.0.1", ProfileName: roomsFamily, Version: 1,
		SiteCode: room.Site, Building: room.Building, Room: room.Room}
	if err := e.LoadDevices([]DeviceSnapshot{snap}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	at := parityAt("10:05:00")
	d.SetClock(pinned(&at))
	return d, &at
}

// The rooms a service speaks for are the world's covers; with no world it speaks
// for the room it sits in, and with no room for none.
func TestAFamilyReadsTheRoomsItSpeaksFor(t *testing.T) {
	w, cal := parityWorld("svc-1", roomsFamily, parityCovers())
	d, _ := roomsDevice(t, "", w, cal, parityRoom)
	r := spec.RoomsOf(goDevice{d})
	served := r.Served()
	if len(served) != 3 || served[0].System != "sys-a" || served[0].Room != "tower-1-01" || served[0].Label != "Room tower-1-01" {
		t.Fatalf("served = %+v, want the world's three covers", served)
	}
	if len(r.Covered()) != 3 {
		t.Errorf("covered = %+v", r.Covered())
	}
	if room, ok := r.Place("sys-b"); !ok || room.Room != "tower-1-02" {
		t.Errorf("sys-b is placed at %+v (%v)", room, ok)
	}

	alone, _ := roomsDevice(t, "", nil, parityCal(), parityRoom)
	if s := spec.RoomsOf(goDevice{alone}).Served(); len(s) != 1 || s[0].Room != parityRoom.Room || s[0].System != "" {
		t.Errorf("a device in a room with no world serves %+v, want its own room", s)
	}
	if c := spec.RoomsOf(goDevice{alone}).Covered(); len(c) != 0 {
		t.Errorf("a device with no world covers %+v", c)
	}
	nowhere, _ := roomsDevice(t, "", nil, nil, calendar.Room{})
	if s := spec.RoomsOf(goDevice{nowhere}).Served(); len(s) != 0 {
		t.Errorf("a device in no room serves %+v", s)
	}
}

// A room's bookings and phase are read once a second however often they are
// asked for, and agree with the template functions' reading.
func TestARoomIsReadOnceASecondForAFamily(t *testing.T) {
	w, cal := parityWorld("svc-1", roomsFamily, parityCovers())
	d, at := roomsDevice(t, "", w, cal, parityRoom)
	r := spec.RoomsOf(goDevice{d})
	room := calendar.Room{Site: "hq", Building: "tower", Room: "tower-1-01"}
	before := d.roomMemo().books.Load()
	first := r.Bookings(room)
	for range 5 {
		if got := r.Bookings(room); got.Today != first.Today || got.State != first.State {
			t.Fatalf("a second read gave %+v, want %+v", got, first)
		}
	}
	if n := d.roomMemo().books.Load() - before; n != 1 {
		t.Errorf("six reads in a second computed %d bookings, want 1", n)
	}
	*at = at.Add(time.Second)
	r.Bookings(room)
	if n := d.roomMemo().books.Load() - before; n != 2 {
		t.Errorf("a read the next second computed %d in all, want 2", n)
	}
	want := RoomBookingsAt(cal, room, *at)
	if got := r.Bookings(room); got.Today != want["today"] || got.Remaining != want["remaining"] || got.State != want["state"] {
		t.Errorf("bookings = %+v, the template function reads %v", got, want)
	}
	if st := r.State(room); !st.HasCalendar {
		t.Errorf("state = %+v, want a room with a calendar", st)
	}
	held := 0
	for tm := parityAt("07:00:00"); !tm.After(parityAt("15:00:00")); tm = tm.Add(5 * time.Minute) {
		*at = tm
		got, ok := r.MeetingNow(room)
		want, wantOK := cal.Now(room, tm, calendar.MeetingKinds)
		if ok != wantOK || (ok && got.Start != want.Start) {
			t.Fatalf("at %s the family reads the meeting %v (%v), the calendar %v (%v)", tm.Format("15:04"), got.Start, ok, want.Start, wantOK)
		}
		if ok {
			held++
		}
	}
	if held == 0 {
		t.Error("the pinned day held no meeting to compare")
	}
}

// A family's post reaches its target signed as the server checks it, and its
// event reaches the stream, numbered with the changes.
func TestAFamilySendsASignedPostAndNamesAnEvent(t *testing.T) {
	var c caught
	d, _ := roomsDevice(t, c.server(t).URL, nil, nil, calendar.Room{})
	d.SetEvents(profile.Events{Path: "/events"}) // as the face does when it starts
	rec := wireRequest(t, d, wireStep{Name: "go", Method: "POST", Path: "/go", Body: `{}`})
	if rec.Status != http.StatusOK {
		t.Fatalf("POST /go = %d %s", rec.Status, rec.Response)
	}
	p := c.wait(t, 1)[0]
	spec := &driver.WebhookSpec{Signature: &driver.Signature{Header: "X-UCC-Signature", Prefix: "sha256=", Timestamp: "X-UCC-Timestamp"}, Delivery: "X-UCC-Delivery"}
	if err := ingress.Authenticate(driver.WebhookHMAC, spec, ingress.Credential{Key: "wire-key"}, p, time.Now()); err != nil {
		t.Fatalf("the server's check refused the post: %v", err)
	}
	if string(p.Body) != `{"event":"go"}` || p.Header.Get("X-Wire-Event") != "go" {
		t.Errorf("post = %s %v", p.Body, p.Header)
	}
	d.events.mu.Lock()
	defer d.events.mu.Unlock()
	if len(d.events.ring) != 1 || d.events.ring[0].name != "went" || d.events.ring[0].data != `{"ok":true}` {
		t.Errorf("the stream holds %+v, want the one named event", d.events.ring)
	}
}

// A device not yet subscribed (no target) sends nothing.
func TestAFamilyWithNoTargetSendsNothing(t *testing.T) {
	d, _ := roomsDevice(t, "", nil, nil, calendar.Room{})
	wireRequest(t, d, wireStep{Name: "go", Method: "POST", Path: "/go", Body: `{}`})
	time.Sleep(50 * time.Millisecond) // a post that was sent would be here by now
}

// A family's push is the body its Go func renders, for each system, valid JSON.
func TestAFamilyPushesTheBodyItRenders(t *testing.T) {
	w, cal := parityWorld("svc-1", roomsFamily, parityCovers())
	d, _ := roomsDevice(t, "", w, cal, parityRoom)
	if d.PushEvery() != 30*time.Second {
		t.Fatalf("push every %v, want 30s", d.PushEvery())
	}
	b, err := d.RenderPush("sys-a")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Owner string
		Today int
	}
	if err := json.Unmarshal(b, &got); err != nil || got.Owner != "sys-a" || got.Today == 0 {
		t.Errorf("push = %s (%v), want sys-a's meetings today", b, err)
	}
}
