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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/profile"
	"github.com/hyperscaleav/omniglass/internal/sim/profiles"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
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
	server := startServer(t, binPath, "OMNIGLASS_DSN="+dsn).URL

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
	// The run mode binds the profile's ports (lifted) on the endpoint's
	// address, so the endpoint targets them and the device takes an address
	// no other test process holds.
	target := net.JoinHostPort(simtest.ClaimLoopback(t), fmt.Sprint(port+simE2EPortShift))
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`update component set product_id = (select id from product where name = $1) where name = 'sim-dev'`, []any{product}},
		{`insert into endpoint (name, transport, component, node_name, params) values ('sim-dev-tcp', 'tcp', (select id from component where name = 'sim-dev'), (select principal_id from node where name = 'edge-e'), jsonb_build_object('target', $1::text))`, []any{target}},
		{`insert into task (id, mode, endpoint_id, enabled) values ('t-sim', 'poll', (select id from endpoint where name = 'sim-dev-tcp'), true)`, nil},
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
	if v, ok := read(); !ok || len(v.Devices) != 1 || v.Devices[0].HitsByProtocol["tcp"] != 0 {
		t.Fatalf("before any collection: %+v (read ok %v), want one device and no tcp hits", v, ok)
	}

	// The node collects from the simulated device once, and its probe is
	// the hit.
	if out, code := cli("node", "run", "--server", server, "--name", "edge-e", "--token", nodeTok, "--once"); code != 0 {
		t.Fatalf("node run exit %d:\n%s", code, out)
	}
	waitFor(t, "the probe's hit to show in `node simulation`", func() bool {
		v, ok := read()
		return ok && len(v.Devices) == 1 && v.Devices[0].HitsByProtocol["tcp"] > 0
	})

	// The console's way in: fire an HTTP route from the device's surface at
	// the real simulator, through the server and the bus, and get the
	// device's answer; the fire is itself a hit.
	iface, path := e2eSimulatedRoute(t, product)
	// A malformed path is the operator's typo: the device says so, and the
	// simulator lives on to answer the next fire.
	bad, code := cli("component", "simulation", "fire", "sim-dev", "--surface", iface, "--path", "/a b",
		"--server", server, "--token", ownerTok)
	if code != 0 || !strings.Contains(bad, "not a request path") {
		t.Fatalf("a malformed fire exit %d:\n%s\nwant the device's error", code, bad)
	}
	out, code := cli("component", "simulation", "fire", "sim-dev", "--surface", iface, "--path", path,
		"--server", server, "--token", ownerTok)
	if code != 0 {
		t.Fatalf("fire exit %d:\n%s", code, out)
	}
	var answer struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &answer); err != nil || answer.Status != 200 || answer.Body == "" || answer.Error != "" {
		t.Fatalf("fire %s %s answered %s (%v), want the device's 200", iface, path, out, err)
	}
	waitFor(t, "the fire's hit to show", func() bool {
		v, ok := read()
		return ok && len(v.Devices) == 1 && v.Devices[0].HitsByProtocol["http"] > 0
	})

	// Add a simulated device from the console's route: the simulator picks it
	// up on its next pull and serves it, with no restart.
	if out, code := cli("simulation", "add-component", "edge-e", "--name", "sim-added", "--product", product,
		"--server", server, "--token", ownerTok); code != 0 {
		t.Fatalf("add-device exit %d:\n%s", code, out)
	}
	waitFor(t, "the simulator to serve the added device", func() bool {
		v, ok := read()
		if !ok {
			return false
		}
		for _, d := range v.Devices {
			if d.Component == "sim-added" && d.Serving {
				return true
			}
		}
		return false
	})

	// A whole simulated system to a standard, through the CLI: the system and
	// a component per staffed role, served on the next pull with no restart.
	// The boot seeds no standard, so the operator forks the one it builds to.
	out, code = cli("standard", "fork", "--template", "meeting-room", "--server", server, "--token", ownerTok)
	if code != 0 {
		t.Fatalf("standard fork exit %d:\n%s", code, out)
	}
	out, code = cli("simulation", "add-system", "edge-e", "--standard", "meeting-room", "--system-type", "meeting",
		"--server", server, "--token", ownerTok)
	if code != 0 {
		t.Fatalf("add-system exit %d:\n%s", code, out)
	}
	var sys struct {
		Components []struct {
			Component string `json:"component"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(out), &sys); err != nil || len(sys.Components) < 3 {
		t.Fatalf("add-system answered %s (%v), want at least three components", out, err)
	}
	waitFor(t, "the simulator to serve every component of the system", func() bool {
		v, ok := read()
		if !ok {
			return false
		}
		serving := map[string]bool{}
		for _, d := range v.Devices {
			serving[d.Component] = d.Serving
		}
		for _, c := range sys.Components {
			if !serving[c.Component] {
				return false
			}
		}
		return true
	})
}

// e2eSimulatedRoute is a plain-HTTP surface of the product's profile and
// a GET route it serves.
func e2eSimulatedRoute(t *testing.T, product string) (string, string) {
	t.Helper()
	store, err := profile.LoadStoreFS(profiles.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range store.Devices[product].Interfaces {
		if iface.Protocol != "http" || iface.TLS {
			continue
		}
		for _, r := range iface.Routes {
			if path, ok := strings.CutPrefix(r.Match, "GET "); ok && path != "/" {
				return iface.Name, path
			}
		}
	}
	t.Fatalf("%s's profile has no plain-HTTP GET route", product)
	return "", ""
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
	waitForWithin(t, 20*time.Second, what, cond)
}

// waitForWithin is waitFor with a deadline of its own, for a condition that
// waits on a sustain.
func waitForWithin(t *testing.T, within time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
