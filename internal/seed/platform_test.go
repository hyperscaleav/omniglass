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
