package api_test

import (
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/api"
)

// TestAgentToolPolicySchemaNamesItsLayer: the published contract for an
// overridden tool carries its class, policy and the layer that set it, not
// just its name, so a generated client can read why a tool is refused (#2088).
func TestAgentToolPolicySchemaNamesItsLayer(t *testing.T) {
	t.Parallel()
	raw, err := api.OpenAPIJSON(nil)
	if err != nil {
		t.Fatalf("openapi: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	s, ok := doc.Components.Schemas["AgentToolPolicy"]
	if !ok {
		t.Fatal("no AgentToolPolicy schema")
	}
	for _, p := range []string{"tool", "class", "policy", "source", "role"} {
		if _, ok := s.Properties[p]; !ok {
			t.Errorf("AgentToolPolicy has no %q property", p)
		}
	}
}
