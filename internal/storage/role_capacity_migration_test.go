package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/migrate"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestDuplicateStaffingPreflightRaises proves the #626 one-role-per-component
// migration (20260807140000_role_capacity_and_positions.sql) refuses to
// upgrade an estate that already has a component filling two roles in one
// system, rather than aborting mid-migration on an unnamed 23505: stand the
// database at the schema immediately before that migration, write the
// violating pair directly, and migrate forward over it.
func TestDuplicateStaffingPreflightRaises(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	if err := migrate.RollbackBelow(dsn, "20260807140000"); err != nil {
		t.Fatalf("rollback below the role capacity migration: %v", err)
	}
	conn := connectDSN(t, dsn)
	ctx := context.Background()

	var sysID, compID, roleAID, roleBID string
	if err := conn.QueryRow(ctx, `insert into system (name) values ('dup-sys') returning id`).Scan(&sysID); err != nil {
		t.Fatalf("system: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into component (name, product_id) values ('dup-comp', (select id from product where name = 'generic-device')) returning id`,
	).Scan(&compID); err != nil {
		t.Fatalf("component: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into system_role (owner_kind, system_id, name, display_name) values ('system', $1, 'role-a', 'Role A') returning id`,
		sysID).Scan(&roleAID); err != nil {
		t.Fatalf("role a: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`insert into system_role (owner_kind, system_id, name, display_name) values ('system', $1, 'role-b', 'Role B') returning id`,
		sysID).Scan(&roleBID); err != nil {
		t.Fatalf("role b: %v", err)
	}
	mustExec(t, conn, `insert into system_role_assignment (role_id, component_id, system_id) values ($1, $2, $3)`, roleAID, compID, sysID)
	mustExec(t, conn, `insert into system_role_assignment (role_id, component_id, system_id) values ($1, $2, $3)`, roleBID, compID, sysID)

	err := migrate.Run(dsn)
	if err == nil {
		t.Fatal("migrate over a component filling two roles in one system succeeded, want a loud refusal")
	}
	if !strings.Contains(err.Error(), "dup-sys/dup-comp") {
		t.Errorf("refusal error = %v, want it to name dup-sys/dup-comp", err)
	}
}
