package seed_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSeedCreatesThePlatformSystemOnce proves the boot seed creates the one
// reserved platform system and its type (#1625), unplaced, and that a second
// boot neither duplicates them nor renames them. It also adopts a node that
// registered before the seed ran: a node created on an unseeded database has
// no platform to join, and the next boot gives it its platform component.
func TestSeedCreatesThePlatformSystemOnce(t *testing.T) {
	t.Parallel()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	all := scope.Set{All: true}

	early, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "early-node"}, all, all)
	if err != nil {
		t.Fatalf("create node before the seed: %v", err)
	}
	if early.PlatformComponentID != nil {
		t.Fatalf("a node on an unseeded database has a platform component %q; there is no platform system for it yet", *early.PlatformComponentID)
	}

	for i := 0; i < 2; i++ {
		if err := seed.Run(ctx, gw); err != nil {
			t.Fatalf("seed run %d: %v", i, err)
		}
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var types int
	if err := conn.QueryRow(ctx, `select count(*) from system_type where namespace = 'og' and name = 'platform'`).Scan(&types); err != nil {
		t.Fatalf("count platform types: %v", err)
	}
	if types != 1 {
		t.Errorf("og platform system types = %d, want 1", types)
	}
	var systems int
	var placed, generated bool
	if err := conn.QueryRow(ctx, `
		select count(*), bool_or(s.location_id is not null or s.parent_id is not null), bool_or(s.name_generated)
		from system s join system_type st on st.id = s.system_type_id
		where st.namespace = 'og' and st.name = 'platform'`).Scan(&systems, &placed, &generated); err != nil {
		t.Fatalf("count platform systems: %v", err)
	}
	if systems != 1 {
		t.Fatalf("systems of the platform type = %d, want exactly 1 after two boots", systems)
	}
	if placed {
		t.Error("the platform system is placed; it is unplaced so only an all-scope read sees it")
	}
	if generated {
		t.Error("the platform system's name is generated; it is the reserved name platform")
	}

	adopted, err := gw.GetNode(ctx, "early-node", all)
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	if adopted.PlatformComponentID == nil {
		t.Fatal("the boot seed did not give the early node its platform component")
	}
	var system string
	if err := conn.QueryRow(ctx, `
		select s.name from system_member m join system s on s.id = m.system_id
		where m.component_id = $1 and m.is_primary`, *adopted.PlatformComponentID).Scan(&system); err != nil {
		t.Fatalf("read the component's system: %v", err)
	}
	if system != "platform" {
		t.Errorf("the early node's component is in %q, want platform", system)
	}
}

// TestSeedBootsBesideAnOperatorSystemNamedPlatform proves a release meeting an
// operator's own unplaced system named `platform` still boots (#1625): the
// operator's system keeps its name and type, and the platform system takes
// the next free name in the unplaced bucket instead.
func TestSeedBootsBesideAnOperatorSystemNamedPlatform(t *testing.T) {
	t.Parallel()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	all := scope.Set{All: true}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// A database from the release before the platform: seeded, with no
	// platform system, and the operator's own system holding the name.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if _, err := conn.Exec(ctx, `delete from system where name = 'platform'`); err != nil {
		t.Fatalf("take the database back before the platform: %v", err)
	}
	mine, err := gw.CreateSystem(ctx, "", storage.SystemSpec{Name: "platform"}, all, all)
	if err != nil {
		t.Fatalf("create the operator's system: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := seed.Run(ctx, gw); err != nil {
			t.Fatalf("seed run %d beside the operator's platform system: %v", i, err)
		}
	}

	var operatorName string
	var operatorType *string
	if err := conn.QueryRow(ctx, `select name, system_type_id::text from system where id = $1`, mine.ID).Scan(&operatorName, &operatorType); err != nil {
		t.Fatalf("read the operator's system: %v", err)
	}
	if operatorName != "platform" || operatorType != nil {
		t.Errorf("the operator's system is now %q of type %v; the seed must leave it as the operator made it", operatorName, operatorType)
	}
	var names []string
	rows, err := conn.Query(ctx, `select s.name from system s join system_type st on st.id = s.system_type_id
		where st.namespace = 'og' and st.name = 'platform'`)
	if err != nil {
		t.Fatalf("read platform systems: %v", err)
	}
	if names, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		t.Fatalf("collect platform systems: %v", err)
	}
	if len(names) != 1 || names[0] != "platform-2" {
		t.Errorf("platform systems = %v, want exactly one, named platform-2 beside the operator's", names)
	}
}

// TestSeedAdoptsANodeBesideAnOperatorComponentOfItsName proves the boot that
// adopts a node registered before the platform still boots when an operator's
// unplaced component already holds the node's name (#1625): registration
// refuses that collision, but a boot cannot, so the adopted node's platform
// component takes the next free name and the operator's component is left
// as it was.
func TestSeedAdoptsANodeBesideAnOperatorComponentOfItsName(t *testing.T) {
	t.Parallel()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	all := scope.Set{All: true}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// A database from the release before the platform: seeded, with no
	// platform system, an operator's component and a node of one name.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if _, err := conn.Exec(ctx, `delete from system where name = 'platform'`); err != nil {
		t.Fatalf("take the database back before the platform: %v", err)
	}
	mine, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "edge-1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("create the operator's component: %v", err)
	}
	if _, err := gw.CreateNode(ctx, "", storage.NodeSpec{Name: "edge-1"}, all, all); err != nil {
		t.Fatalf("create the node: %v", err)
	}

	for i := 0; i < 2; i++ {
		if err := seed.Run(ctx, gw); err != nil {
			t.Fatalf("seed run %d beside the operator's component: %v", i, err)
		}
	}

	n, err := gw.GetNode(ctx, "edge-1", all)
	if err != nil {
		t.Fatalf("read node: %v", err)
	}
	if n.PlatformComponentID == nil {
		t.Fatal("the boot did not adopt the node")
	}
	if *n.PlatformComponentID == mine.ID {
		t.Fatal("the boot took the operator's component as the node's platform component")
	}
	var name string
	if err := conn.QueryRow(ctx, `select name from component where id = $1`, *n.PlatformComponentID).Scan(&name); err != nil {
		t.Fatalf("read the platform component: %v", err)
	}
	if name != "edge-1-2" {
		t.Errorf("the adopted node's platform component is %q, want edge-1-2 beside the operator's edge-1", name)
	}
	var operatorName string
	if err := conn.QueryRow(ctx, `select name from component where id = $1`, mine.ID).Scan(&operatorName); err != nil {
		t.Fatalf("read the operator's component: %v", err)
	}
	if operatorName != "edge-1" {
		t.Errorf("the operator's component is now %q, want edge-1 as the operator left it", operatorName)
	}
}
