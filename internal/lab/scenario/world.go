package scenario

import (
	"fmt"
	"hash/fnv"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/fleet"
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

// nameMinter predicts the names the platform mints for components (ADR-0097):
// "<stem>-<n>", n the lowest free ordinal among a placement's components of
// that stem. A plan seeds its components in plan order, so counting in that
// order gives the names the seed's platform will mint.
type nameMinter struct{ next map[[2]string]int }

func (n *nameMinter) mint(bucket, stem string) string {
	if n.next == nil {
		n.next = map[[2]string]int{}
	}
	k := [2]string{bucket, stem}
	n.next[k]++
	return fmt.Sprintf("%s-%d", stem, n.next[k])
}

// SiteWorld is the world one site's simulator holds, resolved from the plan
// (#2773): the site's rooms with their standard and capacity, each room's
// planned devices by the name the platform will mint and the simulator
// profile that serves them, the services (webhook senders and token pushers)
// with the rooms each speaks for, the meeting knobs per standard and the
// calendar seed. Pure: the same manifest, catalog and plan give the same
// world, and it reads nothing from a server or a database.
func SiteWorld(m *Manifest, cat Catalog, plan *Plan, site string) (*world.World, error) {
	ws, err := SiteWorlds(m, cat, plan)
	if err != nil {
		return nil, err
	}
	if w, ok := ws[SiteOutpost(site)]; ok {
		return w, nil
	}
	return nil, fmt.Errorf("scenario %s: the plan has no site %q", plan.Scenario, site)
}

// SiteWorlds is each site's world by the name of the outpost it is written
// for. The simulated block's addresses run on across sites in plan order, so
// the worlds are resolved in one pass.
func SiteWorlds(m *Manifest, cat Catalog, plan *Plan) (map[string]*world.World, error) {
	out := make(map[string]*world.World, len(plan.Sites))
	addr := 0 // the next simulated component's place in the block
	for _, sp := range plan.Sites {
		w, err := siteWorld(m, cat, plan, sp, &addr)
		if err != nil {
			return nil, err
		}
		out[w.Outpost] = w
	}
	return out, nil
}

// siteWorld is one site's world; addr is the simulated block's cursor.
func siteWorld(m *Manifest, cat Catalog, plan *Plan, sp SitePlan, addr *int) (*world.World, error) {
	site := sp.Name
	if cat.Stem == nil {
		return nil, fmt.Errorf("scenario %s: the catalog cannot name a component, so no world can be planned", plan.Scenario)
	}
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
	var minter nameMinter
	rooms := make([]world.Room, 0, len(sp.Systems))
	cover := map[string]world.Cover{}
	for _, sys := range sp.Systems {
		r := world.Room{Name: sys.Room, Building: sys.Building, Standard: sys.Standard, System: sys.Name}
		if k, ok := m.MeetingKnobs(sys.Standard); ok {
			r.Capacity = k.Attendance.Max
		}
		for _, fill := range sys.Slots {
			stem, ok := cat.Stem(fill.Product)
			if !ok {
				return nil, fmt.Errorf("scenario %s: product %q has no stem to name its components from", plan.Scenario, fill.Product)
			}
			for i := 0; i < fill.Count; i++ {
				d := world.Device{Name: minter.mint(sys.Room, stem), Product: fill.Product}
				d.Profile, d.Address = simulated(cat, fill.Product, addr)
				r.Devices = append(r.Devices, d)
			}
		}
		rooms = append(rooms, r)
		cover[sys.Name] = world.Cover{System: sys.Name, Site: site, Building: sys.Building, Room: sys.Room}
	}
	w.Sites = []world.Site{{Name: site, Rooms: rooms}}
	for _, p := range sp.Pushers {
		svc := world.Service{Name: p.Name, Profile: p.Profile, Webhook: p.Webhook}
		if p.Webhook {
			// A sender is a component placed at its site, named after the
			// site's rooms are staffed.
			stem, ok := cat.Stem(p.Profile)
			if !ok {
				return nil, fmt.Errorf("scenario %s: product %q has no stem to name its components from", plan.Scenario, p.Profile)
			}
			svc.Name = minter.mint(site, stem)
			_, svc.Address = simulated(cat, p.Profile, addr)
		}
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

// simulated is the simulator profile that serves a product and the address the
// simulated fleet's planner gives its component, "" for both when no profile
// serves it. The planner (sim/simfleet.Plan) hands the block out in creation
// order to each component a profile serves, so the k-th such component is the
// k-th address; devseed's test seeds a plan and holds this to it.
func simulated(cat Catalog, product string, next *int) (profile, address string) {
	if cat.SimProfile == nil {
		return "", ""
	}
	name, ok := cat.SimProfile(product)
	if !ok {
		return "", ""
	}
	address = fleet.Address(*next)
	*next++
	return name, address
}
