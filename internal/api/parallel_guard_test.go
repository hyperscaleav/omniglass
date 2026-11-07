package api_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// This package's tests share one Postgres container and each get a database of
// their own, so they run in parallel; the rule and the scanner live in
// storagetest (#1537, #1540). It scans the package's internal and external test
// files alike.
func TestEveryAPITestRunsInParallelOrSaysWhyNot(t *testing.T) {
	t.Parallel()
	storagetest.RequireParallelOrSerial(t, ".")
}
