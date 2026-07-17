package devseed_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/devseed"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// officialRoles are the role ids the boot seed installs; every fixture grant must
// name one of them, else the grant's role_id foreign key fails at seed time.
var officialRoles = map[string]bool{
	"viewer": true, "operator": true, "deploy": true, "admin": true, "owner": true,
}

// TestFixturesShape is a pure unit check on the embedded fixtures: the tree is
// well formed (every parent named before its children), every user carries a
// password, and every grant references a real role and (when scoped) a location
// declared in the same document. It needs no database, so it runs under -short.
func TestFixturesShape(t *testing.T) {
	doc, err := devseed.Fixtures()
	if err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	if len(doc.Locations) == 0 || len(doc.Users) == 0 {
		t.Fatalf("fixtures empty: %d locations, %d users", len(doc.Locations), len(doc.Users))
	}

	seenLoc := map[string]bool{}
	for _, l := range doc.Locations {
		if l.Name == "" || l.Type == "" {
			t.Errorf("location %+v missing name or type", l)
		}
		if l.Parent != "" && !seenLoc[l.Parent] {
			t.Errorf("location %q references parent %q not declared before it", l.Name, l.Parent)
		}
		seenLoc[l.Name] = true
	}

	for _, u := range doc.Users {
		if u.Username == "" || u.Password == "" {
			t.Errorf("user %+v missing username or password", u)
		}
		if len(u.Grants) == 0 {
			t.Errorf("user %q has no grants (a dev user without access is not useful)", u.Username)
		}
		for _, g := range u.Grants {
			if !officialRoles[g.Role] {
				t.Errorf("user %q grant references unknown role %q", u.Username, g.Role)
			}
			if g.ScopeKind != "all" && !seenLoc[g.ScopeRef] {
				t.Errorf("user %q grant scoped to location %q not in the fixtures", u.Username, g.ScopeRef)
			}
		}
	}

	// Field definitions declare a typed field on a component_type; each needs a name
	// and a data_type in the supported scalar set. seenField lets a later field_value
	// check its field is actually declared in the same document.
	validDataTypes := map[string]bool{"string": true, "int": true, "float": true, "bool": true, "json": true}
	seenField := map[string]bool{}
	var assetTagDisplay string
	for _, fd := range doc.FieldDefinitions {
		if fd.Name == "" || fd.ComponentType == "" {
			t.Errorf("field definition %+v missing name or component_type", fd)
		}
		if !validDataTypes[fd.DataType] {
			t.Errorf("field definition %q has unsupported data_type %q", fd.Name, fd.DataType)
		}
		if fd.Name == "asset_tag" {
			assetTagDisplay = fd.DisplayName
		}
		seenField[fd.Name] = true
	}
	// A field carries an optional human label; the seed sets one on asset_tag.
	if assetTagDisplay != "Asset tag" {
		t.Errorf("asset_tag display_name = %q, want \"Asset tag\"", assetTagDisplay)
	}

	// Components place a device in the estate; a component that names a location must
	// name one declared in this document (the seed resolves the placement by name).
	seenComp := map[string]bool{}
	for _, c := range doc.Components {
		if c.Name == "" || c.ComponentType == "" {
			t.Errorf("component %+v missing name or component_type", c)
		}
		if c.Location != "" && !seenLoc[c.Location] {
			t.Errorf("component %q placed at location %q not in the fixtures", c.Name, c.Location)
		}
		seenComp[c.Name] = true
	}

	// Field values set a literal on a component for a field: both must be declared in
	// this document, else the seed's create fails at run time.
	for _, fv := range doc.FieldValues {
		if !seenComp[fv.Component] {
			t.Errorf("field value references component %q not in the fixtures", fv.Component)
		}
		if !seenField[fv.Field] {
			t.Errorf("field value on %q references field %q not declared", fv.Component, fv.Field)
		}
	}
}

// TestRunIdempotent proves devseed.Run lands the example estate through the
// Storage Gateway and that a second run neither duplicates nor errors: make dev
// runs it on every start. Reference data (roles, location types) must exist
// first, so the boot seed runs ahead of it, exactly as bootstrap does. Skipped
// under -short by the testcontainer harness.
func TestRunIdempotent(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("boot seed: %v", err)
	}
	// Run twice: idempotency is the property under test.
	for i := 0; i < 2; i++ {
		if err := devseed.Run(ctx, gw, ""); err != nil {
			t.Fatalf("devseed run %d: %v", i, err)
		}
	}

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	// Counts prove idempotency: the second Run added nothing.
	var locs, humans, grants int
	if err := conn.QueryRow(ctx, `select count(*) from location`).Scan(&locs); err != nil {
		t.Fatalf("count locations: %v", err)
	}
	if locs != 13 {
		t.Errorf("locations = %d, want 13 (seed not idempotent or incomplete)", locs)
	}
	// A multi-site estate: three campuses, not one.
	var campuses int
	if err := conn.QueryRow(ctx, `select count(*) from location where location_type = 'campus'`).Scan(&campuses); err != nil {
		t.Fatalf("count campuses: %v", err)
	}
	if campuses != 3 {
		t.Errorf("campuses = %d, want 3 (hq, east, airport)", campuses)
	}
	if err := conn.QueryRow(ctx, `select count(*) from principal where kind = 'human'`).Scan(&humans); err != nil {
		t.Fatalf("count humans: %v", err)
	}
	if humans != 3 {
		t.Errorf("human principals = %d, want 3", humans)
	}
	if err := conn.QueryRow(ctx, `select count(*) from principal_grant`).Scan(&grants); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	if grants != 3 {
		t.Errorf("grants = %d, want 3", grants)
	}

	// The field primitive seeds three definitions, one component, and one value
	// override. The counts prove the second Run added none of them: the definition,
	// component, and value loops are each idempotent.
	var fieldDefs, comps, fieldVals int
	if err := conn.QueryRow(ctx, `select count(*) from field_definition`).Scan(&fieldDefs); err != nil {
		t.Fatalf("count field definitions: %v", err)
	}
	if fieldDefs != 3 {
		t.Errorf("field definitions = %d, want 3 (seed not idempotent or incomplete)", fieldDefs)
	}
	if err := conn.QueryRow(ctx, `select count(*) from component`).Scan(&comps); err != nil {
		t.Fatalf("count components: %v", err)
	}
	if comps != 1 {
		t.Errorf("components = %d, want 1 (lobby-display)", comps)
	}
	if err := conn.QueryRow(ctx, `select count(*) from field_value`).Scan(&fieldVals); err != nil {
		t.Fatalf("count field values: %v", err)
	}
	if fieldVals != 1 {
		t.Errorf("field values = %d, want 1 (diagonal_inches override)", fieldVals)
	}

	// The tree links resolve: the west building hangs under the hq campus.
	var parentName string
	if err := conn.QueryRow(ctx, `
		select p.name from location c join location p on p.id = c.parent_id
		where c.name = 'hq-west'`).Scan(&parentName); err != nil {
		t.Fatalf("read hq-west parent: %v", err)
	}
	if parentName != "hq" {
		t.Errorf("hq-west parent = %q, want hq", parentName)
	}

	// Each seeded user has a password credential (so they can sign in to make dev).
	var pwCreds int
	if err := conn.QueryRow(ctx, `
		select count(*) from credential
		where kind = 'password'
		  and principal_id in (select principal_id from human
		    where username in ('operator', 'viewer-hq', 'tech-east'))`).Scan(&pwCreds); err != nil {
		t.Fatalf("count password creds: %v", err)
	}
	if pwCreds != 3 {
		t.Errorf("password credentials for seeded users = %d, want 3", pwCreds)
	}

	// Grant shapes: operator is all-scoped; viewer-hq reads the hq subtree; tech-east
	// deploys under the East campus excluding its root. The region-scoped users name
	// their region; the all-scoped one does not.
	assertGrant(t, conn, ctx, "operator", "operator", "all", "", "subtree")
	assertGrant(t, conn, ctx, "viewer-hq", "viewer", "location", "hq", "subtree")
	assertGrant(t, conn, ctx, "tech-east", "deploy", "location", "east", "subtree_excl_root")
}

// assertGrant checks a seeded user holds exactly the expected role at the
// expected scope. scopeName is the location name the grant points at ("" for the
// all scope), resolved to its id for the comparison.
func assertGrant(t *testing.T, conn *pgx.Conn, ctx context.Context, username, role, scopeKind, scopeName, scopeOp string) {
	t.Helper()
	var gotKind, gotOp string
	var gotScopeID *string
	if err := conn.QueryRow(ctx, `
		select g.scope_kind, g.scope_op, g.scope_id
		from principal_grant g join human h on h.principal_id = g.principal_id
		where h.username = $1 and g.role_id = $2`, username, role).Scan(&gotKind, &gotOp, &gotScopeID); err != nil {
		t.Fatalf("read grant for %s/%s: %v", username, role, err)
	}
	if gotKind != scopeKind || gotOp != scopeOp {
		t.Errorf("%s grant = kind %q op %q, want kind %q op %q", username, gotKind, gotOp, scopeKind, scopeOp)
	}
	if scopeName == "" {
		if gotScopeID != nil {
			t.Errorf("%s all-scope grant has scope_id %v, want null", username, *gotScopeID)
		}
		return
	}
	var wantID string
	if err := conn.QueryRow(ctx, `select id from location where name = $1`, scopeName).Scan(&wantID); err != nil {
		t.Fatalf("resolve location %q: %v", scopeName, err)
	}
	if gotScopeID == nil || *gotScopeID != wantID {
		t.Errorf("%s grant scope_id = %v, want %s (%s)", username, gotScopeID, wantID, scopeName)
	}
}
