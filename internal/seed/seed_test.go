package seed_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// TestSeedRolesIdempotent proves the boot-seed installs exactly the four
// official roles and that running it twice does not duplicate or drift them.
// Skipped under -short.
func TestSeedRolesIdempotent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	// Run twice: idempotency is the property under test.
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

	var count int
	if err := conn.QueryRow(ctx, `select count(*) from role where official`).Scan(&count); err != nil {
		t.Fatalf("count roles: %v", err)
	}
	if count != 5 {
		t.Errorf("official roles = %d, want 5 (viewer, operator, deploy, admin, owner; seed not idempotent or incomplete)", count)
	}

	var ownerPerms []string
	if err := conn.QueryRow(ctx, `select permissions from role where id = 'owner'`).Scan(&ownerPerms); err != nil {
		t.Fatalf("read owner role: %v", err)
	}
	if len(ownerPerms) != 1 || ownerPerms[0] != ">" {
		t.Errorf("owner permissions = %v, want [>] (the superuser tail wildcard)", ownerPerms)
	}

	// The four official location types seed alongside the roles, in
	// alphabetical order by display_name, and idempotently (the second Run
	// above must not have duplicated them).
	var typeCount int
	if err := conn.QueryRow(ctx, `select count(*) from location_type where official`).Scan(&typeCount); err != nil {
		t.Fatalf("count location_types: %v", err)
	}
	if typeCount != 4 {
		t.Errorf("official location_types = %d, want 4", typeCount)
	}
	var topType string
	if err := conn.QueryRow(ctx, `select id from location_type order by display_name, id limit 1`).Scan(&topType); err != nil {
		t.Fatalf("read top location_type: %v", err)
	}
	if topType != "building" {
		t.Errorf("first-alphabetically location_type = %q, want building", topType)
	}
	// Each shipped type seeds its glyph key, and re-running Run keeps it (the icon
	// is part of the idempotent upsert, not just the initial insert).
	for id, wantIcon := range map[string]string{
		"campus": "landmark", "building": "building", "floor": "layers", "room": "door-open",
	} {
		var icon string
		if err := conn.QueryRow(ctx, `select icon from location_type where id = $1`, id).Scan(&icon); err != nil {
			t.Fatalf("read %s icon: %v", id, err)
		}
		if icon != wantIcon {
			t.Errorf("%s icon = %q, want %q", id, icon, wantIcon)
		}
	}

	// The official system types seed too, idempotently.
	var sysTypeCount int
	if err := conn.QueryRow(ctx, `select count(*) from system_type where official`).Scan(&sysTypeCount); err != nil {
		t.Fatalf("count system_types: %v", err)
	}
	if sysTypeCount != 6 {
		t.Errorf("official system_types = %d, want 6", sysTypeCount)
	}

	var compTypeCount int
	if err := conn.QueryRow(ctx, `select count(*) from component_type where official`).Scan(&compTypeCount); err != nil {
		t.Fatalf("count component_types: %v", err)
	}
	if compTypeCount != 10 {
		t.Errorf("official component_types = %d, want 10", compTypeCount)
	}

	// The official secret_types seed with their per-field shape.
	var secTypeCount int
	if err := conn.QueryRow(ctx, `select count(*) from secret_type where official`).Scan(&secTypeCount); err != nil {
		t.Fatalf("count secret_types: %v", err)
	}
	if secTypeCount != 3 {
		t.Errorf("official secret_types = %d, want 3", secTypeCount)
	}
	// The type default seeds the create form: a device type is operational, the
	// OAuth2 integration type is admin-sensitive.
	var snmpDefault, oauthDefault bool
	if err := conn.QueryRow(ctx, `select default_admin_sensitive from secret_type where id = 'snmp-community'`).Scan(&snmpDefault); err != nil {
		t.Fatalf("read snmp-community default_admin_sensitive: %v", err)
	}
	if err := conn.QueryRow(ctx, `select default_admin_sensitive from secret_type where id = 'oauth2-client'`).Scan(&oauthDefault); err != nil {
		t.Fatalf("read oauth2-client default_admin_sensitive: %v", err)
	}
	if snmpDefault {
		t.Error("snmp-community default_admin_sensitive = true, want false (operational device secret)")
	}
	if !oauthDefault {
		t.Error("oauth2-client default_admin_sensitive = false, want true (platform credential)")
	}
	var community string
	if err := conn.QueryRow(ctx, `select schema->0->>'name' from secret_type where id = 'snmp-community'`).Scan(&community); err != nil {
		t.Fatalf("read snmp-community schema: %v", err)
	}
	if community != "community" {
		t.Errorf("snmp-community first field = %q, want community", community)
	}

	// Each shipped type seeds its allowed_parent_types set, matching the
	// implied hierarchy (campus is root-only; a room may sit under a floor, a
	// building, or straight under a campus), and re-running Run keeps it.
	wantParents := map[string][]string{
		"campus": {"root"}, "building": {"root", "campus"},
		"floor": {"building", "campus"}, "room": {"floor", "building", "campus"},
	}
	for id, want := range wantParents {
		var got []string
		if err := conn.QueryRow(ctx, `select allowed_parent_types from location_type where id = $1`, id).Scan(&got); err != nil {
			t.Fatalf("read %s allowed_parent_types: %v", id, err)
		}
		if len(got) != len(want) {
			t.Errorf("%s allowed_parent_types = %v, want %v", id, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s allowed_parent_types = %v, want %v", id, got, want)
				break
			}
		}
	}
}
