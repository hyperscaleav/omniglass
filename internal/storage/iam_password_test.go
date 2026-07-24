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

// TestPasswordAuth proves the password-credential round trip end to end against a
// real Postgres: bootstrap installs a password, the right password authenticates
// and resolves the owner's grants, the wrong password and an unknown user both
// fail with ErrBadCredentials, and SetPassword rotates the credential. Skipped
// under -short.
func TestPasswordAuth(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()

	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	if err := gw.UpsertRole(ctx, storage.Role{Name: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner role: %v", err)
	}

	hash, err := auth.HashPassword("hunter2-correct")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	created, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{
		Username:     "ops",
		SecretHash:   make([]byte, 32),
		Prefix:       "abcd1234",
		PasswordHash: hash,
	})
	if err != nil || !created {
		t.Fatalf("bootstrap: created=%v err=%v", created, err)
	}

	// Right password authenticates and carries the owner@all grant.
	pr, err := gw.AuthenticatePassword(ctx, "ops", "hunter2-correct")
	if err != nil {
		t.Fatalf("authenticate right password: %v", err)
	}
	if pr.Kind != "human" || pr.Human == nil || pr.Human.Username != "ops" {
		t.Fatalf("unexpected principal: %+v", pr)
	}
	if len(pr.Grants) != 1 || pr.Grants[0].Role != "owner" || pr.Grants[0].ScopeKind != "all" {
		t.Fatalf("expected owner@all grant, got %+v", pr.Grants)
	}

	// Wrong password and an unknown user both fail the same way.
	if _, err := gw.AuthenticatePassword(ctx, "ops", "wrong"); !errors.Is(err, storage.ErrBadCredentials) {
		t.Fatalf("wrong password: want ErrBadCredentials, got %v", err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "nobody", "whatever"); !errors.Is(err, storage.ErrBadCredentials) {
		t.Fatalf("unknown user: want ErrBadCredentials, got %v", err)
	}

	// Rotate the password: the new one works, the old one no longer does.
	newHash, err := auth.HashPassword("fresh-secret")
	if err != nil {
		t.Fatalf("hash new: %v", err)
	}
	ok, err := gw.SetPassword(ctx, "ops", newHash)
	if err != nil || !ok {
		t.Fatalf("set password: ok=%v err=%v", ok, err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "ops", "fresh-secret"); err != nil {
		t.Fatalf("authenticate new password: %v", err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "ops", "hunter2-correct"); !errors.Is(err, storage.ErrBadCredentials) {
		t.Fatalf("old password should fail after rotation, got %v", err)
	}

	// SetPassword on an unknown username is a clean false.
	if ok, err := gw.SetPassword(ctx, "ghost", newHash); err != nil || ok {
		t.Fatalf("set password unknown user: ok=%v err=%v", ok, err)
	}
}

// TestSetPrincipalPassword proves an admin can reset another human's password by id:
// the new secret authenticates and the old one does not, the reset is audited with
// the admin (not the target) as the actor, a non-all scope is refused, and an unknown
// id is not-found. Skipped under -short.
func TestSetPrincipalPassword(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := gw.UpsertRole(ctx, storage.Role{Name: "owner", Official: true, Permissions: []string{"*:*", ">"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, _ := gw.AuthenticateBearer(ctx, make([]byte, 32))
	all := scope.Set{All: true}

	oldHash, _ := auth.HashPassword("orange-boat-42x")
	alice, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice", PasswordHash: oldHash}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// Admin resets alice's password: the new secret works, the old does not.
	newHash, _ := auth.HashPassword("purple-canyon-7")
	if err := gw.SetPrincipalPassword(ctx, root.ID, alice.ID, newHash, all); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "alice", "purple-canyon-7"); err != nil {
		t.Fatalf("new password should authenticate: %v", err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "alice", "orange-boat-42x"); !errors.Is(err, storage.ErrBadCredentials) {
		t.Fatalf("old password should be rejected: %v", err)
	}

	// A non-all scope is refused (principals are not scope-tree scoped).
	if err := gw.SetPrincipalPassword(ctx, root.ID, alice.ID, newHash, scope.Set{}); !errors.Is(err, storage.ErrPrincipalForbidden) {
		t.Fatalf("non-all scope: want ErrPrincipalForbidden, got %v", err)
	}
	// An unknown id is not-found.
	if err := gw.SetPrincipalPassword(ctx, root.ID, "00000000-0000-0000-0000-000000000000", newHash, all); !errors.Is(err, storage.ErrPrincipalNotFound) {
		t.Fatalf("unknown id: want ErrPrincipalNotFound, got %v", err)
	}

	// The reset is audited with the admin (root) as the actor, not alice.
	entries, err := gw.ListAuditLog(ctx, storage.AuditFilter{Limit: 500})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var found bool
	for _, e := range entries {
		if e.Verb == "reset_password" && e.ActorID == root.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a reset_password audit event with the admin as the actor")
	}
}
