package scenario

import (
	"fmt"
	"hash/fnv"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// calendarSeed is the lab calendar's seed (#2757), derived from the scenario's
// name alone, so a room's meetings do not move when the lab is sized or
// resized.
func calendarSeed(scenario string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("omniglass-lab-calendar:" + scenario))
	return int64(h.Sum64() >> 1)
}

// maxComponentNameLen is the longest name the platform takes for an entity.
const maxComponentNameLen = 100

// ComponentName is the name the world gives the n-th (from 1) component that
// fills a slot of a system: "<system>-<slot>-<n>". A system's name is unique
// across a plan, so the name is too, which is what lets a simulator join a
// pulled device to the world by an exact name lookup. The world owns the name
// and the lab's seed onboards the component under it (#2773); the platform's
// own name generator is not asked.
func ComponentName(system, slot string, n int) string {
	return fmt.Sprintf("%s-%s-%d", system, slot, n)
}

// SiteWorld is the world one site's simulator holds, resolved from the plan
// (#2773): the site's rooms with their standard and capacity, each room's
// planned devices by name and the simulator profile that serves them, the
// services (webhook senders and token pushers) with the rooms each speaks
// for, the meeting knobs per standard and the calendar seed. Pure: the same
// manifest, catalog and plan give the same world, and it reads nothing from a
// server or a database.
func SiteWorld(m *Manifest, cat Catalog, plan *Plan, site string) (*world.World, error) {
	for _, sp := range plan.Sites {
		if sp.Name == site {
			return siteWorld(m, cat, plan, sp)
		}
	}
	return nil, fmt.Errorf("scenario %s: the plan has no site %q", plan.Scenario, site)
}

// SiteWorlds is each site's world by the name of the outpost it is written
// for, in plan order.
func SiteWorlds(m *Manifest, cat Catalog, plan *Plan) (map[string]*world.World, error) {
	out := make(map[string]*world.World, len(plan.Sites))
	for _, sp := range plan.Sites {
		w, err := siteWorld(m, cat, plan, sp)
		if err != nil {
			return nil, err
		}
		out[w.Outpost] = w
	}
	return out, nil
}

func siteWorld(m *Manifest, cat Catalog, plan *Plan, sp SitePlan) (*world.World, error) {
	site := sp.Name
	w := &world.World{
		Version: world.Version, Scenario: plan.Scenario, Outpost: SiteOutpost(site),
		CalendarSeed: calendarSeed(plan.Scenario),
	}
	if m.Calendar != nil {
		for _, sm := range m.Calendar.Meetings {
			if w.Meetings == nil {
				w.Meetings = map[string]calendar.Knobs{}
			}
			w.Meetings[sm.Standard.Name] = sm.Knobs
		}
	}
	profileOf := func(product string) string {
		if cat.SimProfile == nil {
			return ""
		}
		p, _ := cat.SimProfile(product)
		return p
	}
	names := map[string]bool{}
	claim := func(name string) error {
		switch {
		case len(name) > maxComponentNameLen:
			return fmt.Errorf("scenario %s: the world's name %q is over %d characters, which the platform refuses", plan.Scenario, name, maxComponentNameLen)
		case names[name]:
			return fmt.Errorf("scenario %s: the world names %q twice", plan.Scenario, name)
		}
		names[name] = true
		return nil
	}
	rooms := make([]world.Room, 0, len(sp.Systems))
	cover := map[string]world.Cover{}
	for _, sys := range sp.Systems {
		r := world.Room{Name: sys.Room, Building: sys.Building, Standard: sys.Standard, System: sys.Name}
		if k, ok := m.MeetingKnobs(sys.Standard); ok {
			r.Capacity = k.Attendance.Max
		}
		for _, fill := range sys.Slots {
			for i := 1; i <= fill.Count; i++ {
				if err := claim(ComponentName(sys.Name, fill.Slot, i)); err != nil {
					return nil, err
				}
				r.Devices = append(r.Devices, world.Device{
					Name: ComponentName(sys.Name, fill.Slot, i), Product: fill.Product, Profile: profileOf(fill.Product),
				})
			}
		}
		rooms = append(rooms, r)
		cover[sys.Name] = world.Cover{System: sys.Name, Site: site, Building: sys.Building, Room: sys.Room}
	}
	w.Sites = []world.Site{{Name: site, Rooms: rooms}}
	for _, p := range sp.Pushers {
		// A sender is a component named for its pusher; a token pusher is a
		// device of its own name apart from the fleet.
		if p.Webhook {
			if err := claim(p.Name); err != nil {
				return nil, err
			}
		}
		svc := world.Service{Name: p.Name, Profile: p.Profile, Webhook: p.Webhook}
		for _, s := range p.Systems {
			c, ok := cover[s]
			if !ok {
				return nil, fmt.Errorf("scenario %s: pusher %s covers %q, which is no system of site %s", plan.Scenario, p.Name, s, site)
			}
			svc.Covers = append(svc.Covers, c)
		}
		w.Services = append(w.Services, svc)
	}
	return w, nil
}
