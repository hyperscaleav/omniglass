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
	if err := conn.QueryRow(ctx, `select permissions from role where name = 'owner'`).Scan(&ownerPerms); err != nil {
		t.Fatalf("read owner role: %v", err)
	}
	if len(ownerPerms) != 1 || ownerPerms[0] != ">" {
		t.Errorf("owner permissions = %v, want [>] (the superuser tail wildcard)", ownerPerms)
	}

	// The four shipped location types seed alongside the roles, in alphabetical
	// order by display_name, and idempotently (the second Run above must not have
	// duplicated them). Every one is OFFICIAL, which is the inversion #703 made
	// deliberately: this assertion used to read "want 0 (a shipped location type
	// is operator-owned)" and now guards the opposite claim, that the platform
	// owns every row it ships here.
	//
	// What it protects is the withdrawal. Operator-owned rows are why the seed
	// could not be authoritative, and an insert-if-absent seed can add a shipped
	// value but never remove one. Owning the rows is what lets the boot rewrite
	// them, and an operator loses nothing by it: their edit forks into
	// registry_shadow and resolves over the row (ADR-0095). Read together with
	// the count above, it also says no operator row leaked into the shipped set.
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
	if officialTypes != 4 {
		t.Errorf("official location_types = %d, want 4 (the platform owns every location type it ships, #703)", officialTypes)
	}
	var topType string
	if err := conn.QueryRow(ctx, `select name from location_type order by display_name, name limit 1`).Scan(&topType); err != nil {
		t.Fatalf("read top location_type: %v", err)
	}
	if topType != "building" {
		t.Errorf("first-alphabetically location_type = %q, want building", topType)
	}
	// Each shipped type seeds its glyph key, and re-running Run RESTATES it: the
	// seed is `on conflict (name) do update` since #703, so every boot writes
	// the shipped value over whatever the row holds. The icon surviving here is
	// now the upsert asserting it rather than nothing touching it, which is the
	// distinction #657 learned the hard way in the other direction: under
	// insert-if-absent a value REMOVED from the shipped YAML was never withdrawn
	// from an estate that already seeded it, because no update carried the
	// removal.
	for name, wantIcon := range map[string]string{
		"campus": "landmark", "building": "building", "floor": "layers", "room": "door-open",
	} {
		var icon string
		if err := conn.QueryRow(ctx, `select icon from location_type where name = $1`, name).Scan(&icon); err != nil {
			t.Fatalf("read %s icon: %v", name, err)
		}
		if icon != wantIcon {
			t.Errorf("%s icon = %q, want %q", name, icon, wantIcon)
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
	if _, err := conn.Exec(ctx, `update standard set display_name = 'Our Huddle Room' where name = 'huddle-room'`); err != nil {
		t.Fatalf("edit seeded standard: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed re-run: %v", err)
	}
	var huddleName string
	if err := conn.QueryRow(ctx, `select display_name from standard where name = 'huddle-room'`).Scan(&huddleName); err != nil {
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
		where owner_kind = 'standard' and standard_id = (select id from standard where name = 'meeting-room')`).Scan(&roleCount); err != nil {
		t.Fatalf("count meeting-room roles: %v", err)
	}
	if roleCount != 2 {
		t.Errorf("meeting-room roles = %d, want 2 (seed not idempotent or incomplete)", roleCount)
	}
	var micTypes []string
	if err := conn.QueryRow(ctx, `select array_agg(ct.name order by ct.name)
		from system_role r join system_role_type rt on rt.role_id = r.id
		join component_type ct on ct.id = rt.component_type_id
		where r.standard_id = (select id from standard where name = 'meeting-room') and r.name = 'room-mic'`).Scan(&micTypes); err != nil {
		t.Fatalf("read room-mic accepted types: %v", err)
	}
	if len(micTypes) != 1 || micTypes[0] != "video-bar" {
		t.Errorf("room-mic accepted types = %v, want [video-bar]", micTypes)
	}
	if _, err := conn.Exec(ctx, `update system_role set quorum = 4
		where standard_id = (select id from standard where name = 'meeting-room') and name = 'room-mic'`); err != nil {
		t.Fatalf("retune seeded role: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed re-run after retune: %v", err)
	}
	var quorum int
	if err := conn.QueryRow(ctx, `select quorum from system_role
		where standard_id = (select id from standard where name = 'meeting-room') and name = 'room-mic'`).Scan(&quorum); err != nil {
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
	if err := conn.QueryRow(ctx, `select count(*) from product_property where product_id = (select id from product where name = 'cisco-room-bar')`).Scan(&barContract); err != nil {
		t.Fatalf("count cisco-room-bar contract: %v", err)
	}
	if barContract != 3 {
		t.Errorf("cisco-room-bar contract = %d properties, want 3 (seed not idempotent or incomplete)", barContract)
	}
	var barModelDefault string
	if err := conn.QueryRow(ctx, `select default_value #>> '{}' from product_property
		where product_id = (select id from product where name = 'cisco-room-bar') and property_type_id = (select id from property_type where name = 'model-number')`).Scan(&barModelDefault); err != nil {
		t.Fatalf("read cisco-room-bar model-number default: %v", err)
	}
	if barModelDefault != "Room Bar" {
		t.Errorf("cisco-room-bar model-number default = %q, want %q", barModelDefault, "Room Bar")
	}

	// Re-running Run keeps the metadata fields, not just the initial insert.
	var crestronWebsite string
	if err := conn.QueryRow(ctx, `select website from vendor where name = 'crestron'`).Scan(&crestronWebsite); err != nil {
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
	if err := conn.QueryRow(ctx, `select default_admin_sensitive from secret_type where name = 'snmp-community'`).Scan(&snmpDefault); err != nil {
		t.Fatalf("read snmp-community default_admin_sensitive: %v", err)
	}
	if err := conn.QueryRow(ctx, `select default_admin_sensitive from secret_type where name = 'oauth2-client'`).Scan(&oauthDefault); err != nil {
		t.Fatalf("read oauth2-client default_admin_sensitive: %v", err)
	}
	if snmpDefault {
		t.Error("snmp-community default_admin_sensitive = true, want false (operational device secret)")
	}
	if !oauthDefault {
		t.Error("oauth2-client default_admin_sensitive = false, want true (platform credential)")
	}
	var community string
	if err := conn.QueryRow(ctx, `select schema->0->>'name' from secret_type where name = 'snmp-community'`).Scan(&community); err != nil {
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
		if err := conn.QueryRow(ctx, `select allowed_parent_types from location_type where name = $1`, id).Scan(&got); err != nil {
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

// TestSeedRoleChoicesIdempotent proves the shipped huddle-room conferencing
// choice (#626) seeds exactly once (the second Run above must not duplicate
// the choice, its two alternates, or any of the roles that join them) and
// that every conferencing role resolves a non-null alternate_id pointing at
// the alternate its Choice/Alternate fields name in standards.yaml: the
// failure mode seedStandardRoles running before seedStandardChoices would
// produce (nothing to resolve against yet) or a name typo between the two
// blocks would produce (a silently unconditional role instead of a grouped
// one).
func TestSeedRoleChoicesIdempotent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

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

	var choiceCount int
	if err := conn.QueryRow(ctx, `
		select count(*) from role_choice
		where owner_kind = 'standard' and standard_id = (select id from standard where name = 'huddle-room')
		  and name = 'conferencing'`).Scan(&choiceCount); err != nil {
		t.Fatalf("count conferencing choices: %v", err)
	}
	if choiceCount != 1 {
		t.Errorf("conferencing choices under huddle-room = %d, want 1 (seed not idempotent or incomplete)", choiceCount)
	}

	var altCount int
	if err := conn.QueryRow(ctx, `
		select count(*) from choice_alternate ca
		join role_choice rc on rc.id = ca.choice_id
		where rc.owner_kind = 'standard' and rc.standard_id = (select id from standard where name = 'huddle-room')
		  and rc.name = 'conferencing'`).Scan(&altCount); err != nil {
		t.Fatalf("count conferencing alternates: %v", err)
	}
	if altCount != 2 {
		t.Errorf("conferencing alternates = %d, want 2 (all-in-one, component-system; seed not idempotent or incomplete)", altCount)
	}

	// Positions land 1..n from standards.yaml's list order, not whatever the
	// planner happens to return: this is what internal/health.Choice.Active
	// reads to break a tie, and TestAlternateTieBreaksByPosition pins that
	// nothing else in the read path orders alternates.
	wantPos := map[string]int{"all-in-one": 1, "component-system": 2}
	for altName, want := range wantPos {
		var pos int
		if err := conn.QueryRow(ctx, `
			select ca.position from choice_alternate ca
			join role_choice rc on rc.id = ca.choice_id
			where rc.owner_kind = 'standard' and rc.standard_id = (select id from standard where name = 'huddle-room')
			  and rc.name = 'conferencing' and ca.name = $1`, altName).Scan(&pos); err != nil {
			t.Fatalf("read %s position: %v", altName, err)
		}
		if pos != want {
			t.Errorf("%s position = %d, want %d", altName, pos, want)
		}
	}

	// Every conferencing role resolves a non-null alternate_id, and it
	// points at the alternate under huddle-room's own choice, not some
	// other owner's: the composite FK (system_role_alternate_fk) is what
	// would refuse it otherwise, but this pins that the seed data itself
	// never relied on that refusal firing.
	rows, err := conn.Query(ctx, `
		select sr.name, ca.name from system_role sr
		join choice_alternate ca on ca.id = sr.alternate_id
		join role_choice rc on rc.id = ca.choice_id
		where sr.owner_kind = 'standard' and sr.standard_id = (select id from standard where name = 'huddle-room')
		  and rc.name = 'conferencing'
		order by sr.name`)
	if err != nil {
		t.Fatalf("query conferencing roles: %v", err)
	}
	defer rows.Close()
	want := map[string]string{
		"conf-amp": "component-system", "conf-bar": "all-in-one", "conf-camera": "component-system",
		"conf-codec": "component-system", "conf-dsp": "component-system", "conf-mic": "component-system",
	}
	got := map[string]string{}
	for rows.Next() {
		var role, alt string
		if err := rows.Scan(&role, &alt); err != nil {
			t.Fatalf("scan conferencing role/alternate: %v", err)
		}
		got[role] = alt
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate conferencing roles: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("conferencing roles resolving an alternate = %v, want %v (a role with no alternate_id is missing here)", got, want)
	}
	for role, wantAlt := range want {
		if got[role] != wantAlt {
			t.Errorf("%s joins alternate %q, want %q", role, got[role], wantAlt)
		}
	}

	// An operator's edit to the choice survives the second seed run, the
	// same seed-if-absent contract every other shipped declaration keeps.
	if _, err := conn.Exec(ctx, `update role_choice set display_name = 'Our Conferencing' where name = 'conferencing'`); err != nil {
		t.Fatalf("edit seeded choice: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed re-run after edit: %v", err)
	}
	var displayName string
	if err := conn.QueryRow(ctx, `select display_name from role_choice where name = 'conferencing'`).Scan(&displayName); err != nil {
		t.Fatalf("read edited choice: %v", err)
	}
	if displayName != "Our Conferencing" {
		t.Errorf("choice display_name = %q after re-seed, want the operator's edit to survive", displayName)
	}
}
