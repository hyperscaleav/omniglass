package collection_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

func TestRegistryAllows(t *testing.T) {
	metric := "metric"
	reg := collection.NewRegistry(
		[]storage.PropertyType{
			{Name: "tcp-open", Kind: &metric},
			{Name: "icmp-reachable", Kind: &metric},
		},
		[]storage.EventType{{Name: "call-started"}},
	)

	if kind, ok := reg.Allows("tcp-open"); !ok || kind != "metric" {
		t.Errorf("tcp-open: want (metric,true), got (%q,%v)", kind, ok)
	}
	// A registered event_type resolves to kind "event" (the occurrence keyspace).
	if kind, ok := reg.Allows("call-started"); !ok || kind != "event" {
		t.Errorf("call-started: want (event,true), got (%q,%v)", kind, ok)
	}
	if _, ok := reg.Allows("bogus.key"); ok {
		t.Errorf("bogus.key: want reject, got allow")
	}
}

// TestRegistryReportsCollisions pins the shadowing bug. property_type and
// event_type are separate tables with separate uniqueness, so nothing stops the
// same name existing in both. The merge applied event types second, so the event
// silently won and a metric-kind name became unwritable: it reached the event
// arm, failed the value extraction, and vanished with no row and no error.
//
// The snapshot cannot fix the data, so it must not hide it. Collisions() names
// them, and the ingest path can refuse rather than guess which registry meant it.
func TestRegistryReportsCollisions(t *testing.T) {
	metric, state := "metric", "state"
	reg := collection.NewRegistry(
		[]storage.PropertyType{
			{Name: "call-started", Kind: &metric}, // collides with the event type below
			{Name: "video-input", Kind: &state},
			{Name: "serial-number"}, // declared-only, not collectable
		},
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
	// A declared-only property stays uncollectable.
	if _, ok := reg.Allows("serial-number"); ok {
		t.Fatal("a declared-only property must not be collectable")
	}
}

// TestRegistryNoCollisionsIsEmpty keeps the common path honest: the seeded
// vocabulary has no overlap, so Collisions() must stay empty rather than
// reporting noise every boot.
func TestRegistryNoCollisionsIsEmpty(t *testing.T) {
	metric := "metric"
	reg := collection.NewRegistry(
		[]storage.PropertyType{{Name: "icmp-rtt-avg", Kind: &metric}},
		[]storage.EventType{{Name: "call-started"}},
	)
	if got := reg.Collisions(); len(got) != 0 {
		t.Fatalf("Collisions() = %v, want none", got)
	}
}
