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

// TestInterfaceScopeCRUD covers the interface tier's component-cascade scope: an
// interface inherits its owning component's read/action scope. A principal scoped
// to component A can list/get/create/update/delete A's interfaces and is denied
// (empty list / non-disclosing 404 / forbidden) B's interface and any
// component-less one. Plus the FK faults and audit.
func TestInterfaceScopeCRUD(t *testing.T) {
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

	// Two root components, plus a scope confined to A's subtree.
	compA := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "comp-a", ComponentType: "display"}, all)
	compB := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "comp-b", ComponentType: "display"}, all)
	readA := scope.Set{IDs: []string{compA.ID}}
	_ = compB

	// Owner (all) creates an interface on A, on B, and a component-less one. The
	// surrogate ids returned are the addresses used below (interfaces are id-keyed).
	ifA, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "if-a", Type: "tcp", Component: strptr("comp-a")}, all)
	if err != nil {
		t.Fatalf("create if-a: %v", err)
	}
	ifB, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "if-b", Type: "tcp", Component: strptr("comp-b")}, all)
	if err != nil {
		t.Fatalf("create if-b: %v", err)
	}
	ifNull, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "if-null", Type: "icmp"}, all)
	if err != nil {
		t.Fatalf("create if-null: %v", err)
	}
	if ifA.Component == nil || *ifA.Component != "comp-a" {
		t.Fatalf("if-a component = %v, want comp-a", ifA.Component)
	}

	// Cascade READ: A-scope sees only A's interface, not B's, not the component-less.
	got, err := gw.ListInterfaces(ctx, readA)
	if err != nil || len(got) != 1 || got[0].Name != "if-a" {
		t.Fatalf("A-scope list = %+v (err %v), want just if-a", got, err)
	}
	if _, err := gw.GetInterface(ctx, ifA.ID, readA); err != nil {
		t.Fatalf("get if-a under A-scope: %v", err)
	}
	if _, err := gw.GetInterface(ctx, ifB.ID, readA); !errors.Is(err, storage.ErrInterfaceNotFound) {
		t.Errorf("get if-b under A-scope = %v, want ErrInterfaceNotFound (non-disclosing)", err)
	}
	if _, err := gw.GetInterface(ctx, ifNull.ID, readA); !errors.Is(err, storage.ErrInterfaceNotFound) {
		t.Errorf("get component-less if-null under A-scope = %v, want ErrInterfaceNotFound", err)
	}
	// All-scope sees all three, including the component-less one.
	if all3, err := gw.ListInterfaces(ctx, all); err != nil || len(all3) != 3 {
		t.Fatalf("all-scope list = %d (err %v), want 3", len(all3), err)
	}

	// Cascade CREATE: A-scope can create under A, is forbidden under B and
	// component-less.
	ifA2, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "if-a2", Type: "tcp", Component: strptr("comp-a")}, readA)
	if err != nil {
		t.Errorf("create under A with A-scope = %v, want ok", err)
	}
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "if-b2", Type: "tcp", Component: strptr("comp-b")}, readA); !errors.Is(err, storage.ErrInterfaceForbidden) {
		t.Errorf("create under B with A-scope = %v, want ErrInterfaceForbidden", err)
	}
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "if-null2", Type: "icmp"}, readA); !errors.Is(err, storage.ErrInterfaceForbidden) {
		t.Errorf("create component-less with A-scope = %v, want ErrInterfaceForbidden", err)
	}

	// Cascade UPDATE/DELETE: out of read scope is 404; readable but not actionable
	// is 403.
	if _, err := gw.UpdateInterface(ctx, "", ifB.ID, storage.InterfacePatch{Node: strptr("")}, readA, readA); !errors.Is(err, storage.ErrInterfaceNotFound) {
		t.Errorf("update if-b under A-scope = %v, want ErrInterfaceNotFound", err)
	}
	if _, err := gw.UpdateInterface(ctx, "", ifA.ID, storage.InterfacePatch{Params: []byte(`{"target":"10.0.0.9"}`)}, readA, scope.Set{}); !errors.Is(err, storage.ErrInterfaceForbidden) {
		t.Errorf("update if-a in-read not-action = %v, want ErrInterfaceForbidden", err)
	}
	upd, err := gw.UpdateInterface(ctx, "", ifA.ID, storage.InterfacePatch{Params: []byte(`{"target":"10.0.0.9"}`)}, readA, readA)
	if err != nil || string(upd.Params) != `{"target": "10.0.0.9"}` {
		t.Fatalf("update if-a params = %q (err %v)", string(upd.Params), err)
	}
	if err := gw.DeleteInterface(ctx, "", ifB.ID, readA, readA); !errors.Is(err, storage.ErrInterfaceNotFound) {
		t.Errorf("delete if-b under A-scope = %v, want ErrInterfaceNotFound", err)
	}

	// Delete refused while a task still references the interface.
	if _, err := gw.CreateTask(ctx, "", storage.TaskSpec{Mode: "poll", InterfaceID: ifA.ID}, all); err != nil {
		t.Fatalf("create task on if-a: %v", err)
	}
	if err := gw.DeleteInterface(ctx, "", ifA.ID, all, all); !errors.Is(err, storage.ErrInterfaceOccupied) {
		t.Errorf("delete occupied if-a = %v, want ErrInterfaceOccupied", err)
	}
	if err := gw.DeleteInterface(ctx, "", ifA2.ID, all, all); err != nil {
		t.Errorf("delete unoccupied if-a2 = %v, want ok", err)
	}

	// FK faults.
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "bad-type", Type: "galaxy"}, all); !errors.Is(err, storage.ErrUnknownInterfaceType) {
		t.Errorf("unknown type = %v, want ErrUnknownInterfaceType", err)
	}
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "bad-comp", Type: "tcp", Component: strptr("nope")}, all); !errors.Is(err, storage.ErrInterfaceComponentNotFound) {
		t.Errorf("unknown component = %v, want ErrInterfaceComponentNotFound", err)
	}
	// Duplicate name ON THE SAME COMPONENT is refused (if-a still lives on comp-a).
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "if-a", Type: "tcp", Component: strptr("comp-a")}, all); !errors.Is(err, storage.ErrInterfaceExists) {
		t.Errorf("dup name on same component = %v, want ErrInterfaceExists", err)
	}

	// Audit rows: creates (if-a, if-b, if-null, if-a2) + update (if-a) + deletes
	// (if-a2) = check the interface resource is audited.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("audit connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'interface'`).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n == 0 {
		t.Errorf("interface audit rows = 0, want the create/update/delete trail")
	}
}

// TestInterfaceNamePerComponent proves the identity change: an interface name is
// unique WITHIN its component, not globally. Two different components can each own
// an interface with the same friendly name, but a duplicate name on ONE component
// is refused (a 409 via ErrInterfaceExists).
func TestInterfaceNamePerComponent(t *testing.T) {
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

	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "comp-x", ComponentType: "display"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "comp-y", ComponentType: "display"}, all)

	// The SAME friendly name on two DIFFERENT components: both succeed, and they are
	// distinct rows (distinct surrogate ids).
	onX, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "mgmt", Type: "tcp", Component: strptr("comp-x")}, all)
	if err != nil {
		t.Fatalf("create mgmt on comp-x: %v", err)
	}
	onY, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "mgmt", Type: "tcp", Component: strptr("comp-y")}, all)
	if err != nil {
		t.Fatalf("create mgmt on comp-y (same name, different component): %v", err)
	}
	if onX.ID == onY.ID {
		t.Fatalf("per-component names collided on one id: %s", onX.ID)
	}

	// A duplicate name on the SAME component is refused.
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Name: "mgmt", Type: "icmp", Component: strptr("comp-x")}, all); !errors.Is(err, storage.ErrInterfaceExists) {
		t.Errorf("dup mgmt on comp-x = %v, want ErrInterfaceExists (409)", err)
	}
}
