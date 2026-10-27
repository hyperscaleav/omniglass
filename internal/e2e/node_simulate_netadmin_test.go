//go:build linux && netadmin

// node run --simulate --impairments in a network namespace of the test's own
// (make test-netadmin): the impairments the in-process simulator applies
// reach only its simulated components, never the node's own traffic to its
// server, which shares the process but not the components' addresses.
package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestNodeSimulateImpairmentsSpareTheNode degrades one of two simulated
// components with a 1000 ms latency and proves the node in the same process
// keeps checking in and shipping on time: its heartbeat advances at its
// cadence, and the other component's samples keep landing with a fast
// connect. Then it unplugs the degraded one: it goes dark while the other
// stays up.
func TestNodeSimulateImpairmentsSpareTheNode(t *testing.T) {
	if os.Getenv("OMNIGLASS_NETADMIN") != "1" {
		t.Fatal("this test rewrites the network it runs in; run it through make test-netadmin")
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
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-n"}, all, all); err != nil {
		t.Fatal(err)
	}
	nodeTok := "node-simulate-netadmin-token"
	sum := sha256.Sum256([]byte(nodeTok))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-n", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	product, port := e2eSimulatedProduct(t, ctx, conn)

	cli := runCLI(t, root, binPath, os.Environ())
	as := func(args ...string) string {
		t.Helper()
		out, code := cli(append(args, "--server", server, "--token", ownerTok)...)
		if code != 0 {
			t.Fatalf("omniglass %v exit %d:\n%s", args, code, out)
		}
		return out
	}
	addrs := map[string]string{"sim-slow": "127.63.0.10", "sim-fine": "127.63.0.11"}
	for name, ip := range addrs {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `update component set product_id = (select id from product where name = $1) where name = $2`, product, name); err != nil {
			t.Fatal(err)
		}
		as("endpoint", "create", "--component", name, "--node", "edge-n", "--transport", "tcp",
			"--params", fmt.Sprintf(`{"target":"%s:%d"}`, ip, port))
	}

	pctx, stop := context.WithCancel(ctx)
	p := exec.CommandContext(pctx, binPath, "node", "run", "--simulate", "--impairments", "--poll", "300ms", "--heartbeat", "1s")
	p.Env = append(os.Environ(), "OMNIGLASS_SERVER="+server, "OMNIGLASS_NODE_NAME=edge-n", "OMNIGLASS_NODE_TOKEN="+nodeTok)
	p.Stdout, p.Stderr = os.Stderr, os.Stderr
	p.Cancel = func() error { return p.Process.Signal(os.Interrupt) }
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stop(); _ = p.Wait() })

	get := func(path string, into any) bool {
		req, _ := http.NewRequest(http.MethodGet, server+"/api/v1"+path, nil)
		req.Header.Set("Authorization", "Bearer "+ownerTok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode == http.StatusOK && json.Unmarshal(b, into) == nil
	}
	latest := func(comp, metric string) (float64, time.Time, bool) {
		var out struct {
			Samples []struct {
				Value float64   `json:"value"`
				TS    time.Time `json:"ts"`
			} `json:"samples"`
		}
		if !get("/components/"+comp+"/metrics/"+metric+"/samples?limit=1", &out) || len(out.Samples) == 0 {
			return 0, time.Time{}, false
		}
		s := out.Samples[len(out.Samples)-1]
		return s.Value, s.TS, true
	}
	heartbeat := func() time.Time {
		var n struct {
			LastHeartbeatAt *time.Time `json:"last_heartbeat_at"`
		}
		if !get("/nodes/edge-n", &n) || n.LastHeartbeatAt == nil {
			return time.Time{}
		}
		return *n.LastHeartbeatAt
	}
	fresh := func(comp, metric string, since time.Time, want func(float64) bool) func() bool {
		return func() bool {
			v, ts, ok := latest(comp, metric)
			return ok && ts.After(since) && want(v)
		}
	}
	up := func(v float64) bool { return v == 1 }
	down := func(v float64) bool { return v == 0 }

	start := time.Now()
	for name := range addrs {
		waitForWithin(t, 30*time.Second, name+" to read up", fresh(name, "tcp-open", start, up))
	}

	slowed := time.Now()
	as("component", "impairment", "set", "sim-slow", "--latency-ms", "1000")
	waitForWithin(t, 30*time.Second, "the 1000 ms latency to show on sim-slow", fresh("sim-slow", "tcp-connect-time", slowed, func(ms float64) bool { return ms >= 990 }))

	// With the latency in force, watch the node for six seconds: its
	// check-ins keep their 1s cadence, and the untouched component keeps
	// landing fast samples.
	last := heartbeat()
	watchUntil := time.Now().Add(6 * time.Second)
	for time.Now().Before(watchUntil) {
		time.Sleep(500 * time.Millisecond)
		if hb := heartbeat(); hb.After(last) {
			last = hb
		}
		if gap := time.Since(last); gap > 3*time.Second {
			t.Fatalf("the node's last check-in is %s old while sim-slow is degraded; its traffic to the server was delayed", gap.Round(time.Millisecond))
		}
	}
	checked := time.Now()
	waitForWithin(t, 10*time.Second, "a fresh, fast sample from sim-fine while sim-slow is degraded",
		fresh("sim-fine", "tcp-connect-time", checked, func(ms float64) bool { return ms < 200 }))

	unplugged := time.Now()
	as("component", "impairment", "set", "sim-slow", "--unplugged")
	waitForWithin(t, 30*time.Second, "sim-slow to go dark once unplugged", fresh("sim-slow", "tcp-open", unplugged, down))
	waitForWithin(t, 10*time.Second, "sim-fine to stay up beside it", fresh("sim-fine", "tcp-open", time.Now(), up))
}
