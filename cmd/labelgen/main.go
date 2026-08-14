// Command labelgen renders the label rule language from the code that runs it,
// the rule-language arm of the generation pipeline. Two artifacts, because the
// rule language has two closed halves and they are declared in different places:
//
//   - docs/src/generated/label.json, the FUNCTIONS a rule may name, declared once
//     in internal/label so the FuncMap, the published names, the AST allowlist and
//     the docs table are four readers of one list rather than four transcriptions
//     (#701). Each example is executed through the engine on the way in.
//   - docs/src/generated/labeldata.json, the DATA a rule may read, declared once
//     per entity kind in internal/storage beside the accessors that build the map
//     (#729), so the taught set and the reachable set cannot disagree.
//
// Both committed files are reviewed like any generated code; the drift tests in
// internal/label and internal/storage fail when either goes stale.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/label"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

const (
	funcDst = "docs/src/generated/label.json"
	dataDst = "docs/src/generated/labeldata.json"
)

func main() {
	funcs, err := label.FactsJSON()
	if err != nil {
		log.Fatalf("labelgen: %v", err)
	}
	write(funcDst, funcs)
	log.Printf("wrote the rule language to %s", funcDst)

	data, err := storage.LabelDataFactsJSON()
	if err != nil {
		log.Fatalf("labelgen: %v", err)
	}
	write(dataDst, data)
	log.Printf("wrote the rule data map to %s", dataDst)
}

func write(dst string, body []byte) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		log.Fatalf("labelgen: mkdir: %v", err)
	}
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		log.Fatalf("labelgen: write %s: %v", dst, err)
	}
}
