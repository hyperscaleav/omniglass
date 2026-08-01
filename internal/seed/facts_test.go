package seed

import (
	"encoding/json"
	"os"
	"testing"
)

// TestSeedFactsGroundTruth pins the generated seed facts against what the
// embedded YAMLs actually ship: the property-type set the guides undercounted
// (the audit's finding), the transports with their built flags, and roles
// carrying effective (inheritance-resolved) permissions, not just declared.
func TestSeedFactsGroundTruth(t *testing.T) {
	raw, err := FactsJSON()
	if err != nil {
		t.Fatalf("FactsJSON: %v", err)
	}
	var doc struct {
		Roles []struct {
			ID        string   `json:"id"`
			Declared  []string `json:"declared"`
			Effective []string `json:"effective"`
		} `json:"roles"`
		PropertyTypes []struct {
			Name string `json:"name"`
		} `json:"property_types"`
		InterfaceTypes []struct {
			Name  string `json:"name"`
			Built bool   `json:"built"`
		} `json:"interface_types"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("facts JSON does not parse: %v", err)
	}

	props := map[string]bool{}
	for _, p := range doc.PropertyTypes {
		props[p.Name] = true
	}
	for _, want := range []string{"health", "video.input", "icmp.reachable"} {
		if !props[want] {
			t.Errorf("property_types missing %q (the hand-written guides omitted it; the artifact must not)", want)
		}
	}

	if len(doc.Roles) == 0 {
		t.Fatal("no roles in facts")
	}
	for _, r := range doc.Roles {
		if len(r.Effective) < len(r.Declared) {
			t.Errorf("role %s: effective (%d) smaller than declared (%d); inheritance not resolved", r.ID, len(r.Effective), len(r.Declared))
		}
	}

	builtICMP := false
	for _, it := range doc.InterfaceTypes {
		if it.Name == "icmp" && it.Built {
			builtICMP = true
		}
	}
	if !builtICMP {
		t.Error("interface_types: icmp missing or not built")
	}
}

// TestSeedFactsMatchesCommitted is the drift gate: the committed
// docs/src/generated/seed.json must equal a fresh in-process render of the
// embedded YAMLs, or a seed change shipped without make gen. No database.
func TestSeedFactsMatchesCommitted(t *testing.T) {
	fresh, err := FactsJSON()
	if err != nil {
		t.Fatalf("FactsJSON: %v", err)
	}
	committed, err := os.ReadFile("../../docs/src/generated/seed.json")
	if err != nil {
		t.Fatalf("read committed seed.json (run make gen and commit it): %v", err)
	}
	if string(committed) != string(fresh) {
		t.Error("docs/src/generated/seed.json drifted from the embedded seed YAMLs: run make gen and commit the result")
	}
}
