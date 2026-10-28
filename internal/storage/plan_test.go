package storage_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
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
		CanAdmin: true, CanForkStandard: true}
}

// TestCreatePlanLandsWhole: a plan with a fork, a subtree of places, a system
// staffed by two components, declared values and a standalone component lands
// in one write, every audit row naming the template; the same plan with one
// bad piece at the end lands nothing at all.
func TestCreatePlanLandsWhole(t *testing.T) {
	gw, count := planFixture(t)
	ctx := context.Background()
	bar := storagetest.MintProduct(t, ctx, gw, "video-bar")
	disp := storagetest.MintProduct(t, ctx, gw, "display")
	std := "plan-room"
	sysType := "meeting"
	plan := func(lastProduct string) storage.Plan {
		return storage.Plan{
			Template: "plan-fixture",
			Standards: []storage.StandardFork{{Template: "plan-fixture", Name: std, Label: "Plan Room", Roles: []storage.ForkRole{
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
	if n := count(`select count(*) from location`) + count(`select count(*) from system`) + count(`select count(*) from component`) + count(`select count(*) from standard`); n != 0 {
		t.Fatalf("a refused plan left %d rows behind", n)
	}
	if n := count(`select count(*) from audit_log`); n != before {
		t.Fatalf("a refused plan left %d audit rows", n-before)
	}

	made, err := gw.CreatePlan(ctx, "", plan(disp.Name), allScopes())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if len(made.Forked) != 1 || len(made.Places) != 2 || len(made.Systems) != 1 || len(made.Systems[0].Components) != 2 || len(made.Components) != 1 {
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
	// Every audit row the plan wrote names the template.
	wrote := count(`select count(*) from audit_log`) - before
	named := count(`select count(*) from audit_log where new->>'template' = 'plan-fixture'`)
	if wrote == 0 || named != wrote {
		t.Fatalf("the plan wrote %d audit rows and %d name the template, want all", wrote, named)
	}

	// A second plan naming the standard uses it as it is: no second fork.
	again := plan(disp.Name)
	again.Places = []storage.PlannedPlace{{Key: "b", Location: storage.LocationSpec{Name: "plan-annex", LocationType: "building"}},
		{Key: "f", ParentKey: "b", Location: storage.LocationSpec{Name: "f1", LocationType: "floor"}}}
	if made, err := gw.CreatePlan(ctx, "", again, allScopes()); err != nil || len(made.Forked) != 0 {
		t.Fatalf("a plan on a held standard forked %v (%v), want none", made, err)
	}
}

// TestCreatePlanChecksEveryPieceInScope: a caller who may create systems but
// not components, or who may not fork a standard the plan needs, is refused
// at that piece, and nothing lands.
func TestCreatePlanChecksEveryPieceInScope(t *testing.T) {
	gw, count := planFixture(t)
	ctx := context.Background()
	disp := storagetest.MintProduct(t, ctx, gw, "display")
	sysType := "meeting"
	std := "scoped-room"
	plan := storage.Plan{
		Standards: []storage.StandardFork{{Name: std, Roles: []storage.ForkRole{{SystemRoleSpec: storage.SystemRoleSpec{Name: "screen", AcceptedTypes: []string{"display"}}}}}},
		Places:    []storage.PlannedPlace{{Key: "b", Location: storage.LocationSpec{Name: "scoped-hq", LocationType: "building"}}},
		Systems: []storage.PlannedSystem{{PlaceKey: "b", System: storage.SystemSpec{SystemTypeID: &sysType, StandardID: &std},
			Components: []storage.PlannedComponent{{Role: "screen", Component: storage.ComponentSpec{ProductName: &disp.Name}}}}},
	}

	noFork := allScopes()
	noFork.CanForkStandard = false
	if _, err := gw.CreatePlan(ctx, "", plan, noFork); !isStage(err, "standard") {
		t.Fatalf("a plan needing a fork without standard:create = %v, want a standard-stage refusal", err)
	}
	noComponent := allScopes()
	noComponent.ComponentCreate = scope.Set{}
	if _, err := gw.CreatePlan(ctx, "", plan, noComponent); !isStage(err, "component") {
		t.Fatalf("a plan without component:create = %v, want a component-stage refusal", err)
	}
	if n := count(`select count(*) from location`) + count(`select count(*) from system`) + count(`select count(*) from standard`); n != 0 {
		t.Fatalf("refused plans left %d rows behind", n)
	}
}

func isStage(err error, stage string) bool {
	pe, ok := err.(*storage.PlanError)
	return ok && pe.Stage == stage
}
