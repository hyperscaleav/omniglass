//go:build linux

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

const simE2EPortShift = 24000

// TestSimulatorEndToEnd drives the simulator as an operator would: the real
// server, `omniglass sim run` beside a node with that node's credential, and
// `omniglass node run` collecting from the simulated device. The node's
// reachability probe reaches the device, and `omniglass node simulation`
// shows the simulator connected, the device served, and the probe's hit.
func TestSimulatorEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("simulator e2e: skipped under -short (Postgres testcontainer + go build)")
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
	server := "http://" + addr
	pollHealthz(t, server+"/api/v1/healthz")

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	ownerTok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix}); err != nil {
		t.Fatal(err)
	}
	all := scope.Set{All: true}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-e"}, all, all); err != nil {
		t.Fatal(err)
	}
	nodeTok := "sim-e2e-token"
	sum := sha256.Sum256([]byte(nodeTok))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-e", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "sim-dev"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	product, port := e2eSimulatedProduct(t, ctx, conn)
	target := net.JoinHostPort("127.33.0.10", fmt.Sprint(port+simE2EPortShift))
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`update component set product_id = (select id from product where name = $1) where name = 'sim-dev'`, []any{product}},
		{`insert into interface (name, type, component, node_name, params) values ('sim-dev-tcp', (select id from interface_type where name = 'tcp'), (select id from component where name = 'sim-dev'), (select principal_id from node where name = 'edge-e'), jsonb_build_object('target', $1::text))`, []any{target}},
		{`insert into task (id, mode, interface_id, enabled) values ('t-sim', 'poll', (select id from interface where name = 'sim-dev-tcp'), true)`, nil},
	} {
		if _, err := conn.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}

	// The simulator, beside the node, with the node's identity in its env.
	simCtx, stopSim := context.WithCancel(ctx)
	defer stopSim()
	simCmd := exec.CommandContext(simCtx, binPath, "sim", "run",
		"--port-offset", fmt.Sprint(simE2EPortShift), "--poll", "300ms")
	simCmd.Env = append(os.Environ(),
		"OMNIGLASS_SERVER="+server, "OMNIGLASS_NODE_NAME=edge-e", "OMNIGLASS_NODE_TOKEN="+nodeTok)
	simCmd.Stdout, simCmd.Stderr = os.Stderr, os.Stderr
	if err := simCmd.Start(); err != nil {
		t.Fatalf("start sim: %v", err)
	}
	t.Cleanup(func() { stopSim(); _ = simCmd.Wait() })
	// The operator's view: the generated CLI over GET /nodes/{name}/simulation.
	cli := runCLI(t, root, binPath, os.Environ())
	type simView struct {
		Connected bool `json:"connected"`
		Devices   []struct {
			Component      string         `json:"component"`
			Serving        bool           `json:"serving"`
			HitsByProtocol map[string]int `json:"hits_by_protocol"`
		} `json:"devices"`
	}
	read := func() (simView, bool) {
		var v simView
		out, code := cli("node", "simulation", "edge-e", "--server", server, "--token", ownerTok)
		return v, code == 0 && json.Unmarshal([]byte(out), &v) == nil
	}
	// Wait on the simulator's own report, not a dial: a dial is a hit.
	waitFor(t, "the simulator to report serving the device", func() bool {
		v, ok := read()
		return ok && v.Connected && len(v.Devices) == 1 && v.Devices[0].Serving
	})
	if v, _ := read(); v.Devices[0].HitsByProtocol["tcp"] != 0 {
		t.Fatalf("hits before any collection: %v", v.Devices[0].HitsByProtocol)
	}

	// The node collects from the simulated device once, and its probe is
	// the hit.
	if out, code := cli("node", "run", "--server", server, "--name", "edge-e", "--token", nodeTok, "--once"); code != 0 {
		t.Fatalf("node run exit %d:\n%s", code, out)
	}
	waitFor(t, "the probe's hit to show in `node simulation`", func() bool {
		v, ok := read()
		return ok && v.Devices[0].HitsByProtocol["tcp"] > 0
	})
}

func e2eSimulatedProduct(t *testing.T, ctx context.Context, conn *pgx.Conn) (string, int) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := conn.Query(ctx, `select name from product`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	rows.Close()
	sort.Strings(names)
	for _, n := range names {
		if p, ok := store.Devices[n]; ok {
			for _, iface := range p.Interfaces {
				if iface.Protocol == "http" && !iface.TLS {
					return n, iface.Port
				}
			}
		}
	}
	t.Fatal("no seeded product has a simulator profile with plain HTTP")
	return "", 0
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
