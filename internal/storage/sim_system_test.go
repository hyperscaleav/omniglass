package storage_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestCreateSimSystem proves a simulated system lands whole or not at all:
// the system to a standard, a component per planned slot with its endpoint
// on the node, each assigned its role, in one transaction; a slot that
// cannot be assigned, or a scope that refuses the system, leaves nothing.
func TestCreateSimSystem(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge"}, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "loc-s", LocationType: "campus"}, all); err != nil {
		t.Fatal(err)
	}
	bar := storagetest.MintProduct(t, ctx, gw, "video-bar")
	disp := storagetest.MintProduct(t, ctx, gw, "display")
	pan := storagetest.MintProduct(t, ctx, gw, "touch-panel")
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	count := func(q string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	loc, sysType, standard := "loc-s", "meeting", "meeting-room"
	slot := func(role, product, target string) storage.SimSystemComponent {
		return storage.SimSystemComponent{Role: role, Product: product,
			Endpoints: []storage.EndpointSpec{{Transport: "tcp", Params: []byte(`{"target":"` + target + `"}`)}}}
	}
	sc := storage.SimSystemScopes{SystemCreate: all, LocationRead: all, ComponentCreate: all,
		SystemRead: all, SystemUpdate: all, EndpointCreate: all, SecretRead: all, CanAdmin: true,
		ComponentRead: all, ComponentUpdate: all}
	spec := storage.SimSystemSpec{
		System: storage.SystemSpec{SystemTypeID: &sysType, StandardID: &standard, LocationName: &loc},
		Node:   "edge",
		Components: []storage.SimSystemComponent{
			slot("video-bar", bar.Name, "127.60.0.1:80"),
			slot("main-display", disp.Name, "127.60.0.2:80"),
			slot("touch-control", pan.Name, "127.60.0.3:80"),
		},
	}

	// A slot whose role the standard does not have fails the whole write.
	bad := spec
	bad.Components = append(append([]storage.SimSystemComponent{}, spec.Components...), slot("no-such-role", disp.Name, "127.60.0.4:80"))
	before := count(`select count(*) from component`)
	if _, err := gw.CreateSimSystem(ctx, "", bad, sc); err == nil {
		t.Fatal("a slot with an unknown role was accepted")
	}
	if n := count(`select count(*) from component`); n != before {
		t.Fatalf("a failed simulated system left %d components behind", n-before)
	}
	if n := count(`select count(*) from system where location_id = (select id from location where name = 'loc-s')`); n != 0 {
		t.Fatalf("a failed simulated system left %d systems behind", n)
	}

	// A caller who may not create systems is refused before anything lands.
	noSys := sc
	noSys.SystemCreate = scope.Set{}
	if _, err := gw.CreateSimSystem(ctx, "", spec, noSys); err == nil {
		t.Fatal("a caller without system:create created a simulated system")
	}
	if n := count(`select count(*) from component`); n != before {
		t.Fatalf("a refused simulated system left %d components behind", n-before)
	}

	got, err := gw.CreateSimSystem(ctx, "", spec, sc)
	if err != nil {
		t.Fatal(err)
	}
	if got.System == nil || got.System.Name == "" || len(got.Components) != 3 {
		t.Fatalf("result = %+v, want a named system and three components", got)
	}
	if n := count(`select count(*) from system_role_assignment where system_id = (select id from system where name = '` + got.System.Name + `')`); n != 3 {
		t.Fatalf("%d role assignments, want 3", n)
	}
	devs, err := gw.NodeSimDevices(ctx, "edge", all)
	if err != nil || len(devs) != 3 {
		t.Fatalf("node lists %d simulated components (%v), want 3", len(devs), err)
	}
	if n := count(`select count(*) from audit_log where resource = 'system_role_assignment'`); n < 3 {
		t.Fatalf("%d role assignment audit rows, want each assignment audited", n)
	}
}
