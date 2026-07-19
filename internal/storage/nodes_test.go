package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// TestNodeGateway proves the node enrollment lifecycle and the worklist read:
// create, mint (set token), authenticate, claim, heartbeat, and NodeWorklist.
// Skipped under -short.
func TestNodeGateway(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	// Create requires an all-scope grant (node is estate-wide, not tree-scoped).
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a", Description: "lab a"}, scope.Set{}); !errors.Is(err, storage.ErrNodeForbidden) {
		t.Fatalf("create without all-scope: want ErrNodeForbidden, got %v", err)
	}
	n, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a", Description: "lab a"}, all)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	if n.Name != "node-a" || n.Enrolled || n.PrincipalID == "" {
		t.Fatalf("fresh node: want name node-a, not enrolled, with a principal id, got %+v", n)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-a"}, all); !errors.Is(err, storage.ErrNodeExists) {
		t.Fatalf("duplicate node: want ErrNodeExists, got %v", err)
	}

	// The node is a first-class principal of kind='node' (not a standalone island):
	// its detail row's principal_id points to a principal row of that kind.
	probe, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer probe.Close(ctx)
	var kind string
	if err := probe.QueryRow(ctx, `select kind from principal where id = $1`, n.PrincipalID).Scan(&kind); err != nil {
		t.Fatalf("load node principal: %v", err)
	}
	if kind != "node" {
		t.Fatalf("node principal kind = %q, want node", kind)
	}

	// Mint: store the token hash.
	token := "enroll-secret-a"
	if _, err := gw.SetEnrollmentToken(ctx, "", "node-a", hashHex(token), all); err != nil {
		t.Fatalf("set enrollment token: %v", err)
	}
	if _, err := gw.SetEnrollmentToken(ctx, "", "ghost", hashHex(token), all); !errors.Is(err, storage.ErrNodeNotFound) {
		t.Fatalf("mint unknown node: want ErrNodeNotFound, got %v", err)
	}

	// The enrollment secret is a bearer credential ROW on the node principal (the
	// same machinery a service token uses), not a bespoke column.
	var credKind string
	if err := probe.QueryRow(ctx,
		`select kind from credential where principal_id = $1`, n.PrincipalID).Scan(&credKind); err != nil {
		t.Fatalf("load node credential: %v", err)
	}
	if credKind != "bearer" {
		t.Fatalf("node credential kind = %q, want bearer", credKind)
	}

	// Authenticate (the NATS callback path): right hash true, wrong hash false.
	if ok, err := gw.AuthenticateNode(ctx, "node-a", hashHex(token)); err != nil || !ok {
		t.Fatalf("authenticate right: want true, got (%v,%v)", ok, err)
	}
	if ok, _ := gw.AuthenticateNode(ctx, "node-a", hashHex("wrong")); ok {
		t.Fatalf("authenticate wrong: want false")
	}
	if ok, _ := gw.AuthenticateNode(ctx, "ghost", hashHex(token)); ok {
		t.Fatalf("authenticate unknown node: want false")
	}

	// Claim: right token sets enrolled_at; wrong token is rejected.
	claimed, err := gw.ClaimNode(ctx, "node-a", hashHex(token))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !claimed.Enrolled || claimed.EnrolledAt == nil {
		t.Fatalf("claimed node: want enrolled with enrolled_at, got %+v", claimed)
	}
	if _, err := gw.ClaimNode(ctx, "node-a", hashHex("wrong")); !errors.Is(err, storage.ErrEnrollmentInvalid) {
		t.Fatalf("claim wrong token: want ErrEnrollmentInvalid, got %v", err)
	}

	// Heartbeat updates last_heartbeat_at.
	if err := gw.RecordHeartbeat(ctx, "node-a"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	got, err := gw.GetNode(ctx, "node-a", all)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got.LastHeartbeatAt == nil {
		t.Fatalf("last_heartbeat_at not set after heartbeat")
	}

	// Worklist: seed a component + interface + enabled task bound to node-a.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1", ComponentType: "display"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, node_name, params) values ('disp-1-icmp', 'icmp', 'disp-1', 'node-a', '{"target":"10.0.0.1"}'::jsonb)`); err != nil {
		t.Fatalf("insert interface: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_id, spec, enabled) values ('t-icmp', 'poll', (select id from interface where name = 'disp-1-icmp'), '{"probe":"icmp"}'::jsonb, true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_id, spec, enabled) values ('t-off', 'poll', (select id from interface where name = 'disp-1-icmp'), '{}'::jsonb, false)`); err != nil {
		t.Fatalf("insert disabled task: %v", err)
	}

	wl, err := gw.NodeWorklist(ctx, "node-a")
	if err != nil {
		t.Fatalf("worklist: %v", err)
	}
	if len(wl.Tasks) != 1 {
		t.Fatalf("worklist tasks = %d, want 1 (enabled only)", len(wl.Tasks))
	}
	if wl.Tasks[0].ID != "t-icmp" || wl.Tasks[0].InterfaceType != "icmp" || wl.Tasks[0].Mode != "poll" {
		t.Fatalf("worklist task: got %+v", wl.Tasks[0])
	}
	if wl.ConfigGeneration == 0 {
		t.Fatalf("config_generation = 0, want the interface's updated_at epoch")
	}

	// An unknown node worklist is empty, not an error.
	empty, err := gw.NodeWorklist(ctx, "no-such-node")
	if err != nil {
		t.Fatalf("worklist unknown node: %v", err)
	}
	if len(empty.Tasks) != 0 || empty.ConfigGeneration != 0 {
		t.Fatalf("unknown node worklist: want empty/0, got %+v", empty)
	}
}

// TestNodeNameSubjectSafety proves the node-name invariant is enforced at the
// Storage Gateway, not only by validNodeName at the API layer: a node name
// becomes a NATS subject token (og.v1.telemetry.<name>, ...) and its per-node
// subject grant, so a name containing a dot, a subject wildcard ('*', '>'), or
// whitespace must be rejected by the node table's CHECK constraint, with
// CreateNode surfacing a clean ErrInvalidNodeName rather than a raw pg error.
// Skipped under -short.
func TestNodeNameSubjectSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	for _, bad := range []string{"bad.name", "*", ">", "has space", "tab\tname"} {
		if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: bad, Description: "subject-unsafe"}, all); !errors.Is(err, storage.ErrInvalidNodeName) {
			t.Fatalf("create node %q: want ErrInvalidNodeName, got %v", bad, err)
		}
		if _, err := gw.GetNode(ctx, bad, all); !errors.Is(err, storage.ErrNodeNotFound) {
			t.Fatalf("get node %q after rejected create: want ErrNodeNotFound (no row), got %v", bad, err)
		}
	}

	// A subject-safe name still succeeds.
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "node-safe", Description: "ok"}, all); err != nil {
		t.Fatalf("create valid node: %v", err)
	}
}

// TestNodeIdentityAndEdit covers the N1 identity fields (display_name, location)
// and the UpdateNode patch path: nil-unchanged semantics, location set / change /
// clear, an unknown location rejected, name immutability, and the location FK's
// ON DELETE SET NULL. See .claude/superpowers/specs/2026-07-19-node-identity-and-edit-design.md.
func TestNodeIdentityAndEdit(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	closet, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "hq-closet", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	other, err := gw.CreateLocation(ctx, "", storage.LocationSpec{Name: "east-rack", LocationType: "campus"}, all)
	if err != nil {
		t.Fatalf("create other location: %v", err)
	}

	// Create carries display_name and location.
	n, err := gw.CreateNode(ctx, "", storage.NodeSpec{
		Name: "edge-hq", DisplayName: "HQ Closet Node", Description: "rack 3", LocationName: &closet.Name,
	}, all)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	if n.DisplayName != "HQ Closet Node" || n.LocationName == nil || *n.LocationName != "hq-closet" {
		t.Fatalf("fresh node identity: got display=%q location=%v", n.DisplayName, n.LocationName)
	}

	str := func(s string) *string { return &s }

	// Patch display_name only: description and location are untouched (nil = unchanged).
	up, err := gw.UpdateNode(ctx, "", "edge-hq", storage.NodePatch{DisplayName: str("HQ Node")}, all, all)
	if err != nil {
		t.Fatalf("update display_name: %v", err)
	}
	if up.DisplayName != "HQ Node" || up.Description != "rack 3" || up.LocationName == nil || *up.LocationName != "hq-closet" {
		t.Fatalf("patch display only: got %+v", up)
	}
	if up.Name != "edge-hq" {
		t.Fatalf("name must be immutable, got %q", up.Name)
	}

	// Move the location to another valid one.
	up, err = gw.UpdateNode(ctx, "", "edge-hq", storage.NodePatch{LocationName: &other.Name}, all, all)
	if err != nil {
		t.Fatalf("relocate: %v", err)
	}
	if up.LocationName == nil || *up.LocationName != "east-rack" {
		t.Fatalf("relocate: got %v", up.LocationName)
	}

	// Clear the location (a pointer to "").
	up, err = gw.UpdateNode(ctx, "", "edge-hq", storage.NodePatch{LocationName: str("")}, all, all)
	if err != nil {
		t.Fatalf("clear location: %v", err)
	}
	if up.LocationName != nil {
		t.Fatalf("clear location: want nil, got %v", *up.LocationName)
	}

	// An unknown location is rejected (the location FK), not silently applied.
	if _, err := gw.UpdateNode(ctx, "", "edge-hq", storage.NodePatch{LocationName: str("ghost-room")}, all, all); !errors.Is(err, storage.ErrLocationNotFound) {
		t.Fatalf("unknown location: want ErrLocationNotFound, got %v", err)
	}

	// Estate-wide: an update without all-scope is forbidden; an unknown node is not found.
	if _, err := gw.UpdateNode(ctx, "", "edge-hq", storage.NodePatch{DisplayName: str("x")}, scope.Set{}, scope.Set{}); !errors.Is(err, storage.ErrNodeForbidden) {
		t.Fatalf("update without all-scope: want ErrNodeForbidden, got %v", err)
	}
	if _, err := gw.UpdateNode(ctx, "", "ghost", storage.NodePatch{DisplayName: str("x")}, all, all); !errors.Is(err, storage.ErrNodeNotFound) {
		t.Fatalf("update unknown node: want ErrNodeNotFound, got %v", err)
	}

	// The location FK is ON DELETE SET NULL: place the node, delete its location,
	// the node survives with a cleared placement (deleted via a probe, since
	// location-delete guards are not the unit under test).
	if _, err := gw.UpdateNode(ctx, "", "edge-hq", storage.NodePatch{LocationName: &closet.Name}, all, all); err != nil {
		t.Fatalf("re-place before delete: %v", err)
	}
	probe, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer probe.Close(ctx)
	if _, err := probe.Exec(ctx, `delete from location where name = 'hq-closet'`); err != nil {
		t.Fatalf("delete location: %v", err)
	}
	got, err := gw.GetNode(ctx, "edge-hq", all)
	if err != nil {
		t.Fatalf("get node after location delete: %v", err)
	}
	if got.LocationName != nil {
		t.Fatalf("ON DELETE SET NULL: want nil location, got %v", *got.LocationName)
	}
}

// TestDeleteNode covers node decommission (N3): a hard delete of the node
// principal cascades its interfaces, their derived tasks, and its node-owned tag
// bindings, while a component's own telemetry (owner arc = component, node_id
// null) survives. Plus the all-scope gate and the unknown-node not-found.
func TestDeleteNode(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "dsp-1", ComponentType: "dsp"}, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	node, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-del"}, all)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	comp, nodeName := "dsp-1", "edge-del"
	if _, err := gw.CreateInterface(ctx, "", storage.InterfaceSpec{
		Type: "tcp", Component: &comp, Node: &nodeName, Params: []byte(`{"target":"10.0.0.1:80"}`),
	}, all); err != nil {
		t.Fatalf("create interface: %v", err)
	}
	// A component-owned datapoint (owner arc = component, node_id null): it must
	// survive the node's deletion.
	if err := gw.InsertMetricDatapoints(ctx, []storage.MetricDatapointEvent{
		{OwnerKind: "component", OwnerID: "dsp-1", Key: "tcp.open", Instance: "tcp", Value: 1, Source: "tcp", TS: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("insert component datapoint: %v", err)
	}
	// A node-owned tag binding (cascades on delete).
	mustNodeTag(t, gw, node.Name)

	probe, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("probe connect: %v", err)
	}
	defer probe.Close(ctx)
	count := func(q string, args ...any) int {
		var n int
		if err := probe.QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("count (%s): %v", q, err)
		}
		return n
	}
	if count(`select count(*) from interface where node_name = $1`, nodeName) != 1 {
		t.Fatalf("precondition: want 1 interface on the node")
	}
	if count(`select count(*) from task`) != 1 {
		t.Fatalf("precondition: want 1 derived task")
	}
	if count(`select count(*) from tag_binding where node_id = $1`, node.PrincipalID) != 1 {
		t.Fatalf("precondition: want 1 node tag binding")
	}

	// The all-scope gate and unknown-node, then the delete.
	if err := gw.DeleteNode(ctx, "", nodeName, scope.Set{}, scope.Set{}); !errors.Is(err, storage.ErrNodeForbidden) {
		t.Fatalf("delete without all-scope: want ErrNodeForbidden, got %v", err)
	}
	if err := gw.DeleteNode(ctx, "", "ghost", all, all); !errors.Is(err, storage.ErrNodeNotFound) {
		t.Fatalf("delete unknown node: want ErrNodeNotFound, got %v", err)
	}
	if err := gw.DeleteNode(ctx, "", nodeName, all, all); err != nil {
		t.Fatalf("delete node: %v", err)
	}

	// The node and everything keyed to it are gone; the node principal too.
	if _, err := gw.GetNode(ctx, nodeName, all); !errors.Is(err, storage.ErrNodeNotFound) {
		t.Errorf("node still present after delete: %v", err)
	}
	if n := count(`select count(*) from node where name = $1`, nodeName); n != 0 {
		t.Errorf("node rows = %d, want 0", n)
	}
	if n := count(`select count(*) from principal where id = $1`, node.PrincipalID); n != 0 {
		t.Errorf("node principal rows = %d, want 0", n)
	}
	if n := count(`select count(*) from interface where node_name = $1`, nodeName); n != 0 {
		t.Errorf("interfaces = %d, want 0 (cascade)", n)
	}
	if n := count(`select count(*) from task`); n != 0 {
		t.Errorf("tasks = %d, want 0 (cascade through the interface)", n)
	}
	if n := count(`select count(*) from tag_binding where node_id = $1`, node.PrincipalID); n != 0 {
		t.Errorf("node tag bindings = %d, want 0 (cascade)", n)
	}
	// The component's own telemetry survives (owner arc = component, not the node).
	if n := count(`select count(*) from metric_datapoint where owner_kind = 'component'`); n != 1 {
		t.Errorf("component datapoints = %d, want 1 (must survive the node delete)", n)
	}
}

// mustNodeTag mints a node-applicable key and binds it to the node.
func mustNodeTag(t *testing.T, gw storage.Gateway, nodeName string) {
	t.Helper()
	ctx := context.Background()
	all := scope.Set{All: true}
	if _, err := gw.CreateTag(ctx, "", storage.TagSpec{Name: "rack", AppliesTo: []string{"node"}}, all); err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if _, err := gw.SetTagBinding(ctx, "", "rack", "node", &nodeName, "r5", all, all); err != nil {
		t.Fatalf("bind node tag: %v", err)
	}
}
