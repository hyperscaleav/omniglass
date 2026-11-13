package seed

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/transport"
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
		Transports []struct {
			Name      string   `json:"name"`
			Built     bool     `json:"built"`
			Params    []string `json:"params"`
			ProbeKeys []string `json:"probe_keys"`
		} `json:"transports"`
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
	for _, want := range []string{"icmp-rtt-avg", "tcp-connect-time"} {
		if !metrics[want] {
			t.Errorf("metric_types missing %q", want)
		}
	}
	// A probe's result is a state, so it renders on the property lane (#1592).
	for _, want := range []string{"icmp-reachable", "tcp-open"} {
		if !props[want] {
			t.Errorf("property_types missing %q (a probe's result is a state, not a number)", want)
		}
		if metrics[want] {
			t.Errorf("%q rendered under metric_types; a probe's result is a property", want)
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
	for _, tr := range doc.Transports {
		if tr.Name == "icmp" && tr.Built {
			builtICMP = true
		}
	}
	if !builtICMP {
		t.Error("transports: icmp missing or not built (the facts doc renders the code registry)")
	}
	// The collection page's probe table renders these (#1087): each dialed
	// transport carries its params and probe keys through, snmp included.
	probed := 0
	for _, tr := range doc.Transports {
		if len(tr.ProbeKeys) > 0 && len(tr.Params) > 0 {
			probed++
		}
		if tr.Name == "snmp" && !slices.Contains(tr.ProbeKeys, "snmp-response-time") {
			t.Errorf("transports: snmp probe keys = %v, want snmp-response-time (the probe table is generated from them)", tr.ProbeKeys)
		}
	}
	dialed := 0
	for _, tr := range transport.All() {
		if tr.Built && !tr.Push {
			dialed++
		}
	}
	if probed != dialed {
		t.Errorf("transports: %d carry probe facts, want the %d a node dials", probed, dialed)
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
	// The OAVC tree (#802): mic sits under the audio-device category root.
	if got := comps["mic"].ParentID; got != "audio-device" {
		t.Errorf("component_types: mic parent_id = %q, want %q", got, "audio-device")
	}
	if comps["audio-device"].ParentID != "" {
		t.Errorf("component_types: audio-device parent_id = %q, want a root", comps["audio-device"].ParentID)
	}
	if comps["audio-device"].Stem == "" {
		t.Error("component_types: audio-device carries no stem; a root has no ancestor to inherit one from")
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

// TestSeedFactsSeverityAndRules pins the severity registry and the official
// event rules into the facts artifact (#921), so the alarms page and the rules
// guide render the shipped levels and rules instead of restating them: each
// level with its outage flag computed by the health rule's own floor.
func TestSeedFactsSeverityAndRules(t *testing.T) {
	raw, err := FactsJSON()
	if err != nil {
		t.Fatalf("FactsJSON: %v", err)
	}
	var doc struct {
		SeverityLevels []factsSeverityLevel `json:"severity_levels"`
		EventRules     []factsEventRule     `json:"event_rules"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("facts JSON does not parse: %v", err)
	}
	got := map[string]factsSeverityLevel{}
	for _, l := range doc.SeverityLevels {
		got[l.Name] = l
	}
	if len(got) != 5 || got["high"].Rank != 40 || !got["high"].Outage || got["average"].Outage || got["disaster"].Color != "darkred" {
		t.Errorf("severity_levels = %+v, want the five shipped levels with high and above an outage", doc.SeverityLevels)
	}
	var names []string
	rules := map[string]factsEventRule{}
	for _, r := range doc.EventRules {
		names = append(names, r.Name)
		rules[r.Name] = r
	}
	want := []string{"reachability-lost", "ingest-lag-high", "node-silent", "disk-nearly-full", "retention-behind"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("event_rules = %v, want the official rules %v in file order", names, want)
	}
	for _, w := range []factsEventRule{
		{Name: "reachability-lost", SampleKey: "endpoint-reachable", Fire: `value == "down"`, Clear: `value == "up"`, FireSustain: "15s", Severity: "high"},
		{Name: "ingest-lag-high", SampleKey: "ingest-lag-max", Fire: "value > 30", Clear: "value <= 30", FireSustain: "2m", Severity: "warning"},
		// A node three 30 s heartbeats quiet fires at once: the age already
		// is the sustain (#1707).
		{Name: "node-silent", SampleKey: "checkin-age", Fire: "value > 90", Clear: "value <= 90", Severity: "high"},
		// The data directory's disk past 90 percent used, cleared below 85 so
		// a disk hovering at the line does not flap (#1707).
		{Name: "disk-nearly-full", SampleKey: "data-dir-disk-used-ratio", Fire: "value > 0.9", Clear: "value <= 0.85", FireSustain: "2m", Severity: "warning"},
		// The retention pass's lane behind past its catch-up passes (#1629).
		{Name: "retention-behind", SampleKey: "retention-behind", Fire: "value == 1", Clear: "value == 0", FireSustain: "5m", Severity: "warning"},
	} {
		w.FireEventType, w.ClearEventType = "condition-started", "condition-ended"
		r := rules[w.Name]
		if r.Label == "" || r.Description == "" {
			t.Errorf("event rule %s has no label or description: %+v", w.Name, r)
		}
		r.Label, r.Description = "", ""
		if r != w {
			t.Errorf("event rule %s = %+v, want %+v", w.Name, r, w)
		}
	}
}
