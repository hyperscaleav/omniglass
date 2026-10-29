package seed

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/sim"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"gopkg.in/yaml.v3"
)

// shippedSpec is a seeded driver's spec, parsed as the write path parses it.
func shippedSpec(t *testing.T, id string) *driver.Spec {
	t.Helper()
	var doc struct {
		Drivers []struct {
			ID   string         `yaml:"id"`
			Spec map[string]any `yaml:"spec"`
		} `yaml:"drivers"`
	}
	if err := yaml.Unmarshal(driversYAML, &doc); err != nil {
		t.Fatal(err)
	}
	for _, d := range doc.Drivers {
		if d.ID == id {
			raw, err := json.Marshal(d.Spec)
			if err != nil {
				t.Fatal(err)
			}
			sp, err := driver.Parse(raw)
			if err != nil {
				t.Fatalf("parse %s: %v", id, err)
			}
			return sp
		}
	}
	t.Fatalf("no shipped driver %s", id)
	return nil
}

// bake carries a function whole with each emit's lane, as attach does.
func bake(fn string, records string, id *driver.IdentitySpec, emits []driver.Emit) *driver.BakedFunction {
	lanes := map[string]string{"call-state": driver.LaneProperty, "participant-count": driver.LaneMetric, "call-started": driver.LaneEvent, "call-ended": driver.LaneEvent}
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
			ID    string `json:"id"`
			State string `json:"state"`
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
		report.Rooms = append(report.Rooms, rec2{r.ID, r.State})
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
