package storage_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/secret"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// A rename must never disturb a binding. The arcs reference the owner's uuid,
// which a rename does not touch, so this holds for the strongest possible reason:
// there is nothing to rewrite and no cascade to get wrong.
//
// The test is kept from the period when these arcs keyed by name and needed
// `on update cascade` to survive this. It is worth keeping precisely because it
// is now cheap to satisfy: if anyone reintroduces a name-keyed arc, this is what
// notices when the rename starts failing or orphaning rows.
func TestOwnerArcsSurviveARename(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test needs Postgres")
	}
	ctx := context.Background()
	dsn := storagetest.NewDSN(t)
	gw, err := storage.NewPG(ctx, dsn,
		storage.WithSecretProvider(secret.NewStaticProvider(bytes.Repeat([]byte{0x7}, 32))))
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	t.Cleanup(gw.Close)
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	all := scope.Set{All: true}

	// A room needs a campus above it, per the location-type contract.
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "site", LocationType: "campus"}, all); err != nil {
		t.Fatalf("campus: %v", err)
	}
	if _, err := gw.CreateLocation(ctx, "", storage.LocationSpec{
		Name: "old-room", LocationType: "room", ParentName: strptr("site")}, all); err != nil {
		t.Fatalf("location: %v", err)
	}
	if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{
		Name: "codec", LocationName: strptr("old-room")}, all); err != nil {
		t.Fatalf("component: %v", err)
	}

	// One binding of each kind, all owned by the location about to be renamed.
	mustTag(t, gw, "environment", nil, true)
	mustBind(t, gw, "environment", "location", strptr("old-room"), "prod")
	if _, err := gw.CreateVariable(ctx, "", storage.VariableSpec{
		Name: "poll", ValueType: "int", OwnerKind: "location", OwnerName: strptr("old-room"),
		Value: []byte(`30`)}, all); err != nil {
		t.Fatalf("variable: %v", err)
	}
	if _, err := gw.CreateSecret(ctx, "", storage.SecretSpec{
		Name: "admin", SecretType: "basic-auth", OwnerKind: "location", OwnerName: strptr("old-room"),
		Fields: map[string]string{"username": "a", "password": "b"}}, all, true); err != nil {
		t.Fatalf("secret: %v", err)
	}

	comp, err := gw.GetComponent(ctx, "codec", all)
	if err != nil {
		t.Fatalf("get component: %v", err)
	}
	before, err := gw.ResolveTags(ctx, comp.ID, "", all)
	if err != nil {
		t.Fatalf("resolve before: %v", err)
	}
	if winnerOwner(before, "environment") != "old-room" {
		t.Fatalf("before the rename the location should own the winner, got %q",
			winnerOwner(before, "environment"))
	}

	// The rename. Nothing references the name, so this is a single-row update.
	newName := "new-room"
	if _, err := gw.UpdateLocation(ctx, "", "old-room", storage.LocationPatch{Name: &newName}, all, all); err != nil {
		t.Fatalf("rename location: %v", err)
	}

	after, err := gw.ResolveTags(ctx, comp.ID, "", all)
	if err != nil {
		t.Fatalf("resolve after: %v", err)
	}
	if got := winnerOwner(after, "environment"); got != "new-room" {
		t.Errorf("tag owner after the rename = %q, want new-room: the binding points at the id, "+
			"so it follows the entity and reports its current name", got)
	}

	// The variable and the secret followed too, which is what proves the clause is
	// on all three arcs rather than only the one the tag test happens to exercise.
	vars, err := gw.ListVariables(ctx, all)
	if err != nil {
		t.Fatalf("list variables: %v", err)
	}
	if n := ownerNamed(len(vars), func(i int) string { return vars[i].OwnerName }, "new-room"); n != 1 {
		t.Errorf("variables owned by new-room = %d, want 1", n)
	}
	secs, err := gw.ListSecrets(ctx, all, true)
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}
	if n := ownerNamed(len(secs), func(i int) string { return secs[i].OwnerName }, "new-room"); n != 1 {
		t.Errorf("secrets owned by new-room = %d, want 1", n)
	}
}

func winnerOwner(rows []storage.ResolvedTag, key string) string {
	for _, r := range rows {
		if r.Key == key && r.Winner {
			return r.OwnerName
		}
	}
	return ""
}

func ownerNamed(n int, at func(int) string, want string) int {
	c := 0
	for i := range n {
		if at(i) == want {
			c++
		}
	}
	return c
}
