//go:build linux

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/sim/simtest"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

const outpostSimulatePortShift = 25000

// TestOutpostRunSimulateCollectsItsOwnFleet drives `omniglass outpost run
// --simulate` as an operator would: ONE process, holding one outpost's one
// enrollment token, serves the simulated component placed on that outpost and
// collects from it. The simulator reports the component running, and the
// outpost's TCP sample lands, so the two halves share the process and its
// loopback with no second enrollment and no second process.
func TestOutpostRunSimulateCollectsItsOwnFleet(t *testing.T) {
	if testing.Short() {
		t.Skip("outpost run --simulate e2e: skipped under -short (Postgres testcontainer + go build)")
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
	if _, err := gw.CreateOutpost(ctx, "", storage.OutpostSpec{Name: "edge-s"}, all, all); err != nil {
		t.Fatal(err)
	}
	outpostTok := "outpost-simulate-e2e-token"
	sum := sha256.Sum256([]byte(outpostTok))
	if _, err := gw.SetEnrollmentToken(ctx, "", "edge-s", hex.EncodeToString(sum[:]), all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "sim-own"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	product, port := e2eSimulatedProduct(t, ctx, conn)
	if _, err := conn.Exec(ctx, `update component set product_id = (select id from product where name = $1) where name = 'sim-own'`, product); err != nil {
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
	target := net.JoinHostPort(simtest.ClaimLoopback(t), fmt.Sprint(port+outpostSimulatePortShift))
	as("endpoint", "create", "--component", "sim-own", "--outpost", "edge-s", "--transport", "tcp",
		"--params", fmt.Sprintf(`{"target":%q}`, target))

	// The one process: the outpost, its simulator inside it.
	pctx, stop := context.WithCancel(ctx)
	p := exec.CommandContext(pctx, binPath, "outpost", "run", "--simulate",
		"--port-offset", fmt.Sprint(outpostSimulatePortShift), "--poll", "300ms", "--heartbeat", "1s", "--probe-every", "1s", "--worklist-every", "1s")
	p.Env = append(os.Environ(), "OMNIGLASS_SERVER="+server, "OMNIGLASS_OUTPOST_NAME=edge-s", "OMNIGLASS_OUTPOST_TOKEN="+outpostTok)
	p.Stdout, p.Stderr = os.Stderr, os.Stderr
	// Stopped the way an operator stops it, so the graceful path is the one
	// under test.
	p.Cancel = func() error { return p.Process.Signal(os.Interrupt) }
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- p.Wait() }()
	t.Cleanup(func() { stop(); <-exited })

	type simView struct {
		Connected bool `json:"connected"`
		Devices   []struct {
			Component      string         `json:"component"`
			Status         string         `json:"status"`
			HitsByProtocol map[string]int `json:"hits_by_protocol"`
		} `json:"devices"`
	}
	read := func() (simView, bool) {
		var v simView
		out, code := cli("outpost", "simulation", "edge-s", "--server", server, "--token", ownerTok)
		return v, code == 0 && json.Unmarshal([]byte(out), &v) == nil
	}
	waitForWithin(t, 30*time.Second, "the in-process simulator to report the component running", func() bool {
		v, ok := read()
		return ok && v.Connected && len(v.Devices) == 1 && v.Devices[0].Status == "running"
	})
	waitForWithin(t, 30*time.Second, "the outpost's probe to land on its own simulated component", func() bool {
		v, ok := read()
		return ok && len(v.Devices) == 1 && v.Devices[0].HitsByProtocol["tcp"] > 0
	})
	waitForWithin(t, 30*time.Second, "the outpost's tcp-open result to read open", func() bool {
		req, _ := http.NewRequest(http.MethodGet, server+"/api/v1/components/sim-own/properties/tcp-open/samples?limit=1", nil)
		req.Header.Set("Authorization", "Bearer "+ownerTok)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var out struct {
			Samples []struct {
				Value string `json:"value"`
			} `json:"samples"`
		}
		return resp.StatusCode == http.StatusOK && json.Unmarshal(b, &out) == nil &&
			len(out.Samples) > 0 && out.Samples[len(out.Samples)-1].Value == "open"
	})

	// Stopping the process stops both halves: it exits cleanly, and the
	// simulator's address is free again (no orphan listener).
	stop()
	select {
	case <-exited:
		exited <- nil
	case <-time.After(15 * time.Second):
		t.Fatal("outpost run --simulate did not exit within 15s of its signal")
	}
	if c, err := net.DialTimeout("tcp", target, time.Second); err == nil {
		c.Close()
		t.Fatalf("%s still answers after the process stopped", target)
	}
}
