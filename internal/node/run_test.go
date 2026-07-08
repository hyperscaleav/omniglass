package node_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/bus"
	"github.com/hyperscaleav/omniglass/internal/node"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestNodeRunOnce drives the whole node path as the user would: a real embedded
// bus, a real HTTP claim, and node.Run pulling the worklist and heartbeating.
// Skipped under -short.
func TestNodeRunOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres + nats-server")
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

	// Enroll the node (create + mint token). The stored form is hex sha256.
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "site-a"}, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "enroll-token-a"
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", "site-a", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}

	// Seed a component + interface + enabled task on the node.
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1", ComponentType: "display"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, node_name, params) values ('disp-1-icmp', 'icmp', 'disp-1', 'site-a', '{"target":"10.0.0.1"}'::jsonb)`); err != nil {
		t.Fatalf("insert interface: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_id, node_name, spec, enabled) values ('t-icmp', 'poll', (select id from interface where name = 'disp-1-icmp'), 'site-a', '{}'::jsonb, true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	conn.Close(ctx)

	// Start the bus and an API server that advertises it.
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()

	// Run the node once: claim, pull, heartbeat.
	wl, err := node.Run(ctx, node.Config{ServerURL: apiSrv.URL, Name: "site-a", Token: token, Once: true})
	if err != nil {
		t.Fatalf("node run: %v", err)
	}
	if len(wl.Tasks) != 1 || wl.Tasks[0].ID != "t-icmp" {
		t.Fatalf("worklist = %+v, want one t-icmp task", wl.Tasks)
	}

	// The heartbeat landed: last_heartbeat_at is set.
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, err := gw.GetNode(ctx, "site-a", all)
		if err != nil {
			t.Fatalf("get node: %v", err)
		}
		if n.LastHeartbeatAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("last_heartbeat_at not set after node run")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
