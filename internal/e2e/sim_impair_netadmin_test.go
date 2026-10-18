//go:build linux && netadmin

// The impairment loop end to end, in a network namespace of the test's own
// with NET_ADMIN (make test-netadmin runs it in a container): never on a
// host whose firewall the simulator would rewrite.
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

// TestImpairingASimulatedDeviceShowsInCollection drives the operator's
// loop: a simulated device collected by a real node, unplugged through the
// CLI (its ping and TCP checks fail and both endpoints read down),
// replugged (all recover), then slowed (the delay shows in its TCP connect
// time).
func TestImpairingASimulatedDeviceShowsInCollection(t *testing.T) {
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
	addr := "127.0.0.1:" + freePort(t)
	natsPort := freePort(t)
	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := exec.CommandContext(srvCtx, binPath, "server")
	srv.Env = append(os.Environ(), "OMNIGLASS_DSN="+dsn, "OMNIGLASS_ADDR="+addr,
		"OMNIGLASS_NATS_ADDR=127.0.0.1:"+natsPort, "OMNIGLASS_NATS_URL=nats://127.0.0.1:"+natsPort,
		"OMNIGLASS_NATS_STORE_DIR="+t.TempDir(), "OMNIGLASS_DATA_DIR="+t.TempDir())
	srv.Stdout, srv.Stderr = os.Stderr, os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatal(err)
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
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-i"}, all, all); err != nil {
		t.Fatal(err)
	}
	nodeTok := "impair-e2e-token"
	sum := sha256.Sum256([]byte(nodeTok))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-i", hex.EncodeToString(sum[:]), all); err != nil {
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
	if _, err := conn.Exec(ctx, `update component set product_id = (select id from product where name = $1) where name = 'sim-dev'`, product); err != nil {
		t.Fatal(err)
	}

	cli := runCLI(t, root, binPath, os.Environ())
	as := func(args ...string) string {
		t.Helper()
		out, code := cli(append(args, "--server", server, "--token", ownerTok)...)
		if code != 0 {
			t.Fatalf("omniglass %v exit %d:\n%s", args, code, out)
		}
		return out
	}
	ip := "127.62.0.10"
	as("endpoint", "create", "--component", "sim-dev", "--node", "edge-i", "--transport", "tcp",
		"--params", fmt.Sprintf(`{"target":"%s:%d"}`, ip, port))
	as("endpoint", "create", "--component", "sim-dev", "--node", "edge-i", "--transport", "icmp",
		"--params", fmt.Sprintf(`{"target":%q}`, ip))

	nodeEnv := append(os.Environ(), "OMNIGLASS_SERVER="+server, "OMNIGLASS_NODE_NAME=edge-i", "OMNIGLASS_NODE_TOKEN="+nodeTok)
	for _, args := range [][]string{
		{"sim", "run", "--impairments", "--poll", "300ms"},
		{"node", "run", "--server", server, "--name", "edge-i", "--token", nodeTok, "--heartbeat", "1s"},
	} {
		pctx, stop := context.WithCancel(ctx)
		p := exec.CommandContext(pctx, binPath, args...)
		p.Env = nodeEnv
		p.Stdout, p.Stderr = os.Stderr, os.Stderr
		if err := p.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stop(); _ = p.Wait() })
	}

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
	// metric reads the device's latest sample of a metric, by the series
	// its probe writes (the endpoint name is the transport).
	metric := func(name string) (float64, time.Time, bool) {
		var out struct {
			Samples []struct {
				Value float64   `json:"value"`
				TS    time.Time `json:"ts"`
			} `json:"samples"`
		}
		if !get("/components/sim-dev/metrics/"+name+"/samples?limit=1", &out) || len(out.Samples) == 0 {
			return 0, time.Time{}, false
		}
		s := out.Samples[len(out.Samples)-1]
		return s.Value, s.TS, true
	}
	// reach reads each endpoint's reachability verdict: the panel an
	// operator watches.
	reach := func() map[string]string {
		var r struct {
			Endpoints []struct {
				Endpoint string `json:"endpoint"`
				Verdict  *struct {
					Value string `json:"value"`
				} `json:"verdict"`
			} `json:"endpoints"`
		}
		out := map[string]string{}
		if get("/components/sim-dev/reachability", &r) {
			for _, e := range r.Endpoints {
				if e.Verdict != nil {
					out[e.Endpoint] = e.Verdict.Value
				}
			}
		}
		return out
	}
	both := func(v string) func() bool {
		return func() bool { r := reach(); return r["tcp"] == v && r["icmp"] == v }
	}
	// after waits for a fresh sample of a metric taken after since, and
	// checks it against want.
	after := func(what, name string, since time.Time, want func(float64) bool) {
		t.Helper()
		waitFor(t, what, func() bool {
			v, ts, ok := metric(name)
			return ok && ts.After(since) && want(v)
		})
	}
	up := func(v float64) bool { return v == 1 }
	down := func(v float64) bool { return v == 0 }

	start := time.Now()
	after("TCP to answer", "tcp-open", start, up)
	after("ping to answer", "icmp-reachable", start, up)
	waitFor(t, "both endpoints to read up", both("up"))

	unplugged := time.Now()
	as("component", "impairment", "set", "sim-dev", "--unplugged")
	after("TCP to fail once unplugged", "tcp-open", unplugged, down)
	after("ping to fail once unplugged", "icmp-reachable", unplugged, down)
	waitFor(t, "both endpoints to read down", both("down"))

	replugged := time.Now()
	as("component", "impairment", "set", "sim-dev")
	after("TCP to recover once replugged", "tcp-open", replugged, up)
	after("ping to recover once replugged", "icmp-reachable", replugged, up)
	waitFor(t, "both endpoints to read up again", both("up"))

	slowed := time.Now()
	as("component", "impairment", "set", "sim-dev", "--latency-ms", "300")
	after("the delay to show in the TCP connect time", "tcp-connect-time", slowed, func(ms float64) bool { return ms >= 290 })
}
