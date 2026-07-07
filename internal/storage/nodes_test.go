package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

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
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_name, node_name, spec, enabled) values ('t-icmp', 'poll', 'disp-1-icmp', 'node-a', '{"probe":"icmp"}'::jsonb, true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_name, node_name, spec, enabled) values ('t-off', 'poll', 'disp-1-icmp', 'node-a', '{}'::jsonb, false)`); err != nil {
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
