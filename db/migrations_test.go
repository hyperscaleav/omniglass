package db

import (
	"strings"
	"testing"
)

// TestMigrationVersionsAreUnique fails when two migration files share a
// version prefix. dbmate keys an applied migration on its version alone, so
// of two files with one version, whichever merges second is recorded as
// applied without ever running (#933: two branches each took 20261019050000).
func TestMigrationVersionsAreUnique(t *testing.T) {
	entries, err := FS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	seen := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		version, _, ok := strings.Cut(name, "_")
		if !ok || version == "" {
			t.Errorf("%s: no version prefix before the first underscore", name)
			continue
		}
		if first, dup := seen[version]; dup {
			t.Errorf("version %s is taken twice: %s and %s; dbmate would never apply one of them", version, first, name)
			continue
		}
		seen[version] = name
	}
}
