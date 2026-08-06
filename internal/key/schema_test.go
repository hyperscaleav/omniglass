package key

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestValidateSchema drives the bare-required refusal (#595): huma enforces a
// schema's required list only over names declared under properties, so a
// fragment whose required list names an undeclared property is stored but never
// enforced. The checker refuses it at every nesting level the validator
// recurses into, naming the offending property; a fragment whose required names
// are all declared stays accepted.
func TestValidateSchema(t *testing.T) {
	cases := []struct {
		name, schema string
		ok           bool
		offender     string
	}{
		{"empty_fragment_ok", ``, true, ""},
		{"no_required_ok", `{"type":"object","properties":{"a":{"type":"string"}}}`, true, ""},
		{"scalar_fragment_ok", `{"pattern":"^[a-z]+$"}`, true, ""},
		{"enum_fragment_ok", `{"enum":["up","down"]}`, true, ""},
		{"required_declared_ok", `{"type":"object","required":["serial"],"properties":{"serial":{"type":"string"}}}`, true, ""},
		{"bare_required_refused", `{"type":"object","required":["serial"]}`, false, "serial"},
		{"required_beyond_properties_refused", `{"type":"object","required":["serial","extra"],"properties":{"serial":{"type":"string"}}}`, false, "extra"},
		{"nested_property_refused", `{"type":"object","properties":{"net":{"type":"object","required":["mac"]}}}`, false, "mac"},
		{"items_refused", `{"type":"array","items":{"type":"object","required":["port"]}}`, false, "port"},
		{"allOf_refused", `{"allOf":[{"type":"object","required":["ip"]}]}`, false, "ip"},
		{"anyOf_refused", `{"anyOf":[{"type":"object","required":["ip"]}]}`, false, "ip"},
		{"oneOf_refused", `{"oneOf":[{"type":"object","required":["ip"]}]}`, false, "ip"},
		{"not_refused", `{"not":{"type":"object","required":["ip"]}}`, false, "ip"},
		// required at one level, declared at another: the validator consults
		// required only against the same node's properties, so it still
		// enforces nothing where it stands and is refused there.
		{"required_declared_elsewhere_refused", `{"type":"object","required":["x"],"allOf":[{"properties":{"x":{"type":"string"}}}]}`, false, "x"},
		{"not_schema_shaped_refused", `["a","b"]`, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSchema(json.RawMessage(c.schema))
			if c.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("want refusal, got ok")
			}
			if c.offender != "" && !strings.Contains(err.Error(), c.offender) {
				t.Fatalf("error %q does not name the offending property %q", err, c.offender)
			}
		})
	}
}

// additionalProperties may carry a schema of its own, and huma's validator only
// honors it when the field holds a real *Schema, which a stored fragment's plain
// map never is without normalization. These pin both halves of the fix: the
// checker reaches a bare required nested there, and the runtime actually
// enforces a normalized additionalProperties constraint on a value.
func TestAdditionalPropertiesSchemaIsReachedAndEnforced(t *testing.T) {
	// The checker refuses a bare required hidden inside additionalProperties.
	bad := []byte(`{"type":"object","additionalProperties":{"type":"object","required":["id"]}}`)
	if err := ValidateSchema(bad); err == nil {
		t.Error("a bare required nested in additionalProperties passed the checker; the validator recurses there, so the checker must too")
	}

	// A well-formed additionalProperties subschema is enforced at validate time:
	// every additional value must be an integer, so a string value refuses.
	schema := []byte(`{"type":"object","additionalProperties":{"type":"integer"}}`)
	if err := ValidateSchema(schema); err != nil {
		t.Fatalf("a well-formed additionalProperties schema was refused: %v", err)
	}
	if err := ValidateValue("json", []byte(`{"count":"three"}`), schema); err == nil {
		t.Error("a value violating the additionalProperties subschema validated; the normalization exists so the stored constraint is not inert")
	}
	if err := ValidateValue("json", []byte(`{"count":3}`), schema); err != nil {
		t.Errorf("a conforming value was refused: %v", err)
	}
}
