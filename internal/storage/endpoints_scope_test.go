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
	compA := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "comp-a"}, all)
	compB := mustCreateComponent(t, gw, storage.ComponentSpec{Name: "comp-b"}, all)
	readA := scope.Set{IDs: []string{compA.ID}}
	_ = compB

	// Owner (all) creates a tcp interface on A, on B, and a component-less icmp one.
	// The interface is named by its transport, so its Name is its Type.
	ifA, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: strptr("comp-a")}, all)
	if err != nil {
		t.Fatalf("create tcp on comp-a: %v", err)
	}
	ifB, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: strptr("comp-b")}, all)
	if err != nil {
		t.Fatalf("create tcp on comp-b: %v", err)
	}
	ifNull, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "icmp"}, all)
	if err != nil {
		t.Fatalf("create component-less icmp: %v", err)
	}
	if ifA.Component == nil || *ifA.Component != "comp-a" || ifA.Name != "tcp" {
		t.Fatalf("comp-a interface = name %q component %v, want name tcp on comp-a", ifA.Name, ifA.Component)
	}

	// Cascade READ: A-scope sees only A's interface, not B's, not the component-less.
	got, err := gw.ListEndpoints(ctx, readA)
	if err != nil || len(got) != 1 || got[0].ID != ifA.ID {
		t.Fatalf("A-scope list = %+v (err %v), want just A's interface", got, err)
	}
	if _, err := gw.GetEndpoint(ctx, ifA.ID, readA); err != nil {
		t.Fatalf("get A's interface under A-scope: %v", err)
	}
	if _, err := gw.GetEndpoint(ctx, ifB.ID, readA); !errors.Is(err, storage.ErrEndpointNotFound) {
		t.Errorf("get B's interface under A-scope = %v, want ErrEndpointNotFound (non-disclosing)", err)
	}
	if _, err := gw.GetEndpoint(ctx, ifNull.ID, readA); !errors.Is(err, storage.ErrEndpointNotFound) {
		t.Errorf("get component-less interface under A-scope = %v, want ErrEndpointNotFound", err)
	}
	if all3, err := gw.ListEndpoints(ctx, all); err != nil || len(all3) != 3 {
		t.Fatalf("all-scope list = %d (err %v), want 3", len(all3), err)
	}

	// Cascade CREATE: A-scope creates a second (different-transport) interface on A,
	// is forbidden under B and component-less.
	ifA2, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "icmp", Component: strptr("comp-a")}, readA)
	if err != nil {
		t.Errorf("create under A with A-scope = %v, want ok", err)
	}
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "http", Component: strptr("comp-b")}, readA); !errors.Is(err, storage.ErrEndpointForbidden) {
		t.Errorf("create under B with A-scope = %v, want ErrEndpointForbidden", err)
	}
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "http"}, readA); !errors.Is(err, storage.ErrEndpointForbidden) {
		t.Errorf("create component-less with A-scope = %v, want ErrEndpointForbidden", err)
	}

	// Cascade UPDATE/DELETE: out of read scope is 404; readable but not actionable
	// is 403.
	if _, err := gw.UpdateEndpoint(ctx, "", ifB.ID, storage.EndpointPatch{Params: []byte(`{"target":"x"}`)}, readA, readA); !errors.Is(err, storage.ErrEndpointNotFound) {
		t.Errorf("update B's interface under A-scope = %v, want ErrEndpointNotFound", err)
	}
	if _, err := gw.UpdateEndpoint(ctx, "", ifA.ID, storage.EndpointPatch{Params: []byte(`{"target":"10.0.0.9"}`)}, readA, scope.Set{}); !errors.Is(err, storage.ErrEndpointForbidden) {
		t.Errorf("update A's interface in-read not-action = %v, want ErrEndpointForbidden", err)
	}
	upd, err := gw.UpdateEndpoint(ctx, "", ifA.ID, storage.EndpointPatch{Params: []byte(`{"target":"10.0.0.9"}`)}, readA, readA)
	if err != nil || string(upd.Params) != `{"target": "10.0.0.9"}` {
		t.Fatalf("update A's interface params = %q (err %v)", string(upd.Params), err)
	}
	if err := gw.DeleteEndpoint(ctx, "", ifB.ID, readA, readA); !errors.Is(err, storage.ErrEndpointNotFound) {
		t.Errorf("delete B's interface under A-scope = %v, want ErrEndpointNotFound", err)
	}

	// Deleting an interface CASCADES its derived task; it is never refused for
	// having one. Both A's interfaces delete cleanly.
	if err := gw.DeleteEndpoint(ctx, "", ifA.ID, all, all); err != nil {
		t.Errorf("delete A's interface (with derived task) = %v, want ok (task cascades)", err)
	}
	if err := gw.DeleteEndpoint(ctx, "", ifA2.ID, all, all); err != nil {
		t.Errorf("delete A's second interface = %v, want ok", err)
	}

	// FK / value faults.
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "galaxy"}, all); !errors.Is(err, storage.ErrUnknownTransport) {
		t.Errorf("unknown type = %v, want ErrUnknownTransport", err)
	}
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: strptr("nope")}, all); !errors.Is(err, storage.ErrEndpointComponentNotFound) {
		t.Errorf("unknown component = %v, want ErrEndpointComponentNotFound", err)
	}

	// Audit rows: the endpoint resource is audited across create/update/delete.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("audit connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from audit_log where resource = 'endpoint'`).Scan(&n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n == 0 {
		t.Errorf("endpoint audit rows = 0, want the create/update/delete trail")
	}
}

// TestInterfaceProtocolNamed proves the identity model: an endpoint is named by
// its transport, unique WITHIN its component. Two different components can each
// own a tcp endpoint, but a second endpoint of the same transport on ONE
// component is refused (a 409 via ErrEndpointExists).
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

	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "comp-x"}, all)
	mustCreateComponent(t, gw, storage.ComponentSpec{Name: "comp-y"}, all)

	// The same transport on two different components: both succeed, both named by
	// the protocol, distinct rows (distinct surrogate ids).
	onX, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: strptr("comp-x")}, all)
	if err != nil {
		t.Fatalf("create tcp on comp-x: %v", err)
	}
	onY, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: strptr("comp-y")}, all)
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
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{Transport: "tcp", Component: strptr("comp-x")}, all); !errors.Is(err, storage.ErrEndpointExists) {
		t.Errorf("dup tcp on comp-x = %v, want ErrEndpointExists (409)", err)
	}
}
