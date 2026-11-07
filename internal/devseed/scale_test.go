package devseed_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/lab/scenario"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func corporatePlan(t *testing.T, size int) *scenario.Plan {
	t.Helper()
	m, err := scenario.Shipped("corporate")
	if err != nil {
		t.Fatal(err)
	}
	cat, err := scenario.ShippedCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if size == 0 {
		size = scenario.MinSystems(m)
	}
	p, err := scenario.Resolve(m, cat, size)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The plan's location types are the example fleet's, read off its deepest
// branch, so no type is named here either: the room level is the one a
// fixture room holding a system has.
func TestFixtureLevelsReadTheFixturesFourLevels(t *testing.T) {
	t.Parallel()
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	lv, err := devseed.FixtureLevels(doc)
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, l := range doc.Locations {
		types[l.Key] = l.Type
	}
	roomTypes := map[string]bool{}
	for _, s := range doc.Systems {
		roomTypes[types[s.Location]] = true
	}
	if !roomTypes[lv.Room] {
		t.Errorf("room level %q holds no fixture system (system location types %v)", lv.Room, roomTypes)
	}
	distinct := map[string]bool{lv.Site: true, lv.Building: true, lv.Floor: true, lv.Room: true}
	if len(distinct) != 4 {
		t.Errorf("levels %+v are not four distinct types", lv)
	}
	if _, err := devseed.FixtureLevels(devseed.Doc{}); err == nil {
		t.Error("a document with no four-deep branch read as levels")
	}
}

// PlanDoc is the plan as a seed document: every location's parent comes
// first, every system sits in a room, and every planned component is placed
// in its system's room and assigned to a role of that system.
func TestPlanDocPlacesEveryPlannedComponentInItsSystemsRoom(t *testing.T) {
	t.Parallel()
	p := corporatePlan(t, 100)
	lv := devseed.PlanLevels{Site: "s", Building: "b", Floor: "f", Room: "r"}
	doc := devseed.PlanDoc(p, lv)

	declared := map[string]string{}
	for _, l := range doc.Locations {
		if l.Parent != "" && declared[l.Parent] == "" {
			t.Errorf("location %s comes before its parent %s", l.Key, l.Parent)
		}
		if declared[l.Key] != "" {
			t.Errorf("location %s declared twice", l.Key)
		}
		declared[l.Key] = l.Type
	}
	sites := 0
	for _, l := range doc.Locations {
		if l.Parent == "" {
			sites++
		}
	}
	if sites != len(p.Sites) {
		t.Errorf("%d root locations, want one per site (%d)", sites, len(p.Sites))
	}
	if len(doc.Systems) != p.Totals.Systems {
		t.Errorf("%d systems, want %d", len(doc.Systems), p.Totals.Systems)
	}
	sysRoom := map[string]string{}
	for _, s := range doc.Systems {
		if declared[s.Location] != lv.Room {
			t.Errorf("system %s is in %q, a %q, not a room", s.Key, s.Location, declared[s.Location])
		}
		sysRoom[s.Key] = s.Location
	}
	if len(doc.Components) != p.Totals.Components {
		t.Errorf("%d components, want %d", len(doc.Components), p.Totals.Components)
	}
	compRoom := map[string]string{}
	for _, c := range doc.Components {
		if c.Name != "" {
			t.Errorf("component %s is named %q; the platform mints a planned component's name", c.Key, c.Name)
		}
		compRoom[c.Key] = c.Location
	}
	if len(doc.RoleAssignments) != len(doc.Components) {
		t.Errorf("%d role assignments for %d components", len(doc.RoleAssignments), len(doc.Components))
	}
	for _, ra := range doc.RoleAssignments {
		if compRoom[ra.Component] != sysRoom[ra.System] {
			t.Errorf("%s fills %s of %s from another room", ra.Component, ra.Role, ra.System)
		}
	}
}

// The seed stands the plan up with one node per site, named for it, and
// every component of a site simulated on that site's node at an address no
// other component has.
func TestSeedPlanPutsEachSitesComponentsOnItsOwnNode(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("needs Postgres")
	}
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, devseed.BootSeededDSN(t), devseed.SecretProvider())
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	p := corporatePlan(t, 0)
	if err := devseed.SeedPlan(ctx, gw, "", p, devseed.Options{}); err != nil {
		t.Fatal(err)
	}
	// A re-run changes nothing.
	if err := devseed.SeedPlan(ctx, gw, "", p, devseed.Options{}); err != nil {
		t.Fatalf("re-run: %v", err)
	}

	all := scope.Set{All: true}
	nodes, err := gw.ListNodes(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != len(p.Sites) {
		t.Errorf("%d nodes for %d sites", len(nodes), len(p.Sites))
	}
	addresses := map[string]string{}
	simulated := 0
	for _, site := range p.Sites {
		devices, err := gw.NodeSimDevices(ctx, scenario.SiteNode(site.Name), all)
		if err != nil {
			t.Fatal(err)
		}
		if len(devices) != site.Totals.Components {
			t.Errorf("site %s: node %s simulates %d components, want the site's %d",
				site.Name, scenario.SiteNode(site.Name), len(devices), site.Totals.Components)
		}
		for _, d := range devices {
			if d.Address == "" {
				t.Errorf("%s has no address", d.ComponentID)
				continue
			}
			if other, ok := addresses[d.Address]; ok {
				t.Errorf("%s and %s share %s", d.ComponentID, other, d.Address)
			}
			addresses[d.Address] = d.ComponentID
			simulated++
		}
	}
	if simulated != p.Totals.Components {
		t.Errorf("%d components simulated, want all %d", simulated, p.Totals.Components)
	}
}
