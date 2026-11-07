package devseed_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The package's database tests are clones of a seeded template and run in
// parallel; the rule and the scanner live in storagetest (#1537).
func TestEveryDevseedTestRunsInParallelOrSaysWhyNot(t *testing.T) {
	t.Parallel()
	storagetest.RequireParallelOrSerial(t, ".")
}
