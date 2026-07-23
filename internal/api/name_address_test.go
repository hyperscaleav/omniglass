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
//
// The registry references join the estate references here now that every registry
// has a uuid primary key and a renameable `name` (epic #262, ADR-0062). A product,
// a vendor, a driver, a standard, and their parents are addressed the same way an
// estate entity is: the id is the handle, the name is the label, and a response
// carries both.
var referenceFields = map[string]string{
	"parent_id":          "parent",
	"location_id":        "location",
	"system_id":          "system",
	"owner_id":           "owner_name",
	"component_id":       "component",
	"node_id":            "node",
	"product_id":         "product",
	"parent_product_id":  "parent_product",
	"vendor_id":          "vendor",
	"driver_id":          "driver",
	"standard_id":        "standard",
	"parent_standard_id": "parent_standard",
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
}

// References exempt from the both-forms rule, each for a reason of its own.
// The nine component-classification registries used to live here, back when their
// name WAS their primary key; epic #262 gave each a uuid key and a renameable
// name, so they moved into referenceFields and now carry both like any reference.
// What remains is a still-slug-keyed taxonomy (`datapoint_type`, whose id is its
// written kind) and a set of row ids that address rows with no separate name of
// their own (an alarm, an event, an audit row, a rule, a contract line, a role,
// a tag binding's key).
var exemptRefs = map[string]bool{
	"datapoint_type_id": true, "role_id": true, "alarm_id": true,
	"event_id": true, "audit_id": true, "source_rule_id": true,
	"product_property_id": true, "tag_id": true,
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
			if exemptRefs[field] {
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
