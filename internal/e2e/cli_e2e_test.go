package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestCLIEndToEnd drives the generated CLI as an operator would: it builds the
// real binary, bootstraps an owner with the hand-written command (proving the
// hand-written and generated commands coexist on one root), runs the server, and
// then exercises the generated location commands against it, asserting the
// user-observable output and exit codes. Skipped under -short.
func TestCLIEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("cli e2e: skipped under -short (Postgres testcontainer + go build)")
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
	dbEnv := append(os.Environ(), "OMNIGLASS_DSN="+dsn)

	// Hand-written command: bootstrap the owner (with a password, so the generated
	// change-password command has a current secret to verify) and capture its token.
	bootOut, code := runCLI(t, root, binPath, dbEnv)("bootstrap", "root", "--password", "init-secret-pw")
	if code != 0 {
		t.Fatalf("bootstrap exit %d:\n%s", code, bootOut)
	}
	tok := regexp.MustCompile(`ogp_[A-Za-z0-9_\-]+`).FindString(bootOut)
	if tok == "" {
		t.Fatalf("no bearer token in bootstrap output:\n%s", bootOut)
	}

	// Run the server against the same DB.
	addr := "127.0.0.1:" + freePort(t)
	srvCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := exec.CommandContext(srvCtx, binPath, "server")
	srv.Env = append(dbEnv, "OMNIGLASS_ADDR="+addr)
	srv.Stdout, srv.Stderr = os.Stderr, os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { cancel(); _ = srv.Wait() })
	pollHealthz(t, "http://"+addr+"/api/v1/healthz")

	// Generated commands run against the live server with the connection flags.
	base := []string{"--server", "http://" + addr, "--token", tok}
	cli := func(args ...string) (string, int) {
		return runCLI(t, root, binPath, os.Environ())(append(base, args...)...)
	}

	// Create, then it appears in the scoped list, and a targeted get returns it.
	if out, code := cli("location", "create", "--name", "hq", "--location-type", "campus"); code != 0 || !strings.Contains(out, `"name": "hq"`) {
		t.Fatalf("location create exit %d:\n%s", code, out)
	}
	if out, code := cli("location", "list"); code != 0 || !strings.Contains(out, `"name": "hq"`) {
		t.Fatalf("location list exit %d:\n%s", code, out)
	}
	if out, code := cli("location", "get", "hq"); code != 0 || !strings.Contains(out, `"location_type": "campus"`) {
		t.Fatalf("location get exit %d:\n%s", code, out)
	}

	// A missing location is a non-zero exit (the 404 surfaces as a failure).
	if out, code := cli("location", "get", "nope"); code == 0 {
		t.Fatalf("location get nope should fail, got exit 0:\n%s", out)
	}

	// auth me (hand-written-style data command, here generated) shows the owner.
	if out, code := cli("auth", "me"); code != 0 || !strings.Contains(out, `"username": "root"`) {
		t.Fatalf("auth me exit %d:\n%s", code, out)
	}

	// Self-service profile edit: the generated command updates the owner's own
	// display name (email is admin-only), and auth me reflects it.
	if out, code := cli("auth", "update-profile", "--display-name", "Root Admin"); code != 0 || !strings.Contains(out, `"display_name": "Root Admin"`) {
		t.Fatalf("auth update-profile exit %d:\n%s", code, out)
	}
	if out, code := cli("auth", "me"); code != 0 || !strings.Contains(out, `"display_name": "Root Admin"`) {
		t.Fatalf("auth me after update exit %d:\n%s", code, out)
	}

	// Self-service change-password: the right current secret rotates it (exit 0), a
	// wrong one is refused (the 403 surfaces as a non-zero exit).
	if out, code := cli("auth", "change-password", "--current-password", "init-secret-pw", "--new-password", "rotated-secret-pw"); code != 0 {
		t.Fatalf("auth change-password exit %d:\n%s", code, out)
	}
	if out, code := cli("auth", "change-password", "--current-password", "wrong-secret", "--new-password", "another-secret-pw"); code == 0 {
		t.Fatalf("change-password with a wrong current should fail, got exit 0:\n%s", out)
	}

	// healthz needs no token.
	if out, code := runCLI(t, root, binPath, os.Environ())("--server", "http://"+addr, "healthz"); code != 0 || !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("healthz exit %d:\n%s", code, out)
	}

	// Delete the leaf, then it is gone (a second get fails).
	if _, code := cli("location", "delete", "hq"); code != 0 {
		t.Fatalf("location delete should succeed")
	}
	if _, code := cli("location", "get", "hq"); code == 0 {
		t.Fatalf("location get after delete should fail")
	}

	// Generated help carries the operation summary and example.
	help, code := cli("location", "get", "--help")
	if code != 0 {
		t.Fatalf("help exit %d", code)
	}
	// cobra renders Long (the operation description) plus the generated example.
	if !strings.Contains(help, "Fetches a location") || !strings.Contains(help, "Examples:") ||
		!strings.Contains(help, "omniglass location get") {
		t.Errorf("help missing description/example:\n%s", help)
	}
}

// runCLI returns a runner that executes the binary with the given environment
// and returns combined stdout+stderr and the process exit code.
func runCLI(t *testing.T, dir, bin string, env []string) func(args ...string) (string, int) {
	return func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		code := 0
		if err != nil {
			var ee *exec.ExitError
			if ok := asExitError(err, &ee); ok {
				code = ee.ExitCode()
			} else {
				t.Fatalf("run %v: %v", args, err)
			}
		}
		return string(out), code
	}
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}
