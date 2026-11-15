package storage_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// Every write that changes what NodeWorklist serves, beyond the endpoint
// writes, announces the nodes it touched (#1997): a secret an endpoint's
// inputs resolve, a path edge, the platform report window, and a move or a
// rename that changes which secret or which upstream name a task carries.
// Skipped under -short.

// writersFixture is a small placed fleet: two campuses, a building and a room
// under each of the first campus's two buildings, and four components. amp-a
// sits in r1, amp-b in r2, amp-c is unplaced, and amp-a-card is amp-a's child.
// Each component has one snmp endpoint on its own node, and every endpoint's
// community input names a platform secret: shared-comm for amp-a, amp-b and
// the card, other-comm for amp-c.
type writersFixture struct {
	ctx     context.Context
	gw      *storage.PG
	changes <-chan string
}

func newWritersFixture(t *testing.T) *writersFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn, storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	for _, l := range []storage.LocationSpec{
		{Name: "hq", LocationType: "campus"},
		{Name: "hq2", LocationType: "campus"},
		{Name: "hq-b1", LocationType: "building", ParentName: strptr("hq")},
		{Name: "hq-b2", LocationType: "building", ParentName: strptr("hq")},
		{Name: "r1", LocationType: "room", ParentName: strptr("hq-b1")},
		{Name: "r2", LocationType: "room", ParentName: strptr("hq-b2")},
	} {
		if _, err := gw.CreateLocation(ctx, "", l, all); err != nil {
			t.Fatalf("create location %s: %v", l.Name, err)
		}
	}
	for _, c := range []storage.ComponentSpec{
		{Name: "amp-a", LocationName: strptr("r1")},
		{Name: "amp-b", LocationName: strptr("r2")},
		{Name: "amp-c"},
		{Name: "amp-a-card", ParentName: strptr("amp-a")},
	} {
		if _, err := gw.CreateComponent(ctx, "", c, all, all, all, all); err != nil {
			t.Fatalf("create component %s: %v", c.Name, err)
		}
	}
	for _, s := range []string{"shared-comm", "other-comm"} {
		if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
			Name: s, SecretType: "snmp-community", OwnerKind: "platform",
			Fields: map[string]string{"community": "public"},
		}, all, true); err != nil {
			t.Fatalf("create secret %s: %v", s, err)
		}
	}
	if _, err := gw.CreateDriver(ctx, "", storage.Driver{Name: "snmp-scalar", Label: "SNMP Scalar", Version: "1.0.0", Spec: []byte(snmpSpec)}); err != nil {
		t.Fatal(err)
	}
	drv := "snmp-scalar"
	for i, e := range []struct{ comp, node, secret string }{
		{"amp-a", "edge-a", "shared-comm"},
		{"amp-b", "edge-b", "shared-comm"},
		{"amp-c", "edge-c", "other-comm"},
		{"amp-a-card", "edge-d", "shared-comm"},
	} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: e.node}, all, all); err != nil {
			t.Fatal(err)
		}
		comp, node := e.comp, e.node
		if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
			Driver: &drv, Component: &comp, Node: &node,
			Inputs:     map[string]string{"host": "10.20.4." + string(rune('1'+i)), "community": e.secret},
			SecretRead: all, CanAdmin: true,
		}, all); err != nil {
			t.Fatalf("attach %s: %v", e.comp, err)
		}
	}
	changes, err := gw.WorklistChanges(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		for range changes {
		}
		gw.Close()
	})
	return &writersFixture{ctx: ctx, gw: gw, changes: changes}
}

// quiet fails on any announcement within the settle window: a write that
// changed no node's worklist, or one that was refused, announces nothing.
func (f *writersFixture) quiet(t *testing.T, what string) {
	t.Helper()
	select {
	case n := <-f.changes:
		t.Fatalf("%s: announced %q, want nothing", what, n)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestWorklistWritersSecrets(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWritersFixture(t)
	ctx, gw, all := f.ctx, f.gw, scope.Set{All: true}
	fields := map[string]string{"community": "private"}

	// A platform secret reaches every endpoint whose input names it.
	if _, err := gw.UpdateSecret(ctx, "", "shared-comm", storage.SecretPatch{Fields: fields}, all, all, true, true); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a platform secret rotation", "edge-a", "edge-b", "edge-d")

	// Only the endpoints naming the rotated secret hear it.
	if _, err := gw.UpdateSecret(ctx, "", "other-comm", storage.SecretPatch{Fields: fields}, all, all, true, true); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "another platform secret", "edge-c")

	// A location secret shadows the platform one for what is placed under it.
	loc, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "shared-comm", SecretType: "snmp-community", OwnerKind: "location", OwnerName: strptr("hq-b1"),
		Fields: map[string]string{"community": "b1"},
	}, all, true)
	if err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a location secret create", "edge-a")

	// A component secret reaches the component and its children.
	comp, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "shared-comm", SecretType: "snmp-community", OwnerKind: "component", OwnerName: strptr("amp-b"),
		Fields: map[string]string{"community": "b"},
	}, all, true)
	if err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a component secret create", "edge-b")

	if _, err := gw.UpdateSecret(ctx, "", comp.ID, storage.SecretPatch{Fields: fields}, all, all, true, true); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a component secret rotation", "edge-b")

	if err := gw.DeleteSecret(ctx, "", loc.ID, all, all, true, true); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a location secret delete", "edge-a")

	// A secret no input names changes no worklist.
	unused, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "spare-comm", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "spare"},
	}, all, true)
	if err != nil {
		t.Fatal(err)
	}
	f.quiet(t, "an unused secret create")
	if err := gw.DeleteSecret(ctx, "", unused.ID, all, all, true, true); err != nil {
		t.Fatal(err)
	}
	f.quiet(t, "an unused secret delete")

	// A refused write announces nothing.
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "shared-comm", SecretType: "snmp-community", OwnerKind: "platform",
		Fields: map[string]string{"community": "dup"},
	}, all, true); err == nil {
		t.Fatal("a duplicate platform secret was accepted")
	}
	f.quiet(t, "a refused secret create")
	if _, err := gw.UpdateSecret(ctx, "", "shared-comm", storage.SecretPatch{Fields: map[string]string{"nope": "x"}}, all, all, true, true); err == nil {
		t.Fatal("an undeclared secret field was accepted")
	}
	f.quiet(t, "a refused secret update")
}

func TestWorklistWritersPathEdges(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWritersFixture(t)
	ctx, gw, all := f.ctx, f.gw, scope.Set{All: true}

	// A task carries a path upstream only when the upstream also has an
	// in-band endpoint on the task's node, so amp-b gains one on edge-a.
	comp, node := "amp-b", "edge-a"
	if _, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Transport: "tcp", Component: &comp, Node: &node, Params: []byte(`{"host":"10.20.4.9","port":23}`),
	}, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a second amp-b endpoint", "edge-a")

	// A path edge rides the dependent's tasks on the nodes the upstream shares.
	if _, err := gw.CreateComponentDependency(ctx, "", "amp-a", storage.ComponentDependencySpec{Upstream: "amp-b", Kind: storage.DependencyPath}, all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a path edge add", "edge-a")

	// amp-c's node holds no amp-b endpoint, so its worklist is unchanged.
	if _, err := gw.CreateComponentDependency(ctx, "", "amp-c", storage.ComponentDependencySpec{Upstream: "amp-b", Kind: storage.DependencyPath}, all, all); err != nil {
		t.Fatal(err)
	}
	f.quiet(t, "a path edge with no shared node")

	// The upstream's name rides the dependent's worklist too.
	if _, err := gw.RenameComponent(ctx, "", "amp-b", "amp-b2", all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a path upstream rename", "edge-a")

	if err := gw.DeleteComponentDependency(ctx, "", "amp-a", storage.DependencyPath, "amp-b2", all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a path edge delete", "edge-a")

	// A host edge, and a rename with no path dependents, change no worklist.
	if _, err := gw.CreateComponentDependency(ctx, "", "amp-a", storage.ComponentDependencySpec{Upstream: "amp-c", Kind: storage.DependencyHost}, all, all); err != nil {
		t.Fatal(err)
	}
	f.quiet(t, "a host edge add")
	if _, err := gw.RenameComponent(ctx, "", "amp-c", "amp-c2", all, all); err != nil {
		t.Fatal(err)
	}
	f.quiet(t, "a rename with no path dependents")

	// A refused edge announces nothing.
	if _, err := gw.CreateComponentDependency(ctx, "", "amp-a", storage.ComponentDependencySpec{Upstream: "amp-a", Kind: storage.DependencyPath}, all, all); err == nil {
		t.Fatal("a self edge was accepted")
	}
	f.quiet(t, "a refused path edge")
}

func TestWorklistWritersReportWindow(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWritersFixture(t)
	ctx, gw := f.ctx, f.gw
	every := []string{"edge-a", "edge-b", "edge-c", "edge-d"}

	if _, err := gw.UpsertSettingOverride(ctx, "", "platform", "collection", map[string]any{"report_seconds": 30}, nil); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a report window upsert", every...)

	if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "collection", map[string]any{"report_seconds": 45}); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a report window merge-patch", every...)

	// The same value again, or another namespace, moves no worklist.
	if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "collection", map[string]any{"report_seconds": 45}); err != nil {
		t.Fatal(err)
	}
	f.quiet(t, "an unchanged report window")
	if _, err := gw.MergePatchSettingOverride(ctx, "", "platform", "branding", map[string]any{"product_name": "Glass"}); err != nil {
		t.Fatal(err)
	}
	f.quiet(t, "another namespace")

	if err := gw.DeleteSettingOverride(ctx, "", "platform", "collection"); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a report window restore", every...)

	if err := gw.DeleteAllSettingOverrides(ctx, "", "platform"); err != nil {
		t.Fatal(err)
	}
	f.quiet(t, "a reset with the window at its default")
	if _, err := gw.UpsertSettingOverride(ctx, "", "platform", "collection", map[string]any{"report_seconds": 20}, nil); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a report window upsert", every...)
	if err := gw.DeleteAllSettingOverrides(ctx, "", "platform"); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a factory reset", every...)
}

func TestWorklistWritersMoves(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	f := newWritersFixture(t)
	ctx, gw, all := f.ctx, f.gw, scope.Set{All: true}

	// A move changes which location secrets cascade onto what moved, so the
	// nodes of everything in the moved subtree hear it.
	if _, err := gw.MoveLocation(ctx, "", "hq-b1", storage.LocationMove{ParentName: strptr("hq2")}, all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a location move", "edge-a")

	if _, err := gw.MoveComponent(ctx, "", "amp-b", storage.ComponentMove{LocationName: strptr("r1")}, all, all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a component move", "edge-b")

	if _, err := gw.MoveComponent(ctx, "", "amp-a", storage.ComponentMove{LocationName: strptr("r2")}, all, all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a component move with a child", "edge-a")

	// A child resolves its location secrets from its own placement, never its
	// parent's, but its component secrets along its parents: a reparent moves
	// the whole subtree's chain.
	if _, err := gw.MoveComponent(ctx, "", "amp-a", storage.ComponentMove{ParentName: strptr("amp-c")}, all, all, all); err != nil {
		t.Fatal(err)
	}
	expectChanges(t, f.changes, "a reparent with a child", "edge-a", "edge-d")

	// A refused move announces nothing.
	if _, err := gw.MoveComponent(ctx, "", "amp-a", storage.ComponentMove{LocationName: strptr("nowhere")}, all, all, all); err == nil {
		t.Fatal("a move to an unknown location was accepted")
	}
	f.quiet(t, "a refused component move")
}
