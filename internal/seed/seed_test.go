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

	// The four shipped location types seed alongside the roles, in alphabetical
	// order by display_name, and idempotently (the second Run above must not have
	// duplicated them). They are operator-owned example content (the estate shapes
	// its own place vocabulary), so they seed if absent and are not official.
	var typeCount, officialTypes int
	if err := conn.QueryRow(ctx, `select count(*) from location_type`).Scan(&typeCount); err != nil {
		t.Fatalf("count location_types: %v", err)
	}
	if typeCount != 4 {
		t.Errorf("location_types = %d, want 4", typeCount)
	}
	if err := conn.QueryRow(ctx, `select count(*) from location_type where official`).Scan(&officialTypes); err != nil {
		t.Fatalf("count official location_types: %v", err)
	}
	if officialTypes != 0 {
		t.Errorf("official location_types = %d, want 0 (a shipped location type is operator-owned)", officialTypes)
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

	// The shipped standards seed idempotently, and they are operator-owned
	// (official=false): a standard is example content forked from an in-code
	// template, so the estate owns it once it lands.
	var standardCount, officialStandards int
	if err := conn.QueryRow(ctx, `select count(*) from standard`).Scan(&standardCount); err != nil {
		t.Fatalf("count standards: %v", err)
	}
	if standardCount != 6 {
		t.Errorf("standards = %d, want 6 (seed not idempotent or incomplete)", standardCount)
	}
	if err := conn.QueryRow(ctx, `select count(*) from standard where official`).Scan(&officialStandards); err != nil {
		t.Fatalf("count official standards: %v", err)
	}
	if officialStandards != 0 {
		t.Errorf("official standards = %d, want 0 (a shipped standard is operator-owned, not authoritative)", officialStandards)
	}

	// The property that makes them operator-owned: re-seeding must not reassert
	// over an operator's edit. An authoritative upsert would silently revert this
	// on the next boot.
	if _, err := conn.Exec(ctx, `update standard set display_name = 'Our Huddle Room' where id = 'huddle-room'`); err != nil {
		t.Fatalf("edit seeded standard: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed re-run: %v", err)
	}
	var huddleName string
	if err := conn.QueryRow(ctx, `select display_name from standard where id = 'huddle-room'`).Scan(&huddleName); err != nil {
		t.Fatalf("read huddle-room: %v", err)
	}
	if huddleName != "Our Huddle Room" {
		t.Errorf("huddle-room display_name = %q after re-seed, want the operator's edit to survive", huddleName)
	}

	// The shipped standards also declare the roles a conforming system needs
	// filled, seeded once and left alone after: the second Run above must not
	// have duplicated room-mic, and an operator's retune of its quorum must
	// survive the next boot.
	var roleCount int
	if err := conn.QueryRow(ctx, `select count(*) from system_role
		where owner_kind = 'standard' and standard_id = 'meeting-room'`).Scan(&roleCount); err != nil {
		t.Fatalf("count meeting-room roles: %v", err)
	}
	if roleCount != 2 {
		t.Errorf("meeting-room roles = %d, want 2 (seed not idempotent or incomplete)", roleCount)
	}
	var micCaps []string
	if err := conn.QueryRow(ctx, `select array_agg(rc.capability_id order by rc.capability_id)
		from system_role r join role_capability rc on rc.role_id = r.id
		where r.standard_id = 'meeting-room' and r.name = 'room-mic'`).Scan(&micCaps); err != nil {
		t.Fatalf("read room-mic capabilities: %v", err)
	}
	if len(micCaps) != 2 || micCaps[0] != "microphone" || micCaps[1] != "speaker" {
		t.Errorf("room-mic capabilities = %v, want [microphone speaker]", micCaps)
	}
	if _, err := conn.Exec(ctx, `update system_role set quorum = 4
		where standard_id = 'meeting-room' and name = 'room-mic'`); err != nil {
		t.Fatalf("retune seeded role: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed re-run after retune: %v", err)
	}
	var quorum int
	if err := conn.QueryRow(ctx, `select quorum from system_role
		where standard_id = 'meeting-room' and name = 'room-mic'`).Scan(&quorum); err != nil {
		t.Fatalf("read room-mic quorum: %v", err)
	}
	if quorum != 4 {
		t.Errorf("room-mic quorum = %d after re-seed, want the operator's retune (4) to survive", quorum)
	}

	// The official vendors seed too, idempotently (the second Run
	// above must not have duplicated them), and every seeded row is official
	// (read-only in the API layer).
	var makeCount int
	if err := conn.QueryRow(ctx, `select count(*) from vendor where official`).Scan(&makeCount); err != nil {
		t.Fatalf("count vendors: %v", err)
	}
	if makeCount != 8 {
		t.Errorf("official vendors = %d, want 8", makeCount)
	}
	var totalMakeCount int
	if err := conn.QueryRow(ctx, `select count(*) from vendor`).Scan(&totalMakeCount); err != nil {
		t.Fatalf("count all vendors: %v", err)
	}
	if totalMakeCount != makeCount {
		t.Errorf("total vendors = %d, official = %d, want equal (a non-official row leaked in)", totalMakeCount, makeCount)
	}
	// The seeded products ship a declared-property contract, and a second Run
	// upserts it rather than duplicating (the contract is keyed by product +
	// property).
	var barContract int
	if err := conn.QueryRow(ctx, `select count(*) from product_property where product_id = 'cisco-room-bar'`).Scan(&barContract); err != nil {
		t.Fatalf("count cisco-room-bar contract: %v", err)
	}
	if barContract != 3 {
		t.Errorf("cisco-room-bar contract = %d properties, want 3 (seed not idempotent or incomplete)", barContract)
	}
	var barModelDefault string
	if err := conn.QueryRow(ctx, `select default_value #>> '{}' from product_property
		where product_id = 'cisco-room-bar' and property_name = 'model_number'`).Scan(&barModelDefault); err != nil {
		t.Fatalf("read cisco-room-bar model_number default: %v", err)
	}
	if barModelDefault != "Room Bar" {
		t.Errorf("cisco-room-bar model_number default = %q, want %q", barModelDefault, "Room Bar")
	}

	// Re-running Run keeps the metadata fields, not just the initial insert.
	var crestronWebsite string
	if err := conn.QueryRow(ctx, `select website from vendor where id = 'crestron'`).Scan(&crestronWebsite); err != nil {
		t.Fatalf("read crestron website: %v", err)
	}
	if crestronWebsite != "https://www.crestron.com" {
		t.Errorf("crestron website = %q, want https://www.crestron.com", crestronWebsite)
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
