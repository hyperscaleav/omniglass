// Command seedgen renders the seed facts (docs/src/generated/seed.json) from
// the embedded seed YAMLs, the shipped-set arm of the generation pipeline. No
// database: the YAMLs are the source of truth and seed.FactsJSON parses them
// in-process, resolving effective role permissions through the same rbac path
// the live authorizer uses. The committed JSON is reviewed like any generated
// code; the drift test in internal/seed fails when it goes stale.
//
// The output is merge-friendly by construction (#1492): one entry per line,
// map keys sorted by encoding/json, list entries in the order their source
// YAML lists them, so two branches that each add a row at different places
// merge without a conflict in the derived file. main_test.go holds that.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/seed"
)

const dst = "docs/src/generated/seed.json"

func main() {
	if err := run(dst); err != nil {
		log.Fatalf("seedgen: %v", err)
	}
	log.Printf("wrote seed facts to %s", dst)
}

// run renders the seed facts and writes them to path, creating its directory.
func run(path string) error {
	facts, err := seed.FactsJSON()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, facts, 0o644)
}
