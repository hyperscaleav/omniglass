package collection_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func TestRegistryAllows(t *testing.T) {
	reg := collection.NewRegistry(
		[]storage.MetricType{
			{Name: "tcp-open"},
			{Name: "icmp-reachable"},
		},
		[]storage.PropertyType{{Name: "video-input"}},
		[]storage.EventType{{Name: "call-started"}},
	)

	// The lane IS the routing (#587): a metric_type name routes to the metric
	// sink, a property_type name to the state sink.
	if kind, ok := reg.Allows("tcp-open"); !ok || kind != "metric" {
		t.Errorf("tcp-open: want (metric,true), got (%q,%v)", kind, ok)
	}
	if kind, ok := reg.Allows("video-input"); !ok || kind != "state" {
		t.Errorf("video-input: want (state,true), got (%q,%v)", kind, ok)
	}
	// A registered event_type resolves to kind "event" (the occurrence keyspace).
	if kind, ok := reg.Allows("call-started"); !ok || kind != "event" {
		t.Errorf("call-started: want (event,true), got (%q,%v)", kind, ok)
	}
	if _, ok := reg.Allows("bogus.key"); ok {
		t.Errorf("bogus.key: want reject, got allow")
	}
}

// TestRegistryReportsCollisions pins the shadowing bug. The catalogs are separate
// tables with separate uniqueness, so nothing stops the same name existing in two
// of them. The merge applied later catalogs second, so the later one silently won
// and a colliding name became unwritable on its own lane: it reached the wrong
// arm, failed the value extraction, and vanished with no row and no error.
//
// The snapshot cannot fix the data, so it must not hide it. Collisions() names
// them, and the ingest path can refuse rather than guess which catalog meant it.
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

	// A colliding name must not resolve to either kind: resolving it would pick a
	// winner arbitrarily, which is the bug.
	if kind, ok := reg.Allows("call-started"); ok {
		t.Fatalf("a colliding name resolved to %q; it must be rejected until the collision is fixed", kind)
	}
	// Non-colliding names are unaffected.
	if kind, ok := reg.Allows("video-input"); !ok || kind != "state" {
		t.Fatalf("video-input = (%q,%v), want (state,true)", kind, ok)
	}
	if kind, ok := reg.Allows("command-issued"); !ok || kind != "event" {
		t.Fatalf("command-issued = (%q,%v), want (event,true)", kind, ok)
	}
}

// TestRegistryReportsCrossLaneCollisions is the same refusal for the split's new
// seam: a name in BOTH catalog lanes (metric and property) would route to two
// sinks, so it must resolve to nothing until one side is renamed.
func TestRegistryReportsCrossLaneCollisions(t *testing.T) {
	reg := collection.NewRegistry(
		[]storage.MetricType{{Name: "fan-speed"}},
		[]storage.PropertyType{{Name: "fan-speed"}, {Name: "video-input"}},
		nil,
	)
	if got := reg.Collisions(); len(got) != 1 || got[0] != "fan-speed" {
		t.Fatalf("Collisions() = %v, want exactly [fan-speed]", got)
	}
	if kind, ok := reg.Allows("fan-speed"); ok {
		t.Fatalf("a cross-lane colliding name resolved to %q; it must be rejected", kind)
	}
	if kind, ok := reg.Allows("video-input"); !ok || kind != "state" {
		t.Fatalf("video-input = (%q,%v), want (state,true)", kind, ok)
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
