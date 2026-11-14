package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// A role name means one role per system (#503, ADR-0503). The write paths that
// would create a collision are driven over the wire by
// internal/api/role_name_collision_e2e_test.go; these are the two gateway-only
// halves: a standard's role write refused by a system's own role of the name,
// and a collision that predates the rule refused by the staffing resolver
// rather than resolved to whichever row came back first.

type collisionRig struct {
	t    *testing.T
	ctx  context.Context
	gw   *storage.PG
	conn *pgx.Conn
}

// newCollisionRig opens a seeded gateway with the standard "seed-std", planted
// through the audited create from a template.
func newCollisionRig(t *testing.T) *collisionRig {
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
	if _, err := gw.CreateStandardFromTemplate(ctx, "", storage.StandardFromTemplate{
		Template: "test", Name: "seed-std", Label: "Seed standard",
	}); err != nil {
		t.Fatalf("standard: %v", err)
	}
	return &collisionRig{t: t, ctx: ctx, gw: gw, conn: conn}
}

func (r *collisionRig) count(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.conn.QueryRow(r.ctx, sql, args...).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

// TestStandardRoleWriteRefusedBySystemsOwnRoleOfTheName: a role a conforming
// system already declares itself (an operator withdrew the standard's and
// declared their own on one room) cannot then be declared on the standard under
// the same name, which would make the name mean two roles in that room.
func TestStandardRoleWriteRefusedBySystemsOwnRoleOfTheName(t *testing.T) {
	t.Parallel()
	r := newCollisionRig(t)
	all := scope.Set{All: true}
	std := "seed-std"
	if _, err := r.gw.CreateSystem(r.ctx, "", storage.SystemSpec{Name: "seed-room", StandardID: &std}, all, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	if _, err := r.gw.SetSystemRole(r.ctx, "", "system", "seed-room", storage.SystemRoleSpec{Name: "room-display", AcceptedTypes: []string{"display"}}); err != nil {
		t.Fatalf("the room's own role: %v", err)
	}
	_, err := r.gw.SetSystemRole(r.ctx, "", "standard", std, storage.SystemRoleSpec{Name: "room-display", AcceptedTypes: []string{"display"}})
	var collision *storage.RoleNameCollision
	if !errors.As(err, &collision) {
		t.Fatalf("declaring the standard's role over the room's own = %v, want *RoleNameCollision", err)
	}
	if n := r.count(`select count(*) from system_role where name = 'room-display' and standard_id = (select id from standard where name = 'seed-std')`); n != 0 {
		t.Fatalf("the standard's role landed over the room's own (%d rows), want it refused", n)
	}
	roles, err := r.gw.EffectiveRoles(r.ctx, "seed-room", all)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].FromStandard {
		t.Fatalf("seed-room roles = %+v, want the room's own room-display alone", roles)
	}
}

// TestStaffingRefusesACollisionWrittenBeforeTheRule: a system whose own role
// shares a name with its standard's (written straight to the table, the way a
// database from before the rule can hold one) is refused by every staffing
// write naming it, with both owners named, rather than bound to whichever row
// the resolver read first.
func TestStaffingRefusesACollisionWrittenBeforeTheRule(t *testing.T) {
	t.Parallel()
	r := newCollisionRig(t)
	all := scope.Set{All: true}
	std := "seed-std"
	if _, err := r.gw.CreateSystem(r.ctx, "", storage.SystemSpec{Name: "legacy-room", StandardID: &std}, all, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	if _, err := r.conn.Exec(r.ctx, `
		insert into system_role (owner_kind, standard_id, name, label) values ('standard', (select id from standard where name = 'seed-std'), 'table-mic', 'Table mic');
		insert into system_role (owner_kind, system_id, name, label) values ('system', (select id from system where name = 'legacy-room'), 'table-mic', 'Second mic')`); err != nil {
		t.Fatalf("legacy collision: %v", err)
	}
	bar := storagetest.MintProduct(t, r.ctx, r.gw, "video-bar").Name
	if _, err := r.gw.CreateComponent(r.ctx, "", storage.ComponentSpec{Name: "legacy-bar", ProductName: &bar}, all, all, all, all); err != nil {
		t.Fatalf("component: %v", err)
	}
	err := r.gw.AssignRole(r.ctx, "", "legacy-room", "table-mic", "legacy-bar", all, all, all, all)
	var collision *storage.RoleNameCollision
	if !errors.As(err, &collision) {
		t.Fatalf("assign into a collided name = %v, want *RoleNameCollision", err)
	}
	if collision.Role != "table-mic" || collision.Standard != "seed-std" || collision.System != "legacy-room" {
		t.Fatalf("collision = %+v, want table-mic on seed-std and legacy-room", collision)
	}
	if n := r.count(`select count(*) from system_role_assignment`); n != 0 {
		t.Fatalf("%d assignments written through a collided name, want none", n)
	}
	// Withdrawing the system's own role, arc-scoped as always, resolves it.
	if err := r.gw.DeleteSystemRole(r.ctx, "", "system", "legacy-room", "table-mic"); err != nil {
		t.Fatalf("withdraw the room's role: %v", err)
	}
	if err := r.gw.AssignRole(r.ctx, "", "legacy-room", "table-mic", "legacy-bar", all, all, all, all); err != nil {
		t.Fatalf("assign after the collision is resolved: %v", err)
	}
}
