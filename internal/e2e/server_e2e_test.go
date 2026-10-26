// Package e2e drives the real binary the way an operator would: build it, run
// `omniglass server` against a testcontainer Postgres, and hit the HTTP API.
// It catches contract drift the in-process tests miss, because it exercises the
// actual run-mode wiring (env config, migrate-on-boot, listen address).
package e2e

import (
	"context"
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

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestServerHealthzEndToEnd builds the binary, points it at a fresh migrated
// DB via OMNIGLASS_DSN, starts it the one way the end-to-end tests start a
// server (both listeners on port 0, the addresses read back from its ready
// file), and asserts GET /api/v1/healthz returns 200 with db ok at the address
// it reported. Skipped under -short.
func TestServerHealthzEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("server e2e: skipped under -short (Postgres testcontainer + go build)")
	}
	ctx := context.Background()

	// 1. Build the real binary: the same artifact an operator runs, so the
	//    run-mode wiring is what gets tested, not an in-process registration.
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "omniglass")
	build := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./cmd/omniglass")
	build.Dir = repoRoot(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build omniglass: %v\n%s", err, out)
	}

	// 2. Fresh migrated DB (storagetest re-runs migrate; the server re-runs it
	//    too on boot, proving migrate is idempotent across the two callers).
	dsn := storagetest.NewDSN(t)

	// 3. Run `omniglass server` as a child process on ports it picks.
	srv := startServer(t, binPath, "OMNIGLASS_DSN="+dsn)

	// 4. Each listener reports the port the kernel handed it, never the 0 it
	//    was asked for nor the bus's fixed default, and answers there.
	for name, addr := range map[string]string{"addr": srv.Addr, "nats_addr": srv.NatsAddr} {
		_, port, err := net.SplitHostPort(addr)
		if err != nil || port == "0" || port == "4222" {
			t.Fatalf("ready file %s = %q (%v), want the port the kernel handed out", name, addr, err)
		}
	}
	if srv.NatsURL != "nats://"+srv.NatsAddr {
		t.Errorf("ready file nats_url = %q, want the bus it bound (nats://%s)", srv.NatsURL, srv.NatsAddr)
	}
	c, err := net.Dial("tcp", srv.NatsAddr)
	if err != nil {
		t.Fatalf("the bus does not answer at its reported %s: %v", srv.NatsAddr, err)
	}
	_ = c.Close()

	body := pollHealthz(t, srv.URL+"/api/v1/healthz")
	var got struct {
		Status string `json:"status"`
		DB     string `json:"db"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("healthz body not JSON: %v (%q)", err, body)
	}
	if got.Status != "ok" {
		t.Errorf("status = %q, want ok", got.Status)
	}
	if got.DB != "ok" {
		t.Errorf("db = %q, want ok", got.DB)
	}
}

// runningServer is an `omniglass server` child and what its ready file
// reported: the addresses it bound and the bus URL it advertises.
type runningServer struct {
	URL      string // http://<addr>, the API's base
	Addr     string // the HTTP listener's bound address
	NatsAddr string // the embedded bus's bound address
	NatsURL  string // the bus URL the node-claim exchange advertises
}

// startServer is the one way an end-to-end test starts `omniglass server`.
// Both listeners ask for port 0, so the kernel hands each a port no other
// process holds, and the server writes the addresses it bound to its ready
// file (OMNIGLASS_READY_FILE), which this waits for. Nothing picks a port,
// releases it and hands the number to a later bind, so a concurrent run on
// the host cannot take it first (#1077). The bus URL is left to the server
// to derive from the address it bound, so a node is told the port it got.
//
// env is the caller's settings (the DSN, at least), laid over a fresh
// JetStream store and data dir; the listen settings and the ready file are
// the harness's own and come last, so neither the caller nor the developer's
// shell can pin a port. The server is stopped when the test ends.
func startServer(t *testing.T, bin string, env ...string) runningServer {
	t.Helper()
	ready := filepath.Join(t.TempDir(), "ready.json")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "server")
	cmd.Env = append(os.Environ(),
		"OMNIGLASS_NATS_STORE_DIR="+t.TempDir(),
		// Keep the fallback encryption key out of the source tree (the server's
		// cwd is this package dir under `go test`).
		"OMNIGLASS_DATA_DIR="+t.TempDir(),
	)
	cmd.Env = append(cmd.Env, env...)
	cmd.Env = append(cmd.Env,
		"OMNIGLASS_ADDR=127.0.0.1:0",
		"OMNIGLASS_NATS_ADDR=127.0.0.1:0",
		"OMNIGLASS_NATS_URL=",
		"OMNIGLASS_READY_FILE="+ready,
	)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start server: %v", err)
	}
	p := watch(cmd)
	t.Cleanup(func() { cancel(); <-p.done })
	srv, err := awaitReadyFile(ready, p, 60*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// process is a started child whose exit any number of readers can observe:
// done is closed when it exits, and err holds its Wait result from then on.
// A value sent once would be taken by whichever reader saw it first, leaving
// the cleanup that stops the server blocked forever.
type process struct {
	done chan struct{}
	err  error
}

// watch observes cmd's exit.
func watch(cmd *exec.Cmd) *process {
	p := &process{done: make(chan struct{})}
	go func() { p.err = cmd.Wait(); close(p.done) }()
	return p
}

// awaitReadyFile waits for the server to write its ready file at path, and
// fails if the process exits first or the file is not there within within.
func awaitReadyFile(path string, p *process, within time.Duration) (runningServer, error) {
	deadline := time.After(within)
	for {
		if b, err := os.ReadFile(path); err == nil {
			var r struct {
				Addr     string `json:"addr"`
				NatsAddr string `json:"nats_addr"`
				NatsURL  string `json:"nats_url"`
			}
			if err := json.Unmarshal(b, &r); err != nil || r.Addr == "" || r.NatsAddr == "" || r.NatsURL == "" {
				return runningServer{}, fmt.Errorf("ready file %q (%v), want addr, nats_addr and nats_url", b, err)
			}
			return runningServer{URL: "http://" + r.Addr, Addr: r.Addr, NatsAddr: r.NatsAddr, NatsURL: r.NatsURL}, nil
		}
		select {
		case <-p.done:
			return runningServer{}, fmt.Errorf("server exited before it was ready: %v", p.err)
		case <-deadline:
			return runningServer{}, fmt.Errorf("server wrote no ready file within %s (%s)", within, path)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func pollHealthz(t *testing.T, url string) []byte {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		b := readAllClose(t, resp)
		if resp.StatusCode == http.StatusOK {
			return b
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("healthz never returned 200 within deadline (%s)", url)
	return nil
}

func readAllClose(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read healthz body: %v", err)
	}
	return b
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs cwd: %v", err)
	}
	for {
		if fi, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found walking up from cwd")
		}
		dir = parent
	}
}
