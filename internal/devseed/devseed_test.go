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

// officialMakes are the component_make ids the boot seed installs; every
// fixture component model must reference one of them, else the model's
// make_id foreign key fails at seed time.
var officialMakes = map[string]bool{
	"crestron": true, "biamp": true, "qsc": true, "shure": true,
	"cisco": true, "extron": true, "sony": true, "samsung": true,
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

	if len(doc.ComponentModels) == 0 {
		t.Fatal("fixtures empty: 0 component models")
	}
	for _, m := range doc.ComponentModels {
		if m.ID == "" || m.DisplayName == "" || m.MakeID == "" {
			t.Errorf("component model %+v missing id, display_name, or make_id", m)
		}
		if !officialMakes[m.MakeID] {
			t.Errorf("component model %q references unknown make %q", m.ID, m.MakeID)
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
	var models int
	if err := conn.QueryRow(ctx, `select count(*) from component_model`).Scan(&models); err != nil {
		t.Fatalf("count component models: %v", err)
	}
	if models != 3 {
		t.Errorf("component models = %d, want 3", models)
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
