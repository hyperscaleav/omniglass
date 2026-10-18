package build

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
)

// make dev-sim serves the dev fleet's devices at the ports the dev seed
// aimed their endpoints at, so the Makefile's default port shift is the dev
// seed's.
func TestDevSimShiftsPortsAsTheDevSeedExpects(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^SIM_PORT_OFFSET \?= (\d+)$`).FindSubmatch(b)
	if m == nil {
		t.Fatal("the Makefile sets no SIM_PORT_OFFSET default")
	}
	if got, _ := strconv.Atoi(string(m[1])); got != devseed.SimPortOffset {
		t.Fatalf("Makefile SIM_PORT_OFFSET defaults to %d, the dev seed targets %d", got, devseed.SimPortOffset)
	}
}
