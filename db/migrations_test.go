package db

import "testing"

// TestMigrationVersionsAreUnique holds the real migration tree to CheckUnique:
// every file carries a dbmate new stamp, and no two share one. dbmate keys an
// applied migration on its version alone, so of two files with one version,
// whichever merges second is recorded as applied without ever running (#933:
// two branches each took 20261019050000). The fixture cases are in
// versions_test.go; the base-relative rule needs a base revision, so CI and
// make migration-check run it (cmd/migrationcheck, #1246).
func TestMigrationVersionsAreUnique(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no migrations embedded")
	}
	for _, err := range CheckUnique(names) {
		t.Error(err)
	}
}
