package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The label rule's columns are rules (#2065, ADR-2033): one migration renames
// label_rule.default_template to default_rule and label_rule.template to rule.

const labelRuleRenameVersion = "20261117041735"

// labelRuleColumns is label_rule's columns in table order.
const labelRuleColumns = `select column_name from information_schema.columns
	where table_schema = 'public' and table_name = 'label_rule' order by ordinal_position`

// TestLabelRuleRenameKeepsEveryRule: a shipped default and an operator's
// override planted below the rename read back under default_rule and rule,
// and rolled back they read under the old names again.
func TestLabelRuleRenameKeepsEveryRule(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, labelRuleRenameVersion); err != nil {
		t.Fatalf("roll back below the rename: %v", err)
	}
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into label_rule (entity_kind, default_template, template)
		values ('system', '{{.Name}}', '{{title .Name}}'), ('location', '{{.Stem}}', null)
		on conflict (entity_kind) do update set default_template = excluded.default_template, template = excluded.template`)

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	cols := snapshotRows(t, conn, labelRuleColumns, true)
	for _, c := range cols {
		if c == "default_template" || c == "template" {
			t.Errorf("after the rename label_rule still has %s: %v", c, cols)
		}
	}
	for _, q := range []string{
		`select count(*) from label_rule where entity_kind = 'system' and default_rule = '{{.Name}}' and rule = '{{title .Name}}'`,
		`select count(*) from label_rule where entity_kind = 'location' and default_rule = '{{.Stem}}' and rule is null`,
	} {
		var n int
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: %d (%v), want 1", q, n, err)
		}
	}

	if err := migrate.RollbackBelow(dsn, labelRuleRenameVersion); err != nil {
		t.Fatalf("roll back the rename: %v", err)
	}
	for _, q := range []string{
		`select count(*) from label_rule where entity_kind = 'system' and default_template = '{{.Name}}' and template = '{{title .Name}}'`,
		`select count(*) from label_rule where entity_kind = 'location' and default_template = '{{.Stem}}' and template is null`,
	} {
		var n int
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil || n != 1 {
			t.Errorf("rolled back, %s: %d (%v), want 1", q, n, err)
		}
	}
}
