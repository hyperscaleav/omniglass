package e2e

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A server that dies before it is ready fails the wait at once, and its
// exit stays observable to the cleanup that stops it: a wait that took the
// only exit value left that cleanup blocked until the suite's timeout, which
// aborted every test after it and hid the boot error.
func TestADeadServerFailsTheWaitAndFreesItsCleanup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 3")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := watch(cmd)
	_, err := awaitReadyFile(filepath.Join(t.TempDir(), "ready.json"), p, 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("awaitReadyFile = %v, want the server's exit", err)
	}
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the exit was seen once; the cleanup waiting on it would hang")
	}
}
