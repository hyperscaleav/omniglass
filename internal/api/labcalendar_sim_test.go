package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/lab"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/simtest/shipped"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The lab test of GET /lab/calendar (#2770): the world file a lab writes is
// read twice, by the server for the route and by the simulator for a device in
// the room. Over a simulated day, the people the room's camera frames each
// minute equal the people the route's entry for that minute says were there.
func TestLabCalendarRouteMatchesWhatTheRoomsDeviceActsOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, seededDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-a"}, all, all); err != nil {
		t.Fatal(err)
	}

	// The lab's seed writes the world file; the server reads it for the route
	// and the simulator loads it for its devices.
	dir := filepath.Join(t.TempDir(), "world")
	w := labCalWorld()
	w.Sites[0].Rooms[0].Devices = []world.Device{{Name: "sys-a-cam-1", Product: "cam", Profile: "kestrel-gaze-p2"}}
	if _, err := lab.WriteWorlds(dir, map[string]*world.World{"o": w}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.NewHandler(gw, api.WithLab(true), api.WithLabCalendar(api.WorldDirCalendar(dir))))
	defer srv.Close()
	c := &apiClient{t: t, ctx: ctx, base: srv.URL}

	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	var got labCalendarBody
	path := "/lab/calendar?" + url.Values{"start": {day.Format(time.RFC3339)}, "end": {day.Add(24 * time.Hour).Format(time.RFC3339)}, "system": {"sys-a"}}.Encode()
	if err := json.Unmarshal(c.do(tok, http.MethodGet, path, nil, http.StatusOK), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) == 0 {
		t.Fatal("the route returned no entries for the room")
	}

	sw, err := world.Load(filepath.Join(dir, "o.json"))
	if err != nil {
		t.Fatal(err)
	}
	e := shipped.Engine(t)
	e.SetCalendar(sw.Calendar())
	e.SetWorld(sw)
	if err := e.LoadDevices([]sim.DeviceSnapshot{{DeviceID: "sys-a-cam-1", Hostname: "sys-a-cam-1", LoopbackIP: "127.0.0.1",
		ProfileName: "kestrel-gaze-p2", Version: 1, SiteCode: "hq", Building: "tower", Room: "r101"}}); err != nil {
		t.Fatal(err)
	}
	d := e.Devices()[0]
	at := day
	d.SetClock(func() time.Time { return at })

	framed := 0
	for at = day; at.Before(day.Add(24 * time.Hour)); at = at.Add(time.Minute) {
		d.TickBehavior("room_meeting")
		people := 0
		for _, en := range got.Entries {
			if !at.Before(en.Start) && at.Before(en.End) && en.Payload["outcome"] != "no-show" {
				people = int(en.Payload["actual"].(float64))
			}
		}
		if people > 0 {
			framed++
		}
		if have := fmt.Sprint(d.ReadPath("status.camera.people")); have != fmt.Sprint(people) {
			t.Fatalf("%s: the camera frames %s, the route's entries say %d", at.Format("15:04"), have, people)
		}
	}
	if framed == 0 {
		t.Fatal("no minute of the day had anyone in the room; the test proved nothing")
	}
}
