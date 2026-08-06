package seed

import (
	"encoding/json"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/key"
	"gopkg.in/yaml.v3"
)

// TestShippedSchemasPassChecker holds the seed's own schemas to the checker the
// catalogs now hold operators to (#595): every schema fragment the seed ships
// must pass the bare-required refusal. A seed row failing it is a seed bug to
// fix in the YAML, never a reason to relax the checker. The scan is generic over
// the schema-bearing keys so a schema added to a seed file is covered before its
// loader learns about it; the storage upserts refuse at boot as the backstop.
func TestShippedSchemasPassChecker(t *testing.T) {
	docs := map[string][]byte{
		"property_types.yaml": propertyTypesYAML,
		"event_types.yaml":    eventTypesYAML,
		"command_types.yaml":  commandTypesYAML,
	}
	schemaKeys := []string{"validation", "payload_schema", "params_schema"}
	for file, raw := range docs {
		var doc map[string][]map[string]any
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: parse: %v", file, err)
		}
		for _, rows := range doc {
			for _, row := range rows {
				for _, k := range schemaKeys {
					fragment, ok := row[k]
					if !ok {
						continue
					}
					b, err := json.Marshal(fragment)
					if err != nil {
						t.Errorf("%s: %v: marshal %s: %v", file, row["name"], k, err)
						continue
					}
					if err := key.ValidateSchema(b); err != nil {
						t.Errorf("%s: %v: shipped %s fails the checker: %v", file, row["name"], k, err)
					}
				}
			}
		}
	}
}
