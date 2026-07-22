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

// TestSystemScopeCRUD mirrors the location scope test for the system tier: the
// shared scoped-tree primitive must give systems the same subtree scoping, the
// three-way 404/403 split, occupancy, FK faults, and in-tx audit. It also
// exercises located-at (system.location_id) resolution.
func TestSystemScopeCRUD(t *testing.T) {
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

	// A location to place a system at (exercises location_id resolution).
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "campus"}, all); err != nil {
		t.Fatalf("seed location: %v", err)
	}

	// Build: av (root) > av-sub (subsystem) > av-leaf; plus lab (root). av is
	// located at hq.
	av := mustCreateSystem(t, gw, storage.SystemSpec{Name: "av", LocationName: strptr("hq")}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "av-sub", ParentName: strptr("av")}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "av-leaf", ParentName: strptr("av-sub")}, all)
	mustCreateSystem(t, gw, storage.SystemSpec{Name: "lab"}, all)

	if av.LocationID == nil {
		t.Error("av located-at not set")
	}

	if got, err := gw.ListSystems(ctx, all); err != nil || len(got) != 4 {
		t.Fatalf("list all = %d, err %v, want 4", len(got), err)
	}

	// Read scope rooted at av sees av + 2 descendants, never lab.
	readAV := scope.Set{IDs: []string{av.ID}}
	got, err := gw.ListSystems(ctx, readAV)
	if err != nil || len(got) != 3 {
		t.Fatalf("av-scope list = %d, err %v, want 3", len(got), err)
	}
	for _, s := range got {
		if s.Name == "lab" {
			t.Fatal("av-scope leaked lab")
		}
	}

	// Non-disclosing 404 outside read scope; ok inside.
	if _, err := gw.GetSystem(ctx, "lab", readAV); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("get lab under av-scope = %v, want ErrSystemNotFound", err)
	}
	if _, err := gw.GetSystem(ctx, "av-sub", readAV); err != nil {
		t.Errorf("get av-sub under av-scope = %v, want ok", err)
	}

	// Three-way split: readable, no action scope = 403.
	if _, err := gw.UpdateSystem(ctx, "", "av-sub", storage.SystemPatch{DisplayName: strptr("Sub")}, readAV, scope.Set{}); !errors.Is(err, storage.ErrSystemForbidden) {
		t.Errorf("update in-read not-action = %v, want ErrSystemForbidden", err)
	}
	// Out of read scope = 404.
	if _, err := gw.UpdateSystem(ctx, "", "lab", storage.SystemPatch{DisplayName: strptr("X")}, readAV, readAV); !errors.Is(err, storage.ErrSystemNotFound) {
		t.Errorf("update out-of-read = %v, want ErrSystemNotFound", err)
	}
	// Fully scoped update succeeds.
	if up, err := gw.UpdateSystem(ctx, "", "av-sub", storage.SystemPatch{DisplayName: strptr("Subsystem A")}, all, all); err != nil || up.DisplayName != "Subsystem A" {
		t.Fatalf("scoped update = %+v, err %v", up, err)
	}

	// Occupancy: av has children -> refused; a leaf deletes.
	if err := gw.DeleteSystem(ctx, "", "av", all, all); !errors.Is(err, storage.ErrSystemOccupied) {
		t.Errorf("delete occupied av = %v, want ErrSystemOccupied", err)
	}
	if err := gw.DeleteSystem(ctx, "", "av-leaf", all, all); err != nil {
		t.Errorf("delete leaf av-leaf = %v, want ok", err)
	}

	// Faults.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "weird", StandardID: strptr("galaxy")}, all); !errors.Is(err, storage.ErrUnknownStandard) {
		t.Errorf("unknown standard = %v, want ErrUnknownStandard", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "av"}, all); !errors.Is(err, storage.ErrSystemExists) {
		t.Errorf("dup name = %v, want ErrSystemExists", err)
	}
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "bad-loc", LocationName: strptr("nope")}, all); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("unknown location = %v, want ErrLocationNotFound", err)
	}
	// Create under out-of-scope parent forbidden.
	if _, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "x", ParentName: strptr("lab")}, readAV); !errors.Is(err, storage.ErrSystemForbidden) {
		t.Errorf("create under out-of-scope parent = %v, want ErrSystemForbidden", err)
	}

	// Audit rows for systems: 4 create + 1 update + 1 delete = 6.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("audit connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'system'`).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 6 {
		t.Errorf("system audit rows = %d, want 6", n)
	}
}

func mustCreateSystem(t *testing.T, gw storage.Gateway, spec storage.SystemSpec, sc scope.Set) *storage.System {
	t.Helper()
	s, err := gw.CreateSystem(context.Background(), "", spec, sc)
	if err != nil {
		t.Fatalf("create system %s: %v", spec.Name, err)
	}
	return s
}
