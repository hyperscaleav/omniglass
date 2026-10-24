// Command writerclassgen renders the writer-class ruling
// (docs/src/generated/writerclass.json) from internal/writerclass, the one the
// Storage Gateway enforces (ADR-0161), so the page that teaches who may write a
// key renders the code rather than restating it. The committed JSON is reviewed
// like any generated code; TestFactsRenderTheRuling fails when it goes stale.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

const dst = "docs/src/generated/writerclass.json"

func main() {
	facts, err := writerclass.FactsJSON()
	if err != nil {
		log.Fatalf("writerclassgen: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Fatalf("writerclassgen: mkdir: %v", err)
	}
	if err := os.WriteFile(dst, facts, 0o644); err != nil {
		log.Fatalf("writerclassgen: write %s: %v", dst, err)
	}
	log.Printf("wrote the writer-class ruling to %s", dst)
}
