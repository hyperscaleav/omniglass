// Command openapigen writes the API's OpenAPI 3.1 document to api/openapi.json
// and api/openapi.yaml, server-less, from the route registrations. Run via
// `make gen`. The Go API (the Huma operation registrations) is the single
// source of truth; the committed spec files are derived artifacts.
//
// The JSON is written canonical (canonicalJSON): pretty-printed with its keys
// sorted, so a route change rewrites only its own lines. Written as Huma emits it
// (one line), any two API changes conflicted on the whole file (#1095).
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// stubGateway satisfies storage.Gateway with no real backend. Spec generation
// only registers handlers (never invokes the gateway), so its methods are never
// called; this keeps the codegen tool free of any database dependency.
type stubGateway struct{ storage.UnimplementedGateway }

func main() {
	var gw storage.Gateway = stubGateway{}

	raw, err := api.OpenAPIJSON(gw)
	if err != nil {
		log.Fatal(err)
	}
	jsonDoc, err := canonicalJSON(raw)
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
	writeFile(filepath.Join("api", "openapi.yaml"), append(yamlDoc, '\n'))
}

func writeFile(path string, body []byte) {
	if err := os.WriteFile(path, body, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%d bytes)", path, len(body))
}

// canonicalJSON re-encodes a JSON document two-space indented with every
// object's keys in sorted order and a trailing newline. Numbers keep their
// literal text (json.Number), and <, > and & stay as written: the file is read
// by people in a diff as much as by the generators, which parse it and are
// indifferent to layout. Idempotent.
func canonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("canonicalJSON: trailing data after the document")
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
