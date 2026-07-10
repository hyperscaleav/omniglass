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

// TestResolvePrincipalRef proves the uuid-or-username addressing against a real
// Postgres (issue #163): a username resolves to its principal id, a uuid passes
// through unchanged (even an unknown one, so the caller's own not-found applies),
// and an unknown username is ErrPrincipalNotFound. Skipped under -short.
func TestResolvePrincipalRef(t *testing.T) {
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
	alice, err := gw.CreateHumanPrincipal(ctx, root.ID, storage.HumanSpec{Username: "alice", PasswordHash: pwHash}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	// A username resolves to the principal id.
	if got, err := gw.ResolvePrincipalRef(ctx, "alice"); err != nil || got != alice.ID {
		t.Fatalf("resolve username: got (%q, %v), want (%q, nil)", got, err, alice.ID)
	}
	// A uuid passes through unchanged.
	if got, err := gw.ResolvePrincipalRef(ctx, alice.ID); err != nil || got != alice.ID {
		t.Fatalf("resolve uuid: got (%q, %v), want (%q, nil)", got, err, alice.ID)
	}
	// An unknown uuid still passes through (not a username lookup), so downstream
	// not-found handling is unchanged for uuids.
	const ghostID = "00000000-0000-0000-0000-000000000000"
	if got, err := gw.ResolvePrincipalRef(ctx, ghostID); err != nil || got != ghostID {
		t.Fatalf("resolve unknown uuid: got (%q, %v), want (%q, nil)", got, err, ghostID)
	}
	// An unknown username is not found.
	if _, err := gw.ResolvePrincipalRef(ctx, "nobody"); !errors.Is(err, storage.ErrPrincipalNotFound) {
		t.Fatalf("resolve unknown username: want ErrPrincipalNotFound, got %v", err)
	}
}
