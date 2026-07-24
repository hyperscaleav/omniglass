package storage_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The collection tier's references survive a rename for the same reason the estate
// tier's do: every arc stores the owner's primary key, so a rename touches nothing.
//
// This is the tier where getting it wrong is least visible. An orphaned interface
// does not raise an error, it stops being collected: the node's worklist quietly
// comes back one task shorter, and the component's reachability panel goes blank
// rather than red. That silence is why the assertions below read the resolved
// worklist and the component's interface list, not just the row count.
func TestCollectionReferencesSurviveARename(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn,
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "site", LocationType: "campus"}, all); err != nil {
		t.Fatalf("campus: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "old-room", LocationType: "room", ParentName: strptr("site")}, all); err != nil {
		t.Fatalf("room: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "old-codec", LocationName: strptr("old-room")}, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{
		Name: "edge", LocationName: strptr("old-room")}, all); err != nil {
		t.Fatalf("node: %v", err)
	}
	// One interface carrying both arcs at once: owned by the component, placed on
	// the node. Creating it derives the reachability task, so the worklist below is
	// the real resolved thing rather than a hand-built row.
	iface, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{
		Type: "icmp", Component: strptr("old-codec"), Node: strptr("edge"),
		Params: []byte(`{"target":"10.0.0.1"}`)}, all)
	if err != nil {
		t.Fatalf("interface: %v", err)
	}
	if err := gw.InsertMetricDatapoints(ctx, []storage.MetricDatapointEvent{{
		OwnerKind: "component", OwnerID: "old-codec", Key: "tcp.open", Value: 1, Source: "test"}}); err != nil {
		t.Fatalf("datapoint: %v", err)
	}

	// The renames an operator can actually perform. A node's name is immutable (it
	// is the enrollment identity), so the node arc is exercised below instead.
	if _, err := gw.UpdateComponent(ctx, "", "old-codec",
		storage.ComponentPatch{Name: strptr("new-codec")}, all, all); err != nil {
		t.Fatalf("rename component: %v", err)
	}
	if _, err := gw.UpdateLocation(ctx, "", "old-room",
		storage.LocationPatch{Name: strptr("new-room")}, all, all); err != nil {
		t.Fatalf("rename location: %v", err)
	}

	got, err := gw.GetInterface(ctx, iface.ID, all)
	if err != nil {
		t.Fatalf("get interface after rename: %v", err)
	}
	if got.Component == nil || *got.Component != "new-codec" {
		t.Errorf("interface component = %v, want new-codec (the arc should follow the rename)", deref(got.Component))
	}
	if got.ComponentID == nil || *got.ComponentID != *iface.ComponentID {
		t.Errorf("interface component id = %v, want unchanged %v", got.ComponentID, iface.ComponentID)
	}
	if got.Node == nil || *got.Node != "edge" {
		t.Errorf("interface node = %v, want edge", deref(got.Node))
	}

	// The reachability read addresses the component by its new name; before the
	// conversion this returned nothing, because the arc still held the old one.
	ifaces, err := gw.ListComponentInterfaces(ctx, "new-codec")
	if err != nil {
		t.Fatalf("list component interfaces: %v", err)
	}
	if len(ifaces) != 1 {
		t.Fatalf("interfaces on the renamed component = %d, want 1", len(ifaces))
	}
	if ifaces[0].NodeName != "edge" {
		t.Errorf("projected node = %q, want edge", ifaces[0].NodeName)
	}

	// The node's placement follows its location's rename.
	n, err := gw.GetNode(ctx, "edge", all)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if n.LocationName == nil || *n.LocationName != "new-room" {
		t.Errorf("node location = %v, want new-room", deref(n.LocationName))
	}

	// The datapoint still resolves under the component's new name.
	dp, err := gw.LatestMetric(ctx, "new-codec", "tcp.open")
	if err != nil {
		t.Fatalf("latest metric: %v", err)
	}
	if dp == nil {
		t.Fatal("no datapoint under the new name: the owner arc did not follow the rename")
	}

	// The node arc, at the level it is actually guaranteed. A node's name is not
	// patchable, so this renames it underneath the gateway: the point is that the
	// interface placement is keyed by principal_id and therefore has nothing to
	// rewrite. The worklist resolving under the new name is the observable proof.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `update node set name = 'edge-2' where name = 'edge'`); err != nil {
		t.Fatalf("rename node: %v", err)
	}
	wl, err := gw.NodeWorklist(ctx, "edge-2")
	if err != nil {
		t.Fatalf("worklist: %v", err)
	}
	if len(wl.Tasks) != 1 {
		t.Fatalf("tasks for the renamed node = %d, want 1 (its interface lost its placement)", len(wl.Tasks))
	}
	if wl.Tasks[0].InterfaceName != iface.Name {
		t.Errorf("worklist interface = %q, want %q", wl.Tasks[0].InterfaceName, iface.Name)
	}
}

// deref renders an optional reference for a failure message: the pointer itself
// says nothing about which name came back.
func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
