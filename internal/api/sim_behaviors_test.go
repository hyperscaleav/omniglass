package api

import (
	"encoding/json"
	"os"
	"testing"
)

// A simulated behavior is described by its name, interval and the paths it
// writes, never by whether it posts a webhook: a device posts from its own
// handlers, so the flag was always false once the YAML profiles were gone, and
// the API no longer carries it (#2837, BREAKING).
func TestASimulatedBehaviorCarriesNoWebhookFlag(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read openapi.json (run make gen): %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	s, ok := doc.Components.Schemas["SimBehaviorBody"]
	if !ok {
		t.Fatal("no SimBehaviorBody schema")
	}
	if _, has := s.Properties["emits"]; has {
		t.Errorf("SimBehaviorBody lists emits; want name, interval and writes only")
	}
	for _, want := range []string{"name", "interval", "writes"} {
		if _, has := s.Properties[want]; !has {
			t.Errorf("SimBehaviorBody lacks %s", want)
		}
	}
}
