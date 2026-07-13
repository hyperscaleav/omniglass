package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestMustChangePasswordFlag proves the force-change flag against a real Postgres
// (issue #159): an admin reset sets human.must_change_password, and the target's
// own change-password clears it. Read back through AuthenticateBearer, the path the
// authn gate uses. Skipped under -short.
func TestMustChangePasswordFlag(t *testing.T) {
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

	// flag reads the current must_change_password for alice via a fresh bearer.
	flag := func(label string) bool {
		t.Helper()
		_, bh, bp, _ := auth.NewBearerToken()
		if ok, err := gw.IssueBearerCredential(ctx, storage.BearerIssue{Username: "alice", SecretHash: bh, Prefix: bp, Purpose: "token", ExpiresAt: nil}); err != nil || !ok {
			t.Fatalf("%s: issue bearer: ok=%v err=%v", label, ok, err)
		}
		pr, err := gw.AuthenticateBearer(ctx, bh)
		if err != nil || pr.Human == nil {
			t.Fatalf("%s: authenticate: pr=%v err=%v", label, pr, err)
		}
		return pr.Human.MustChangePassword
	}

	// A freshly created account is not flagged.
	if flag("initial") {
		t.Fatal("a new account should not require a password change")
	}

	// An admin reset flags the account (and revokes the bearer just issued; flag()
	// mints a new one each call, so the read still works).
	newHash, _ := auth.HashPassword("purple-canyon-7")
	if err := gw.SetPrincipalPassword(ctx, root.ID, alice.ID, newHash, all); err != nil {
		t.Fatalf("admin reset: %v", err)
	}
	if !flag("after reset") {
		t.Fatal("an admin reset should require a password change")
	}

	// The target changing their own password clears the flag.
	selfHash, _ := auth.HashPassword("silver-meadow-9")
	if ok, err := gw.SetPassword(ctx, "alice", selfHash); err != nil || !ok {
		t.Fatalf("self change: ok=%v err=%v", ok, err)
	}
	if flag("after self change") {
		t.Fatal("a self-service change should clear the force-change flag")
	}
}
