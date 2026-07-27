package erd

import (
	"os"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestMain reaps the shared Postgres testcontainer in-process on exit, as every
// package that uses the storagetest harness must.
func TestMain(m *testing.M) {
	os.Exit(storagetest.Main(m))
}
