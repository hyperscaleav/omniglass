package storagetest

import (
	"strings"
	"testing"
)

// TestReclaimGuardRefusesABinaryNotUnderMain pins the mechanism that replaced
// the package comment's honor system. Cleanup is in-process, in Main, because
// the testcontainers reaper cannot be relied on here, so a consuming package
// that never routes its TestMain through Main leaks a Postgres per run. Three
// packages had done exactly that.
//
// This mutates harness state, so it restores it and must not run alongside a
// t.Parallel test. No test in this package takes one.
func TestReclaimGuardRefusesABinaryNotUnderMain(t *testing.T) {
	if err := reclaimGuard(); err != nil {
		t.Fatalf("reclaimGuard under this package's own Main-routed TestMain: %v", err)
	}

	mainRan.Store(false)
	t.Cleanup(func() { mainRan.Store(true) })

	err := reclaimGuard()
	if err == nil {
		t.Fatal("reclaimGuard returned nil for a binary not running under Main; the leak it exists to catch would be silent again")
	}
	// Whoever reads this message just added the package that broke the contract,
	// so the message has to carry the fix rather than only the complaint.
	for _, want := range []string{"TestMain", "storagetest.Main"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("guard error does not name %q, so it does not tell the reader what to do: %v", want, err)
		}
	}
}
