package api_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A reference carries BOTH forms: the uuid is the canonical handle and the name
// is the label a human reads. A body carrying only one is the defect this guards.
//
// Only the id would force every client to fetch a second collection and join by
// hand to render a label, which is what the console did before. Only the name
// would hide the stable handle, which is what a response briefly did after
// ADR-0053 and is the reason that ADR is superseded. Both, uniformly, so there is
// no rule about which references are interesting enough to label.
//
// This is checked against the generated OpenAPI rather than in prose because the
// failure is invisible at runtime: a body missing one half still serves 200s, and
// the cost only lands in whoever consumes it.

// referenceFields maps a uuid field to the name field that must accompany it, per
// response schema. A schema listed here must carry both.
var referenceFields = map[string]string{
	"parent_id":   "parent",
	"location_id": "location",
	"system_id":   "system",
}

// Schemas where a *_id field addresses something with no name to pair it with.
// Each entry is a real decision: if the target has a name, carry the name too.
var idOnlyIsCorrect = map[string]string{
	"resource_id":  "an audit row's target is polymorphic and may since have been deleted",
	"value_id":     "a stored property value has no name, only the property it answers",
	"interface_id": "an interface's name is unique only within its component",
	"principal_id": "a principal is addressed by uuid; a username is a credential, not an address",
	"scope_id":     "a scope root is a uuid handle on a subtree",
	"group_id":     "a principal group is uuid-keyed",
	"target_id":    "an impersonation target is a principal, as above",
	"node_id":      "a node is addressed by its enrollment identity, which is its primary key",
	"owner_id":     "the tag, variable, and secret owner arcs pair with owner_name; see #346",
}

// Catalog ids that ARE names, because the registry is keyed by a written slug.
var slugKeyedCatalogs = map[string]bool{
	"product_id": true, "standard_id": true, "parent_standard_id": true, "parent_product_id": true,
	"driver_id": true, "vendor_id": true, "capability_id": true,
	"location_type_id": true, "secret_type_id": true, "datapoint_type_id": true,
	"interface_type_id": true, "property_id": true, "role_id": true, "alarm_id": true,
	"event_id": true, "audit_id": true, "source_rule_id": true, "product_property_id": true,
	"tag_id": true,
}

func TestReferencesCarryBothForms(t *testing.T) {
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if len(doc.Components.Schemas) == 0 {
		t.Fatal("no schemas in the spec: this test would pass vacuously")
	}

	checked := 0
	for name, sch := range doc.Components.Schemas {
		// Input bodies take ONE field per reference, accepting either form, so the
		// pairing rule is about responses.
		if strings.HasSuffix(name, "InputBody") {
			continue
		}
		for field := range sch.Properties {
			if field == "id" || !strings.HasSuffix(field, "_id") {
				continue
			}
			if slugKeyedCatalogs[field] {
				continue
			}
			if _, ok := idOnlyIsCorrect[field]; ok {
				continue
			}
			pair, ok := referenceFields[field]
			if !ok {
				t.Errorf("%s.%s: unknown reference field. Either pair it with a name in "+
					"referenceFields, or record why an id alone is correct in idOnlyIsCorrect.", name, field)
				continue
			}
			checked++
			if _, has := sch.Properties[pair]; !has {
				t.Errorf("%s carries %q but not %q: a reference carries both, the id as the handle "+
					"and the name as the label", name, field, pair)
			}
		}
	}
	if checked == 0 {
		t.Error("no reference pairs were checked, so this test proved nothing")
	}
}
