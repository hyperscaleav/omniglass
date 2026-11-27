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
)

// Room control and switching follow the lab calendar (#2776): on a real
// corporate lab, a conference room's control processor, touch panel, switcher
// and extender (real devices on a real daemon) each read the room's meeting.
// Every reading is taken over the console's fire API off the simulator and
// compared with the calendar the plan's world implies; nothing is read from
// the server's own state. The video bar in the same room is read beside them,
// the switcher routing a source exactly while the bar is in a call.
func TestIntegrationCorporateRoomControlFollowsTheCalendar(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	base, client := startCorporateLab(t, "roomcontrol", systems)

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

	// A conference room's devices by the world's own naming
	// (<system>-<slot>-<n>).
	slots := []string{"control-system", "touch-control", "video-switcher", "display-endpoint", "video-bar"}
	var system string
	var room calendar.Room
	dev := map[string]string{}
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			if system != "" || r.Standard != "divisible-conference" {
				continue
			}
			found := map[string]string{}
			for _, d := range r.Devices {
				for _, slot := range slots {
					if strings.HasPrefix(d.Name, r.System+"-"+slot+"-") {
						found[slot] = d.Name
					}
				}
			}
			if len(found) == len(slots) {
				system, dev = r.System, found
				room = calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}
			}
		}
	}
	if system == "" {
		t.Fatalf("the world has no conference room with %v", slots)
	}

	// The devices tick every 30 and 60 seconds, so a reading may be from a
	// tick before its own start: it must match the calendar at some instant
	// of [start-70s, end].
	agrees := func(name string, read func() (string, error), expect func(time.Time) string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Minute)
		for {
			lo := time.Now().Add(-70 * time.Second)
			got, err := read()
			hi := time.Now()
			if err == nil {
				for at := lo; !at.After(hi); at = at.Add(5 * time.Second) {
					if expect(at) == got {
						t.Logf("%s read %q, the calendar agrees at %s", name, got, at.Format("15:04:05Z"))
						return
					}
				}
				err = fmt.Errorf("read %q, the calendar says %q to %q", got, expect(lo), expect(hi))
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s after five minutes: %v", name, err)
			}
			time.Sleep(10 * time.Second)
		}
	}
	httpJSON := func(slot, path string, into any) error {
		body, err := fireOn(ctx, client, base, system, dev[slot], map[string]string{"surface": "http-api", "method": "GET", "path": path})
		if err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(body), into); err != nil {
			return fmt.Errorf("%s answered %q: %v", path, body, err)
		}
		return nil
	}
	held := func(at time.Time) (calendar.Entry, bool) {
		e, ok := cal.Now(room, at)
		if !ok || e.Meeting.Outcome == calendar.OutcomeNoShow {
			return calendar.Entry{}, false
		}
		return e, true
	}
	shares := func(e calendar.Entry, at time.Time) bool {
		dur := e.End.Sub(e.Start)
		into := at.Sub(e.Start) * 3
		return dur >= 30*time.Minute && into >= dur && into < 2*dur
	}

	agrees("control processor power", func() (string, error) {
		body, err := fireOn(ctx, client, base, system, dev["control-system"], map[string]string{"surface": "nvp", "command": "GET ROOMPOWER"})
		return strings.TrimSpace(body), err
	}, func(at time.Time) string {
		if awakeAt(cal, room, at) {
			return "ROOMPOWER on"
		}
		return "ROOMPOWER standby"
	})

	agrees("touch panel", func() (string, error) {
		var s struct {
			Panel struct {
				Awake bool   `json:"awake"`
				Page  string `json:"page"`
			} `json:"panel"`
		}
		if err := httpJSON("touch-control", "/api/status", &s); err != nil {
			return "", err
		}
		return fmt.Sprintf("awake=%v page=%s", s.Panel.Awake, s.Panel.Page), nil
	}, func(at time.Time) string {
		page := "idle"
		if e, ok := held(at); ok {
			page = "in-call"
			if shares(e, at) {
				page = "sharing"
			}
		}
		return fmt.Sprintf("awake=%v page=%s", awakeAt(cal, room, at), page)
	})

	agrees("switcher", func() (string, error) {
		var s struct {
			Switcher struct {
				Input string `json:"input"`
			} `json:"switcher"`
		}
		if err := httpJSON("video-switcher", "/api/status", &s); err != nil {
			return "", err
		}
		return s.Switcher.Input, nil
	}, func(at time.Time) string {
		if e, ok := held(at); ok {
			if shares(e, at) {
				return "hdmi1"
			}
			return "hdmi2"
		}
		return "dp1"
	})

	agrees("video bar", func() (string, error) {
		var s struct {
			Call struct {
				Active bool `json:"active"`
			} `json:"call"`
		}
		if err := httpJSON("video-bar", "/api/status", &s); err != nil {
			return "", err
		}
		return fmt.Sprint(s.Call.Active), nil
	}, func(at time.Time) string {
		_, ok := held(at)
		return fmt.Sprint(ok)
	})

	agrees("extender", func() (string, error) {
		var s struct {
			Extender struct {
				Link   string `json:"link"`
				Signal string `json:"signal"`
			} `json:"extender"`
		}
		if err := httpJSON("display-endpoint", "/api/status", &s); err != nil {
			return "", err
		}
		return s.Extender.Link + "/" + s.Extender.Signal, nil
	}, func(at time.Time) string {
		if _, ok := held(at); ok {
			return "up/present"
		}
		return "up/absent"
	})
}
