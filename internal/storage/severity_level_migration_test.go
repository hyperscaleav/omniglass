package storage_test

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSeverityLevelBackfill proves the move from a three-value CHECK to the
// level registry keeps every existing alarm's meaning: critical (the outage
// word) becomes high (the outage floor of the ranked levels), info and
// warning keep their names, the CHECK is gone and the reference stands.
func TestSeverityLevelBackfill(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20261019000000"); err != nil {
		t.Fatal(err)
	}
	conn := connectDSN(t, dsn)
	mustExec(t, conn, `insert into component (name, product_id) values ('sev-c', (select id from product where name = 'generic-device'))`)
	for _, s := range []string{"critical", "warning", "info"} {
		mustExec(t, conn, `insert into alarm (component_id, severity, message, dedup_key) values ((select id from component where name = 'sev-c'), $1, $1, $1)`, s)
	}
	if err := migrate.Run(dsn); err != nil {
		t.Fatal(err)
	}
	for old, want := range map[string]string{"critical": "high", "warning": "warning", "info": "info"} {
		if got := scalar(t, conn, `select severity from alarm where dedup_key = '`+old+`'`); got != want {
			t.Errorf("an alarm raised %s now reads %s, want %s", old, got, want)
		}
	}
	if constraintExists(t, conn, "alarm_severity_check") {
		t.Error("the three-value severity CHECK survived")
	}
	if count(t, conn, `select count(*) from pg_constraint where conname = 'alarm_severity_fkey' and contype = 'f'`) != 1 {
		t.Error("alarm.severity does not reference severity_level")
	}
}
