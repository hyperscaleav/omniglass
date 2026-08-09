package storage_test

import (
	"os"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestMain routes this package's tests through the storage harness so the
// shared Postgres container is terminated on normal exit. See storagetest.Main.
//
// It also hands the harness to the package's INTERNAL test files (the
// `package storage` ones, path_batch_test.go among them). storagetest imports
// storage, so an internal test file cannot import the harness itself without
// an import cycle in test; this assignment is the one direction that works,
// and it lives here because TestMain is already this package's single bridge
// to the harness.
func TestMain(m *testing.M) {
	storage.HarnessDSN = storagetest.NewDSN
	os.Exit(storagetest.Main(m))
}
