//go:build labintegration

package lab

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/sim"
	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// The scheduling side reads the lab calendar (#2765): on a corporate lab, the
// booking service's pushed booking-count and booking-state for a system, and
// the scheduling panel's collected booking-state in a room, are the booked
// entries of that room's calendar at the instant they were observed. The
// expectation is the plan's own: the same scenario resolved at the same size,
// the world of the site, and the calendar it implies. Nothing is asked of
// Omniglass but what it collected.
func TestIntegrationCorporateSchedulingReadsTheCalendar(t *testing.T) {
	ctx := context.Background()
	const systems = 20
	base, client := startCorporateLab(t, "sched", systems)

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

	// A system the booking service covers, and a room whose panel is placed.
	var covered world.Cover
	for _, s := range w.Services {
		if s.Profile == "booking-service" && len(s.Covers) > 0 {
			covered = s.Covers[0]
			break
		}
	}
	if covered.System == "" {
		t.Fatal("the world has no booking service covering a system")
	}
	var panel, panelRoom string
	for _, s := range w.Sites {
		for _, r := range s.Rooms {
			for _, d := range r.Devices {
				if strings.HasPrefix(d.Profile, "newtron-slate-") && panel == "" {
					panel, panelRoom = d.Name, r.System
				}
			}
		}
	}
	if panel == "" {
		t.Fatal("the world places no scheduling panel")
	}

	get := func(path string, into any) error { return getJSON(ctx, client, base+"/api/v1"+path, into) }
	type series struct {
		Samples []struct {
			TS    time.Time `json:"ts"`
			Value any       `json:"value"`
		} `json:"samples"`
	}
	// read waits until the series holds a sample, then returns them.
	read := func(path string) series {
		var s series
		deadline := time.Now().Add(6 * time.Minute)
		for {
			if err := get(path, &s); err == nil && len(s.Samples) > 0 {
				return s
			}
			if time.Now().After(deadline) {
				t.Fatalf("after six minutes %s holds no sample", path)
			}
			time.Sleep(10 * time.Second)
		}
	}
	// agrees says whether the calendar's reading of the room held the value at
	// some instant in the window the sample could have been computed in: the
	// device reads on its tick, collection lands a little later.
	agrees := func(room calendar.Room, ts time.Time, window time.Duration, key string, value string) bool {
		for at := ts.Add(-window); !at.After(ts); at = at.Add(5 * time.Second) {
			if fmt.Sprint(sim.RoomBookingsAt(cal, room, at)[key]) == value {
				return true
			}
		}
		return false
	}

	at, ok := w.PlaceSystem(covered.System)
	if !ok {
		t.Fatalf("the world places no system %s", covered.System)
	}
	room := at.CalRoom()
	count := read("/systems/" + url.PathEscape(covered.System) + "/metrics/booking-count/samples?hours=1")
	state := read("/systems/" + url.PathEscape(covered.System) + "/properties/booking-state/samples?hours=1")
	for _, s := range count.Samples {
		if v := fmt.Sprint(s.Value); !agrees(room, s.TS, time.Minute, "remaining", v) {
			t.Fatalf("%s pushed booking-count %s at %s; the calendar's booked entries never say so in the minute before", covered.System, v, s.TS)
		}
	}
	for _, s := range state.Samples {
		if v := fmt.Sprint(s.Value); !agrees(room, s.TS, time.Minute, "state", v) {
			t.Fatalf("%s pushed booking-state %s at %s; the calendar's booked entries never say so in the minute before", covered.System, v, s.TS)
		}
	}

	// The room's panel reports its booking, collected from the panel itself.
	pat, ok := w.PlaceSystem(panelRoom)
	if !ok {
		t.Fatalf("the world places no system %s", panelRoom)
	}
	ps := read("/components/" + url.PathEscape(panel) + "/properties/booking-state/samples?hours=1")
	// The oldest sample may be the panel's default, collected before its first
	// tick (a behavior's first tick follows its phase offset): a later one is
	// the calendar's. A history of one is judged as it stands.
	panelJudged := ps.Samples
	if len(panelJudged) > 1 {
		panelJudged = panelJudged[:len(panelJudged)-1] // newest first
	}
	for _, s := range panelJudged {
		if v := fmt.Sprint(s.Value); !agrees(pat.CalRoom(), s.TS, 3*time.Minute, "state", v) {
			t.Fatalf("panel %s reads %s at %s; the calendar's booked entries never say so in the minutes before", panel, v, s.TS)
		}
	}
	// The fleet's one scheduling service is a conduit at the headquarters
	// (ADR-1282): one poll reads every room it books as a record naming its
	// room by the system's name, and each record's values land on the system
	// the lab's installer bound that room id to, the calendar's own booked
	// meetings for it. A room id nobody bound lands in its unbound inbox.
	var conduit string
	var covers []world.Cover
	for _, svc := range w.Services {
		if strings.HasPrefix(svc.Profile, "scheduling-serv") {
			conduit, covers = svc.Name, svc.Covers
		}
	}
	if conduit == "" || len(covers) != systems {
		t.Fatalf("the headquarters' world holds the scheduling service %q covering %d rooms, want it covering all %d", conduit, len(covers), systems)
	}
	pushed := map[string]bool{}
	for _, svc := range w.Services {
		if svc.Profile == "booking-service" {
			for _, c := range svc.Covers {
				pushed[c.System] = true
			}
		}
	}
	// Judge the rooms the booking service does not also push for, so a value
	// on a system can only be the conduit's.
	judged := 0
	for _, c := range covers {
		if pushed[c.System] || judged >= 3 {
			continue
		}
		judged++
		room := calendar.Room{Site: c.Site, Building: c.Building, Room: c.Room}
		fan := read("/systems/" + url.PathEscape(c.System) + "/properties/booking-state/samples?hours=1")
		for _, sm := range fan.Samples {
			if v := fmt.Sprint(sm.Value); !agrees(room, sm.TS, 2*time.Minute, "state", v) {
				t.Fatalf("the conduit fanned booking-state %s to %s at %s; the calendar's booked entries never say so in the two minutes before", v, c.System, sm.TS)
			}
		}
		cnt := read("/systems/" + url.PathEscape(c.System) + "/metrics/booking-count/samples?hours=1")
		for _, sm := range cnt.Samples {
			if v := fmt.Sprint(sm.Value); !agrees(room, sm.TS, 2*time.Minute, "remaining", v) {
				t.Fatalf("the conduit fanned booking-count %s to %s at %s; the calendar never says so in the two minutes before", v, c.System, sm.TS)
			}
		}
	}
	if judged == 0 {
		t.Fatal("every covered room is also pushed for; no room tells the conduit's values apart")
	}
	var unbound struct {
		Identities []struct {
			Key, Value string
		} `json:"identities"`
	}
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if err := get("/components/"+url.PathEscape(conduit)+"/unbound-identities", &unbound); err == nil {
			found := false
			for _, u := range unbound.Identities {
				found = found || (u.Key == "sched-room-id" && u.Value == "unassigned-room")
			}
			if found {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("after five minutes the conduit %s holds %v unbound, want the room nobody bound", conduit, unbound.Identities)
		}
		time.Sleep(10 * time.Second)
	}
	t.Logf("%s: %d count and %d state samples agree with the calendar; panel %s: %d samples agree; %d rooms fan out from %s", covered.System, len(count.Samples), len(state.Samples), panel, len(panelJudged), judged, conduit)
}
