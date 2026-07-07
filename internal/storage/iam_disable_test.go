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

// TestDisablePrincipal proves the soft-disable path against a real Postgres: a
// disabled principal cannot authenticate by password or bearer, enabling restores
// it, the last active owner cannot be disabled, and a swap is allowed. Skipped
// under -short.
func TestDisablePrincipal(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := gw.UpsertRole(ctx, storage.Role{ID: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, _ := gw.AuthenticateBearer(ctx, zeros)
	all := scope.Set{All: true}

	pwHash, _ := auth.HashPassword("alice-s3cret")
	alice, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice", PasswordHash: pwHash}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	// Give alice a bearer so we can prove the bearer path is refused too.
	_, bh, bp, _ := auth.NewBearerToken()
	if ok, err := gw.IssueBearerCredential(ctx, "alice", bh, bp); err != nil || !ok {
		t.Fatalf("issue bearer: ok=%v err=%v", ok, err)
	}

	// Disable alice: both auth paths are refused, and the directory shows inactive.
	if err := gw.SetPrincipalActive(ctx, root.ID, alice.ID, false, all); err != nil {
		t.Fatalf("disable: %v", err)
	}
	// A correct password against a disabled account is a distinct, disclosable
	// signal (so the sign-in screen can say "account disabled").
	if _, err := gw.AuthenticatePassword(ctx, "alice", "alice-s3cret"); !errors.Is(err, storage.ErrAccountDisabled) {
		t.Fatalf("disabled + correct password: want ErrAccountDisabled, got %v", err)
	}
	// A WRONG password against a disabled account stays generic, so the endpoint is
	// not an account-state oracle for an attacker who does not hold the password.
	if _, err := gw.AuthenticatePassword(ctx, "alice", "wrong-pw"); !errors.Is(err, storage.ErrBadCredentials) {
		t.Fatalf("disabled + wrong password: want ErrBadCredentials, got %v", err)
	}
	if _, err := gw.AuthenticateBearer(ctx, bh); !errors.Is(err, storage.ErrCredentialNotFound) {
		t.Fatalf("disabled bearer: want ErrCredentialNotFound, got %v", err)
	}
	if got, _ := gw.GetPrincipal(ctx, alice.ID, all); got.Active {
		t.Fatal("alice should read inactive")
	}

	// Enable alice: access is restored.
	if err := gw.SetPrincipalActive(ctx, root.ID, alice.ID, true, all); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "alice", "alice-s3cret"); err != nil {
		t.Fatalf("re-enabled password should work: %v", err)
	}

	// The last active owner cannot be disabled.
	if err := gw.SetPrincipalActive(ctx, root.ID, root.ID, false, all); !errors.Is(err, storage.ErrLastOwner) {
		t.Fatalf("disable last owner: want ErrLastOwner, got %v", err)
	}
	// After a swap (alice becomes an active owner) the old owner can be disabled.
	if _, err := gw.CreateGrant(ctx, root.ID, alice.ID, storage.GrantSpec{Role: "owner", ScopeKind: "all"}, all); err != nil {
		t.Fatalf("grant alice owner: %v", err)
	}
	if err := gw.SetPrincipalActive(ctx, root.ID, root.ID, false, all); err != nil {
		t.Fatalf("disable old owner after swap should succeed: %v", err)
	}

	// Scope and not-found guards.
	if err := gw.SetPrincipalActive(ctx, root.ID, alice.ID, false, scope.Set{}); !errors.Is(err, storage.ErrPrincipalForbidden) {
		t.Fatalf("scoped disable: want ErrPrincipalForbidden, got %v", err)
	}
	if err := gw.SetPrincipalActive(ctx, root.ID, "00000000-0000-0000-0000-000000000000", false, all); !errors.Is(err, storage.ErrPrincipalNotFound) {
		t.Fatalf("unknown disable: want ErrPrincipalNotFound, got %v", err)
	}
}
