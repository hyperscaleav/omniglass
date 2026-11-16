package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/db"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// The product-named driver rename (#1483): `newtron-nvp` became
// `newtron-panel-nvp` and `http-json` became `tessellate-census-http`, once
// each served only one product. A driver's name is its seed key, and an
// endpoint holds the driver by uuid, so without a carry-over the boot seed
// would insert a second driver row under the new name and leave every
// attached endpoint on the old, never-again-updated one. The migration
// renames the row in place so its uuid, and every endpoint attached through
// it, comes along.

const driverRenameVersion = "20261115120000"

func driverRenameUpSQL(t *testing.T) string {
	t.Helper()
	raw, err := db.FS.ReadFile("migrations/" + driverRenameVersion + "_driver_product_names.sql")
	if err != nil {
		t.Fatalf("read driver rename migration: %v", err)
	}
	up, _, ok := strings.Cut(string(raw), "-- migrate:down")
	if !ok {
		t.Fatalf("driver rename migration missing '-- migrate:down' marker")
	}
	return strings.TrimPrefix(up, "-- migrate:up")
}

var driverRenames = [][2]string{
	{"newtron-nvp", "newtron-panel-nvp"},
	{"http-json", "tessellate-census-http"},
}

// An install seeded before the rename keeps its endpoints on the same driver
// row across the upgrade: the migration renames the row, the next boot's seed
// updates it in place, and the endpoint still collects through it.
func TestDriverRenameKeepsAttachedEndpointsCollecting(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
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
	conn := connectDSN(t, dsn)

	all := scope.Set{All: true}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "panel-up"}, all, all, all, all); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-up"}, all, all); err != nil {
		t.Fatal(err)
	}
	comp, drv, node := "panel-up", "newtron-panel-nvp", "edge-up"
	ep, err := gw.CreateEndpoint(ctx, "", storage.EndpointSpec{
		Driver: &drv, Component: &comp, Node: &node,
		Inputs: map[string]string{"host": "10.20.4.61"}, SecretRead: all, SecretBind: all, CanAdmin: true,
	}, all)
	if err != nil {
		t.Fatal(err)
	}
	tasksBefore := endpointTasks(t, ctx, conn, ep.ID)
	if len(tasksBefore) == 0 {
		t.Fatal("the attached endpoint derived no tasks")
	}

	// Stand the database as a pre-rename install left it: the drivers under
	// their old names, the same rows.
	ids := map[string]string{}
	for _, r := range driverRenames {
		var id string
		if err := conn.QueryRow(ctx,
			`update driver set name = $1 where namespace = 'og' and name = $2 returning id`, r[0], r[1]).Scan(&id); err != nil {
			t.Fatalf("stand %s under its old name: %v", r[1], err)
		}
		ids[r[1]] = id
	}

	// The upgrade: the migration, then the new binary's boot seed.
	if _, err := conn.Exec(ctx, driverRenameUpSQL(t)); err != nil {
		t.Fatalf("apply driver rename: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatal(err)
	}

	for _, r := range driverRenames {
		old, renamed := r[0], r[1]
		var n int
		if err := conn.QueryRow(ctx, `select count(*) from driver where name = $1`, old).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("driver %s survived the upgrade", old)
		}
		var id string
		if err := conn.QueryRow(ctx, `select id from driver where namespace = 'og' and name = $1`, renamed).Scan(&id); err != nil {
			t.Fatalf("driver %s missing after the upgrade: %v", renamed, err)
		}
		if id != ids[renamed] {
			t.Fatalf("driver %s is a new row (%s), not the renamed one (%s): attached endpoints would be orphaned", renamed, id, ids[renamed])
		}
	}

	// The endpoint is still attached through the same driver, still has its
	// tasks, and reads as the renamed driver's.
	var attached string
	if err := conn.QueryRow(ctx,
		`select d.name from endpoint e join driver d on d.id = e.driver_id where e.id = $1`, ep.ID).Scan(&attached); err != nil {
		t.Fatal(err)
	}
	if attached != "newtron-panel-nvp" {
		t.Fatalf("the endpoint is attached through %q after the upgrade, want newtron-panel-nvp", attached)
	}
	if after := endpointTasks(t, ctx, conn, ep.ID); len(after) != len(tasksBefore) {
		t.Fatalf("the upgrade moved the endpoint's tasks from %d to %d", len(tasksBefore), len(after))
	}
	// The products that ran through each driver still do: the renamed rows
	// carry them, none is left on a driver by its old name.
	for _, r := range driverRenames {
		var n int
		if err := conn.QueryRow(ctx,
			`select count(*) from product p join driver d on d.id = p.driver_id where d.name = $1`, r[1]).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Fatalf("no product drives through %s after the upgrade", r[1])
		}
	}
}

// The migration is a no-op where the new name already exists (a database the
// new seed has already run on), so it can never collide on the name's unique
// key, and re-running it changes nothing.
func TestDriverRenameIsIdempotent(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
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
	conn := connectDSN(t, dsn)
	var before string
	if err := conn.QueryRow(ctx, `select string_agg(id::text || name, ',' order by name) from driver`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := conn.Exec(ctx, driverRenameUpSQL(t)); err != nil {
			t.Fatalf("apply driver rename: %v", err)
		}
	}
	var after string
	if err := conn.QueryRow(ctx, `select string_agg(id::text || name, ',' order by name) from driver`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("the rename changed an already-renamed database:\n%s\n%s", before, after)
	}
}
