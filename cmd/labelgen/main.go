// Command labelgen renders the label rule language (docs/src/generated/
// label.json) from the engine's own closed function set, the rule-language arm
// of the generation pipeline. The set is declared once in internal/label and the
// FuncMap, the published names and the AST allowlist all derive from it, so this
// makes the docs the fourth reader of one declaration rather than the fourth
// place it is typed. The committed JSON is reviewed like any generated code; the
// drift test in internal/label fails when it goes stale.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/label"
)

const dst = "docs/src/generated/label.json"

func main() {
	facts, err := label.FactsJSON()
	if err != nil {
		log.Fatalf("labelgen: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Fatalf("labelgen: mkdir: %v", err)
	}
	if err := os.WriteFile(dst, facts, 0o644); err != nil {
		log.Fatalf("labelgen: write %s: %v", dst, err)
	}
	log.Printf("wrote the rule language to %s", dst)
}
