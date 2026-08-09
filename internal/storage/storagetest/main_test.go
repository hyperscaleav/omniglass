package storagetest

import (
	"os"
	"testing"
)

// TestMain routes this package's own tests through Main so the shared Postgres
// container is terminated on normal exit, exactly as every consuming package
// does. The harness tests provision databases through NewDSN, so this package
// is a consumer of the shared container like any other.
func TestMain(m *testing.M) {
	os.Exit(Main(m))
}
