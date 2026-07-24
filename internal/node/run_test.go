package node_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
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
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, node_name, params) values ('disp-1-icmp', (select id from interface_type where name = 'icmp'), (select id from component where name = 'disp-1'), (select principal_id from node where name = 'site-a'), '{"target":"10.0.0.1"}'::jsonb)`); err != nil {
		t.Fatalf("insert interface: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_id, spec, enabled) values ('t-icmp', 'poll', (select id from interface where name = 'disp-1-icmp'), '{}'::jsonb, true)`); err != nil {
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

// TestNodeVerdictPerInterface pins the per-component-name regression: two
// components each carry an interface named "api" on the SAME node, over different
// transports (http and tcp). Before interface names went per-component this
// collision could not occur; now it is the common case. The node's verdict-dedup
// map must be keyed by the node-unique task id, not the interface name, or the
// second component's reachability verdict is silently suppressed as a repeat of the
// first. Both verdicts must land, which also proves http and tcp both route to the
// tcp-connect reach probe. Skipped under -short.
func TestNodeVerdictPerInterface(t *testing.T) {
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

	// A live loopback port both probes find open, so each tcp check yields a
	// concrete "up" verdict (not an inconclusive skip).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	target := ln.Addr().String()

	// Enroll the node.
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "site-a"}, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "enroll-token-verdict"
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", "site-a", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}

	// Two components, each with an interface named "tcp" on the same node, each
	// with its own poll task. Same friendly name, different components: the
	// per-component-uniqueness case a global name key could not represent.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	for _, c := range []struct{ comp, itype string }{
		{"disp-1", "http"},
		{"disp-2", "tcp"},
	} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: c.comp}, all); err != nil {
			t.Fatalf("create component %s: %v", c.comp, err)
		}
		if _, err := conn.Exec(ctx,
			`insert into interface (name, type, component, node_name, params) values ('api', (select id from interface_type where name = $1), (select id from component where name = $2), (select principal_id from node where name = 'site-a'), $3::jsonb)`,
			c.itype, c.comp, fmt.Sprintf(`{"target":%q}`, target)); err != nil {
			t.Fatalf("insert interface for %s: %v", c.comp, err)
		}
		if _, err := conn.Exec(ctx,
			`insert into task (id, mode, interface_id, spec, enabled) values ($1, 'poll', (select id from interface where component = (select id from component where name = $2) and name = 'api'), '{}'::jsonb, true)`,
			"t-"+c.comp, c.comp); err != nil {
			t.Fatalf("insert task for %s: %v", c.comp, err)
		}
	}
	conn.Close(ctx)

	// Bus + API that advertises it (the bus starts the telemetry consumer).
	srv, err := bus.New(bus.Config{Host: "127.0.0.1", Port: -1}, gw)
	if err != nil {
		t.Fatalf("start bus: %v", err)
	}
	defer srv.Shutdown()
	apiSrv := httptest.NewServer(api.NewHandler(gw, api.WithNatsURL(srv.ClientURL())))
	defer apiSrv.Close()

	// Run the node once: it probes both interfaces and publishes a verdict per task.
	wl, err := node.Run(ctx, node.Config{ServerURL: apiSrv.URL, Name: "site-a", Token: token, Once: true})
	if err != nil {
		t.Fatalf("node run: %v", err)
	}
	if len(wl.Tasks) != 2 {
		t.Fatalf("worklist = %d tasks, want 2", len(wl.Tasks))
	}

	// Both components' reachability verdicts land. The regression: keyed by interface
	// name, disp-2's "api" verdict was dropped as a repeat of disp-1's, so disp-2 would
	// have no verdict here. disp-1 (over http) also proves the transport routes to the
	// tcp-connect reach probe.
	deadline := time.Now().Add(5 * time.Second)
	for _, comp := range []string{"disp-1", "disp-2"} {
		for {
			v, err := gw.LatestState(ctx, comp, "interface.reachable", "api")
			if err != nil {
				t.Fatalf("latest state %s: %v", comp, err)
			}
			if v != nil && v.Value == "up" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("component %s has no up verdict (%+v): name-key collision suppressed it?", comp, v)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}
