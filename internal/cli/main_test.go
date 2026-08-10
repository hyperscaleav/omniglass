package cli_test

import (
	"os"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestMain routes this package's tests through the storage harness so the
// shared Postgres container is terminated on normal exit. See storagetest.Main.
//
// It lives in the external test package, like every other main_test.go here,
// even though the harness-using tests in internal/cli are internal ones: a
// TestMain governs the whole test binary, both halves of it, whichever half
// declares it.
func TestMain(m *testing.M) {
	os.Exit(storagetest.Main(m))
}
