//go:build labintegration

package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
)

// The PTZ camera and the recording service follow the lab calendar (#2775):
// on a real corporate lab, a conference room's camera reports the people the
// calendar says are in the room, and its recorder carries a recording exactly
// while a recorded meeting runs. Both are read over the console's fire API,
// off the simulator, and compared with the calendar the plan's world implies.
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

	var system, camera, recorder string
	var room calendar.Room
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			if system != "" || r.Standard != "divisible-conference" {
				continue
			}
			var cam, rec string
			for _, d := range r.Devices {
				switch {
				case strings.HasPrefix(d.Name, r.System+"-camera-"):
					cam = d.Name
				case strings.HasPrefix(d.Name, r.System+"-recorder-"):
					rec = d.Name
				}
			}
			if cam != "" && rec != "" {
				system, camera, recorder = r.System, cam, rec
				room = calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}
			}
		}
	}
	if system == "" {
		t.Fatal("the world has no conference room with a camera and a recorder")
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

	agrees(func() (string, error) {
		var r struct {
			InProgress int `json:"recordings_in_progress"`
		}
		if _, err := readJSON(recorder, "/api/work", &r); err != nil {
			return "", err
		}
		return fmt.Sprint(r.InProgress), nil
	}, func(at time.Time) string {
		if e, ok := cal.Now(room, at, calendar.MeetingKinds); ok && caltest.M(e).Recorded {
			return "1"
		}
		return "0"
	})
}
