package devseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/simfleet"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The scale lab (#1554) seeds a resolved scenario plan rather than the example
// fleet: every site, building, floor and room the plan places systems in, every
// system staffed by its standard's components, and one edge node per site with
// that site's components simulated on it, each at its own loopback address.
// SeedPlan is the one function the bulk seed (#1555) replaces; it goes through
// the same seed path as the example fleet (seedFleet, then the simulated
// fleet's helpers).

// PlanLevels are the location types a plan's four levels take, read off the
// example fleet (location types are customer data, never named in code).
type PlanLevels struct {
	Site, Building, Floor, Room string
}

// FixtureLevels reads the plan's location types off the first branch of the
// document's location tree that is four deep, root first.
func FixtureLevels(doc Doc) (PlanLevels, error) {
	byKey := map[string]Location{}
	for _, l := range doc.Locations {
		byKey[l.Key] = l
	}
	for _, l := range doc.Locations {
		chain := []string{l.Type}
		for p := l.Parent; p != ""; p = byKey[p].Parent {
			chain = append([]string{byKey[p].Type}, chain...)
		}
		if len(chain) == 4 {
			return PlanLevels{Site: chain[0], Building: chain[1], Floor: chain[2], Room: chain[3]}, nil
		}
	}
	return PlanLevels{}, errors.New("devseed: the fixture has no location four levels deep to read a plan's location types from")
}

// PlannedComponent is one component a plan places: the site it is at, the
// system and role it fills, its fixture key, its product and its room.
type PlannedComponent struct {
	Site, System, Role, Key, Product, Room string
}

// PlanComponents lists every component a plan places, site by site, system by
// system, role by role. Pure.
func PlanComponents(p *scenario.Plan) []PlannedComponent {
	var out []PlannedComponent
	for _, site := range p.Sites {
		for _, sys := range site.Systems {
			for _, st := range sys.Staffing {
				for n := 1; n <= st.Count; n++ {
					out = append(out, PlannedComponent{
						Site: site.Name, System: sys.Name, Role: st.Role, Product: st.Product, Room: sys.Room,
						Key: fmt.Sprintf("%s/%s/%d", sys.Name, st.Role, n),
					})
				}
			}
		}
	}
	return out
}

// PlanDoc is the seed document a plan stands for: each site, then each
// building, floor and room its systems sit in (a parent always ahead of its
// children), each system in its room, and each system's components in the
// same room, assigned to their roles. Location and system keys are their plan
// names, which are fleet-unique; components are nameless, so the platform
// mints their names as it does the example fleet's. Pure.
func PlanDoc(p *scenario.Plan, lv PlanLevels) Doc {
	var doc Doc
	seen := map[string]bool{}
	place := func(name, label, typ, parent string) {
		if seen[name] {
			return
		}
		seen[name] = true
		doc.Locations = append(doc.Locations, Location{Key: name, Name: name, Label: label, Type: typ, Parent: parent})
	}
	for _, site := range p.Sites {
		place(site.Name, site.Label, lv.Site, "")
		for _, sys := range site.Systems {
			place(sys.Building, "", lv.Building, site.Name)
			place(sys.Floor, "", lv.Floor, sys.Building)
			place(sys.Room, "", lv.Room, sys.Floor)
			doc.Systems = append(doc.Systems, System{Key: sys.Name, Name: sys.Name, Label: sys.Label,
				Standard: sys.Standard, SystemType: sys.SystemType, Location: sys.Room})
		}
	}
	for _, c := range PlanComponents(p) {
		doc.Components = append(doc.Components, Component{Key: c.Key, Product: c.Product, Location: c.Room})
		doc.RoleAssignments = append(doc.RoleAssignments, RoleAssignment{System: c.System, Role: c.Role, Component: c.Key})
	}
	return doc
}

// SeedPlan seeds a plan through the seed path: the fleet PlanDoc describes,
// then for each site its node (scenario.SiteNode, at the site) and its
// components' simulated endpoints on that node. Addresses are allotted across
// all sites from one pool, so no two components share one. A component that
// already has an endpoint keeps it, so a re-run changes nothing.
func SeedPlan(ctx context.Context, gw storage.Gateway, actorID string, p *scenario.Plan, opts Options) error {
	all := scope.Set{All: true}
	fixtures, err := Fixtures()
	if err != nil {
		return err
	}
	lv, err := FixtureLevels(fixtures)
	if err != nil {
		return err
	}
	var f fleet
	if err := seedFleet(ctx, gw, actorID, PlanDoc(p, lv), &f); err != nil {
		return err
	}
	if err := seedSimSecrets(ctx, gw, actorID); err != nil {
		return err
	}
	cat, _, err := simCatalog(ctx, gw, opts.SimPortOffset)
	if err != nil {
		return err
	}
	endpoints, err := gw.ListEndpoints(ctx, all)
	if err != nil {
		return fmt.Errorf("devseed: list endpoints: %w", err)
	}
	used, wired := map[string]bool{}, map[string]bool{}
	for _, e := range endpoints {
		if target := endpointTarget(e.Params); target != "" {
			used[storage.TargetHost(target)] = true
		}
		if e.ComponentID != nil {
			wired[*e.ComponentID] = true
		}
	}
	bySite := map[string][]simfleet.Component{}
	for _, c := range PlanComponents(p) {
		id := f.compIDs[c.Key]
		if id == "" {
			return fmt.Errorf("devseed: planned component %s was not seeded", c.Key)
		}
		if !wired[id] {
			bySite[c.Site] = append(bySite[c.Site], simfleet.Component{ID: id, Product: c.Product})
		}
	}
	for _, site := range p.Sites {
		node := scenario.SiteNode(site.Name)
		if err := ensureSiteNode(ctx, gw, actorID, node, site, f.locIDs[site.Name]); err != nil {
			return err
		}
		plan, err := simfleet.Plan(bySite[site.Name], used, cat)
		if err != nil {
			return err
		}
		for _, dev := range plan {
			used[dev.Address] = true
		}
		if err := createSimEndpoints(ctx, gw, actorID, node, plan); err != nil {
			return err
		}
	}
	return nil
}

// ensureSiteNode creates a site's node at the site, unenrolled: the lab mints
// its enrollment token, as it does the example fleet's node. A node already
// there is left as it is.
func ensureSiteNode(ctx context.Context, gw storage.Gateway, actorID, node string, site scenario.SitePlan, locID string) error {
	all := scope.Set{All: true}
	if _, err := gw.GetNode(ctx, node, all); !errors.Is(err, storage.ErrNodeNotFound) {
		return err
	}
	label := site.Label
	if label == "" {
		label = site.Name
	}
	if _, err := gw.CreateNode(ctx, actorID, storage.NodeSpec{
		Name: node, Label: label + " Edge Node", Description: "the scale lab's node for " + site.Name,
		LocationName: &locID,
	}, all, all); err != nil {
		return fmt.Errorf("devseed: create site node %s: %w", node, err)
	}
	return nil
}
