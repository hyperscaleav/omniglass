// Command configgen renders the env-var reference (docs/src/generated/
// config.json) from the declarative registry in internal/config, the
// environment arm of the generation pipeline. No process reads a variable the
// registry does not declare (config.Get refuses), so the reference is complete
// by construction; the drift test in internal/config fails when it goes stale.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/config"
)

const dst = "docs/src/generated/config.json"

func main() {
	facts, err := config.FactsJSON()
	if err != nil {
		log.Fatalf("configgen: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Fatalf("configgen: mkdir: %v", err)
	}
	if err := os.WriteFile(dst, facts, 0o644); err != nil {
		log.Fatalf("configgen: write %s: %v", dst, err)
	}
	log.Printf("wrote env-var reference to %s", dst)
}
