package seed

import (
	"slices"
	"testing"
)

// TestShippedCatalogsNeverShareAName is #457's pin, folded into #311: the
// three ship-with catalogs (metric_type, property_type, event_type) are
// disjoint by name. A name in two of them resolves to nothing at ingest
// (collection.NewCatalog refuses it on every lane), so a one-line YAML edit
// that collided would make every sample carrying the name unwritable on every
// install. The create routes refuse a collision; the boot seed writes through
// upserts that do not look across catalogs, so this test and the boot-time
// refusal (Run) are what hold the seed to it. A database constraint is not the
// tool: uniqueness across separate tables needs a trigger or a shared name
// table, and neither exists.
func TestShippedCatalogsNeverShareAName(t *testing.T) {
	shared, err := sharedCatalogNames(metricTypesYAML, propertyTypesYAML, eventTypesYAML)
	if err != nil {
		t.Fatalf("parse the ship-with catalogs: %v", err)
	}
	if len(shared) > 0 {
		t.Fatalf("ship-with catalogs share names %v: a shared name resolves to nothing at ingest", shared)
	}
}

// TestSharedCatalogNamesFindsAClash: the check names every name two
// catalogs hold, whichever two.
func TestSharedCatalogNamesFindsAClash(t *testing.T) {
	metric := []byte("metric_types:\n  - name: uptime\n  - name: volume\n")
	property := []byte("property_types:\n  - name: volume\n  - name: mute\n")
	event := []byte("event_types:\n  - name: mute\n  - name: call-started\n")
	shared, err := sharedCatalogNames(metric, property, event)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !slices.Equal(shared, []string{"mute", "volume"}) {
		t.Fatalf("shared = %v, want [mute volume]", shared)
	}
}
