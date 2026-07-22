package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestCollectionEndToEnd drives the reachability datapoint path with the REAL
// binaries an operator runs: `omniglass server` (embedded NATS + the telemetry
// ingest consumer) and `omniglass node` (a real tcp probe, a real protobuf Event
// over JetStream). A node runs one probe against a live listener; the datapoint
// must land in metric_datapoint owned by the target component. This is the
// user-facing entry-point tier: the run-mode wiring, not an in-process call.
func TestCollectionEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("collection e2e: skipped under -short (Postgres testcontainer + go build)")
	}
	ctx := context.Background()
	root := repoRoot(t)

	binPath := filepath.Join(t.TempDir(), "omniglass")
	build := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd/omniglass")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	dsn := storagetest.NewDSN(t)
	addr := "127.0.0.1:" + freePort(t)
	natsPort := freePort(t)

	// Run the real server: it migrates, seeds (the tcp interface_type + datapoint
	// types), and hosts the embedded bus. NATS_URL must match NATS_ADDR so the
	// claim reply advertises a URL the node can dial.
	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := exec.CommandContext(srvCtx, binPath, "server")
	srv.Env = append(os.Environ(),
		"OMNIGLASS_DSN="+dsn,
		"OMNIGLASS_ADDR="+addr,
		"OMNIGLASS_NATS_ADDR=127.0.0.1:"+natsPort,
		"OMNIGLASS_NATS_URL=nats://127.0.0.1:"+natsPort,
		"OMNIGLASS_NATS_STORE_DIR="+t.TempDir(),
	)
	srv.Stdout, srv.Stderr = os.Stderr, os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { cancel(); _ = srv.Wait() })
	pollHealthz(t, "http://"+addr+"/api/v1/healthz")

	// A live listener is the probe's open target.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	target := ln.Addr().String()

	// Provision the node, its enrollment token, the target component, and a tcp
	// interface + task (interface/task have no API yet, so via SQL) against the
	// same DB the server serves.
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "site-a"}, all); err != nil {
		t.Fatalf("create node: %v", err)
	}
	token := "e2e-enroll-token"
	sum := sha256.Sum256([]byte(token))
	if _, err := gw.SetEnrollmentToken(ctx, "", "site-a", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatalf("mint token: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "disp-1"}, all); err != nil {
		t.Fatalf("create component: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into interface (name, type, component, node_name, params) values ('disp-1-tcp', 'tcp', (select id from component where name = 'disp-1'), (select principal_id from node where name = 'site-a'), $1::jsonb)`, `{"target":"`+target+`"}`); err != nil {
		t.Fatalf("insert interface: %v", err)
	}
	if _, err := conn.Exec(ctx, `insert into task (id, mode, interface_id, enabled) values ('t-a', 'poll', (select id from interface where name = 'disp-1-tcp'), true)`); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	conn.Close(ctx)

	// Run the real node binary once: claim, pull, probe the live listener, publish.
	out, code := runCLI(t, root, binPath, os.Environ())(
		"node", "--server", "http://"+addr, "--name", "site-a", "--token", token, "--once")
	if code != 0 {
		t.Fatalf("omniglass node exit %d:\n%s", code, out)
	}

	// The datapoint is observable via the read path: tcp.open=1, component-owned,
	// observed. The consumer is async, so poll.
	deadline := time.Now().Add(5 * time.Second)
	for {
		dp, err := gw.LatestMetric(ctx, "disp-1", "tcp.open")
		if err != nil {
			t.Fatalf("latest metric: %v", err)
		}
		if dp != nil {
			if dp.Value != 1 || dp.OwnerKind != "component" || dp.Provenance != "observed" {
				t.Fatalf("tcp.open row = %+v, want value 1 component observed", dp)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tcp.open datapoint never landed for disp-1 after the node run")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
