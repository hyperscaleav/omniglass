// Command seedgen renders the seed facts (docs/src/generated/seed.json) from
// the embedded seed YAMLs, the shipped-set arm of the generation pipeline. No
// database: the YAMLs are the source of truth and seed.FactsJSON parses them
// in-process, resolving effective role permissions through the same rbac path
// the live authorizer uses. The committed JSON is reviewed like any generated
// code; the drift test in internal/seed fails when it goes stale.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/seed"
)

const dst = "docs/src/generated/seed.json"

func main() {
	facts, err := seed.FactsJSON()
	if err != nil {
		log.Fatalf("seedgen: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Fatalf("seedgen: mkdir: %v", err)
	}
	if err := os.WriteFile(dst, facts, 0o644); err != nil {
		log.Fatalf("seedgen: write %s: %v", dst, err)
	}
	log.Printf("wrote seed facts to %s", dst)
}
