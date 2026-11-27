//go:build labintegration

package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/calendar/caltest"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// fireOn fires one operation at the named component of a system through the
// console's simulation:fire API and returns what the simulated device answered.
func fireOn(ctx context.Context, c *http.Client, base, system, component string, op map[string]string) (string, error) {
	get := func(path string, into any) error {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1"+path, nil)
		resp, err := c.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s: %s", path, resp.Status)
		}
		return json.NewDecoder(resp.Body).Decode(into)
	}
	var members struct {
		Members []struct {
			Component   string `json:"component"`
			ComponentID string `json:"component_id"`
		} `json:"members"`
	}
	if err := get("/systems/"+url.PathEscape(system)+"/members", &members); err != nil {
		return "", err
	}
	id := ""
	for _, m := range members.Members {
		if m.Component == component {
			id = m.ComponentID
		}
	}
	if id == "" {
		return "", fmt.Errorf("system %s has no component %s", system, component)
	}
	body, _ := json.Marshal(op)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/components/"+url.PathEscape(id)+"/simulation:fire", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fire on %s: %s %s", component, resp.Status, raw)
	}
	var res struct {
		Body  string `json:"body"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	if res.Error != "" {
		return "", fmt.Errorf("%s", res.Error)
	}
	return res.Body, nil
}

// awakeAt is the lab calendar's own answer for a panel in room at t: awake
// from two minutes before a booked meeting someone will attend (at the start
// of a walk-in) to five minutes after its end.
func awakeAt(cal *calendar.Calendar, room calendar.Room, t time.Time) bool {
	for _, e := range cal.Entries(room, t.Add(-time.Hour), t.Add(time.Hour), calendar.MeetingKinds) {
		if caltest.M(e).Outcome == calendar.OutcomeNoShow {
			continue
		}
		lead := time.Duration(0)
		if caltest.M(e).Origin == calendar.OriginBooked {
			lead = 2 * time.Minute
		}
		if !t.Before(e.Start.Add(-lead)) && t.Before(e.End.Add(5*time.Minute)) {
			return true
		}
	}
	return false
}

// buildingOpenAt is whether the building of room is open at t: the widest
// span of the hours the world's meeting knobs give the rooms of that site and
// building.
func buildingOpenAt(w *world.World, room calendar.Room, t time.Time) bool {
	var span calendar.Hours
	found := false
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			k, ok := w.Meetings[r.Standard]
			if s.Name != room.Site || r.Building != room.Building || !ok {
				continue
			}
			a, b := k.Hours.Span()
			if !found {
				span, found = calendar.Hours{Start: a, End: b}, true
				continue
			}
			span.Start, span.End = min(span.Start, a), max(span.End, b)
		}
	}
	return !found || span.Open(t)
}

// Displays and signage follow the lab calendar (#2768): on a real corporate
// lab, a room's 86-inch display (a real device on a real daemon) is awake
// exactly when the room's meetings say so, and a lobby sign's player is
// playing exactly when its building is open. Both are read over the console's
// fire API, off the simulator, and compared with the calendar the plan's
// world implies; nothing is read from the server's own state. The conference
// room's projector (the Beam, in the divisible-conference standard's
// projection slot) is read the same way as its display.
func TestIntegrationCorporateDisplaysFollowTheCalendar(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	base, client := startCorporateLab(t, "displays", systems)

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

	// The device in a slot of a room of the standard, by the world's own
	// naming (<system>-<slot>-<n>): the conference room's main display and a
	// lobby sign's player, and the conference room's projector.
	find := func(standard, slot string) (system, name string, room calendar.Room) {
		for _, s := range w.Sites {
			for _, r := range s.Rooms {
				for _, d := range r.Devices {
					if r.Standard == standard && strings.HasPrefix(d.Name, r.System+"-"+slot+"-") {
						return r.System, d.Name, calendar.Room{Site: s.Name, Building: r.Building, Room: r.Name}
					}
				}
			}
		}
		t.Fatalf("the world has no %s in a %s room", slot, standard)
		return
	}
	panelSys, panel, panelRoom := find("divisible-conference", "main-display")
	projSys, projector, projRoom := find("divisible-conference", "projection")
	signSys, player, signRoom := find("ds55", "player")

	// The devices tick every 30 and 60 seconds, so a reading may be from a
	// tick before the reading's own start: it must match the calendar at some
	// instant of [start-70s, end].
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

	agrees(func() (string, error) {
		return fireOn(ctx, client, base, panelSys, panel, map[string]string{"surface": "snmp", "oid": "1.3.6.1.4.1.55520.1.1.1.0"})
	}, func(at time.Time) string {
		if awakeAt(cal, panelRoom, at) {
			return "on"
		}
		return "standby"
	})

	agrees(func() (string, error) {
		return fireOn(ctx, client, base, projSys, projector, map[string]string{"surface": "snmp", "oid": "1.3.6.1.4.1.55520.1.1.1.0"})
	}, func(at time.Time) string {
		if awakeAt(cal, projRoom, at) {
			return "on"
		}
		return "standby"
	})

	agrees(func() (string, error) {
		body, err := fireOn(ctx, client, base, signSys, player, map[string]string{"surface": "http-api", "method": "GET", "path": "/api/playback"})
		if err != nil {
			return "", err
		}
		var pb struct {
			State string `json:"state"`
		}
		if err := json.Unmarshal([]byte(body), &pb); err != nil {
			return "", fmt.Errorf("playback answered %q: %v", body, err)
		}
		return pb.State, nil
	}, func(at time.Time) string {
		if buildingOpenAt(w, signRoom, at) {
			return "playing"
		}
		return "stopped"
	})
}
