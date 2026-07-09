package storage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestLoginLockout proves the brute-force policy against a real Postgres (issue
// #158): a run of wrong passwords locks the account, a correct password inside the
// window is still refused, forcing the lock to expire lets the correct password
// through and clears the counter, and a success below the threshold resets it.
func TestLoginLockout(t *testing.T) {
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

	pwHash, _ := auth.HashPassword("orange-boat-42x")
	if _, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice", PasswordHash: pwHash}, all); err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// A raw connection lets the test force the lock window to expire (the policy is a
	// 15-minute cooldown; waiting is not an option in a unit run).
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("raw conn: %v", err)
	}
	defer conn.Close(ctx)
	expireLock := func() {
		if _, err := conn.Exec(ctx, `update human set locked_until = now() - interval '1 hour' where username = 'alice'`); err != nil {
			t.Fatalf("expire lock: %v", err)
		}
	}

	// Five wrong passwords (the threshold): each is a generic bad-credential error,
	// and the fifth trips the lock.
	for i := 0; i < 5; i++ {
		if _, err := gw.AuthenticatePassword(ctx, "alice", "wrong-guess"); !errors.Is(err, storage.ErrBadCredentials) {
			t.Fatalf("wrong attempt %d: want ErrBadCredentials, got %v", i+1, err)
		}
	}

	// Inside the lock window the CORRECT password is still refused (as ErrAccountLocked).
	if _, err := gw.AuthenticatePassword(ctx, "alice", "orange-boat-42x"); !errors.Is(err, storage.ErrAccountLocked) {
		t.Fatalf("correct password while locked: want ErrAccountLocked, got %v", err)
	}
	// A wrong password while locked is also the locked error (the lock wins).
	if _, err := gw.AuthenticatePassword(ctx, "alice", "still-wrong"); !errors.Is(err, storage.ErrAccountLocked) {
		t.Fatalf("wrong password while locked: want ErrAccountLocked, got %v", err)
	}

	// Force the window to expire: the correct password now authenticates and clears
	// the counter.
	expireLock()
	if _, err := gw.AuthenticatePassword(ctx, "alice", "orange-boat-42x"); err != nil {
		t.Fatalf("correct password after cooldown should authenticate: %v", err)
	}

	// The counter reset means a fresh run of wrong guesses starts from zero: four
	// misses do not lock (still generic), and a correct password still works.
	for i := 0; i < 4; i++ {
		if _, err := gw.AuthenticatePassword(ctx, "alice", "wrong-guess"); !errors.Is(err, storage.ErrBadCredentials) {
			t.Fatalf("post-reset wrong attempt %d: want ErrBadCredentials, got %v", i+1, err)
		}
	}
	if _, err := gw.AuthenticatePassword(ctx, "alice", "orange-boat-42x"); err != nil {
		t.Fatalf("correct password below threshold should clear and authenticate: %v", err)
	}
}
