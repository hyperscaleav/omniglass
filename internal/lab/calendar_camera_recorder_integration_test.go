//go:build labintegration

package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The PTZ camera and the recording service follow the lab calendar (#2775,
// #2757): on a real corporate lab, a conference room's camera reports the
// people the calendar says are in the room, read over the console's fire API
// off the simulator. The fleet's one recording service is an account, not a
// slot of the room: its recordings list is walked in pages and each room's
// recordings land on the room's system, which carry a recording exactly while
// a recorded meeting runs and a queue for the ten minutes after. Both are
// compared with the calendar the plan's world implies.
func TestIntegrationCorporateCameraAndRecorderFollowTheCalendar(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	base, client := startCorporateLab(t, "camrec", systems)

	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := scenario.Resolve(m, cat, systems)
	if err != nil {
		t.Fatal(err)
	}
	w, err := scenario.SiteWorld(m, cat, plan, "hq")
	if err != nil {
		t.Fatal(err)
	}
	cal := w.Calendar()
	if cal == nil {
		t.Fatal("the corporate world holds no calendar")
	}

	var system, camera string
	var room calendar.Room
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			if system != "" || r.Standard != "divisible-conference" {
				continue
			}
			var cam string
			for _, d := range r.Devices {
				switch {
				case strings.HasPrefix(d.Name, r.System+"-camera-"):
					cam = d.Name
				case strings.HasPrefix(d.Name, r.System+"-recorder-"):
					t.Fatalf("room %s places %s: the recording service is the account's one component, not a slot", r.System, d.Name)
				}
			}
			if cam != "" {
				system, camera = r.System, cam
				room = calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}
			}
		}
	}
	if system == "" {
		t.Fatal("the world has no conference room with a camera")
	}

	// The devices tick every 20 and 60 seconds, so a reading may be from a
	// tick before its own start: it must match the calendar at some instant
	// of [start-70s, end].
	agrees := func(read func() (string, error), expect func(time.Time) string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Minute)
		for {
			lo := time.Now().Add(-70 * time.Second)
			got, err := read()
			hi := time.Now()
			if err == nil {
				for at := lo; !at.After(hi); at = at.Add(5 * time.Second) {
					if expect(at) == got {
						t.Logf("read %q, the calendar agrees at %s", got, at.Format("15:04:05Z"))
						return
					}
				}
				err = fmt.Errorf("read %q, the calendar says %q to %q", got, expect(lo), expect(hi))
			}
			if time.Now().After(deadline) {
				t.Fatalf("after five minutes: %v", err)
			}
			time.Sleep(10 * time.Second)
		}
	}
	readJSON := func(dev, path string, into any) (string, error) {
		body, err := fireOn(ctx, client, base, system, dev, map[string]string{"surface": "http-api", "method": "GET", "path": path})
		if err != nil {
			return "", err
		}
		if err := json.Unmarshal([]byte(body), into); err != nil {
			return "", fmt.Errorf("%s answered %q: %v", path, body, err)
		}
		return body, nil
	}

	agrees(func() (string, error) {
		var c struct {
			People int `json:"people"`
		}
		if _, err := readJSON(camera, "/api/camera", &c); err != nil {
			return "", err
		}
		return fmt.Sprint(c.People), nil
	}, func(at time.Time) string {
		if e, ok := cal.Now(room, at, calendar.MeetingKinds); ok && caltest.M(e).Outcome != calendar.OutcomeNoShow {
			return fmt.Sprint(caltest.M(e).Actual)
		}
		return "0"
	})

	// The recording service is one account for the fleet: its world entry
	// covers every system, and a room's recordings in progress and queue are
	// what its list says for that room, fanned to the room's system by the
	// binding the lab's installer put on the account's component.
	var recProfile, conduit string
	for _, d := range m.Services {
		if d.Name.Name == "recording" {
			recProfile = d.Profile.Name
		}
	}
	var covers []world.Cover
	for _, svc := range w.Services {
		if recProfile != "" && svc.Profile == recProfile {
			conduit, covers = svc.Name, svc.Covers
		}
	}
	if conduit == "" || len(covers) != systems {
		t.Fatalf("the headquarters' world holds the recording service %q covering %d rooms, want it covering all %d", conduit, len(covers), systems)
	}
	type series struct {
		Samples []struct {
			TS    time.Time `json:"ts"`
			Value any       `json:"value"`
		} `json:"samples"`
	}
	read := func(path string) series {
		var s series
		deadline := time.Now().Add(6 * time.Minute)
		for {
			if err := getJSON(ctx, client, base+"/api/v1"+path, &s); err == nil && len(s.Samples) > 0 {
				return s
			}
			if time.Now().After(deadline) {
				t.Fatalf("after six minutes %s holds no sample", path)
			}
			time.Sleep(10 * time.Second)
		}
	}
	// A value is the calendar's if the room held it at some instant in the two
	// minutes before it was collected: the service ticks every minute and the
	// poll runs every thirty seconds.
	holds := func(room calendar.Room, ts time.Time, key string, v string) bool {
		for at := ts.Add(-2 * time.Minute); !at.After(ts); at = at.Add(5 * time.Second) {
			if fmt.Sprint(roomRecordings(cal, room, at)[key]) == v {
				return true
			}
		}
		return false
	}
	// The list pages ten rooms at a time (the driver's rule, #2789), sorted by
	// room id: judge the room of the camera and the last room of the list, a
	// value on which can only come of a walk that followed the cursor.
	last := covers[0]
	for _, c := range covers {
		if c.System > last.System {
			last = c
		}
	}
	for _, c := range covers {
		if c.System != system && c.System != last.System {
			continue
		}
		at, ok := w.PlaceSystem(c.System)
		if !ok {
			t.Fatalf("the world places no system %s", c.System)
		}
		for _, m := range []struct{ metric, key string }{{"active-recording-count", "in_progress"}, {"queue-depth", "queue_depth"}} {
			for _, s := range read("/systems/" + url.PathEscape(c.System) + "/metrics/" + m.metric + "/samples?hours=1").Samples {
				if v := fmt.Sprint(s.Value); !holds(at.CalRoom(), s.TS, m.key, v) {
					t.Fatalf("%s carries %s %s at %s on its system; the calendar never says so in the two minutes before", c.System, m.metric, v, s.TS)
				}
			}
		}
	}
}

// roomRecordings is the calendar's own reading of a room's recordings at an
// instant: one in progress while a recorded meeting runs, one queued for the
// ten minutes after it ends.
func roomRecordings(cal *calendar.Calendar, room calendar.Room, at time.Time) map[string]int {
	out := map[string]int{"in_progress": 0, "queue_depth": 0}
	for _, e := range cal.Entries(room, at.Add(-time.Hour), at.Add(time.Hour), calendar.MeetingKinds) {
		if !caltest.M(e).Recorded {
			continue
		}
		if !at.Before(e.Start) && at.Before(e.End) {
			out["in_progress"]++
		}
		if !e.End.After(at) && at.Sub(e.End) <= 10*time.Minute {
			out["queue_depth"]++
		}
	}
	return out
}
