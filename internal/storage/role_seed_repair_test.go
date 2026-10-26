package storage_test

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/hyperscaleav/omniglass/internal/updatemask"
	"github.com/jackc/pgx/v5"
)

// The boot seed's role lane (#508, ADR-0508): a shipped role lands whole or not
// at all, and a later boot repairs the one state the old two-statement seed
// could leave behind (a role nobody revised with a typed-slot set missing
// outright), without ever touching a set an operator owns.

type seedRig struct {
	t    *testing.T
	ctx  context.Context
	gw   *storage.PG
	conn *pgx.Conn
}

func newSeedRig(t *testing.T) *seedRig {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { conn.Close(ctx) })
	if err := gw.UpsertStandard(ctx, storage.Standard{Name: "seed-std", Label: "Seed standard"}); err != nil {
		t.Fatalf("standard: %v", err)
	}
	return &seedRig{t: t, ctx: ctx, gw: gw, conn: conn}
}

// legacyRole inserts a standard role the way the old seed's first statement
// did, with no typed-slot rows: created_at and updated_at are one now().
func (r *seedRig) legacyRole(name string) string {
	r.t.Helper()
	var id string
	if err := r.conn.QueryRow(r.ctx, `
		insert into system_role (owner_kind, standard_id, name, label, quorum)
		values ('standard', (select id from standard where name = 'seed-std'), $1, $1, 1)
		returning id`, name).Scan(&id); err != nil {
		r.t.Fatalf("legacy role %s: %v", name, err)
	}
	return id
}

// slot reads a role's typed slot by name, sorted.
func (r *seedRig) slot(roleID string) (types, products []string) {
	r.t.Helper()
	rows, err := r.conn.Query(r.ctx, `select ct.name from system_role_type rt join component_type ct on ct.id = rt.component_type_id where rt.role_id = $1`, roleID)
	if err != nil {
		r.t.Fatal(err)
	}
	types, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		r.t.Fatal(err)
	}
	rows, err = r.conn.Query(r.ctx, `select p.name from system_role_product rp join product p on p.id = rp.product_id where rp.role_id = $1`, roleID)
	if err != nil {
		r.t.Fatal(err)
	}
	products, err = pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		r.t.Fatal(err)
	}
	sort.Strings(types)
	sort.Strings(products)
	return types, products
}

func (r *seedRig) seedRole(spec storage.SystemRoleSpec) error {
	return r.gw.SeedSystemRole(r.ctx, "standard", "seed-std", spec)
}

func (r *seedRig) count(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.conn.QueryRow(r.ctx, sql, args...).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

// TestSeedSystemRoleLandsWholeOrNotAtAll: a shipped declaration naming a type
// the registry lacks fails the seed and leaves no role behind, so the role can
// never be committed accepting anything.
func TestSeedSystemRoleLandsWholeOrNotAtAll(t *testing.T) {
	r := newSeedRig(t)
	err := r.seedRole(storage.SystemRoleSpec{Name: "probe-role", Quorum: 2, AcceptedTypes: []string{"display", "not-a-type"}})
	if err == nil {
		t.Fatal("seeding a role naming an unknown type succeeded, want an error")
	}
	if n := r.count(`select count(*) from system_role where name = 'probe-role'`); n != 0 {
		t.Fatalf("%d role rows committed by a failed seed, want none", n)
	}
	// Once the declaration is valid the same seed lands it whole.
	if err := r.seedRole(storage.SystemRoleSpec{Name: "probe-role", Quorum: 2, AcceptedTypes: []string{"display", "video-bar"}}); err != nil {
		t.Fatalf("seed a valid role: %v", err)
	}
	var id string
	if err := r.conn.QueryRow(r.ctx, `select id from system_role where name = 'probe-role'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if types, _ := r.slot(id); strings.Join(types, ",") != "display,video-bar" {
		t.Fatalf("seeded types = %v, want display and video-bar", types)
	}
}

// TestSeedSystemRoleRepairsAnUnrevisedRolesMissingSet: a role the old seed left
// without its accepted types (or its pinned products) gets them on the next
// boot, idempotently; a set that holds rows, and a role an operator revised,
// are never touched.
func TestSeedSystemRoleRepairsAnUnrevisedRolesMissingSet(t *testing.T) {
	r := newSeedRig(t)
	pinned := storagetest.MintProduct(t, r.ctx, r.gw, "display").Name

	// Both sets missing: the role insert committed and nothing after it.
	bare := r.legacyRole("bare-role")
	spec := storage.SystemRoleSpec{Name: "bare-role", AcceptedTypes: []string{"display"}, PinnedProducts: []string{pinned}}
	for run := 0; run < 2; run++ {
		if err := r.seedRole(spec); err != nil {
			t.Fatalf("run %d: seed: %v", run, err)
		}
		types, products := r.slot(bare)
		if strings.Join(types, ",") != "display" || strings.Join(products, ",") != pinned {
			t.Fatalf("run %d: repaired slot = %v %v, want display and %s", run, types, products, pinned)
		}
	}

	// Types landed, products did not: the second statement was the one lost.
	half := r.legacyRole("half-role")
	if _, err := r.conn.Exec(r.ctx, `insert into system_role_type (role_id, component_type_id) select $1, id from component_type where name = 'display'`, half); err != nil {
		t.Fatal(err)
	}
	if err := r.seedRole(storage.SystemRoleSpec{Name: "half-role", AcceptedTypes: []string{"display", "video-bar"}, PinnedProducts: []string{pinned}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if types, products := r.slot(half); strings.Join(types, ",") != "display" || strings.Join(products, ",") != pinned {
		t.Fatalf("half-seeded slot = %v %v, want the types it holds kept and the missing products filled", types, products)
	}

	// An operator cleared the accepted types on purpose: the revision bumps
	// updated_at, and the seed leaves the role exactly as the operator left it.
	cleared := r.legacyRole("cleared-role")
	if _, err := r.gw.SetSystemRole(r.ctx, "", "standard", "seed-std", storage.SystemRoleSpec{
		Name: "cleared-role", Write: updatemask.Of(storage.RoleFieldAcceptedTypes),
	}); err != nil {
		t.Fatalf("operator clears the types: %v", err)
	}
	if err := r.seedRole(storage.SystemRoleSpec{Name: "cleared-role", AcceptedTypes: []string{"display"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if types, _ := r.slot(cleared); len(types) != 0 {
		t.Fatalf("revised role's types = %v, want the operator's empty set kept", types)
	}
}
