package labfootprint

import (
	"os"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestMain routes the package's tests through the storage harness, so the
// Postgres container the receipt statement's test starts is reclaimed.
func TestMain(m *testing.M) {
	os.Exit(storagetest.Main(m))
}
