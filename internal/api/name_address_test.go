package api_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A name is the address: every response field that points at ANOTHER entity
// carries that entity's name, never its uuid. A uuid appears only as an entity's
// own `id`, an opaque handle.
//
// This guards the rule at the contract rather than in prose, because the failure
// is invisible: a body that emits `parent_id` still serves 200s, and the cost
// only shows up in a client that has to fetch a second collection and join by
// hand to render one label. That is exactly how the console came to carry a
// uuid-to-name map for systems.
//
// The allowed list below is the whole of the exception, and every entry is an
// entity with NO name to use instead. Adding to it is a real decision: if the
// target has a name, the field should carry it.
var idFieldsWithNoNameToUse = map[string]string{
	"resource_id":  "audit rows are polymorphic: the target may be any resource, including one since deleted",
	"value_id":     "a stored property value has no name of its own, only the property it answers",
	"interface_id": "an interface's name is unique only within its component, so the surrogate is the address",
	"principal_id": "a principal is addressed by uuid; its username is an authentication credential, not an address",
	"scope_id":     "a scope root is a uuid handle on a subtree",
	"group_id":     "a principal group is uuid-keyed",
	"target_id":    "an impersonation target is a principal, addressed as above",
	"node_id":      "a node is addressed by its enrollment identity",
}

// Catalog ids that ARE names: these registries are keyed by a human-written slug,
// so `product_id: "cisco-room-bar"` already satisfies the rule.
var slugKeyedCatalogs = map[string]bool{
	"product_id": true, "standard_id": true, "parent_standard_id": true,
	"driver_id": true, "vendor_id": true, "parent_product_id": true, "capability_id": true, "tag_id": true,
	"location_type_id": true, "secret_type_id": true, "datapoint_type_id": true,
	"interface_type_id": true, "property_id": true, "role_id": true, "alarm_id": true,
	"event_id": true, "audit_id": true, "source_rule_id": true, "product_property_id": true,
}

func TestResponsesAddressEntitiesByName(t *testing.T) {
	raw, err := os.ReadFile("../../api/openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type any `json:"type"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	if len(doc.Components.Schemas) == 0 {
		t.Fatal("no schemas in the spec: this test would pass vacuously")
	}

	var offenders []string
	for name, sch := range doc.Components.Schemas {
		for field := range sch.Properties {
			if field == "id" || !strings.HasSuffix(field, "_id") {
				continue
			}
			if slugKeyedCatalogs[field] {
				continue
			}
			if _, allowed := idFieldsWithNoNameToUse[field]; allowed {
				continue
			}
			offenders = append(offenders, name+"."+field)
		}
	}
	if len(offenders) > 0 {
		t.Errorf("these response fields name another entity by uuid, but a name is the address: %v\n"+
			"Either carry the target's name, or, if the target genuinely has no name, add the field to "+
			"idFieldsWithNoNameToUse with the reason.", offenders)
	}
}
