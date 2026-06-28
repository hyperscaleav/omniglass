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

// all is the owner-equivalent scope used to build the fixture tree.
var all = scope.Set{All: true}

func strptr(s string) *string { return &s }

// TestLocationScopeCRUD exercises the gateway's scope expansion and the
// three-way read/action split end to end against real Postgres: an all-scoped
// caller builds a tree, a location-scoped read set sees only the subtree, a
// targeted op outside read scope is non-disclosing (not found), one inside read
// but outside action scope is forbidden, occupancy refuses a delete, and every
// write leaves an audit row.
func TestLocationScopeCRUD(t *testing.T) {
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

	// Build: hq (campus root) > hq-b1 (building) > hq-r1 (room); plus lab (root).
	hq := mustCreate(t, gw, storage.LocationSpec{Name: "hq", LocationType: "campus"}, all)
	mustCreate(t, gw, storage.LocationSpec{Name: "hq-b1", LocationType: "building", ParentName: strptr("hq")}, all)
	mustCreate(t, gw, storage.LocationSpec{Name: "hq-r1", LocationType: "room", ParentName: strptr("hq-b1")}, all)
	mustCreate(t, gw, storage.LocationSpec{Name: "lab", LocationType: "campus"}, all)

	// All scope lists everything.
	if locs, err := gw.ListLocations(ctx, all); err != nil || len(locs) != 4 {
		t.Fatalf("list all = %d locs, err %v, want 4", len(locs), err)
	}

	// A read scope rooted at hq sees hq + its two descendants, never lab.
	readHQ := scope.Set{IDs: []string{hq.ID}}
	locs, err := gw.ListLocations(ctx, readHQ)
	if err != nil {
		t.Fatalf("list hq-scope: %v", err)
	}
	if len(locs) != 3 {
		t.Fatalf("hq-scope list = %d, want 3 (hq subtree)", len(locs))
	}
	for _, l := range locs {
		if l.Name == "lab" {
			t.Fatal("hq-scope leaked lab")
		}
	}

	// Targeted GET: lab is outside the hq read scope, so it is non-disclosing.
	if _, err := gw.GetLocation(ctx, "lab", readHQ); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("get lab under hq-scope = %v, want ErrLocationNotFound", err)
	}
	if _, err := gw.GetLocation(ctx, "hq-b1", readHQ); err != nil {
		t.Errorf("get hq-b1 under hq-scope = %v, want ok", err)
	}

	// The three-way split on update. Readable (hq scope) but no action scope = 403.
	_, err = gw.UpdateLocation(ctx, "", "hq-b1", storage.LocationPatch{DisplayName: strptr("B1")}, readHQ, scope.Set{})
	if !errors.Is(err, storage.ErrLocationForbidden) {
		t.Errorf("update in-read not-in-action = %v, want ErrLocationForbidden", err)
	}
	// Outside read scope entirely = non-disclosing 404, even with an action scope.
	_, err = gw.UpdateLocation(ctx, "", "lab", storage.LocationPatch{DisplayName: strptr("X")}, readHQ, readHQ)
	if !errors.Is(err, storage.ErrLocationNotFound) {
		t.Errorf("update out-of-read = %v, want ErrLocationNotFound", err)
	}
	// Fully scoped update succeeds and returns the new shape.
	updated, err := gw.UpdateLocation(ctx, "", "hq-b1", storage.LocationPatch{DisplayName: strptr("Building One")}, all, all)
	if err != nil || updated.DisplayName != "Building One" {
		t.Fatalf("scoped update = %+v, err %v", updated, err)
	}

	// Delete is refused while the location still has children.
	if err := gw.DeleteLocation(ctx, "", "hq", all, all); !errors.Is(err, storage.ErrLocationOccupied) {
		t.Errorf("delete occupied hq = %v, want ErrLocationOccupied", err)
	}
	// A leaf deletes cleanly.
	if err := gw.DeleteLocation(ctx, "", "hq-r1", all, all); err != nil {
		t.Errorf("delete leaf hq-r1 = %v, want ok", err)
	}

	// Create under a parent outside the create scope is forbidden.
	_, err = gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "lab-r1", LocationType: "room", ParentName: strptr("lab")}, readHQ)
	if !errors.Is(err, storage.ErrLocationForbidden) {
		t.Errorf("create under out-of-scope parent = %v, want ErrLocationForbidden", err)
	}
	// A non-all caller cannot create a root.
	_, err = gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "root2", LocationType: "campus"}, readHQ)
	if !errors.Is(err, storage.ErrLocationForbidden) {
		t.Errorf("create root without all = %v, want ErrLocationForbidden", err)
	}
	// An unknown location_type is rejected by the FK.
	_, err = gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "weird", LocationType: "galaxy"}, all)
	if !errors.Is(err, storage.ErrUnknownType) {
		t.Errorf("create unknown type = %v, want ErrUnknownType", err)
	}
	// A duplicate name clashes.
	_, err = gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq", LocationType: "campus"}, all)
	if !errors.Is(err, storage.ErrLocationExists) {
		t.Errorf("create dup name = %v, want ErrLocationExists", err)
	}

	// The writes left audit rows: 4 creates + 1 update + 1 delete = 6.
	assertAuditCount(t, ctx, dsn)
}

func mustCreate(t *testing.T, gw storage.Gateway, spec storage.LocationSpec, sc scope.Set) *storage.Location {
	t.Helper()
	l, err := gw.CreateLocation(context.Background(), "", spec, sc)
	if err != nil {
		t.Fatalf("create %s: %v", spec.Name, err)
	}
	return l
}

// assertAuditCount reads the audit_log directly: the gateway is the system under
// test, and the audit row is an internal effect, so a direct read against the
// same database is the honest assertion.
func assertAuditCount(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("audit connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'location'`).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 6 {
		t.Errorf("audit rows = %d, want 6 (4 create, 1 update, 1 delete)", n)
	}
}
