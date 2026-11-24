package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// metricRetentionFloorVersion is the migration that raises a metric type's own
// retention under 3 days to 3 (#2144): such a window folds nothing.
const metricRetentionFloorVersion = "20261124000000"

// TestMetricTypeRetentionUnderMinimumIsRaisedOnMigrate: a metric type holding 1
// or 2 days from before the minimum reads 3 after the migration; 3 and 30, a
// null and a property type's own window are untouched.
func TestMetricTypeRetentionUnderMinimumIsRaisedOnMigrate(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, metricRetentionFloorVersion); err != nil {
		t.Fatalf("roll back below the floor: %v", err)
	}
	conn := connectDSN(t, dsn)
	for name, days := range map[string]int{"floor-one": 1, "floor-two": 2, "floor-three": 3, "floor-thirty": 30} {
		mustExec(t, conn, `insert into metric_type (name, data_type, retention_days) values ($1, 'float', $2)`, name, days)
	}
	mustExec(t, conn, `insert into metric_type (name, data_type) values ('floor-null', 'float')`)
	mustExec(t, conn, `insert into property_type (name, data_type, retention_days) values ('floor-prop', 'string', 1)`)

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("migrate forward: %v", err)
	}
	for name, want := range map[string]int{"floor-one": 3, "floor-two": 3, "floor-three": 3, "floor-thirty": 30} {
		var got int
		if err := conn.QueryRow(ctx, `select retention_days from metric_type where name = $1`, name).Scan(&got); err != nil || got != want {
			t.Errorf("%s reads %d (%v), want %d", name, got, err, want)
		}
	}
	var n int
	if err := conn.QueryRow(ctx, `select count(*) from metric_type where name = 'floor-null' and retention_days is null`).Scan(&n); err != nil || n != 1 {
		t.Errorf("a null window changed: %d (%v)", n, err)
	}
	if err := conn.QueryRow(ctx, `select retention_days from property_type where name = 'floor-prop'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("a property type's window reads %d (%v), want 1", n, err)
	}
}
