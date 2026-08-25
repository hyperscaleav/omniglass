package seed_test

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// The expectations here DERIVE from the embedded YAMLs (via seed.FactsJSON)
// instead of restating them (#804): a catalog change touches the YAML and this
// file keeps agreeing with it by construction. What stays literal is behavior
// the YAML cannot state: idempotency, ownership (official flags), and the
// operator-edit-survives contract.
type catalogFacts struct {
	Roles         []json.RawMessage `json:"roles"`
	LocationTypes []struct {
		ID                 string   `json:"id"`
		Label              string   `json:"label"`
		Icon               string   `json:"icon"`
		AllowedParentTypes []string `json:"allowed_parent_types"`
	} `json:"location_types"`
	Standards []struct {
		ID    string `json:"id"`
		Roles []struct {
			Name          string   `json:"name"`
			Quorum        int      `json:"quorum"`
			AcceptedTypes []string `json:"accepted_types"`
		} `json:"roles"`
	} `json:"standards"`
	Vendors []struct {
		ID      string `json:"id"`
		Website string `json:"website"`
	} `json:"vendors"`
	Products []struct {
		ID         string `json:"id"`
		Properties []struct {
			Name    string `json:"name"`
			Default string `json:"default"`
		} `json:"properties"`
	} `json:"products"`
	SecretTypes []struct {
		ID                    string `json:"id"`
		DefaultAdminSensitive bool   `json:"default_admin_sensitive"`
		Fields                []struct {
			Name string `json:"name"`
		} `json:"fields"`
	} `json:"secret_types"`
}

func seedFacts(t *testing.T) catalogFacts {
	t.Helper()
	raw, err := seed.FactsJSON()
	if err != nil {
		t.Fatalf("render seed facts: %v", err)
	}
	var doc catalogFacts
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse seed facts: %v", err)
	}
	return doc
}

// standardRole finds one shipped standard's role declaration in the facts.
func standardRole(t *testing.T, doc catalogFacts, std, role string) (quorum int, accepted []string) {
	t.Helper()
	for _, s := range doc.Standards {
		if s.ID != std {
			continue
		}
		for _, r := range s.Roles {
			if r.Name == role {
				accepted = append([]string(nil), r.AcceptedTypes...)
				sort.Strings(accepted)
				return r.Quorum, accepted
			}
		}
	}
	t.Fatalf("standard %s role %s not in the embedded YAML", std, role)
	return 0, nil
}

// TestSeedRolesIdempotent proves the boot-seed installs exactly the shipped
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

	facts := seedFacts(t)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	var count int
	if err := conn.QueryRow(ctx, `select count(*) from role where official`).Scan(&count); err != nil {
		t.Fatalf("count roles: %v", err)
	}
	if count != len(facts.Roles) {
		t.Errorf("official roles = %d, want %d (the shipped set; seed not idempotent or incomplete)", count, len(facts.Roles))
	}

	var ownerPerms []string
	if err := conn.QueryRow(ctx, `select permissions from role where name = 'owner'`).Scan(&ownerPerms); err != nil {
		t.Fatalf("read owner role: %v", err)
	}
	if len(ownerPerms) != 1 || ownerPerms[0] != ">" {
		t.Errorf("owner permissions = %v, want [>] (the superuser tail wildcard)", ownerPerms)
	}

	// The shipped location types seed alongside the roles, idempotently (the
	// second Run above must not have duplicated them). Every one is OFFICIAL,
	// which is the inversion #703 made deliberately: this assertion used to
	// read "want 0 (a shipped location type is operator-owned)" and now guards
	// the opposite claim, that the platform owns every row it ships here.
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
	if typeCount != len(facts.LocationTypes) {
		t.Errorf("location_types = %d, want %d", typeCount, len(facts.LocationTypes))
	}
	if err := conn.QueryRow(ctx, `select count(*) from location_type where official`).Scan(&officialTypes); err != nil {
		t.Fatalf("count official location_types: %v", err)
	}
	if officialTypes != len(facts.LocationTypes) {
		t.Errorf("official location_types = %d, want %d (the platform owns every location type it ships, #703)", officialTypes, len(facts.LocationTypes))
	}
	// The registry lists alphabetically by label; the DB agrees with the YAML
	// on who comes first.
	wantTop := ""
	for _, lt := range facts.LocationTypes {
		if wantTop == "" {
			wantTop = lt.ID
			continue
		}
		for _, cur := range facts.LocationTypes {
			if cur.ID == wantTop {
				if lt.Label < cur.Label || (lt.Label == cur.Label && lt.ID < cur.ID) {
					wantTop = lt.ID
				}
			}
		}
	}
	var topType string
	if err := conn.QueryRow(ctx, `select name from location_type order by label, name limit 1`).Scan(&topType); err != nil {
		t.Fatalf("read top location_type: %v", err)
	}
	if topType != wantTop {
		t.Errorf("first-alphabetically location_type = %q, want %q", topType, wantTop)
	}
	// Each shipped type seeds its glyph key, and re-running Run RESTATES it: the
	// seed is `on conflict (name) do update` since #703, so every boot writes
	// the shipped value over whatever the row holds. The icon surviving here is
	// now the upsert asserting it rather than nothing touching it, which is the
	// distinction #657 learned the hard way in the other direction: under
	// insert-if-absent a value REMOVED from the shipped YAML was never withdrawn
	// from a fleet that already seeded it, because no update carried the
	// removal.
	for _, lt := range facts.LocationTypes {
		var icon string
		if err := conn.QueryRow(ctx, `select icon from location_type where name = $1`, lt.ID).Scan(&icon); err != nil {
			t.Fatalf("read %s icon: %v", lt.ID, err)
		}
		if icon != lt.Icon {
			t.Errorf("%s icon = %q, want %q", lt.ID, icon, lt.Icon)
		}
	}

	// The shipped standards seed idempotently, and they are operator-owned
	// (official=false): a standard is example content forked from an in-code
	// template, so the fleet owns it once it lands.
	var standardCount, officialStandards int
	if err := conn.QueryRow(ctx, `select count(*) from standard`).Scan(&standardCount); err != nil {
		t.Fatalf("count standards: %v", err)
	}
	if standardCount != len(facts.Standards) {
		t.Errorf("standards = %d, want %d (seed not idempotent or incomplete)", standardCount, len(facts.Standards))
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
	if _, err := conn.Exec(ctx, `update standard set label = 'Our Huddle Room' where name = 'huddle-room'`); err != nil {
		t.Fatalf("edit seeded standard: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed re-run: %v", err)
	}
	var huddleName string
	if err := conn.QueryRow(ctx, `select label from standard where name = 'huddle-room'`).Scan(&huddleName); err != nil {
		t.Fatalf("read huddle-room: %v", err)
	}
	if huddleName != "Our Huddle Room" {
		t.Errorf("huddle-room label = %q after re-seed, want the operator's edit to survive", huddleName)
	}

	// The shipped standards also declare the roles a conforming system needs
	// filled, seeded once and left alone after: the second Run above must not
	// have duplicated them, and an operator's retune of a quorum must survive
	// the next boot.
	wantMeetingRoles := 0
	for _, s := range facts.Standards {
		if s.ID == "meeting-room" {
			wantMeetingRoles = len(s.Roles)
		}
	}
	if wantMeetingRoles == 0 {
		t.Fatal("meeting-room declares no roles in the embedded YAML, which means this test is not reading the standard it thinks it is")
	}
	var roleCount int
	if err := conn.QueryRow(ctx, `select count(*) from system_role
		where owner_kind = 'standard' and standard_id = (select id from standard where name = 'meeting-room')`).Scan(&roleCount); err != nil {
		t.Fatalf("count meeting-room roles: %v", err)
	}
	if roleCount != wantMeetingRoles {
		t.Errorf("meeting-room roles = %d, want %d (seed not idempotent or incomplete)", roleCount, wantMeetingRoles)
	}
	_, wantBarTypes := standardRole(t, facts, "meeting-room", "video-bar")
	var barTypes []string
	if err := conn.QueryRow(ctx, `select array_agg(ct.name order by ct.name)
		from system_role r join system_role_type rt on rt.role_id = r.id
		join component_type ct on ct.id = rt.component_type_id
		where r.standard_id = (select id from standard where name = 'meeting-room') and r.name = 'video-bar'`).Scan(&barTypes); err != nil {
		t.Fatalf("read video-bar accepted types: %v", err)
	}
	if len(barTypes) != len(wantBarTypes) {
		t.Errorf("video-bar accepted types = %v, want %v", barTypes, wantBarTypes)
	} else {
		for i := range wantBarTypes {
			if barTypes[i] != wantBarTypes[i] {
				t.Errorf("video-bar accepted types = %v, want %v", barTypes, wantBarTypes)
				break
			}
		}
	}
	// The divisible pair's mics are typed by FORM FACTOR (#802): whatever form
	// the YAML declares is what the seeded role accepts.
	shippedQuorum, wantMicTypes := standardRole(t, facts, "divisible-conference", "room-mic")
	var micTypes []string
	if err := conn.QueryRow(ctx, `select array_agg(ct.name order by ct.name)
		from system_role r join system_role_type rt on rt.role_id = r.id
		join component_type ct on ct.id = rt.component_type_id
		where r.standard_id = (select id from standard where name = 'divisible-conference') and r.name = 'room-mic'`).Scan(&micTypes); err != nil {
		t.Fatalf("read room-mic accepted types: %v", err)
	}
	if len(micTypes) != len(wantMicTypes) {
		t.Errorf("room-mic accepted types = %v, want %v", micTypes, wantMicTypes)
	} else {
		for i := range wantMicTypes {
			if micTypes[i] != wantMicTypes[i] {
				t.Errorf("room-mic accepted types = %v, want %v", micTypes, wantMicTypes)
				break
			}
		}
	}
	// The retune is relative to the shipped value, so it can never
	// coincidentally equal what the seed would write anyway.
	retuned := shippedQuorum + 3
	if _, err := conn.Exec(ctx, `update system_role set quorum = $1
		where standard_id = (select id from standard where name = 'divisible-conference') and name = 'room-mic'`, retuned); err != nil {
		t.Fatalf("retune seeded role: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed re-run after retune: %v", err)
	}
	var quorum int
	if err := conn.QueryRow(ctx, `select quorum from system_role
		where standard_id = (select id from standard where name = 'divisible-conference') and name = 'room-mic'`).Scan(&quorum); err != nil {
		t.Fatalf("read room-mic quorum: %v", err)
	}
	if quorum != retuned {
		t.Errorf("room-mic quorum = %d after re-seed, want the operator's retune (%d) to survive", quorum, retuned)
	}

	// The official vendors seed too, idempotently (the second Run
	// above must not have duplicated them), and every seeded row is official
	// (read-only in the API layer).
	var makeCount int
	if err := conn.QueryRow(ctx, `select count(*) from vendor where official`).Scan(&makeCount); err != nil {
		t.Fatalf("count vendors: %v", err)
	}
	if makeCount != len(facts.Vendors) {
		t.Errorf("official vendors = %d, want %d", makeCount, len(facts.Vendors))
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
	// property). kestrel-vroom is the anchor; its expectations derive from the
	// YAML, so only the anchor's existence is pinned here.
	wantContract := -1
	wantModelDefault := ""
	for _, p := range facts.Products {
		if p.ID != "kestrel-vroom" {
			continue
		}
		wantContract = len(p.Properties)
		for _, pp := range p.Properties {
			if pp.Name == "model-number" {
				if err := json.Unmarshal([]byte(pp.Default), &wantModelDefault); err != nil {
					t.Fatalf("decode kestrel-vroom model-number default %q: %v", pp.Default, err)
				}
			}
		}
	}
	if wantContract < 1 || wantModelDefault == "" {
		t.Fatal("kestrel-vroom declares no contract in the embedded YAML, which means this test is not reading the product it thinks it is")
	}
	var barContract int
	if err := conn.QueryRow(ctx, `select count(*) from product_property where product_id = (select id from product where name = 'kestrel-vroom')`).Scan(&barContract); err != nil {
		t.Fatalf("count kestrel-vroom contract: %v", err)
	}
	if barContract != wantContract {
		t.Errorf("kestrel-vroom contract = %d properties, want %d (seed not idempotent or incomplete)", barContract, wantContract)
	}
	var barModelDefault string
	if err := conn.QueryRow(ctx, `select default_value #>> '{}' from product_property
		where product_id = (select id from product where name = 'kestrel-vroom') and property_type_id = (select id from property_type where name = 'model-number')`).Scan(&barModelDefault); err != nil {
		t.Fatalf("read kestrel-vroom model-number default: %v", err)
	}
	if barModelDefault != wantModelDefault {
		t.Errorf("kestrel-vroom model-number default = %q, want %q", barModelDefault, wantModelDefault)
	}

	// Re-running Run keeps the metadata fields, not just the initial insert.
	wantWebsite := ""
	for _, v := range facts.Vendors {
		if v.ID == "boreal" {
			wantWebsite = v.Website
		}
	}
	if wantWebsite == "" {
		t.Fatal("boreal states no website in the embedded YAML, which means this test is not reading the vendor it thinks it is")
	}
	var borealWebsite string
	if err := conn.QueryRow(ctx, `select website from vendor where name = 'boreal'`).Scan(&borealWebsite); err != nil {
		t.Fatalf("read boreal website: %v", err)
	}
	if borealWebsite != wantWebsite {
		t.Errorf("boreal website = %q, want %q", borealWebsite, wantWebsite)
	}

	// The official secret_types seed with their per-field shape.
	var secTypeCount int
	if err := conn.QueryRow(ctx, `select count(*) from secret_type where official`).Scan(&secTypeCount); err != nil {
		t.Fatalf("count secret_types: %v", err)
	}
	if secTypeCount != len(facts.SecretTypes) {
		t.Errorf("official secret_types = %d, want %d", secTypeCount, len(facts.SecretTypes))
	}
	// The type default seeds the create form and the first schema field seeds
	// its shape, both restated from the YAML on every boot.
	for _, st := range facts.SecretTypes {
		var gotDefault bool
		if err := conn.QueryRow(ctx, `select default_admin_sensitive from secret_type where name = $1`, st.ID).Scan(&gotDefault); err != nil {
			t.Fatalf("read %s default_admin_sensitive: %v", st.ID, err)
		}
		if gotDefault != st.DefaultAdminSensitive {
			t.Errorf("%s default_admin_sensitive = %v, want %v", st.ID, gotDefault, st.DefaultAdminSensitive)
		}
		if len(st.Fields) == 0 {
			t.Errorf("%s declares no fields in the embedded YAML", st.ID)
			continue
		}
		var first string
		if err := conn.QueryRow(ctx, `select schema->0->>'name' from secret_type where name = $1`, st.ID).Scan(&first); err != nil {
			t.Fatalf("read %s schema: %v", st.ID, err)
		}
		if first != st.Fields[0].Name {
			t.Errorf("%s first field = %q, want %q", st.ID, first, st.Fields[0].Name)
		}
	}

	// Each shipped type seeds its allowed_parent_types set, matching the
	// implied hierarchy, and re-running Run keeps it.
	for _, lt := range facts.LocationTypes {
		var got []string
		if err := conn.QueryRow(ctx, `select allowed_parent_types from location_type where name = $1`, lt.ID).Scan(&got); err != nil {
			t.Fatalf("read %s allowed_parent_types: %v", lt.ID, err)
		}
		want := lt.AllowedParentTypes
		if len(got) != len(want) {
			t.Errorf("%s allowed_parent_types = %v, want %v", lt.ID, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s allowed_parent_types = %v, want %v", lt.ID, got, want)
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
	if _, err := conn.Exec(ctx, `update role_choice set label = 'Our Conferencing' where name = 'conferencing'`); err != nil {
		t.Fatalf("edit seeded choice: %v", err)
	}
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed re-run after edit: %v", err)
	}
	var label string
	if err := conn.QueryRow(ctx, `select label from role_choice where name = 'conferencing'`).Scan(&label); err != nil {
		t.Fatalf("read edited choice: %v", err)
	}
	if label != "Our Conferencing" {
		t.Errorf("choice label = %q after re-seed, want the operator's edit to survive", label)
	}
}
