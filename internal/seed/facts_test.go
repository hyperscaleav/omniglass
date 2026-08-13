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
		MetricTypes []struct {
			Name string `json:"name"`
		} `json:"metric_types"`
		InterfaceTypes []struct {
			Name  string `json:"name"`
			Built bool   `json:"built"`
		} `json:"interface_types"`
		ComponentTypes []factsTypeNode `json:"component_types"`
		SystemTypes    []factsTypeNode `json:"system_types"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("facts JSON does not parse: %v", err)
	}

	props := map[string]bool{}
	for _, p := range doc.PropertyTypes {
		props[p.Name] = true
	}
	for _, want := range []string{"health", "video-input", "serial-number"} {
		if !props[want] {
			t.Errorf("property_types missing %q (the hand-written guides omitted it; the artifact must not)", want)
		}
	}
	// The numeric canon renders on its own lane (#587).
	metrics := map[string]bool{}
	for _, m := range doc.MetricTypes {
		metrics[m.Name] = true
	}
	for _, want := range []string{"icmp-reachable", "icmp-rtt-avg", "tcp-open", "tcp-connect-time"} {
		if !metrics[want] {
			t.Errorf("metric_types missing %q", want)
		}
	}
	if props["icmp-reachable"] {
		t.Error("icmp-reachable rendered under property_types; numbers are metric types")
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

	// The two inheriting registries (#678). What the docs used to type out by
	// hand was the SHAPE, so that is what is checked: a child points at its
	// parent, and the blank facts a child leaves for its ancestor to supply
	// survive the render rather than being flattened into it. Flattening would
	// make the published tree teach the opposite of ADR-0095's walk.
	comps := indexTypeNodes(doc.ComponentTypes)
	if got := comps["ceiling-mic"].ParentID; got != "mic" {
		t.Errorf("component_types: ceiling-mic parent_id = %q, want %q", got, "mic")
	}
	if comps["mic"].ParentID != "" {
		t.Errorf("component_types: mic parent_id = %q, want a root", comps["mic"].ParentID)
	}
	if comps["display"].Stem == "" {
		t.Error("component_types: display carries no stem; a root has no ancestor to inherit one from")
	}

	systems := indexTypeNodes(doc.SystemTypes)
	if got := systems["board"].ParentID; got != "room" {
		t.Errorf("system_types: board parent_id = %q, want %q", got, "room")
	}
	if systems["board"].Icon != "" {
		t.Errorf("system_types: board icon = %q, want empty; it inherits room's and a render that flattened it would teach the wrong rule", systems["board"].Icon)
	}
	if systems["room"].Icon == "" {
		t.Error("system_types: room carries no icon of its own, which is the value board inherits")
	}
	if systems["av"].ParentID != "" {
		t.Errorf("system_types: av parent_id = %q, want the root", systems["av"].ParentID)
	}

	// A parent named before it is declared would render as an orphan, so the
	// tree the docs draw would silently lose a subtree.
	for _, set := range []struct {
		kind  string
		nodes []factsTypeNode
	}{{"component_types", doc.ComponentTypes}, {"system_types", doc.SystemTypes}} {
		seen := map[string]bool{}
		for _, n := range set.nodes {
			if n.ParentID != "" && !seen[n.ParentID] {
				t.Errorf("%s: %s names parent %q, which has not been declared yet", set.kind, n.ID, n.ParentID)
			}
			seen[n.ID] = true
		}
	}
}

func indexTypeNodes(nodes []factsTypeNode) map[string]factsTypeNode {
	out := make(map[string]factsTypeNode, len(nodes))
	for _, n := range nodes {
		out[n.ID] = n
	}
	return out
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
