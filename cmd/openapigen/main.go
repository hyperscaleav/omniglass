// Command openapigen writes the API's OpenAPI 3.1 document to api/openapi.json
// and api/openapi.yaml, server-less, from the route registrations. Run via
// `make gen`. The Go API (the Huma operation registrations) is the single
// source of truth; the committed spec files are derived artifacts.
//
// This slice has one route, so the spec is the whole pipeline. Later slices add
// the typed SPA client and the CLI as downstream generators reading this spec.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// stubGateway satisfies storage.Gateway with no real backend. Spec generation
// only registers handlers (never invokes the gateway), so its methods are never
// called; this keeps the codegen tool free of any database dependency.
type stubGateway struct{}

func (stubGateway) Ping(context.Context) error { return nil }
func (stubGateway) Close()                     {}

func main() {
	var gw storage.Gateway = stubGateway{}

	jsonDoc, err := api.OpenAPIJSON(gw)
	if err != nil {
		log.Fatal(err)
	}
	yamlDoc, err := api.OpenAPIYAML(gw)
	if err != nil {
		log.Fatal(err)
	}

	if err := os.MkdirAll("api", 0o755); err != nil {
		log.Fatal(err)
	}
	writeFile(filepath.Join("api", "openapi.json"), jsonDoc)
	writeFile(filepath.Join("api", "openapi.yaml"), yamlDoc)
}

func writeFile(path string, body []byte) {
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%d bytes)", path, len(body))
}
