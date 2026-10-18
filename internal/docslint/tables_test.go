package docslint

import "testing"

// TestScanTableColumnsIn pins the extraction rule: only a known-table head is
// checked, so subjects, frontmatter paths, and foreign JSON paths never fire.
func TestScanTableColumnsIn(t *testing.T) {
	tables := map[string]map[string]bool{
		"event":     {"id": true, "origin": true, "correlation_id": true},
		"interface": {"params": true},
	}
	seeded := map[string]bool{"endpoint-reachable": true}
	cases := []struct {
		line string
		want int
	}{
		{"the `event.origin` vocabulary", 0},
		{"the `event.alarm_id` edge", 1},
		{"a nested `interface.params.path` token", 0},
		{"the `sidebar.badge` frontmatter", 0},
		{"the `og.v1` subject family", 0},
		{"the seeded `endpoint-reachable` verdict key", 0},
		{"an illustrative `event.ghost_column` example", 0},
	}
	for _, c := range cases {
		if got := scanTableColumnsIn(c.line, tables, seeded); len(got) != c.want {
			t.Errorf("scanTableColumnsIn(%q) = %v, want %d", c.line, got, c.want)
		}
	}
}

// TestSchemaColumnsLoad guards the empty-universe failure mode.
func TestSchemaColumnsLoad(t *testing.T) {
	tables, err := loadSchemaColumns()
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) < 40 {
		t.Fatalf("only %d tables loaded; the live schema has ~51", len(tables))
	}
	if !tables["audit_log"]["actor_principal_id"] {
		t.Error("audit_log.actor_principal_id missing: the column facts did not load")
	}
}

// TestTableColumnReferences runs the storage-table lint over the real corpus,
// enforced.
func TestTableColumnReferences(t *testing.T) {
	findings, err := ScanTableColumns()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	report(t, "table-column", findings, true)
}
