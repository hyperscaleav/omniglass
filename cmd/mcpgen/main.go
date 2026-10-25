// Command mcpgen generates the MCP tool catalog from the committed OpenAPI
// document (api/openapi.json), the sibling of cligen in the generation pipeline
// (#1162, ADR-1162). It is run by `make gen`. Every operation stamped with the
// x-omniglass-mcp extension in Go becomes one tool; the builder and renderers
// live in internal/mcp/mcpgen so the drift test can run them too. It writes the
// Go catalog the server serves (internal/mcp/catalog_gen.go) and the docs
// reference page (docs/src/content/docs/reference/mcp/index.md), both committed
// and reviewed like code.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/mcp/mcpgen"
)

func main() {
	raw, err := os.ReadFile("api/openapi.json")
	if err != nil {
		log.Fatalf("mcpgen: read spec: %v", err)
	}
	defs, err := mcpgen.Build(raw)
	if err != nil {
		log.Fatalf("mcpgen: %v", err)
	}
	src, err := mcpgen.RenderGo(defs)
	if err != nil {
		log.Fatalf("mcpgen: render catalog: %v", err)
	}
	const catalog = "internal/mcp/catalog_gen.go"
	if err := os.WriteFile(catalog, src, 0o644); err != nil {
		log.Fatalf("mcpgen: write catalog: %v", err)
	}
	const page = "docs/src/content/docs/reference/mcp/index.md"
	if err := os.MkdirAll(filepath.Dir(page), 0o755); err != nil {
		log.Fatalf("mcpgen: docs dir: %v", err)
	}
	if err := os.WriteFile(page, mcpgen.RenderDoc(defs), 0o644); err != nil {
		log.Fatalf("mcpgen: write docs page: %v", err)
	}
	log.Printf("wrote %s and %s (%d tools)", catalog, page, len(defs))
}
