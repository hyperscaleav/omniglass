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

// TestUserDirectory drives the admin read + create surface against a real
// Postgres: an all-scope reader lists and gets users; a non-all scope is
// refused (users are not scope-tree entities); create installs a human with
// an optional password, refuses a duplicate username, and audits the write.
// Skipped under -short.
func TestUserDirectory(t *testing.T) {
	t.Parallel()
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

	// A non-all scope is refused for both reads (a user is not under a location).
	if _, err := gw.ListUsers(ctx, scope.Set{}, false); !errors.Is(err, storage.ErrUserForbidden) {
		t.Fatalf("list with empty scope: want ErrUserForbidden, got %v", err)
	}
	if _, err := gw.GetUser(ctx, owner.ID, scope.Set{IDs: []string{"HQ"}}); !errors.Is(err, storage.ErrUserForbidden) {
		t.Fatalf("get with location scope: want ErrUserForbidden, got %v", err)
	}

	// All-scope lists the owner.
	list, err := gw.ListUsers(ctx, all, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Person == nil || list[0].Person.Username != "root" {
		t.Fatalf("expected just the owner, got %+v", list)
	}

	// Create a human with an initial password.
	hash, err := auth.HashPassword("alice-s3cret")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	alice, err := gw.CreatePerson(ctx, owner.ID, storage.PersonSpec{
		Username: "alice", Email: "alice@example.test", Label: "Alice", PasswordHash: hash,
	}, all)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if alice.Kind != "person" || alice.Person == nil || alice.Person.Username != "alice" || alice.Person.Email != "alice@example.test" {
		t.Fatalf("unexpected created user: %+v", alice)
	}
	if len(alice.Grants) != 0 {
		t.Fatalf("a fresh user has no grants, got %+v", alice.Grants)
	}

	// The installed password authenticates; the directory now has two.
	if _, err := gw.AuthenticatePassword(ctx, "alice", "alice-s3cret"); err != nil {
		t.Fatalf("created password should authenticate: %v", err)
	}
	if list, err := gw.ListUsers(ctx, all, false); err != nil || len(list) != 2 {
		t.Fatalf("list after create: want 2, got %d (err %v)", len(list), err)
	}
	if got, err := gw.GetUser(ctx, alice.ID, all); err != nil || got.Person.Username != "alice" {
		t.Fatalf("get alice: %+v (err %v)", got, err)
	}

	// A duplicate username is refused.
	if _, err := gw.CreatePerson(ctx, owner.ID, storage.PersonSpec{Username: "alice"}, all); !errors.Is(err, storage.ErrUsernameTaken) {
		t.Fatalf("duplicate username: want ErrUsernameTaken, got %v", err)
	}
	// Create is refused without an all-scope grant.
	if _, err := gw.CreatePerson(ctx, owner.ID, storage.PersonSpec{Username: "bob"}, scope.Set{}); !errors.Is(err, storage.ErrUserForbidden) {
		t.Fatalf("create without all scope: want ErrUserForbidden, got %v", err)
	}
	// An unknown id, and a malformed (non-uuid) id, are both a clean not-found.
	if _, err := gw.GetUser(ctx, "00000000-0000-0000-0000-000000000000", all); !errors.Is(err, storage.ErrUserNotFound) {
		t.Fatalf("get unknown: want ErrUserNotFound, got %v", err)
	}
	if _, err := gw.GetUser(ctx, "not-a-uuid", all); !errors.Is(err, storage.ErrUserNotFound) {
		t.Fatalf("get malformed id: want ErrUserNotFound, got %v", err)
	}
}

// TestUserListGroupsKinds pins the directory ordering rule: people list
// before outposts no matter which was created first. Creation stamps are not a
// stable cross-kind order (bootstrap, seeding, and enrollment write in separate
// transactions), and a stamp-led sort flapped the screenshot gate when a run's
// outpost landed an earlier stamp than its people. Skipped under -short.
func TestUserListGroupsKinds(t *testing.T) {
	t.Parallel()
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
	zeros := make([]byte, 32)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: zeros, Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	owner, err := gw.AuthenticateBearer(ctx, zeros)
	if err != nil {
		t.Fatalf("resolve owner: %v", err)
	}
	all := scope.Set{All: true}

	// The outpost lands first, so its created_at precedes the second person's: the
	// exact shape that put an outpost above the people under a stamp-led sort.
	if _, err := gw.CreateOutpost(ctx, owner.ID, storage.OutpostSpec{Name: "edge-1"}, all, all); err != nil {
		t.Fatalf("create outpost: %v", err)
	}
	if _, err := gw.CreatePerson(ctx, owner.ID, storage.PersonSpec{Username: "later-human"}, all); err != nil {
		t.Fatalf("create human: %v", err)
	}

	list, err := gw.ListUsers(ctx, all, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var kinds []string
	for _, pr := range list {
		kinds = append(kinds, pr.Kind)
	}
	want := []string{"person", "person", "machine"}
	if len(kinds) != 3 || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] {
		t.Fatalf("kind order: want %v, got %v", want, kinds)
	}
	// Within the people, creation order holds: the bootstrap owner first.
	if list[0].Person == nil || list[0].Person.Username != "root" || list[1].Person == nil || list[1].Person.Username != "later-human" {
		t.Fatalf("human order: want root then later-human, got %+v", list)
	}
}
