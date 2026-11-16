// Command vocabgen renders the docs glossary from the product vocabulary
// (internal/vocab/terms.yaml, #2029). The same table stamps the OpenAPI entity
// schema descriptions at route registration, so the spec dump carries them
// without a step here. The output is committed; do not hand-edit it.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/vocab"
)

func main() {
	page := vocab.MustLoad().RenderGlossary()
	if err := os.MkdirAll(filepath.Dir(vocab.GlossaryPath), 0o755); err != nil {
		log.Fatalf("vocabgen: mkdir: %v", err)
	}
	if err := os.WriteFile(vocab.GlossaryPath, []byte(page), 0o644); err != nil {
		log.Fatalf("vocabgen: write: %v", err)
	}
	log.Printf("wrote %s", vocab.GlossaryPath)
}
