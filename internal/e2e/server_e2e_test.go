// Package e2e drives the real binary the way an operator would: build it, run
// `omniglass server` against a testcontainer Postgres, and hit the HTTP API.
// It catches contract drift the in-process tests miss, because it exercises the
// actual run-mode wiring (env config, migrate-on-boot, listen address).
package e2e

import (
	"context"
	"encoding/json"
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
// DB via OMNIGLASS_DSN on a free port, waits for the API to come up, and
// asserts GET /api/v1/healthz returns 200 with db ok. Skipped under -short.
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
	addr := "127.0.0.1:" + freePort(t)

	// 3. Run `omniglass server` as a child process.
	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(srvCtx, binPath, "server")
	cmd.Env = append(os.Environ(),
		"OMNIGLASS_DSN="+dsn,
		"OMNIGLASS_ADDR="+addr,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	// 4. Poll healthz until the server is listening (build + boot can take a
	//    moment), then assert the response shape.
	url := "http://" + addr + "/api/v1/healthz"
	body := pollHealthz(t, url)

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

// freePort asks the OS for an unused TCP port and returns it as a string. The
// listener is closed immediately; a brief race window before the server binds
// is acceptable for a local test.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	return port
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
