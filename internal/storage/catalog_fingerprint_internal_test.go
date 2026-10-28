package storage

import (
	"regexp"
	"slices"
	"testing"
)

// TestCatalogFingerprintCoversTheRegistryReads is the drift guard for the
// ingest registry cache (#1279): the fingerprint stands for everything the
// three catalog list queries read, so every table those queries name, in a
// FROM or a JOIN, at any depth, must be one the fingerprint covers. A list
// query that starts reading a new table (a new computed column, a join) fails
// here until the table joins registryTables, rather than leaving the cache
// blind to a change that alters the registry.
func TestCatalogFingerprintCoversTheRegistryReads(t *testing.T) {
	tableRef := regexp.MustCompile(`(?i)\b(?:from|join)\s+([a-z_][a-z0-9_]*)`)
	for _, q := range []string{metricTypeListSQL, propertyTypeListSQL, eventTypeListSQL} {
		refs := tableRef.FindAllStringSubmatch(q, -1)
		if len(refs) == 0 {
			t.Fatalf("no table found in %q: the guard would pass vacuously", q)
		}
		for _, m := range refs {
			if !slices.Contains(registryTables, m[1]) {
				t.Errorf("a registry list query reads %s, which the catalog fingerprint does not cover; add it to registryTables", m[1])
			}
		}
	}
}
