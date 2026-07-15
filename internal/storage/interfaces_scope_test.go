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
// component-less one. The interface is protocol-named (name = its transport/type),
// so on one component each transport is unique. Deleting an interface cascades its
// derived task away.
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

	// Owner (all) creates a tcp interface on A, on B, and a component-less icmp one.
	// The interface is named by its transport, so its Name is its Type.
	ifA, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr("comp-a")}, all)
	if err != nil {
		t.Fatalf("create tcp on comp-a: %v", err)
	}
	ifB, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr("comp-b")}, all)
	if err != nil {
		t.Fatalf("create tcp on comp-b: %v", err)
	}
	ifNull, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "icmp"}, all)
	if err != nil {
		t.Fatalf("create component-less icmp: %v", err)
	}
	if ifA.Component == nil || *ifA.Component != "comp-a" || ifA.Name != "tcp" {
		t.Fatalf("comp-a interface = name %q component %v, want name tcp on comp-a", ifA.Name, ifA.Component)
	}

	// Cascade READ: A-scope sees only A's interface, not B's, not the component-less.
	got, err := gw.ListInterfaces(ctx, readA)
	if err != nil || len(got) != 1 || got[0].ID != ifA.ID {
		t.Fatalf("A-scope list = %+v (err %v), want just A's interface", got, err)
	}
	if _, err := gw.GetInterface(ctx, ifA.ID, readA); err != nil {
		t.Fatalf("get A's interface under A-scope: %v", err)
	}
	if _, err := gw.GetInterface(ctx, ifB.ID, readA); !errors.Is(err, storage.ErrInterfaceNotFound) {
		t.Errorf("get B's interface under A-scope = %v, want ErrInterfaceNotFound (non-disclosing)", err)
	}
	if _, err := gw.GetInterface(ctx, ifNull.ID, readA); !errors.Is(err, storage.ErrInterfaceNotFound) {
		t.Errorf("get component-less interface under A-scope = %v, want ErrInterfaceNotFound", err)
	}
	if all3, err := gw.ListInterfaces(ctx, all); err != nil || len(all3) != 3 {
		t.Fatalf("all-scope list = %d (err %v), want 3", len(all3), err)
	}

	// Cascade CREATE: A-scope creates a second (different-transport) interface on A,
	// is forbidden under B and component-less.
	ifA2, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "icmp", Component: strptr("comp-a")}, readA)
	if err != nil {
		t.Errorf("create under A with A-scope = %v, want ok", err)
	}
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "http", Component: strptr("comp-b")}, readA); !errors.Is(err, storage.ErrInterfaceForbidden) {
		t.Errorf("create under B with A-scope = %v, want ErrInterfaceForbidden", err)
	}
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "http"}, readA); !errors.Is(err, storage.ErrInterfaceForbidden) {
		t.Errorf("create component-less with A-scope = %v, want ErrInterfaceForbidden", err)
	}

	// Cascade UPDATE/DELETE: out of read scope is 404; readable but not actionable
	// is 403.
	if _, err := gw.UpdateInterface(ctx, "", ifB.ID, storage.InterfacePatch{Params: []byte(`{"target":"x"}`)}, readA, readA); !errors.Is(err, storage.ErrInterfaceNotFound) {
		t.Errorf("update B's interface under A-scope = %v, want ErrInterfaceNotFound", err)
	}
	if _, err := gw.UpdateInterface(ctx, "", ifA.ID, storage.InterfacePatch{Params: []byte(`{"target":"10.0.0.9"}`)}, readA, scope.Set{}); !errors.Is(err, storage.ErrInterfaceForbidden) {
		t.Errorf("update A's interface in-read not-action = %v, want ErrInterfaceForbidden", err)
	}
	upd, err := gw.UpdateInterface(ctx, "", ifA.ID, storage.InterfacePatch{Params: []byte(`{"target":"10.0.0.9"}`)}, readA, readA)
	if err != nil || string(upd.Params) != `{"target": "10.0.0.9"}` {
		t.Fatalf("update A's interface params = %q (err %v)", string(upd.Params), err)
	}
	if err := gw.DeleteInterface(ctx, "", ifB.ID, readA, readA); !errors.Is(err, storage.ErrInterfaceNotFound) {
		t.Errorf("delete B's interface under A-scope = %v, want ErrInterfaceNotFound", err)
	}

	// Deleting an interface CASCADES its derived task; it is never refused for
	// having one. Both A's interfaces delete cleanly.
	if err := gw.DeleteInterface(ctx, "", ifA.ID, all, all); err != nil {
		t.Errorf("delete A's interface (with derived task) = %v, want ok (task cascades)", err)
	}
	if err := gw.DeleteInterface(ctx, "", ifA2.ID, all, all); err != nil {
		t.Errorf("delete A's second interface = %v, want ok", err)
	}

	// FK / value faults.
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "galaxy"}, all); !errors.Is(err, storage.ErrUnknownInterfaceType) {
		t.Errorf("unknown type = %v, want ErrUnknownInterfaceType", err)
	}
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr("nope")}, all); !errors.Is(err, storage.ErrInterfaceComponentNotFound) {
		t.Errorf("unknown component = %v, want ErrInterfaceComponentNotFound", err)
	}

	// Audit rows: the interface resource is audited across create/update/delete.
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

// TestInterfaceProtocolNamed proves the identity model: an interface is named by
// its transport (its type), unique WITHIN its component. Two different components
// can each own a tcp interface, but a second interface of the same transport on ONE
// component is refused (a 409 via ErrInterfaceExists).
func TestInterfaceProtocolNamed(t *testing.T) {
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

	// The same transport on two different components: both succeed, both named by
	// the protocol, distinct rows (distinct surrogate ids).
	onX, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr("comp-x")}, all)
	if err != nil {
		t.Fatalf("create tcp on comp-x: %v", err)
	}
	onY, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr("comp-y")}, all)
	if err != nil {
		t.Fatalf("create tcp on comp-y (same transport, different component): %v", err)
	}
	if onX.Name != "tcp" || onY.Name != "tcp" {
		t.Fatalf("interface names = %q / %q, want both derived to tcp", onX.Name, onY.Name)
	}
	if onX.ID == onY.ID {
		t.Fatalf("per-component protocol names collided on one id: %s", onX.ID)
	}

	// A second interface of the SAME transport on the SAME component is refused.
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{Type: "tcp", Component: strptr("comp-x")}, all); !errors.Is(err, storage.ErrInterfaceExists) {
		t.Errorf("dup tcp on comp-x = %v, want ErrInterfaceExists (409)", err)
	}
}
