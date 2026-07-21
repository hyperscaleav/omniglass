package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestComponentScopeCRUD covers the component tier on the generic scoped-CRUD
// helpers: subtree scope, the 3-way split, occupancy, the system/location
// belongs-to + located-at FK resolution, FK faults, and audit.
func TestComponentScopeCRUD(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A location and a system for the component to bind to. campus is the
	// official type allowed at root; the type is incidental here, only the
	// name (rm-1) is asserted below.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "rm-1", LocationType: "campus"}, all); err != nil {
		t.Fatalf("seed location: %v", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "sys-1"}, all); err != nil {
		t.Fatalf("seed system: %v", err)
	}

	// disp (root, bound to sys-1 @ rm-1) > sub; plus cam (root).
	disp := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "disp", SystemName: strptr("sys-1"), LocationName: strptr("rm-1")}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "sub", ParentName: strptr("disp")}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "cam"}, all)

	if disp.SystemID == nil || disp.LocationID == nil {
		t.Errorf("disp belongs-to/located-at not set: %+v", disp)
	}

	readDisp := scope.Set{IDs: []string{disp.ID}}
	got, err := gw.ListComponents(ctx, readDisp)
	if err != nil || len(got) != 2 {
		t.Fatalf("disp-scope list = %d, err %v, want 2", len(got), err)
	}
	if _, err := gw.GetComponent(ctx, "cam", readDisp); !errors.Is(err, storage.ErrComponentNotFound) {
		t.Errorf("get cam under disp-scope = %v, want ErrComponentNotFound", err)
	}
	if _, err := gw.UpdateComponent(ctx, "", "sub", storage.ComponentPatch{DisplayName: strptr("S")}, readDisp, scope.Set{}); !errors.Is(err, storage.ErrComponentForbidden) {
		t.Errorf("update in-read not-action = %v, want ErrComponentForbidden", err)
	}
	if err := gw.DeleteComponent(ctx, "", "disp", all, all); !errors.Is(err, storage.ErrComponentOccupied) {
		t.Errorf("delete occupied disp = %v, want ErrComponentOccupied", err)
	}
	if err := gw.DeleteComponent(ctx, "", "sub", all, all); err != nil {
		t.Errorf("delete leaf sub = %v, want ok", err)
	}

	// FK faults.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "y", SystemName: strptr("nope")}, all); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("unknown system = %v, want ErrSystemNotFound", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp"}, all); !errors.Is(err, storage.ErrComponentExists) {
		t.Errorf("dup name = %v, want ErrComponentExists", err)
	}

	// Audit: 3 create + 1 delete = 4 component rows.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("audit connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'component'`).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 4 {
		t.Errorf("component audit rows = %d, want 4", n)
	}
}

func mustCreateComponent(t *testing.T, gw storage.Gateway, spec storage.ComponentSpec, sc scope.Set) *storage.Component {
	t.Helper()
	c, err := gw.CreateComponent(context.Background(), "", spec, sc)
	if err != nil {
		t.Fatalf("create component %s: %v", spec.Name, err)
	}
	return c
}
