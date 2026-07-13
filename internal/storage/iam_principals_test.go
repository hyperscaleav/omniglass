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

// TestPrincipalDirectory drives the admin read + create surface against a real
// Postgres: an all-scope reader lists and gets principals; a non-all scope is
// refused (principals are not scope-tree entities); create installs a human with
// an optional password, refuses a duplicate username, and audits the write.
// Skipped under -short.
func TestPrincipalDirectory(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()

	if err := gw.UpsertRole(ctx, storage.Role{ID: "owner", Official: true, Permissions: []string{"*:*"}}); err != nil {
		t.Fatalf("seed owner role: %v", err)
	}
	// Bootstrap the owner with an all-zero bearer secret so we can resolve its id
	// (the audit actor) via AuthenticateBearer.
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	owner, err := gw.AuthenticateBearer(ctx, zeros)
	if err != nil {
		t.Fatalf("resolve owner: %v", err)
	}
	all := scope.Set{All: true}

	// A non-all scope is refused for both reads (a principal is not under a location).
	if _, err := gw.ListPrincipals(ctx, scope.Set{}, false); !errors.Is(err, storage.ErrPrincipalForbidden) {
		t.Fatalf("list with empty scope: want ErrPrincipalForbidden, got %v", err)
	}
	if _, err := gw.GetPrincipal(ctx, owner.ID, scope.Set{IDs: []string{"HQ"}}); !errors.Is(err, storage.ErrPrincipalForbidden) {
		t.Fatalf("get with location scope: want ErrPrincipalForbidden, got %v", err)
	}

	// All-scope lists the owner.
	list, err := gw.ListPrincipals(ctx, all, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Human == nil || list[0].Human.Username != "root" {
		t.Fatalf("expected just the owner, got %+v", list)
	}

	// Create a human with an initial password.
	hash, err := auth.HashPassword("alice-s3cret")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	alice, err := gw.CreateHumanPrincipal(ctx, owner.ID, storage.HumanSpec{
		Username: "alice", Email: "alice@example.test", DisplayName: "Alice", PasswordHash: hash,
	}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if alice.Kind != "human" || alice.Human == nil || alice.Human.Username != "alice" || alice.Human.Email != "alice@example.test" {
		t.Fatalf("unexpected created principal: %+v", alice)
	}
	if len(alice.Grants) != 0 {
		t.Fatalf("a fresh principal has no grants, got %+v", alice.Grants)
	}

	// The installed password authenticates; the directory now has two.
	if _, err := gw.AuthenticatePassword(ctx, "alice", "alice-s3cret"); err != nil {
		t.Fatalf("created password should authenticate: %v", err)
	}
	if list, err := gw.ListPrincipals(ctx, all, false); err != nil || len(list) != 2 {
		t.Fatalf("list after create: want 2, got %d (err %v)", len(list), err)
	}
	if got, err := gw.GetPrincipal(ctx, alice.ID, all); err != nil || got.Human.Username != "alice" {
		t.Fatalf("get alice: %+v (err %v)", got, err)
	}

	// A duplicate username is refused.
	if _, err := gw.CreateHumanPrincipal(ctx, owner.ID, storage.HumanSpec{Username: "alice"}, all); !errors.Is(err, storage.ErrUsernameTaken) {
		t.Fatalf("duplicate username: want ErrUsernameTaken, got %v", err)
	}
	// Create is refused without an all-scope grant.
	if _, err := gw.CreateHumanPrincipal(ctx, owner.ID, storage.HumanSpec{Username: "bob"}, scope.Set{}); !errors.Is(err, storage.ErrPrincipalForbidden) {
		t.Fatalf("create without all scope: want ErrPrincipalForbidden, got %v", err)
	}
	// An unknown id, and a malformed (non-uuid) id, are both a clean not-found.
	if _, err := gw.GetPrincipal(ctx, "00000000-0000-0000-0000-000000000000", all); !errors.Is(err, storage.ErrPrincipalNotFound) {
		t.Fatalf("get unknown: want ErrPrincipalNotFound, got %v", err)
	}
	if _, err := gw.GetPrincipal(ctx, "not-a-uuid", all); !errors.Is(err, storage.ErrPrincipalNotFound) {
		t.Fatalf("get malformed id: want ErrPrincipalNotFound, got %v", err)
	}
}
