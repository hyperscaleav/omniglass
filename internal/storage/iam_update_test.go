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

// TestUpdatePerson proves the admin profile update against a real Postgres:
// label, email, and username are editable; a rename follows the user id
// (the password still authenticates under the new username, not the old); a clash is
// refused; and a non-all scope is refused. Skipped under -short.
func TestUpdatePerson(t *testing.T) {
	t.Parallel()
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
	alice, err := gw.CreatePerson(ctx, owner.ID, storage.PersonSpec{Username: "alice", Label: "Alice", PasswordHash: hash}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// Update all three admin-owned fields, including a rename.
	newName, newEmail, newUser := "Alice Cooper", "ac@example.test", "alice2"
	updated, err := gw.UpdatePerson(ctx, owner.ID, alice.ID, storage.AdminPersonPatch{
		Label: &newName, Email: &newEmail, Username: &newUser,
	}, all)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Person.Username != "alice2" || updated.Person.Label != "Alice Cooper" || updated.Person.Email != "ac@example.test" {
		t.Fatalf("update did not stick: %+v", updated.Person)
	}

	// The rename follows the user id: the password authenticates under the new
	// username, and the old username no longer resolves.
	if _, err := gw.AuthenticatePassword(ctx, "alice2", "alice-s3cret"); err != nil {
		t.Fatalf("password should authenticate under the new username: %v", err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "alice", "alice-s3cret"); !errors.Is(err, storage.ErrBadCredentials) {
		t.Fatalf("the old username should no longer resolve, got %v", err)
	}

	// A partial update leaves the other fields alone.
	onlyDisplay := "Just Display"
	again, err := gw.UpdatePerson(ctx, owner.ID, alice.ID, storage.AdminPersonPatch{Label: &onlyDisplay}, all)
	if err != nil || again.Person.Username != "alice2" || again.Person.Email != "ac@example.test" {
		t.Fatalf("partial update touched other fields: %+v (err %v)", again.Person, err)
	}

	// A username clash is refused.
	if _, err := gw.CreatePerson(ctx, owner.ID, storage.PersonSpec{Username: "bob"}, all); err != nil {
		t.Fatalf("create bob: %v", err)
	}
	bob, _ := gw.ListUsers(ctx, all, false)
	var bobID string
	for _, p := range bob {
		if p.Person != nil && p.Person.Username == "bob" {
			bobID = p.ID
		}
	}
	if _, err := gw.UpdatePerson(ctx, owner.ID, bobID, storage.AdminPersonPatch{Username: &newUser}, all); !errors.Is(err, storage.ErrUsernameTaken) {
		t.Fatalf("username clash: want ErrUsernameTaken, got %v", err)
	}

	// Scope and not-found guards.
	if _, err := gw.UpdatePerson(ctx, owner.ID, alice.ID, storage.AdminPersonPatch{Label: &newName}, scope.Set{}); !errors.Is(err, storage.ErrUserForbidden) {
		t.Fatalf("non-all scope: want ErrUserForbidden, got %v", err)
	}
	if _, err := gw.UpdatePerson(ctx, owner.ID, "00000000-0000-0000-0000-000000000000", storage.AdminPersonPatch{Label: &newName}, all); !errors.Is(err, storage.ErrUserNotFound) {
		t.Fatalf("unknown id: want ErrUserNotFound, got %v", err)
	}
}
