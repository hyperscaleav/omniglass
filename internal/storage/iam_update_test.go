package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestUpdatePrincipalHuman proves the admin profile update against a real Postgres:
// display name, email, and username are editable; a rename follows the principal id
// (the password still authenticates under the new username, not the old); a clash is
// refused; and a non-all scope is refused. Skipped under -short.
func TestUpdatePrincipalHuman(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := gw.UpsertRole(ctx, storage.Role{Name: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	owner, _ := gw.AuthenticateBearer(ctx, zeros)
	all := scope.Set{All: true}

	hash, _ := auth.HashPassword("alice-s3cret")
	alice, err := gw.CreateHumanPrincipal(ctx, owner.ID, storage.HumanSpec{Username: "alice", DisplayName: "Alice", PasswordHash: hash}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// Update all three admin-owned fields, including a rename.
	newName, newEmail, newUser := "Alice Cooper", "ac@example.test", "alice2"
	updated, err := gw.UpdatePrincipalHuman(ctx, owner.ID, alice.ID, storage.AdminHumanPatch{
		DisplayName: &newName, Email: &newEmail, Username: &newUser,
	}, all)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Human.Username != "alice2" || updated.Human.DisplayName != "Alice Cooper" || updated.Human.Email != "ac@example.test" {
		t.Fatalf("update did not stick: %+v", updated.Human)
	}

	// The rename follows the principal id: the password authenticates under the new
	// username, and the old username no longer resolves.
	if _, err := gw.AuthenticatePassword(ctx, "alice2", "alice-s3cret"); err != nil {
		t.Fatalf("password should authenticate under the new username: %v", err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "alice", "alice-s3cret"); !errors.Is(err, storage.ErrBadCredentials) {
		t.Fatalf("the old username should no longer resolve, got %v", err)
	}

	// A partial update leaves the other fields alone.
	onlyDisplay := "Just Display"
	again, err := gw.UpdatePrincipalHuman(ctx, owner.ID, alice.ID, storage.AdminHumanPatch{DisplayName: &onlyDisplay}, all)
	if err != nil || again.Human.Username != "alice2" || again.Human.Email != "ac@example.test" {
		t.Fatalf("partial update touched other fields: %+v (err %v)", again.Human, err)
	}

	// A username clash is refused.
	if _, err := gw.CreateHumanPrincipal(ctx, owner.ID, storage.HumanSpec{Username: "bob"}, all); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	bob, _ := gw.ListPrincipals(ctx, all, false)
	var bobID string
	for _, p := range bob {
		if p.Human != nil && p.Human.Username == "bob" {
			bobID = p.ID
		}
	}
	if _, err := gw.UpdatePrincipalHuman(ctx, owner.ID, bobID, storage.AdminHumanPatch{Username: &newUser}, all); !errors.Is(err, storage.ErrUsernameTaken) {
		t.Fatalf("username clash: want ErrUsernameTaken, got %v", err)
	}

	// Scope and not-found guards.
	if _, err := gw.UpdatePrincipalHuman(ctx, owner.ID, alice.ID, storage.AdminHumanPatch{DisplayName: &newName}, scope.Set{}); !errors.Is(err, storage.ErrPrincipalForbidden) {
		t.Fatalf("non-all scope: want ErrPrincipalForbidden, got %v", err)
	}
	if _, err := gw.UpdatePrincipalHuman(ctx, owner.ID, "00000000-0000-0000-0000-000000000000", storage.AdminHumanPatch{DisplayName: &newName}, all); !errors.Is(err, storage.ErrPrincipalNotFound) {
		t.Fatalf("unknown id: want ErrPrincipalNotFound, got %v", err)
	}
}
