package mcp

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/mcp/mcpgen"
	"github.com/hyperscaleav/omniglass/internal/mcp/tooldef"
)

// The served catalog is exactly what the generator builds from the committed
// spec (#1162): a stamp added or removed in Go without `make gen`, or a route
// changed under a tool, fails here in `make test`, before the gen-drift gate.
func TestCatalogMatchesTheSpec(t *testing.T) {
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	want, err := mcpgen.Build(raw)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(want) != len(catalog) {
		t.Fatalf("catalog has %d tools, the spec stamps %d: run make gen", len(catalog), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(want[i], catalog[i]) {
			t.Fatalf("tool %s differs from the spec: run make gen\n got %+v\nwant %+v", want[i].Name, catalog[i], want[i])
		}
	}
}

// Every tool path is relative to the spec's one server URL, which the
// dispatcher prefixes.
func TestAPIBaseIsTheSpecsServer(t *testing.T) {
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var doc struct {
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Servers) != 1 {
		t.Fatalf("spec servers = %+v (%v), want exactly one", doc.Servers, err)
	}
	if doc.Servers[0].URL != apiBase {
		t.Errorf("spec server %q, dispatcher prefix %q", doc.Servers[0].URL, apiBase)
	}
}

// apply-plan writes in bulk but is gated by a read, so its stamp says it writes:
// an agent can import a plan (#2368, #2369), and a client confirms it as a
// write rather than running it as a read-only, idempotent preview.
func TestApplyPlanIsAWriteTool(t *testing.T) {
	for _, d := range catalog {
		if d.Name == "apply-plan" {
			if d.Annotations.ReadOnly || d.Annotations.Idempotent || d.Class != tooldef.ClassWrite {
				t.Fatalf("apply-plan is classed %s (read-only %t, idempotent %t), want a write", d.Class, d.Annotations.ReadOnly, d.Annotations.Idempotent)
			}
			return
		}
	}
	t.Fatal("apply-plan is not an MCP tool")
}
