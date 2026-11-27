//go:build labintegration

package lab

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/labcheck"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The fleet's one meeting platform is a conduit that speaks for every room and
// lists every device (#2790, ADR-1282): on a corporate lab, one poll of its
// paged room list lands each room's call state and headcount on the system
// the lab's installer bound that room's id to, and one poll of its paged
// device list lands each device's platform status and firmware on the component
// its id is bound to, all of it the calendar's and the world's own: the plan's
// scenario resolved at the same size, the world of the headquarters, and the
// calendar it implies. A signed meeting-started post lands on the system its
// room id names, and a room id and a device id nobody bound land in the
// conduit's unbound inbox.
func TestIntegrationCorporateMeetingPlatformFansOut(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	base, client := startCorporateLab(t, "meeting", systems)

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
	var conduit string
	var covers []string
	for _, svc := range w.Services {
		if svc.Webhook && svc.Profile == "ucc-platform" {
			conduit = svc.Name
			for _, c := range svc.Covers {
				covers = append(covers, c.System)
			}
		}
	}
	if conduit == "" || len(covers) != systems {
		t.Fatalf("the headquarters' world holds the meeting platform %q covering %d rooms, want it covering all %d", conduit, len(covers), systems)
	}
	listed := w.Listed(conduit)
	if len(listed) <= 50 {
		t.Fatalf("the world lists %d devices, want more than the fifty a page holds so the poll must walk", len(listed))
	}
	sort.Strings(covers)

	get := func(path string, into any) error { return getJSON(ctx, client, base+"/api/v1"+path, into) }
	type series struct {
		Samples []struct {
			TS    time.Time `json:"ts"`
			Value any       `json:"value"`
		} `json:"samples"`
	}
	read := func(path string) series {
		var s series
		deadline := time.Now().Add(8 * time.Minute)
		for {
			if err := get(path, &s); err == nil && len(s.Samples) > 0 {
				return s
			}
			if time.Now().After(deadline) {
				t.Fatalf("after eight minutes %s holds no sample", path)
			}
			time.Sleep(10 * time.Second)
		}
	}
	// occupied says whether the calendar had somebody in the room at some
	// instant in the window before ts, and the most who could have come then.
	occupied := func(room calendar.Room, ts time.Time, window time.Duration) (could, couldNot bool, most int) {
		for at := ts.Add(-window); !at.After(ts); at = at.Add(5 * time.Second) {
			if e, ok := cal.Now(room, at); ok && e.Meeting.Outcome != calendar.OutcomeNoShow {
				could = true
				most = max(most, e.Meeting.Actual)
			} else {
				couldNot = true
			}
		}
		return
	}

	// Rooms: each judged system carries the platform's call state and headcount
	// as the calendar had it. The first rooms and the last, so the list's later
	// pages (ten rooms a page) are walked too.
	var judgedRooms []string
	judgedRooms = append(judgedRooms, covers[:3]...)
	judgedRooms = append(judgedRooms, covers[len(covers)-3:]...)
	states := 0
	for _, sys := range judgedRooms {
		at, ok := w.PlaceSystem(sys)
		if !ok {
			t.Fatalf("the world places no system %s", sys)
		}
		room := at.CalRoom()
		cs := read("/systems/" + url.PathEscape(sys) + "/properties/call-state/samples?hours=1")
		for _, s := range cs.Samples {
			could, couldNot, _ := occupied(room, s.TS, 2*time.Minute)
			switch v := fmt.Sprint(s.Value); {
			case v == "active" && !could:
				t.Fatalf("the platform fanned call-state active to %s at %s with nobody in the room on the calendar in the two minutes before", sys, s.TS)
			case v == "idle" && !couldNot:
				t.Fatalf("the platform fanned call-state idle to %s at %s while the calendar had the room in a meeting throughout the two minutes before", sys, s.TS)
			}
			states++
		}
		pc := read("/systems/" + url.PathEscape(sys) + "/metrics/participant-count/samples?hours=1")
		for _, s := range pc.Samples {
			_, _, most := occupied(room, s.TS, 2*time.Minute)
			if v, ok := s.Value.(float64); !ok || v < 0 || int(v) > most {
				t.Fatalf("the platform fanned participant-count %v to %s at %s, at most %d came", s.Value, sys, s.TS, most)
			}
		}
	}

	// Devices: each judged device's component carries the platform's status
	// for it (online) and the firmware the world says it reports, through the
	// fan-out, from the first page of devices and the last.
	var judgedDevices = append(append([]string{}, idsOf(listed[:3])...), idsOf(listed[len(listed)-3:])...)
	byID := map[string]string{}
	firmware := map[string]string{}
	for _, l := range listed {
		byID[l.ID], firmware[l.ID] = l.Name, l.Firmware
	}
	for _, id := range judgedDevices {
		name := byID[id]
		ps := read("/components/" + url.PathEscape(name) + "/properties/platform-state/samples?hours=1")
		for _, s := range ps.Samples {
			if v := fmt.Sprint(s.Value); v != "online" {
				t.Fatalf("the platform fanned platform-state %s to %s at %s, want online (nothing in the lab has it unplugged)", v, name, s.TS)
			}
		}
		fw := read("/components/" + url.PathEscape(name) + "/properties/firmware-version/samples?hours=1")
		seen := false
		for _, s := range fw.Samples {
			seen = seen || fmt.Sprint(s.Value) == firmware[id]
		}
		if firmware[id] != "" && !seen {
			t.Fatalf("%s carries firmware samples %v, none the world's %q", name, fw.Samples, firmware[id])
		}
	}

	// A signed meeting-started post lands on the system its room id names: fire
	// the platform's own route in a room, and the system holds a call-started
	// event naming the meeting the route started (only a post of that meeting
	// can carry it).
	quiet := covers[len(covers)/2]
	var comp struct {
		ID string `json:"id"`
	}
	if err := get("/components/"+url.PathEscape(conduit), &comp); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"room":%q,"id":"m-lab-2790","participants":3}`, quiet)
	deadline := time.Now().Add(5 * time.Minute)
	for {
		// The platform is told of its webhook endpoint when the lab subscribes it; until
		// then a start posts nowhere.
		if _, err := fireOnComponent(ctx, client, base, comp.ID, labcheck.Fire{Surface: "http-api", Method: "POST", Path: "/api/rooms/meetings/start", Body: body}); err != nil && time.Now().After(deadline) {
			t.Fatalf("firing a meeting start in %s: %v", quiet, err)
		}
		var evs struct {
			Events []map[string]any `json:"events"`
		}
		if err := get("/systems/"+url.PathEscape(quiet)+"/events?limit=200", &evs); err == nil {
			found := false
			for _, e := range evs.Events {
				found = found || (e["key"] == "call-started" && strings.Contains(fmt.Sprint(e), "m-lab-2790"))
			}
			if found {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("after five minutes %s holds no call-started event for the meeting the platform started", quiet)
		}
		time.Sleep(15 * time.Second)
	}

	// An unbound room id and an unbound device id land in the conduit's unbound inbox.
	var unbound struct {
		Identities []struct {
			Key, Value string
		} `json:"identities"`
	}
	deadline = time.Now().Add(6 * time.Minute)
	for {
		if err := get("/components/"+url.PathEscape(conduit)+"/unbound-identities", &unbound); err == nil {
			room, device := false, false
			for _, u := range unbound.Identities {
				room = room || (u.Key == "ucc-room-id" && strings.HasPrefix(u.Value, "zr-"))
				device = device || (u.Key == "ucc-device-id" && strings.HasPrefix(u.Value, "zd-"))
			}
			if room && device {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("after six minutes the conduit %s holds %v unbound, want a room and a device nobody bound", conduit, unbound.Identities)
		}
		time.Sleep(10 * time.Second)
	}
	t.Logf("%s: %d call-state samples agree with the calendar over %d rooms; %d of %d devices judged; a started post landed on %s", conduit, states, len(judgedRooms), len(judgedDevices), len(listed), quiet)
}

func idsOf(ls []world.Listing) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.ID
	}
	return out
}
