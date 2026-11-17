package node_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// This package's tests share one Postgres container and each get a database of
// their own, so they run in parallel; the rule and the scanner live in
// storagetest (#1537, #1540). Its listeners (UDP, SNMP, HTTP, the bus) bind
// ephemeral ports, never fixed ones, which is what makes that safe; a test that
// needs a fixed port says so in a "// Serial:" comment. It scans the package's
// internal and external test files alike.
func TestEveryNodeTestRunsInParallelOrSaysWhyNot(t *testing.T) {
	t.Parallel()
	storagetest.RequireParallelOrSerial(t, ".")
}
