package storage_test

import (
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A role name means one role per system (#503, ADR-0503). The write paths that
// would create a collision are driven over the wire by
// internal/api/role_name_collision_e2e_test.go; these are the two gateway-only
// halves: the boot seed deferring to a system's own role, and a collision that
// predates the rule refused by the staffing resolver rather than resolved to
// whichever row came back first.

// TestSeedDefersToASystemsOwnRoleOfTheName: a shipped role that a conforming
// system already declares itself (an operator withdrew the shipped one and
// declared their own on one room, and the next boot reseeds) is not installed.
func TestSeedDefersToASystemsOwnRoleOfTheName(t *testing.T) {
	r := newSeedRig(t)
	all := scope.Set{All: true}
	std := "seed-std"
	if _, err := r.gw.CreateSystem(r.ctx, "", storage.SystemSpec{Name: "seed-room", StandardID: &std}, all, all); err != nil {
		t.Fatalf("system: %v", err)
	}
	if _, err := r.gw.SetSystemRole(r.ctx, "", "system", "seed-room", storage.SystemRoleSpec{Name: "room-display", AcceptedTypes: []string{"display"}}); err != nil {
		t.Fatalf("the room's own role: %v", err)
	}
	if err := r.seedRole(storage.SystemRoleSpec{Name: "room-display", AcceptedTypes: []string{"display"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if n := r.count(`select count(*) from system_role where name = 'room-display' and standard_id = (select id from standard where name = 'seed-std')`); n != 0 {
		t.Fatalf("the seed installed the shipped role over the room's own (%d rows), want it deferred", n)
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
	r := newSeedRig(t)
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
