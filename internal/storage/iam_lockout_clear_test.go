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

// TestPasswordRotationClearsLockout proves that rotating a password clears the
// brute-force lockout (issue #171): before this, the lock only expired lazily at the
// next login, so an admin reset left the account locked for the rest of the window.
// Both rotation lanes clear it: SetPrincipalPassword (the admin reset, keyed on id,
// the reachable intervention while an account is locked) and SetPassword (the
// self-service change / CLI set-password, keyed on username). A locked account whose
// password is rotated authenticates immediately with the new secret, with no wait for
// the window: were the lock still set, AuthenticatePassword would return
// ErrAccountLocked whatever the password. Skipped under -short.
func TestPasswordRotationClearsLockout(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := gw.UpsertRole(ctx, storage.Role{ID: "owner", Official: true, Permissions: []string{"*:*", ">"}}); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	root, _ := gw.AuthenticateBearer(ctx, zeros)
	all := scope.Set{All: true}

	oldHash, _ := auth.HashPassword("orange-boat-42x")
	alice, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice", PasswordHash: oldHash}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// lockAlice trips the brute-force lock: five wrong passwords (the threshold), then a
	// confirmation that the account is now locked (even the correct password is refused).
	lockAlice := func() {
		t.Helper()
		for i := 0; i < 5; i++ {
			if _, err := gw.AuthenticatePassword(ctx, "alice", "wrong-guess"); !errors.Is(err, storage.ErrBadCredentials) {
				t.Fatalf("wrong attempt %d: want ErrBadCredentials, got %v", i+1, err)
			}
		}
		if _, err := gw.AuthenticatePassword(ctx, "alice", "orange-boat-42x"); !errors.Is(err, storage.ErrAccountLocked) {
			t.Fatalf("account should be locked: want ErrAccountLocked, got %v", err)
		}
	}

	// Admin reset (SetPrincipalPassword) clears the lock: the new secret authenticates
	// immediately, with no wait for the 15-minute window to pass.
	lockAlice()
	adminHash, _ := auth.HashPassword("purple-canyon-7")
	if err := gw.SetPrincipalPassword(ctx, root.ID, alice.ID, adminHash, all); err != nil {
		t.Fatalf("admin reset password: %v", err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "alice", "purple-canyon-7"); err != nil {
		t.Fatalf("admin reset should clear the lock and authenticate now: %v", err)
	}

	// Self-service change (SetPassword) also clears the lock: re-lock the account, rotate
	// by username, and the new secret authenticates immediately.
	lockAlice2 := func() {
		t.Helper()
		for i := 0; i < 5; i++ {
			if _, err := gw.AuthenticatePassword(ctx, "alice", "wrong-guess"); !errors.Is(err, storage.ErrBadCredentials) {
				t.Fatalf("re-lock wrong attempt %d: want ErrBadCredentials, got %v", i+1, err)
			}
		}
		if _, err := gw.AuthenticatePassword(ctx, "alice", "purple-canyon-7"); !errors.Is(err, storage.ErrAccountLocked) {
			t.Fatalf("account should be locked again: want ErrAccountLocked, got %v", err)
		}
	}
	lockAlice2()
	selfHash, _ := auth.HashPassword("green-meadow-9")
	if ok, err := gw.SetPassword(ctx, "alice", selfHash); err != nil || !ok {
		t.Fatalf("self-service set password: ok=%v err=%v", ok, err)
	}
	if _, err := gw.AuthenticatePassword(ctx, "alice", "green-meadow-9"); err != nil {
		t.Fatalf("self-service change should clear the lock and authenticate now: %v", err)
	}
}
