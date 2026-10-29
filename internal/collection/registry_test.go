package collection_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// TestRegistryLaneMembership: with the per-lane wire (#594) the lane IS the
// routing, so the registry answers membership per catalog, not a kind string. A
// name resolves only on its own lane, and the resolved entry carries the catalog
// row so ingest can validate against it (data_type, validation, payload_schema).
func TestRegistryLaneMembership(t *testing.T) {
	reg := collection.NewRegistry(
		[]storage.MetricType{
			{Name: "tcp-open"},
			{Name: "icmp-reachable"},
		},
		[]storage.PropertyType{{Name: "video-input", DataType: "string", Validation: []byte(`{"enum":["hdmi1","hdmi2"]}`)}},
		[]storage.EventType{{Name: "call-started", PayloadSchema: []byte(`{"type":"object"}`)}},
	)

	if _, ok := reg.Metric("tcp-open"); !ok {
		t.Error("tcp-open: want metric-lane membership")
	}
	pt, ok := reg.Property("video-input")
	if !ok || pt.DataType != "string" || len(pt.Validation) == 0 {
		t.Errorf("video-input: want the property row (data_type, validation) back, got %+v ok=%v", pt, ok)
	}
	et, ok := reg.Event("call-started")
	if !ok || len(et.PayloadSchema) == 0 {
		t.Errorf("call-started: want the event row (payload_schema) back, got %+v ok=%v", et, ok)
	}

	// A name is refused BY LANE: a property name pushed as a metric is unknown on
	// the metric lane, even though the catalog knows the name elsewhere.
	if _, ok := reg.Metric("video-input"); ok {
		t.Error("video-input resolved on the metric lane; membership is per catalog")
	}
	if _, ok := reg.Property("tcp-open"); ok {
		t.Error("tcp-open resolved on the property lane; membership is per catalog")
	}
	if _, ok := reg.Event("tcp-open"); ok {
		t.Error("tcp-open resolved on the event lane; membership is per catalog")
	}

	// Unregistered everywhere: refused everywhere.
	for _, probe := range []func(string) bool{
		func(n string) bool { _, ok := reg.Metric(n); return ok },
		func(n string) bool { _, ok := reg.Property(n); return ok },
		func(n string) bool { _, ok := reg.Event(n); return ok },
	} {
		if probe("bogus.key") {
			t.Error("bogus.key: want reject on every lane")
		}
	}
}

// TestRegistryReportsCollisions pins the shadowing bug. The catalogs are separate
// tables with separate uniqueness, so nothing stops the same name existing in two
// of them. Resolving a colliding name would pick a winner arbitrarily, so it must
// resolve on NO lane, and Collisions() names it so the refusal is visible.
func TestRegistryReportsCollisions(t *testing.T) {
	reg := collection.NewRegistry(
		[]storage.MetricType{
			{Name: "call-started"}, // collides with the event type below
		},
		[]storage.PropertyType{{Name: "video-input"}},
		[]storage.EventType{{Name: "call-started"}, {Name: "command-issued"}},
	)

	got := reg.Collisions()
	if len(got) != 1 || got[0] != "call-started" {
		t.Fatalf("Collisions() = %v, want exactly [call-started]", got)
	}

	// A colliding name must not resolve on either of its lanes.
	if _, ok := reg.Metric("call-started"); ok {
		t.Fatal("a colliding name resolved on the metric lane; it must be refused until the collision is fixed")
	}
	if _, ok := reg.Event("call-started"); ok {
		t.Fatal("a colliding name resolved on the event lane; it must be refused until the collision is fixed")
	}
	// Non-colliding names are unaffected.
	if _, ok := reg.Property("video-input"); !ok {
		t.Fatal("video-input: want property-lane membership")
	}
	if _, ok := reg.Event("command-issued"); !ok {
		t.Fatal("command-issued: want event-lane membership")
	}
}

// TestRegistryReportsCrossLaneCollisions is the same refusal for the catalog
// split's seam: a name in BOTH catalog lanes (metric and property) would land in
// two sinks, so it must resolve to nothing until one side is renamed.
func TestRegistryReportsCrossLaneCollisions(t *testing.T) {
	reg := collection.NewRegistry(
		[]storage.MetricType{{Name: "fan-speed"}},
		[]storage.PropertyType{{Name: "fan-speed"}, {Name: "video-input"}},
		nil,
	)
	if got := reg.Collisions(); len(got) != 1 || got[0] != "fan-speed" {
		t.Fatalf("Collisions() = %v, want exactly [fan-speed]", got)
	}
	if _, ok := reg.Metric("fan-speed"); ok {
		t.Fatal("a cross-lane colliding name resolved on the metric lane; it must be refused")
	}
	if _, ok := reg.Property("fan-speed"); ok {
		t.Fatal("a cross-lane colliding name resolved on the property lane; it must be refused")
	}
	if _, ok := reg.Property("video-input"); !ok {
		t.Fatal("video-input: want property-lane membership")
	}
}

// TestRegistryNoCollisionsIsEmpty keeps the common path honest: the seeded
// vocabulary has no overlap, so Collisions() must stay empty rather than
// reporting noise every boot.
func TestRegistryNoCollisionsIsEmpty(t *testing.T) {
	reg := collection.NewRegistry(
		[]storage.MetricType{{Name: "icmp-rtt-avg"}},
		[]storage.PropertyType{{Name: "video-input"}},
		[]storage.EventType{{Name: "call-started"}},
	)
	if got := reg.Collisions(); len(got) != 0 {
		t.Fatalf("Collisions() = %v, want none", got)
	}
}

// A name an official row and an operator's row both hold, in different catalogs
// (#1307): a release shipped a name an operator had already coined. The official
// row is the platform's and always stays, but on this install the operator's
// shadows it at ingest, so the operator's series keep landing where they did and
// nothing is refused. Shadowed() names it so it is visible.
func TestAnOperatorsNameShadowsAnOfficialOneInAnotherCatalog(t *testing.T) {
	cases := []struct {
		what                            string
		metrics                         []storage.MetricType
		properties                      []storage.PropertyType
		events                          []storage.EventType
		wantMetric, wantProp, wantEvent bool
	}{
		{"official metric, operator property",
			[]storage.MetricType{{Name: "x", Official: true}}, []storage.PropertyType{{Name: "x"}}, nil, false, true, false},
		{"official property, operator metric",
			[]storage.MetricType{{Name: "x"}}, []storage.PropertyType{{Name: "x", Official: true}}, nil, true, false, false},
		{"official event, operator property",
			nil, []storage.PropertyType{{Name: "x"}}, []storage.EventType{{Name: "x", Official: true}}, false, true, false},
		{"official metric, operator event",
			[]storage.MetricType{{Name: "x", Official: true}}, nil, []storage.EventType{{Name: "x"}}, false, false, true},
	}
	for _, c := range cases {
		reg := collection.NewRegistry(c.metrics, c.properties, c.events)
		if got := reg.Collisions(); len(got) != 0 {
			t.Errorf("%s: Collisions() = %v, want none: the operator's row wins", c.what, got)
		}
		if got := reg.Shadowed(); len(got) != 1 || got[0] != "x" {
			t.Errorf("%s: Shadowed() = %v, want [x]", c.what, got)
		}
		_, m := reg.Metric("x")
		_, p := reg.Property("x")
		_, e := reg.Event("x")
		if m != c.wantMetric || p != c.wantProp || e != c.wantEvent {
			t.Errorf("%s: x resolves metric %v property %v event %v, want %v %v %v", c.what, m, p, e, c.wantMetric, c.wantProp, c.wantEvent)
		}
	}
}

// The shadow is one operator row over official ones. Two official rows is a
// release bug and two operator rows is a create the fence should have refused;
// an operator row among several is ambiguous: each still resolves to nothing.
func TestOnlyAnUnambiguousOperatorRowShadows(t *testing.T) {
	for _, c := range []struct {
		what       string
		metrics    []storage.MetricType
		properties []storage.PropertyType
		events     []storage.EventType
	}{
		{"two official rows", []storage.MetricType{{Name: "x", Official: true}}, []storage.PropertyType{{Name: "x", Official: true}}, nil},
		{"two operator rows", []storage.MetricType{{Name: "x"}}, []storage.PropertyType{{Name: "x"}}, nil},
		{"two operator rows over an official one", []storage.MetricType{{Name: "x"}}, []storage.PropertyType{{Name: "x"}}, []storage.EventType{{Name: "x", Official: true}}},
	} {
		reg := collection.NewRegistry(c.metrics, c.properties, c.events)
		if got := reg.Collisions(); len(got) != 1 || got[0] != "x" {
			t.Errorf("%s: Collisions() = %v, want [x]", c.what, got)
		}
		if got := reg.Shadowed(); len(got) != 0 {
			t.Errorf("%s: Shadowed() = %v, want none", c.what, got)
		}
		if _, ok := reg.Metric("x"); ok {
			t.Errorf("%s: x resolved on the metric lane", c.what)
		}
		if _, ok := reg.Property("x"); ok {
			t.Errorf("%s: x resolved on the property lane", c.what)
		}
		if _, ok := reg.Event("x"); ok {
			t.Errorf("%s: x resolved on the event lane", c.what)
		}
	}
}

// A reserved key (the node's or the platform's own) is never shadowed: held in two
// catalogs it resolves to nothing, so an operator row that appears beside a key a
// later release reserves cannot take the official probe key's place.
func TestAReservedKeyIsNeverShadowed(t *testing.T) {
	reg := collection.NewRegistry(
		[]storage.MetricType{{Name: "icmp-reachable", Official: true}},
		[]storage.PropertyType{{Name: "icmp-reachable"}},
		nil,
	)
	if got := reg.Collisions(); len(got) != 1 || got[0] != "icmp-reachable" {
		t.Errorf("Collisions() = %v, want [icmp-reachable]", got)
	}
	if got := reg.Shadowed(); len(got) != 0 {
		t.Errorf("Shadowed() = %v, want none", got)
	}
	if _, ok := reg.Property("icmp-reachable"); ok {
		t.Error("an operator row took a reserved key")
	}
}
