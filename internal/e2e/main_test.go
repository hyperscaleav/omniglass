package e2e

import (
	"os"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestMain routes this package's tests through the storage harness so the
// shared Postgres container is terminated on normal exit. See storagetest.Main.
func TestMain(m *testing.M) {
	os.Exit(storagetest.Main(m))
}
