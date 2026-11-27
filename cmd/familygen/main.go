// Command familygen writes sim/device/families_gen.go, the registry of the
// simulator's Go device families, by scanning sim/device/* (#2829). The list
// is generated so that adding a family package registers it and nothing
// hand-kept can drift from the directories.
package main

import (
	"log"
	"os"

	"github.com/hyperscaleav/omniglass/internal/simfamilygen"
)

const (
	root   = "sim/device"
	dst    = "sim/device/families_gen.go"
	module = "github.com/hyperscaleav/omniglass"
)

func main() {
	fams, err := simfamilygen.Scan(root)
	if err != nil {
		log.Fatalf("familygen: %v", err)
	}
	src, err := simfamilygen.Render(module, fams)
	if err != nil {
		log.Fatalf("familygen: %v", err)
	}
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		log.Fatalf("familygen: write %s: %v", dst, err)
	}
	log.Printf("wrote the registry of %d Go device families to %s", len(fams), dst)
}
