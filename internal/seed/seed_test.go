package seed_test

import (
	"context"
	"encoding/json"
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

	// The boot seed plants no standard (ADR-0827): the template catalog
	// offers them and an operator forks the ones they want, so a fresh
	// install, re-seeded, still holds none.
	var standardCount int
	if err := conn.QueryRow(ctx, `select count(*) from standard`).Scan(&standardCount); err != nil {
		t.Fatalf("count standards: %v", err)
	}
	if standardCount != 0 {
		t.Errorf("standards = %d after two boots, want 0: the seed plants none", standardCount)
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
