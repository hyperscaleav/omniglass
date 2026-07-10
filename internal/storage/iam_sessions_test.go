package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestListAndRevokeBearerCredentials proves a principal's own bearer credentials
// list with their metadata (id, prefix, created_at, expires_at) and never the
// secret, that one is revocable by id scoped to the owning principal, and that
// another principal's credential id is not revocable (a no-op, false). This backs
// the self-service session list and revoke (issue #172, slice 1).
func TestListAndRevokeBearerCredentials(t *testing.T) {
	gw := storagetest.NewDB(t)
	ctx := context.Background()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Bootstrap an owner; its bootstrap bearer is the first credential (a token, so
	// it has no expiry).
	_, hash1, prefix1, _ := auth.NewBearerToken()
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash1, Prefix: prefix1}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	pid, err := gw.ResolvePrincipalRef(ctx, "root")
	if err != nil {
		t.Fatalf("resolve root: %v", err)
	}

	// Issue a second bearer for the same principal, a bounded session (expires_at set).
	_, hash2, prefix2, _ := auth.NewBearerToken()
	future := time.Now().Add(12 * time.Hour)
	if ok, err := gw.IssueBearerCredential(ctx, "root", hash2, prefix2, &future); err != nil || !ok {
		t.Fatalf("issue second: ok=%v err=%v", ok, err)
	}

	// List (as the bootstrap credential's session) returns both, with metadata and no
	// secret; the bootstrap credential is flagged current, the other is not.
	creds, err := gw.ListBearerCredentials(ctx, pid, hash1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(creds) != 2 {
		t.Fatalf("list = %d creds, want 2", len(creds))
	}
	byPrefix := map[string]storage.BearerCredential{}
	for _, c := range creds {
		if c.ID == "" || c.Prefix == "" {
			t.Errorf("credential missing id/prefix: %+v", c)
		}
		if c.CreatedAt.IsZero() {
			t.Errorf("credential missing created_at: %+v", c)
		}
		byPrefix[c.Prefix] = c
	}
	// The bootstrap token never expires; the issued one is a bounded session.
	if got := byPrefix[prefix1]; got.ExpiresAt != nil {
		t.Errorf("bootstrap credential expiry = %v, want nil (a token)", got.ExpiresAt)
	}
	if got := byPrefix[prefix2]; got.ExpiresAt == nil {
		t.Errorf("session credential expiry = nil, want a bounded time")
	}
	// Current marks the credential that authenticated this list, and only it.
	if !byPrefix[prefix1].Current {
		t.Errorf("the listing credential should be marked current")
	}
	if byPrefix[prefix2].Current {
		t.Errorf("a different credential must not be marked current")
	}

	// A second principal cannot revoke root's credential: the revoke is scoped to the
	// owning principal, so a mismatched principal id is a no-op that returns false and
	// deletes nothing.
	mallory, err := gw.CreateHumanPrincipal(ctx, pid, storage.HumanSpec{Username: "mallory"}, scope.Set{All: true})
	if err != nil {
		t.Fatalf("create mallory: %v", err)
	}
	sessionID := byPrefix[prefix2].ID
	if ok, err := gw.RevokeBearerByID(ctx, mallory.ID, sessionID); err != nil || ok {
		t.Fatalf("cross-principal revoke = (%v, %v), want (false, nil)", ok, err)
	}
	if creds, err := gw.ListBearerCredentials(ctx, pid, hash1); err != nil || len(creds) != 2 {
		t.Fatalf("after cross-principal revoke root should still have 2 creds, got %d (err %v)", len(creds), err)
	}

	// The owner revokes its own session by id: it drops from the list, and the
	// remaining credential still authenticates.
	if ok, err := gw.RevokeBearerByID(ctx, pid, sessionID); err != nil || !ok {
		t.Fatalf("revoke own = (%v, %v), want (true, nil)", ok, err)
	}
	after, err := gw.ListBearerCredentials(ctx, pid, hash1)
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(after) != 1 || after[0].Prefix != prefix1 {
		t.Fatalf("after revoke = %+v, want only the bootstrap credential", after)
	}
	if pr, err := gw.AuthenticateBearer(ctx, hash1); err != nil || pr.ID != pid {
		t.Fatalf("bootstrap credential should still authenticate: pr=%v err=%v", pr, err)
	}

	// Revoking the same id again is a no-op (already gone), and a malformed id is a
	// clean false, not an error (the API maps it to 404, never a 500).
	if ok, err := gw.RevokeBearerByID(ctx, pid, sessionID); err != nil || ok {
		t.Fatalf("re-revoke = (%v, %v), want (false, nil)", ok, err)
	}
	if ok, err := gw.RevokeBearerByID(ctx, pid, "not-a-uuid"); err != nil || ok {
		t.Fatalf("malformed id revoke = (%v, %v), want (false, nil)", ok, err)
	}
}
