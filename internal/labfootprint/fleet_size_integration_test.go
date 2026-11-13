package labfootprint

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A lab's fleet is the scenario's systems: the platform system the boot seed
// plants on every server is the server's own, so a scale lab at 20 systems
// reads 20 and the perf gate does not refuse its own baseline as another
// size (#1640).
func TestIntegrationFleetSizeLeavesOutThePlatformSystem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	if _, err := conn.Exec(ctx, `insert into system (name) values ('scenario-1'), ('scenario-2')`); err != nil {
		t.Fatal(err)
	}
	var all int
	if err := conn.QueryRow(ctx, `select count(*) from system`).Scan(&all); err != nil {
		t.Fatal(err)
	}
	var systems, components, tasks int
	if err := conn.QueryRow(ctx, fleetSizeSQL).Scan(&systems, &components, &tasks); err != nil {
		t.Fatal(err)
	}
	if all != 3 || systems != 2 {
		t.Fatalf("the database holds %d systems and the fleet reads %d, want 3 (the platform's and two) and 2", all, systems)
	}
}
