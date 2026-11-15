// Command settingsgen renders the settings reference (docs/src/generated/
// settings.json) from the Settings struct in internal/settings, the settings
// arm of the generation pipeline. The struct's tags are the one declaration of
// a setting's type, default, range and doc, so the reference is complete by
// construction; the drift test in internal/settings fails when it goes stale.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/settings"
)

const dst = "docs/src/generated/settings.json"

func main() {
	facts, err := settings.FactsJSON()
	if err != nil {
		log.Fatalf("settingsgen: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Fatalf("settingsgen: mkdir: %v", err)
	}
	if err := os.WriteFile(dst, facts, 0o644); err != nil {
		log.Fatalf("settingsgen: write %s: %v", dst, err)
	}
	log.Printf("wrote settings reference to %s", dst)
}
