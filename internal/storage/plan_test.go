package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest/querycount"
)

func planFixture(t *testing.T) (storage.Gateway, func(string) int) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return gw, func(q string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
}

func allScopes() storage.PlanScopes {
	all := scope.Set{All: true}
	return storage.PlanScopes{LocationCreate: all, LocationRead: all, SystemCreate: all, SystemRead: all, SystemUpdate: all,
		ComponentCreate: all, ComponentRead: all, ComponentUpdate: all, EndpointCreate: all, SecretRead: all,
		CanAdmin: true, CanCreateStandard: true}
}

// TestCreatePlanLandsWhole: a plan with a fork, a subtree of places, a system
// staffed by two components, declared values and a standalone component lands
// in one write, every audit row naming the template; the same plan with one
// bad piece at the end lands nothing at all.
func TestCreatePlanLandsWhole(t *testing.T) {
	t.Parallel()
	gw, count := planFixture(t)
	ctx := context.Background()
	bar := storagetest.MintProduct(t, ctx, gw, "video-bar")
	disp := storagetest.MintProduct(t, ctx, gw, "display")
	std := "plan-room"
	sysType := "meeting"
	plan := func(lastProduct string) storage.Plan {
		return storage.Plan{
			Template: "plan-fixture",
			Standards: []storage.StandardFromTemplate{{Template: "plan-room-template", Name: std, Label: "Plan Room", Roles: []storage.TemplateRole{
				{SystemRoleSpec: storage.SystemRoleSpec{Name: "bar", AcceptedTypes: []string{"video-bar"}}},
				{SystemRoleSpec: storage.SystemRoleSpec{Name: "screen", AcceptedTypes: []string{"display"}}},
			}}},
			Places: []storage.PlannedPlace{
				{Key: "b", Location: storage.LocationSpec{Name: "plan-hq", LocationType: "building"}},
				{Key: "f", ParentKey: "b", Location: storage.LocationSpec{Name: "f1", LocationType: "floor"}},
			},
			Systems: []storage.PlannedSystem{{PlaceKey: "f", System: storage.SystemSpec{SystemTypeID: &sysType, StandardID: &std},
				Components: []storage.PlannedComponent{
					{Role: "bar", Component: storage.ComponentSpec{ProductName: &bar.Name}},
					{Role: "screen", Component: storage.ComponentSpec{ProductName: &disp.Name},
						Properties: []storage.PlannedValue{{Name: "video-input", Value: json.RawMessage(`"hdmi1"`)}}},
				}}},
			Components: []storage.PlannedComponent{{PlaceKey: "b", Component: storage.ComponentSpec{ProductName: &lastProduct}}},
		}
	}

	before := count(`select count(*) from audit_log`)
	if _, err := gw.CreatePlan(ctx, "", plan("no-such-product"), allScopes()); err == nil {
		t.Fatal("a plan with an unknown product landed")
	}
	if n := count(`select count(*) from location`) + count(`select count(*) from system where name <> 'platform'`) + count(`select count(*) from component`) + count(`select count(*) from standard`); n != 0 {
		t.Fatalf("a refused plan left %d rows behind", n)
	}
	if n := count(`select count(*) from audit_log`); n != before {
		t.Fatalf("a refused plan left %d audit rows", n-before)
	}

	made, err := gw.CreatePlan(ctx, "", plan(disp.Name), allScopes())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if len(made.CreatedStandards) != 1 || len(made.Places) != 2 || len(made.Systems) != 1 || len(made.Systems[0].Components) != 2 || len(made.Components) != 1 {
		t.Fatalf("made = %+v, want one fork, two places, one system of two, one standalone", made)
	}
	if made.Places[1].ParentID == nil || *made.Places[1].ParentID != made.Places[0].ID {
		t.Fatalf("the floor's parent is %v, want the building", made.Places[1].ParentID)
	}
	if got := count(`select count(*) from system_role_assignment`); got != 2 {
		t.Fatalf("role assignments = %d, want 2", got)
	}
	if got := count(`select count(*) from property where provenance = 'declared' and component_id is not null`); got != 1 {
		t.Fatalf("declared component values = %d, want 1", got)
	}
	// Every audit row the plan wrote names a template under from_template: the
	// standard its own standard template, every other row the plan's.
	wrote := count(`select count(*) from audit_log`) - before
	named := count(`select count(*) from audit_log where new->>'from_template' = 'plan-fixture'`)
	own := count(`select count(*) from audit_log where resource = 'standard' and new->>'from_template' = 'plan-room-template'`)
	if wrote == 0 || own != 1 || named+own != wrote {
		t.Fatalf("the plan wrote %d audit rows: %d name the plan's template and %d the standard's own, want all", wrote, named, own)
	}

	// A second plan naming the standard uses it as it is: no second fork.
	again := plan(disp.Name)
	again.Places = []storage.PlannedPlace{{Key: "b", Location: storage.LocationSpec{Name: "plan-annex", LocationType: "building"}},
		{Key: "f", ParentKey: "b", Location: storage.LocationSpec{Name: "f1", LocationType: "floor"}}}
	if made, err := gw.CreatePlan(ctx, "", again, allScopes()); err != nil || len(made.CreatedStandards) != 0 {
		t.Fatalf("a plan on a held standard forked %v (%v), want none", made, err)
	}
}

// TestCreatePlanChecksEveryPieceInScope: a caller who may create systems but
// not components, or who may not fork a standard the plan needs, is refused
// at that piece, and nothing lands.
func TestCreatePlanChecksEveryPieceInScope(t *testing.T) {
	t.Parallel()
	gw, count := planFixture(t)
	ctx := context.Background()
	disp := storagetest.MintProduct(t, ctx, gw, "display")
	sysType := "meeting"
	std := "scoped-room"
	plan := storage.Plan{
		Standards: []storage.StandardFromTemplate{{Name: std, Roles: []storage.TemplateRole{{SystemRoleSpec: storage.SystemRoleSpec{Name: "screen", AcceptedTypes: []string{"display"}}}}}},
		Places:    []storage.PlannedPlace{{Key: "b", Location: storage.LocationSpec{Name: "scoped-hq", LocationType: "building"}}},
		Systems: []storage.PlannedSystem{{PlaceKey: "b", System: storage.SystemSpec{SystemTypeID: &sysType, StandardID: &std},
			Components: []storage.PlannedComponent{{Role: "screen", Component: storage.ComponentSpec{ProductName: &disp.Name}}}}},
	}

	noFork := allScopes()
	noFork.CanCreateStandard = false
	if _, err := gw.CreatePlan(ctx, "", plan, noFork); !isStage(err, "standard") {
		t.Fatalf("a plan needing a fork without standard:create = %v, want a standard-stage refusal", err)
	}
	noComponent := allScopes()
	noComponent.ComponentCreate = scope.Set{}
	if _, err := gw.CreatePlan(ctx, "", plan, noComponent); !isStage(err, "component") {
		t.Fatalf("a plan without component:create = %v, want a component-stage refusal", err)
	}
	if n := count(`select count(*) from location`) + count(`select count(*) from system where name <> 'platform'`) + count(`select count(*) from standard`); n != 0 {
		t.Fatalf("refused plans left %d rows behind", n)
	}
}

func isStage(err error, stage string) bool {
	pe, ok := err.(*storage.PlanError)
	return ok && pe.Stage == stage
}

// slotPlan is a plan for one system of n staffed slots under a building and a
// floor, on a standard of its own with a role per slot, so the system is
// healthy the moment its last slot is filled and impaired before. The names
// carry tag so two plans can share a database.
func slotPlan(tag string, n int, product string) storage.Plan {
	std, sysType := tag+"-room", "meeting"
	var roles []storage.TemplateRole
	var slots []storage.PlannedComponent
	for i := range n {
		role := fmt.Sprintf("slot-%d", i)
		roles = append(roles, storage.TemplateRole{SystemRoleSpec: storage.SystemRoleSpec{Name: role, AcceptedTypes: []string{"display"}}})
		slots = append(slots, storage.PlannedComponent{Role: role, Component: storage.ComponentSpec{ProductName: &product}})
	}
	return storage.Plan{
		Template:  tag + "-template",
		Standards: []storage.StandardFromTemplate{{Template: tag + "-room-template", Name: std, Label: tag, Roles: roles}},
		Places: []storage.PlannedPlace{
			{Key: "b", Location: storage.LocationSpec{Name: tag + "-hq", LocationType: "building"}},
			{Key: "f", ParentKey: "b", Location: storage.LocationSpec{Name: tag + "-f1", LocationType: "floor"}},
		},
		Systems: []storage.PlannedSystem{{PlaceKey: "f", System: storage.SystemSpec{SystemTypeID: &sysType, StandardID: &std}, Components: slots}},
	}
}

// TestCreatePlanRecordsOneHealthEdgePerOwner: a plan's system, staffed slot by
// slot inside one transaction, records its final verdict once, and so does
// each location above it. The intermediate impaired state (the system born
// with every role empty, then each slot filled) is an edge no reader could
// have observed, because the write is atomic; recording it put a phantom
// outage in every composite's history (#915).
func TestCreatePlanRecordsOneHealthEdgePerOwner(t *testing.T) {
	t.Parallel()
	gw, count := planFixture(t)
	ctx := context.Background()
	disp := storagetest.MintProduct(t, ctx, gw, "display")

	made, err := gw.CreatePlan(ctx, "", slotPlan("edge", 3, disp.Name), allScopes())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if len(made.Systems) != 1 || len(made.Systems[0].Components) != 3 || len(made.Places) != 2 {
		t.Fatalf("made = %+v, want one system of three under two places", made)
	}

	series := func(col, id string) int {
		t.Helper()
		return count(fmt.Sprintf(`select count(*) from property
			where %s = '%s' and provenance = 'calculated'
			  and property_type_id = (select id from property_type where name = 'health')`, col, id))
	}
	owners := []struct{ kind, col, id string }{{"system", "system_id", made.Systems[0].System.ID}}
	for _, l := range made.Places {
		owners = append(owners, struct{ kind, col, id string }{"location", "location_id", l.ID})
	}
	for _, o := range owners {
		if n := series(o.col, o.id); n != 1 {
			t.Errorf("%s %s recorded %d health edges, want 1 (the final verdict)", o.kind, o.id, n)
		}
	}
	for _, c := range made.Systems[0].Components {
		if n := series("component_id", c.ID); n != 1 {
			t.Errorf("component %s recorded %d health edges, want 1", c.ID, n)
		}
	}

	all := scope.Set{All: true}
	rep, err := gw.SystemHealth(ctx, made.Systems[0].System.ID, 0, all)
	if err != nil {
		t.Fatalf("system health: %v", err)
	}
	if rep.Verdict != "healthy" {
		t.Errorf("the staffed system reads %q, want healthy", rep.Verdict)
	}
	for _, l := range made.Places {
		rep, err := gw.LocationHealth(ctx, l.ID, 0, all)
		if err != nil {
			t.Fatalf("location health %s: %v", l.Name, err)
		}
		if rep.Verdict != "healthy" {
			t.Errorf("location %s reads %q, want healthy", l.Name, rep.Verdict)
		}
	}
}

// TestCreatePlanHealthRecomputeCostIsFlatInSlotCount: the health recompute a
// composite pays is one pass over its chain, not one per slot. Measured as
// the system and location rows the rollup records (a recompute's per-owner
// write), counted off the pool's own statements: a plan of two slots and a
// plan of six must cost the same, because each system and location is visited
// once however many components staff the system. The per-component write
// grows with the slots by design (each component's opening verdict) and is
// not what is held flat.
func TestCreatePlanHealthRecomputeCostIsFlatInSlotCount(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	counter := querycount.New()
	gw, err := storage.NewPG(ctx, storagetest.NewDSN(t), storage.WithQueryTracer(counter))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	disp := storagetest.MintProduct(t, ctx, gw, "display")

	ownerRecords := func(tag string, slots int) (records, total int) {
		t.Helper()
		counter.Reset()
		if _, err := gw.CreatePlan(ctx, "", slotPlan(tag, slots, disp.Name), allScopes()); err != nil {
			t.Fatalf("create plan of %d: %v", slots, err)
		}
		for _, c := range counter.Calls() {
			if !strings.Contains(c.SQL, "insert into property") || len(c.Args) == 0 {
				continue
			}
			if kind, _ := c.Args[0].(string); kind == "system" || kind == "location" {
				records++
			}
		}
		return records, counter.N()
	}
	small, smallTotal := ownerRecords("two", 2)
	large, largeTotal := ownerRecords("six", 6)
	if largeTotal <= smallTotal {
		t.Fatalf("six slots cost %d statements and two cost %d: the dimension never varied, so nothing was measured", largeTotal, smallTotal)
	}
	// One system, one floor and one building, each recorded once.
	if small != 3 || large != 3 {
		t.Errorf("the rollup recorded %d system/location rows for two slots and %d for six, want 3 for both (once per owner of the chain)", small, large)
	}
}
