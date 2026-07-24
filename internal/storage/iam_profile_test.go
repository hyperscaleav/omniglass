package storage_test

import (
	"context"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestUpdateHumanProfile proves the self-profile write against a real Postgres:
// a provided field is set, an absent field is left unchanged, and an explicitly
// empty field clears the nullable column. Skipped under -short.
func TestUpdateHumanProfile(t *testing.T) {
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

	// The bearer secret_hash is all-zeros, so AuthenticateBearer(zeros) resolves the
	// owner and hands back the principal id the profile write needs.
	zeros := make([]byte, 32)
	created, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{
		Username: "ops", Email: "ops@old.example", DisplayName: "Old Name",
		SecretHash: zeros, Prefix: "abcd1234",
	})
	if err != nil || !created {
		t.Fatalf("bootstrap: created=%v err=%v", created, err)
	}
	pr, err := gw.AuthenticateBearer(ctx, zeros)
	if err != nil {
		t.Fatalf("resolve owner: %v", err)
	}
	pid := pr.ID

	reload := func() *storage.HumanProfile {
		t.Helper()
		p, err := gw.AuthenticateBearer(ctx, zeros)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		return p.Human
	}

	// Update only the display name: email is left untouched.
	newName := "New Name"
	if err := gw.UpdateHumanProfile(ctx, pid, storage.HumanProfilePatch{DisplayName: &newName}); err != nil {
		t.Fatalf("update display name: %v", err)
	}
	if h := reload(); h.DisplayName != "New Name" || h.Email != "ops@old.example" {
		t.Fatalf("after name update: got display=%q email=%q", h.DisplayName, h.Email)
	}

	// Update the email and explicitly clear the display name (empty -> NULL).
	empty := ""
	newEmail := "new@example.test"
	if err := gw.UpdateHumanProfile(ctx, pid, storage.HumanProfilePatch{Email: &newEmail, DisplayName: &empty}); err != nil {
		t.Fatalf("update email + clear name: %v", err)
	}
	if h := reload(); h.Email != "new@example.test" || h.DisplayName != "" {
		t.Fatalf("after email update: got display=%q email=%q", h.DisplayName, h.Email)
	}

	// An empty patch is a no-op that changes nothing.
	if err := gw.UpdateHumanProfile(ctx, pid, storage.HumanProfilePatch{}); err != nil {
		t.Fatalf("empty patch: %v", err)
	}
	if h := reload(); h.Email != "new@example.test" || h.Username != "ops" {
		t.Fatalf("empty patch changed data: %+v", h)
	}
}
